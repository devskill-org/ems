package scheduler

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/devskill-org/ems/meteo"
	"github.com/devskill-org/ems/mpc"
	"github.com/devskill-org/ems/openmeteo"
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

// getSolarForecast gets solar power forecast using Open-Meteo irradiance data at slotDuration resolution.
// Weather metadata (cloud coverage, symbol, temperature) is still sourced from the MET Norway forecast.
// Falls back to weather-based estimation if the Open-Meteo forecast is unavailable.
func (s *MinerScheduler) getSolarForecast(config *Config, now time.Time, slotDuration time.Duration, weatherForecast *meteo.METJSONForecast, plantInfo *sigenergy.PlantRunningInfo) (map[int]float64, map[int]WeatherData, error) {
	if weatherForecast == nil || weatherForecast.Properties == nil {
		return nil, nil, fmt.Errorf("invalid weather forecast data")
	}

	// Get current PV power to detect if panels are already covered by snow
	currentPVPower := 0.0
	if plantInfo != nil {
		currentPVPower = plantInfo.PhotovoltaicPower
	}

	forecastDuration := 36 * time.Hour
	numSlots := int(forecastDuration / slotDuration)

	solarForecast := make(map[int]float64)
	weatherData := make(map[int]WeatherData)

	// Try to get solar irradiance data from Open-Meteo
	var solarDataPoints []openmeteo.SolarDataPoint
	solarIrradiance, err := s.getOrFetchSolarForecast(config)
	if err != nil {
		s.logger.Printf("Warning: failed to fetch Open-Meteo solar forecast: %v, falling back to weather-based estimation", err)
	} else {
		solarDataPoints, err = solarIrradiance.DataPoints()
		if err != nil {
			s.logger.Printf("Warning: failed to parse Open-Meteo data points: %v, falling back to weather-based estimation", err)
			solarDataPoints = nil
		} else {
			s.logger.Printf("Using Open-Meteo solar irradiance forecast with %d data points", len(solarDataPoints))
		}
	}

	for i := range numSlots {
		futureTime := now.Add(time.Duration(i) * slotDuration)

		// Get weather metadata from MET Norway forecast
		cloudCoverage, weatherSymbol, airTemp := s.getWeatherDataAtTime(weatherForecast, futureTime)
		weatherData[i] = WeatherData{
			CloudCoverage:  cloudCoverage,
			WeatherSymbol:  weatherSymbol,
			AirTemperature: airTemp,
		}

		// Estimate solar power from irradiance or fall back to weather-based estimation
		if solarDataPoints != nil {
			solarForecast[i] = irradianceAtTime(solarDataPoints, futureTime, config.MaxSolarPower)
		} else {
			solarPower, _, _, _ := s.estimateSolarPowerFromWeather(weatherForecast, futureTime, config.MaxSolarPower, currentPVPower)
			solarForecast[i] = solarPower
		}
	}

	// Override slot 0 with actual current PV power reading
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

// getOrFetchSolarForecast gets solar irradiance forecast from cache or fetches a new one from Open-Meteo.
func (s *MinerScheduler) getOrFetchSolarForecast(config *Config) (*openmeteo.SolarForecast, error) {
	// Try cache first
	if forecast, ok := s.solarForecastCache.Get(); ok {
		return forecast, nil
	}

	// Fetch new forecast from Open-Meteo
	client := openmeteo.NewClient()

	forecast, err := client.GetSolarForecast(openmeteo.QueryParams{
		Location: openmeteo.Location{
			Latitude:  config.Latitude,
			Longitude: config.Longitude,
		},
		ForecastDays: 2,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch solar irradiance forecast: %w", err)
	}

	// Cache it
	s.solarForecastCache.Set(forecast)

	return forecast, nil
}

// getWeatherDataAtTime extracts cloud coverage, weather symbol, and air temperature
// from the MET Norway forecast for a given time.
func (s *MinerScheduler) getWeatherDataAtTime(forecast *meteo.METJSONForecast, targetTime time.Time) (cloudCoverage float64, weatherSymbol string, airTemperature float64) {
	if forecast.Properties == nil || len(forecast.Properties.Timeseries) == 0 {
		return 0, "", 0
	}

	var closestStep *meteo.ForecastTimeStep
	minDiff := time.Duration(math.MaxInt64)

	for _, step := range forecast.Properties.Timeseries {
		diff := step.Time.Sub(targetTime)
		if diff < 0 {
			diff = -diff
		}
		if diff < minDiff {
			minDiff = diff
			closestStep = &step
		}
	}

	if closestStep == nil || closestStep.Data == nil || closestStep.Data.Instant == nil || closestStep.Data.Instant.Details == nil {
		return 0, "", 0
	}

	details := closestStep.Data.Instant.Details

	if details.CloudAreaFraction != nil {
		cloudCoverage = *details.CloudAreaFraction
	}
	if symbol := closestStep.GetSymbolCode(); symbol != nil {
		weatherSymbol = string(*symbol)
	}
	if details.AirTemperature != nil {
		airTemperature = *details.AirTemperature
	}

	return cloudCoverage, weatherSymbol, airTemperature
}

// irradianceAtTime finds the closest solar irradiance data point to the target time
// and converts the shortwave radiation (W/m²) to estimated solar power (kW).
// Under STC (Standard Test Conditions), solar panels are rated at 1000 W/m², so:
//
//	power_kw = peakPower * (shortwave_radiation / 1000.0)
func irradianceAtTime(dataPoints []openmeteo.SolarDataPoint, targetTime time.Time, peakPower float64) float64 {
	if len(dataPoints) == 0 {
		return 0
	}

	var closest *openmeteo.SolarDataPoint
	minDiff := time.Duration(math.MaxInt64)

	for i := range dataPoints {
		diff := dataPoints[i].Time.Sub(targetTime)
		if diff < 0 {
			diff = -diff
		}
		if diff < minDiff {
			minDiff = diff
			closest = &dataPoints[i]
		}
	}

	if closest == nil {
		return 0
	}

	// Convert GHI (Global Horizontal Irradiance) to solar power.
	// STC reference irradiance is 1000 W/m².
	power := peakPower * (closest.ShortwaveRadiation / 1000.0)
	if power < 0 {
		power = 0
	}
	if power > peakPower {
		power = peakPower
	}
	return power
}

// estimateSolarPowerFromWeather estimates solar power output from weather data
func (s *MinerScheduler) estimateSolarPowerFromWeather(forecast *meteo.METJSONForecast, targetTime time.Time, peakPower float64, currentPVPower float64) (float64, float64, string, float64) {
	cloudCoverage := 0.0
	weatherSymbol := ""
	airTemperature := 0.0

	if forecast.Properties == nil || len(forecast.Properties.Timeseries) == 0 {
		return 0, cloudCoverage, weatherSymbol, airTemperature
	}

	// Find closest time step
	var closestStep *meteo.ForecastTimeStep
	minDiff := time.Duration(math.MaxInt64)

	for _, step := range forecast.Properties.Timeseries {
		diff := step.Time.Sub(targetTime)
		if diff < 0 {
			diff = -diff
		}
		if diff < minDiff {
			minDiff = diff
			closestStep = &step
		}
	}

	if closestStep == nil || closestStep.Data == nil || closestStep.Data.Instant == nil || closestStep.Data.Instant.Details == nil {
		return 0, cloudCoverage, weatherSymbol, airTemperature
	}

	details := closestStep.Data.Instant.Details

	// Get cloud coverage
	if details.CloudAreaFraction != nil {
		cloudCoverage = *details.CloudAreaFraction
	}

	// Get weather symbol
	if symbol := closestStep.GetSymbolCode(); symbol != nil {
		weatherSymbol = string(*symbol)
	}

	// Get air temperature
	if details.AirTemperature != nil {
		airTemperature = *details.AirTemperature
	}

	// Get location from config
	config := s.GetConfig()
	lat := config.Latitude
	lon := config.Longitude

	// Get sun times for the target date
	sunTimes := suncalc.GetTimes(targetTime, lat, lon)
	sunrise := sunTimes["sunrise"].Value
	sunset := sunTimes["sunset"].Value

	// Check if we're between sunrise and sunset
	if targetTime.Before(sunrise) || targetTime.After(sunset) {
		return 0, cloudCoverage, weatherSymbol, airTemperature // No sun available
	}

	// Get solar position to calculate altitude angle
	pos := suncalc.GetPosition(targetTime, lat, lon)
	altitude := pos.Altitude // in radians

	// Solar altitude factor (0-1)
	// Altitude ranges from 0 (horizon) to π/2 (zenith)
	// Use sine of altitude as a factor (0 at horizon, 1 at zenith)
	solarAngleFactor := math.Sin(altitude)
	if solarAngleFactor < 0 {
		return 0, cloudCoverage, weatherSymbol, airTemperature
	}

	// Check for snow conditions - PV panels covered by snow produce zero power
	if symbol := closestStep.GetSymbolCode(); symbol != nil {
		if symbol.HasSnow() {
			s.logger.Printf("Snow detected in weather forecast at %s, setting solar power to zero", targetTime.Format(time.RFC3339))
			return 0, cloudCoverage, weatherSymbol, airTemperature
		}
	}

	// Check if panels are already covered by snow:
	// If current PV power is zero but we expect power based on sun angle, panels might be covered
	expectedPower := peakPower * solarAngleFactor * 0.5 // Rough estimate with some clouds
	if currentPVPower < 0.1 && expectedPower > 1.0 && time.Until(targetTime).Hours() < 1 {
		// Current power is essentially zero but we expect power - likely snow covered
		s.logger.Printf("Current PV power is zero (%.2f kW) but forecast expects %.2f kW - panels may be snow covered", currentPVPower, expectedPower)
		return 0, cloudCoverage, weatherSymbol, airTemperature
	}

	// Cloud factor (0-1, where 1 = no clouds)
	cloudFactor := 1.0
	if details.CloudAreaFraction != nil {
		cloudFraction := *details.CloudAreaFraction / 100.0
		cloudFactor = 1.0 - (cloudFraction * 0.90) // Clouds reduce output by up to 90%
	}

	// Estimate solar power
	solarPower := peakPower * solarAngleFactor * cloudFactor

	return solarPower, cloudCoverage, weatherSymbol, airTemperature
}

// estimateLoadForecast estimates power load based on price and available power
// Follows the same logic as manageMiners: miners wake up in Eco mode when price <= limit,
// but only if there's enough power budget (when PV power control is enabled)
// When miners are not running, they still consume standby power
// estimateLoadForecast estimates the total power consumption of all miners for a given
// time slot, taking into account the spot price, price limit, solar availability, and
// whether PV power control is enabled.
//
// When usePowerControl is enabled the function finds the highest work mode (Super →
// Standard → Eco) whose per-miner power consumption allows at least one miner to run
// within the effective power limit (min(solarForecast, MinersPowerLimit)).  All miners
// that fit at that mode are assumed to be running; the remainder are in standby.
func (s *MinerScheduler) estimateLoadForecast(spotPrice float64, priceLimit float64, solarForecast float64, config *Config) float64 {
	// Convert spotPrice from EUR/MWh to EUR/kWh for comparison with priceLimit
	spotPricePerKWh := spotPrice / 1000.0

	// Get discovered miners
	minersList := s.GetDiscoveredMiners()
	if len(minersList) == 0 {
		return 0.0
	}

	numMiners := len(minersList)

	// Miners are only ON if price is below or equal the limit
	// Otherwise they consume standby power
	if spotPricePerKWh > priceLimit {
		// All miners are in standby mode
		return float64(numMiners) * config.MinerPowerStandby
	}

	// Check if PV power control is enabled: active when price is at or above the configured threshold
	pvControlEnabled := spotPrice >= config.PVPowerControlPriceLimit
	if !pvControlEnabled {
		// Without PV power control, miners can run but total power must not exceed
		// the configured MinersPowerLimit.
		totalMinerPower := float64(numMiners) * config.MinerPowerSuper
		if totalMinerPower > config.MinersPowerLimit {
			totalMinerPower = config.MinersPowerLimit
		}
		return totalMinerPower
	}

	// With power control enabled, derive the effective limit from the solar forecast
	// capped at the configured miners power limit.
	effectiveLimit := config.MinersPowerLimit
	if solarForecast < effectiveLimit {
		effectiveLimit = solarForecast
	}

	// Try each work mode from highest to lowest.  Use the first (highest) mode where
	// at least one miner can run within the effective limit.
	type modeEntry struct {
		power float64
	}
	modes := []modeEntry{
		{config.MinerPowerSuper},
		{config.MinerPowerStandard},
		{config.MinerPowerEco},
	}

	for _, m := range modes {
		perMinerPower := m.power
		if perMinerPower <= 0 {
			continue
		}
		maxCanRun := int(effectiveLimit / perMinerPower)
		if maxCanRun <= 0 {
			// This mode consumes more than the available solar — try a lower mode
			continue
		}
		actualRunning := min(maxCanRun, numMiners)
		inStandby := numMiners - actualRunning
		return float64(actualRunning)*perMinerPower + float64(inStandby)*config.MinerPowerStandby
	}

	// No mode fits within the effective limit — all miners stay in standby
	return float64(numMiners) * config.MinerPowerStandby
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
		// Battery should charge.
		// Decide mode based on whether grid charging is also needed.
		if decision.BatteryChargeFromGrid > 0.01 {
			// Mode 4: Command charging (PV first, then grid).
			// The charge limit must be the total desired charge rate so that the
			// inverter can draw from both PV surplus and the grid to reach it.
			// Clamp to BatteryMaxCharge: the inverter rejects any value above the
			// hardware-rated maximum with a Modbus illegal-data-address exception.
			mode = 4
			chargeLimit := math.Min(
				decision.BatteryChargeFromPV+decision.BatteryChargeFromGrid,
				config.BatteryMaxCharge,
			)
			s.logger.Printf("Setting battery to CHARGE mode (PV + Grid): ChargeFromPV: %.1f kW, ChargeFromGrid: %.1f kW, TotalLimit: %.1f kW",
				decision.BatteryChargeFromPV, decision.BatteryChargeFromGrid, chargeLimit)

			// Set Remote EMS control mode
			if err := client.SetRemoteEMSMode(mode); err != nil {
				return fmt.Errorf("failed to set remote EMS mode: %w", err)
			}

			// Set ESS max charging limit to the combined PV + grid charge rate
			if err := client.SetESSMaxChargingLimit(chargeLimit); err != nil {
				return fmt.Errorf("failed to set ESS charging limit: %w", err)
			}
		} else {
			// Mode 2: Self-use mode — charge from PV surplus only.
			// The charge limit is normally the MPC-planned PV-sourced charge rate.
			// However, when the export price is non-positive (zero or negative),
			// exporting any surplus PV to the grid is actively costly. In that case
			// we raise the limit to BatteryMaxCharge so the inverter absorbs as much
			// real-time PV surplus as possible rather than spilling it to the grid.
			mode = 2
			chargeLimit := decision.BatteryChargeFromPV
			if decision.ExportPrice <= 0 {
				chargeLimit = config.BatteryMaxCharge
				s.logger.Printf("Setting battery to CHARGE mode (PV only, export price non-positive — limit raised to max): ChargeFromPV: %.1f kW -> MaxCharge: %.1f kW",
					decision.BatteryChargeFromPV, chargeLimit)
			} else {
				s.logger.Printf("Setting battery to CHARGE mode (PV only): ChargeFromPV: %.1f kW",
					decision.BatteryChargeFromPV)
			}

			// Set Remote EMS control mode
			if err := client.SetRemoteEMSMode(mode); err != nil {
				return fmt.Errorf("failed to set remote EMS mode: %w", err)
			}

			// Set ESS max charging limit to the PV-only charge rate
			if err := client.SetESSMaxChargingLimit(chargeLimit); err != nil {
				return fmt.Errorf("failed to set ESS charging limit: %w", err)
			}
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
