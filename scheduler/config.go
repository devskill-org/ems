package scheduler

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Config represents the configuration for the miner scheduler
type Config struct {
	// Scheduler settings
	PriceLimit               float64       `json:"price_limit"`                 // Price limit in EUR/MWh
	Network                  string        `json:"network"`                     // Network to scan for miners (CIDR notation)
	CheckPriceInterval       time.Duration `json:"check_price_interval"`        // How often to run the task
	MinersStateCheckInterval time.Duration `json:"miners_state_check_interval"` // How often to check miners state
	MinerDiscoveryInterval   time.Duration `json:"miner_discovery_interval"`    // How often to discover miners
	DryRun                   bool          `json:"dry_run"`                     // Run in dry-run mode (simulate actions without executing)

	// API settings
	SecurityToken string        `json:"security_token"` // ENTSO-E API token
	APITimeout    time.Duration `json:"api_timeout"`    // Timeout for API calls
	UrlFormat     string        `json:"url_format"`     // ENTSO-E API URL format string

	// Logging settings
	LogLevel  string `json:"log_level"`  // Log level: debug, info, warn, error
	LogFormat string `json:"log_format"` // Log format: text, json

	// Timezone configuration
	Location string `json:"location"` // Timezone location string (e.g., "CET"), when the market data published at 00:00

	// Miner settings
	MinerTimeout time.Duration `json:"miner_timeout"` // Timeout for miner operations

	// Advanced settings
	HealthCheckPort int `json:"health_check_port"` // Port for health check endpoint (0 = disabled)

	// FanR thresholds for work mode switching
	FanRHighThreshold int `json:"fanr_high_threshold"` // FanR threshold to decrease work mode
	FanRLowThreshold  int `json:"fanr_low_threshold"`  // FanR threshold to increase work mode

	// Power consumption settings (in kilowatts)
	MinersPowerLimit   float64 `json:"miners_power_limit"`   // Maximum total power limit for miners in kW
	MinerPowerStandby  float64 `json:"miner_power_standby"`  // Power consumption in standby mode (kW)
	MinerPowerEco      float64 `json:"miner_power_eco"`      // Power consumption in eco mode (kW)
	MinerPowerStandard float64 `json:"miner_power_standard"` // Power consumption in standard mode (kW)
	MinerPowerSuper    float64 `json:"miner_power_super"`    // Power consumption in super mode (kW)
	UsePVPowerControl  bool    `json:"use_pv_power_control"` // Enable PV power-based control

	// Plant Modbus server
	PlantModbusAddress string `json:"plant_modbus_address"` // Plant Modbus server address (format: IP:PORT, e.g., "192.168.1.100:502")

	// PV metrics integration
	DeviceID            int           `json:"device_id"`             // Device ID for metrics table
	PVPollInterval      time.Duration `json:"pv_poll_interval"`      // Poll interval for PV power (duration)
	PVIntegrationPeriod time.Duration `json:"pv_integration_period"` // Integration period for PV power (duration)
	PostgresConnString  string        `json:"postgres_conn_string"`  // PostgreSQL connection string

	// Weather API settings
	WeatherUpdateInterval time.Duration `json:"weather_update_interval"` // How often to update weather
	Latitude              float64       `json:"latitude"`                // Latitude for weather data
	Longitude             float64       `json:"longitude"`               // Longitude for weather data
	UserAgent             string        `json:"user_agent"`              // User agent for weather API client

	// Battery/Inverter system configuration (MPC)
	BatteryCapacity        float64       `json:"battery_capacity"`         // kWh
	BatteryMaxCharge       float64       `json:"battery_max_charge"`       // kW
	BatteryMaxDischarge    float64       `json:"battery_max_discharge"`    // kW
	BatteryMinSOC          float64       `json:"battery_min_soc"`          // percentage (0-1)
	BatteryMaxSOC          float64       `json:"battery_max_soc"`          // percentage (0-1)
	BatteryEfficiency      float64       `json:"battery_efficiency"`       // round-trip efficiency (0-1)
	BatteryDegradationCost float64       `json:"battery_degradation_cost"` // $/kWh cycled
	MaxGridImport          float64       `json:"max_grid_import"`          // kW
	MaxGridExport          float64       `json:"max_grid_export"`          // kW
	MaxSolarPower          float64       `json:"max_solar_power"`          // kW - peak solar power capacity
	MPCExecutionInterval   time.Duration `json:"mpc_execution_interval"`   // How often to re-execute current MPC decision

	// Price adjustments
	ImportPriceOperatorFee float64 `json:"import_price_operator_fee"` // EUR/MWh - Operator fee for import
	ImportPriceDeliveryFee float64 `json:"import_price_delivery_fee"` // EUR/MWh - Delivery fee for import
	ExportPriceOperatorFee float64 `json:"export_price_operator_fee"` // EUR/MWh - Operator fee for export (subtracted)
}

// DefaultConfig returns a configuration with default values
func DefaultConfig() *Config {
	return &Config{
		PriceLimit:               60.0,
		Network:                  "192.168.88.0/24",
		CheckPriceInterval:       15 * time.Minute,
		MinersStateCheckInterval: 1 * time.Minute,
		MinerDiscoveryInterval:   10 * time.Minute,
		MPCExecutionInterval:     1 * time.Minute,
		DryRun:                   false,
		APITimeout:               30 * time.Second,
		LogLevel:                 "info",
		LogFormat:                "text",
		MinerTimeout:             5 * time.Second,
		HealthCheckPort:          0,
		DeviceID:                 0,
		PVPollInterval:           10 * time.Second,
		PVIntegrationPeriod:      15 * time.Minute,
		PostgresConnString:       "",
		UrlFormat:                "https://web-api.tp.entsoe.eu/api?documentType=A44&out_Domain=10YLV-1001A00074&in_Domain=10YLV-1001A00074&periodStart=%s&periodEnd=%s&securityToken=%s",
		PlantModbusAddress:       "",
		Latitude:                 56.9496, // Riga, Latvia
		Longitude:                24.1052, // Riga, Latvia
		WeatherUpdateInterval:    1 * time.Hour,
		UserAgent:                "MyApp/1.0 (username@example.com)",
		BatteryCapacity:          24.0,  // 24 kWh
		BatteryMaxCharge:         12.0,  // 12 kW
		BatteryMaxDischarge:      12.0,  // 12 kW
		BatteryMinSOC:            0.0,   // 0%
		BatteryMaxSOC:            1.0,   // 100%
		BatteryEfficiency:        0.92,  // 92% round-trip
		BatteryDegradationCost:   0.05,  // $0.05 per kWh cycled
		MaxGridImport:            30.0,  // 30 kW
		MaxGridExport:            30.0,  // 30 kW
		MaxSolarPower:            30.0,  // 30 kW peak solar power
		ImportPriceOperatorFee:   8.5,   // 8.5 EUR/MWh from Operator
		ImportPriceDeliveryFee:   40.0,  // 40 EUR/MWh for delivery
		ExportPriceOperatorFee:   17.0,  // 17 EUR/MWh from Operator
		MinersPowerLimit:         30.0,  // 30 kW total power limit for miners
		MinerPowerStandby:        0.05,  // 0.05 kW (50 W) in standby
		MinerPowerEco:            0.8,   // 0.8 kW (800 W) in eco mode
		MinerPowerStandard:       1.6,   // 1.6 kW (1600 W) in standard mode
		MinerPowerSuper:          1.8,   // 1.8 kW (1800 W) in super mode
		UsePVPowerControl:        false, // Disabled by default
	}
}

// LoadConfig loads configuration from a JSON file
func LoadConfig(filename string) (*Config, error) {
	// Clean the filepath to prevent directory traversal
	cleanPath := filepath.Clean(filename)
	// #nosec G304 -- filename is cleaned and comes from trusted configuration
	file, err := os.Open(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	return LoadConfigFromReader(file)
}

// LoadConfigFromReader loads configuration from an io.Reader
func LoadConfigFromReader(reader io.Reader) (*Config, error) {
	config := DefaultConfig()

	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(config); err != nil {
		return nil, fmt.Errorf("failed to decode config JSON: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return config, nil
}

// SaveConfig saves the configuration to a JSON file
func (c *Config) SaveConfig(filename string) error {
	// Clean the filepath to prevent directory traversal
	cleanPath := filepath.Clean(filename)
	// #nosec G304 -- filename is cleaned and comes from trusted configuration
	file, err := os.Create(cleanPath)
	if err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}
	defer file.Close()

	return c.SaveConfigToWriter(file)
}

// SaveConfigToWriter saves the configuration to an io.Writer
func (c *Config) SaveConfigToWriter(writer io.Writer) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(c); err != nil {
		return fmt.Errorf("failed to encode config JSON: %w", err)
	}

	return nil
}

// Validate checks if the configuration values are valid
func (c *Config) Validate() error {
	if c.SecurityToken == "" {
		return fmt.Errorf("security_token cannot be empty")
	}

	if c.Network == "" {
		return fmt.Errorf("network cannot be empty")
	}

	if c.CheckPriceInterval <= 0 {
		return fmt.Errorf("check_price_interval must be greater than 0, got: %s", c.CheckPriceInterval)
	}

	if c.WeatherUpdateInterval <= 0 {
		return fmt.Errorf("weather_update_interval must be greater than 0, got: %s", c.WeatherUpdateInterval)
	}

	if c.MinersStateCheckInterval <= 0 {
		return fmt.Errorf("miners_state_check_interval must be greater than 0, got: %s", c.MinersStateCheckInterval)
	}

	if c.MinerDiscoveryInterval <= 0 {
		return fmt.Errorf("miner_discovery_interval must be greater than 0, got: %s", c.MinerDiscoveryInterval)
	}

	if c.APITimeout <= 0 {
		return fmt.Errorf("api_timeout must be greater than 0, got: %s", c.APITimeout)
	}

	if c.UrlFormat == "" {
		return fmt.Errorf("url_format cannot be empty")
	}

	if c.MinerTimeout <= 0 {
		return fmt.Errorf("miner_timeout must be greater than 0, got: %s", c.MinerTimeout)
	}

	if c.HealthCheckPort < 0 || c.HealthCheckPort > 65535 {
		return fmt.Errorf("health_check_port must be between 0 and 65535, got: %d", c.HealthCheckPort)
	}

	// Validate log level
	validLogLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if !validLogLevels[c.LogLevel] {
		return fmt.Errorf("invalid log_level: %s, must be one of: debug, info, warn, error", c.LogLevel)
	}

	// Validate log format
	validLogFormats := map[string]bool{
		"text": true,
		"json": true,
	}
	if !validLogFormats[c.LogFormat] {
		return fmt.Errorf("invalid log_format: %s, must be one of: text, json", c.LogFormat)
	}

	// Validate latitude
	if c.Latitude < -90 || c.Latitude > 90 {
		return fmt.Errorf("latitude must be between -90 and 90, got: %f", c.Latitude)
	}

	// Validate longitude
	if c.Longitude < -180 || c.Longitude > 180 {
		return fmt.Errorf("longitude must be between -180 and 180, got: %f", c.Longitude)
	}

	// Validate user agent
	if c.UserAgent == "" {
		return fmt.Errorf("user_agent cannot be empty")
	}

	// Validate battery configuration
	if c.BatteryCapacity < 0 {
		return fmt.Errorf("battery_capacity must be non-negative, got: %f", c.BatteryCapacity)
	}

	if c.BatteryMaxCharge < 0 {
		return fmt.Errorf("battery_max_charge must be non-negative, got: %f", c.BatteryMaxCharge)
	}

	if c.BatteryMaxDischarge < 0 {
		return fmt.Errorf("battery_max_discharge must be non-negative, got: %f", c.BatteryMaxDischarge)
	}

	if c.BatteryMinSOC < 0 || c.BatteryMinSOC > 1 {
		return fmt.Errorf("battery_min_soc must be between 0 and 1, got: %f", c.BatteryMinSOC)
	}

	if c.BatteryMaxSOC < 0 || c.BatteryMaxSOC > 1 {
		return fmt.Errorf("battery_max_soc must be between 0 and 1, got: %f", c.BatteryMaxSOC)
	}

	if c.BatteryMinSOC > c.BatteryMaxSOC {
		return fmt.Errorf("battery_min_soc (%f) cannot be greater than battery_max_soc (%f)", c.BatteryMinSOC, c.BatteryMaxSOC)
	}

	if c.BatteryEfficiency < 0 || c.BatteryEfficiency > 1 {
		return fmt.Errorf("battery_efficiency must be between 0 and 1, got: %f", c.BatteryEfficiency)
	}

	if c.BatteryDegradationCost < 0 {
		return fmt.Errorf("battery_degradation_cost must be non-negative, got: %f", c.BatteryDegradationCost)
	}

	if c.MaxGridImport < 0 {
		return fmt.Errorf("max_grid_import must be non-negative, got: %f", c.MaxGridImport)
	}

	if c.MaxGridExport < 0 {
		return fmt.Errorf("max_grid_export must be non-negative, got: %f", c.MaxGridExport)
	}

	if c.MaxSolarPower < 0 {
		return fmt.Errorf("max_solar_power must be non-negative, got: %f", c.MaxSolarPower)
	}

	// Validate price adjustments
	if c.ImportPriceOperatorFee < 0 {
		return fmt.Errorf("import_price_operator_fee must be non-negative, got: %f", c.ImportPriceOperatorFee)
	}

	if c.ImportPriceDeliveryFee < 0 {
		return fmt.Errorf("import_price_delivery_fee must be non-negative, got: %f", c.ImportPriceDeliveryFee)
	}

	if c.ExportPriceOperatorFee < 0 {
		return fmt.Errorf("export_price_operator_fee must be non-negative, got: %f", c.ExportPriceOperatorFee)
	}

	// Validate power settings
	if c.MinersPowerLimit < 0 {
		return fmt.Errorf("miners_power_limit must be non-negative, got: %f", c.MinersPowerLimit)
	}

	if c.MinerPowerStandby < 0 {
		return fmt.Errorf("miner_power_standby must be non-negative, got: %f", c.MinerPowerStandby)
	}

	if c.MinerPowerEco < 0 {
		return fmt.Errorf("miner_power_eco must be non-negative, got: %f", c.MinerPowerEco)
	}

	if c.MinerPowerStandard < 0 {
		return fmt.Errorf("miner_power_standard must be non-negative, got: %f", c.MinerPowerStandard)
	}

	if c.MinerPowerSuper < 0 {
		return fmt.Errorf("miner_power_super must be non-negative, got: %f", c.MinerPowerSuper)
	}

	// Validate PV integration settings
	if c.PVPollInterval <= 0 {
		return fmt.Errorf("pv_poll_interval must be greater than 0, got: %s", c.PVPollInterval)
	}

	if c.PVIntegrationPeriod <= 0 {
		return fmt.Errorf("pv_integration_period must be greater than 0, got: %s", c.PVIntegrationPeriod)
	}

	return nil
}

// MarshalJSON implements custom JSON marshaling to handle durations
func (c *Config) MarshalJSON() ([]byte, error) {
	type Alias Config
	return json.Marshal(&struct {
		*Alias
		CheckInterval            string `json:"check_price_interval"`
		MinersStateCheckInterval string `json:"miners_state_check_interval"`
		MinerDiscoveryInterval   string `json:"miner_discovery_interval"`
		MPCExecutionInterval     string `json:"mpc_execution_interval"`
		APITimeout               string `json:"api_timeout"`
		MinerTimeout             string `json:"miner_timeout"`
		PVPollInterval           string `json:"pv_poll_interval"`
		PVIntegrationPeriod      string `json:"pv_integration_period"`
		WeatherUpdateInterval    string `json:"weather_update_interval"`
	}{
		Alias:                    (*Alias)(c),
		CheckInterval:            c.CheckPriceInterval.String(),
		MinersStateCheckInterval: c.MinersStateCheckInterval.String(),
		MinerDiscoveryInterval:   c.MinerDiscoveryInterval.String(),
		MPCExecutionInterval:     c.MPCExecutionInterval.String(),
		APITimeout:               c.APITimeout.String(),
		MinerTimeout:             c.MinerTimeout.String(),
		PVPollInterval:           c.PVPollInterval.String(),
		PVIntegrationPeriod:      c.PVIntegrationPeriod.String(),
		WeatherUpdateInterval:    c.WeatherUpdateInterval.String(),
	})
}

// UnmarshalJSON implements custom JSON unmarshaling to handle durations
func (c *Config) UnmarshalJSON(data []byte) error {
	type Alias Config
	aux := &struct {
		*Alias
		CheckPriceInterval       string `json:"check_price_interval"`
		MinersStateCheckInterval string `json:"miners_state_check_interval"`
		MinerDiscoveryInterval   string `json:"miner_discovery_interval"`
		MPCExecutionInterval     string `json:"mpc_execution_interval"`
		APITimeout               string `json:"api_timeout"`
		MinerTimeout             string `json:"miner_timeout"`
		UrlFormat                string `json:"url_format"`
		PVPollInterval           string `json:"pv_poll_interval"`
		PVIntegrationPeriod      string `json:"pv_integration_period"`
		WeatherUpdateInterval    string `json:"weather_update_interval"`
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	var err error
	if aux.CheckPriceInterval != "" {
		if c.CheckPriceInterval, err = time.ParseDuration(aux.CheckPriceInterval); err != nil {
			return fmt.Errorf("invalid check_price_interval: %w", err)
		}
	}

	if aux.WeatherUpdateInterval != "" {
		if c.WeatherUpdateInterval, err = time.ParseDuration(aux.WeatherUpdateInterval); err != nil {
			return fmt.Errorf("invalid weather_update_interval: %w", err)
		}
	}

	if aux.MinersStateCheckInterval != "" {
		if c.MinersStateCheckInterval, err = time.ParseDuration(aux.MinersStateCheckInterval); err != nil {
			return fmt.Errorf("invalid miners_state_check_interval: %w", err)
		}
	}

	if aux.MinerDiscoveryInterval != "" {
		if c.MinerDiscoveryInterval, err = time.ParseDuration(aux.MinerDiscoveryInterval); err != nil {
			return fmt.Errorf("invalid miner_discovery_interval: %w", err)
		}
	}

	if aux.MPCExecutionInterval != "" {
		if c.MPCExecutionInterval, err = time.ParseDuration(aux.MPCExecutionInterval); err != nil {
			return fmt.Errorf("invalid mpc_execution_interval: %w", err)
		}
	}

	if aux.APITimeout != "" {
		if c.APITimeout, err = time.ParseDuration(aux.APITimeout); err != nil {
			return fmt.Errorf("invalid api_timeout: %w", err)
		}
	}

	if aux.MinerTimeout != "" {
		if c.MinerTimeout, err = time.ParseDuration(aux.MinerTimeout); err != nil {
			return fmt.Errorf("invalid miner_timeout: %w", err)
		}
	}

	if aux.PVPollInterval != "" {
		if c.PVPollInterval, err = time.ParseDuration(aux.PVPollInterval); err != nil {
			return fmt.Errorf("invalid pv_poll_interval: %w", err)
		}
	}
	if aux.PVIntegrationPeriod != "" {
		if c.PVIntegrationPeriod, err = time.ParseDuration(aux.PVIntegrationPeriod); err != nil {
			return fmt.Errorf("invalid pv_integration_period: %w", err)
		}
	}
	if aux.UrlFormat != "" {
		c.UrlFormat = aux.UrlFormat
	}

	return nil
}

// String returns a string representation of the config
func (c *Config) String() string {
	data, _ := json.MarshalIndent(c, "", "  ")
	return string(data)
}
