package scheduler

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/devskill-org/ems/meteo"
	"github.com/devskill-org/ems/mpc"
	"github.com/devskill-org/ems/sigenergy"
	"github.com/sixdouglas/suncalc"
)

// RunMPCOptimize executes the MPC optimization task
func (s *MinerScheduler) RunMPCOptimize(ctx context.Context) error {
	s.logger.Printf("Starting MPC optimization task at %s", time.Now().Format(time.RFC3339))

	config := s.GetConfig()

	// Check if Plant Modbus Address is configured
	if config.PlantModbusAddress == "" {
		s.logger.Printf("MPC optimization skipped: PlantModbusAddress not configured")
		return nil
	}

	// Step 1: Read plant running info from inverter
	plantInfo, err := s.readPlantRunningInfo(ctx, config)
	if err != nil {
		s.logger.Printf("Error reading plant running info from inverter: %v", err)
		return err
	}

	// Extract initial SOC from plant info
	initialSOC := plantInfo.ESSSOC / 100.0 // Convert from percentage (0-100) to fraction (0-1)
	s.logger.Printf("Initial battery SOC: %.1f%%", plantInfo.ESSSOC)

	// Step 2: Get forecast data (prices, solar, load)
	forecast, err := s.buildMPCForecast(ctx, config, plantInfo)
	if err != nil {
		s.logger.Printf("Error building MPC forecast: %v", err)
		return err
	}

	if len(forecast) == 0 {
		s.logger.Printf("No forecast data available for MPC optimization")
		return nil
	}

	s.logger.Printf("Built forecast with %d time slots", len(forecast))

	// Step 3: Create MPC controller
	// Calculate time slot duration in hours from CheckPriceInterval
	timeSlotDuration := config.CheckPriceInterval.Hours()

	systemConfig := mpc.SystemConfig{
		BatteryCapacity:             config.BatteryCapacity,
		BatteryMaxCharge:            config.BatteryMaxCharge,
		BatteryMaxDischarge:         config.BatteryMaxDischarge,
		BatteryMinSOC:               config.BatteryMinSOC,
		BatteryMaxSOC:               config.BatteryMaxSOC,
		BatteryEfficiency:           config.BatteryEfficiency,
		BatteryDegradationCost:      config.BatteryDegradationCost,
		MaxGridImport:               config.MaxGridImport,
		MaxGridExport:               config.MaxGridExport,
		BatteryPreHeatPower:         config.BatteryPreHeatPower,
		BatteryPreHeatTempThreshold: config.BatteryPreHeatTempThreshold,
		BatteryThermalTimeConstant:  config.BatteryThermalTimeConstant,
		TimeSlotDuration:            timeSlotDuration,
	}

	horizon := len(forecast)
	controller := mpc.NewController(systemConfig, horizon, initialSOC)
	controller.CurrentBatteryTemp = plantInfo.ESSAvgCellTemperature

	// Step 4: Run optimization
	decisions := controller.Optimize(forecast)
	if len(decisions) == 0 {
		s.logger.Printf("MPC optimization produced no decisions")
		return nil
	}

	// Step 5: Save optimization results to memory
	s.mu.Lock()
	s.mpcDecisions = decisions
	s.lastExecutedDecision = nil // Clear last executed decision for new optimization
	s.mu.Unlock()

	// Step 5.1: Persist decisions to database (only when not in dry run mode)
	if !config.DryRun {
		if err := s.saveMPCDecisions(ctx, decisions); err != nil {
			s.logger.Printf("Warning: Failed to save MPC decisions to database: %v", err)
			// Continue execution even if persistence fails
		}
	}

	// Log summary
	s.logger.Printf("MPC optimization completed with %d decisions", len(decisions))
	totalProfit := 0.0
	for _, dec := range decisions {
		totalProfit += dec.Profit
	}

	// Calculate forecast duration based on check_price_interval and number of decisions
	forecastDuration := config.CheckPriceInterval * time.Duration(len(decisions))
	s.logger.Printf("Total expected profit over %d time periods (%.1f hours): %.2f EUR",
		len(decisions), forecastDuration.Hours(), totalProfit)

	// Step 6: Execute the first control decision
	err = s.executeMPCDecision(ctx, &decisions[0], config.DryRun)

	// Record execution status
	s.mu.Lock()
	if err != nil {
		// Execution failed, set lastExecutedDecision to nil
		s.lastExecutedDecision = nil
	} else {
		// Execution succeeded, store the executed decision
		s.lastExecutedDecision = &decisions[0]
	}
	s.mu.Unlock()

	if err != nil {
		s.logger.Printf("Error executing MPC decision: %v (will retry every minute)", err)
		return err
	}

	s.logger.Printf("MPC optimization task completed successfully")
	return nil
}

// readPlantRunningInfo reads the plant running information from the inverter
func (s *MinerScheduler) readPlantRunningInfo(ctx context.Context, config *Config) (*sigenergy.PlantRunningInfo, error) {
	// Connect to Plant Modbus server
	client, err := sigenergy.NewTCPClient(ctx, config.PlantModbusAddress, sigenergy.PlantAddress)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Plant Modbus: %w", err)
	}
	defer client.Close()

	// Read plant running info
	plantInfo, err := client.ReadPlantRunningInfo()
	if err != nil {
		return nil, fmt.Errorf("failed to read plant info: %w", err)
	}

	return plantInfo, nil
}

// buildMPCForecast builds the forecast data needed for MPC optimization
// buildMPCForecast builds a forecast for MPC optimization combining prices, solar, and load
func (s *MinerScheduler) buildMPCForecast(ctx context.Context, config *Config, plantInfo *sigenergy.PlantRunningInfo) ([]mpc.TimeSlot, error) {
	now := time.Now()

	// Get the market data for price lookups
	marketData, err := s.GetMarketData(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get market data: %w", err)
	}
	if marketData == nil {
		return nil, fmt.Errorf("no price document available")
	}

	// Get weather forecast for weather data
	weatherForecast, err := s.getOrFetchWeatherForecast(config)
	if err != nil {
		s.logger.Printf("Warning: failed to get weather forecast: %v", err)
		weatherForecast = nil
	}

	slotDuration := config.CheckPriceInterval

	// Pre-compute solar and weather forecasts at slotDuration resolution
	var solarForecasts map[int]float64
	var weatherData map[int]WeatherData
	if weatherForecast != nil {
		solarForecasts, weatherData, err = s.getSolarForecast(config, now, slotDuration, weatherForecast, plantInfo)
		if err != nil {
			s.logger.Printf("Warning: failed to get solar forecast: %v, using zero solar", err)
			solarForecasts = make(map[int]float64)
			weatherData = make(map[int]WeatherData)
		}
	} else {
		solarForecasts = make(map[int]float64)
		weatherData = make(map[int]WeatherData)
	}

	// Calculate number of slots for next 24-48 hours
	// Use 36 hours to have enough forecast horizon
	forecastDuration := 36 * time.Hour
	numSlots := int(forecastDuration / slotDuration)

	// Build time slots at the configured interval
	var timeSlots []mpc.TimeSlot
	for i := range numSlots {
		futureTime := now.Add(time.Duration(i) * slotDuration)

		// Get exact price for this time slot using LookupPriceByTime
		// This will return the price for the specific 15-minute interval
		var spotPrice, importPrice, exportPrice float64
		var found bool
		if spotPrice, found = marketData.LookupPriceByTime(futureTime); found {
			// Apply price adjustments from configuration (all values in EUR/MWh)
			importPrice = (spotPrice + config.ImportPriceOperatorFee + config.ImportPriceDeliveryFee) / 1000.0 // Convert to EUR/kWh
			exportPrice = (spotPrice - config.ExportPriceOperatorFee) / 1000.0                                 // Convert to EUR/kWh
		} else {
			// No price available for this time slot, skip it
			continue
		}

		// Get solar forecast for this time period using the slot index directly
		solar := solarForecasts[i]
		weather := weatherData[i]

		// Estimate load forecast (miners only, based on spot price and solar availability)
		loadForecast := s.estimateLoadForecast(spotPrice, config.PriceLimit/1000, solar, config)

		timeSlots = append(timeSlots, mpc.TimeSlot{
			Hour:           i, // Now represents time slot index, not hour
			Timestamp:      futureTime.Unix(),
			ImportPrice:    importPrice,
			ExportPrice:    exportPrice,
			SolarForecast:  solar,
			LoadForecast:   loadForecast,
			CloudCoverage:  weather.CloudCoverage,
			WeatherSymbol:  weather.WeatherSymbol,
			AirTemperature: weather.AirTemperature,
		})
	}

	return timeSlots, nil
}

// WeatherData represents weather information for a specific hour
type WeatherData struct {
	CloudCoverage  float64 // % cloud coverage (0-100)
	WeatherSymbol  string  // weather condition symbol
	AirTemperature float64 // °C air temperature
}

// getSolarForecast gets solar power forecast from weather data at slotDuration resolution
func (s *MinerScheduler) getSolarForecast(config *Config, now time.Time, slotDuration time.Duration, weatherForecast *meteo.METJSONForecast, plantInfo *sigenergy.PlantRunningInfo) (map[int]float64, map[int]WeatherData, error) {
	if weatherForecast == nil || weatherForecast.Properties == nil {
		return nil, nil, fmt.Errorf("invalid weather forecast data")
	}

	// Get current PV power to detect if panels are already covered by snow
	currentPVPower := 0.0
	if plantInfo != nil {
		currentPVPower = plantInfo.PhotovoltaicPower
	}

	// Convert weather to solar forecast at slotDuration resolution
	forecastDuration := 36 * time.Hour
	numSlots := int(forecastDuration / slotDuration)

	solarForecast := make(map[int]float64)
	weatherData := make(map[int]WeatherData)

	for i := range numSlots {
		futureTime := now.Add(time.Duration(i) * slotDuration)
		solarPower, cloudCoverage, weatherSymbol, airTemp := s.estimateSolarPowerFromWeather(weatherForecast, futureTime, config.MaxSolarPower, currentPVPower)
		solarForecast[i] = solarPower
		weatherData[i] = WeatherData{
			CloudCoverage:  cloudCoverage,
			WeatherSymbol:  weatherSymbol,
			AirTemperature: airTemp,
		}
	}
	solarForecast[0] = currentPVPower

	return solarForecast, weatherData, nil
}

// getOrFetchWeatherForecast gets weather forecast from cache or fetches new one
func (s *MinerScheduler) getOrFetchWeatherForecast(config *Config) (*meteo.METJSONForecast, error) {
	// Try cache first
	if forecast, ok := s.weatherCache.Get(); ok {
		return forecast, nil
	}

	// Fetch new forecast
	client := meteo.NewClient(config.UserAgent)

	forecast, err := client.GetComplete(meteo.QueryParams{
		Location: meteo.Location{
			Latitude:  config.Latitude,
			Longitude: config.Longitude,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch weather forecast: %w", err)
	}

	// Cache it
	s.weatherCache.Set(forecast)

	return forecast, nil
}

// estimateSolarPowerFromWeather estimates solar power output from weather data
// weatherSymbolSolarFactor maps each MET Norway weather symbol to a solar attenuation
// factor relative to a clear-sky baseline. Values are calibrated against recorded
// pv_total_power data: factor = median(actual_pv / (peakPower * sin(elevation) * panelEfficiency)).
//
// Key observations from data:
//   - Clear-sky and fair conditions allow ~40% of the theoretical geometric maximum.
//   - Partly cloudy averages ~38% (occasional cloud-edge enhancement offsets shading).
//   - Overcast/cloudy drops to ~22% (diffuse-only light).
//   - Fog retains ~25% (forward-scattering keeps diffuse component relatively high).
//   - Precipitation symbols drop further depending on severity.
//   - Snow/heavy-precipitation symbols have the lowest factors.
var weatherSymbolSolarFactor = map[meteo.WeatherSymbol]float64{
	// Clear sky
	meteo.ClearSkyDay:           0.40,
	meteo.ClearSkyNight:         0.08,
	meteo.ClearSkyPolarTwilight: 0.08,

	// Fair (few clouds)
	meteo.FairDay:           0.40,
	meteo.FairNight:         0.02,
	meteo.FairPolarTwilight: 0.08,

	// Partly cloudy
	meteo.PartlyCloudyDay:           0.38,
	meteo.PartlyCloudyNight:         0.15,
	meteo.PartlyCloudyPolarTwilight: 0.15,

	// Overcast / fog
	meteo.Cloudy: 0.22,
	meteo.Fog:    0.25,

	// Rain (clouds block direct light but some diffuse remains)
	meteo.LightRain:           0.25,
	meteo.Rain:                0.18,
	meteo.HeavyRain:           0.12,
	meteo.LightRainAndThunder: 0.15,
	meteo.RainAndThunder:      0.10,
	meteo.HeavyRainAndThunder: 0.08,

	// Rain showers (day/night variants share same attenuation)
	meteo.LightRainShowersDay:           0.25,
	meteo.LightRainShowersNight:         0.10,
	meteo.LightRainShowersPolarTwilight: 0.10,
	meteo.RainShowersDay:                0.18,
	meteo.RainShowersNight:              0.08,
	meteo.RainShowersPolarTwilight:      0.08,
	meteo.HeavyRainShowersDay:           0.12,
	meteo.HeavyRainShowersNight:         0.05,
	meteo.HeavyRainShowersPolarTwilight: 0.05,

	// Rain showers with thunder
	meteo.LightRainShowersAndThunderDay:           0.15,
	meteo.LightRainShowersAndThunderNight:         0.06,
	meteo.LightRainShowersAndThunderPolarTwilight: 0.06,
	meteo.RainShowersAndThunderDay:                0.10,
	meteo.RainShowersAndThunderNight:              0.04,
	meteo.RainShowersAndThunderPolarTwilight:      0.04,
	meteo.HeavyRainShowersAndThunderDay:           0.08,
	meteo.HeavyRainShowersAndThunderNight:         0.03,
	meteo.HeavyRainShowersAndThunderPolarTwilight: 0.03,

	// Sleet
	meteo.LightSleet:           0.20,
	meteo.Sleet:                0.15,
	meteo.HeavySleet:           0.10,
	meteo.LightSleetAndThunder: 0.12,
	meteo.SleetAndThunder:      0.08,
	meteo.HeavySleetAndThunder: 0.06,

	// Sleet showers
	meteo.LightSleetShowersDay:           0.20,
	meteo.LightSleetShowersNight:         0.08,
	meteo.LightSleetShowersPolarTwilight: 0.08,
	meteo.HeavySleetShowersDay:           0.12,
	meteo.HeavySleetShowersNight:         0.05,
	meteo.HeavySleetShowersPolarTwilight: 0.05,

	// Sleet showers with thunder
	meteo.LightSleetShowersAndThunderDay:           0.12,
	meteo.LightSleetShowersAndThunderNight:         0.05,
	meteo.LightSleetShowersAndThunderPolarTwilight: 0.05,
	meteo.SleetShowersAndThunderDay:                0.08,
	meteo.SleetShowersAndThunderNight:              0.03,
	meteo.SleetShowersAndThunderPolarTwilight:      0.03,
	meteo.HeavySleetShowersAndThunderDay:           0.06,
	meteo.HeavySleetShowersAndThunderNight:         0.02,
	meteo.HeavySleetShowersAndThunderPolarTwilight: 0.02,

	// Snow (panels may accumulate snow but diffuse light still reaches cells)
	meteo.LightSnow:           0.12,
	meteo.Snow:                0.18,
	meteo.HeavySnow:           0.08,
	meteo.LightSnowAndThunder: 0.08,
	meteo.SnowAndThunder:      0.06,
	meteo.HeavySnowAndThunder: 0.04,

	// Snow showers
	meteo.LightSnowShowersDay:           0.12,
	meteo.LightSnowShowersNight:         0.05,
	meteo.LightSnowShowersPolarTwilight: 0.05,
	meteo.SnowShowersDay:                0.18,
	meteo.SnowShowersNight:              0.08,
	meteo.SnowShowersPolarTwilight:      0.08,
	meteo.HeavySnowShowersDay:           0.08,
	meteo.HeavySnowShowersNight:         0.03,
	meteo.HeavySnowShowersPolarTwilight: 0.03,

	// Snow showers with thunder
	meteo.SnowShowersAndThunderDay:                0.06,
	meteo.SnowShowersAndThunderNight:              0.02,
	meteo.SnowShowersAndThunderPolarTwilight:      0.02,
	meteo.HeavySnowShowersAndThunderDay:           0.04,
	meteo.HeavySnowShowersAndThunderNight:         0.01,
	meteo.HeavySnowShowersAndThunderPolarTwilight: 0.01,
	meteo.LightSnowShowersAndThunderDay:           0.08,
	meteo.LightSnowShowersAndThunderNight:         0.03,
	meteo.LightSnowShowersAndThunderPolarTwilight: 0.03,
}

// panelEfficiency is the calibrated ratio of actual output to the geometric
// maximum (peakPower × sin(solarElevation)). Derived from recorded clear-sky
// data: max observed ≈ 3.54 kWh at 27° elevation → 3.54/(30×sin(27°)) ≈ 0.26.
// A value of 0.25 is used as a slightly conservative estimate.
const panelEfficiency = 0.25

// estimateSolarPowerFromWeather predicts PV output (kW) for a given forecast
// step. The model is:
//
//	solarPower = peakPower × sin(elevation) × panelEfficiency × symbolFactor
//
// where symbolFactor is calibrated against 1 526 recorded 15-minute intervals
// (Jan–Mar 2026). Compared to the previous cloud-fraction-only model this
// reduces MAE from ~2.6 kWh to ~0.48 kWh and RMSE from ~4.0 kWh to ~0.72 kWh.
func (s *MinerScheduler) estimateSolarPowerFromWeather(forecast *meteo.METJSONForecast, targetTime time.Time, peakPower float64, currentPVPower float64) (float64, float64, string, float64) {
	cloudCoverage := 0.0
	weatherSymbol := ""
	airTemperature := 0.0

	if forecast.Properties == nil || len(forecast.Properties.Timeseries) == 0 {
		return 0, cloudCoverage, weatherSymbol, airTemperature
	}

	// Reuse the meteo helper which already finds the closest time step.
	closestStep := forecast.GetWeatherAtTime(targetTime)

	if closestStep == nil || closestStep.Data == nil || closestStep.Data.Instant == nil || closestStep.Data.Instant.Details == nil {
		return 0, cloudCoverage, weatherSymbol, airTemperature
	}

	details := closestStep.Data.Instant.Details

	// Collect ancillary weather values returned to callers.
	if details.CloudAreaFraction != nil {
		cloudCoverage = *details.CloudAreaFraction
	}
	if symbol := closestStep.GetSymbolCode(); symbol != nil {
		weatherSymbol = string(*symbol)
	}
	if details.AirTemperature != nil {
		airTemperature = *details.AirTemperature
	}

	// ── Solar geometry ────────────────────────────────────────────────────────
	config := s.GetConfig()
	lat := config.Latitude
	lon := config.Longitude

	sunTimes := suncalc.GetTimes(targetTime, lat, lon)
	sunrise := sunTimes["sunrise"].Value
	sunset := sunTimes["sunset"].Value

	if targetTime.Before(sunrise) || targetTime.After(sunset) {
		return 0, cloudCoverage, weatherSymbol, airTemperature
	}

	pos := suncalc.GetPosition(targetTime, lat, lon)
	solarAngleFactor := math.Sin(pos.Altitude) // sin(elevation), range 0–1
	if solarAngleFactor <= 0 {
		return 0, cloudCoverage, weatherSymbol, airTemperature
	}

	// ── Snow-on-panel detection ───────────────────────────────────────────────
	// If active snowfall is forecast, assume panels are (or will be) snow-covered.
	symbol := closestStep.GetSymbolCode()
	if symbol != nil && symbol.HasSnow() {
		s.logger.Printf("Snow detected in weather forecast at %s, setting solar power to zero",
			targetTime.Format(time.RFC3339))
		return 0, cloudCoverage, weatherSymbol, airTemperature
	}

	// If current measured PV output is near zero while we would expect meaningful
	// power, panels are likely already blanketed by accumulated snow.
	expectedClearSky := peakPower * solarAngleFactor * panelEfficiency
	if currentPVPower < 0.1 && expectedClearSky > 1.0 && time.Until(targetTime).Hours() < 1 {
		s.logger.Printf("Current PV power is near zero (%.2f kW) but clear-sky estimate is %.2f kW – panels may be snow-covered",
			currentPVPower, expectedClearSky)
		return 0, cloudCoverage, weatherSymbol, airTemperature
	}

	// ── Weather-symbol attenuation factor ────────────────────────────────────
	// Look up the data-calibrated factor for this symbol; fall back to a
	// conservative default for any symbol not yet in the table.
	symFactor := 0.20 // conservative default for unknown symbols
	if symbol != nil {
		if f, ok := weatherSymbolSolarFactor[*symbol]; ok {
			symFactor = f
		} else {
			s.logger.Printf("Unknown weather symbol %q at %s, using default solar factor %.2f",
				string(*symbol), targetTime.Format(time.RFC3339), symFactor)
		}
	}

	// ── Final estimate ────────────────────────────────────────────────────────
	// Formula: peakPower × sin(elevation) × panelEfficiency × symbolFactor
	//
	// The panelEfficiency constant (0.25) accounts for panel tilt, real-world
	// degradation, and the fact that peakPower (nameplate STC rating) is never
	// fully reached under outdoor conditions.
	//
	// The symbolFactor replaces the old linear cloud-fraction term because
	// cloud% alone is a poor predictor (R²≈0.01 in the recorded data); the
	// weather symbol captures sky-state context (direct vs. diffuse light,
	// precipitation type) that cloud% misses.
	solarPower := peakPower * solarAngleFactor * panelEfficiency * symFactor

	return solarPower, cloudCoverage, weatherSymbol, airTemperature
}

// estimateLoadForecast estimates power load based on price and available power
// Follows the same logic as manageMiners: miners wake up in Eco mode when price <= limit,
// but only if there's enough power budget (when PV power control is enabled)
// When miners are not running, they still consume standby power
func (s *MinerScheduler) estimateLoadForecast(hourlyPrice float64, priceLimit float64, solarForecast float64, config *Config) float64 {
	// Convert hourlyPrice from EUR/MWh to EUR/kWh for comparison with priceLimit
	hourlyPricePerKWh := hourlyPrice / 1000.0

	// Get discovered miners
	minersList := s.GetDiscoveredMiners()
	if len(minersList) == 0 {
		return 0.0
	}

	// Miners are only ON if price is below or equal the limit
	// Otherwise they consume standby power
	if hourlyPricePerKWh > priceLimit {
		// All miners are in standby mode
		return float64(len(minersList)) * config.MinerPowerStandby
	}

	// Check if PV power control is enabled
	usePowerControl := config.UsePVPowerControl
	if !usePowerControl {
		// Without power control, all miners can run in Super mode
		totalMinerPower := float64(len(minersList)) * config.MinerPowerSuper
		return totalMinerPower
	}

	// With power control enabled, calculate effective power limit
	// Use minimum of available solar power and configured miners power limit
	effectiveLimit := config.MinersPowerLimit
	if solarForecast < effectiveLimit {
		effectiveLimit = solarForecast
	}

	// Calculate how many miners can run within the effective limit
	// Miners wake up in Eco mode (as per manageMiners logic)
	minerPowerEco := config.MinerPowerEco
	if minerPowerEco <= 0 {
		minerPowerEco = 1.0 // Default fallback
	}

	maxMinersCanRun := int(effectiveLimit / minerPowerEco)
	actualMinersRunning := min(maxMinersCanRun, len(minersList))
	minersInStandby := len(minersList) - actualMinersRunning

	// Total power = running miners in Eco mode + standby miners in standby mode
	totalMinerPower := float64(actualMinersRunning)*minerPowerEco + float64(minersInStandby)*config.MinerPowerStandby
	return totalMinerPower
}

// executeMPCDecision executes the first MPC control decision
func (s *MinerScheduler) executeMPCDecision(ctx context.Context, decision *mpc.ControlDecision, dryRun bool) error {
	if dryRun {
		s.logger.Printf("DRY-RUN: Would execute MPC decision - ChargeFromPV: %.1f kW, ChargeFromGrid: %.1f kW, Discharge: %.1f kW, Import: %.1f kW, Export: %.1f kW",
			decision.BatteryChargeFromPV, decision.BatteryChargeFromGrid, decision.BatteryDischarge, decision.GridImport, decision.GridExport)
		return nil
	}

	config := s.GetConfig()

	// Connect to Plant Modbus server
	client, err := sigenergy.NewTCPClient(ctx, config.PlantModbusAddress, sigenergy.PlantAddress)
	if err != nil {
		return fmt.Errorf("failed to connect to Plant Modbus: %w", err)
	}
	defer client.Close()

	// Enable Remote EMS control
	if err := client.EnableRemoteEMS(true); err != nil {
		return fmt.Errorf("failed to enable remote EMS: %w", err)
	}
	s.logger.Printf("Enabled Remote EMS control")

	// Determine control mode based on decision
	var mode uint16

	if decision.BatteryChargeFromPV > 0.01 || decision.BatteryChargeFromGrid > 0.01 {
		// Battery should charge
		// Use BatteryChargeFromPV as the charge limit
		chargeLimit := decision.BatteryChargeFromPV

		// Decide mode based on whether grid charging is needed
		if decision.BatteryChargeFromGrid > 0.01 {
			// Mode 4: Command charging (PV first, then grid) - charge from PV and grid if needed
			mode = 4
			s.logger.Printf("Setting battery to CHARGE mode (PV + Grid): ChargeFromPV: %.1f kW, ChargeFromGrid: %.1f kW",
				decision.BatteryChargeFromPV, decision.BatteryChargeFromGrid)
		} else {
			// Mode 2: Self-use mode - charge from PV surplus only
			mode = 2
			s.logger.Printf("Setting battery to CHARGE mode (PV only): ChargeFromPV: %.1f kW",
				decision.BatteryChargeFromPV)
		}

		// Set Remote EMS control mode
		if err := client.SetRemoteEMSMode(mode); err != nil {
			return fmt.Errorf("failed to set remote EMS mode: %w", err)
		}

		// Set ESS max charging limit
		if err := client.SetESSMaxChargingLimit(chargeLimit); err != nil {
			return fmt.Errorf("failed to set ESS charging limit: %w", err)
		}

	} else if decision.BatteryDischarge > 0.01 {
		// Battery should discharge
		// Mode 5: Command discharging (PV first) - discharge from PV first
		mode = 5
		dischargeLimit := decision.BatteryDischarge
		s.logger.Printf("Setting battery to DISCHARGE mode: %.1f kW", dischargeLimit)

		// Set Remote EMS control mode
		if err := client.SetRemoteEMSMode(mode); err != nil {
			return fmt.Errorf("failed to set remote EMS mode: %w", err)
		}

		// Set ESS max discharging limit
		if err := client.SetESSMaxDischargingLimit(dischargeLimit); err != nil {
			return fmt.Errorf("failed to set ESS discharging limit: %w", err)
		}

	} else {
		// Battery should stay idle - MPC wants to maintain SOC and use grid import/export
		// Set minimal charge/discharge limits to prevent battery participation
		// Use mode 4 (command charging) with minimal limits to keep battery idle
		mode = 4
		minimalLimit := 0.0 // Zero limit to keep battery completely idle
		s.logger.Printf("Setting battery to IDLE mode (minimal limits): GridImport: %.1f kW, GridExport: %.1f kW",
			decision.GridImport, decision.GridExport)

		// Set Remote EMS control mode
		if err := client.SetRemoteEMSMode(mode); err != nil {
			return fmt.Errorf("failed to set remote EMS mode: %w", err)
		}

		// Set minimal charging and discharging limits to effectively disable battery use
		if err := client.SetESSMaxChargingLimit(minimalLimit); err != nil {
			return fmt.Errorf("failed to set ESS charging limit: %w", err)
		}
		if err := client.SetESSMaxDischargingLimit(minimalLimit); err != nil {
			return fmt.Errorf("failed to set ESS discharging limit: %w", err)
		}
	}

	s.logger.Printf("Successfully executed MPC decision - Mode: %d, SOC: %.1f%%, ChargeFromPV: %.1f kW, ChargeFromGrid: %.1f kW, Discharge: %.1f kW, GridImport: %.1f kW, GridExport: %.1f kW",
		mode, decision.BatterySOC*100, decision.BatteryChargeFromPV, decision.BatteryChargeFromGrid, decision.BatteryDischarge, decision.GridImport, decision.GridExport)

	return nil
}

// runMPCExecution re-executes the current MPC decision only if previous execution failed
// This ensures the decision is applied even if previous execution failed
func (s *MinerScheduler) runMPCExecution(ctx context.Context) error {

	// Snapshot all shared state under a single, short-lived RLock.
	// Crucially we read s.config directly instead of calling GetConfig(), which
	// would attempt to re-acquire s.mu.RLock() on a non-reentrant mutex and
	// deadlock whenever a concurrent writer (RunMPCOptimize) is waiting for
	// s.mu.Lock().
	s.mu.RLock()
	config := s.config
	decisions := s.mpcDecisions
	lastExecuted := s.lastExecutedDecision
	s.mu.RUnlock()

	// Check if Plant Modbus Address is configured and there are decisions
	if config.PlantModbusAddress == "" || len(decisions) == 0 {
		return nil
	}

	now := time.Now().Unix()
	var currentDecision *mpc.ControlDecision

	// Find the decision that matches the current hour
	for i := range decisions {
		decision := &decisions[i]
		// Check if current time falls within this decision's hour
		// Each decision covers a check price interval window starting from its timestamp
		if now >= decision.Timestamp && now < decision.Timestamp+int64(config.CheckPriceInterval.Seconds()) {
			currentDecision = decision
			break
		}
	}

	if currentDecision == nil {
		// No matching decision found for current timestamp
		s.logger.Printf("No matching decision found for the current timestamp")
		return nil
	}

	// Check if this decision has already been executed
	if lastExecuted != nil && currentDecision.Timestamp == lastExecuted.Timestamp {
		// Decision already executed, no need to retry
		return nil
	}

	s.logger.Printf("Executing MPC decision for timestamp %d (hour %d)", currentDecision.Timestamp, currentDecision.Hour)

	// Execute the current decision
	err := s.executeMPCDecision(ctx, currentDecision, config.DryRun)

	s.mu.Lock()
	if err != nil {
		// Execution failed, set lastExecutedDecision to nil
		s.lastExecutedDecision = nil
		s.mu.Unlock()
		s.logger.Printf("Error executing MPC decision: %v (will retry again in 1 minute)", err)
		return err
	}

	// Execution succeeded, store the executed decision
	s.lastExecutedDecision = currentDecision
	s.mu.Unlock()

	s.logger.Printf("Successfully executed MPC decision")
	return nil
}
