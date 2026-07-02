// Package mpc provides Model Predictive Control optimization for energy management systems.
package mpc

import (
	"math"
	"time"
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

	// Cell-balancing parameters (all default to zero = disabled for backward compatibility).

	// BatteryBalancingSOCThreshold is the SOC level above which the constant-voltage
	// (CV) phase begins.  Above this threshold more input energy is required per unit
	// of SOC increase because the cells are being actively balanced.
	// Example: 0.999 (99.9%).  0 disables the CV-phase modelling.
	BatteryBalancingSOCThreshold float64

	// BatteryBalancingEfficiencyFactor is multiplied with BatteryEfficiency whenever
	// the current SOC is at or above BatteryBalancingSOCThreshold.
	// Example: 0.3 means ~3× more input energy is needed for the last sliver of SOC.
	// 0 disables the CV-phase modelling.
	BatteryBalancingEfficiencyFactor float64

	// BatteryBalancingBonus is a one-time profit bonus (same currency unit as Profit)
	// awarded inside the DP optimisation when the battery first reaches BatteryMaxSOC
	// within the current horizon.  It incentivises daily cell-balancing without
	// forcing expensive grid imports – the optimizer will only claim the bonus when
	// the energy cost is lower than the bonus value.  0 disables the feature.
	BatteryBalancingBonus float64
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

	// LastBalancingTime is the Unix timestamp of the most recent time the battery
	// reached BatteryMaxSOC for cell-balancing.  0 means it has never happened.
	// This field must be updated externally (e.g. by the EMS main loop) whenever
	// BatterySOC is observed to reach BatteryMaxSOC.
	LastBalancingTime int64
}

// NewController creates a new MPC controller.
// If initialSOC is at or above 100%, LastBalancingTime is initialised to the
// current time so the optimizer knows balancing is already complete.
func NewController(config SystemConfig, horizon int, initialSOC float64) *Controller {
	c := &Controller{
		Config:             config,
		Horizon:            horizon,
		CurrentSOC:         initialSOC,
		CurrentBatteryTemp: 20.0, // Default to room temperature
	}
	if initialSOC >= 1.0 {
		c.LastBalancingTime = time.Now().Unix()
	}
	return c
}

// Optimize finds the optimal control strategy using dynamic programming
// It runs two optimizations: one with solar forecast and one without (grid-only)
// Then splits the BatteryCharge into BatteryChargeFromPV and BatteryChargeFromGrid
func (mpc *Controller) Optimize(forecast []TimeSlot) []ControlDecision {
	if len(forecast) == 0 {
		return nil
	}

	// Determine whether cell-balancing (charging to BatteryMaxSOC) should be
	// incentivised during this optimisation run – at most once per calendar week.
	needsBalancing := mpc.needsWeeklyBalancing(forecast)

	// Run optimization with full solar forecast
	decisionsWithSolar := mpc.optimizeWithForecast(forecast, true, needsBalancing)

	// Run optimization without solar (grid-only scenario).
	// This is used to determine how much grid charging is profitable regardless
	// of solar, so that we still charge from the grid when the solar forecast is
	// inaccurate (e.g. cloudy day with optimistic forecast).
	decisionsWithoutSolar := mpc.optimizeWithForecast(forecast, false, needsBalancing)

	// Combine results: split BatteryCharge into PV and Grid components.
	//
	// The solar-scenario decision already accounts for the full charge rate
	// (PV surplus first, topped up by grid when profitable). We derive the
	// PV portion as whatever charge the solar surplus can cover, and treat
	// the remainder as the grid portion.
	n := min(len(decisionsWithSolar), len(forecast))
	finalDecisions := make([]ControlDecision, n)
	for i, slot := range forecast[:n] {
		finalDecisions[i] = decisionsWithSolar[i]

		totalCharge := decisionsWithSolar[i].BatteryCharge

		// PV surplus available for charging after serving the load.
		// BatteryCharge/eff is the actual power drawn from the supply side to
		// push `totalCharge` kW into the battery.
		pvSurplus := math.Max(0, slot.SolarForecast-slot.LoadForecast)

		// The PV portion is how much of the charge is covered by PV surplus
		// (capped at the total charge being applied).
		pvPortion := math.Min(pvSurplus, totalCharge)

		// The grid portion is whatever the without-solar scenario recommends,
		// but never more than what remains of BatteryMaxCharge after the PV
		// portion is accounted for. This ensures pvPortion+gridPortion never
		// exceeds the hardware-rated maximum charge power, which would cause
		// the inverter to reject the register write with an illegal-data error.
		gridPortion := math.Min(decisionsWithoutSolar[i].BatteryCharge, mpc.Config.BatteryMaxCharge-pvPortion)
		gridPortion = math.Max(0, gridPortion)

		finalDecisions[i].BatteryChargeFromPV = pvPortion
		finalDecisions[i].BatteryChargeFromGrid = gridPortion

		// Keep total BatteryCharge for backward compatibility
		finalDecisions[i].BatteryCharge = totalCharge
	}

	return finalDecisions
}

// optimizeWithForecast performs the actual optimization with optional solar forecast
func (mpc *Controller) optimizeWithForecast(forecast []TimeSlot, includeSolar bool, needsBalancing bool) []ControlDecision {
	// Use dynamic programming for optimization.
	// State: (SOC level, balanced) where balanced tracks whether cell-balancing
	// (reaching BatteryMaxSOC for the first time) has already been claimed within
	// this horizon.  The extra boolean dimension ensures BatteryBalancingBonus is
	// counted exactly once per optimisation run, not every time 100% is touched.
	socSteps := 500
	socStep := (mpc.Config.BatteryMaxSOC - mpc.Config.BatteryMinSOC) / float64(socSteps)

	// DP table: [time][soc_index][balanced_state]
	//   balanced_state 0 = balancing bonus not yet claimed in this horizon
	//   balanced_state 1 = balancing bonus already claimed
	type dpState struct {
		profit       float64
		decision     ControlDecision
		prevSOC      int
		prevBalanced int     // 0 or 1 – balanced state of the predecessor step (for path tracing)
		batteryTemp  float64 // °C battery temperature at this state
	}

	dp := make([][][2]dpState, len(forecast)+1)
	for i := range dp {
		dp[i] = make([][2]dpState, socSteps+1)
		for j := range dp[i] {
			dp[i][j][0].profit = math.Inf(-1)
			dp[i][j][1].profit = math.Inf(-1)
		}
	}

	// Initialize with current SOC and battery temperature.
	// Clamp to the configured SOC range so that a real battery reading that
	// sits slightly outside [BatteryMinSOC, BatteryMaxSOC] (e.g. due to
	// inverter rounding or a recent limit change) does not produce a negative
	// index and cause a panic.
	// Always start in balanced-state 0; the bonus can be claimed during the run.
	clampedSOC := math.Max(mpc.Config.BatteryMinSOC, math.Min(mpc.Config.BatteryMaxSOC, mpc.CurrentSOC))
	startSOCIndex := mpc.socToIndex(clampedSOC, socStep)
	dp[0][startSOCIndex][0].profit = 0
	dp[0][startSOCIndex][0].batteryTemp = mpc.CurrentBatteryTemp

	// Forward pass - build DP table
	for t := range forecast {
		slot := forecast[t]
		if !includeSolar {
			slot.SolarForecast = 0
		}

		for socIdx := 0; socIdx <= socSteps; socIdx++ {
			for balState := 0; balState < 2; balState++ {
				if math.IsInf(dp[t][socIdx][balState].profit, -1) {
					continue
				}

				currentSOC := mpc.indexToSOC(socIdx, socStep)
				currentBatteryTemp := dp[t][socIdx][balState].batteryTemp

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

					// Award the one-time cell-balancing bonus the first time the
					// battery reaches BatteryMaxSOC during this horizon.
					// Transitioning from balState 0 → 1 prevents double-counting
					// even if the battery later discharges and charges back to 100%.
					newBalState := balState
					balancingBonus := 0.0
					if needsBalancing && balState == 0 && newSOC >= mpc.Config.BatteryMaxSOC {
						newBalState = 1
						balancingBonus = mpc.Config.BatteryBalancingBonus
					}

					totalProfit := dp[t][socIdx][balState].profit + profit + balancingBonus

					if totalProfit > dp[t+1][newSOCIdx][newBalState].profit {
						dp[t+1][newSOCIdx][newBalState].profit = totalProfit
						dp[t+1][newSOCIdx][newBalState].decision = dec
						dp[t+1][newSOCIdx][newBalState].decision.BatterySOC = newSOC
						dp[t+1][newSOCIdx][newBalState].decision.Profit = profit
						dp[t+1][newSOCIdx][newBalState].decision.Timestamp = slot.Timestamp
						dp[t+1][newSOCIdx][newBalState].decision.ImportPrice = slot.ImportPrice
						dp[t+1][newSOCIdx][newBalState].decision.ExportPrice = slot.ExportPrice
						dp[t+1][newSOCIdx][newBalState].decision.SolarForecast = slot.SolarForecast
						dp[t+1][newSOCIdx][newBalState].decision.LoadForecast = slot.LoadForecast
						dp[t+1][newSOCIdx][newBalState].decision.CloudCoverage = slot.CloudCoverage
						dp[t+1][newSOCIdx][newBalState].decision.WeatherSymbol = slot.WeatherSymbol
						dp[t+1][newSOCIdx][newBalState].decision.AirTemperature = slot.AirTemperature
						dp[t+1][newSOCIdx][newBalState].decision.BatteryAvgCellTemp = currentBatteryTemp
						dp[t+1][newSOCIdx][newBalState].prevSOC = socIdx
						dp[t+1][newSOCIdx][newBalState].prevBalanced = balState
						dp[t+1][newSOCIdx][newBalState].batteryTemp = newBatteryTemp
					}
				}
			}
		}
	}

	// Backward pass - find the best final state across all SOC levels and both
	// balanced states, then trace the path back to the start.
	bestFinalSOC := 0
	bestFinalBalState := 0
	bestFinalProfit := math.Inf(-1)
	for socIdx := 0; socIdx <= socSteps; socIdx++ {
		for balState := range 2 {
			if dp[len(forecast)][socIdx][balState].profit > bestFinalProfit {
				bestFinalProfit = dp[len(forecast)][socIdx][balState].profit
				bestFinalSOC = socIdx
				bestFinalBalState = balState
			}
		}
	}

	// Trace back the path
	path := make([]ControlDecision, len(forecast))
	currentIdx := bestFinalSOC
	currentBalState := bestFinalBalState
	for t := len(forecast) - 1; t >= 0; t-- {
		path[t] = dp[t+1][currentIdx][currentBalState].decision
		prevBalState := dp[t+1][currentIdx][currentBalState].prevBalanced
		currentIdx = dp[t+1][currentIdx][currentBalState].prevSOC
		currentBalState = prevBalState
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

	// Add a precise top-up action that charges exactly to BatteryMaxSOC.
	// The discrete steps above (multiples of BatteryMaxCharge/granularity) may be
	// too coarse to bridge the final gap to 100% — e.g. the smallest step can
	// overshoot, leaving the battery stranded at ~99.8%.  This dedicated option
	// guarantees the optimizer always has a path to exactly BatteryMaxSOC so that
	// cell balancing (which only starts at 100%) can activate.
	{
		timeSlotDuration := mpc.Config.TimeSlotDuration
		if timeSlotDuration == 0 {
			timeSlotDuration = 1.0
		}
		socGap := mpc.Config.BatteryMaxSOC - currentSOC
		if socGap > 1e-9 {
			// Use the same balancing-aware efficiency as calculateNewSOC so that
			// the top-up charge correctly accounts for the extra energy required
			// in the CV/balancing phase when the battery is nearly full.
			efficiency := mpc.Config.BatteryEfficiency
			if mpc.Config.BatteryBalancingSOCThreshold > 0 &&
				mpc.Config.BatteryBalancingEfficiencyFactor > 0 &&
				currentSOC >= mpc.Config.BatteryBalancingSOCThreshold {
				efficiency *= mpc.Config.BatteryBalancingEfficiencyFactor
			}
			// Invert calculateNewSOC: charge needed so that
			//   currentSOC + charge * duration * efficiency / capacity == BatteryMaxSOC
			topUpCharge := socGap * mpc.Config.BatteryCapacity / (efficiency * timeSlotDuration)
			if topUpCharge > 0 && topUpCharge <= mpc.Config.BatteryMaxCharge {
				batteryActions = append(batteryActions, struct {
					charge    float64
					discharge float64
				}{topUpCharge, 0})
			}
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
			// Excess power available after serving load and the current charge action.
			if action.discharge > 0 {
				// Discharging is causing (or contributing to) the surplus.
				// Only allow this if we can export at a positive price; otherwise
				// we would waste stored energy for zero or negative return.
				if slot.ExportPrice > 0 {
					dec.GridExport = math.Min(balance, mpc.Config.MaxGridExport)
					dec.GridImport = 0
				} else {
					// Skip: discharging into a non-positive export price is wasteful.
					continue
				}
			} else {
				// Solar is producing the surplus. Prefer absorbing it into the
				// battery before exporting. We already enumerated a charge level
				// (action.charge) for this iteration; the remaining surplus after
				// charging is what we consider exporting.
				//
				// Note: the surplus here already accounts for action.charge (it is
				// in netLoad), so `balance` is the PV power that still has nowhere
				// to go. Try to increase charging to absorb it — but only up to the
				// hardware maximum and SOC limits.
				extraCharge := math.Min(balance*mpc.Config.BatteryEfficiency, mpc.Config.BatteryMaxCharge-action.charge)
				if extraCharge > 0 && mpc.canCharge(currentSOC, action.charge+extraCharge) {
					// Absorb as much surplus as possible into the battery.
					dec.BatteryCharge += extraCharge
					// Recalculate balance after the extra charging.
					balance -= extraCharge / mpc.Config.BatteryEfficiency
				}

				if balance > 0.001 {
					// There is still remaining surplus after maxing out charging.
					if slot.ExportPrice > 0 {
						dec.GridExport = math.Min(balance, mpc.Config.MaxGridExport)
					} else {
						// Negative or zero export price — curtail the remainder.
						dec.GridExport = 0
					}
				}
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

	// Use the same balancing-aware efficiency as calculateNewSOC: in the CV/balancing
	// phase near 100% SOC the same charge power produces less SOC increase, so more
	// charge actions remain valid (they won't overshoot BatteryMaxSOC).
	efficiency := mpc.Config.BatteryEfficiency
	if mpc.Config.BatteryBalancingSOCThreshold > 0 &&
		mpc.Config.BatteryBalancingEfficiencyFactor > 0 &&
		soc >= mpc.Config.BatteryBalancingSOCThreshold {
		efficiency *= mpc.Config.BatteryBalancingEfficiencyFactor
	}

	// Convert power (kW) to energy (kWh) using the same formula as calculateNewSOC:
	// multiply by time slot duration AND efficiency so that both functions agree on
	// how much the SOC actually rises.
	chargeEnergy := charge * timeSlotDuration * efficiency
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

	// Apply reduced charging efficiency in the CV/balancing phase.
	// When currentSOC is at or above BatteryBalancingSOCThreshold the battery is in
	// constant-voltage mode: cells are being balanced and more input energy is needed
	// per unit of SOC increase.  The efficiency factor captures this extra cost so
	// that the optimizer correctly prices charging into the very top of the SOC range.
	efficiency := mpc.Config.BatteryEfficiency
	if charge > 0 &&
		mpc.Config.BatteryBalancingSOCThreshold > 0 &&
		mpc.Config.BatteryBalancingEfficiencyFactor > 0 &&
		currentSOC >= mpc.Config.BatteryBalancingSOCThreshold {
		efficiency *= mpc.Config.BatteryBalancingEfficiencyFactor
	}

	// Convert power (kW) to energy (kWh) by multiplying by time slot duration
	chargeEnergy := charge * timeSlotDuration * efficiency
	dischargeEnergy := discharge * timeSlotDuration
	socChange := (chargeEnergy - dischargeEnergy) / mpc.Config.BatteryCapacity
	newSOC := currentSOC + socChange
	return math.Max(mpc.Config.BatteryMinSOC, math.Min(mpc.Config.BatteryMaxSOC, newSOC))
}

func (mpc *Controller) socToIndex(soc float64, socStep float64) int {
	return int(math.Floor((soc - mpc.Config.BatteryMinSOC) / socStep))
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

// needsWeeklyBalancing returns true when cell-balancing (charging to BatteryMaxSOC)
// should be incentivised during this optimisation run.
//
// Balancing is needed when ALL of the following hold:
//   - BatteryBalancingBonus is configured (non-zero) – feature is enabled
//   - At least 7 days have elapsed since the last balancing (or it has never happened)
//
// The caller is responsible for updating LastBalancingTime whenever the battery
// is observed to reach 100% SOC (e.g. in the EMS main loop).
// Setting LastBalancingTime prevents the optimizer from attempting balancing again
// within the following week, keeping the battery cycle count to a minimum.
func (mpc *Controller) needsWeeklyBalancing(forecast []TimeSlot) bool {
	if mpc.Config.BatteryBalancingBonus <= 0 {
		return false // feature disabled – no bonus configured
	}
	if len(forecast) == 0 {
		return false // nothing to optimise
	}
	if mpc.LastBalancingTime == 0 {
		return true // battery has never been fully charged for balancing
	}
	const week = int64(7 * 24 * 3600)
	// Balancing is needed once at least a full week has passed since the last one.
	return forecast[0].Timestamp-mpc.LastBalancingTime >= week
}
