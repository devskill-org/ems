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
		BatteryCapacity:                  config.BatteryCapacity,
		BatteryMaxCharge:                 config.BatteryMaxCharge,
		BatteryMaxDischarge:              config.BatteryMaxDischarge,
		BatteryMinSOC:                    config.BatteryMinSOC,
		BatteryMaxSOC:                    config.BatteryMaxSOC,
		BatteryEfficiency:                config.BatteryEfficiency,
		BatteryDegradationCost:           config.BatteryDegradationCost,
		MaxGridImport:                    config.MaxGridImport,
		MaxGridExport:                    config.MaxGridExport,
		BatteryPreHeatPower:              config.BatteryPreHeatPower,
		BatteryPreHeatTempThreshold:      config.BatteryPreHeatTempThreshold,
		BatteryThermalTimeConstant:       config.BatteryThermalTimeConstant,
		TimeSlotDuration:                 timeSlotDuration,
		BatteryBalancingSOCThreshold:     config.BatteryBalancingSOCThreshold,
		BatteryBalancingEfficiencyFactor: config.BatteryBalancingEfficiencyFactor,
		BatteryBalancingBonus:            config.BatteryBalancingBonus,
	}

	horizon := len(forecast)
	controller := mpc.NewController(systemConfig, horizon, initialSOC)
	controller.CurrentBatteryTemp = plantInfo.ESSAvgCellTemperature

	// Persist lastBalancingTime across optimization runs.
	// NewController sets LastBalancingTime when initialSOC >= BatteryMaxSOC;
	// otherwise fall back to the value remembered from a previous run.
	s.mu.Lock()
	if controller.LastBalancingTime > 0 {
		s.lastBalancingTime = controller.LastBalancingTime
		s.logger.Printf("Battery at 100%% SOC, updating lastBalancingTime")
	} else {
		controller.LastBalancingTime = s.lastBalancingTime
	}
	s.mu.Unlock()

	// Step 4: Run optimization
	decisions := controller.Optimize(forecast)
	if len(decisions) == 0 {
		s.logger.Printf("MPC optimization produced no decisions")
		return nil
	}

	// Step 5: Save optimization results to memory
	s.mu.Lock()
	s.mpcDecisions = decisions
	s.lastExecutedDecision = nil      // Clear last executed decision for new optimization
	s.lastWrittenMode = 0             // Force re-write of inverter registers after new plan
	s.lastWrittenGridExportLimit = -1 // Force re-write of grid export limit after new plan
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
	plantInfo, err := client.ReadPlantRunningInfo(byte(config.DCChargerSlaveID)) //nolint:gosec // SlaveID is expected to be in [0,255] range
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
	// getSolarForecast tolerates a nil weatherForecast: Open-Meteo irradiance data is fetched
	// independently. Weather metadata (cloud coverage, symbol, temperature) will be zero/empty
	// when MET Norway is unavailable, but solar power estimates are still produced from Open-Meteo.
	solarForecasts, weatherData, err = s.getSolarForecast(config, now, slotDuration, weatherForecast, plantInfo)
	if err != nil {
		s.logger.Printf("Warning: failed to get solar forecast: %v, using zero solar", err)
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
// Weather metadata (cloud coverage, symbol, temperature) is sourced from the MET Norway forecast when
// available; if weatherForecast is nil (e.g. MET Norway is unreachable) the metadata fields default to
// zero/empty but the Open-Meteo irradiance data is still used for the solar power estimate.
// Falls back to sun-angle + cloud-based estimation if the Open-Meteo forecast is also unavailable.
func (s *MinerScheduler) getSolarForecast(config *Config, now time.Time, slotDuration time.Duration, weatherForecast *meteo.METJSONForecast, plantInfo *sigenergy.PlantRunningInfo) (map[int]float64, map[int]WeatherData, error) {
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

	// Override slot 0 with a short rolling average of recent PV readings.
	// A single instantaneous sample can be near zero if a cloud passes at the
	// exact moment the MPC optimization runs, causing the optimizer to pull from
	// the grid for the entire slot even though the day is otherwise sunny.
	// Averaging the last 5 minutes of samples (≈60 readings at the default 5 s
	// poll interval) smooths out these transient dips while still tracking genuine
	// sustained low-irradiance conditions.  Falls back to the latest single reading
	// when the buffer has no samples within that window (e.g. on startup).
	const pvSmoothingWindow = 5 * time.Minute
	solarForecast[0] = s.dataSamples.AveragePVPowerLast(pvSmoothingWindow)

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
	if s.openMeteoBaseURL != "" {
		client.SetBaseURL(s.openMeteoBaseURL)
	}

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
	if forecast == nil || forecast.Properties == nil || len(forecast.Properties.Timeseries) == 0 {
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

	if forecast == nil || forecast.Properties == nil || len(forecast.Properties.Timeseries) == 0 {
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

// maxWorkModePower returns the per-miner power consumption (kW) for the
// configured maximum work mode (MinerMaxWorkMode: 0=Eco, 1=Standard, 2=Super).
func maxWorkModePower(config *Config) float64 {
	switch config.MinerMaxWorkMode {
	case 0:
		return config.MinerPowerEco
	case 1:
		return config.MinerPowerStandard
	default: // 2 = Super
		return config.MinerPowerSuper
	}
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
		// the configured MinersPowerLimit.  Cap at the maximum allowed work mode.
		totalMinerPower := float64(numMiners) * maxWorkModePower(config)
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

	// Try each work mode from MinerMaxWorkMode down to Eco.  Use the first
	// (highest) mode where at least one miner can run within the effective limit.
	type modeEntry struct {
		power float64
	}
	allModePowers := []float64{config.MinerPowerEco, config.MinerPowerStandard, config.MinerPowerSuper}
	modes := make([]modeEntry, 0, config.MinerMaxWorkMode+1)
	for i := config.MinerMaxWorkMode; i >= 0; i-- {
		modes = append(modes, modeEntry{allModePowers[i]})
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

// batteryAction holds the Modbus control parameters derived from an MPC decision.
type batteryAction struct {
	mode           uint16
	chargeLimit    float64
	dischargeLimit float64
	setCharge      bool
	setDischarge   bool
	logMsg         string
}

// decideBatteryAction translates an MPC control decision into a concrete battery
// action without any side-effects, making the mapping easy to read and test.
//
// recentAvgPV is the short-term average PV output (kW) measured just before
// execution.  When it is high enough to cover the load plus the full planned
// charge rate, grid charging is suppressed and the inverter is switched to
// PV-only self-use mode instead.  Pass 0 to disable the gate (e.g. in tests
// that are not exercising the cloud-recovery logic).
func decideBatteryAction(decision *mpc.ControlDecision, maxCharge float64, recentAvgPV float64) batteryAction {
	switch {
	case decision.BatteryChargeFromGrid > 0.01:
		totalPlannedCharge := decision.BatteryChargeFromPV + decision.BatteryChargeFromGrid

		// PV recovery gate: the MPC may have planned grid charging because it
		// observed low solar at optimisation time (a cloud was passing).  If
		// actual PV is now high enough to cover both the load and the full
		// planned charge, the cloud has cleared and the grid import is no longer
		// needed.  Switch to Mode 2 (PV self-use) so the battery still charges
		// at the planned rate but entirely from solar.
		if recentAvgPV >= decision.LoadForecast+totalPlannedCharge {
			limit := math.Min(totalPlannedCharge, maxCharge)
			return batteryAction{
				mode:        2,
				chargeLimit: limit,
				setCharge:   true,
				logMsg: fmt.Sprintf(
					"Grid charging suppressed (PV recovery): recent PV %.1f kW >= load %.1f kW + planned charge %.1f kW; switching to PV-only mode, limit %.1f kW",
					recentAvgPV, decision.LoadForecast, totalPlannedCharge, limit),
			}
		}

		// Mode 4: Command charging (PV first, then grid).
		// The charge limit must be the total desired charge rate so that the
		// inverter can draw from both PV surplus and the grid to reach it.
		// Clamp to maxCharge: the inverter rejects any value above the
		// hardware-rated maximum with a Modbus illegal-data-address exception.
		limit := math.Min(totalPlannedCharge, maxCharge)
		return batteryAction{
			mode:        4,
			chargeLimit: limit,
			setCharge:   true,
			logMsg: fmt.Sprintf("Setting battery to CHARGE mode (PV + Grid): ChargeFromPV: %.1f kW, ChargeFromGrid: %.1f kW, TotalLimit: %.1f kW",
				decision.BatteryChargeFromPV, decision.BatteryChargeFromGrid, limit),
		}

	case decision.BatteryChargeFromPV > 0.01:
		// Mode 2: Self-use mode — charge from PV surplus only.
		// When the export price is non-positive raise the limit to maxCharge so
		// the inverter absorbs PV surplus instead of spilling it to the grid.
		limit := decision.BatteryChargeFromPV
		msg := fmt.Sprintf("Setting battery to CHARGE mode (PV only): ChargeFromPV: %.1f kW", limit)
		if decision.ExportPrice <= 0 {
			limit = maxCharge
			msg = fmt.Sprintf("Setting battery to CHARGE mode (PV only, export price non-positive — limit raised to max): ChargeFromPV: %.1f kW -> MaxCharge: %.1f kW",
				decision.BatteryChargeFromPV, maxCharge)
		}
		return batteryAction{mode: 2, chargeLimit: limit, setCharge: true, logMsg: msg}

	case decision.BatteryDischarge > 0.01:
		// When the export is not planned, avoid Mode 5 (forced discharge)
		// because forecast overestimation could cause excess grid export at a
		// financial loss.  Mode 2 (Maximum self-consumption) discharges the
		// battery reactively to cover actual load only, preventing unwanted export.
		if decision.GridExport < 0.01 && decision.LoadForecast <= decision.BatteryDischarge {
			return batteryAction{
				mode:           2,
				setDischarge:   true,
				dischargeLimit: decision.BatteryDischarge,
				logMsg:         fmt.Sprintf("Setting battery to DISCHARGE mode (self-consumption, export price non-positive — using Mode 2 to prevent grid export): %.1f kW", decision.BatteryDischarge),
			}
		}
		// Mode 5: Command discharging (PV first).
		return batteryAction{
			mode:           5,
			dischargeLimit: decision.BatteryDischarge,
			setDischarge:   true,
			logMsg:         fmt.Sprintf("Setting battery to DISCHARGE mode: %.1f kW", decision.BatteryDischarge),
		}

	default:
		// Mode 4: Idle — zero charge and discharge limits keep the battery passive.
		return batteryAction{
			mode:         4,
			setCharge:    true,
			setDischarge: true,
			logMsg: fmt.Sprintf("Setting battery to IDLE mode (minimal limits): GridImport: %.1f kW, GridExport: %.1f kW",
				decision.GridImport, decision.GridExport),
		}
	}
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

	// Check whether an EV session is currently active.  All session state
	// management (register reads, evSessionActive updates, debounce, and Mode 2
	// writes) is owned exclusively by runEVControl, which runs at PVPollInterval.
	// This function only reads the flag — it never writes it.
	s.mu.RLock()
	evActive := s.evSessionActive
	s.mu.RUnlock()

	if evActive {
		s.logger.Printf("EV session active - MPC battery control deferred to runEVControl")
		return nil
	}

	// Get recent average PV power for the grid-charging gate.  A 5-minute
	// window covers ~60 readings at the default 5 s poll interval and is
	// long enough to distinguish a sustained cloud-free period from a brief
	// spike.  Falls back to 0 (gate disabled) when no samples are available.
	const pvGateWindow = 5 * time.Minute
	recentAvgPV := s.dataSamples.AveragePVPowerLast(pvGateWindow)

	action := decideBatteryAction(decision, config.BatteryMaxCharge, recentAvgPV)
	s.logger.Print(action.logMsg)

	// Enforce hardware-level grid export limit based on the current export price.
	// When the export price is negative (or zero), set the inverter's grid-point
	// export limit to 0 so no power can reach the grid regardless of ESS mode or
	// PV surplus.  The limit is restored to MaxGridExport when prices turn positive.
	if err := s.applyGridExportLimit(client, decision.ExportPrice, config.MaxGridExport); err != nil {
		return err
	}

	if err := s.applyInverterMode(client, action.mode, action.chargeLimit, action.dischargeLimit, action.setCharge, action.setDischarge); err != nil {
		return err
	}

	s.logger.Printf("Successfully executed MPC decision - Mode: %d, SOC: %.1f%%, ChargeFromPV: %.1f kW, ChargeFromGrid: %.1f kW, Discharge: %.1f kW, GridImport: %.1f kW, GridExport: %.1f kW",
		action.mode, decision.BatterySOC*100, decision.BatteryChargeFromPV, decision.BatteryChargeFromGrid, decision.BatteryDischarge, decision.GridImport, decision.GridExport)

	return nil
}

// applyGridExportLimit enforces the grid-point export limit on the inverter.
// When the export price is negative or zero the limit is set to 0 (no export
// allowed).  When the price is positive the limit is restored to maxGridExport.
// Writes are suppressed when the value has not changed since the last write.
func (s *MinerScheduler) applyGridExportLimit(
	client interface {
		SetGridPointMaxExportLimit(float64) error
	},
	exportPrice float64,
	maxGridExport float64,
) error {
	var targetLimit float64
	if exportPrice <= 0 {
		targetLimit = 0
	} else {
		targetLimit = maxGridExport
	}

	s.mu.Lock()
	last := s.lastWrittenGridExportLimit
	s.mu.Unlock()

	if last == targetLimit {
		return nil
	}

	if err := client.SetGridPointMaxExportLimit(targetLimit); err != nil {
		return fmt.Errorf("failed to set grid point export limit: %w", err)
	}

	s.mu.Lock()
	s.lastWrittenGridExportLimit = targetLimit
	s.mu.Unlock()

	if exportPrice <= 0 {
		s.logger.Printf("Grid export disabled: export price %.4f $/kWh is non-positive (grid point export limit set to 0 kW)", exportPrice)
	} else {
		s.logger.Printf("Grid export restored: export price %.4f $/kWh is positive (grid point export limit set to %.1f kW)", exportPrice, targetLimit)
	}
	return nil
}

// applyInverterMode writes Remote EMS enable + mode + charge/discharge limits to
// the inverter via Modbus.  Writes are suppressed when the values are identical
// to what was last successfully written, reducing register churn that can disturb
// the DC charging protocol during an active EV session.
func (s *MinerScheduler) applyInverterMode(
	client interface {
		EnableRemoteEMS(bool) error
		SetRemoteEMSMode(uint16) error
		SetESSMaxChargingLimit(float64) error
		SetESSMaxDischargingLimit(float64) error
	},
	mode uint16, chargeLimit, dischargeLimit float64,
	setCharge, setDischarge bool,
) error {
	s.mu.Lock()
	lastMode := s.lastWrittenMode
	lastCharge := s.lastWrittenChargeLimit
	lastDischarge := s.lastWrittenDischargeLimit
	s.mu.Unlock()

	// Always enable Remote EMS on the first write (lastMode == 0) or when
	// switching modes, but skip the Modbus call when nothing changed.
	if lastMode == 0 || lastMode != mode || (setCharge && lastCharge != chargeLimit) || (setDischarge && lastDischarge != dischargeLimit) {
		if err := client.EnableRemoteEMS(true); err != nil {
			return fmt.Errorf("failed to enable remote EMS: %w", err)
		}
		if err := client.SetRemoteEMSMode(mode); err != nil {
			return fmt.Errorf("failed to set remote EMS mode: %w", err)
		}
		if setCharge {
			if err := client.SetESSMaxChargingLimit(chargeLimit); err != nil {
				return fmt.Errorf("failed to set ESS charging limit: %w", err)
			}
		}
		if setDischarge {
			if err := client.SetESSMaxDischargingLimit(dischargeLimit); err != nil {
				return fmt.Errorf("failed to set ESS discharging limit: %w", err)
			}
		}
		s.mu.Lock()
		s.lastWrittenMode = mode
		if setCharge {
			s.lastWrittenChargeLimit = chargeLimit
		}
		if setDischarge {
			s.lastWrittenDischargeLimit = dischargeLimit
		}
		s.mu.Unlock()
		s.logger.Printf("Inverter mode applied: mode=%d chargeLimit=%.1f kW dischargeLimit=%.1f kW", mode, chargeLimit, dischargeLimit)
	}
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
