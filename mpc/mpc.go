// Package mpc provides Model Predictive Control optimization for energy management systems.
package mpc

import (
	"math"
)

// SystemConfig holds the inverter system configuration
type SystemConfig struct {
	BatteryCapacity             float64 // kWh
	BatteryMaxCharge            float64 // kW
	BatteryMaxDischarge         float64 // kW
	BatteryMinSOC               float64 // percentage (0-1)
	BatteryMaxSOC               float64 // percentage (0-1)
	BatteryEfficiency           float64 // round-trip efficiency (0-1)
	BatteryDegradationCost      float64 // $/kWh cycled
	MaxGridImport               float64 // kW
	MaxGridExport               float64 // kW
	BatteryPreHeatPower         float64 // kW - power consumption of battery preheating when active
	BatteryPreHeatTempThreshold float64 // °C - temperature threshold below which battery preheating activates
	BatteryThermalTimeConstant  float64 // fraction per hour - rate at which battery temperature approaches air temperature (0-1). This is automatically scaled based on TimeSlotDuration.
	TimeSlotDuration            float64 // hours - duration of each time slot (e.g., 0.25 for 15 minutes, 1.0 for 1 hour). MUST match CheckPriceInterval configuration.
}

// TimeSlot represents one time period of operation (typically 15 minutes, configurable via check_price_interval)
type TimeSlot struct {
	Hour           int
	Timestamp      int64   // Unix timestamp when this time slot begins
	ImportPrice    float64 // $/kWh
	ExportPrice    float64 // $/kWh
	SolarForecast  float64 // kW average for the time period
	LoadForecast   float64 // kW average for the time period
	CloudCoverage  float64 // % cloud coverage (0-100)
	WeatherSymbol  string  // weather condition symbol
	AirTemperature float64 // °C air temperature
}

// ControlDecision represents the optimal control for one time slot (typically 15 minutes, configurable via check_price_interval)
type ControlDecision struct {
	Hour                  int
	Timestamp             int64   // Unix timestamp when this time slot begins
	BatteryCharge         float64 // kW (positive = charging) - DEPRECATED: use BatteryChargeFromPV + BatteryChargeFromGrid
	BatteryChargeFromPV   float64 // kW (positive = charging from PV surplus)
	BatteryChargeFromGrid float64 // kW (positive = charging from grid)
	BatteryDischarge      float64 // kW (positive = discharging)
	GridImport            float64 // kW (positive = importing)
	GridExport            float64 // kW (positive = exporting)
	BatterySOC            float64 // percentage (0-1)
	Profit                float64 // $ for this time period
	BatteryPreHeatActive  bool    // true if battery preheating is active during this time slot
	// Forecast data used for this decision
	ImportPrice        float64 // $/kWh
	ExportPrice        float64 // $/kWh
	SolarForecast      float64 // kW average for the time period
	LoadForecast       float64 // kW average for the time period
	CloudCoverage      float64 // % cloud coverage (0-100)
	WeatherSymbol      string  // weather condition symbol
	BatteryAvgCellTemp float64 // °C average cell temperature
	AirTemperature     float64 // °C air temperature
}

// Controller implements Model Predictive Control
type Controller struct {
	Config             SystemConfig
	Horizon            int // number of time periods to look ahead
	CurrentSOC         float64
	CurrentBatteryTemp float64 // °C current battery temperature
}

// NewController creates a new MPC controller
func NewController(config SystemConfig, horizon int, initialSOC float64) *Controller {
	return &Controller{
		Config:             config,
		Horizon:            horizon,
		CurrentSOC:         initialSOC,
		CurrentBatteryTemp: 20.0, // Default to room temperature
	}
}

// Optimize finds the optimal control strategy using dynamic programming
// It runs two optimizations: one with solar forecast and one without (grid-only)
// Then splits the BatteryCharge into BatteryChargeFromPV and BatteryChargeFromGrid
func (mpc *Controller) Optimize(forecast []TimeSlot) []ControlDecision {
	if len(forecast) == 0 {
		return nil
	}

	// Run optimization with full solar forecast
	decisionsWithSolar := mpc.optimizeWithForecast(forecast, true)

	// Run optimization without solar (grid-only scenario)
	decisionsWithoutSolar := mpc.optimizeWithForecast(forecast, false)

	// Combine results: split BatteryCharge into PV and Grid components
	finalDecisions := make([]ControlDecision, len(decisionsWithSolar))
	for i := range decisionsWithSolar {
		finalDecisions[i] = decisionsWithSolar[i]

		// BatteryChargeFromPV is the charge power when solar is available
		finalDecisions[i].BatteryChargeFromPV = decisionsWithSolar[i].BatteryCharge

		// BatteryChargeFromGrid is the charge power when solar is zero
		finalDecisions[i].BatteryChargeFromGrid = decisionsWithoutSolar[i].BatteryCharge

		// Keep total BatteryCharge for backward compatibility
		finalDecisions[i].BatteryCharge = decisionsWithSolar[i].BatteryCharge
	}

	return finalDecisions
}

// optimizeWithForecast performs the actual optimization with optional solar forecast
func (mpc *Controller) optimizeWithForecast(forecast []TimeSlot, includeSolar bool) []ControlDecision {
	// Use dynamic programming for optimization
	// State: SOC level, Time: hour
	// We'll discretize SOC into steps for tractability
	// Use finer granularity for better precision in finding optimal discharge timing
	socSteps := 500
	socStep := (mpc.Config.BatteryMaxSOC - mpc.Config.BatteryMinSOC) / float64(socSteps)

	// DP table: [time][soc_index] -> (best_profit, best_decision, battery_temp)
	type dpState struct {
		profit      float64
		decision    ControlDecision
		prevSOC     int
		batteryTemp float64 // °C battery temperature at this state
	}

	dp := make([][]dpState, len(forecast)+1)
	for i := range dp {
		dp[i] = make([]dpState, socSteps+1)
		for j := range dp[i] {
			dp[i][j].profit = math.Inf(-1)
		}
	}

	// Initialize with current SOC and battery temperature
	startSOCIndex := mpc.socToIndex(mpc.CurrentSOC, socStep)
	dp[0][startSOCIndex].profit = 0
	dp[0][startSOCIndex].batteryTemp = mpc.CurrentBatteryTemp

	// Forward pass - build DP table
	for t := range forecast {
		slot := forecast[t]
		if !includeSolar {
			slot.SolarForecast = 0
		}

		for socIdx := 0; socIdx <= socSteps; socIdx++ {
			if math.IsInf(dp[t][socIdx].profit, -1) {
				continue
			}

			currentSOC := mpc.indexToSOC(socIdx, socStep)
			currentBatteryTemp := dp[t][socIdx].batteryTemp

			// Try different control decisions
			decisions := mpc.generateFeasibleDecisions(currentSOC, currentBatteryTemp, slot)

			for _, dec := range decisions {
				newSOC := mpc.calculateNewSOC(currentSOC, dec.BatteryCharge, dec.BatteryDischarge)
				newSOCIdx := mpc.socToIndex(newSOC, socStep)

				if newSOCIdx < 0 || newSOCIdx > socSteps {
					continue
				}

				// Calculate next battery temperature based on this decision
				newBatteryTemp := mpc.calculateNextBatteryTemp(currentBatteryTemp, slot.AirTemperature, dec.BatteryCharge > 0, dec.BatteryPreHeatActive)

				profit := mpc.calculateProfit(dec, slot)
				totalProfit := dp[t][socIdx].profit + profit

				if totalProfit > dp[t+1][newSOCIdx].profit {
					dp[t+1][newSOCIdx].profit = totalProfit
					dp[t+1][newSOCIdx].decision = dec
					dp[t+1][newSOCIdx].decision.BatterySOC = newSOC
					dp[t+1][newSOCIdx].decision.Profit = profit
					dp[t+1][newSOCIdx].decision.Timestamp = slot.Timestamp
					dp[t+1][newSOCIdx].decision.ImportPrice = slot.ImportPrice
					dp[t+1][newSOCIdx].decision.ExportPrice = slot.ExportPrice
					dp[t+1][newSOCIdx].decision.SolarForecast = slot.SolarForecast
					dp[t+1][newSOCIdx].decision.LoadForecast = slot.LoadForecast
					dp[t+1][newSOCIdx].decision.CloudCoverage = slot.CloudCoverage
					dp[t+1][newSOCIdx].decision.WeatherSymbol = slot.WeatherSymbol
					dp[t+1][newSOCIdx].decision.AirTemperature = slot.AirTemperature
					dp[t+1][newSOCIdx].decision.BatteryAvgCellTemp = currentBatteryTemp
					dp[t+1][newSOCIdx].prevSOC = socIdx
					dp[t+1][newSOCIdx].batteryTemp = newBatteryTemp
				}
			}
		}
	}

	// Backward pass - reconstruct optimal path
	// Prefer paths that end with lower SOC (use more battery for arbitrage)
	bestFinalSOC := 0
	bestFinalProfit := math.Inf(-1)
	for socIdx := 0; socIdx <= socSteps; socIdx++ {
		if dp[len(forecast)][socIdx].profit > bestFinalProfit {
			bestFinalProfit = dp[len(forecast)][socIdx].profit
			bestFinalSOC = socIdx
		}
	}

	// Trace back the path
	path := make([]ControlDecision, len(forecast))
	currentIdx := bestFinalSOC
	for t := len(forecast) - 1; t >= 0; t-- {
		path[t] = dp[t+1][currentIdx].decision
		currentIdx = dp[t+1][currentIdx].prevSOC
	}

	return path
}

// calculateNextBatteryTemp calculates the battery temperature for the next time slot
// based on current temperature, air temperature, and whether the battery is charging
func (mpc *Controller) calculateNextBatteryTemp(currentTemp, airTemp float64, isCharging, isPreHeating bool) float64 {
	if isCharging && isPreHeating {
		// When charging with preheat, battery maintains temperature at threshold
		return math.Max(currentTemp, mpc.Config.BatteryPreHeatTempThreshold)
	}

	// Get time slot duration (default to 1 hour if not specified for backward compatibility)
	timeSlotDuration := mpc.Config.TimeSlotDuration
	if timeSlotDuration == 0 {
		timeSlotDuration = 1.0
	}

	// Scale thermal time constant by time slot duration
	// BatteryThermalTimeConstant is defined per hour, so we scale it for the actual slot duration
	// For small k: k_slot ≈ k_hour * slot_duration_hours
	// For larger k, use exponential formula: k_slot = 1 - (1 - k_hour)^slot_duration_hours
	thermalConstant := mpc.Config.BatteryThermalTimeConstant
	if thermalConstant < 0.2 {
		// Use linear approximation for small values (more efficient)
		thermalConstant = thermalConstant * timeSlotDuration
	} else {
		// Use exponential formula for larger values (more accurate)
		thermalConstant = 1.0 - math.Pow(1.0-thermalConstant, timeSlotDuration)
	}

	// When not charging or warm enough, battery temperature moves toward air temperature
	// T(t+1) = T(t) + k * (T_air - T(t))
	// This models natural cooling/heating toward ambient air temperature
	tempDiff := airTemp - currentTemp
	return currentTemp + thermalConstant*tempDiff
}

// generateFeasibleDecisions creates a set of feasible control decisions
func (mpc *Controller) generateFeasibleDecisions(currentSOC float64, currentBatteryTemp float64, slot TimeSlot) []ControlDecision {
	decisions := []ControlDecision{}

	// Determine if battery preheating would be needed based on battery temperature
	// Battery preheating is only active when actually charging the battery
	needsPreHeat := mpc.Config.BatteryPreHeatPower > 0 && currentBatteryTemp < mpc.Config.BatteryPreHeatTempThreshold
	preHeatPower := 0.0
	if needsPreHeat {
		preHeatPower = mpc.Config.BatteryPreHeatPower
	}

	// Always include idle option
	batteryActions := []struct {
		charge    float64
		discharge float64
	}{
		{0, 0}, // Idle
	}

	// For better arbitrage, focus on key power levels:
	// 1. Maximum power (for concentrated operations)
	// 2. A few intermediate levels (for flexibility)
	// 3. Minimum meaningful power (for fine adjustments)

	granularity := 60

	// Charge options - use finer granularity for better optimization
	for i := granularity; i > 0; i-- {
		charge := float64(i) * mpc.Config.BatteryMaxCharge / float64(granularity)
		if mpc.canCharge(currentSOC, charge) {
			batteryActions = append(batteryActions, struct {
				charge    float64
				discharge float64
			}{charge, 0})
		}
	}

	// Discharge options - use finer granularity for better optimization
	for i := granularity; i > 0; i-- {
		discharge := float64(i) * mpc.Config.BatteryMaxDischarge / float64(granularity)
		if mpc.canDischarge(currentSOC, discharge) {
			batteryActions = append(batteryActions, struct {
				charge    float64
				discharge float64
			}{0, discharge})
		}
	}

	// For each battery action, calculate power balance
	for _, action := range batteryActions {
		// Battery preheating is only active when we're actually charging and temp is below threshold
		preHeatActive := needsPreHeat && action.charge > 0

		dec := ControlDecision{
			Hour:                 slot.Hour,
			Timestamp:            slot.Timestamp,
			BatteryCharge:        action.charge,
			BatteryDischarge:     action.discharge,
			BatteryPreHeatActive: preHeatActive,
		}

		// Power balance: Solar + GridImport + BatteryDischarge = Load + GridExport + BatteryCharge + BatteryPreHeat
		// When battery preheating is active (battery is charging at low temp), it consumes extra power from the grid
		netSolar := slot.SolarForecast
		extraLoad := 0.0

		// Battery preheating only consumes power when battery is charging
		if preHeatActive {
			extraLoad = preHeatPower
		}

		netLoad := slot.LoadForecast + action.charge/mpc.Config.BatteryEfficiency + extraLoad
		netSupply := netSolar + action.discharge*mpc.Config.BatteryEfficiency

		balance := netSupply - netLoad

		if balance > 0 {
			// Excess power - can only export when the export price is positive.
			// When export price is negative or zero, skip any decision that would
			// result in grid export: battery discharge that exceeds local consumption
			// would waste stored energy (degradation cost) for zero or negative return.
			// Solar-only excess is also curtailed rather than exported at a loss.
			if slot.ExportPrice > 0 {
				dec.GridExport = math.Min(balance, mpc.Config.MaxGridExport)
				dec.GridImport = 0
			} else if action.discharge > 0 {
				// Discharging is causing the surplus — skip this decision entirely.
				// The battery should only discharge to cover load, not to export.
				continue
			} else {
				// Solar surplus with no export revenue — curtail, no grid interaction.
				dec.GridExport = 0
				dec.GridImport = 0
			}
		} else {
			// Deficit - need to import
			dec.GridImport = math.Min(-balance, mpc.Config.MaxGridImport)
			dec.GridExport = 0
		}

		// Check if decision is feasible
		if mpc.isFeasible(dec) {
			decisions = append(decisions, dec)
		}
	}

	return decisions
}

// calculateProfit computes the profit for a decision
// The power balance equation ensures: Solar + GridImport + BatteryDischarge*eff = Load + GridExport + BatteryCharge/eff + BatteryPreHeat
// Therefore, GridImport and GridExport already reflect the effect of battery operations and battery preheating.
// Profit is simply: revenue from exports - cost of imports - degradation cost
// Note: The battery preheating cost is already included in GridImport when battery is charging at low temperatures
// All power values (kW) are multiplied by time slot duration (hours) to get energy (kWh)
func (mpc *Controller) calculateProfit(dec ControlDecision, slot TimeSlot) float64 {
	// Get time slot duration (default to 1 hour if not specified for backward compatibility)
	timeSlotDuration := mpc.Config.TimeSlotDuration
	if timeSlotDuration == 0 {
		timeSlotDuration = 1.0
	}

	// Revenue from exporting to grid
	// GridExport is in kW, multiply by time slot duration to get kWh
	revenue := dec.GridExport * slot.ExportPrice * timeSlotDuration

	// Cost of importing from grid (already includes battery preheating consumption when active)
	// GridImport is in kW, multiply by time slot duration to get kWh
	importCost := dec.GridImport * slot.ImportPrice * timeSlotDuration

	// Battery degradation cost (wear and tear from cycling)
	// Throughput is in kW, multiply by time slot duration to get kWh cycled
	batteryThroughput := (dec.BatteryCharge + dec.BatteryDischarge) * timeSlotDuration
	degradationCost := batteryThroughput * mpc.Config.BatteryDegradationCost

	// Net profit:
	// + Revenue from exports (GridExport already accounts for battery discharge to grid)
	// - Cost of imports (GridImport already accounts for battery charging, battery preheating, and reduced imports from discharge)
	// - Battery degradation (wear and tear cost)
	//
	// This correctly incentivizes arbitrage:
	// - Charging at low import prices reduces profit by importCost
	// - Discharging at high export prices increases profit by revenue
	// - When battery temp is low (<10°C), charging incurs additional battery preheating cost (700W)
	// - The DP optimizer will naturally prefer charge-low/discharge-high strategies
	// - The optimizer will avoid charging at low temperatures unless prices are very favorable
	profit := revenue - importCost - degradationCost

	return profit
}

// Helper functions
func (mpc *Controller) canCharge(soc, charge float64) bool {
	// Get time slot duration (default to 1 hour if not specified for backward compatibility)
	timeSlotDuration := mpc.Config.TimeSlotDuration
	if timeSlotDuration == 0 {
		timeSlotDuration = 1.0
	}

	// Convert power (kW) to energy (kWh) by multiplying by time slot duration
	chargeEnergy := charge * timeSlotDuration
	newSOC := soc + (chargeEnergy / mpc.Config.BatteryCapacity)
	return newSOC <= mpc.Config.BatteryMaxSOC
}

func (mpc *Controller) canDischarge(soc, discharge float64) bool {
	// Get time slot duration (default to 1 hour if not specified for backward compatibility)
	timeSlotDuration := mpc.Config.TimeSlotDuration
	if timeSlotDuration == 0 {
		timeSlotDuration = 1.0
	}

	// Convert power (kW) to energy (kWh) by multiplying by time slot duration
	dischargeEnergy := discharge * timeSlotDuration
	newSOC := soc - (dischargeEnergy / mpc.Config.BatteryCapacity)
	return newSOC >= mpc.Config.BatteryMinSOC
}

func (mpc *Controller) calculateNewSOC(currentSOC, charge, discharge float64) float64 {
	// Get time slot duration (default to 1 hour if not specified for backward compatibility)
	timeSlotDuration := mpc.Config.TimeSlotDuration
	if timeSlotDuration == 0 {
		timeSlotDuration = 1.0
	}

	// Convert power (kW) to energy (kWh) by multiplying by time slot duration
	chargeEnergy := charge * timeSlotDuration * mpc.Config.BatteryEfficiency
	dischargeEnergy := discharge * timeSlotDuration
	socChange := (chargeEnergy - dischargeEnergy) / mpc.Config.BatteryCapacity
	newSOC := currentSOC + socChange
	return math.Max(mpc.Config.BatteryMinSOC, math.Min(mpc.Config.BatteryMaxSOC, newSOC))
}

func (mpc *Controller) socToIndex(soc float64, socStep float64) int {
	return int(math.Round((soc - mpc.Config.BatteryMinSOC) / socStep))
}

func (mpc *Controller) indexToSOC(index int, socStep float64) float64 {
	return mpc.Config.BatteryMinSOC + float64(index)*socStep
}

func (mpc *Controller) isFeasible(dec ControlDecision) bool {
	// Check all constraints are satisfied
	if dec.BatteryCharge > mpc.Config.BatteryMaxCharge {
		return false
	}
	if dec.BatteryDischarge > mpc.Config.BatteryMaxDischarge {
		return false
	}
	if dec.GridImport > mpc.Config.MaxGridImport {
		return false
	}
	if dec.GridExport > mpc.Config.MaxGridExport {
		return false
	}
	return true
}
