// Package mpc – cell-balancing feature tests.
//
// These tests cover:
//  1. needsWeeklyBalancing – the once-per-week guard
//  2. CV-phase energy modelling – reduced efficiency near BatteryMaxSOC
//  3. Optimizer integration – bonus incentivises full charge, no double-counting
package mpc

import (
	"math"
	"testing"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// balancingConfig returns a SystemConfig with all cell-balancing parameters
// enabled at reasonable values.  Individual tests override fields as needed.
func balancingConfig() SystemConfig {
	return SystemConfig{
		BatteryCapacity:        10.0,  // kWh
		BatteryMaxCharge:       5.0,   // kW
		BatteryMaxDischarge:    5.0,   // kW
		BatteryMinSOC:          0.1,   // 10 %
		BatteryMaxSOC:          1.0,   // 100 %
		BatteryEfficiency:      0.9,   // 90 % round-trip
		BatteryDegradationCost: 0.005, // $/kWh cycled
		MaxGridImport:          10.0,
		MaxGridExport:          10.0,
		TimeSlotDuration:       1.0, // 1 hour

		// Cell-balancing
		BatteryBalancingSOCThreshold:     0.999, // CV phase starts at 99.9 %
		BatteryBalancingEfficiencyFactor: 0.3,   // 3 × more input energy in CV phase
		BatteryBalancingBonus:            0.10,  // $0.10 one-time optimisation bonus
	}
}

// Anchor timestamps at noon UTC.
// Using noon (12:00 UTC) keeps time-based comparisons in needsWeeklyBalancing
// consistent across all populated timezones (UTC-11 … UTC+11).
//
// 2024-01-04 00:00:00 UTC = 1704326400 (used by existing tests)
// 2024-01-04 12:00:00 UTC = 1704326400 + 12*3600 = 1704369600
// 2024-01-05 12:00:00 UTC = 1704369600 + 24*3600 = 1704456000
const (
	jan04noon = int64(1704369600)
	jan05noon = int64(1704456000)
)

// cheapSlot creates a TimeSlot with a very low import price at the given
// Unix timestamp.
func cheapBalancingSlot(ts int64, hour int) TimeSlot {
	return TimeSlot{
		Hour:          hour,
		Timestamp:     ts,
		ImportPrice:   0.05,
		ExportPrice:   0.02,
		SolarForecast: 0,
		LoadForecast:  0,
	}
}

// expensiveBalancingSlot creates a TimeSlot with a high import price.
func expensiveBalancingSlot(ts int64, hour int) TimeSlot {
	return TimeSlot{
		Hour:          hour,
		Timestamp:     ts,
		ImportPrice:   0.50,
		ExportPrice:   0.30,
		SolarForecast: 0,
		LoadForecast:  0.5,
	}
}

// ─── needsDailyBalancing ─────────────────────────────────────────────────────
// ─── needsWeeklyBalancing ──────────────────────────────────────────────────────────────────────

func TestNeedsWeeklyBalancing_FeatureDisabled(t *testing.T) {
	config := balancingConfig()
	config.BatteryBalancingBonus = 0 // feature off

	ctrl := NewController(config, 1, 0.5)
	ctrl.LastBalancingTime = 0 // never balanced

	if ctrl.needsWeeklyBalancing([]TimeSlot{{Timestamp: jan05noon}}) {
		t.Error("needsWeeklyBalancing must return false when BatteryBalancingBonus is 0")
	}
}

func TestNeedsWeeklyBalancing_NeverBalanced(t *testing.T) {
	ctrl := NewController(balancingConfig(), 1, 0.5)
	ctrl.LastBalancingTime = 0 // zero means "never"

	if !ctrl.needsWeeklyBalancing([]TimeSlot{{Timestamp: jan05noon}}) {
		t.Error("needsWeeklyBalancing must return true when the battery has never been fully charged")
	}
}

func TestNeedsWeeklyBalancing_AlreadyBalancedWithinAWeek(t *testing.T) {
	ctrl := NewController(balancingConfig(), 1, 0.5)
	ctrl.LastBalancingTime = jan05noon - 6*24*3600 // 6 days before forecast start

	if ctrl.needsWeeklyBalancing([]TimeSlot{{Timestamp: jan05noon}}) {
		t.Error("needsWeeklyBalancing must return false when balancing happened less than 7 days ago")
	}
}

func TestNeedsWeeklyBalancing_BalancedExactlyAWeekAgo(t *testing.T) {
	ctrl := NewController(balancingConfig(), 1, 0.5)
	ctrl.LastBalancingTime = jan05noon - 7*24*3600 // exactly 7 days before forecast start

	if !ctrl.needsWeeklyBalancing([]TimeSlot{{Timestamp: jan05noon}}) {
		t.Error("needsWeeklyBalancing must return true when at least 7 days have elapsed since last balancing")
	}
}

func TestNeedsWeeklyBalancing_EmptyForecast(t *testing.T) {
	ctrl := NewController(balancingConfig(), 0, 0.5)
	ctrl.LastBalancingTime = 0

	if ctrl.needsWeeklyBalancing([]TimeSlot{}) {
		t.Error("needsWeeklyBalancing must return false for an empty forecast")
	}
}

// ─── CV-phase energy modelling ───────────────────────────────────────────────

// TestCVPhase_ReducedSOCIncrease verifies that the same charge power produces
// less SOC increase when the battery is at/above BatteryBalancingSOCThreshold.
func TestCVPhase_ReducedSOCIncrease(t *testing.T) {
	config := balancingConfig()
	ctrl := NewController(config, 1, 0.5)

	// Use a small charge power so neither SOC trajectory overshoots BatteryMaxSOC
	// and gets clamped, which would mask the efficiency difference we are testing.
	// Below threshold: 0.989 + 0.01*1.0*0.9/10 = 0.9899  (< 1.0 ✓)
	// At threshold:    0.999 + 0.01*1.0*0.27/10 = 0.99927 (< 1.0 ✓)
	chargeKW := 0.01 // kW for one hour

	// ── Below threshold: normal efficiency ──────────────────────────────────
	socBelow := config.BatteryBalancingSOCThreshold - 0.01 // e.g. 98.9 %
	newSOCBelow := ctrl.calculateNewSOC(socBelow, chargeKW, 0)

	wantBelow := socBelow + chargeKW*config.TimeSlotDuration*config.BatteryEfficiency/config.BatteryCapacity
	if math.Abs(newSOCBelow-wantBelow) > 1e-9 {
		t.Errorf("Below threshold: calculateNewSOC=%.8f, want %.8f", newSOCBelow, wantBelow)
	}

	// ── At threshold: reduced (CV-phase) efficiency ──────────────────────────
	cvEff := config.BatteryEfficiency * config.BatteryBalancingEfficiencyFactor
	socAt := config.BatteryBalancingSOCThreshold
	newSOCAt := ctrl.calculateNewSOC(socAt, chargeKW, 0)

	wantAt := math.Min(config.BatteryMaxSOC,
		socAt+chargeKW*config.TimeSlotDuration*cvEff/config.BatteryCapacity)
	if math.Abs(newSOCAt-wantAt) > 1e-9 {
		t.Errorf("At CV threshold: calculateNewSOC=%.8f, want %.8f", newSOCAt, wantAt)
	}

	// ── The SOC increase must be BatteryBalancingEfficiencyFactor× smaller ──
	increaseBelow := newSOCBelow - socBelow
	increaseAt := newSOCAt - socAt
	if increaseAt <= 0 {
		t.Fatal("Expected positive SOC increase above threshold")
	}
	ratio := increaseBelow / increaseAt
	wantRatio := 1.0 / config.BatteryBalancingEfficiencyFactor // ~3.33
	if math.Abs(ratio-wantRatio) > 0.01 {
		t.Errorf("SOC-increase ratio (below/at threshold) = %.3f, want %.3f (= 1/BatteryBalancingEfficiencyFactor)",
			ratio, wantRatio)
	}
}

// TestCVPhase_DischargeUnaffected verifies that discharging always uses the
// normal (non-reduced) energy formula, even when SOC is in the CV range.
func TestCVPhase_DischargeUnaffected(t *testing.T) {
	config := balancingConfig()
	ctrl := NewController(config, 1, 0.5)

	dischargeKW := 2.0
	soc := config.BatteryBalancingSOCThreshold // deep in CV range

	got := ctrl.calculateNewSOC(soc, 0, dischargeKW)
	want := math.Max(config.BatteryMinSOC,
		soc-dischargeKW*config.TimeSlotDuration/config.BatteryCapacity)
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("Discharge above CV threshold: got %.8f, want %.8f (efficiency factor must not affect discharge)",
			got, want)
	}
}

// TestCVPhase_CanChargeConsistency checks that canCharge and calculateNewSOC
// agree: a charge rate that canCharge accepts must not push SOC above BatteryMaxSOC.
func TestCVPhase_CanChargeConsistency(t *testing.T) {
	config := balancingConfig()
	ctrl := NewController(config, 1, 0.5)

	soc := config.BatteryBalancingSOCThreshold

	// Maximum charge that fits within [soc, BatteryMaxSOC] in the CV phase:
	//   maxCharge = (BatteryMaxSOC - soc) * capacity / (duration * efficiency * factor)
	cvEff := config.BatteryEfficiency * config.BatteryBalancingEfficiencyFactor
	maxAllowed := (config.BatteryMaxSOC - soc) * config.BatteryCapacity /
		(config.TimeSlotDuration * cvEff)

	// 90 % of max → must be accepted and must stay within bounds
	if !ctrl.canCharge(soc, maxAllowed*0.9) {
		t.Errorf("canCharge(%.4f, %.4f kW) should return true (90 %% of CV-phase max)", soc, maxAllowed*0.9)
	}
	newSOC := ctrl.calculateNewSOC(soc, maxAllowed*0.9, 0)
	if newSOC > config.BatteryMaxSOC+1e-9 {
		t.Errorf("calculateNewSOC overshoot: %.8f > BatteryMaxSOC %.8f", newSOC, config.BatteryMaxSOC)
	}

	// 110 % of max → must be rejected
	if ctrl.canCharge(soc, maxAllowed*1.1) {
		t.Errorf("canCharge(%.4f, %.4f kW) should return false (110 %% of CV-phase max would overshoot)", soc, maxAllowed*1.1)
	}
}

// TestCVPhase_DisabledWhenFieldsAreZero verifies backward compatibility:
// setting BatteryBalancingSOCThreshold and BatteryBalancingEfficiencyFactor to
// zero must restore the original (unmodified) efficiency behaviour.
func TestCVPhase_DisabledWhenFieldsAreZero(t *testing.T) {
	config := balancingConfig()
	config.BatteryBalancingSOCThreshold = 0
	config.BatteryBalancingEfficiencyFactor = 0
	ctrl := NewController(config, 1, 0.5)

	chargeKW := 1.0
	soc := 0.999 // would normally be in CV phase
	got := ctrl.calculateNewSOC(soc, chargeKW, 0)
	want := math.Min(config.BatteryMaxSOC,
		soc+chargeKW*config.TimeSlotDuration*config.BatteryEfficiency/config.BatteryCapacity)
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("CV phase disabled: calculateNewSOC=%.8f, want %.8f (normal efficiency should apply)",
			got, want)
	}
}

// ─── Optimizer integration ────────────────────────────────────────────────────

// TestOptimizer_BalancingBonusIncentivisesFullCharge checks that, when the
// BatteryBalancingBonus is generous and import is cheap, the optimizer plans
// a charge to BatteryMaxSOC (100 %) for cell balancing.
func TestOptimizer_BalancingBonusIncentivisesFullCharge(t *testing.T) {
	config := balancingConfig()
	config.BatteryBalancingBonus = 0.30 // clearly outweighs CV-phase import cost

	forecast := []TimeSlot{
		cheapBalancingSlot(jan05noon+0*3600, 0),
		cheapBalancingSlot(jan05noon+1*3600, 1),
		cheapBalancingSlot(jan05noon+2*3600, 2),
		expensiveBalancingSlot(jan05noon+3*3600, 3),
	}

	ctrl := NewController(config, len(forecast), 0.90)
	ctrl.LastBalancingTime = 0 // never balanced → incentive is active

	decisions := ctrl.Optimize(forecast)
	if len(decisions) != len(forecast) {
		t.Fatalf("expected %d decisions, got %d", len(forecast), len(decisions))
	}

	maxSOC := 0.0
	for _, d := range decisions {
		if d.BatterySOC > maxSOC {
			maxSOC = d.BatterySOC
		}
	}

	if maxSOC < config.BatteryMaxSOC-0.001 {
		t.Errorf("optimizer should reach BatteryMaxSOC (%.1f %%) when bonus is large and energy is cheap; peak SOC was %.2f %%",
			config.BatteryMaxSOC*100, maxSOC*100)
	}
	t.Logf("Peak SOC with balancing incentive active: %.2f %%", maxSOC*100)
}

// TestOptimizer_BalancingNotJustifiedByExpensiveEnergy verifies that a tiny
// bonus does NOT cause the optimizer to charge to 100 % when import prices are
// high – the expected energy cost must exceed the bonus value.
func TestOptimizer_BalancingNotJustifiedByExpensiveEnergy(t *testing.T) {
	config := balancingConfig()
	config.BatteryBalancingBonus = 0.01 // tiny bonus

	// Charging the remaining 10 % at $0.80/kWh costs roughly $0.89 >> $0.01
	forecast := []TimeSlot{
		{Hour: 0, Timestamp: jan05noon + 0*3600, ImportPrice: 0.80, ExportPrice: 0.0, LoadForecast: 0},
		{Hour: 1, Timestamp: jan05noon + 1*3600, ImportPrice: 0.80, ExportPrice: 0.0, LoadForecast: 0},
		{Hour: 2, Timestamp: jan05noon + 2*3600, ImportPrice: 0.80, ExportPrice: 0.0, LoadForecast: 0},
	}

	ctrl := NewController(config, len(forecast), 0.90)
	ctrl.LastBalancingTime = 0

	decisions := ctrl.Optimize(forecast)

	for i, d := range decisions {
		if d.BatterySOC >= config.BatteryMaxSOC-0.001 {
			t.Errorf("slot %d: optimizer reached 100 %% SOC despite tiny bonus and expensive energy (SOC=%.4f)",
				i, d.BatterySOC)
		}
	}
	t.Log("Balancing correctly skipped when energy cost exceeds bonus value")
}

// TestOptimizer_BalancingSkippedWhenAlreadyDoneToday verifies the once-per-day
// guard: a second run on the same calendar day must not re-apply the balancing
// incentive, so the two runs produce different peak-SOC plans.
func TestOptimizer_BalancingSkippedWhenAlreadyDoneToday(t *testing.T) {
	config := balancingConfig()
	config.BatteryBalancingBonus = 0.30 // large – always justifies charging when active

	forecast := []TimeSlot{
		cheapBalancingSlot(jan05noon+0*3600, 0),
		cheapBalancingSlot(jan05noon+1*3600, 1),
		expensiveBalancingSlot(jan05noon+2*3600, 2),
	}

	// Run A: balancing never done → incentive active
	ctrlA := NewController(config, len(forecast), 0.90)
	ctrlA.LastBalancingTime = 0
	decisionsA := ctrlA.Optimize(forecast)

	// Run B: balancing done recently (within 7 days) → incentive suppressed
	ctrlB := NewController(config, len(forecast), 0.90)
	ctrlB.LastBalancingTime = jan05noon - 3600 // 1 h before the forecast start
	decisionsB := ctrlB.Optimize(forecast)

	peakA, peakB := 0.0, 0.0
	for _, d := range decisionsA {
		if d.BatterySOC > peakA {
			peakA = d.BatterySOC
		}
	}
	for _, d := range decisionsB {
		if d.BatterySOC > peakB {
			peakB = d.BatterySOC
		}
	}

	t.Logf("Peak SOC – incentive active: %.2f %%, incentive suppressed: %.2f %%", peakA*100, peakB*100)

	if peakA < config.BatteryMaxSOC-0.001 {
		t.Errorf("run A (incentive active): expected peak SOC %.1f %%, got %.2f %%",
			config.BatteryMaxSOC*100, peakA*100)
	}
	if peakB >= config.BatteryMaxSOC-0.001 {
		t.Errorf("run B (already balanced today): should NOT reach 100 %%, got peak SOC %.2f %%", peakB*100)
	}
}

// TestOptimizer_BalancingBonusCountedOnce ensures the DP awards
// BatteryBalancingBonus at most once per optimisation horizon.
//
// The test creates a forecast with two cheap-energy windows separated by a
// discharge window.  If the bonus were counted twice the optimizer would be
// over-optimistic and plan two full charges.  With correct once-only accounting
// the second cheap window does not receive an artificial extra boost and the
// optimizer only reaches 100 % as many times as the energy economics justify.
func TestOptimizer_BalancingBonusCountedOnce(t *testing.T) {
	config := balancingConfig()
	// Bonus is large enough to justify one CV-phase charge, but checking twice
	// would unfairly double it.
	config.BatteryBalancingBonus = 0.50

	// Forecast layout: cheap × 2 → expensive × 2 → cheap × 2
	forecast := []TimeSlot{
		cheapBalancingSlot(jan05noon+0*3600, 0),
		cheapBalancingSlot(jan05noon+1*3600, 1),
		{Hour: 2, Timestamp: jan05noon + 2*3600, ImportPrice: 0.40, ExportPrice: 0.35, LoadForecast: 1.0},
		{Hour: 3, Timestamp: jan05noon + 3*3600, ImportPrice: 0.40, ExportPrice: 0.35, LoadForecast: 1.0},
		cheapBalancingSlot(jan05noon+4*3600, 4),
		cheapBalancingSlot(jan05noon+5*3600, 5),
	}

	ctrl := NewController(config, len(forecast), 0.50)
	ctrl.LastBalancingTime = 0

	decisions := ctrl.Optimize(forecast)
	if len(decisions) != len(forecast) {
		t.Fatalf("expected %d decisions, got %d", len(forecast), len(decisions))
	}

	// Count how many distinct upward transitions into BatteryMaxSOC occur.
	transitions := 0
	prevSOC := ctrl.CurrentSOC
	for _, d := range decisions {
		if d.BatterySOC >= config.BatteryMaxSOC-0.001 && prevSOC < config.BatteryMaxSOC-0.001 {
			transitions++
		}
		prevSOC = d.BatterySOC
	}

	socHistory := make([]float64, len(decisions))
	for i, d := range decisions {
		socHistory[i] = d.BatterySOC
	}
	t.Logf("SOC timeline: %v", socHistory)
	t.Logf("Transitions into BatteryMaxSOC: %d", transitions)

	// With the bonus only counted once, there is no artificial incentive for a
	// second full charge.  Any second reach of 100 % must be purely economic
	// (driven by arbitrage), which for this price structure should not happen.
	if transitions > 1 {
		t.Errorf("battery reached BatteryMaxSOC %d times; expected at most 1 (bonus must not be counted twice)", transitions)
	}
}

// TestOptimizer_BackwardsCompatZeroBalancingFields confirms that setting all
// three balancing fields to zero (the package default) leaves the optimizer
// fully functional and unchanged from pre-feature behaviour.
func TestOptimizer_BackwardsCompatZeroBalancingFields(t *testing.T) {
	config := SystemConfig{
		BatteryCapacity:        10.0,
		BatteryMaxCharge:       5.0,
		BatteryMaxDischarge:    5.0,
		BatteryMinSOC:          0.1,
		BatteryMaxSOC:          1.0,
		BatteryEfficiency:      0.9,
		BatteryDegradationCost: 0.005,
		MaxGridImport:          10.0,
		MaxGridExport:          10.0,
		TimeSlotDuration:       1.0,
		// BatteryBalancingSOCThreshold, BatteryBalancingEfficiencyFactor,
		// BatteryBalancingBonus all zero → feature completely disabled
	}

	forecast := []TimeSlot{
		cheapBalancingSlot(jan05noon, 0),
		expensiveBalancingSlot(jan05noon+3600, 1),
		cheapBalancingSlot(jan05noon+7200, 2),
	}

	ctrl := NewController(config, len(forecast), 0.5)
	decisions := ctrl.Optimize(forecast)

	if len(decisions) != len(forecast) {
		t.Fatalf("expected %d decisions, got %d", len(forecast), len(decisions))
	}

	for i, d := range decisions {
		if d.BatterySOC < config.BatteryMinSOC-1e-9 || d.BatterySOC > config.BatteryMaxSOC+1e-9 {
			t.Errorf("slot %d: SOC %.4f out of [%.2f, %.2f]",
				i, d.BatterySOC, config.BatteryMinSOC, config.BatteryMaxSOC)
		}
		if d.BatteryCharge > config.BatteryMaxCharge+1e-9 {
			t.Errorf("slot %d: BatteryCharge %.4f exceeds BatteryMaxCharge %.4f", i, d.BatteryCharge, config.BatteryMaxCharge)
		}
		if d.BatteryDischarge > config.BatteryMaxDischarge+1e-9 {
			t.Errorf("slot %d: BatteryDischarge %.4f exceeds BatteryMaxDischarge %.4f", i, d.BatteryDischarge, config.BatteryMaxDischarge)
		}
	}
	t.Log("Backward-compatibility OK: optimizer works correctly with all balancing fields set to zero")
}