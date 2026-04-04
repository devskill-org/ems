# Energy Management System (EMS)

[![Tests](https://github.com/devskill-org/ems/actions/workflows/test.yml/badge.svg)](https://github.com/devskill-org/ems/actions/workflows/test.yml)
[![Coverage](https://codecov.io/gh/devskill-org/ems/branch/main/graph/badge.svg)](https://codecov.io/gh/devskill-org/ems)
[![Go Report Card](https://goreportcard.com/badge/github.com/devskill-org/ems)](https://goreportcard.com/report/github.com/devskill-org/ems)
[![Go Version](https://img.shields.io/github/go-mod/go-version/devskill-org/ems)](https://github.com/devskill-org/ems/blob/main/go.mod)
[![License](https://img.shields.io/github/license/devskill-org/ems)](https://github.com/devskill-org/ems/blob/main/LICENSE)

A comprehensive Go-based Energy Management System that optimizes energy consumption, production, and storage across multiple sources including solar (PV), battery storage, grid connection, and controllable loads. The system uses real-time electricity price data, weather forecasts, and Model Predictive Control (MPC) to minimize energy costs while maintaining system reliability.

![Web UI Screenshot](screen.png)

## Overview

The Energy Management System provides intelligent control and optimization for residential or small commercial energy systems. It integrates:

- **Solar Power (PV)**: Real-time monitoring via Modbus, solar position calculations, and weather-based forecasting
- **Battery Storage**: Intelligent charge/discharge scheduling with degradation cost consideration
- **Grid Connection**: Dynamic import/export management based on real-time pricing
- **Controllable Loads**: Automated management of high-power devices (e.g., cryptocurrency miners, EV chargers, heat pumps)
- **Price Optimization**: Integration with ENTSO-E Transparency Platform for real-time electricity market prices
- **Weather Integration**: Meteorological data for solar production forecasting
- **Web Dashboard**: Real-time monitoring and control interface

## Features

### Energy Source Management

- **Solar Power Monitoring**: Real-time PV production tracking via Modbus (SigEnergy integration)
- **Solar Forecasting**: Weather-based production forecasting with sun position calculations
- **Battery Management**: SOC monitoring, intelligent charge/discharge scheduling, degradation tracking
- **Grid Integration**: Dynamic import/export based on price signals and system state

### Load Management

- **Automatic Device Discovery**: Network scanning to find controllable loads (currently supports Avalon miners)
- **Price-Based Scheduling**: Automatically activates/deactivates loads based on configurable price thresholds
- **Power-Based Control**: Manages loads to stay within PV production limits or configured power budgets
- **Thermal Protection**: Monitors device temperatures and adjusts operation modes to prevent overheating
- **Multiple Operating Modes**: Support for eco, standard, and high-performance modes

### Optimization & Control

- **Model Predictive Control (MPC)**: Advanced optimization for battery scheduling and load management
- **Real-time Price Monitoring**: Fetches electricity prices from ENTSO-E API with caching
- **Dynamic Scheduling**: Adjusts system behavior based on price forecasts, weather, and system state
- **Configurable Thresholds**: Flexible configuration for prices, power limits, and operating modes

### System Features

- **Dry-run Mode**: Test configurations without executing actual control commands
- **Concurrent Operations**: Manages multiple devices simultaneously for efficiency
- **Robust Error Handling**: Comprehensive retry logic and error recovery
- **Health Monitoring**: Optional health check endpoint for system monitoring
- **Flexible Configuration**: Support for both command-line arguments and configuration files
- **Comprehensive Logging**: Detailed logging with configurable levels and formats
- **Web Interface**: Real-time dashboard with WebSocket updates

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                  Energy Management System                    │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐     │
│  │   Weather    │  │  ENTSO-E     │  │   Modbus     │     │
│  │  Forecasts   │  │  Pricing     │  │   (PV/Bat)   │     │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘     │
│         │                  │                  │              │
│         └──────────────────┼──────────────────┘              │
│                            ↓                                 │
│              ┌─────────────────────────┐                    │
│              │   MPC Optimization      │                    │
│              │   - Battery Schedule    │                    │
│              │   - Load Management     │                    │
│              │   - Cost Minimization   │                    │
│              └─────────┬───────────────┘                    │
│                        ↓                                     │
│         ┌──────────────────────────────┐                   │
│         │    Scheduler & Controller     │                   │
│         └──────────┬───────────────────┘                   │
│                    │                                         │
│        ┌───────────┼───────────┐                           │
│        ↓           ↓           ↓                            │
│   ┌────────┐  ┌────────┐  ┌────────┐                      │
│   │  PV    │  │Battery │  │ Loads  │                      │
│   │Monitor │  │Control │  │Control │                      │
│   └────────┘  └────────┘  └────────┘                      │
│                                                              │
│              ┌─────────────────────────┐                    │
│              │   Web Dashboard         │                    │
│              │   - Real-time Monitor   │                    │
│              │   - Manual Control      │                    │
│              │   - Charts & Analytics  │                    │
│              └─────────────────────────┘                    │
└─────────────────────────────────────────────────────────────┘
```

## How It Works

The Energy Management System operates on multiple time scales:

### Real-time Monitoring (10-second intervals, configurable)
1. **PV Production**: Reads current solar power production via Modbus
2. **Battery State**: Monitors State of Charge (SOC), power flow, and health
3. **Grid Metrics**: Tracks import/export power and cumulative energy
4. **Data Logging**: Stores metrics in PostgreSQL for analysis and visualization

### Load State Management (1-minute intervals, configurable)
1. **Device Monitoring**: Checks state and health of all controllable loads
2. **Temperature Protection**: Monitors device temperatures and adjusts modes
   - High temperature: Switches to Eco mode to prevent overheating
   - Normal temperature: Returns to Standard/Super mode as configured
3. **Power Limit Enforcement**: Ensures total load stays within configured limits
4. **Fault Detection**: Identifies and logs device issues

### Energy Optimization (15-minute intervals, configurable)
1. **Price Updates**: Fetches current and forecasted electricity prices at 15-minute intervals
2. **Weather Forecast**: Updates solar production forecast
3. **MPC Optimization**: Calculates optimal battery and load schedules for next 24-48 hours using exact 15-minute price data (not hourly averages)
4. **Schedule Execution**: Implements optimized decisions for current 15-minute period
5. **Load Management**:
   - **Price below threshold**: Activate loads, prioritize self-consumption
   - **Price above threshold**: Deactivate non-essential loads, maximize export
   - **Excess PV available**: Activate loads to consume surplus power
   - **Limited PV**: Deactivate loads or switch to economy modes

### Thermal Protection
- **High Temperature Detection**: Fan speed monitoring (for miners) or temperature sensors
- **Automatic Mode Reduction**: Switches devices to lower power modes when overheating
- **Gradual Recovery**: Returns to higher performance modes when safe
- **Configurable Thresholds**: Customizable temperature/fan speed limits

## Prerequisites

- Go 1.25.1 or later
- ENTSO-E Transparency Platform API token (optional, for price-based optimization)
- Network access to controllable devices
- Modbus-compatible PV/battery system (optional, for solar monitoring)
- PostgreSQL database (optional, for data logging)

## Installation

### From Source

```bash
git clone https://github.com/devskill-org/ems.git
cd energy-management-system
go build -o ems ./
```

### Using Docker

```bash
docker build -t ems .
docker run ems
```

## Configuration

### Command Line Usage

```bash
# Basic usage with defaults
./ems

# Custom configuration
./ems --config custom.json

# Show plant information
./ems -info

# Run web server only (no automated control)
./ems -serverOnly

# Show help
./ems -help
```

### Configuration File

Create a `config.json` file for system configuration:

```json
{
  "price_limit": 50.0,
  "network": "192.168.1.0/24",
  "check_price_interval": "15m",
  "miner_discovery_interval": "10m",
  "miners_state_check_interval": "1m",
  "dry_run": false,
  "api_timeout": "30s",
  "log_level": "info",
  "log_format": "text",
  "miner_timeout": "5s",
  "health_check_port": 8080,
  "security_token": "<ENTSO-E API token>",
  "fanr_high_threshold": 70,
  "fanr_low_threshold": 50,
  "url_format": "https://web-api.tp.entsoe.eu/api?documentType=A44&out_Domain=10YLV-1001A00074&in_Domain=10YLV-1001A00074&periodStart=%s&periodEnd=%s&securityToken=%s",
  "location": "CET",
  "plant_modbus_address": "192.168.1.100:502",
  "device_id": 0,
  "pv_poll_interval": "10s",
  "pv_integration_period": "15m",
  "postgres_conn_string": "postgres://user:pass@host/dbname",
  "battery_capacity": 24.0,
  "battery_max_charge": 12.0,
  "battery_max_discharge": 12.0,
  "battery_min_soc": 0.0,
  "battery_max_soc": 1.0,
  "battery_efficiency": 0.92,
  "battery_degradation_cost": 0.05,
  "max_grid_import": 30.0,
  "max_grid_export": 30.0,
  "max_solar_power": 30.0,
  "import_price_operator_fee": 8.5,
  "import_price_delivery_fee": 40.0,
  "export_price_operator_fee": 17.0,
  "miners_power_limit": 30.0,
  "miner_power_standby": 0.05,
  "miner_power_eco": 0.8,
  "miner_power_standard": 1.6,
  "miner_power_super": 1.8,
  "pv_power_control_price_limit": 100.0,
  "latitude": 56.9496,
  "longitude": 24.1052,
  "weather_update_interval": "1h",
  "user_agent": "EMS/1.0 (contact@example.com)"
}
```

## Configuration Options

### Core Settings

| Option | Default | Description |
|--------|---------|-------------|
| `price_limit` | 50.0 | Price threshold in EUR/MWh for load activation |
| `network` | "192.168.1.0/24" | Network to scan for controllable devices (CIDR notation) |
| `check_price_interval` | 15m | Frequency of price checks and optimization |
| `dry_run` | false | Simulation mode (log actions without executing) |
| `log_level` | info | Logging level (debug, info, warn, error) |
| `log_format` | text | Log format (text, json) |
| `health_check_port` | 8080 | Health check and web dashboard port (0 = disabled) |

### Energy Sources

| Option | Default | Description |
|--------|---------|-------------|
| `plant_modbus_address` | "" | Modbus TCP address for PV/battery system |
| `device_id` | 0 | Modbus device ID |
| `pv_poll_interval` | 10s | PV system polling frequency |
| `pv_integration_period` | 15m | Period for PV data integration |
| `max_solar_power` | 30.0 | Maximum solar system capacity (kW) |

### Battery Settings

| Option | Default | Description |
|--------|---------|-------------|
| `battery_capacity` | 24.0 | Battery capacity (kWh) |
| `battery_max_charge` | 12.0 | Maximum charge power (kW) |
| `battery_max_discharge` | 12.0 | Maximum discharge power (kW) |
| `battery_min_soc` | 0.0 | Minimum State of Charge (0.0-1.0) |
| `battery_max_soc` | 1.0 | Maximum State of Charge (0.0-1.0) |
| `battery_efficiency` | 0.92 | Round-trip efficiency (0.0-1.0) |
| `battery_degradation_cost` | 0.05 | Cost per kWh for battery degradation (EUR) |

### Grid Settings

| Option | Default | Description |
|--------|---------|-------------|
| `max_grid_import` | 30.0 | Maximum grid import power (kW) |
| `max_grid_export` | 30.0 | Maximum grid export power (kW) |
| `import_price_operator_fee` | 8.5 | Grid operator fee for import (EUR/MWh) |
| `import_price_delivery_fee` | 40.0 | Delivery fee for import (EUR/MWh) |
| `export_price_operator_fee` | 17.0 | Grid operator fee for export (EUR/MWh) |

### Load Management

| Option | Default | Description |
|--------|---------|-------------|
| `miner_discovery_interval` | 10m | Device discovery frequency |
| `miners_state_check_interval` | 1m | Device state monitoring frequency |
| `miner_timeout` | 5s | Timeout for device operations |
| `miners_power_limit` | 30.0 | Maximum total power for controllable loads (kW) |
| `pv_power_control_price_limit` | 99999.0 | EUR/MWh price threshold at or above which PV-based power limiting is enabled |
| `fanr_high_threshold` | 70 | Fan speed % triggering power reduction |
| `fanr_low_threshold` | 50 | Fan speed % allowing power increase |

### Load Power Consumption

| Option | Default | Description |
|--------|---------|-------------|
| `miner_power_standby` | 0.05 | Power consumption in standby (kW) |
| `miner_power_eco` | 0.8 | Power consumption in eco mode (kW) |
| `miner_power_standard` | 1.6 | Power consumption in standard mode (kW) |
| `miner_power_super` | 1.8 | Power consumption in super mode (kW) |

### Weather & Location

| Option | Default | Description |
|--------|---------|-------------|
| `latitude` | 56.9496 | Location latitude for solar calculations |
| `longitude` | 24.1052 | Location longitude for solar calculations |
| `weather_update_interval` | 1h | Weather forecast update frequency |
| `user_agent` | "" | User agent for weather API requests |

### Pricing API

| Option | Default | Description |
|--------|---------|-------------|
| `security_token` | "" | ENTSO-E API token |
| `url_format` | "" | ENTSO-E API URL format |
| `location` | "CET" | Timezone for price data |
| `api_timeout` | 30s | Timeout for API calls |

### Data Storage

| Option | Default | Description |
|--------|---------|-------------|
| `postgres_conn_string` | "" | PostgreSQL connection string for data logging |

## Usage Examples

### Basic Setup

1. Get your ENTSO-E API token from the [transparency platform](https://transparency.entsoe.eu/)
2. Configure your system in `config.json`
3. Run the EMS:
   ```bash
   ./ems
   ```

### Testing with Dry-run Mode

Test your configuration without controlling actual devices:

```bash
./ems --config config.json
```

Set `"dry_run": true` in your config file.

Example output:
```
[SCHEDULER] DRY-RUN MODE ENABLED: Actions will be simulated only
[SCHEDULER] Current electricity price: 65.50 EUR/MWh
[SCHEDULER] PV Production: 12.5 kW, Battery SOC: 85%
[SCHEDULER] Price limit: 60.00 EUR/MWh
[SCHEDULER] DRY-RUN: Would reduce load at 192.168.1.100:4028 (price too high)
[SCHEDULER] DRY-RUN: Would charge battery (excess PV available)
```

### Monitoring System Status

Access the web dashboard at `http://localhost:8080` (or your configured port) to:
- View real-time energy flow
- Monitor PV production and battery state
- See current electricity prices
- Control devices manually
- View historical data and charts

### Viewing Plant Information

Check your PV/battery system configuration:

```bash
./ems -info
```

## API Reference

### Device States

The system recognizes multiple device states:

- `Running` (0): Device is active
- `Mining` (1): Device is actively processing (for miners)
- `StandBy` (2): Device is in standby/idle mode

### Device Operating Modes

Controllable devices support multiple operating modes:

- `Eco Mode` (0): Reduced power consumption (e.g., 0.8 kW)
- `Standard Mode` (1): Normal operation (e.g., 1.6 kW)
- `Super Mode` (2): Maximum performance (e.g., 1.8 kW)

### EMS Operations

- **WakeUp**: Activates device from standby
- **Standby**: Transitions device to standby state
- **SetWorkMode**: Changes device operating mode
- **GetLiteStats**: Retrieves current device status
- **GetPlantRunningInfo**: Retrieves complete plant running information via Modbus (PV power, battery SOC, grid power, ESS power, etc.)
- **GetBatterySOC**: Reads battery state of charge
- **OptimizeSchedule**: Runs MPC optimization

## Web Dashboard

The integrated web interface provides:

- **Real-time Monitoring**: Live updates via WebSocket
  - Current PV production
  - Battery state and power flow
  - Grid import/export
  - Load status and power consumption
  
- **Price Display**: Current and forecasted electricity prices

- **Device Control**: Manual override of automatic control
  - Activate/deactivate loads
  - Change operating modes
  - Force battery charge/discharge

- **Historical Data**: Charts and graphs of system performance
  - Energy production and consumption
  - Price trends
  - Battery usage patterns

- **System Status**: Health monitoring and diagnostics

Access the dashboard at: `http://<your-server-ip>:8080`

## Troubleshooting

### Common Issues

1. **No devices discovered**
   - Check network configuration (CIDR notation)
   - Verify devices are accessible and have API enabled
   - Check firewall settings

2. **Modbus connection errors**
   - Verify `plant_modbus_address` is correct
   - Check network connectivity to PV system
   - Ensure Modbus TCP is enabled on the device

3. **Price API errors**
   - Verify `security_token` is correct
   - Check internet connectivity
   - Ensure ENTSO-E API is accessible
   - Verify `url_format` matches your region

4. **Database connection issues**
   - Verify PostgreSQL connection string
   - Check database permissions
   - Ensure database schema is initialized

5. **Device control failures**
   - Check device API port (default: 4028 for miners)
   - Verify device firmware supports required commands
   - Check network connectivity

### Debug Mode

Enable detailed logging:

```json
{
  "log_level": "debug",
  "log_format": "json"
}
```

### Health Check

Monitor system health:

```bash
curl http://localhost:8080/health
```

Expected response:
```json
{
  "status": "healthy",
  "timestamp": "2024-01-15T10:30:00Z",
  "pv_connected": true,
  "database_connected": true,
  "devices_discovered": 3
}
```

## Use Cases

### Residential Solar + Battery System

Optimize self-consumption and minimize grid import costs:
- Charge battery during low-price periods
- Discharge battery during high-price periods
- Activate high-power loads when excess PV is available
- Export surplus to grid when prices are favorable

### Cryptocurrency Mining Operation

Maximize profitability by mining only when economical:
- Mine only when electricity prices are below threshold
- Scale mining power with available PV production
- Protect hardware with thermal management
- Track energy costs for profitability analysis

### Small Commercial Energy Management

Optimize energy costs for small businesses:
- Schedule high-power equipment during low-price periods
- Maximize solar self-consumption
- Peak shaving with battery storage
- Demand response based on price signals

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

This project is licensed under the terms specified in the [LICENSE](LICENSE) file.

## Acknowledgments

- ENTSO-E Transparency Platform for electricity price data
- Open-Meteo for weather forecasts
- SigEnergy for PV system integration
- The Go community for excellent libraries and tools

## Support

- **Issues**: [GitHub Issues](https://github.com/devskill-org/ems/issues)
- **Discussions**: [GitHub Discussions](https://github.com/devskill-org/ems/discussions)
- **Documentation**: Check the `/docs` folder for detailed guides

## Roadmap

- [ ] Support for additional controllable load types (EV chargers, heat pumps, etc.)
- [ ] Machine learning for improved solar production forecasting
- [ ] Integration with additional battery systems
- [ ] Mobile app for remote monitoring
- [ ] Multi-site management
- [ ] Enhanced MPC algorithms
- [ ] Integration with home automation systems (Home Assistant, etc.)
