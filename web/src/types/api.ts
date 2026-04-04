export interface EmsConfig {
  price_limit: number;
  network: string;
  check_price_interval: string;
  miners_state_check_interval: string;
  miner_discovery_interval: string;
  miner_max_consecutive_errors: number;
  dry_run: boolean;
  security_token: string;
  api_timeout: string;
  url_format: string;
  log_level: string;
  log_format: string;
  location: string;
  miner_timeout: string;
  health_check_port: number;
  fanr_high_threshold: number;
  fanr_low_threshold: number;
  miners_power_limit: number;
  miner_power_standby: number;
  miner_power_eco: number;
  miner_power_standard: number;
  miner_power_super: number;
  pv_power_control_price_limit: number;
  plant_modbus_address: string;
  device_id: number;
  pv_poll_interval: string;
  pv_integration_period: string;
  postgres_conn_string: string;
  weather_update_interval: string;
  latitude: number;
  longitude: number;
  user_agent: string;
  battery_capacity: number;
  battery_max_charge: number;
  battery_max_discharge: number;
  battery_min_soc: number;
  battery_max_soc: number;
  battery_efficiency: number;
  battery_degradation_cost: number;
  max_grid_import: number;
  max_grid_export: number;
  max_solar_power: number;
  mpc_execution_interval: string;
  battery_preheat_power: number;
  battery_preheat_temp_threshold: number;
  battery_thermal_time_constant: number;
  import_price_operator_fee: number;
  import_price_delivery_fee: number;
  export_price_operator_fee: number;
}

export interface MPCDecisionInfo {
  hour: number;
  timestamp: number;
  battery_charge: number;
  battery_discharge: number;
  grid_import: number;
  grid_export: number;
  battery_soc: number;
  profit: number;
  // Forecast data used for this decision
  import_price: number;
  export_price: number;
  solar_forecast: number;
  load_forecast: number;
  cloud_coverage: number;
  weather_symbol: string;
  battery_avg_cell_temp: number;
  air_temperature: number;
}

export interface SchedulerStatus {
  is_running: boolean;
  miners_count: number;
  has_market_data: boolean;
  price_limit: number;
  network: string;
  mpc_decisions?: MPCDecisionInfo[];
}

export interface HealthResponse {
  status: string;
  timestamp: string;
  version: string;
  scheduler: SchedulerStatus;
  system: {
    uptime: string;
    goroutines: number;
  };
  ems: {
    current_pv_power: number;
    ess_power: number;
    ess_soc: number;
    grid_sensor_status: number;
    grid_sensor_active_power: number;
    plant_active_power: number;
    dc_charger_output_power: number;
    dc_charger_vehicle_soc: number;
  };
  sun: {
    solar_angle: number;
    sunrise: string;
    sunset: string;
  };
}

export interface StatusResponse {
  scheduler_status: {
    is_running: boolean;
    miners_count: number;
    has_market_data: boolean;
  };
  miners: {
    count: number;
    list: Array<{
      ip: string;
      status: string;
      dna?: string;
      fan_r?: number;
      filter_usage?: number;
    }>;
  };
  price_data: {
    has_document: boolean;
    current_avg_price?: number;
    current?: number;
    limit?: number;
  };
  timestamp: string;
}

export interface MetricsSummary {
  total_import_cost: number;
  total_export_cost: number;
  total_import_kwh: number;
  total_export_kwh: number;
  start_time: string;
  end_time: string;
}

export interface WebSocketMessage {
  type: string;
  health: HealthResponse;
  status: StatusResponse;
}
