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
	Hour      int
	Timestamp int64 // Unix timestamp when this time slot begins
	// batteryCharge is the total planned charge power (kW) before it's known how
	// much comes from PV vs. grid. It's an internal DP working value only, used
	// for SOC/temperature/profit calculations during optimization; callers must
	// use BatteryChargeFromPV + BatteryChargeFromGrid for the actual charge.
	batteryCharge         float64
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
	// BalancingNeeded reports whether this optimisation run determined that
	// weekly cell-balancing (charging to BatteryMaxSOC) should be incentivised.
	// It is the same value for every decision returned by a given Optimize
	// call, since needsWeeklyBalancing is evaluated once per run. Callers can
	// use it to distinguish "battery is full because balancing was due" from
	// "battery is full incidentally", e.g. to avoid opportunistically pushing
	// SOC toward BatteryMaxSOC outside of the weekly balancing window.
	BalancingNeeded bool
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

// Optimize finds the optimal control strategy using dynamic programming, then splits
// the resulting charge into BatteryChargeFromPV and BatteryChargeFromGrid.
//
// The split is derived from the single solar-aware DP pass: whatever the PV surplus
// can cover is attributed to PV, and the remainder is grid charge.  Deriving it this
// way is what keeps overnight grid charging honest — the optimizer sizes the night's
// imports knowing how much free PV is arriving in the morning, so it buys only what
// still leaves headroom for that PV rather than filling the battery at 03:00 and
// forcing the next day's generation to be exported at midday's low prices.
//
// Grid charging is additionally suppressed *within the daylight window* when the
// forecasted solar surplus is sufficient to charge the battery from its current SOC
// to full.  This prevents unnecessary grid imports during temporary cloud cover when
// overall daily solar production is more than enough to meet the full charge
// requirement.
//
// That suppression deliberately does NOT extend to slots outside the daylight window.
// Before sunrise and after sunset there is no PV to wait for, so "today's sun will
// cover it" is not an argument against buying cheap energy: overnight
// charge-low/discharge-high arbitrage is profitable independently of how sunny the
// following day turns out to be.  Applying the gate across the whole horizon left the
// battery idle through the cheapest hours of the night purely because the next
// afternoon looked bright.
func (mpc *Controller) Optimize(forecast []TimeSlot) []ControlDecision {
	if len(forecast) == 0 {
		return nil
	}

	// Determine whether cell-balancing (charging to BatteryMaxSOC) should be
	// incentivised during this optimisation run – at most once per calendar week.
	needsBalancing := mpc.needsWeeklyBalancing(forecast)

	// Run optimization with the full solar forecast.
	decisionsWithSolar := mpc.optimizeWithForecast(forecast, true, needsBalancing)

	n := min(len(decisionsWithSolar), len(forecast))

	// ── Solar-sufficiency check ──────────────────────────────────────────────
	// If the total forecasted solar surplus (generation minus load, summed over
	// all slots through the nearest sunset) is enough to charge the battery
	// from its current SOC to BatteryMaxSOC, suppress all grid charging
	// decisions.  Clouds may temporarily reduce per-slot solar to zero, but as
	// long as the day's overall production covers the full charge requirement
	// there is no reason to import from the grid.
	timeSlotDuration := mpc.Config.TimeSlotDuration
	if timeSlotDuration == 0 {
		timeSlotDuration = 1.0
	}

	// Energy (kWh at the AC input) needed to charge from CurrentSOC to
	// BatteryMaxSOC, accounting for charging efficiency.
	energyNeededToFull := math.Max(0,
		(mpc.Config.BatteryMaxSOC-mpc.CurrentSOC)*mpc.Config.BatteryCapacity/mpc.batteryEfficiency())

	// Only count solar surplus up to the nearest sunset. Summing across a
	// multi-day horizon would let solar production from a future day mask an
	// inability to charge fully today, causing grid charging to be suppressed
	// even though today's solar alone can't cover it.
	//
	// Sunset cannot simply be "the first zero-solar slot after the sun came up":
	// a passing cloud also drives the forecast to zero, and treating it as
	// sunset would cut the day short — collapsing the surplus total and
	// defeating the very transient-cloud case this heuristic exists to handle.
	// Instead a sunset is a zero-solar run long enough to be actual night
	// (nightGapSlots ≈ 3 hours); shorter gaps are cloud cover and are absorbed
	// into the day.  If no such run occurs within the horizon (e.g. the horizon
	// ends mid-afternoon, or there is no daylight forecast at all) the full
	// horizon is used.
	//
	// sunriseIdx is the first slot with any forecasted solar. Together the two
	// indices delimit the daylight window [sunriseIdx, sunsetIdx) in which the
	// solar-sufficiency argument actually applies.
	nightGapSlots := int(math.Ceil(3.0 / timeSlotDuration))
	if nightGapSlots < 1 {
		nightGapSlots = 1
	}

	sunriseIdx := n
	sunsetIdx := n
	sawSun := false
	zeroRunStart := -1
	for i, slot := range forecast[:n] {
		if slot.SolarForecast > 0 {
			if !sawSun {
				sunriseIdx = i
			}
			sawSun = true
			zeroRunStart = -1
			continue
		}
		if !sawSun {
			continue
		}
		if zeroRunStart < 0 {
			zeroRunStart = i
		}
		if i-zeroRunStart+1 >= nightGapSlots {
			sunsetIdx = zeroRunStart
			break
		}
	}

	// Sum of per-slot solar surplus (solar minus load, floored at 0) in kWh,
	// only through the nearest sunset.
	var totalSolarSurplus float64
	for _, slot := range forecast[:sunsetIdx] {
		totalSolarSurplus += math.Max(0, slot.SolarForecast-slot.LoadForecast) * timeSlotDuration
	}

	// When solar alone can cover the full charge, discard grid charging to
	// avoid pulling from the grid during transient cloud cover.
	solarSufficient := totalSolarSurplus >= energyNeededToFull

	// ── Split the optimized charge into PV and grid portions ────────────────
	// The DP decision accounts for the full charge rate; we attribute whatever
	// the PV surplus can cover to PV and the remainder to the grid.
	//
	// Splitting can *reduce* the charge that will actually happen versus what
	// the DP assumed (grid top-up gets suppressed when solarSufficient is true
	// inside the daylight window). Everything the DP derived from its charge
	// amount — GridImport/GridExport, battery preheating, the SOC trajectory,
	// and Profit — is therefore recomputed below from the actual (possibly
	// lower) charge so the returned decisions stay internally consistent with
	// what will really be executed.
	finalDecisions := make([]ControlDecision, n)
	runningSOC := mpc.CurrentSOC
	for i, slot := range forecast[:n] {
		finalDecisions[i] = decisionsWithSolar[i]

		totalCharge := decisionsWithSolar[i].batteryCharge

		// PV surplus available for charging after serving the load.
		pvSurplus := math.Max(0, slot.SolarForecast-slot.LoadForecast)

		// The PV portion is how much of the charge is covered by PV surplus
		// (capped at the total charge being applied).
		pvPortion := math.Min(pvSurplus, totalCharge)

		// Grid portion: the part of the DP's charge that PV cannot cover,
		// suppressed when daily solar production is sufficient to charge the
		// battery in full *and* this slot lies inside the daylight window.
		// Outside that window (before sunrise / after sunset) there is no PV
		// on the way, so cheap-hour grid charging stays available and normal
		// overnight arbitrage is preserved.
		//
		// Because totalCharge comes from the solar-aware DP, the grid portion
		// is already sized in the knowledge of the coming day's generation: it
		// buys only what still leaves room for that PV.
		inDaylightWindow := i >= sunriseIdx && i < sunsetIdx
		gridPortion := 0.0
		if !solarSufficient || !inDaylightWindow {
			gridPortion = math.Max(0, totalCharge-pvPortion)
		}

		actualCharge := pvPortion + gridPortion

		// Clamp charge and discharge to what the battery can actually accept or
		// deliver from runningSOC.  The DP's SOC trajectory differs from the
		// reconciled one computed here whenever the grid top-up was suppressed
		// above.  Without this clamp calculateNewSOC silently saturates at the
		// SOC limits while the grid import/export below is still sized for the
		// unclamped power — producing plans that import into a full battery or
		// export energy the battery does not hold (observed in production as a
		// 20 kW discharge scheduled from a battery at 0.2% SOC).
		discharge := finalDecisions[i].BatteryDischarge

		if maxCharge := mpc.maxChargePower(runningSOC, timeSlotDuration); actualCharge > maxCharge {
			// Give up the grid top-up first: PV surplus is free and would
			// otherwise have to be curtailed or exported, whereas grid charge
			// is purely optional and costs money.
			pvPortion = math.Min(pvPortion, maxCharge)
			gridPortion = math.Max(0, maxCharge-pvPortion)
			actualCharge = pvPortion + gridPortion
		}

		if maxDischarge := mpc.maxDischargePower(runningSOC, timeSlotDuration); discharge > maxDischarge {
			discharge = math.Max(0, maxDischarge)
			finalDecisions[i].BatteryDischarge = discharge
		}

		finalDecisions[i].BatteryChargeFromPV = pvPortion
		finalDecisions[i].BatteryChargeFromGrid = gridPortion

		// Keep the internal working value consistent with the charge that will
		// actually be applied, so the calculateProfit call below (degradation
		// cost) agrees with the public ChargeFromPV/ChargeFromGrid fields.
		finalDecisions[i].batteryCharge = actualCharge

		// Preheating only makes sense while the battery is actually charging;
		// if the charge got suppressed above, drop the preheat flag/load too.
		preHeatActive := finalDecisions[i].BatteryPreHeatActive && actualCharge > 0
		finalDecisions[i].BatteryPreHeatActive = preHeatActive
		extraLoad := 0.0
		if preHeatActive {
			extraLoad = mpc.Config.BatteryPreHeatPower
		}

		// Recompute the grid import/export to match the actual charge. The
		// with-solar scenario may have picked a higher charge rate that
		// included a grid top-up; when that top-up is suppressed above, the
		// import/export figures must be recalculated — otherwise the
		// decision would report a grid import that funds a charge which no
		// longer happens.  `discharge` is the clamped value from above.
		netSupply := slot.SolarForecast + discharge*mpc.batteryEfficiency()
		netLoad := slot.LoadForecast + actualCharge/mpc.batteryEfficiency() + extraLoad

		balance := netSupply - netLoad

		if balance > 0 {
			if slot.ExportPrice > 0 {
				finalDecisions[i].GridExport = math.Min(balance, mpc.Config.MaxGridExport)
			} else {
				// Negative or zero export price — curtail the remainder.
				finalDecisions[i].GridExport = 0
			}
			finalDecisions[i].GridImport = 0
		} else {
			finalDecisions[i].GridImport = math.Min(-balance, mpc.Config.MaxGridImport)
			finalDecisions[i].GridExport = 0
		}

		// Recompute the SOC trajectory from the actual charge/discharge so it
		// reflects what will really happen to the battery, rather than the
		// (possibly higher) charge rate assumed by the with-solar DP pass.
		runningSOC = mpc.calculateNewSOC(runningSOC, actualCharge, discharge)
		finalDecisions[i].BatterySOC = runningSOC

		// Recompute profit from the corrected decision. This mirrors exactly
		// what the DP itself would have computed for this decision/slot pair
		// (see optimizeWithForecast, where decision.Profit is likewise set to
		// calculateProfit(dec, slot) before any cell-balancing bonus, which is
		// tracked separately and not part of this per-slot field).
		finalDecisions[i].Profit = mpc.calculateProfit(finalDecisions[i], slot)

		finalDecisions[i].BalancingNeeded = needsBalancing
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
					newSOC := mpc.calculateNewSOC(currentSOC, dec.batteryCharge, dec.BatteryDischarge)
					newSOCIdx := mpc.socToIndex(newSOC, socStep)

					if newSOCIdx < 0 || newSOCIdx > socSteps {
						continue
					}

					// Calculate next battery temperature based on this decision
					newBatteryTemp := mpc.calculateNextBatteryTemp(currentBatteryTemp, slot.AirTemperature, dec.batteryCharge > 0, dec.BatteryPreHeatActive)

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
			// Use the same DC-side, balancing-aware efficiency as calculateNewSOC
			// so that the top-up charge correctly accounts for the extra energy
			// required in the CV/balancing phase when the battery is nearly full.
			efficiency := 1.0
			if mpc.Config.BatteryBalancingSOCThreshold > 0 &&
				mpc.Config.BatteryBalancingEfficiencyFactor > 0 &&
				currentSOC >= mpc.Config.BatteryBalancingSOCThreshold {
				efficiency = mpc.Config.BatteryBalancingEfficiencyFactor
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
			batteryCharge:        action.charge,
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

		netLoad := slot.LoadForecast + action.charge/mpc.batteryEfficiency() + extraLoad
		netSupply := netSolar + action.discharge*mpc.batteryEfficiency()

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
				extraCharge := math.Min(balance*mpc.batteryEfficiency(), mpc.Config.BatteryMaxCharge-action.charge)
				if extraCharge > 0 && mpc.canCharge(currentSOC, action.charge+extraCharge) {
					// Absorb as much surplus as possible into the battery.
					dec.batteryCharge += extraCharge
					// Recalculate balance after the extra charging.
					balance -= extraCharge / mpc.batteryEfficiency()
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
	batteryThroughput := (dec.batteryCharge + dec.BatteryDischarge) * timeSlotDuration
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

	// Use the same DC-side convention and balancing-aware derate as
	// calculateNewSOC so that both functions agree on how much the SOC actually
	// rises.  BatteryEfficiency is deliberately NOT applied here: it is already
	// accounted for on the AC side of the power balance.
	efficiency := 1.0
	if mpc.Config.BatteryBalancingSOCThreshold > 0 &&
		mpc.Config.BatteryBalancingEfficiencyFactor > 0 &&
		soc >= mpc.Config.BatteryBalancingSOCThreshold {
		efficiency = mpc.Config.BatteryBalancingEfficiencyFactor
	}

	// Convert power (kW) to energy (kWh) using the same formula as calculateNewSOC:
	// multiply by time slot duration AND efficiency so that both functions agree on
	// how much the SOC actually rises.
	chargeEnergy := charge * timeSlotDuration * efficiency

	// With no usable capacity nothing can be stored; charging is never feasible.
	// Guarding here keeps the ±Inf/NaN out of the comparison below.
	if mpc.Config.BatteryCapacity <= 0 {
		return false
	}

	newSOC := soc + (chargeEnergy / mpc.Config.BatteryCapacity)
	return newSOC <= mpc.Config.BatteryMaxSOC
}

func (mpc *Controller) canDischarge(soc, discharge float64) bool {
	// Get time slot duration (default to 1 hour if not specified for backward compatibility)
	timeSlotDuration := mpc.Config.TimeSlotDuration
	if timeSlotDuration == 0 {
		timeSlotDuration = 1.0
	}

	// With no usable capacity there is nothing stored to discharge.
	if mpc.Config.BatteryCapacity <= 0 {
		return false
	}

	// Convert power (kW) to energy (kWh) by multiplying by time slot duration
	dischargeEnergy := discharge * timeSlotDuration
	newSOC := soc - (dischargeEnergy / mpc.Config.BatteryCapacity)
	return newSOC >= mpc.Config.BatteryMinSOC
}

// batteryEfficiency returns the configured round-trip efficiency, defaulting to 1.0
// (lossless) when it is unset or non-positive.
//
// Several power-balance expressions divide by this value. With an unpopulated config
// it is zero, and `0 / 0` is NaN — which silently propagated into GridImport, Profit
// and ultimately the SOC index, where it surfaced as an out-of-range panic. Defaulting
// mirrors how TimeSlotDuration is already handled throughout this file and keeps every
// returned figure finite.
func (mpc *Controller) batteryEfficiency() float64 {
	if mpc.Config.BatteryEfficiency <= 0 {
		return 1.0
	}
	return mpc.Config.BatteryEfficiency
}

// maxChargePower returns the largest charge power (kW) that can be applied for one
// time slot from soc without exceeding BatteryMaxSOC.  It is the exact inverse of the
// SOC update in calculateNewSOC, including the CV/balancing derate, so that clamping
// to this value never saturates.  The result is also capped at the hardware limit.
func (mpc *Controller) maxChargePower(soc, timeSlotDuration float64) float64 {
	efficiency := 1.0
	if mpc.Config.BatteryBalancingSOCThreshold > 0 &&
		mpc.Config.BatteryBalancingEfficiencyFactor > 0 &&
		soc >= mpc.Config.BatteryBalancingSOCThreshold {
		efficiency = mpc.Config.BatteryBalancingEfficiencyFactor
	}

	headroom := mpc.Config.BatteryMaxSOC - soc
	if headroom <= 0 {
		return 0
	}

	return math.Min(
		headroom*mpc.Config.BatteryCapacity/(efficiency*timeSlotDuration),
		mpc.Config.BatteryMaxCharge,
	)
}

// maxDischargePower returns the largest discharge power (kW) that can be sustained for
// one time slot from soc without dropping below BatteryMinSOC, capped at the hardware
// limit.  Mirrors the SOC update in calculateNewSOC.
func (mpc *Controller) maxDischargePower(soc, timeSlotDuration float64) float64 {
	available := soc - mpc.Config.BatteryMinSOC
	if available <= 0 {
		return 0
	}

	return math.Min(
		available*mpc.Config.BatteryCapacity/timeSlotDuration,
		mpc.Config.BatteryMaxDischarge,
	)
}

func (mpc *Controller) calculateNewSOC(currentSOC, charge, discharge float64) float64 {
	// Get time slot duration (default to 1 hour if not specified for backward compatibility)
	timeSlotDuration := mpc.Config.TimeSlotDuration
	if timeSlotDuration == 0 {
		timeSlotDuration = 1.0
	}

	// Charge/discharge powers are DC-side (battery terminal) quantities, which
	// is the convention the power balance in generateFeasibleDecisions uses:
	// charging draws charge/BatteryEfficiency from the AC bus and discharging
	// delivers discharge*BatteryEfficiency to it.  The conversion loss is
	// therefore already accounted for on the AC side, and the DC-side energy
	// that actually moves in or out of the cells is simply power * duration.
	//
	// Applying BatteryEfficiency again here would double-count the charging
	// loss: bus->SOC would be modelled at efficiency^2 while SOC->bus stayed at
	// efficiency, giving an asymmetric round trip (0.778 instead of 0.846 at
	// efficiency=0.92) that made charging look worse than discharging.
	//
	// The CV/balancing derate is a separate physical effect and still applies:
	// at or above BatteryBalancingSOCThreshold the cells are being balanced and
	// far more input energy is needed per unit of SOC gain.
	efficiency := 1.0
	if charge > 0 &&
		mpc.Config.BatteryBalancingSOCThreshold > 0 &&
		mpc.Config.BatteryBalancingEfficiencyFactor > 0 &&
		currentSOC >= mpc.Config.BatteryBalancingSOCThreshold {
		efficiency = mpc.Config.BatteryBalancingEfficiencyFactor
	}

	// Convert power (kW) to energy (kWh) by multiplying by time slot duration
	chargeEnergy := charge * timeSlotDuration * efficiency
	dischargeEnergy := discharge * timeSlotDuration

	// A non-positive capacity means no energy can move in or out of the cells.
	// Dividing by it would yield NaN (0/0) or ±Inf and poison the SOC for the
	// rest of the horizon — and ultimately the DP index derived from it.
	if mpc.Config.BatteryCapacity <= 0 {
		return math.Max(mpc.Config.BatteryMinSOC, math.Min(mpc.Config.BatteryMaxSOC, currentSOC))
	}

	socChange := (chargeEnergy - dischargeEnergy) / mpc.Config.BatteryCapacity
	newSOC := currentSOC + socChange
	return math.Max(mpc.Config.BatteryMinSOC, math.Min(mpc.Config.BatteryMaxSOC, newSOC))
}

// socToIndex maps an SOC value onto its DP table row.
//
// It must never return a value that cannot be used to index the table. Two
// degenerate configurations previously produced one:
//
//   - BatteryMaxSOC == BatteryMinSOC makes socStep zero, so the division
//     yields NaN (0/0) or ±Inf. Converting NaN to int is implementation
//     defined and on amd64/arm64 gives the minimum int64, which panicked as
//     soon as it was used as an index.
//   - A non-finite soc (e.g. NaN propagated from a zero BatteryCapacity)
//     produces the same result.
//
// A zero-width SOC range has exactly one reachable level, so index 0 is the
// correct answer there. A NaN SOC is not a real state and is reported as -1,
// which callers already treat as out of range and skip.
func (mpc *Controller) socToIndex(soc float64, socStep float64) int {
	if !(socStep > 0) {
		return 0
	}

	idx := math.Floor((soc - mpc.Config.BatteryMinSOC) / socStep)
	switch {
	case math.IsNaN(idx):
		return -1
	case math.IsInf(idx, -1):
		return -1
	case math.IsInf(idx, 1):
		return math.MaxInt
	}

	return int(idx)
}

func (mpc *Controller) indexToSOC(index int, socStep float64) float64 {
	return mpc.Config.BatteryMinSOC + float64(index)*socStep
}

func (mpc *Controller) isFeasible(dec ControlDecision) bool {
	// Check all constraints are satisfied
	if dec.batteryCharge > mpc.Config.BatteryMaxCharge {
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
