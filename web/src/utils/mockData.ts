import {
  HealthResponse,
  StatusResponse,
  MPCDecisionInfo,
  WebSocketMessage,
} from "../types/api";

/**
 * Generates realistic mock data for the Energy Management System
 */

// Helper function to get current hour
function getCurrentHour(): number {
  return new Date().getHours();
}

// Helper function to generate timestamps (returns unix timestamp in seconds)
function getTimestamp(minutesOffset: number = 0): number {
  const now = new Date();
  now.setMinutes(now.getMinutes() + minutesOffset);
  now.setSeconds(0);
  now.setMilliseconds(0);
  return Math.floor(now.getTime() / 1000);
}

// Generate realistic solar power based on time of day (0-100 kW)
function generateSolarPower(hour: number): number {
  if (hour < 6 || hour > 20) return 0;
  if (hour < 8) return Math.random() * 15 + 5; // 5-20 kW
  if (hour < 12) return Math.random() * 30 + 60; // 60-90 kW
  if (hour < 16) return Math.random() * 20 + 70; // 70-90 kW
  if (hour < 18) return Math.random() * 30 + 30; // 30-60 kW
  return Math.random() * 10 + 5; // 5-15 kW
}

// Generate realistic battery state
function generateBatteryState(hour: number) {
  const baseSOC = hour < 12 ? 0.3 + hour * 0.03 : 0.9 - (hour - 12) * 0.02;
  const soc = Math.max(
    0.1,
    Math.min(0.95, baseSOC + Math.random() * 0.1 - 0.05),
  );

  // Charging during day, discharging during evening (0-20 kW range)
  let power = 0;
  if (hour >= 9 && hour < 15) {
    power = Math.random() * 10 + 5; // Charging: 5-15 kW
  } else if (hour >= 18 && hour < 23) {
    power = -(Math.random() * 15 + 5); // Discharging: -5 to -20 kW
  } else {
    power = Math.random() * 4 - 2; // Idle/small fluctuations: -2 to 2 kW
  }

  return { soc, power };
}

// Generate realistic grid power (0-100 kW range)
function generateGridPower(hour: number): number {
  if (hour >= 10 && hour < 16) {
    // Exporting to grid during peak solar
    return -(Math.random() * 50 + 20); // -20 to -70 kW
  } else if (hour >= 18 && hour < 22) {
    // Importing during evening peak
    return Math.random() * 40 + 40; // 40-80 kW
  }
  return Math.random() * 20 - 10; // -10 to 10 kW
}

// Generate realistic plant active power (consumption) (0-10 kW range)
function generatePlantPower(hour: number): number {
  if (hour >= 8 && hour < 18) {
    // Higher consumption during work hours
    return Math.random() * 3 + 6; // 6-9 kW
  } else if (hour >= 18 && hour < 23) {
    // Evening peak
    return Math.random() * 2 + 7; // 7-9 kW
  }
  // Night time lower consumption
  return Math.random() * 2 + 2; // 2-4 kW
}

// Generate EV charger state (0-30 kW range)
function generateEVChargerState(hour: number) {
  const isCharging = hour >= 10 && hour < 16 && Math.random() > 0.3;
  const vehicleSOC = isCharging
    ? Math.min(0.95, 0.5 + (hour - 10) * 0.07)
    : Math.random() * 0.3 + 0.2;
  const power = isCharging ? Math.random() * 15 + 10 : 0; // 10-25 kW when charging

  return { power, soc: vehicleSOC };
}

// Generate electricity prices (€/kWh) with variations per 15-minute interval
function generatePrice(hour: number, minute: number): number {
  // Generate base price for the hour if not cached
  if (cachedPriceByHour[hour] === undefined) {
    // Generate base price based on time of day (0.10 to 1 EUR per kWh)
    let basePrice: number;
    if (hour >= 7 && hour < 9) {
      basePrice = Math.random() * 0.3 + 0.6; // Morning peak: 0.60-0.90 €/kWh
    } else if (hour >= 18 && hour < 21) {
      basePrice = Math.random() * 0.4 + 0.6; // Evening peak: 0.60-1.00 €/kWh
    } else if (hour >= 2 && hour < 6) {
      basePrice = Math.random() * 0.2 + 0.1; // Night low: 0.10-0.30 €/kWh
    } else {
      basePrice = Math.random() * 0.3 + 0.3; // Normal: 0.30-0.60 €/kWh
    }

    // Cache the base price for this hour
    cachedPriceByHour[hour] = basePrice;
  }

  const basePrice = cachedPriceByHour[hour];

  // Add random variation based on 15-minute interval (±10% of base price)
  // Use minute value and hour to seed random variation
  const intervalIndex = Math.floor(minute / 15); // 0, 1, 2, or 3
  const seed = (hour * 7 + intervalIndex * 17 + minute) * 9301 + 49297;
  const pseudoRandom = ((seed * seed) % 233280) / 233280; // Value between 0 and 1
  const variation = (pseudoRandom - 0.5) * 0.2; // ±10%

  return basePrice * (1 + variation);
}

// Generate weather symbol codes
function getWeatherSymbol(hour: number): string {
  const symbols = [
    "clearsky_day",
    "clearsky_night",
    "fair_day",
    "fair_night",
    "partlycloudy_day",
    "partlycloudy_night",
    "cloudy",
    "lightrain",
  ];

  if (hour < 6 || hour > 20) {
    return Math.random() > 0.5 ? "clearsky_night" : "fair_night";
  }
  return symbols[Math.floor(Math.random() * 5)];
}

// Cache for MPC decisions (regenerate every 15 minutes)
let cachedMPCDecisions: MPCDecisionInfo[] | null = null;
let mpcCacheTimestamp: number = 0;
const MPC_CACHE_DURATION = 15 * 60 * 1000; // 15 minutes in milliseconds

// Cache for miners data (regenerate every hour)
let cachedMiners: Array<{ ip: string; status: string }> | null = null;
let minersCacheTimestamp: number = 0;
const MINERS_CACHE_DURATION = 60 * 60 * 1000; // 1 hour in milliseconds

// Cache for electricity prices (base price per hour)
const cachedPriceByHour: { [hour: number]: number } = {};

// Generate MPC decisions for the next 24 hours in 15-minute intervals (96 decisions)
function generateMPCDecisions(): MPCDecisionInfo[] {
  // Return cached decisions if still valid
  const now = Date.now();
  if (cachedMPCDecisions && now - mpcCacheTimestamp < MPC_CACHE_DURATION) {
    return cachedMPCDecisions;
  }

  // Generate fresh decisions
  const decisions: MPCDecisionInfo[] = [];
  const currentTime = new Date();
  const currentMinute = currentTime.getMinutes();

  // Simulate battery temperature (typically 20-30°C for normal operation)
  const baseBatteryTemp = 22 + Math.random() * 6; // 22-28°C range

  // Simulate base air temperature (varies by time of day)
  const baseAirTemp = 10 + Math.random() * 5; // 10-15°C base range

  // Calculate minutes to next 15-minute boundary (0, 15, 30, or 45)
  const nextBoundary = Math.ceil(currentMinute / 15) * 15;
  const minutesToNextBoundary = nextBoundary - currentMinute;

  // Start with current realistic battery SOC (as decimal 0-1)
  let currentSOC = Math.max(0.2, Math.min(0.8, 0.4 + Math.random() * 0.2));

  // Generate 96 decisions (24 hours * 4 intervals per hour = 96 intervals)
  for (let i = 0; i < 96; i++) {
    const minutesOffset = minutesToNextBoundary + i * 15;
    const totalMinutes = currentMinute + minutesOffset;
    const hour = Math.floor(totalMinutes / 60) % 24;
    const minute = totalMinutes % 60;
    const timestamp = getTimestamp(minutesOffset);

    // Interpolate solar power within the hour (more granular)
    const solarForecast =
      generateSolarPower(hour) *
      (1 + (Math.sin((minute / 60) * Math.PI) * 0.15 - 0.075)); // ±7.5% variation within hour
    const importPrice = generatePrice(hour, minute);
    const exportPrice = importPrice * 0.7; // Export typically lower than import
    const loadForecast =
      generatePlantPower(hour) * (1 + Math.random() * 0.1 - 0.05); // ±5% variation
    const cloudCoverage = Math.random() * 100;

    // Battery strategy based on prices and solar (adjusted for 15-min intervals)
    let batteryCharge = 0;
    let batteryDischarge = 0;

    if (solarForecast > 50 && importPrice < 0.6 && currentSOC < 0.9) {
      // Charge battery during cheap hours with solar
      batteryCharge = Math.random() * 2 + 1;
      currentSOC = Math.min(0.95, currentSOC + batteryCharge * 0.005); // Adjusted for 15-min interval
    } else if (importPrice > 0.8 && currentSOC > 0.2) {
      // Discharge during expensive hours
      batteryDischarge = Math.random() * 2 + 0.5;
      currentSOC = Math.max(0.1, currentSOC - batteryDischarge * 0.005); // Adjusted for 15-min interval
    } else {
      // Small fluctuations for idle state
      currentSOC = Math.max(
        0.1,
        Math.min(0.95, currentSOC + Math.random() * 0.005 - 0.0025),
      );
    }

    // Split battery charge into PV and grid portions
    const pvSurplus = Math.max(0, solarForecast - loadForecast);
    const batteryChargeFromPV = Math.min(pvSurplus, batteryCharge);

    // Simulate grid-only profitable charging (cheap price periods independent of solar)
    let batteryChargeFromGrid = 0;
    if (importPrice < 0.25 && currentSOC < 0.85) {
      // Very cheap grid hours - aggressively charge from grid
      batteryChargeFromGrid = Math.random() * 2 + 1; // 1-3 kW
    } else if (importPrice < 0.35 && currentSOC < 0.75) {
      // Moderately cheap - some grid charging
      batteryChargeFromGrid = Math.random() * 1 + 0.5; // 0.5-1.5 kW
    }

    // Grid strategy - only import OR export, never both
    let gridImport = 0;
    let gridExport = 0;

    // Determine net power flow: positive = surplus (export), negative = deficit (import)
    const netPower =
      solarForecast - loadForecast + batteryDischarge - batteryCharge;

    if (netPower > 0 && exportPrice > 0.05) {
      // Surplus power and reasonable export price - export to grid
      gridExport = Math.min(netPower, Math.random() * 40 + 20); // 20-60 kW max
    } else if (netPower < 0) {
      // Power deficit - import from grid
      gridImport = Math.abs(netPower) + Math.random() * 10; // Cover deficit + some margin
    }

    // Account for grid charging draw (mirrors the backend Optimize() fix):
    // absorb into reduced export first, then raise import for any remainder
    if (batteryChargeFromGrid > 0) {
      const batteryEfficiency = 0.95;
      const additionalGridDraw = batteryChargeFromGrid / batteryEfficiency;
      if (gridExport >= additionalGridDraw) {
        gridExport -= additionalGridDraw;
      } else {
        gridImport += additionalGridDraw - gridExport;
        gridExport = 0;
      }
    }

    const profit = (gridExport * exportPrice - gridImport * importPrice) / 4000; // Divided by 4 for 15-min interval

    decisions.push({
      hour: hour + minute / 60, // Fractional hour (e.g., 10.25 for 10:15)
      timestamp,
      battery_charge: batteryCharge,
      battery_charge_from_pv: batteryChargeFromPV,
      battery_charge_from_grid: batteryChargeFromGrid,
      battery_discharge: batteryDischarge,
      grid_import: gridImport,
      grid_export: gridExport,
      battery_soc: Math.max(0, Math.min(1, currentSOC)),
      profit,
      import_price: importPrice,
      export_price: exportPrice,
      solar_forecast: solarForecast,
      load_forecast: loadForecast,
      cloud_coverage: cloudCoverage,
      weather_symbol: getWeatherSymbol(hour),
      battery_avg_cell_temp: baseBatteryTemp + (Math.random() * 2 - 1), // Small variation around base temp
      air_temperature:
        baseAirTemp + (hour - 12) * 0.5 + (Math.random() * 2 - 1), // Warmer in afternoon, cooler at night
    });
  }

  // Cache the new decisions
  cachedMPCDecisions = decisions;
  mpcCacheTimestamp = now;

  return decisions;
}

// Generate mock miner data
function generateMiners() {
  // Return cached miners if still valid
  const now = Date.now();
  if (cachedMiners && now - minersCacheTimestamp < MINERS_CACHE_DURATION) {
    return cachedMiners;
  }

  // Generate fresh miners data
  const minerCount = Math.floor(Math.random() * 3) + 2; // 2-4 miners
  const miners = [];
  const statuses = ["RUNNING", "STOPPED", "IDLE"];

  for (let i = 0; i < minerCount; i++) {
    miners.push({
      ip: `192.168.1.${100 + i}`,
      status: statuses[Math.floor(Math.random() * statuses.length)],
    });
  }

  // Cache the new miners data
  cachedMiners = miners;
  minersCacheTimestamp = now;

  return miners;
}

// Calculate sunrise and sunset times
function getSunTimes() {
  const now = new Date();
  const sunrise = new Date(now);
  sunrise.setHours(6, 30 + Math.floor(Math.random() * 30), 0, 0);

  const sunset = new Date(now);
  sunset.setHours(18, 30 + Math.floor(Math.random() * 60), 0, 0);

  return {
    sunrise: sunrise.toISOString(),
    sunset: sunset.toISOString(),
  };
}

// Calculate solar angle (simplified)
function getSolarAngle(): number {
  const hour = getCurrentHour();
  if (hour < 6 || hour > 20) return 0;

  // Peak at noon
  const minutesFromNoon = Math.abs((hour - 12) * 60);
  const maxAngle = 65;
  return Math.max(0, maxAngle - (minutesFromNoon / 360) * maxAngle);
}

/**
 * Generate a complete mock WebSocket message
 */
export function generateMockWebSocketMessage(): WebSocketMessage {
  const now = new Date();
  const hour = now.getHours();
  const minute = now.getMinutes();
  const battery = generateBatteryState(hour);
  const evCharger = generateEVChargerState(hour);
  const sunTimes = getSunTimes();
  const miners = generateMiners();
  const currentPrice = generatePrice(hour, minute);
  const priceLimit = 75;

  const health: HealthResponse = {
    status: "healthy",
    timestamp: new Date().toISOString(),
    version: "1.0.0-demo",
    scheduler: {
      is_running: true,
      miners_count: miners.length,
      has_market_data: true,
      price_limit: priceLimit,
      network: "Demo Network",
      mpc_decisions: generateMPCDecisions(),
    },
    system: {
      uptime: "5d 12h 34m",
      goroutines: 42,
    },
    ems: {
      current_pv_power: generateSolarPower(hour),
      ess_power: battery.power,
      ess_soc: battery.soc,
      grid_sensor_status: 1,
      grid_sensor_active_power: generateGridPower(hour),
      plant_active_power: generatePlantPower(hour),
      dc_charger_output_power: evCharger.power,
      dc_charger_vehicle_soc: evCharger.soc,
    },
    sun: {
      solar_angle: getSolarAngle(),
      sunrise: sunTimes.sunrise,
      sunset: sunTimes.sunset,
    },
  };

  const status: StatusResponse = {
    scheduler_status: {
      is_running: true,
      miners_count: miners.length,
      has_market_data: true,
    },
    miners: {
      count: miners.length,
      list: miners,
    },
    price_data: {
      has_document: true,
      current_avg_price: currentPrice,
      current: currentPrice,
      limit: priceLimit,
    },
    timestamp: new Date().toISOString(),
  };

  return {
    type: "status_update",
    health,
    status,
  };
}

/**
 * Create a mock WebSocket-like interface that emits periodic updates
 */
export function createMockWebSocket(
  onMessage: (data: WebSocketMessage) => void,
  interval: number = 10000,
): { close: () => void } {
  // Send initial message immediately
  setTimeout(() => {
    onMessage(generateMockWebSocketMessage());
  }, 100);

  // Set up periodic updates
  const intervalId = setInterval(() => {
    onMessage(generateMockWebSocketMessage());
  }, interval);

  return {
    close: () => clearInterval(intervalId),
  };
}
