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
