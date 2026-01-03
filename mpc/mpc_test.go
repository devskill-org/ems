package mpc

import (
	"math"
	"testing"
)

func TestCalculateProfit(t *testing.T) {
	tests := []struct {
		name           string
		config         SystemConfig
		decision       ControlDecision
		slot           TimeSlot
		expectedProfit float64
		description    string
	}{
		{
			name: "Battery discharge to support load only - no export",
			config: SystemConfig{
				BatteryCapacity:        10.0,
				BatteryMaxCharge:       5.0,
				BatteryMaxDischarge:    5.0,
				BatteryMinSOC:          0.1,
				BatteryMaxSOC:          0.9,
				BatteryEfficiency:      0.9,
				BatteryDegradationCost: 0.01,
				MaxGridImport:          10.0,
				MaxGridExport:          10.0,
			},
			decision: ControlDecision{
				BatteryCharge:    0,
				BatteryDischarge: 3.0, // 3 kW discharge
				GridImport:       1.3, // Need to import some
				GridExport:       0,
			},
			slot: TimeSlot{
				ImportPrice:   0.30, // $0.30/kWh
				ExportPrice:   0.10, // $0.10/kWh
				SolarForecast: 1.0,  // 1 kW solar
				LoadForecast:  5.0,  // 5 kW load
			},
			// Revenue: 0 (no export)
			// Import cost: 1.3 * 0.30 = 0.39
			// Battery effective discharge: 3 * 0.9 = 2.7 kW
			// Load deficit from solar: 5 - 1 = 4 kW
			// Battery to load: min(2.7, 4) = 2.7 kW
			// Discharge value: 2.7 * 0.30 = 0.81
			// Charge loss: 0
			// Degradation: 3 * 0.01 = 0.03
			// Profit: 0 - 0.39 + 0.81 - 0 - 0.03 = 0.39
			expectedProfit: 0.39,
			description:    "Battery helps meet load, avoiding some imports",
		},
		{
			name: "Battery discharge with export - no double counting",
			config: SystemConfig{
				BatteryCapacity:        10.0,
				BatteryMaxCharge:       5.0,
				BatteryMaxDischarge:    5.0,
				BatteryMinSOC:          0.1,
				BatteryMaxSOC:          0.9,
				BatteryEfficiency:      0.9,
				BatteryDegradationCost: 0.01,
				MaxGridImport:          10.0,
				MaxGridExport:          10.0,
			},
			decision: ControlDecision{
				BatteryCharge:    0,
				BatteryDischarge: 5.0, // 5 kW discharge
				GridImport:       0,
				GridExport:       3.5, // Export excess
			},
			slot: TimeSlot{
				ImportPrice:   0.30, // $0.30/kWh
				ExportPrice:   0.10, // $0.10/kWh
				SolarForecast: 2.0,  // 2 kW solar
				LoadForecast:  3.0,  // 3 kW load
			},
			// Revenue: 3.5 * 0.10 = 0.35
			// Import cost: 0
			// Battery effective discharge: 5 * 0.9 = 4.5 kW
			// Load deficit from solar: 3 - 2 = 1 kW
			// Battery to load: min(4.5, 1) = 1 kW
			// Battery to export: 4.5 - 1 = 3.5 kW (already counted in revenue)
			// Discharge value: 1 * 0.30 = 0.30 (only the 1 kW to load)
			// Charge loss: 0
			// Degradation: 5 * 0.01 = 0.05
			// Profit: 0.35 - 0 + 0.30 - 0 - 0.05 = 0.60
			expectedProfit: 0.60,
			description:    "Battery discharge split between load and export - no double counting",
		},
		{
			name: "Battery charging from grid",
			config: SystemConfig{
				BatteryCapacity:        10.0,
				BatteryMaxCharge:       5.0,
				BatteryMaxDischarge:    5.0,
				BatteryMinSOC:          0.1,
				BatteryMaxSOC:          0.9,
				BatteryEfficiency:      0.9,
				BatteryDegradationCost: 0.01,
				MaxGridImport:          10.0,
				MaxGridExport:          10.0,
			},
			decision: ControlDecision{
				BatteryCharge:    4.0, // 4 kW charging
				BatteryDischarge: 0,
				GridImport:       6.444, // Load + charge losses
				GridExport:       0,
			},
			slot: TimeSlot{
				ImportPrice:   0.10, // $0.10/kWh (cheap - good time to charge)
				ExportPrice:   0.05, // $0.05/kWh
				SolarForecast: 0.5,  // 0.5 kW solar
				LoadForecast:  2.0,  // 2 kW load
			},
			// Revenue: 0
			// Import cost: 6.444 * 0.10 = 0.6444
			// Discharge value: 0
			// Charge loss: 4 * (1 - 0.9) * 0.10 = 4 * 0.1 * 0.10 = 0.04
			// Degradation: 4 * 0.01 = 0.04
			// Profit: 0 - 0.6444 + 0 - 0.04 - 0.04 = -0.7244
			expectedProfit: -0.7244,
			description:    "Charging battery incurs cost (future arbitrage opportunity)",
		},
		{
			name: "Solar export only - no battery",
			config: SystemConfig{
				BatteryCapacity:        10.0,
				BatteryMaxCharge:       5.0,
				BatteryMaxDischarge:    5.0,
				BatteryMinSOC:          0.1,
				BatteryMaxSOC:          0.9,
				BatteryEfficiency:      0.9,
				BatteryDegradationCost: 0.01,
				MaxGridImport:          10.0,
				MaxGridExport:          10.0,
			},
			decision: ControlDecision{
				BatteryCharge:    0,
				BatteryDischarge: 0,
				GridImport:       0,
				GridExport:       5.0, // Export excess solar
			},
			slot: TimeSlot{
				ImportPrice:   0.25,
				ExportPrice:   0.08,
				SolarForecast: 8.0, // 8 kW solar
				LoadForecast:  3.0, // 3 kW load
			},
			// Revenue: 5.0 * 0.08 = 0.40
			// Import cost: 0
			// Discharge value: 0
			// Charge loss: 0
			// Degradation: 0
			// Profit: 0.40 - 0 + 0 - 0 - 0 = 0.40
			expectedProfit: 0.40,
			description:    "Pure solar export scenario",
		},
		{
			name: "Grid import only - no solar, no battery",
			config: SystemConfig{
				BatteryCapacity:        10.0,
				BatteryMaxCharge:       5.0,
				BatteryMaxDischarge:    5.0,
				BatteryMinSOC:          0.1,
				BatteryMaxSOC:          0.9,
				BatteryEfficiency:      0.9,
				BatteryDegradationCost: 0.01,
				MaxGridImport:          10.0,
				MaxGridExport:          10.0,
			},
			decision: ControlDecision{
				BatteryCharge:    0,
				BatteryDischarge: 0,
				GridImport:       4.0,
				GridExport:       0,
			},
			slot: TimeSlot{
				ImportPrice:   0.35,
				ExportPrice:   0.10,
				SolarForecast: 0,   // No solar
				LoadForecast:  4.0, // 4 kW load
			},
			// Revenue: 0
			// Import cost: 4.0 * 0.35 = 1.40
			// Discharge value: 0
			// Charge loss: 0
			// Degradation: 0
			// Profit: 0 - 1.40 + 0 - 0 - 0 = -1.40
			expectedProfit: -1.40,
			description:    "Pure grid import scenario - highest cost",
		},
		{
			name: "Solar meets load exactly - no grid interaction",
			config: SystemConfig{
				BatteryCapacity:        10.0,
				BatteryMaxCharge:       5.0,
				BatteryMaxDischarge:    5.0,
				BatteryMinSOC:          0.1,
				BatteryMaxSOC:          0.9,
				BatteryEfficiency:      0.9,
				BatteryDegradationCost: 0.01,
				MaxGridImport:          10.0,
				MaxGridExport:          10.0,
			},
			decision: ControlDecision{
				BatteryCharge:    0,
				BatteryDischarge: 0,
				GridImport:       0,
				GridExport:       0,
			},
			slot: TimeSlot{
				ImportPrice:   0.30,
				ExportPrice:   0.10,
				SolarForecast: 5.0, // 5 kW solar
				LoadForecast:  5.0, // 5 kW load
			},
			// Revenue: 0
			// Import cost: 0
			// Discharge value: 0
			// Charge loss: 0
			// Degradation: 0
			// Profit: 0
			expectedProfit: 0.0,
			description:    "Perfect balance - no cost, no revenue",
		},
		{
			name: "High efficiency battery - 95%",
			config: SystemConfig{
				BatteryCapacity:        10.0,
				BatteryMaxCharge:       5.0,
				BatteryMaxDischarge:    5.0,
				BatteryMinSOC:          0.1,
				BatteryMaxSOC:          0.9,
				BatteryEfficiency:      0.95,  // Higher efficiency
				BatteryDegradationCost: 0.005, // Lower degradation
				MaxGridImport:          10.0,
				MaxGridExport:          10.0,
			},
			decision: ControlDecision{
				BatteryCharge:    0,
				BatteryDischarge: 4.0,
				GridImport:       0,
				GridExport:       2.8,
			},
			slot: TimeSlot{
				ImportPrice:   0.40,
				ExportPrice:   0.12,
				SolarForecast: 3.0,
				LoadForecast:  3.0,
			},
			// Revenue: 2.8 * 0.12 = 0.336
			// Import cost: 0
			// Battery effective discharge: 4 * 0.95 = 3.8 kW
			// Load deficit from solar: 3 - 3 = 0 kW
			// Battery to load: min(3.8, 0) = 0 kW (all goes to export)
			// Discharge value: 0 * 0.40 = 0
			// Charge loss: 0
			// Degradation: 4 * 0.005 = 0.02
			// Profit: 0.336 - 0 + 0 - 0 - 0.02 = 0.316
			expectedProfit: 0.316,
			description:    "All battery discharge goes to export when load is met by solar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mpc := &MPCController{
				Config: tt.config,
			}

			profit := mpc.calculateProfit(tt.decision, tt.slot)

			// Use a small epsilon for floating point comparison
			epsilon := 0.0001
			if math.Abs(profit-tt.expectedProfit) > epsilon {
				t.Errorf("%s\nExpected profit: %.4f, got: %.4f\nDifference: %.4f",
					tt.description, tt.expectedProfit, profit, profit-tt.expectedProfit)

				// Debug information
				t.Logf("Decision: Charge=%.2f, Discharge=%.2f, Import=%.2f, Export=%.2f",
					tt.decision.BatteryCharge, tt.decision.BatteryDischarge,
					tt.decision.GridImport, tt.decision.GridExport)
				t.Logf("Slot: Solar=%.2f, Load=%.2f, ImportPrice=%.2f, ExportPrice=%.2f",
					tt.slot.SolarForecast, tt.slot.LoadForecast,
					tt.slot.ImportPrice, tt.slot.ExportPrice)
			}
		})
	}
}

func TestCalculateProfitNoDegradation(t *testing.T) {
	// Test with zero degradation cost to verify other calculations
	config := SystemConfig{
		BatteryCapacity:        10.0,
		BatteryEfficiency:      0.9,
		BatteryDegradationCost: 0.0, // No degradation
	}

	mpc := &MPCController{
		Config: config,
	}

	decision := ControlDecision{
		BatteryCharge:    0,
		BatteryDischarge: 2.0,
		GridImport:       0.2,
		GridExport:       0,
	}

	slot := TimeSlot{
		ImportPrice:   0.50,
		ExportPrice:   0.15,
		SolarForecast: 1.0,
		LoadForecast:  3.0,
	}

	// Revenue: 0
	// Import cost: 0.2 * 0.50 = 0.10
	// Battery effective discharge: 2 * 0.9 = 1.8 kW
	// Load deficit: 3 - 1 = 2 kW
	// Battery to load: min(1.8, 2) = 1.8 kW
	// Discharge value: 1.8 * 0.50 = 0.90
	// Charge loss: 0
	// Degradation: 0
	// Profit: 0 - 0.10 + 0.90 - 0 - 0 = 0.80
	expectedProfit := 0.80

	profit := mpc.calculateProfit(decision, slot)

	epsilon := 0.0001
	if math.Abs(profit-expectedProfit) > epsilon {
		t.Errorf("Expected profit: %.4f, got: %.4f", expectedProfit, profit)
	}
}

func TestCalculateProfitArbitrage(t *testing.T) {
	// Test arbitrage scenario: charge when cheap, discharge when expensive
	config := SystemConfig{
		BatteryCapacity:        10.0,
		BatteryEfficiency:      0.9,
		BatteryDegradationCost: 0.01,
	}

	mpc := &MPCController{
		Config: config,
	}

	// Scenario 1: Charge during cheap prices
	chargeDecision := ControlDecision{
		BatteryCharge:    5.0,
		BatteryDischarge: 0,
		GridImport:       7.556, // Load + charge/efficiency
		GridExport:       0,
	}

	cheapSlot := TimeSlot{
		ImportPrice:   0.05, // Very cheap
		ExportPrice:   0.02,
		SolarForecast: 0,
		LoadForecast:  2.0,
	}

	chargeProfit := mpc.calculateProfit(chargeDecision, cheapSlot)

	// Should be negative (cost) but small due to cheap price
	if chargeProfit >= 0 {
		t.Errorf("Expected negative profit when charging, got: %.4f", chargeProfit)
	}

	// Scenario 2: Discharge during expensive prices
	dischargeDecision := ControlDecision{
		BatteryCharge:    0,
		BatteryDischarge: 4.0,
		GridImport:       0,
		GridExport:       0,
	}

	expensiveSlot := TimeSlot{
		ImportPrice:   0.50, // Very expensive
		ExportPrice:   0.15,
		SolarForecast: 0,
		LoadForecast:  3.6, // 4*0.9 = 3.6, so battery covers it exactly
	}

	dischargeProfit := mpc.calculateProfit(dischargeDecision, expensiveSlot)

	// Should be positive (saved money by not importing at expensive price)
	if dischargeProfit <= 0 {
		t.Errorf("Expected positive profit when discharging at high prices, got: %.4f", dischargeProfit)
	}

	// The discharge profit should be significantly higher than charge cost
	// (demonstrating arbitrage opportunity)
	if dischargeProfit <= math.Abs(chargeProfit) {
		t.Logf("Charge cost: %.4f, Discharge profit: %.4f", chargeProfit, dischargeProfit)
		t.Logf("Arbitrage spread may be too small for profitable trading")
	}
}
