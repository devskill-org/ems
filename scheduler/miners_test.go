package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/devskill-org/ems/miners"
)

// This file contains unit tests for the controlMiner function in miners.go.
//
// The controlMiner function manages the state and work mode of individual miners based on:
// - Fan speed (FanR) thresholds: high threshold triggers decrease, low threshold triggers increase
// - Power consumption limits: ensures total power stays within configured limits
// - Historical data: requires 5 history entries before increasing work mode
//
// Test coverage includes:
// - FanR-based work mode adjustments (increase/decrease)
// - Power limit enforcement
// - History validation for work mode increases
// - Edge cases (boundary values, standby states, custom thresholds)
// - Power consumption calculations across different states and modes

// Helper function to create a test scheduler with specific config
func newTestScheduler(cfg *Config) *MinerScheduler {
	if cfg == nil {
		cfg = &Config{
			FanRHighThreshold:  80,
			FanRLowThreshold:   50,
			MinerPowerStandby:  0.1,
			MinerPowerEco:      1.0,
			MinerPowerStandard: 1.5,
			MinerPowerSuper:    2.0,
			MinersPowerLimit:   10.0,
		}
	}
	return NewMinerScheduler(cfg, log.New(os.Stdout, "TEST: ", log.LstdFlags))
}

// Helper function to create a test miner with specific stats
func newTestMiner(fanR int, workMode miners.AvalonWorkMode, state miners.AvalonState, historyFanRValues []int) *miners.AvalonQHost {
	miner := &miners.AvalonQHost{
		Address: "192.168.1.100",
		Port:    4028,
		LastStats: &miners.AvalonLiteStats{
			FanR:     fanR,
			WorkMode: workMode,
			State:    state,
			HBITemp:  60,
			HBOTemp:  55,
			ITemp:    50,
		},
		LiteStatsHistory: make([]*miners.AvalonLiteStats, 0),
	}

	// Add history if provided
	for _, histFanR := range historyFanRValues {
		miner.LiteStatsHistory = append(miner.LiteStatsHistory, &miners.AvalonLiteStats{
			FanR:     histFanR,
			WorkMode: workMode,
			State:    state,
		})
	}

	return miner
}

func TestControlMiner_HighFanR_DecreasesWorkMode(t *testing.T) {
	tests := []struct {
		name             string
		currentWorkMode  miners.AvalonWorkMode
		currentState     miners.AvalonState
		fanR             int
		totalPower       float64
		effectiveLimit   float64
		expectedState    miners.AvalonState
		expectedWorkMode miners.AvalonWorkMode
		description      string
	}{
		{
			name:             "Super to Standard when FanR high",
			currentWorkMode:  miners.AvalonSuperMode,
			currentState:     miners.AvalonStateMining,
			fanR:             85,
			totalPower:       8.0,
			effectiveLimit:   10.0,
			expectedState:    miners.AvalonStateMining,
			expectedWorkMode: miners.AvalonStandardMode,
			description:      "Should decrease from Super to Standard when FanR > high threshold",
		},
		{
			name:             "Standard to Eco when FanR high",
			currentWorkMode:  miners.AvalonStandardMode,
			currentState:     miners.AvalonStateMining,
			fanR:             90,
			totalPower:       5.0,
			effectiveLimit:   10.0,
			expectedState:    miners.AvalonStateMining,
			expectedWorkMode: miners.AvalonEcoMode,
			description:      "Should decrease from Standard to Eco when FanR > high threshold",
		},
		{
			name:             "Eco to Standby when FanR high",
			currentWorkMode:  miners.AvalonEcoMode,
			currentState:     miners.AvalonStateMining,
			fanR:             95,
			totalPower:       3.0,
			effectiveLimit:   10.0,
			expectedState:    miners.AvalonStateStandBy,
			expectedWorkMode: miners.AvalonEcoMode,
			description:      "Should go to Standby when FanR high and already at Eco mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheduler := newTestScheduler(nil)
			miner := newTestMiner(tt.fanR, tt.currentWorkMode, tt.currentState, nil)

			newState, newMode := scheduler.controlMiner(miner, tt.totalPower, tt.effectiveLimit)

			if newState != tt.expectedState {
				t.Errorf("%s: expected state %v, got %v", tt.description, tt.expectedState, newState)
			}
			if newMode != tt.expectedWorkMode {
				t.Errorf("%s: expected work mode %v, got %v", tt.description, tt.expectedWorkMode, newMode)
			}
		})
	}
}

func TestControlMiner_PowerLimitExceeded_DecreasesWorkMode(t *testing.T) {
	tests := []struct {
		name             string
		currentWorkMode  miners.AvalonWorkMode
		currentState     miners.AvalonState
		fanR             int
		totalPower       float64
		effectiveLimit   float64
		expectedState    miners.AvalonState
		expectedWorkMode miners.AvalonWorkMode
	}{
		{
			name:             "Super mode exceeds power limit",
			currentWorkMode:  miners.AvalonSuperMode,
			currentState:     miners.AvalonStateMining,
			fanR:             60,
			totalPower:       10.5,
			effectiveLimit:   10.0,
			expectedState:    miners.AvalonStateMining,
			expectedWorkMode: miners.AvalonStandardMode,
		},
		{
			name:             "Standard mode exceeds power limit significantly",
			currentWorkMode:  miners.AvalonStandardMode,
			currentState:     miners.AvalonStateMining,
			fanR:             60,
			totalPower:       12.0,
			effectiveLimit:   10.0,
			expectedState:    miners.AvalonStateStandBy,
			expectedWorkMode: miners.AvalonEcoMode,
		},
		{
			name:             "Eco mode exceeds power limit",
			currentWorkMode:  miners.AvalonEcoMode,
			currentState:     miners.AvalonStateMining,
			fanR:             60,
			totalPower:       10.5,
			effectiveLimit:   10.0,
			expectedState:    miners.AvalonStateStandBy,
			expectedWorkMode: miners.AvalonEcoMode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheduler := newTestScheduler(nil)
			miner := newTestMiner(tt.fanR, tt.currentWorkMode, tt.currentState, nil)

			newState, newMode := scheduler.controlMiner(miner, tt.totalPower, tt.effectiveLimit)

			if newState != tt.expectedState {
				t.Errorf("expected state %v, got %v", tt.expectedState, newState)
			}
			if newMode != tt.expectedWorkMode {
				t.Errorf("expected work mode %v, got %v", tt.expectedWorkMode, newMode)
			}
		})
	}
}

func TestControlMiner_LowFanR_IncreasesWorkMode(t *testing.T) {
	tests := []struct {
		name              string
		currentWorkMode   miners.AvalonWorkMode
		currentState      miners.AvalonState
		fanR              int
		historyFanRValues []int
		totalPower        float64
		effectiveLimit    float64
		expectedState     miners.AvalonState
		expectedWorkMode  miners.AvalonWorkMode
		description       string
	}{
		{
			name:              "Eco to Standard when FanR low with sufficient history",
			currentWorkMode:   miners.AvalonEcoMode,
			currentState:      miners.AvalonStateMining,
			fanR:              40,
			historyFanRValues: []int{40, 42, 38, 45, 43},
			totalPower:        5.0,
			effectiveLimit:    10.0,
			expectedState:     miners.AvalonStateMining,
			expectedWorkMode:  miners.AvalonStandardMode,
			description:       "Should increase from Eco to Standard when FanR < low threshold with history",
		},
		{
			name:              "Standard to Super when FanR low with sufficient history",
			currentWorkMode:   miners.AvalonStandardMode,
			currentState:      miners.AvalonStateMining,
			fanR:              35,
			historyFanRValues: []int{35, 37, 33, 40, 38},
			totalPower:        6.0,
			effectiveLimit:    10.0,
			expectedState:     miners.AvalonStateMining,
			expectedWorkMode:  miners.AvalonSuperMode,
			description:       "Should increase from Standard to Super when FanR < low threshold with history",
		},
		{
			name:              "No increase when already at Super mode",
			currentWorkMode:   miners.AvalonSuperMode,
			currentState:      miners.AvalonStateMining,
			fanR:              30,
			historyFanRValues: []int{30, 32, 28, 35, 33},
			totalPower:        7.0,
			effectiveLimit:    10.0,
			expectedState:     miners.AvalonStateMining,
			expectedWorkMode:  miners.AvalonSuperMode,
			description:       "Should not change when already at Super mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheduler := newTestScheduler(nil)
			miner := newTestMiner(tt.fanR, tt.currentWorkMode, tt.currentState, tt.historyFanRValues)

			newState, newMode := scheduler.controlMiner(miner, tt.totalPower, tt.effectiveLimit)

			if newState != tt.expectedState {
				t.Errorf("%s: expected state %v, got %v", tt.description, tt.expectedState, newState)
			}
			if newMode != tt.expectedWorkMode {
				t.Errorf("%s: expected work mode %v, got %v", tt.description, tt.expectedWorkMode, newMode)
			}
		})
	}
}

func TestControlMiner_LowFanR_NoIncreaseWithInsufficientHistory(t *testing.T) {
	tests := []struct {
		name              string
		currentWorkMode   miners.AvalonWorkMode
		historyFanRValues []int
		description       string
	}{
		{
			name:              "No history",
			currentWorkMode:   miners.AvalonEcoMode,
			historyFanRValues: []int{},
			description:       "Should not increase work mode with empty history",
		},
		{
			name:              "Insufficient history (less than 5)",
			currentWorkMode:   miners.AvalonEcoMode,
			historyFanRValues: []int{40, 42, 38},
			description:       "Should not increase work mode with only 3 history entries",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheduler := newTestScheduler(nil)
			miner := newTestMiner(40, tt.currentWorkMode, miners.AvalonStateMining, tt.historyFanRValues)

			newState, newMode := scheduler.controlMiner(miner, 5.0, 10.0)

			if newState != miners.AvalonStateMining {
				t.Errorf("%s: expected state %v, got %v", tt.description, miners.AvalonStateMining, newState)
			}
			if newMode != tt.currentWorkMode {
				t.Errorf("%s: expected work mode to remain %v, got %v", tt.description, tt.currentWorkMode, newMode)
			}
		})
	}
}

func TestControlMiner_LowFanR_NoIncreaseWhenHistoryHasHighValues(t *testing.T) {
	scheduler := newTestScheduler(nil)

	// Current FanR is low, but history contains a high value
	miner := newTestMiner(40, miners.AvalonEcoMode, miners.AvalonStateMining, []int{40, 42, 60, 45, 43})

	newState, newMode := scheduler.controlMiner(miner, 5.0, 10.0)

	// Should not increase because one history value (60) is >= FanRLowThreshold (50)
	if newState != miners.AvalonStateMining {
		t.Errorf("expected state %v, got %v", miners.AvalonStateMining, newState)
	}
	if newMode != miners.AvalonEcoMode {
		t.Errorf("expected work mode to remain %v, got %v", miners.AvalonEcoMode, newMode)
	}
}

func TestControlMiner_LowFanR_NoIncreaseWhenPowerLimitWouldExceed(t *testing.T) {
	scheduler := newTestScheduler(nil)

	// Current FanR is low with good history, but increasing would exceed power limit
	miner := newTestMiner(40, miners.AvalonEcoMode, miners.AvalonStateMining, []int{40, 42, 38, 45, 43})

	newState, newMode := scheduler.controlMiner(miner, 9.6, 10.0)

	// Should not increase because new power (9.6 - 1.0 + 1.5 = 10.1) would be > effectiveLimit
	if newState != miners.AvalonStateMining {
		t.Errorf("expected state %v, got %v", miners.AvalonStateMining, newState)
	}
	if newMode != miners.AvalonEcoMode {
		t.Errorf("expected work mode to remain %v, got %v", miners.AvalonEcoMode, newMode)
	}
}

func TestControlMiner_NormalRange_NoChange(t *testing.T) {
	tests := []struct {
		name            string
		fanR            int
		currentWorkMode miners.AvalonWorkMode
		totalPower      float64
		effectiveLimit  float64
	}{
		{
			name:            "FanR in normal range",
			fanR:            60,
			currentWorkMode: miners.AvalonStandardMode,
			totalPower:      7.0,
			effectiveLimit:  10.0,
		},
		{
			name:            "FanR at low threshold",
			fanR:            50,
			currentWorkMode: miners.AvalonStandardMode,
			totalPower:      6.0,
			effectiveLimit:  10.0,
		},
		{
			name:            "FanR at high threshold",
			fanR:            80,
			currentWorkMode: miners.AvalonEcoMode,
			totalPower:      5.0,
			effectiveLimit:  10.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheduler := newTestScheduler(nil)
			miner := newTestMiner(tt.fanR, tt.currentWorkMode, miners.AvalonStateMining, nil)

			newState, newMode := scheduler.controlMiner(miner, tt.totalPower, tt.effectiveLimit)

			if newState != miners.AvalonStateMining {
				t.Errorf("expected state to remain %v, got %v", miners.AvalonStateMining, newState)
			}
			if newMode != tt.currentWorkMode {
				t.Errorf("expected work mode to remain %v, got %v", tt.currentWorkMode, newMode)
			}
		})
	}
}

func TestControlMiner_DecreaseWhenNewPowerWouldStillExceedLimit(t *testing.T) {
	scheduler := newTestScheduler(nil)

	// Eco mode, but even decreasing to standby won't get under limit
	// Total: 10.5, Eco power: 1.0, Standby power: 0.1
	// New total would be: 10.5 - 1.0 + 0.1 = 9.6 (still would need to check, but logic goes to standby)
	miner := newTestMiner(85, miners.AvalonEcoMode, miners.AvalonStateMining, nil)

	newState, newMode := scheduler.controlMiner(miner, 10.5, 10.0)

	// When at Eco and FanR is high or power exceeded, should go to Standby
	if newState != miners.AvalonStateStandBy {
		t.Errorf("expected state %v, got %v", miners.AvalonStateStandBy, newState)
	}
	if newMode != miners.AvalonEcoMode {
		t.Errorf("expected work mode %v, got %v", miners.AvalonEcoMode, newMode)
	}
}

func TestControlMiner_CustomThresholds(t *testing.T) {
	// Test with custom FanR thresholds
	cfg := &Config{
		FanRHighThreshold:  70,
		FanRLowThreshold:   40,
		MinerPowerStandby:  0.1,
		MinerPowerEco:      1.0,
		MinerPowerStandard: 1.5,
		MinerPowerSuper:    2.0,
		MinersPowerLimit:   10.0,
	}
	scheduler := newTestScheduler(cfg)

	t.Run("Decrease at custom high threshold", func(t *testing.T) {
		miner := newTestMiner(71, miners.AvalonSuperMode, miners.AvalonStateMining, nil)
		_, newMode := scheduler.controlMiner(miner, 8.0, 10.0)

		if newMode != miners.AvalonStandardMode {
			t.Errorf("expected work mode %v, got %v", miners.AvalonStandardMode, newMode)
		}
	})

	t.Run("Increase at custom low threshold", func(t *testing.T) {
		miner := newTestMiner(39, miners.AvalonEcoMode, miners.AvalonStateMining, []int{35, 36, 37, 38, 39})
		_, newMode := scheduler.controlMiner(miner, 5.0, 10.0)

		if newMode != miners.AvalonStandardMode {
			t.Errorf("expected work mode %v, got %v", miners.AvalonStandardMode, newMode)
		}
	})

	t.Run("No change between thresholds", func(t *testing.T) {
		miner := newTestMiner(55, miners.AvalonStandardMode, miners.AvalonStateMining, nil)
		newState, newMode := scheduler.controlMiner(miner, 6.0, 10.0)

		if newState != miners.AvalonStateMining || newMode != miners.AvalonStandardMode {
			t.Errorf("expected no change, got state %v mode %v", newState, newMode)
		}
	})
}

func TestControlMiner_EdgeCases(t *testing.T) {
	scheduler := newTestScheduler(nil)

	t.Run("Zero total power", func(t *testing.T) {
		miner := newTestMiner(40, miners.AvalonEcoMode, miners.AvalonStateMining, []int{40, 41, 42, 43, 44})
		_, newMode := scheduler.controlMiner(miner, 0.0, 10.0)

		// Should increase because power allows and FanR is low
		if newMode != miners.AvalonStandardMode {
			t.Errorf("expected work mode %v, got %v", miners.AvalonStandardMode, newMode)
		}
	})

	t.Run("Power exactly at limit", func(t *testing.T) {
		miner := newTestMiner(60, miners.AvalonStandardMode, miners.AvalonStateMining, nil)
		newState, newMode := scheduler.controlMiner(miner, 10.0, 10.0)

		// Should remain the same (FanR in normal range, power at limit but not over)
		if newState != miners.AvalonStateMining || newMode != miners.AvalonStandardMode {
			t.Errorf("expected no change, got state %v mode %v", newState, newMode)
		}
	})

	t.Run("Very high FanR", func(t *testing.T) {
		miner := newTestMiner(99, miners.AvalonSuperMode, miners.AvalonStateMining, nil)
		_, newMode := scheduler.controlMiner(miner, 5.0, 10.0)

		// Should decrease
		if newMode != miners.AvalonStandardMode {
			t.Errorf("expected work mode %v, got %v", miners.AvalonStandardMode, newMode)
		}
	})

	t.Run("Very low FanR", func(t *testing.T) {
		miner := newTestMiner(10, miners.AvalonEcoMode, miners.AvalonStateMining, []int{10, 11, 12, 13, 14})
		_, newMode := scheduler.controlMiner(miner, 3.0, 10.0)

		// Should increase
		if newMode != miners.AvalonStandardMode {
			t.Errorf("expected work mode %v, got %v", miners.AvalonStandardMode, newMode)
		}
	})
}

func TestControlMiner_StandbyState(t *testing.T) {
	scheduler := newTestScheduler(nil)

	t.Run("Standby state with high FanR", func(t *testing.T) {
		miner := newTestMiner(85, miners.AvalonEcoMode, miners.AvalonStateStandBy, nil)
		newState, _ := scheduler.controlMiner(miner, 5.0, 10.0)

		// When in standby, high FanR should still potentially trigger state change
		// But the function logic primarily applies to mining state
		if newState != miners.AvalonStateStandBy {
			t.Errorf("expected state %v, got %v", miners.AvalonStateStandBy, newState)
		}
	})
}

func TestRefreshMinersState_EvictsAfterConsecutiveErrors(t *testing.T) {
	cfg := &Config{
		FanRHighThreshold:         80,
		FanRLowThreshold:          50,
		MinerPowerStandby:         0.1,
		MinerPowerEco:             1.0,
		MinerPowerStandard:        1.5,
		MinerPowerSuper:           2.0,
		MinersPowerLimit:          10.0,
		MinerMaxConsecutiveErrors: 3,
	}
	scheduler := newTestScheduler(cfg)

	// Register a miner that always fails to refresh
	miner := &miners.AvalonQHost{
		Address:        "192.168.1.200",
		Port:           4028,
		LastStatsError: errors.New("connection refused"),
	}
	key := fmt.Sprintf("%s:%d", miner.Address, miner.Port)
	scheduler.discoveredMiners.Store(key, miner)

	ctx := context.Background()

	// First two calls: miner should still be present (below threshold)
	for i := 1; i < cfg.MinerMaxConsecutiveErrors; i++ {
		active := scheduler.refreshMinersState(ctx)
		if len(active) != 1 {
			t.Errorf("call %d: expected miner still active, got %d active miners", i, len(active))
		}
		if _, exists := scheduler.discoveredMiners.Load(key); !exists {
			t.Errorf("call %d: miner should still be in discoveredMiners", i)
		}
	}

	// Third call: miner reaches the threshold and should be evicted
	active := scheduler.refreshMinersState(ctx)
	if len(active) != 0 {
		t.Errorf("expected miner to be evicted, got %d active miners", len(active))
	}
	if _, exists := scheduler.discoveredMiners.Load(key); exists {
		t.Error("evicted miner should have been removed from discoveredMiners")
	}
}

func TestRefreshMinersState_ResetsErrorCountOnSuccess(t *testing.T) {
	// AddLiteStats (called by RefreshLiteStats) owns the ConsecutiveErrors counter.
	// Verify that a successful call resets it to zero and a failing call increments it.
	miner := &miners.AvalonQHost{
		Address:           "192.168.1.201",
		Port:              4028,
		ConsecutiveErrors: 2, // simulate two prior failures
	}

	// Successful stats call → counter must reset to zero.
	stats := &miners.AvalonLiteStats{State: miners.AvalonStateMining}
	miner.AddLiteStats(stats, nil)

	if miner.ConsecutiveErrors != 0 {
		t.Errorf("expected ConsecutiveErrors to be reset to 0 after success, got %d", miner.ConsecutiveErrors)
	}
	if miner.LastStatsError != nil {
		t.Errorf("expected LastStatsError to be nil after success, got %v", miner.LastStatsError)
	}

	// Failing stats call → counter must increment.
	miner.AddLiteStats(nil, errors.New("timeout"))
	if miner.ConsecutiveErrors != 1 {
		t.Errorf("expected ConsecutiveErrors to be 1 after first failure, got %d", miner.ConsecutiveErrors)
	}

	miner.AddLiteStats(nil, errors.New("timeout"))
	if miner.ConsecutiveErrors != 2 {
		t.Errorf("expected ConsecutiveErrors to be 2 after second failure, got %d", miner.ConsecutiveErrors)
	}

	// Another success → counter resets again.
	miner.AddLiteStats(stats, nil)
	if miner.ConsecutiveErrors != 0 {
		t.Errorf("expected ConsecutiveErrors to reset to 0 after recovery, got %d", miner.ConsecutiveErrors)
	}
}

func TestValidate_MinerMaxConsecutiveErrors(t *testing.T) {
	tests := []struct {
		name    string
		value   int
		wantErr bool
	}{
		{name: "zero is invalid", value: 0, wantErr: true},
		{name: "negative is invalid", value: -1, wantErr: true},
		{name: "one is valid", value: 1, wantErr: false},
		{name: "default three is valid", value: 3, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.SecurityToken = "test-token" // satisfy unrelated required fields
			cfg.MinerMaxConsecutiveErrors = tt.value
			err := cfg.Validate()
			hasFieldErr := err != nil && strings.Contains(err.Error(), "miner_max_consecutive_errors")
			if tt.wantErr && !hasFieldErr {
				t.Errorf("expected miner_max_consecutive_errors validation error for value=%d, got: %v", tt.value, err)
			}
			if !tt.wantErr && hasFieldErr {
				t.Errorf("expected no miner_max_consecutive_errors validation error for value=%d, got: %v", tt.value, err)
			}
		})
	}
}

func TestControlMiner_PowerCalculation(t *testing.T) {
	cfg := &Config{
		FanRHighThreshold:  80,
		FanRLowThreshold:   50,
		MinerPowerStandby:  0.2,
		MinerPowerEco:      1.2,
		MinerPowerStandard: 1.8,
		MinerPowerSuper:    2.5,
		MinersPowerLimit:   10.0,
	}
	scheduler := newTestScheduler(cfg)

	t.Run("Power calculation on decrease", func(t *testing.T) {
		// Current: Super (2.5kW), Total: 9.0kW
		// After decrease to Standard: 9.0 - 2.5 + 1.8 = 8.3kW (should fit)
		miner := newTestMiner(85, miners.AvalonSuperMode, miners.AvalonStateMining, nil)
		_, newMode := scheduler.controlMiner(miner, 9.0, 10.0)

		if newMode != miners.AvalonStandardMode {
			t.Errorf("expected work mode %v, got %v", miners.AvalonStandardMode, newMode)
		}
	})

	t.Run("Power calculation on increase", func(t *testing.T) {
		// Current: Eco (1.2kW), Total: 7.0kW
		// After increase to Standard: 7.0 - 1.2 + 1.8 = 7.6kW (should fit)
		miner := newTestMiner(40, miners.AvalonEcoMode, miners.AvalonStateMining, []int{40, 41, 42, 43, 44})
		_, newMode := scheduler.controlMiner(miner, 7.0, 10.0)

		if newMode != miners.AvalonStandardMode {
			t.Errorf("expected work mode %v, got %v", miners.AvalonStandardMode, newMode)
		}
	})
}
