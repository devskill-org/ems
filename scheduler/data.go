package scheduler

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/devskill-org/ems/meteo"
	"github.com/devskill-org/ems/openmeteo"
	"github.com/devskill-org/ems/sigenergy"
)

// WeatherForecastCache caches weather forecast data with expiration.
type WeatherForecastCache struct {
	mu            sync.RWMutex
	forecast      *meteo.METJSONForecast
	fetchedAt     time.Time
	cacheDuration time.Duration
}

// Get retrieves the cached weather forecast if it's still valid.
func (w *WeatherForecastCache) Get() (*meteo.METJSONForecast, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.forecast == nil {
		return nil, false
	}

	if time.Since(w.fetchedAt) > w.cacheDuration {
		return nil, false
	}

	return w.forecast, true
}

// Set updates the cached weather forecast with a new value.
func (w *WeatherForecastCache) Set(forecast *meteo.METJSONForecast) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.forecast = forecast
	w.fetchedAt = time.Now()
}

// SolarForecastCache caches Open-Meteo solar irradiance forecast data with expiration.
type SolarForecastCache struct {
	mu            sync.RWMutex
	forecast      *openmeteo.SolarForecast
	fetchedAt     time.Time
	cacheDuration time.Duration
}

// Get retrieves the cached solar forecast if it's still valid.
func (c *SolarForecastCache) Get() (*openmeteo.SolarForecast, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.forecast == nil {
		return nil, false
	}

	if time.Since(c.fetchedAt) > c.cacheDuration {
		return nil, false
	}

	return c.forecast, true
}

// Set updates the cached solar forecast with a new value.
func (c *SolarForecastCache) Set(forecast *openmeteo.SolarForecast) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.forecast = forecast
	c.fetchedAt = time.Now()
}

// DataSample represents a single measurement of power and battery data.
type DataSample struct {
	pvPower            float64
	gridPower          float64 // positive = import, negative = export
	batteryPower       float64 // positive = charging, negative = discharging
	evdcPower          float64
	batterySoc         float64 // %
	batteryAvgCellTemp float64 // °C
	ts                 time.Time
}

// DataSamples is a thread-safe collection of power measurement samples.
type DataSamples struct {
	mu      sync.Mutex
	samples []DataSample
}

// AddSample adds a new power measurement sample to the collection.
func (d *DataSamples) AddSample(pvPower, gridPower, batteryPower, evdcPower, batterySoc, batteryAvgCellTemp float64, ts time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.samples = append(d.samples, DataSample{
		pvPower:            pvPower,
		gridPower:          gridPower,
		batteryPower:       batteryPower,
		evdcPower:          evdcPower,
		batterySoc:         batterySoc,
		batteryAvgCellTemp: batteryAvgCellTemp,
		ts:                 ts,
	})
}

// IntegratedData represents aggregated power measurements over a period.
type IntegratedData struct {
	pvTotalPower          float64
	gridExportPower       float64
	gridImportPower       float64
	batteryChargePower    float64
	batteryDischargePower float64
	evdcChargePower       float64
	loadPower             float64
	batterySoc            float64
	batteryAvgCellTemp    float64
	timestamp             time.Time
	sampleCount           int // Number of samples integrated
}

// IntegrateSamples computes integrated power values from collected samples up to the specified cutoff time.
// Only samples with timestamp <= cutoffTime are integrated.
// The cutoffTime represents the end of the integration period and is used as the result timestamp.
// Samples are preserved and must be cleared explicitly using ClearBefore() after successful processing.
func (d *DataSamples) IntegrateSamples(pollInterval time.Duration, cutoffTime time.Time) IntegratedData {
	d.mu.Lock()
	defer d.mu.Unlock()

	var result IntegratedData
	result.timestamp = cutoffTime

	for _, sample := range d.samples {
		// Only integrate samples that belong to this period
		if sample.ts.After(cutoffTime) {
			continue
		}

		result.sampleCount++
		energyKWh := pollInterval.Seconds() / 3600.0 // Convert to hours

		result.pvTotalPower += sample.pvPower * energyKWh

		// Grid power: positive = import, negative = export
		if sample.gridPower > 0 {
			result.gridImportPower += sample.gridPower * energyKWh
		} else if sample.gridPower < 0 {
			result.gridExportPower += -sample.gridPower * energyKWh
		}

		// Battery power: positive = charging, negative = discharging
		if sample.batteryPower > 0 {
			result.batteryChargePower += sample.batteryPower * energyKWh
		} else if sample.batteryPower < 0 {
			result.batteryDischargePower += -sample.batteryPower * energyKWh
		}

		// EV DC charging power
		result.evdcChargePower += sample.evdcPower * energyKWh

		// Keep the last battery SOC and temperature
		result.batterySoc = sample.batterySoc
		result.batteryAvgCellTemp = sample.batteryAvgCellTemp
	}

	// Calculate load: Load = PV + Battery Discharge + Grid Import - Battery Charge - Grid Export - EV Charge
	result.loadPower = result.pvTotalPower + result.batteryDischargePower + result.gridImportPower -
		result.batteryChargePower - result.gridExportPower - result.evdcChargePower

	return result
}

// ClearBefore removes all samples with timestamp <= cutoffTime from the collection.
// Should only be called after samples have been successfully processed for that period.
func (d *DataSamples) ClearBefore(cutoffTime time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Filter out samples that should be cleared
	filteredSamples := make([]DataSample, 0, len(d.samples))
	for _, sample := range d.samples {
		if sample.ts.After(cutoffTime) {
			filteredSamples = append(filteredSamples, sample)
		}
	}
	d.samples = filteredSamples
}

// IsEmpty returns true if there are no samples collected.
func (d *DataSamples) IsEmpty() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.samples) == 0
}

// GetLatestPower returns the most recent PV power sample, or 0 if no samples exist
func (d *DataSamples) GetLatestPower() float64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.samples) == 0 {
		return 0
	}
	return d.samples[len(d.samples)-1].pvPower
}

func (s *MinerScheduler) runDataPoll(ctx context.Context, samples *DataSamples) error {
	if s.config.PlantModbusAddress == "" {
		return nil
	}
	client, err := sigenergy.NewTCPClient(ctx, s.config.PlantModbusAddress, sigenergy.PlantAddress)
	if err != nil {
		s.logger.Printf("Data integration: failed to create modbus client: %v", err)
		return err
	}
	defer client.Close()
	info, err := client.ReadPlantRunningInfo()
	if err != nil {
		s.logger.Printf("Data integration: failed to read PlantRunningInfo: %v", err)
		return err
	}
	samples.AddSample(
		info.PhotovoltaicPower,
		info.GridSensorActivePower,
		info.ESSPower,
		info.DCChargerOutputPower,
		info.ESSSOC,
		info.ESSAvgCellTemperature,
		time.Now(),
	)
	return nil
}

func (s *MinerScheduler) runDataIntegration(samples *DataSamples, pollInterval time.Duration, dataDB *sql.DB, deviceID int, dryRun bool) error {
	// Calculate the period boundary timestamp (end of current integration period)
	// This ensures samples are grouped by their integration period
	config := s.GetConfig()
	now := time.Now()
	periodEndTime := now.Truncate(config.PVIntegrationPeriod)
	if periodEndTime.Before(now.Add(-config.PVIntegrationPeriod)) {
		periodEndTime = periodEndTime.Add(config.PVIntegrationPeriod)
	}

	// Integrate only samples up to the period boundary
	data := samples.IntegrateSamples(pollInterval, periodEndTime)

	if data.sampleCount == 0 {
		s.logger.Printf("Data integration: no samples collected in period ending at %s", periodEndTime.Format(time.RFC3339))
		return nil
	}

	timestamp := data.timestamp

	if dataDB == nil {
		samples.ClearBefore(periodEndTime)
		return nil
	}

	// Fetch weather data from meteo API
	cloudCoverage, err := s.fetchCloudCoverage()
	if err != nil {
		s.logger.Printf("Data integration: failed to fetch cloud coverage: %v", err)
	}

	weatherSymbol, err := s.fetchWeatherSymbol()
	if err != nil {
		s.logger.Printf("Data integration: failed to fetch weather symbol: %v", err)
	}

	// Calculate costs using current energy prices

	// Get current spot price for cost calculations
	var gridImportCost, gridExportCost float64
	s.mu.RLock()
	marketData := s.pricesMarketData
	s.mu.RUnlock()

	if marketData != nil {
		spotPrice, found := marketData.LookupPriceByTime(timestamp)
		if found && spotPrice > 0 {
			// Import cost: (spot price + operator fee + delivery fee) * energy in MWh
			importPricePerMWh := spotPrice + config.ImportPriceOperatorFee + config.ImportPriceDeliveryFee
			gridImportCost = (importPricePerMWh / 1000.0) * data.gridImportPower

			// Export revenue (negative cost): (spot price - operator fee) * energy in MWh
			exportPricePerMWh := spotPrice - config.ExportPriceOperatorFee
			gridExportCost = (exportPricePerMWh / 1000.0) * data.gridExportPower
		}
	}

	if dryRun {
		// DRY-RUN MODE: Log the action without saving to database
		s.logger.Printf("Data integration [DRY-RUN]: would save metrics for device_id=%d at %s (samples: %d)",
			deviceID, timestamp.Format(time.RFC3339), data.sampleCount)
		s.logger.Printf("  PV: %.3f kWh, Grid Import: %.3f kWh (%.3f), Grid Export: %.3f kWh (%.3f)",
			data.pvTotalPower, data.gridImportPower, gridImportCost, data.gridExportPower, gridExportCost)
		s.logger.Printf("  Battery Charge: %.3f kWh, Battery Discharge: %.3f kWh, SOC: %.1f%%, Temp: %.1f°C",
			data.batteryChargePower, data.batteryDischargePower, data.batterySoc, data.batteryAvgCellTemp)
		s.logger.Printf("  EV Charge: %.3f kWh, Load: %.3f kWh", data.evdcChargePower, data.loadPower)
		if cloudCoverage != nil {
			s.logger.Printf("  Cloud Coverage: %.1f%%", *cloudCoverage)
		}
		if weatherSymbol != nil {
			s.logger.Printf("  Weather: %s", *weatherSymbol)
		}
		samples.ClearBefore(periodEndTime)
	} else {
		// Insert comprehensive energy flow data
		_, err = dataDB.Exec(
			`INSERT INTO metrics (
				timestamp, device_id, metric_name,
				pv_total_power, cloud_coverage, weather_symbol,
				grid_export_power, grid_import_power,
				battery_charge_power, battery_discharge_power, battery_soc,
				evdc_charge_power, load_power,
				grid_export_cost, grid_import_cost,
				battery_avg_cell_temperature
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
			timestamp, deviceID, "energy_flow",
			data.pvTotalPower, cloudCoverage, weatherSymbol,
			data.gridExportPower, data.gridImportPower,
			data.batteryChargePower, data.batteryDischargePower, data.batterySoc,
			data.evdcChargePower, data.loadPower,
			gridExportCost, gridImportCost,
			data.batteryAvgCellTemp,
		)
		if err != nil {
			s.logger.Printf("Data integration: failed to insert metrics: %v", err)
			return err
		}

		// Only clear samples for this period after successful DB insertion
		samples.ClearBefore(periodEndTime)

		s.logger.Printf("Data integration: saved metrics for device_id=%d at %s (samples: %d)",
			deviceID, timestamp.Format(time.RFC3339), data.sampleCount)
		s.logger.Printf("  PV: %.3f kWh, Grid Import: %.3f kWh (%.3f), Grid Export: %.3f kWh (%.3f)",
			data.pvTotalPower, data.gridImportPower, gridImportCost, data.gridExportPower, gridExportCost)
		s.logger.Printf("  Battery Charge: %.3f kWh, Battery Discharge: %.3f kWh, SOC: %.1f%%, Temp: %.1f°C",
			data.batteryChargePower, data.batteryDischargePower, data.batterySoc, data.batteryAvgCellTemp)
		s.logger.Printf("  EV Charge: %.3f kWh, Load: %.3f kWh", data.evdcChargePower, data.loadPower)
	}
	return nil
}

func (s *MinerScheduler) fetchCloudCoverage() (*float64, error) {
	// Check cache first
	if cachedForecast, ok := s.weatherCache.Get(); ok {
		current := cachedForecast.GetCurrentWeather()
		if current == nil {
			return nil, nil
		}
		return current.GetCloudCoverage(), nil
	}

	// Cache miss, fetch from API
	s.logger.Printf("Data integration: fetching weather forecast from API")
	config := s.GetConfig()
	client := meteo.NewClient(config.UserAgent)

	location := meteo.Location{
		Latitude:  config.Latitude,
		Longitude: config.Longitude,
	}

	params := meteo.QueryParams{Location: location}
	forecast, err := client.GetCompact(params)
	if err != nil {
		return nil, err
	}

	// Store in cache
	s.weatherCache.Set(forecast)

	current := forecast.GetCurrentWeather()
	if current == nil {
		return nil, nil
	}

	return current.GetCloudCoverage(), nil
}

func (s *MinerScheduler) fetchWeatherSymbol() (*string, error) {
	// Check cache first
	if cachedForecast, ok := s.weatherCache.Get(); ok {
		current := cachedForecast.GetCurrentWeather()
		if current == nil {
			return nil, nil
		}
		symbol := current.GetSymbolCode()
		if symbol == nil {
			return nil, nil
		}
		symbolStr := string(*symbol)
		return &symbolStr, nil
	}

	// Cache miss, fetch from API
	config := s.GetConfig()
	client := meteo.NewClient(config.UserAgent)

	location := meteo.Location{
		Latitude:  config.Latitude,
		Longitude: config.Longitude,
	}

	params := meteo.QueryParams{Location: location}
	forecast, err := client.GetCompact(params)
	if err != nil {
		return nil, err
	}

	// Store in cache
	s.weatherCache.Set(forecast)

	current := forecast.GetCurrentWeather()
	if current == nil {
		return nil, nil
	}

	symbol := current.GetSymbolCode()
	if symbol == nil {
		return nil, nil
	}
	symbolStr := string(*symbol)
	return &symbolStr, nil
}

// GetPlantRunningInfo returns the current plant running information
// If PlantModbusAddress is not configured, returns nil
func (s *MinerScheduler) GetPlantRunningInfo(ctx context.Context) *sigenergy.PlantRunningInfo {
	if s.config.PlantModbusAddress == "" {
		return nil
	}

	client, err := sigenergy.NewTCPClient(ctx, s.config.PlantModbusAddress, sigenergy.PlantAddress)
	if err != nil {
		s.logger.Printf("Failed to create modbus client for plant info: %v", err)
		return nil
	}
	defer client.Close()

	info, err := client.ReadPlantRunningInfo()
	if err != nil {
		s.logger.Printf("Failed to read plant running info: %v", err)
		return nil
	}

	return info
}
