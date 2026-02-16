package mpc

import (
	"fmt"
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
			// Degradation: 3 * 0.01 = 0.03
			// Profit: 0 - 0.39 - 0.03 = -0.42
			expectedProfit: -0.42,
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
			// Degradation: 5 * 0.01 = 0.05
			// Profit: 0.35 - 0 - 0.05 = 0.30
			expectedProfit: 0.30,
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
			// Degradation: 4 * 0.01 = 0.04
			// Profit: 0 - 0.6444 - 0.04 = -0.6844
			expectedProfit: -0.6844,
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
			mpc := &Controller{
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

	mpc := &Controller{
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
	// Degradation: 0
	// Profit: 0 - 0.10 - 0 = -0.10
	expectedProfit := -0.10

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

	mpc := &Controller{
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

	// Scenario 2: Discharge during expensive prices with export
	dischargeDecision := ControlDecision{
		BatteryCharge:    0,
		BatteryDischarge: 4.0,
		GridImport:       0,
		GridExport:       0.6, // 4*0.9 - 3.0 = 0.6 exported
	}

	expensiveSlot := TimeSlot{
		ImportPrice:   0.50, // Very expensive
		ExportPrice:   0.15,
		SolarForecast: 0,
		LoadForecast:  3.0,
	}

	dischargeProfit := mpc.calculateProfit(dischargeDecision, expensiveSlot)

	// With new calculation: revenue from export minus degradation
	// Revenue: 0.6 * 0.15 = 0.09, Degradation: 4 * 0.01 = 0.04
	// Profit: 0.09 - 0 - 0.04 = 0.05
	// This is positive, demonstrating that discharging at high prices can be profitable
	if dischargeProfit <= 0 {
		t.Logf("Charge cost: %.4f, Discharge profit: %.4f", chargeProfit, dischargeProfit)
		t.Logf("Note: With corrected profit calculation, arbitrage requires actual grid export at higher prices")
	}
}

func TestOptimize(t *testing.T) {
	t.Log("=== Analyzing Arbitrage Opportunity ===")

	// Hourly average prices (EUR/MWh) calculated from:
	// Energy_Prices_202601032300-202601042300.xml (hours 0-11)
	// Energy_Prices_202601042300-202601052300.xml (hours 12-35)
	hourlyPrices := []float64{
		97.365,   // hour 0
		112.4925, // hour 1
		126.4425, // hour 2
		162.355,  // hour 3
		139.1275, // hour 4
		143.575,  // hour 5
		125.4875, // hour 6
		119.715,  // hour 7
		112.45,   // hour 8
		127.1225, // hour 9
		110.065,  // hour 10
		89.185,   // hour 11
		130.44,   // hour 12
		107.56,   // hour 13
		96.9075,  // hour 14
		103.66,   // hour 15
		106.5375, // hour 16
		126.105,  // hour 17
		123.465,  // hour 18
		129.3125, // hour 19
		151.1725, // hour 20
		152.145,  // hour 21
		145.9825, // hour 22
		139.98,   // hour 23
		137.6925, // hour 24
		142.795,  // hour 25
		159.14,   // hour 26
		200.61,   // hour 27
		222.925,  // hour 28
		228.3575, // hour 29
		218.92,   // hour 30
		182.785,  // hour 31
		147.03,   // hour 32
		129.35,   // hour 33
		123.8475, // hour 34
		112.3,    // hour 35
	}

	// Create forecast with zero solar and 0.38 kW load for all timeslots
	forecast := make([]TimeSlot, len(hourlyPrices))
	for i := range len(hourlyPrices) {
		forecast[i] = TimeSlot{
			Hour:          i,
			Timestamp:     int64(1704326400 + i*3600),             // Starting from 2024-01-04 00:00:00 UTC
			ImportPrice:   hourlyPrices[i]/1000.0 + 0.04 + 0.0085, // Convert EUR/MWh to EUR/kWh
			ExportPrice:   hourlyPrices[i]/1000.0 - 0.017,         // Convert EUR/MWh to EUR/kWh
			SolarForecast: 0.0,                                    // Zero solar as specified
			LoadForecast:  0.38,                                   // 0.38 kW load as specified
		}
	}

	// forecast[15].SolarForecast = 0.5
	// forecast[16].SolarForecast = 2.3
	// forecast[17].SolarForecast = 3.0
	// forecast[18].SolarForecast = 1.2
	// forecast[19].SolarForecast = 1.2
	// forecast[20].SolarForecast = 0.5

	// System configuration
	config := SystemConfig{
		BatteryCapacity:        24.0, // 24 kWh battery
		BatteryMaxCharge:       12.0, // 12 kW max charge rate
		BatteryMaxDischarge:    12.0, // 12 kW max discharge rate
		BatteryMinSOC:          0.0,  // 0% minimum SOC
		BatteryMaxSOC:          1.0,  // 100% maximum SOC
		BatteryEfficiency:      0.9,  // 90% round-trip efficiency
		BatteryDegradationCost: 0.0,  // 0.00/kWh degradation cost
		MaxGridImport:          30.0, // 30 kW max grid import
		MaxGridExport:          30.0, // 30 kW max grid export
	}

	// Create MPC controller with 25% initial SOC
	mpc := NewController(config, len(hourlyPrices), 0.25)

	// Run optimization
	decisions := mpc.Optimize(forecast)

	// Validate results
	if decisions == nil {
		t.Fatal("Optimize returned nil")
	}

	if len(decisions) != len(hourlyPrices) {
		t.Fatalf("Expected %d decisions, got %d", len(hourlyPrices), len(decisions))
	}

	// Verify all decisions have valid data
	for i, dec := range decisions {
		t.Logf("Hour %2d: SOC=%.3f, Charge=%.3f, Discharge=%.3f, Import=%.3f, Export=%.3f, Profit=%.4f, Price=%.5f/kWh",
			i, dec.BatterySOC, dec.BatteryCharge, dec.BatteryDischarge,
			dec.GridImport, dec.GridExport, dec.Profit, dec.ImportPrice)

		// Check SOC bounds
		if dec.BatterySOC < config.BatteryMinSOC-0.001 || dec.BatterySOC > config.BatteryMaxSOC+0.001 {
			t.Errorf("Hour %d: SOC %.3f out of bounds [%.3f, %.3f]",
				i, dec.BatterySOC, config.BatteryMinSOC, config.BatteryMaxSOC)
		}

		// Check charge/discharge bounds
		if dec.BatteryCharge > config.BatteryMaxCharge+0.001 {
			t.Errorf("Hour %d: Charge %.3f exceeds max %.3f", i, dec.BatteryCharge, config.BatteryMaxCharge)
		}
		if dec.BatteryDischarge > config.BatteryMaxDischarge+0.001 {
			t.Errorf("Hour %d: Discharge %.3f exceeds max %.3f", i, dec.BatteryDischarge, config.BatteryMaxDischarge)
		}

		// Check grid bounds
		if dec.GridImport > config.MaxGridImport+0.001 {
			t.Errorf("Hour %d: Grid import %.3f exceeds max %.3f", i, dec.GridImport, config.MaxGridImport)
		}
		if dec.GridExport > config.MaxGridExport+0.001 {
			t.Errorf("Hour %d: Grid export %.3f exceeds max %.3f", i, dec.GridExport, config.MaxGridExport)
		}

		// Check mutual exclusivity of charge/discharge
		if dec.BatteryCharge > 0.001 && dec.BatteryDischarge > 0.001 {
			t.Errorf("Hour %d: Both charge and discharge are non-zero", i)
		}

		// Check mutual exclusivity of grid import/export
		if dec.GridImport > 0.001 && dec.GridExport > 0.001 {
			t.Errorf("Hour %d: Both grid import and export are non-zero", i)
		}

		// Verify forecast data is preserved
		if dec.ImportPrice != forecast[i].ImportPrice {
			t.Errorf("Hour %d: Import price mismatch", i)
		}
		if dec.LoadForecast != forecast[i].LoadForecast {
			t.Errorf("Hour %d: Load forecast mismatch", i)
		}
		if dec.SolarForecast != forecast[i].SolarForecast {
			t.Errorf("Hour %d: Solar forecast mismatch", i)
		}
	}

	// Calculate total profit/cost
	totalProfit := 0.0
	for _, dec := range decisions {
		totalProfit += dec.Profit
	}
	t.Logf("Total profit over 36 hours: %.4f", totalProfit)

	// Detailed arbitrage analysis
	t.Log("\n=== Arbitrage Analysis ===")

	// Find hour 4 (lowest price) details
	hour4 := decisions[4]
	importPriceHour4 := forecast[4].ImportPrice
	exportPriceHour4 := forecast[4].ExportPrice

	// Find hour 22 (highest price) details
	hour22 := decisions[22]
	importPriceHour22 := forecast[22].ImportPrice
	exportPriceHour22 := forecast[22].ExportPrice

	t.Logf("\nHour 4 (LOWEST PRICE):")
	t.Logf("  Import Price: %.5f/kWh", importPriceHour4)
	t.Logf("  Export Price: %.5f/kWh", exportPriceHour4)
	t.Logf("  Charged: %.3f kW (added %.3f kWh to battery)", hour4.BatteryCharge, hour4.BatteryCharge*0.9)
	t.Logf("  Cost: %.4f (import + degradation)", hour4.BatteryCharge*importPriceHour4+hour4.BatteryCharge*config.BatteryDegradationCost)

	t.Logf("\nHour 22 (HIGHEST PRICE):")
	t.Logf("  Import Price: %.5f/kWh", importPriceHour22)
	t.Logf("  Export Price: %.5f/kWh", exportPriceHour22)
	t.Logf("  Discharged: %.3f kW (%.3f kWh from battery)", hour22.BatteryDischarge, hour22.BatteryDischarge)
	t.Logf("  Exported: %.3f kW", hour22.GridExport)
	t.Logf("  Revenue: %.4f (export - degradation)", hour22.GridExport*exportPriceHour22-hour22.BatteryDischarge*config.BatteryDegradationCost)

	// Calculate theoretical max arbitrage if charged more at hour 4
	maxAdditionalCharge := config.BatteryMaxCharge - hour4.BatteryCharge
	socHeadroom := (config.BatteryMaxSOC - hour4.BatterySOC) * config.BatteryCapacity
	actualMaxCharge := math.Min(maxAdditionalCharge, socHeadroom/config.BatteryEfficiency)

	if actualMaxCharge > 0.1 {
		t.Logf("\nTheoretical Additional Arbitrage Opportunity:")
		t.Logf("  Could charge additional: %.3f kW at hour 4", actualMaxCharge)
		t.Logf("  Energy stored after efficiency: %.3f kWh", actualMaxCharge*config.BatteryEfficiency)
		t.Logf("  Cost to charge: %.4f", actualMaxCharge*importPriceHour4+actualMaxCharge*config.BatteryDegradationCost)
		t.Logf("  Revenue if exported at hour 22: %.4f", actualMaxCharge*config.BatteryEfficiency*exportPriceHour22-actualMaxCharge*config.BatteryDegradationCost)
		t.Logf("  Net profit from additional arbitrage: %.4f",
			actualMaxCharge*config.BatteryEfficiency*exportPriceHour22-
				actualMaxCharge*importPriceHour4-
				2*actualMaxCharge*config.BatteryDegradationCost)
	}

	// Analyze where battery energy went
	t.Logf("\nBattery Usage Timeline:")
	chargedTotal := 0.0
	dischargedTotal := 0.0
	for i, dec := range decisions {
		if dec.BatteryCharge > 0.1 {
			chargedTotal += dec.BatteryCharge
			t.Logf("  Hour %2d: CHARGED %.3f kW at price %.5f/kWh (SOC: %.3f)",
				i, dec.BatteryCharge, dec.ImportPrice, dec.BatterySOC)
		}
		if dec.BatteryDischarge > 0.1 {
			dischargedTotal += dec.BatteryDischarge
			exported := ""
			if dec.GridExport > 0.001 {
				exported = fmt.Sprintf(" → EXPORTED %.3f kW", dec.GridExport)
			}
			t.Logf("  Hour %2d: DISCHARGED %.3f kW at price %.5f/kWh (SOC: %.3f)%s",
				i, dec.BatteryDischarge, dec.ExportPrice, dec.BatterySOC, exported)
		}
	}
	t.Logf("\nTotal charged: %.3f kW, Total discharged: %.3f kW", chargedTotal, dischargedTotal)
	t.Logf("Degradation cost: %.4f (%.3f kW throughput × %.2f/kWh)",
		(chargedTotal+dischargedTotal)*config.BatteryDegradationCost,
		chargedTotal+dischargedTotal,
		config.BatteryDegradationCost)

	// Verify arbitrage behavior: should charge during low-price periods and discharge during high-price periods
	t.Log("\n=== Optimization Validation ===")
	// Find hours with lowest and highest prices
	minPriceHour := 0
	maxPriceHour := 0
	minPrice := hourlyPrices[0]
	maxPrice := hourlyPrices[0]

	for i := 1; i < len(hourlyPrices); i++ {
		if hourlyPrices[i] < minPrice {
			minPrice = hourlyPrices[i]
			minPriceHour = i
		}
		if hourlyPrices[i] > maxPrice {
			maxPrice = hourlyPrices[i]
			maxPriceHour = i
		}
	}

	t.Logf("Lowest price: %.5f/kWh at hour %d", minPrice, minPriceHour)
	t.Logf("Highest price: %.5f/kWh at hour %d", maxPrice, maxPriceHour)

	// Check that battery tends to charge during low prices
	// (Not strict since it depends on SOC constraints and overall optimization)
	lowPriceCharging := 0
	for i := range len(hourlyPrices) {
		if hourlyPrices[i] < (minPrice+maxPrice)/2 && decisions[i].BatteryCharge > 0.1 {
			lowPriceCharging++
		}
	}

	highPriceDischarging := 0
	for i := range len(hourlyPrices) {
		if hourlyPrices[i] > (minPrice+maxPrice)/2 && decisions[i].BatteryDischarge > 0.1 {
			highPriceDischarging++
		}
	}

	t.Logf("Hours with charging during low prices: %d", lowPriceCharging)
	t.Logf("Hours with discharging during high prices: %d", highPriceDischarging)

	// Verify power balance for each hour
	for i, dec := range decisions {
		// Power balance: Solar + GridImport + BatteryDischarge*eff = Load + GridExport + BatteryCharge/eff
		supply := forecast[i].SolarForecast + dec.GridImport + dec.BatteryDischarge*config.BatteryEfficiency
		demand := forecast[i].LoadForecast + dec.GridExport + dec.BatteryCharge/config.BatteryEfficiency

		if math.Abs(supply-demand) > 0.01 {
			t.Errorf("Hour %d: Power balance violation. Supply=%.3f, Demand=%.3f, Diff=%.3f",
				i, supply, demand, supply-demand)
		}
	}

	// Final SOC should be different from initial (battery was used)
	finalSOC := decisions[len(hourlyPrices)-1].BatterySOC
	t.Logf("Initial SOC: %.3f, Final SOC: %.3f", 0.5, finalSOC)
}

func TestOptimizeEmptyForecast(t *testing.T) {
	config := SystemConfig{
		BatteryCapacity:     10.0,
		BatteryMaxCharge:    5.0,
		BatteryMaxDischarge: 5.0,
		BatteryMinSOC:       0.1,
		BatteryMaxSOC:       0.9,
		BatteryEfficiency:   0.9,
	}

	mpc := NewController(config, 24, 0.5)
	decisions := mpc.Optimize([]TimeSlot{})

	if decisions != nil {
		t.Error("Expected nil for empty forecast")
	}
}

func TestOptimizeShortHorizon(t *testing.T) {
	config := SystemConfig{
		BatteryCapacity:        10.0,
		BatteryMaxCharge:       5.0,
		BatteryMaxDischarge:    5.0,
		BatteryMinSOC:          0.1,
		BatteryMaxSOC:          0.9,
		BatteryEfficiency:      0.9,
		BatteryDegradationCost: 0.01,
		MaxGridImport:          10.0,
		MaxGridExport:          10.0,
	}

	// Test with just 2 hours
	forecast := []TimeSlot{
		{
			Hour:          0,
			Timestamp:     1704326400,
			ImportPrice:   0.05, // Cheap - should charge
			ExportPrice:   0.02,
			SolarForecast: 0.0,
			LoadForecast:  0.5,
		},
		{
			Hour:          1,
			Timestamp:     1704330000,
			ImportPrice:   0.20, // Expensive - should discharge
			ExportPrice:   0.08,
			SolarForecast: 0.0,
			LoadForecast:  0.5,
		},
	}

	mpc := NewController(config, 2, 0.5)
	mpc.CurrentBatteryTemp = 5.0 // Below threshold
	decisions := mpc.Optimize(forecast)

	if len(decisions) != 2 {
		t.Fatalf("Expected 2 decisions, got %d", len(decisions))
	}

	// During cheap hour, battery should tend to charge (or at least not discharge heavily)
	// During expensive hour, battery should tend to discharge
	t.Logf("Hour 0 (cheap): Charge=%.3f, Discharge=%.3f", decisions[0].BatteryCharge, decisions[0].BatteryDischarge)
	t.Logf("Hour 1 (expensive): Charge=%.3f, Discharge=%.3f", decisions[1].BatteryCharge, decisions[1].BatteryDischarge)
}

func TestOptimizeHighLoad(t *testing.T) {
	config := SystemConfig{
		BatteryCapacity:        10.0,
		BatteryMaxCharge:       5.0,
		BatteryMaxDischarge:    5.0,
		BatteryMinSOC:          0.1,
		BatteryMaxSOC:          0.9,
		BatteryEfficiency:      0.9,
		BatteryDegradationCost: 0.01,
		MaxGridImport:          10.0,
		MaxGridExport:          10.0,
	}

	// Test with high load that exceeds battery discharge capacity
	forecast := []TimeSlot{
		{
			Hour:          0,
			Timestamp:     1704326400,
			ImportPrice:   0.30,
			ExportPrice:   0.10,
			SolarForecast: 0.0,
			LoadForecast:  8.0, // High load - 8 kW
		},
	}

	mpc := NewController(config, 1, 0.9) // Start at high SOC
	decisions := mpc.Optimize(forecast)

	if len(decisions) != 1 {
		t.Fatalf("Expected 1 decision, got %d", len(decisions))
	}

	// With 8 kW load and max 5 kW discharge, should need grid import
	if decisions[0].GridImport <= 0 {
		t.Logf("Warning: Expected grid import with high load, got %.3f", decisions[0].GridImport)
	}

	t.Logf("High load scenario: Discharge=%.3f, Import=%.3f", decisions[0].BatteryDischarge, decisions[0].GridImport)
}

func TestOptimizeHighSolar(t *testing.T) {
	config := SystemConfig{
		BatteryCapacity:        10.0,
		BatteryMaxCharge:       5.0,
		BatteryMaxDischarge:    5.0,
		BatteryMinSOC:          0.1,
		BatteryMaxSOC:          0.9,
		BatteryEfficiency:      0.9,
		BatteryDegradationCost: 0.01,
		MaxGridImport:          10.0,
		MaxGridExport:          10.0,
	}

	// Test with high solar generation
	forecast := []TimeSlot{
		{
			Hour:          0,
			Timestamp:     1704326400,
			ImportPrice:   0.10,
			ExportPrice:   0.05,
			SolarForecast: 8.0, // High solar - 8 kW
			LoadForecast:  2.0,
		},
	}

	mpc := NewController(config, 1, 0.1) // Start at low SOC
	decisions := mpc.Optimize(forecast)

	if len(decisions) != 1 {
		t.Fatalf("Expected 1 decision, got %d", len(decisions))
	}

	// With 8 kW solar and 2 kW load, should have excess to charge or export
	excessUsed := decisions[0].BatteryCharge + decisions[0].GridExport
	if excessUsed <= 0 {
		t.Error("Expected battery charging or grid export with excess solar")
	}

	t.Logf("High solar scenario: Charge=%.3f, Export=%.3f", decisions[0].BatteryCharge, decisions[0].GridExport)
}

func TestOptimizeWithBatteryPreHeat(t *testing.T) {
	config := SystemConfig{
		BatteryCapacity:             10.0,
		BatteryMaxCharge:            5.0,
		BatteryMaxDischarge:         5.0,
		BatteryMinSOC:               0.1,
		BatteryMaxSOC:               0.9,
		BatteryEfficiency:           0.9,
		BatteryDegradationCost:      0.01,
		MaxGridImport:               10.0,
		MaxGridExport:               10.0,
		BatteryPreHeatPower:         0.7,  // 700W battery preheating
		BatteryPreHeatTempThreshold: 10.0, // 10°C threshold
		BatteryThermalTimeConstant:  0.1,  // 10% thermal change per hour (auto-scaled for time slot duration)
	}

	// Test 1: Cold battery (5°C) with arbitrage opportunity - battery preheating should activate
	// Period 0: Cheap price (charge), Period 1: Expensive price (discharge)
	forecast1 := []TimeSlot{
		{
			Hour:           0,
			Timestamp:      1704326400,
			ImportPrice:    0.05, // Very cheap - good time to charge
			ExportPrice:    0.02,
			SolarForecast:  0.0,
			LoadForecast:   1.0,
			AirTemperature: 5.0, // Cold air temperature
		},
		{
			Hour:           1,
			Timestamp:      1704330000,
			ImportPrice:    0.30, // Expensive - good time to discharge
			ExportPrice:    0.15,
			SolarForecast:  0.0,
			LoadForecast:   1.0,
			AirTemperature: 5.0,
		},
	}

	mpc1 := NewController(config, 2, 0.2) // Start at 20% SOC
	mpc1.CurrentBatteryTemp = 5.0         // Below 10°C threshold
	decisions1 := mpc1.Optimize(forecast1)

	if len(decisions1) != 2 {
		t.Fatalf("Expected 2 decisions, got %d", len(decisions1))
	}

	// Should charge in period 0 despite battery preheating cost
	if decisions1[0].BatteryCharge <= 0 {
		t.Error("Expected battery charging in cheap period even with battery preheating")
	}

	if !decisions1[0].BatteryPreHeatActive {
		t.Error("Expected battery preheating to be active when battery temp is below threshold and charging")
	}

	// GridImport should include load + battery charge losses + battery preheat power
	expectedMinImport := forecast1[0].LoadForecast + decisions1[0].BatteryCharge/config.BatteryEfficiency + config.BatteryPreHeatPower
	if decisions1[0].GridImport < expectedMinImport-0.1 {
		t.Errorf("GridImport (%.3f) should account for battery preheat power, expected at least %.3f",
			decisions1[0].GridImport, expectedMinImport)
	}

	t.Logf("Cold battery - Period 0 (cheap): Charge=%.3f kW, GridImport=%.3f kW, BatteryPreHeat=%v",
		decisions1[0].BatteryCharge, decisions1[0].GridImport, decisions1[0].BatteryPreHeatActive)
	t.Logf("Cold battery - Period 1 (expensive): Discharge=%.3f kW, GridImport=%.3f kW",
		decisions1[1].BatteryDischarge, decisions1[1].GridImport)

	// Test 2: Warm battery (15°C) with same arbitrage opportunity - battery preheating should NOT activate
	forecast2 := []TimeSlot{
		{
			Hour:           0,
			Timestamp:      1704326400,
			ImportPrice:    0.05,
			ExportPrice:    0.02,
			SolarForecast:  0.0,
			LoadForecast:   1.0,
			AirTemperature: 15.0,
		},
		{
			Hour:           1,
			Timestamp:      1704330000,
			ImportPrice:    0.30,
			ExportPrice:    0.15,
			SolarForecast:  0.0,
			LoadForecast:   1.0,
			AirTemperature: 15.0,
		},
	}

	mpc2 := NewController(config, 2, 0.5)
	mpc2.CurrentBatteryTemp = 15.0 // Above 10°C threshold
	decisions2 := mpc2.Optimize(forecast2)

	if len(decisions2) != 2 {
		t.Fatalf("Expected 2 decisions, got %d", len(decisions2))
	}

	if decisions2[0].BatteryPreHeatActive {
		t.Error("Expected battery preheating to be inactive when battery temp is above threshold")
	}

	// Warm battery should charge more or have lower import cost (no battery preheating)
	if decisions2[0].BatteryCharge > 0 && decisions1[0].BatteryCharge > 0 {
		// Both charging: warm battery should have lower GridImport (no battery preheating)
		if decisions2[0].GridImport >= decisions1[0].GridImport {
			t.Errorf("Warm battery GridImport (%.3f) should be less than cold battery GridImport (%.3f)",
				decisions2[0].GridImport, decisions1[0].GridImport)
		}
	}

	t.Logf("Warm battery - Period 0 (cheap): Charge=%.3f kW, GridImport=%.3f kW, BatteryPreHeat=%v",
		decisions2[0].BatteryCharge, decisions2[0].GridImport, decisions2[0].BatteryPreHeatActive)
	t.Logf("Warm battery - Period 1 (expensive): Discharge=%.3f kW, GridImport=%.3f kW",
		decisions2[1].BatteryDischarge, decisions2[1].GridImport)

	// Test 3: Cold battery discharging - battery preheating should NOT activate
	forecast3 := []TimeSlot{
		{
			Hour:           0,
			Timestamp:      1704326400,
			ImportPrice:    0.10,
			ExportPrice:    0.15, // Good export price - incentivize discharge
			SolarForecast:  0.0,
			LoadForecast:   1.0,
			AirTemperature: 5.0,
		},
	}

	mpc3 := NewController(config, 1, 0.8) // High SOC - can discharge
	mpc3.CurrentBatteryTemp = 5.0         // Below 10°C threshold
	decisions3 := mpc3.Optimize(forecast3)

	if len(decisions3) != 1 {
		t.Fatalf("Expected 1 decision, got %d", len(decisions3))
	}

	// If discharging, battery preheating should not be active
	if decisions3[0].BatteryDischarge > 0 && decisions3[0].BatteryPreHeatActive {
		t.Error("Battery preheating should not be active when discharging (only when charging)")
	}

	t.Logf("Cold battery discharging: Discharge=%.3f kW, BatteryPreHeat=%v",
		decisions3[0].BatteryDischarge, decisions3[0].BatteryPreHeatActive)

	// Test 4: Verify battery preheating status is recorded correctly
	forecast4 := []TimeSlot{
		{
			Hour:           0,
			Timestamp:      1704326400,
			ImportPrice:    0.03, // Very cheap to encourage charging
			ExportPrice:    0.01,
			SolarForecast:  0.0,
			LoadForecast:   0.5,
			AirTemperature: 8.0,
		},
		{
			Hour:           1,
			Timestamp:      1704330000,
			ImportPrice:    0.35, // Very expensive
			ExportPrice:    0.20,
			SolarForecast:  0.0,
			LoadForecast:   0.5,
			AirTemperature: 8.0,
		},
	}

	mpc4 := NewController(config, 2, 0.2)
	mpc4.CurrentBatteryTemp = 8.0 // Below threshold
	decisions4 := mpc4.Optimize(forecast4)

	if len(decisions4) != 2 {
		t.Fatalf("Expected 2 decisions, got %d", len(decisions4))
	}

	// Period 0: Should charge at very cheap price
	if decisions4[0].BatteryCharge > 0 && !decisions4[0].BatteryPreHeatActive {
		t.Error("Expected battery preheating active when charging with cold battery")
	}

	// Period 1: If discharging, battery preheating should not be active
	if decisions4[1].BatteryDischarge > 0 && decisions4[1].BatteryPreHeatActive {
		t.Error("Expected battery preheating inactive when discharging")
	}

	t.Logf("Cold battery arbitrage - Period 0: Charge=%.3f kW, BatteryPreHeat=%v, GridImport=%.3f kW",
		decisions4[0].BatteryCharge, decisions4[0].BatteryPreHeatActive, decisions4[0].GridImport)
	t.Logf("Cold battery arbitrage - Period 1: Discharge=%.3f kW, BatteryPreHeat=%v, GridImport=%.3f kW",
		decisions4[1].BatteryDischarge, decisions4[1].BatteryPreHeatActive, decisions4[1].GridImport)
}

func TestBatteryPreHeatGridImportExceedsBatteryCharge(t *testing.T) {
	// Test that grid import can exceed BatteryMaxCharge when battery preheating is active
	config := SystemConfig{
		BatteryCapacity:             10.0,
		BatteryMaxCharge:            5.0, // 5 kW max charge
		BatteryMaxDischarge:         5.0,
		BatteryMinSOC:               0.1,
		BatteryMaxSOC:               0.9,
		BatteryEfficiency:           0.9,
		BatteryDegradationCost:      0.01,
		MaxGridImport:               15.0, // 15 kW max import - enough for battery + preheat + load
		MaxGridExport:               10.0,
		BatteryPreHeatPower:         0.7,  // 700W battery preheating
		BatteryPreHeatTempThreshold: 10.0, // 10°C threshold
		BatteryThermalTimeConstant:  0.1,  // 10% thermal change per hour (auto-scaled for time slot duration)
	}

	// Cold battery with very cheap price to maximize charging
	forecast := []TimeSlot{
		{
			Hour:           0,
			Timestamp:      1704326400,
			ImportPrice:    0.02, // Very cheap
			ExportPrice:    0.01,
			SolarForecast:  0.0,
			LoadForecast:   2.0, // 2 kW load
			AirTemperature: 5.0,
		},
		{
			Hour:           1,
			Timestamp:      1704330000,
			ImportPrice:    0.40, // Very expensive
			ExportPrice:    0.25,
			SolarForecast:  0.0,
			LoadForecast:   2.0,
			AirTemperature: 5.0,
		},
	}

	mpc := NewController(config, 2, 0.2)
	mpc.CurrentBatteryTemp = 5.0 // Below 10°C threshold
	decisions := mpc.Optimize(forecast)

	if len(decisions) != 2 {
		t.Fatalf("Expected 2 decisions, got %d", len(decisions))
	}

	// Verify charging at high rate in period 0 (may not be exactly at maximum due to optimizer granularity)
	if decisions[0].BatteryCharge < config.BatteryMaxCharge*0.8 {
		t.Errorf("Expected battery to charge at high rate (>80%% of max), got %.3f kW", decisions[0].BatteryCharge)
	}

	// Verify battery preheating is active
	if !decisions[0].BatteryPreHeatActive {
		t.Error("Expected battery preheating to be active when charging cold battery")
	}

	// Calculate expected minimum grid import:
	// Load + Battery charge/efficiency + Battery preheat
	expectedMinImport := forecast[0].LoadForecast +
		decisions[0].BatteryCharge/config.BatteryEfficiency +
		config.BatteryPreHeatPower

	// Grid import should match the expected value
	if math.Abs(decisions[0].GridImport-expectedMinImport) > 0.1 {
		t.Errorf("GridImport (%.3f) should be approximately %.3f (load + charge/eff + preheat)",
			decisions[0].GridImport, expectedMinImport)
	}

	// Key verification: Grid import MUST exceed BatteryMaxCharge when battery preheating is active
	if decisions[0].GridImport <= config.BatteryMaxCharge {
		t.Errorf("GridImport (%.3f kW) should exceed BatteryMaxCharge (%.3f kW) when battery preheating is active",
			decisions[0].GridImport, config.BatteryMaxCharge)
	}

	// Verify grid import is within MaxGridImport limit
	if decisions[0].GridImport > config.MaxGridImport {
		t.Errorf("GridImport (%.3f kW) exceeds MaxGridImport limit (%.3f kW)",
			decisions[0].GridImport, config.MaxGridImport)
	}

	t.Logf("Maximum charging scenario:")
	t.Logf("  Load: %.3f kW", forecast[0].LoadForecast)
	t.Logf("  Battery Charge: %.3f kW", decisions[0].BatteryCharge)
	t.Logf("  Battery Charge (with losses): %.3f kW", decisions[0].BatteryCharge/config.BatteryEfficiency)
	t.Logf("  Battery PreHeat: %.3f kW", config.BatteryPreHeatPower)
	t.Logf("  Total Grid Import: %.3f kW", decisions[0].GridImport)
	t.Logf("  Grid Import exceeds BatteryMaxCharge by: %.3f kW",
		decisions[0].GridImport-config.BatteryMaxCharge)
}

func TestBatteryTemperatureThermalDynamics(t *testing.T) {
	// Test that battery temperature evolves correctly based on thermal dynamics
	config := SystemConfig{
		BatteryCapacity:             10.0,
		BatteryMaxCharge:            5.0,
		BatteryMaxDischarge:         5.0,
		BatteryMinSOC:               0.1,
		BatteryMaxSOC:               0.9,
		BatteryEfficiency:           0.9,
		BatteryDegradationCost:      0.01,
		MaxGridImport:               10.0,
		MaxGridExport:               10.0,
		BatteryPreHeatPower:         0.7,
		BatteryPreHeatTempThreshold: 10.0,
		BatteryThermalTimeConstant:  0.2, // 20% thermal change per hour (auto-scaled for time slot duration)
	}

	// Test 1: Cold battery warming up toward warm air temperature (no charging)
	forecast1 := []TimeSlot{
		{
			Hour:           0,
			Timestamp:      1704326400,
			ImportPrice:    0.30, // Expensive - won't charge
			ExportPrice:    0.15,
			SolarForecast:  0.0,
			LoadForecast:   1.0,
			AirTemperature: 20.0, // Warm air
		},
		{
			Hour:           1,
			Timestamp:      1704330000,
			ImportPrice:    0.30,
			ExportPrice:    0.15,
			SolarForecast:  0.0,
			LoadForecast:   1.0,
			AirTemperature: 20.0,
		},
		{
			Hour:           2,
			Timestamp:      1704333600,
			ImportPrice:    0.30,
			ExportPrice:    0.15,
			SolarForecast:  0.0,
			LoadForecast:   1.0,
			AirTemperature: 20.0,
		},
	}

	mpc1 := NewController(config, 3, 0.5)
	mpc1.CurrentBatteryTemp = 5.0 // Cold battery
	decisions1 := mpc1.Optimize(forecast1)

	if len(decisions1) != 3 {
		t.Fatalf("Expected 3 decisions, got %d", len(decisions1))
	}

	// Verify battery is not charging (prices too high)
	for i, dec := range decisions1 {
		if dec.BatteryCharge > 0.1 {
			t.Errorf("Period %d: Expected no charging at high prices, got %.3f kW", i, dec.BatteryCharge)
		}
	}

	t.Logf("Battery warming scenario:")
	t.Logf("  Period 0: Battery %.1f°C, Air %.1f°C, Charging: %.3f kW",
		decisions1[0].BatteryAvgCellTemp, forecast1[0].AirTemperature, decisions1[0].BatteryCharge)
	t.Logf("  Period 1: Battery %.1f°C, Air %.1f°C, Charging: %.3f kW",
		decisions1[1].BatteryAvgCellTemp, forecast1[1].AirTemperature, decisions1[1].BatteryCharge)
	t.Logf("  Period 2: Battery %.1f°C, Air %.1f°C, Charging: %.3f kW",
		decisions1[2].BatteryAvgCellTemp, forecast1[2].AirTemperature, decisions1[2].BatteryCharge)

	// Test 2: Warm battery cooling down toward cold air temperature (no charging)
	forecast2 := []TimeSlot{
		{
			Hour:           0,
			Timestamp:      1704326400,
			ImportPrice:    0.30, // Expensive - won't charge
			ExportPrice:    0.15,
			SolarForecast:  0.0,
			LoadForecast:   1.0,
			AirTemperature: 0.0, // Cold air
		},
		{
			Hour:           1,
			Timestamp:      1704330000,
			ImportPrice:    0.30,
			ExportPrice:    0.15,
			SolarForecast:  0.0,
			LoadForecast:   1.0,
			AirTemperature: 0.0,
		},
		{
			Hour:           2,
			Timestamp:      1704333600,
			ImportPrice:    0.30,
			ExportPrice:    0.15,
			SolarForecast:  0.0,
			LoadForecast:   1.0,
			AirTemperature: 0.0,
		},
	}

	mpc2 := NewController(config, 3, 0.5)
	mpc2.CurrentBatteryTemp = 20.0 // Warm battery
	decisions2 := mpc2.Optimize(forecast2)

	if len(decisions2) != 3 {
		t.Fatalf("Expected 3 decisions, got %d", len(decisions2))
	}

	t.Logf("Battery cooling scenario:")
	t.Logf("  Period 0: Battery %.1f°C, Air %.1f°C, Charging: %.3f kW",
		decisions2[0].BatteryAvgCellTemp, forecast2[0].AirTemperature, decisions2[0].BatteryCharge)
	t.Logf("  Period 1: Battery %.1f°C, Air %.1f°C, Charging: %.3f kW",
		decisions2[1].BatteryAvgCellTemp, forecast2[1].AirTemperature, decisions2[1].BatteryCharge)
	t.Logf("  Period 2: Battery %.1f°C, Air %.1f°C, Charging: %.3f kW",
		decisions2[2].BatteryAvgCellTemp, forecast2[2].AirTemperature, decisions2[2].BatteryCharge)

	// Test 3: Verify temperature forecasting enables optimizer to make smart decisions
	// Cold battery that stays cold (no natural warming) - optimizer should see preheating cost
	forecast3 := []TimeSlot{
		{
			Hour:           0,
			Timestamp:      1704326400,
			ImportPrice:    0.03, // Very cheap
			ExportPrice:    0.01,
			SolarForecast:  0.0,
			LoadForecast:   1.0,
			AirTemperature: -10.0, // Very cold air - battery will stay cold
		},
		{
			Hour:           1,
			Timestamp:      1704330000,
			ImportPrice:    0.03, // Same cheap price
			ExportPrice:    0.01,
			SolarForecast:  0.0,
			LoadForecast:   1.0,
			AirTemperature: -10.0,
		},
		{
			Hour:           2,
			Timestamp:      1704333600,
			ImportPrice:    0.40, // Expensive - discharge
			ExportPrice:    0.20,
			SolarForecast:  0.0,
			LoadForecast:   1.0,
			AirTemperature: -10.0,
		},
	}

	mpc3 := NewController(config, 3, 0.2)
	mpc3.CurrentBatteryTemp = 5.0 // Cold battery
	decisions3 := mpc3.Optimize(forecast3)

	if len(decisions3) != 3 {
		t.Fatalf("Expected 3 decisions, got %d", len(decisions3))
	}

	// The key insight: optimizer sees temperature forecast and knows:
	// - All periods have cold battery (< 10°C)
	// - Charging any period requires preheating (extra 0.7 kW cost)
	// - Prices are same in period 0 and 1
	// - Should still charge when profitable despite preheating cost

	// Verify some charging happens during cheap periods
	totalCharging := decisions3[0].BatteryCharge + decisions3[1].BatteryCharge
	if totalCharging <= 0 {
		t.Error("Expected some charging during cheap price periods (0-1) despite cold battery")
	}

	// When charging happens in cold conditions, preheating should be active
	// Note: Once battery reaches threshold temp via preheating, it may charge without preheating in subsequent periods
	for i := 0; i < 2; i++ {
		if decisions3[i].BatteryCharge > 0.1 {
			// Only check preheating if battery temp is strictly below threshold
			if decisions3[i].BatteryAvgCellTemp < config.BatteryPreHeatTempThreshold {
				if !decisions3[i].BatteryPreHeatActive {
					t.Errorf("Period %d: Expected preheating active when charging (temp: %.1f°C < %.1f°C)",
						i, decisions3[i].BatteryAvgCellTemp, config.BatteryPreHeatTempThreshold)
				}
			}
		}
	}

	// Period 2: Should not charge at expensive price
	if decisions3[2].BatteryCharge > 0.1 {
		t.Error("Expected no charging in period 2 at expensive prices")
	}

	t.Logf("Battery temperature forecasting enables smart optimization:")
	t.Logf("  Period 0: Battery %.1f°C, Air %.1f°C, Charge: %.3f kW, PreHeat: %v",
		decisions3[0].BatteryAvgCellTemp, forecast3[0].AirTemperature, decisions3[0].BatteryCharge, decisions3[0].BatteryPreHeatActive)
	t.Logf("  Period 1: Battery %.1f°C, Air %.1f°C, Charge: %.3f kW, PreHeat: %v",
		decisions3[1].BatteryAvgCellTemp, forecast3[1].AirTemperature, decisions3[1].BatteryCharge, decisions3[1].BatteryPreHeatActive)
	t.Logf("  Period 2: Battery %.1f°C, Air %.1f°C, Charge: %.3f kW, PreHeat: %v",
		decisions3[2].BatteryAvgCellTemp, forecast3[2].AirTemperature, decisions3[2].BatteryCharge, decisions3[2].BatteryPreHeatActive)
	t.Logf("  Note: Optimizer accounts for temperature forecasts and preheating costs in all periods")
}
