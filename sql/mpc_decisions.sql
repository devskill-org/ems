CREATE TABLE mpc_decisions (
    timestamp BIGINT PRIMARY KEY,
    hour INTEGER NOT NULL,
    battery_charge NUMERIC NOT NULL,
    battery_charge_from_pv NUMERIC NOT NULL DEFAULT 0,
    battery_charge_from_grid NUMERIC NOT NULL DEFAULT 0,
    battery_discharge NUMERIC NOT NULL,
    grid_import NUMERIC NOT NULL,
    grid_export NUMERIC NOT NULL,
    battery_soc NUMERIC NOT NULL,
    profit NUMERIC NOT NULL,
    import_price NUMERIC NOT NULL,
    export_price NUMERIC NOT NULL,
    solar_forecast NUMERIC NOT NULL,
    load_forecast NUMERIC NOT NULL,
    cloud_coverage NUMERIC,
    weather_symbol VARCHAR(100),
    battery_avg_cell_temp NUMERIC,
    air_temperature NUMERIC,
    battery_preheat_active BOOLEAN
);

CREATE INDEX idx_mpc_decisions_ts ON mpc_decisions USING BRIN (timestamp);

-- Column descriptions:
-- timestamp: Unix timestamp when this time slot begins (PRIMARY KEY)
-- hour: Time slot index in the optimization horizon (0-based), represents periods based on check_price_interval (e.g., 15-minute intervals)
-- battery_charge: Battery charging power in kW (positive = charging) - DEPRECATED: use battery_charge_from_pv + battery_charge_from_grid
-- battery_charge_from_pv: Battery charging power from PV surplus in kW
-- battery_charge_from_grid: Battery charging power from grid in kW (zero solar scenario)
-- battery_discharge: Battery discharging power in kW (positive = discharging)
-- grid_import: Grid import power in kW (positive = importing)
-- grid_export: Grid export power in kW (positive = exporting)
-- battery_soc: Battery state of charge as fraction (0-1)
-- profit: Expected profit for this time period in EUR
-- import_price: Electricity import price in EUR/kWh (exact price for this time slot, not averaged)
-- export_price: Electricity export price in EUR/kWh (exact price for this time slot, not averaged)
-- solar_forecast: Forecasted solar generation in kW (average for the time period)
-- load_forecast: Forecasted load consumption in kW (average for the time period)
-- cloud_coverage: Cloud coverage percentage (0-100)
-- weather_symbol: Weather condition symbol code
-- battery_avg_cell_temp: Forecasted battery average cell temperature in °C for this time slot
-- air_temperature: Forecasted air temperature in °C for this time slot
-- battery_preheat_active: Whether battery preheating is active during this time slot (true when charging below threshold temperature)
