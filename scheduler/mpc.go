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
func (s *MinerScheduler) RunMPCOptimize(ctx context.Context) {
	s.logger.Printf("Starting MPC optimization task at %s", time.Now().Format(time.RFC3339))

	config := s.GetConfig()

	// Check if Plant Modbus Address is configured
	if config.PlantModbusAddress == "" {
		s.logger.Printf("MPC optimization skipped: PlantModbusAddress not configured")
		return
	}

	// Step 1: Read plant running info from inverter
	plantInfo, err := s.readPlantRunningInfo(config)
	if err != nil {
		s.logger.Printf("Error reading plant running info from inverter: %v", err)
		return
	}

	// Extract initial SOC from plant info
	initialSOC := plantInfo.ESSSOC / 100.0 // Convert from percentage (0-100) to fraction (0-1)
	s.logger.Printf("Initial battery SOC: %.1f%%", plantInfo.ESSSOC)

	// Step 2: Get forecast data (prices, solar, load)
	forecast, err := s.buildMPCForecast(ctx, config, plantInfo)
	if err != nil {
		s.logger.Printf("Error building MPC forecast: %v", err)
		return
	}

	if len(forecast) == 0 {
		s.logger.Printf("No forecast data available for MPC optimization")
		return
	}

	s.logger.Printf("Built forecast with %d time slots", len(forecast))

	// Step 3: Create MPC controller
	systemConfig := mpc.SystemConfig{
		BatteryCapacity:        config.BatteryCapacity,
		BatteryMaxCharge:       config.BatteryMaxCharge,
		BatteryMaxDischarge:    config.BatteryMaxDischarge,
		BatteryMinSOC:          config.BatteryMinSOC,
		BatteryMaxSOC:          config.BatteryMaxSOC,
		BatteryEfficiency:      config.BatteryEfficiency,
		BatteryDegradationCost: config.BatteryDegradationCost,
		MaxGridImport:          config.MaxGridImport,
		MaxGridExport:          config.MaxGridExport,
	}

	horizon := len(forecast)
	controller := mpc.NewMPCController(systemConfig, horizon, initialSOC)

	// Step 4: Run optimization
	decisions := controller.Optimize(forecast)
	if len(decisions) == 0 {
		s.logger.Printf("MPC optimization produced no decisions")
		return
	}

	// Step 5: Save optimization results
	s.mu.Lock()
	s.mpcDecisions = decisions
	s.lastMPCExecutionError = nil // Clear any previous execution error
	s.mu.Unlock()

	// Log summary
	s.logger.Printf("MPC optimization completed with %d decisions", len(decisions))
	totalProfit := 0.0
	for _, dec := range decisions {
		totalProfit += dec.Profit
	}
	s.logger.Printf("Total expected profit over %d hours: %.2f EUR", len(decisions), totalProfit)

	// Step 6: Execute the first control decision
	err = s.executeMPCDecision(&decisions[0], config.DryRun)

	// Record execution status
	s.mu.Lock()
	s.lastMPCExecutionError = err
	s.mu.Unlock()

	if err != nil {
		s.logger.Printf("Error executing MPC decision: %v (will retry every minute)", err)
		return
	}

	s.logger.Printf("MPC optimization task completed successfully")
}

// readPlantRunningInfo reads the plant running information from the inverter
func (s *MinerScheduler) readPlantRunningInfo(config *Config) (*sigenergy.PlantRunningInfo, error) {
	// Connect to Plant Modbus server
	client, err := sigenergy.NewTCPClient(config.PlantModbusAddress, sigenergy.PlantAddress)
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
func (s *MinerScheduler) buildMPCForecast(ctx context.Context, config *Config, plantInfo *sigenergy.PlantRunningInfo) ([]mpc.TimeSlot, error) {
	now := time.Now()

	// Get electricity price forecast
	priceForecasts, err := s.getPriceForecast(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("failed to get price forecast: %w", err)
	}

	// Get weather forecast for weather data
	weatherForecast, err := s.getOrFetchWeatherForecast(config)
	if err != nil {
		s.logger.Printf("Warning: failed to get weather forecast: %v", err)
		weatherForecast = nil
	}

	// Get solar forecast with weather data
	solarForecasts, weatherData, err := s.getSolarForecast(config, now, weatherForecast, plantInfo)
	if err != nil {
		s.logger.Printf("Warning: failed to get solar forecast: %v, using zero solar", err)
		// Continue with zero solar forecast
		solarForecasts = make(map[int]float64)
		weatherData = make(map[int]WeatherData)
	}

	// Build time slots
	var timeSlots []mpc.TimeSlot
	for hour, prices := range priceForecasts {
		solar := solarForecasts[hour]
		weather := weatherData[hour]

		// Estimate load forecast (miners only, based on price and solar availability)
		loadForecast := s.estimateLoadForecast(prices.Import, config.PriceLimit/1000, solar, config)

		futureTime := now.Add(time.Duration(hour) * time.Hour)
		timeSlots = append(timeSlots, mpc.TimeSlot{
			Hour:          hour,
			Timestamp:     futureTime.Unix(),
			ImportPrice:   prices.Import / 1000.0, // Convert EUR/MWh to EUR/kWh
			ExportPrice:   prices.Export / 1000.0, // Convert EUR/MWh to EUR/kWh
			SolarForecast: solar,
			LoadForecast:  loadForecast,
			CloudCoverage: weather.CloudCoverage,
			WeatherSymbol: weather.WeatherSymbol,
		})
	}

	// Sort by hour
	for i := 0; i < len(timeSlots); i++ {
		for j := i + 1; j < len(timeSlots); j++ {
			if timeSlots[i].Hour > timeSlots[j].Hour {
				timeSlots[i], timeSlots[j] = timeSlots[j], timeSlots[i]
			}
		}
	}

	return timeSlots, nil
}

// WeatherData represents weather information for a specific hour
type WeatherData struct {
	CloudCoverage float64 // % cloud coverage (0-100)
	WeatherSymbol string  // weather condition symbol
}

// PricePoint represents import and export prices for a specific hour
type PricePoint struct {
	Import float64 // EUR/MWh
	Export float64 // EUR/MWh
}

// getPriceForecast gets electricity prices for the next 24 hours
func (s *MinerScheduler) getPriceForecast(ctx context.Context, now time.Time) (map[int]PricePoint, error) {

	// Get the market data
	marketData, err := s.GetMarketData(ctx)
	if err != nil {
		return nil, err
	}
	if marketData == nil {
		return nil, fmt.Errorf("no price document available")
	}

	// Get configuration for price adjustments
	config := s.GetConfig()

	// Extract prices for next 24 hours
	forecast := make(map[int]PricePoint)
	for i := range 24 {
		futureTime := now.Add(time.Duration(i) * time.Hour)
		price, found := marketData.LookupAveragePriceInHourByTime(futureTime)

		if found {
			// Apply price adjustments from configuration (all values in EUR/MWh)
			// Import price: add operator fee and delivery fee
			// Export price: subtract operator fee
			forecast[i] = PricePoint{
				Import: price + config.ImportPriceOperatorFee + config.ImportPriceDeliveryFee,
				Export: price - config.ExportPriceOperatorFee,
			}
		}
	}

	return forecast, nil
}

// getSolarForecast gets solar power forecast from weather data
func (s *MinerScheduler) getSolarForecast(config *Config, now time.Time, weatherForecast *meteo.METJSONForecast, plantInfo *sigenergy.PlantRunningInfo) (map[int]float64, map[int]WeatherData, error) {
	if weatherForecast == nil || weatherForecast.Properties == nil {
		return nil, nil, fmt.Errorf("invalid weather forecast data")
	}

	// Get current PV power to detect if panels are already covered by snow
	currentPVPower := 0.0
	if plantInfo != nil {
		currentPVPower = plantInfo.PhotovoltaicPower
	}

	// Convert weather to solar forecast
	solarForecast := make(map[int]float64)
	weatherData := make(map[int]WeatherData)

	for i := range 24 {
		futureTime := now.Add(time.Duration(i) * time.Hour)
		solarPower, cloudCoverage, weatherSymbol := s.estimateSolarPowerFromWeather(weatherForecast, futureTime, config.MaxSolarPower, currentPVPower)
		solarForecast[i] = solarPower
		weatherData[i] = WeatherData{
			CloudCoverage: cloudCoverage,
			WeatherSymbol: weatherSymbol,
		}
	}

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
func (s *MinerScheduler) estimateSolarPowerFromWeather(forecast *meteo.METJSONForecast, targetTime time.Time, peakPower float64, currentPVPower float64) (float64, float64, string) {
	cloudCoverage := 0.0
	weatherSymbol := ""

	if forecast.Properties == nil || len(forecast.Properties.Timeseries) == 0 {
		return 0, cloudCoverage, weatherSymbol
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
		return 0, cloudCoverage, weatherSymbol
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
		return 0, cloudCoverage, weatherSymbol // No sun available
	}

	// Get solar position to calculate altitude angle
	pos := suncalc.GetPosition(targetTime, lat, lon)
	altitude := pos.Altitude // in radians

	// Solar altitude factor (0-1)
	// Altitude ranges from 0 (horizon) to π/2 (zenith)
	// Use sine of altitude as a factor (0 at horizon, 1 at zenith)
	solarAngleFactor := math.Sin(altitude)
	if solarAngleFactor < 0 {
		return 0, cloudCoverage, weatherSymbol
	}

	// Check for snow conditions - PV panels covered by snow produce zero power
	if symbol := closestStep.GetSymbolCode(); symbol != nil {
		if symbol.HasSnow() {
			s.logger.Printf("Snow detected in weather forecast at %s, setting solar power to zero", targetTime.Format(time.RFC3339))
			return 0, cloudCoverage, weatherSymbol
		}
	}

	// Check if panels are already covered by snow:
	// If current PV power is zero but we expect power based on sun angle, panels might be covered
	expectedPower := peakPower * solarAngleFactor * 0.5 // Rough estimate with some clouds
	if currentPVPower < 0.1 && expectedPower > 1.0 && time.Until(targetTime).Hours() < 1 {
		// Current power is essentially zero but we expect power - likely snow covered
		s.logger.Printf("Current PV power is zero (%.2f kW) but forecast expects %.2f kW - panels may be snow covered", currentPVPower, expectedPower)
		return 0, cloudCoverage, weatherSymbol
	}

	// Cloud factor (0-1, where 1 = no clouds)
	cloudFactor := 1.0
	if details.CloudAreaFraction != nil {
		cloudFraction := *details.CloudAreaFraction / 100.0
		cloudFactor = 1.0 - (cloudFraction * 0.90) // Clouds reduce output by up to 90%
	}

	// Estimate solar power
	solarPower := peakPower * solarAngleFactor * cloudFactor

	return solarPower, cloudCoverage, weatherSymbol
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
		// Without power control, all miners can run in Eco mode
		totalMinerPower := float64(len(minersList)) * config.MinerPowerEco
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
func (s *MinerScheduler) executeMPCDecision(decision *mpc.ControlDecision, dryRun bool) error {
	if dryRun {
		s.logger.Printf("DRY-RUN: Would execute MPC decision - Charge: %.1f kW, Discharge: %.1f kW, Import: %.1f kW, Export: %.1f kW",
			decision.BatteryCharge, decision.BatteryDischarge, decision.GridImport, decision.GridExport)
		return nil
	}

	config := s.GetConfig()

	// Connect to Plant Modbus server
	client, err := sigenergy.NewTCPClient(config.PlantModbusAddress, sigenergy.PlantAddress)
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

	if decision.BatteryCharge > 0.01 {
		// Battery should charge
		// Mode 4: Command charging (PV first) - charge from PV, then grid if needed
		mode = 4
		chargeLimit := decision.BatteryCharge
		s.logger.Printf("Setting battery to CHARGE mode: %.1f kW", chargeLimit)

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
		// Mode 6: Command discharging (ESS first) - discharge from battery first
		mode = 6
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

	s.logger.Printf("Successfully executed MPC decision - Mode: %d, SOC: %.1f%%, GridImport: %.1f kW, GridExport: %.1f kW",
		mode, decision.BatterySOC*100, decision.GridImport, decision.GridExport)

	return nil
}

// runMPCExecution re-executes the current MPC decision only if previous execution failed
// This ensures the decision is applied even if previous execution failed
func (s *MinerScheduler) runMPCExecution() {

	// Check if last execution failed
	s.mu.RLock()
	lastError := s.lastMPCExecutionError
	if lastError == nil {
		// Last execution succeeded, no need to retry
		s.mu.RUnlock()
		return
	}

	config := s.GetConfig()

	// Check if Plant Modbus Address is configured
	if config.PlantModbusAddress == "" {
		s.mu.RUnlock()
		return
	}

	// Get current MPC decision
	if len(s.mpcDecisions) == 0 {
		s.mu.RUnlock()
		return
	}
	currentDecision := s.mpcDecisions[0]
	s.mu.RUnlock()

	s.logger.Printf("Retrying MPC decision execution (previous error: %v)", lastError)

	// Execute the current decision
	err := s.executeMPCDecision(&currentDecision, config.DryRun)

	s.mu.Lock()
	s.lastMPCExecutionError = err
	s.mu.Unlock()

	if err != nil {
		s.logger.Printf("Error retrying MPC decision: %v (will retry again in 1 minute)", err)
		return
	}

	s.logger.Printf("Successfully retried MPC decision execution")
}
