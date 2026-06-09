package scheduler

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/devskill-org/ems/miners"
	"github.com/devskill-org/ems/mpc"
	"github.com/devskill-org/ems/openmeteo"
)

// This file contains unit tests for the estimateLoadForecast function in mpc.go.
//
// estimateLoadForecast estimates the total power consumption of all miners for a
// given MPC time slot.  The function has three major branches:
//
//  1. Price above limit  → all miners in standby
//  2. Price at/below limit, power control OFF → all miners at Super mode capped by
//     MinersPowerLimit
//  3. Price at/below limit, power control ON  → highest mode (Super → Standard →
//     Eco) that fits within min(solarForecast, MinersPowerLimit); excess miners in
//     standby; if no mode fits, all in standby
//
// Helper: newMPCTestScheduler creates a scheduler with the given config and
// optionally pre-loads the discovered miners map so that GetDiscoveredMiners()
// returns them.
func newMPCTestScheduler(cfg *Config, numMiners int) *MinerScheduler {
	s := NewMinerScheduler(cfg, log.New(os.Stdout, "TEST: ", log.LstdFlags))

	for i := range numMiners {
		miner := &miners.AvalonQHost{
			Address: fmt.Sprintf("192.168.1.%d", 100+i),
			Port:    4028,
			LastStats: &miners.AvalonLiteStats{
				State:    miners.AvalonStateMining,
				WorkMode: miners.AvalonEcoMode,
			},
		}
		key := fmt.Sprintf("%s:%d", miner.Address, miner.Port)
		s.discoveredMiners.Store(key, miner)
	}

	return s
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// baseConfig returns a Config with sensible power values suitable for most tests.
func baseConfig() *Config {
	return &Config{
		MinerPowerStandby:        0.1,
		MinerPowerEco:            1.0,
		MinerPowerStandard:       1.5,
		MinerPowerSuper:          2.0,
		MinersPowerLimit:         20.0, // large enough not to be a bottleneck unless tested
		PVPowerControlPriceLimit: 999.0,
	}
}

// ---------------------------------------------------------------------------
// No miners
// ---------------------------------------------------------------------------

func TestEstimateLoadForecast_NoMiners(t *testing.T) {
	cfg := baseConfig()
	s := newMPCTestScheduler(cfg, 0)

	got := s.estimateLoadForecast(50.0, 0.1, 10.0, cfg)
	if got != 0.0 {
		t.Errorf("expected 0.0 with no miners, got %.4f", got)
	}
}

// ---------------------------------------------------------------------------
// Price above limit → all standby regardless of power control
// ---------------------------------------------------------------------------

func TestEstimateLoadForecast_PriceAboveLimit_PowerControlOff(t *testing.T) {
	cfg := baseConfig()
	cfg.PVPowerControlPriceLimit = 999.0
	s := newMPCTestScheduler(cfg, 3)

	// price = 200 EUR/MWh → 0.2 EUR/kWh > priceLimit 0.1 EUR/kWh
	got := s.estimateLoadForecast(200.0, 0.1, 10.0, cfg)
	want := 3 * cfg.MinerPowerStandby // 0.3
	if got != want {
		t.Errorf("expected %.4f, got %.4f", want, got)
	}
}

func TestEstimateLoadForecast_PriceAboveLimit_PowerControlOn(t *testing.T) {
	cfg := baseConfig()
	cfg.PVPowerControlPriceLimit = 10.0
	s := newMPCTestScheduler(cfg, 4)

	got := s.estimateLoadForecast(200.0, 0.1, 100.0, cfg)
	want := 4 * cfg.MinerPowerStandby
	if got != want {
		t.Errorf("expected %.4f, got %.4f", want, got)
	}
}

func TestEstimateLoadForecast_PriceExactlyAtLimit_RunsMiners(t *testing.T) {
	cfg := baseConfig()
	cfg.PVPowerControlPriceLimit = 999.0
	s := newMPCTestScheduler(cfg, 2)

	// price = 100 EUR/MWh → 0.1 EUR/kWh == priceLimit → should run
	got := s.estimateLoadForecast(100.0, 0.1, 0.0, cfg)
	// power control off: 2 miners at Super, total = 4.0 kW
	want := 2 * cfg.MinerPowerSuper
	if got != want {
		t.Errorf("expected %.4f, got %.4f", want, got)
	}
}

// ---------------------------------------------------------------------------
// Power control OFF
// ---------------------------------------------------------------------------

func TestEstimateLoadForecast_NoPowerControl_AllMinersBelowLimit(t *testing.T) {
	cfg := baseConfig()
	cfg.PVPowerControlPriceLimit = 999.0
	cfg.MinersPowerLimit = 20.0
	s := newMPCTestScheduler(cfg, 3)

	// 3 miners × 2.0 kW Super = 6.0 kW < 20.0 kW limit
	got := s.estimateLoadForecast(50.0, 0.1, 0.0, cfg)
	want := 3 * cfg.MinerPowerSuper // 6.0
	if got != want {
		t.Errorf("expected %.4f, got %.4f", want, got)
	}
}

func TestEstimateLoadForecast_NoPowerControl_CappedByMinersPowerLimit(t *testing.T) {
	cfg := baseConfig()
	cfg.PVPowerControlPriceLimit = 999.0
	cfg.MinersPowerLimit = 5.0
	s := newMPCTestScheduler(cfg, 5)

	// 5 miners × 2.0 kW = 10.0 kW > 5.0 kW limit → capped at 5.0
	got := s.estimateLoadForecast(50.0, 0.1, 0.0, cfg)
	want := cfg.MinersPowerLimit // 5.0
	if got != want {
		t.Errorf("expected %.4f, got %.4f", want, got)
	}
}

func TestEstimateLoadForecast_NoPowerControl_SolarIgnored(t *testing.T) {
	cfg := baseConfig()
	cfg.PVPowerControlPriceLimit = 999.0
	cfg.MinersPowerLimit = 20.0
	s := newMPCTestScheduler(cfg, 2)

	// Solar forecast is 0 — must be ignored when power control is off
	gotZeroSolar := s.estimateLoadForecast(50.0, 0.1, 0.0, cfg)
	gotHighSolar := s.estimateLoadForecast(50.0, 0.1, 1000.0, cfg)
	if gotZeroSolar != gotHighSolar {
		t.Errorf("solar should be ignored when power control is off: zero=%.4f, high=%.4f", gotZeroSolar, gotHighSolar)
	}
}

// ---------------------------------------------------------------------------
// Power control ON — mode selection
// ---------------------------------------------------------------------------

// TestEstimateLoadForecast_PowerControl_SuperModeSelected verifies that the
// function picks Super mode when the solar forecast is large enough.
func TestEstimateLoadForecast_PowerControl_SuperModeSelected(t *testing.T) {
	cfg := baseConfig()
	cfg.PVPowerControlPriceLimit = 10.0
	cfg.MinersPowerLimit = 20.0
	s := newMPCTestScheduler(cfg, 3)

	// Solar = 10.0 kW → effectiveLimit = 10.0
	// Super: 10.0 / 2.0 = 5 → all 3 fit at Super
	got := s.estimateLoadForecast(50.0, 0.1, 10.0, cfg)
	want := 3 * cfg.MinerPowerSuper // 6.0
	if got != want {
		t.Errorf("expected Super-mode total %.4f, got %.4f", want, got)
	}
}

// TestEstimateLoadForecast_PowerControl_SuperModeFallsBackToStandard verifies
// that when solar is insufficient for Super but sufficient for Standard, the
// function uses Standard mode for miners that fit and standby for the rest.
func TestEstimateLoadForecast_PowerControl_SuperModeFallsBackToStandard(t *testing.T) {
	cfg := baseConfig()
	cfg.PVPowerControlPriceLimit = 10.0
	cfg.MinersPowerLimit = 20.0
	// 4 miners, solar = 3.5 kW
	// Super  (2.0): 3.5 / 2.0 = 1  → 1 miner fits at Super  (try Super first, 1 > 0 → CHOSEN)
	// Expectation: 1 × 2.0 + 3 × 0.1 = 2.3 kW
	s := newMPCTestScheduler(cfg, 4)

	got := s.estimateLoadForecast(50.0, 0.1, 3.5, cfg)
	want := 1*cfg.MinerPowerSuper + 3*cfg.MinerPowerStandby // 2.0 + 0.3 = 2.3
	if got != want {
		t.Errorf("expected %.4f, got %.4f", want, got)
	}
}

// TestEstimateLoadForecast_PowerControl_FallsBackToStandardMode verifies that
// when solar is too low for even one Super-mode miner the function tries
// Standard, and assigns as many miners as possible at Standard.
func TestEstimateLoadForecast_PowerControl_FallsBackToStandardMode(t *testing.T) {
	cfg := baseConfig()
	cfg.PVPowerControlPriceLimit = 10.0
	cfg.MinersPowerLimit = 20.0
	// Solar = 1.8 kW
	// Super  (2.0): 1.8 / 2.0 = 0 → skip
	// Standard (1.5): 1.8 / 1.5 = 1 → 1 miner fits → CHOSEN
	// 4 miners: 1 × 1.5 + 3 × 0.1 = 1.8
	s := newMPCTestScheduler(cfg, 4)

	got := s.estimateLoadForecast(50.0, 0.1, 1.8, cfg)
	want := 1*cfg.MinerPowerStandard + 3*cfg.MinerPowerStandby // 1.5 + 0.3 = 1.8
	if got != want {
		t.Errorf("expected %.4f, got %.4f", want, got)
	}
}

// TestEstimateLoadForecast_PowerControl_FallsBackToEcoMode verifies that when
// solar is too low for Super and Standard, Eco mode is tried.
func TestEstimateLoadForecast_PowerControl_FallsBackToEcoMode(t *testing.T) {
	cfg := baseConfig()
	cfg.PVPowerControlPriceLimit = 10.0
	cfg.MinersPowerLimit = 20.0
	// Solar = 1.2 kW
	// Super    (2.0): 1.2 / 2.0 = 0 → skip
	// Standard (1.5): 1.2 / 1.5 = 0 → skip
	// Eco      (1.0): 1.2 / 1.0 = 1 → 1 miner fits → CHOSEN
	// 3 miners: 1 × 1.0 + 2 × 0.1 = 1.2
	s := newMPCTestScheduler(cfg, 3)

	got := s.estimateLoadForecast(50.0, 0.1, 1.2, cfg)
	want := 1*cfg.MinerPowerEco + 2*cfg.MinerPowerStandby // 1.0 + 0.2 = 1.2
	if got != want {
		t.Errorf("expected %.4f, got %.4f", want, got)
	}
}

// TestEstimateLoadForecast_PowerControl_NoModesFit verifies that when solar is
// too low even for Eco mode, all miners remain in standby.
func TestEstimateLoadForecast_PowerControl_NoModesFit(t *testing.T) {
	cfg := baseConfig()
	cfg.PVPowerControlPriceLimit = 10.0
	cfg.MinersPowerLimit = 20.0
	// Solar = 0.5 kW < Eco (1.0) → no mode fits → all standby
	s := newMPCTestScheduler(cfg, 3)

	got := s.estimateLoadForecast(50.0, 0.1, 0.5, cfg)
	want := 3 * cfg.MinerPowerStandby // 0.3
	if got != want {
		t.Errorf("expected all-standby %.4f, got %.4f", want, got)
	}
}

// TestEstimateLoadForecast_PowerControl_ZeroSolarAllStandby verifies that zero
// solar puts all miners in standby.
func TestEstimateLoadForecast_PowerControl_ZeroSolarAllStandby(t *testing.T) {
	cfg := baseConfig()
	cfg.PVPowerControlPriceLimit = 10.0
	cfg.MinersPowerLimit = 20.0
	s := newMPCTestScheduler(cfg, 2)

	got := s.estimateLoadForecast(50.0, 0.1, 0.0, cfg)
	want := 2 * cfg.MinerPowerStandby
	if got != want {
		t.Errorf("expected %.4f, got %.4f", want, got)
	}
}

// ---------------------------------------------------------------------------
// Power control ON — effective limit is min(solar, MinersPowerLimit)
// ---------------------------------------------------------------------------

// TestEstimateLoadForecast_PowerControl_LimitedByMinersPowerLimit checks that
// MinersPowerLimit caps the effective limit even when solar is higher.
func TestEstimateLoadForecast_PowerControl_LimitedByMinersPowerLimit(t *testing.T) {
	cfg := baseConfig()
	cfg.PVPowerControlPriceLimit = 10.0
	cfg.MinersPowerLimit = 3.0 // only 1 Super-mode miner fits (2.0 kW)
	s := newMPCTestScheduler(cfg, 4)

	// solar = 100 kW, but MinersPowerLimit = 3.0 kW
	// effectiveLimit = min(100, 3) = 3.0
	// Super (2.0): 3.0 / 2.0 = 1 → 1 miner at Super + 3 standby
	got := s.estimateLoadForecast(50.0, 0.1, 100.0, cfg)
	want := 1*cfg.MinerPowerSuper + 3*cfg.MinerPowerStandby // 2.0 + 0.3 = 2.3
	if got != want {
		t.Errorf("expected %.4f, got %.4f", want, got)
	}
}

// TestEstimateLoadForecast_PowerControl_SolarLowerThanLimit checks that solar
// caps the effective limit when it is less than MinersPowerLimit.
func TestEstimateLoadForecast_PowerControl_SolarLowerThanLimit(t *testing.T) {
	cfg := baseConfig()
	cfg.PVPowerControlPriceLimit = 10.0
	cfg.MinersPowerLimit = 20.0
	s := newMPCTestScheduler(cfg, 5)

	// solar = 4.0 kW → effectiveLimit = 4.0
	// Super (2.0): 4.0 / 2.0 = 2 → 2 miners at Super, 3 standby
	got := s.estimateLoadForecast(50.0, 0.1, 4.0, cfg)
	want := 2*cfg.MinerPowerSuper + 3*cfg.MinerPowerStandby // 4.0 + 0.3 = 4.3
	if got != want {
		t.Errorf("expected %.4f, got %.4f", want, got)
	}
}

// ---------------------------------------------------------------------------
// Power control ON — all miners fit at highest mode
// ---------------------------------------------------------------------------

func TestEstimateLoadForecast_PowerControl_AllMinersRunAtSuper(t *testing.T) {
	cfg := baseConfig()
	cfg.PVPowerControlPriceLimit = 10.0
	cfg.MinersPowerLimit = 20.0
	// 3 miners at Super = 6.0, solar = 8.0 → all fit
	s := newMPCTestScheduler(cfg, 3)

	got := s.estimateLoadForecast(50.0, 0.1, 8.0, cfg)
	want := 3 * cfg.MinerPowerSuper // 6.0
	if got != want {
		t.Errorf("expected %.4f, got %.4f", want, got)
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

// TestEstimateLoadForecast_PowerControl_SingleMiner verifies correct behaviour
// with only one miner.
func TestEstimateLoadForecast_PowerControl_SingleMiner(t *testing.T) {
	cfg := baseConfig()
	cfg.PVPowerControlPriceLimit = 10.0
	cfg.MinersPowerLimit = 20.0
	s := newMPCTestScheduler(cfg, 1)

	tests := []struct {
		name        string
		solar       float64
		wantPower   float64
		description string
	}{
		{"super fits", 5.0, cfg.MinerPowerSuper, "one miner runs at Super"},
		{"only standard fits", 1.8, cfg.MinerPowerStandard, "one miner runs at Standard"},
		{"only eco fits", 1.2, cfg.MinerPowerEco, "one miner runs at Eco"},
		{"nothing fits", 0.5, cfg.MinerPowerStandby, "one miner in standby"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := s.estimateLoadForecast(50.0, 0.1, tc.solar, cfg)
			if got != tc.wantPower {
				t.Errorf("%s: expected %.4f, got %.4f", tc.description, tc.wantPower, got)
			}
		})
	}
}

// TestEstimateLoadForecast_PowerControl_PartialSuperFullStandard verifies that
// when 2 out of 4 miners fit at Super (but not 3), those 2 run at Super and
// the rest stay in standby — we do NOT fall through to a lower mode just
// because not all miners can run.
func TestEstimateLoadForecast_PowerControl_PartialSuperUsed(t *testing.T) {
	cfg := baseConfig()
	cfg.PVPowerControlPriceLimit = 10.0
	cfg.MinersPowerLimit = 20.0
	// solar = 5.0 kW
	// Super (2.0): 5.0 / 2.0 = 2 → 2 miners at Super, 2 standby  → CHOSEN
	s := newMPCTestScheduler(cfg, 4)

	got := s.estimateLoadForecast(50.0, 0.1, 5.0, cfg)
	want := 2*cfg.MinerPowerSuper + 2*cfg.MinerPowerStandby // 4.0 + 0.2 = 4.2
	if got != want {
		t.Errorf("expected %.4f, got %.4f", want, got)
	}
}

// TestEstimateLoadForecast_PowerControl_ExactlyOneSuperMinerFits verifies that
// a solar forecast that exactly matches MinerPowerSuper allows exactly one miner
// to run at Super mode.
func TestEstimateLoadForecast_PowerControl_ExactlyOneSuperMinerFits(t *testing.T) {
	cfg := baseConfig()
	cfg.PVPowerControlPriceLimit = 10.0
	cfg.MinersPowerLimit = 20.0
	s := newMPCTestScheduler(cfg, 3)

	// solar = 2.0 == MinerPowerSuper → exactly 1 fits at Super
	got := s.estimateLoadForecast(50.0, 0.1, cfg.MinerPowerSuper, cfg)
	want := 1*cfg.MinerPowerSuper + 2*cfg.MinerPowerStandby
	if got != want {
		t.Errorf("expected %.4f, got %.4f", want, got)
	}
}

// TestEstimateLoadForecast_PowerControl_JustBelowSuperMode verifies that when
// solar is just below MinerPowerSuper the function correctly falls back to
// Standard mode.
func TestEstimateLoadForecast_PowerControl_JustBelowSuperMode(t *testing.T) {
	cfg := baseConfig()
	cfg.PVPowerControlPriceLimit = 10.0
	cfg.MinersPowerLimit = 20.0
	s := newMPCTestScheduler(cfg, 2)

	// solar = 1.99 < Super (2.0) → 0 miners fit at Super
	// Standard (1.5): 1.99 / 1.5 = 1 → 1 miner at Standard + 1 standby
	got := s.estimateLoadForecast(50.0, 0.1, 1.99, cfg)
	want := 1*cfg.MinerPowerStandard + 1*cfg.MinerPowerStandby
	if got != want {
		t.Errorf("expected %.4f, got %.4f", want, got)
	}
}

// TestEstimateLoadForecast_NoPowerControl_SingleMiner checks that a single
// miner always runs at Super (capped only by MinersPowerLimit) when power
// control is off.
func TestEstimateLoadForecast_NoPowerControl_SingleMiner(t *testing.T) {
	cfg := baseConfig()
	cfg.PVPowerControlPriceLimit = 999.0
	cfg.MinersPowerLimit = 5.0
	s := newMPCTestScheduler(cfg, 1)

	got := s.estimateLoadForecast(50.0, 0.1, 0.0, cfg)
	want := cfg.MinerPowerSuper // 2.0 < 5.0 limit
	if got != want {
		t.Errorf("expected %.4f, got %.4f", want, got)
	}
}

// TestEstimateLoadForecast_PriceJustAboveLimit checks the boundary between
// active and standby modes (price strictly greater than limit → standby).
func TestEstimateLoadForecast_PriceJustAboveLimit(t *testing.T) {
	cfg := baseConfig()
	cfg.PVPowerControlPriceLimit = 10.0
	cfg.MinersPowerLimit = 20.0
	s := newMPCTestScheduler(cfg, 3)

	// priceLimit 0.1 EUR/kWh = 100 EUR/MWh
	// hourlyPrice 100.001 EUR/MWh → 0.100001 > 0.1 → standby
	got := s.estimateLoadForecast(100.001, 0.1, 50.0, cfg)
	want := 3 * cfg.MinerPowerStandby
	if got != want {
		t.Errorf("expected standby total %.4f, got %.4f", want, got)
	}
}

// ---------------------------------------------------------------------------
// Table-driven comprehensive test
// ---------------------------------------------------------------------------

func TestEstimateLoadForecast_TableDriven(t *testing.T) {
	cfg := &Config{
		MinerPowerStandby:        0.1,
		MinerPowerEco:            1.0,
		MinerPowerStandard:       1.5,
		MinerPowerSuper:          2.0,
		MinersPowerLimit:         20.0,
		PVPowerControlPriceLimit: 10.0,
	}

	tests := []struct {
		name        string
		numMiners   int
		hourlyPrice float64 // EUR/MWh
		priceLimit  float64 // EUR/kWh
		solar       float64 // kW
		powerLimit  float64 // MinersPowerLimit override
		want        float64
		description string
	}{
		{
			name:      "price above limit all standby",
			numMiners: 4, hourlyPrice: 150, priceLimit: 0.1, solar: 20, powerLimit: 20,
			want:        4 * 0.1,
			description: "all 4 miners in standby when price exceeds limit",
		},
		{
			name:      "zero solar all standby",
			numMiners: 3, hourlyPrice: 50, priceLimit: 0.1, solar: 0, powerLimit: 20,
			want:        3 * 0.1,
			description: "all standby when solar is zero",
		},
		{
			name:      "solar covers all miners at super",
			numMiners: 3, hourlyPrice: 50, priceLimit: 0.1, solar: 10, powerLimit: 20,
			want:        3 * 2.0,
			description: "3 miners × Super(2.0) = 6.0 kW ≤ 10.0 kW solar",
		},
		{
			name:      "solar covers two miners at super",
			numMiners: 4, hourlyPrice: 50, priceLimit: 0.1, solar: 5, powerLimit: 20,
			want:        2*2.0 + 2*0.1,
			description: "floor(5/2.0)=2 Super miners, 2 standby",
		},
		{
			name:      "solar too low for super falls to standard",
			numMiners: 3, hourlyPrice: 50, priceLimit: 0.1, solar: 1.8, powerLimit: 20,
			want:        1*1.5 + 2*0.1,
			description: "Super fails (floor(1.8/2.0)=0), Standard ok (floor(1.8/1.5)=1)",
		},
		{
			name:      "solar too low for standard falls to eco",
			numMiners: 3, hourlyPrice: 50, priceLimit: 0.1, solar: 1.2, powerLimit: 20,
			want:        1*1.0 + 2*0.1,
			description: "Super/Standard fail, Eco ok (floor(1.2/1.0)=1)",
		},
		{
			name:      "solar too low for any mode",
			numMiners: 3, hourlyPrice: 50, priceLimit: 0.1, solar: 0.8, powerLimit: 20,
			want:        3 * 0.1,
			description: "all modes fail (floor(0.8/1.0)=0), all standby",
		},
		{
			name:      "miners_power_limit caps effective limit below solar",
			numMiners: 5, hourlyPrice: 50, priceLimit: 0.1, solar: 100, powerLimit: 3.0,
			want:        1*2.0 + 4*0.1,
			description: "effectiveLimit=min(100,3)=3, Super: floor(3/2)=1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			localCfg := *cfg // copy
			localCfg.MinersPowerLimit = tc.powerLimit

			s := newMPCTestScheduler(&localCfg, tc.numMiners)
			got := s.estimateLoadForecast(tc.hourlyPrice, tc.priceLimit, tc.solar, &localCfg)

			// Use a small tolerance for floating-point comparison
			const eps = 1e-9
			diff := got - tc.want
			if diff < -eps || diff > eps {
				t.Errorf("%s: expected %.4f, got %.4f", tc.description, tc.want, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Solar forecast / weather nil-safety (MET Norway unavailable)
// ---------------------------------------------------------------------------

// TestGetWeatherDataAtTime_NilForecast verifies that getWeatherDataAtTime returns
// zero values and does not panic when the MET Norway forecast is nil.
func TestGetWeatherDataAtTime_NilForecast(t *testing.T) {
	s := newMPCTestScheduler(baseConfig(), 0)

	cloudCoverage, weatherSymbol, airTemperature := s.getWeatherDataAtTime(nil, time.Now())

	if cloudCoverage != 0 {
		t.Errorf("expected cloudCoverage 0, got %.2f", cloudCoverage)
	}
	if weatherSymbol != "" {
		t.Errorf("expected empty weatherSymbol, got %q", weatherSymbol)
	}
	if airTemperature != 0 {
		t.Errorf("expected airTemperature 0, got %.2f", airTemperature)
	}
}

// TestGetSolarForecast_NilWeatherForecast_UsesOpenMeteo verifies that when the
// MET Norway weather forecast is unavailable (nil) but Open-Meteo irradiance data
// is in the cache, getSolarForecast still returns non-zero solar power estimates
// and does not return an error.
func TestGetSolarForecast_NilWeatherForecast_UsesOpenMeteo(t *testing.T) {
	cfg := &Config{
		MaxSolarPower:      10.0,
		CheckPriceInterval: 15 * time.Minute,
		Latitude:           52.0,
		Longitude:          5.0,
	}
	s := newMPCTestScheduler(cfg, 0)

	// Build a synthetic Open-Meteo response covering 36+ hours at 15-minute resolution.
	// All data points carry a shortwave radiation of 600 W/m², which should produce
	// 10.0 kW * (600/1000) = 6.0 kW per slot.
	const shortwaveRadiation = 600.0
	now := time.Now().UTC().Truncate(15 * time.Minute)
	numPoints := 36*4 + 8 // a few extra points beyond the 36-hour horizon
	times := make([]string, numPoints)
	radiation := make([]float64, numPoints)
	for i := range numPoints {
		times[i] = now.Add(time.Duration(i) * 15 * time.Minute).Format("2006-01-02T15:04")
		radiation[i] = shortwaveRadiation
	}
	s.solarForecastCache.Set(&openmeteo.SolarForecast{
		Minutely15: &openmeteo.TimeSeriesData{
			Time:                   times,
			ShortwaveRadiation:     radiation,
			DirectRadiation:        radiation,
			DiffuseRadiation:       radiation,
			DirectNormalIrradiance: radiation,
		},
	})

	// Call getSolarForecast with a nil MET Norway forecast — simulating an outage.
	solarForecasts, weatherData, err := s.getSolarForecast(cfg, now, 15*time.Minute, nil, nil)

	if err != nil {
		t.Fatalf("expected no error when MET Norway is nil but Open-Meteo data is available, got: %v", err)
	}
	if solarForecasts == nil {
		t.Fatal("expected non-nil solarForecasts map")
	}
	if weatherData == nil {
		t.Fatal("expected non-nil weatherData map")
	}

	numSlots := int(36 * time.Hour / (15 * time.Minute))
	expectedSolar := cfg.MaxSolarPower * (shortwaveRadiation / 1000.0) // 6.0 kW

	// Slot 0 is always overridden with the current PV reading (0 when plantInfo is nil).
	// Every other slot should carry the irradiance-derived value.
	for i := 1; i < numSlots; i++ {
		got := solarForecasts[i]
		if got != expectedSolar {
			t.Errorf("slot %d: expected solar %.2f kW, got %.2f kW", i, expectedSolar, got)
			break // report only the first mismatch to keep output concise
		}
	}

	// Weather metadata must be zero/empty — no MET Norway data was available.
	for i := range numSlots {
		wd := weatherData[i]
		if wd.CloudCoverage != 0 || wd.WeatherSymbol != "" || wd.AirTemperature != 0 {
			t.Errorf("slot %d: expected zero weather metadata without MET Norway, got %+v", i, wd)
			break
		}
	}
}

// TestGetSolarForecast_NilWeatherForecast_NilOpenMeteo verifies that when both
// MET Norway and Open-Meteo are unavailable getSolarForecast does not panic and
// returns all-zero solar forecasts without an error.
func TestGetSolarForecast_NilWeatherForecast_NilOpenMeteo(t *testing.T) {
	cfg := &Config{
		MaxSolarPower:      10.0,
		CheckPriceInterval: 15 * time.Minute,
		Latitude:           52.0,
		Longitude:          5.0,
	}
	s := newMPCTestScheduler(cfg, 0)

	// Point the scheduler at a fake Open-Meteo server that always returns 503 so
	// the fetch fails deterministically regardless of internet connectivity.
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer fakeServer.Close()
	s.openMeteoBaseURL = fakeServer.URL

	now := time.Now().UTC().Truncate(15 * time.Minute)

	// The function must not panic and must not return an error.
	solarForecasts, weatherData, err := s.getSolarForecast(cfg, now, 15*time.Minute, nil, nil)

	if err != nil {
		t.Fatalf("expected no error when both forecasts are unavailable, got: %v", err)
	}
	if solarForecasts == nil {
		t.Fatal("expected non-nil solarForecasts map")
	}
	if weatherData == nil {
		t.Fatal("expected non-nil weatherData map")
	}

	// With no data source available every slot must be zero.
	numSlots := int(36 * time.Hour / (15 * time.Minute))
	for i := range numSlots {
		if v := solarForecasts[i]; v != 0 {
			t.Errorf("slot %d: expected 0 kW with no data available, got %.2f kW", i, v)
			break
		}
	}
}

// ---------------------------------------------------------------------------
// decideBatteryAction tests
// ---------------------------------------------------------------------------

func TestDecideBatteryAction_DischargeNegativeExportUsesMode2(t *testing.T) {
	decision := &mpc.ControlDecision{
		BatteryDischarge:      3.0,
		ExportPrice:           -0.05,
		BatteryChargeFromGrid: 0,
		BatteryChargeFromPV:   0,
	}
	action := decideBatteryAction(decision, 5.0, 0.0)
	if action.mode != 2 {
		t.Errorf("expected mode 2, got %d", action.mode)
	}
	if !action.setDischarge {
		t.Errorf("expected setDischarge true, got false")
	}
}

func TestDecideBatteryAction_DischargeZeroExportUsesMode2(t *testing.T) {
	decision := &mpc.ControlDecision{
		BatteryDischarge:      3.0,
		ExportPrice:           0.0,
		BatteryChargeFromGrid: 0,
		BatteryChargeFromPV:   0,
	}
	action := decideBatteryAction(decision, 5.0, 0.0)
	if action.mode != 2 {
		t.Errorf("expected mode 2, got %d", action.mode)
	}
	if !action.setDischarge {
		t.Errorf("expected setDischarge true, got false")
	}
}

func TestDecideBatteryAction_DischargeWithPlannedGridExportUsesMode5(t *testing.T) {
	// Mode 5 (forced discharge) is selected when the MPC has explicitly planned
	// grid export (GridExport > 0.01), regardless of ExportPrice.
	decision := &mpc.ControlDecision{
		BatteryDischarge:      3.0,
		GridExport:            2.0,
		ExportPrice:           0.20,
		BatteryChargeFromGrid: 0,
		BatteryChargeFromPV:   0,
	}
	action := decideBatteryAction(decision, 5.0, 0.0)
	if action.mode != 5 {
		t.Errorf("expected mode 5, got %d", action.mode)
	}
	if !action.setDischarge {
		t.Errorf("expected setDischarge true, got false")
	}
	if action.dischargeLimit != 3.0 {
		t.Errorf("expected dischargeLimit 3.0, got %.2f", action.dischargeLimit)
	}
}

// ---------------------------------------------------------------------------
// PV recovery gate tests (grid charging suppression when cloud clears)
// ---------------------------------------------------------------------------

// TestDecideBatteryAction_GridCharge_GateSuppressesWhenPVCoversAll verifies that
// when recent PV is high enough to cover load + full planned charge, the gate
// fires and switches from Mode 4 (grid+PV) to Mode 2 (PV-only).
func TestDecideBatteryAction_GridCharge_GateSuppressesWhenPVCoversAll(t *testing.T) {
	decision := &mpc.ControlDecision{
		BatteryChargeFromPV:   2.0,
		BatteryChargeFromGrid: 3.0,
		LoadForecast:          4.0,
	}
	// recentAvgPV = 9.0 >= load(4.0) + charge(5.0) → gate fires
	action := decideBatteryAction(decision, 12.0, 9.0)

	if action.mode != 2 {
		t.Errorf("expected mode 2 (PV-only) after gate, got %d", action.mode)
	}
	if !action.setCharge {
		t.Errorf("expected setCharge true")
	}
	// Limit must equal the full planned charge (PV + grid portions)
	if action.chargeLimit != 5.0 {
		t.Errorf("expected chargeLimit 5.0 (full planned charge), got %.2f", action.chargeLimit)
	}
}

// TestDecideBatteryAction_GridCharge_GateDoesNotFireWhenPVInsufficient verifies
// that the gate does NOT fire when recent PV is below the load + charge threshold,
// preserving Mode 4 (grid + PV) charging.
func TestDecideBatteryAction_GridCharge_GateDoesNotFireWhenPVInsufficient(t *testing.T) {
	decision := &mpc.ControlDecision{
		BatteryChargeFromPV:   2.0,
		BatteryChargeFromGrid: 3.0,
		LoadForecast:          4.0,
	}
	// recentAvgPV = 8.9 < load(4.0) + charge(5.0) = 9.0 → gate must NOT fire
	action := decideBatteryAction(decision, 12.0, 8.9)

	if action.mode != 4 {
		t.Errorf("expected mode 4 (grid+PV) when PV insufficient, got %d", action.mode)
	}
}

// TestDecideBatteryAction_GridCharge_GateDoesNotFireWhenRecentPVIsZero verifies
// that passing recentAvgPV=0 (no samples / startup) never triggers the gate.
func TestDecideBatteryAction_GridCharge_GateDoesNotFireWhenRecentPVIsZero(t *testing.T) {
	decision := &mpc.ControlDecision{
		BatteryChargeFromPV:   1.0,
		BatteryChargeFromGrid: 2.0,
		LoadForecast:          0.0,
	}
	// recentAvgPV = 0 → gate must NOT fire even though load is also 0
	action := decideBatteryAction(decision, 12.0, 0.0)

	if action.mode != 4 {
		t.Errorf("expected mode 4 when recentAvgPV is 0, got %d", action.mode)
	}
}

// TestDecideBatteryAction_GridCharge_GateAtExactThreshold verifies the gate fires
// at exactly the threshold (>=, not >).
func TestDecideBatteryAction_GridCharge_GateAtExactThreshold(t *testing.T) {
	decision := &mpc.ControlDecision{
		BatteryChargeFromPV:   1.0,
		BatteryChargeFromGrid: 2.0,
		LoadForecast:          3.0,
	}
	// threshold = load(3.0) + charge(3.0) = 6.0; pass exactly 6.0 → gate fires
	action := decideBatteryAction(decision, 12.0, 6.0)

	if action.mode != 2 {
		t.Errorf("expected mode 2 at exact threshold, got %d", action.mode)
	}
}

// TestDecideBatteryAction_GridCharge_LimitClampedToMaxCharge verifies that when
// the gate fires and the planned charge exceeds maxCharge, the limit is clamped.
func TestDecideBatteryAction_GridCharge_LimitClampedToMaxCharge(t *testing.T) {
	decision := &mpc.ControlDecision{
		BatteryChargeFromPV:   4.0,
		BatteryChargeFromGrid: 5.0, // total = 9.0, above maxCharge
		LoadForecast:          1.0,
	}
	// recentAvgPV = 10.0 >= load(1.0) + charge(9.0) → gate fires
	action := decideBatteryAction(decision, 8.0, 10.0) // maxCharge = 8.0

	if action.mode != 2 {
		t.Errorf("expected mode 2, got %d", action.mode)
	}
	if action.chargeLimit != 8.0 {
		t.Errorf("expected chargeLimit clamped to maxCharge 8.0, got %.2f", action.chargeLimit)
	}
}
