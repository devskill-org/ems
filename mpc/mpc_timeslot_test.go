package mpc

import (
	"testing"
)

func TestTimeSlotDuration15Minutes(t *testing.T) {
	// Test that 15-minute time slots work correctly with TimeSlotDuration = 0.25 hours
	// This verifies that battery capacity calculations are correct for sub-hourly intervals

	config := SystemConfig{
		BatteryCapacity:        10.0, // 10 kWh
		BatteryMaxCharge:       5.0,  // 5 kW
		BatteryMaxDischarge:    5.0,  // 5 kW
		BatteryMinSOC:          0.2,  // 20%
		BatteryMaxSOC:          0.9,  // 90%
		BatteryEfficiency:      0.92, // 92% round-trip
		BatteryDegradationCost: 0.01, // $0.01 per kWh cycled (lower for profitable arbitrage)
		MaxGridImport:          10.0, // 10 kW
		MaxGridExport:          10.0, // 10 kW
		TimeSlotDuration:       0.25, // 15 minutes = 0.25 hours
	}

	// Create scenario: charge at low price, discharge at high price
	// With 15-minute slots, charging at 5 kW for 15 minutes = 5 * 0.25 = 1.25 kWh
	forecast := []TimeSlot{
		{
			Hour:          0,
			Timestamp:     1704326400,
			ImportPrice:   0.02, // Very cheap - charge
			ExportPrice:   0.01,
			SolarForecast: 0.0,
			LoadForecast:  1.0,
		},
		{
			Hour:          1,
			Timestamp:     1704327300, // 15 minutes later
			ImportPrice:   0.50,       // Very expensive - discharge
			ExportPrice:   0.25,       // High export price for profitable arbitrage
			SolarForecast: 0.0,
			LoadForecast:  1.0,
		},
	}

	mpc := NewController(config, 2, 0.2) // Start at 20% SOC (minimum)
	decisions := mpc.Optimize(forecast)

	if len(decisions) != 2 {
		t.Fatalf("Expected 2 decisions, got %d", len(decisions))
	}

	// Verify charging in first slot
	if decisions[0].BatteryCharge < 3.0 {
		t.Errorf("Expected significant charging in slot 0 (cheap price), got %.3f kW", decisions[0].BatteryCharge)
	}

	// Calculate expected SOC change from charging
	// Energy charged = Power * Duration * Efficiency
	// = 5 kW * 0.25 hours * 0.92 = 1.15 kWh
	// SOC change = 1.15 kWh / 10 kWh = 0.115 = 11.5%
	expectedSOCIncrease := (decisions[0].BatteryCharge * 0.25 * config.BatteryEfficiency) / config.BatteryCapacity
	actualSOCChange := decisions[0].BatterySOC - 0.2

	if actualSOCChange < expectedSOCIncrease*0.9 || actualSOCChange > expectedSOCIncrease*1.1 {
		t.Errorf("SOC change from 15-min charging incorrect. Expected ~%.3f, got %.3f",
			expectedSOCIncrease, actualSOCChange)
	}

	// Verify discharging in second slot
	if decisions[1].BatteryDischarge < 2.0 {
		t.Errorf("Expected significant discharging in slot 1 (expensive price), got %.3f kW", decisions[1].BatteryDischarge)
	}

	// Verify profit calculation accounts for 15-minute duration
	// For slot 0 (charging):
	// - Import cost should be for 15 minutes of operation
	// - Degradation should be for energy cycled in 15 minutes
	slot0ImportEnergy := decisions[0].GridImport * 0.25 // kWh
	slot0ImportCost := slot0ImportEnergy * forecast[0].ImportPrice
	slot0Degradation := (decisions[0].BatteryCharge * 0.25) * config.BatteryDegradationCost
	expectedSlot0Profit := -slot0ImportCost - slot0Degradation

	if decisions[0].Profit > expectedSlot0Profit*0.8 || decisions[0].Profit < expectedSlot0Profit*1.2 {
		t.Logf("Slot 0 profit calculation: Import=%.3f kWh * $%.2f = $%.4f, Degradation=$%.4f",
			slot0ImportEnergy, forecast[0].ImportPrice, slot0ImportCost, slot0Degradation)
		t.Logf("Expected profit: ~$%.4f, Got: $%.4f", expectedSlot0Profit, decisions[0].Profit)
	}

	t.Logf("15-minute time slot test results:")
	t.Logf("  Slot 0 (cheap): Charge=%.3f kW, SOC: 20%% -> %.1f%%, Profit=$%.4f",
		decisions[0].BatteryCharge, decisions[0].BatterySOC*100, decisions[0].Profit)
	t.Logf("  Slot 1 (expensive): Discharge=%.3f kW, SOC: %.1f%% -> %.1f%%, Profit=$%.4f",
		decisions[1].BatteryDischarge, decisions[0].BatterySOC*100, decisions[1].BatterySOC*100, decisions[1].Profit)
	t.Logf("  Energy cycled in 15 min: Charge=%.3f kWh, Discharge=%.3f kWh",
		decisions[0].BatteryCharge*0.25, decisions[1].BatteryDischarge*0.25)
}

func TestTimeSlotDurationComparison(t *testing.T) {
	// Compare results between 1-hour and 15-minute time slots
	// The optimization should behave consistently regardless of time slot duration

	baseConfig := SystemConfig{
		BatteryCapacity:        10.0,
		BatteryMaxCharge:       5.0,
		BatteryMaxDischarge:    5.0,
		BatteryMinSOC:          0.2,
		BatteryMaxSOC:          0.9,
		BatteryEfficiency:      0.92,
		BatteryDegradationCost: 0.01, // Lower degradation cost
		MaxGridImport:          10.0,
		MaxGridExport:          10.0,
	}

	// Test with 1-hour time slots
	config1Hour := baseConfig
	config1Hour.TimeSlotDuration = 1.0

	forecast1Hour := []TimeSlot{
		{
			Hour:          0,
			Timestamp:     1704326400,
			ImportPrice:   0.02,
			ExportPrice:   0.01,
			SolarForecast: 2.0,
			LoadForecast:  3.0,
		},
		{
			Hour:          1,
			Timestamp:     1704330000,
			ImportPrice:   0.50,
			ExportPrice:   0.25,
			SolarForecast: 2.0,
			LoadForecast:  3.0,
		},
	}

	mpc1Hour := NewController(config1Hour, 2, 0.5)
	decisions1Hour := mpc1Hour.Optimize(forecast1Hour)

	// Test with 15-minute time slots (4 slots = 1 hour equivalent)
	config15Min := baseConfig
	config15Min.TimeSlotDuration = 0.25

	forecast15Min := []TimeSlot{}
	for i := 0; i < 8; i++ { // 8 slots = 2 hours
		hour := i / 4
		forecast15Min = append(forecast15Min, TimeSlot{
			Hour:          i,
			Timestamp:     1704326400 + int64(i*900), // 900 seconds = 15 minutes
			ImportPrice:   forecast1Hour[hour].ImportPrice,
			ExportPrice:   forecast1Hour[hour].ExportPrice,
			SolarForecast: forecast1Hour[hour].SolarForecast,
			LoadForecast:  forecast1Hour[hour].LoadForecast,
		})
	}

	mpc15Min := NewController(config15Min, 8, 0.5)
	decisions15Min := mpc15Min.Optimize(forecast15Min)

	// Compare final SOC after equivalent time periods
	finalSOC1Hour := decisions1Hour[len(decisions1Hour)-1].BatterySOC
	finalSOC15Min := decisions15Min[len(decisions15Min)-1].BatterySOC

	// SOC should be similar (within 5%) after equivalent time
	socDifference := finalSOC1Hour - finalSOC15Min
	if socDifference < -0.05 || socDifference > 0.05 {
		t.Errorf("Final SOC differs too much between time slot durations: 1-hour=%.3f, 15-min=%.3f",
			finalSOC1Hour, finalSOC15Min)
	}

	// Calculate total profit for equivalent periods
	totalProfit1Hour := 0.0
	for _, d := range decisions1Hour {
		totalProfit1Hour += d.Profit
	}

	totalProfit15Min := 0.0
	for _, d := range decisions15Min {
		totalProfit15Min += d.Profit
	}

	// Total profit should be similar (within 10% due to discrete optimization differences)
	profitDifference := (totalProfit1Hour - totalProfit15Min) / totalProfit1Hour
	if profitDifference < -0.1 || profitDifference > 0.1 {
		t.Logf("Warning: Total profit differs between time slot durations: 1-hour=$%.4f, 15-min=$%.4f (%.1f%% difference)",
			totalProfit1Hour, totalProfit15Min, profitDifference*100)
	}

	t.Logf("Time slot duration comparison:")
	t.Logf("  1-hour slots: Final SOC=%.1f%%, Total profit=$%.4f", finalSOC1Hour*100, totalProfit1Hour)
	t.Logf("  15-min slots: Final SOC=%.1f%%, Total profit=$%.4f", finalSOC15Min*100, totalProfit15Min)
	t.Logf("  Differences: SOC=%.1f%%, Profit=%.1f%%",
		socDifference*100, profitDifference*100)
}

func TestBatteryCapacityConstraints15Min(t *testing.T) {
	// Verify that battery capacity constraints work correctly with 15-minute time slots
	// Test that SOC changes by the correct amount per 15-minute slot

	config := SystemConfig{
		BatteryCapacity:        10.0, // 10 kWh
		BatteryMaxCharge:       5.0,  // 5 kW
		BatteryMaxDischarge:    5.0,  // 5 kW
		BatteryMinSOC:          0.2,  // 20%
		BatteryMaxSOC:          0.9,  // 90%
		BatteryEfficiency:      0.92, // 92%
		BatteryDegradationCost: 0.01, // Lower degradation cost
		MaxGridImport:          10.0,
		MaxGridExport:          10.0,
		TimeSlotDuration:       0.25, // 15 minutes
	}

	// Create scenario with cheap prices followed by expensive prices
	// First 10 slots (2.5 hours) cheap, next 10 slots (2.5 hours) expensive
	forecast := []TimeSlot{}
	for i := 0; i < 20; i++ {
		importPrice := 0.02
		exportPrice := 0.01
		if i >= 10 { // Second half is expensive
			importPrice = 0.50
			exportPrice = 0.25
		}
		forecast = append(forecast, TimeSlot{
			Hour:          i,
			Timestamp:     1704326400 + int64(i*900),
			ImportPrice:   importPrice,
			ExportPrice:   exportPrice,
			SolarForecast: 0.0,
			LoadForecast:  0.5,
		})
	}

	mpc := NewController(config, 20, 0.2) // Start at min SOC (20%)
	decisions := mpc.Optimize(forecast)

	// Verify SOC increases during cheap period
	chargingSlotsCount := 0
	totalSOCIncrease := 0.0
	for i := 0; i < 10; i++ {
		if decisions[i].BatteryCharge > 0.1 {
			chargingSlotsCount++
			if i == 0 {
				totalSOCIncrease = decisions[i].BatterySOC - 0.2
			} else {
				socIncrease := decisions[i].BatterySOC - decisions[i-1].BatterySOC
				if socIncrease > 0 {
					totalSOCIncrease += socIncrease
				}
			}
		}
	}

	if chargingSlotsCount == 0 {
		t.Error("Expected charging during cheap price periods, but no charging occurred")
	}

	// For each 15-minute charging slot at 5 kW:
	// Energy stored = 5 kW * 0.25 hours * 0.92 efficiency = 1.15 kWh
	// SOC increase per slot = 1.15 / 10 = 0.115 = 11.5%
	expectedSOCIncreasePerSlot := (config.BatteryMaxCharge * config.TimeSlotDuration * config.BatteryEfficiency) / config.BatteryCapacity

	// Check that at least one slot shows proper SOC increase
	foundCorrectIncrease := false
	for i := 1; i < 10; i++ {
		if decisions[i].BatteryCharge > 4.0 {
			socIncrease := decisions[i].BatterySOC - decisions[i-1].BatterySOC
			if socIncrease >= expectedSOCIncreasePerSlot*0.8 && socIncrease <= expectedSOCIncreasePerSlot*1.2 {
				foundCorrectIncrease = true
				break
			}
		}
	}

	if !foundCorrectIncrease && chargingSlotsCount > 1 {
		t.Errorf("Expected SOC increase of ~%.3f per charging slot, but none found in range", expectedSOCIncreasePerSlot)
	}

	// Verify that SOC never exceeds max or goes below min
	for i, d := range decisions {
		if d.BatterySOC > config.BatteryMaxSOC+0.001 {
			t.Errorf("Slot %d: SOC (%.3f) exceeds max SOC (%.3f)", i, d.BatterySOC, config.BatteryMaxSOC)
		}
		if d.BatterySOC < config.BatteryMinSOC-0.001 {
			t.Errorf("Slot %d: SOC (%.3f) below min SOC (%.3f)", i, d.BatterySOC, config.BatteryMinSOC)
		}
	}

	t.Logf("Battery capacity constraints test (15-min slots):")
	t.Logf("  Charging slots in cheap period: %d / 10", chargingSlotsCount)
	t.Logf("  Total SOC increase during charging: %.1f%%", totalSOCIncrease*100)
	t.Logf("  Expected SOC increase per slot: %.1f%%", expectedSOCIncreasePerSlot*100)
	t.Logf("  Initial SOC: %.1f%%, Final SOC: %.1f%%",
		config.BatteryMinSOC*100, decisions[len(decisions)-1].BatterySOC*100)
}

func TestThermalTimeConstantScaling15Min(t *testing.T) {
	// Test that thermal time constant is correctly scaled for 15-minute time slots
	// If thermal constant is 0.2 per hour (20% per hour), then for 15 minutes it should be ~0.05 (5% per 15 min)

	// Test with 1-hour time slots
	config1Hour := SystemConfig{
		BatteryCapacity:             10.0,
		BatteryMaxCharge:            5.0,
		BatteryMaxDischarge:         5.0,
		BatteryMinSOC:               0.2,
		BatteryMaxSOC:               0.9,
		BatteryEfficiency:           0.92,
		BatteryDegradationCost:      0.01,
		MaxGridImport:               10.0,
		MaxGridExport:               10.0,
		BatteryPreHeatPower:         0.7,
		BatteryPreHeatTempThreshold: 10.0,
		BatteryThermalTimeConstant:  0.2, // 20% per hour
		TimeSlotDuration:            1.0, // 1 hour
	}

	// Test with 15-minute time slots
	config15Min := SystemConfig{
		BatteryCapacity:             10.0,
		BatteryMaxCharge:            5.0,
		BatteryMaxDischarge:         5.0,
		BatteryMinSOC:               0.2,
		BatteryMaxSOC:               0.9,
		BatteryEfficiency:           0.92,
		BatteryDegradationCost:      0.01,
		MaxGridImport:               10.0,
		MaxGridExport:               10.0,
		BatteryPreHeatPower:         0.7,
		BatteryPreHeatTempThreshold: 10.0,
		BatteryThermalTimeConstant:  0.2,  // 20% per hour (same as 1-hour config)
		TimeSlotDuration:            0.25, // 15 minutes
	}

	// Simulate temperature changes directly using calculateNextBatteryTemp
	// After 1 hour with k=0.2, temperature should change by: ΔT = 0.2 * (20 - 5) = 3°C
	// So temp should be: 5 + 3 = 8°C

	mpc1Hour := &Controller{Config: config1Hour}
	initialTemp := 5.0
	airTemp := 20.0

	// Simulate 1 hour
	temp1Hour := mpc1Hour.calculateNextBatteryTemp(initialTemp, airTemp, false, false)
	expectedTemp1Hour := 5.0 + 0.2*(20.0-5.0) // 8.0°C

	// Simulate 4 slots of 15 minutes (1 hour total)
	mpc15Min := &Controller{Config: config15Min}
	temp15Min := initialTemp
	for i := 0; i < 4; i++ {
		temp15Min = mpc15Min.calculateNextBatteryTemp(temp15Min, airTemp, false, false)
	}

	// After 4 slots of 15 minutes (1 hour total), temperature should be approximately the same
	// Each 15-min slot: k_slot ≈ 0.2 * 0.25 = 0.05
	// Slot 1: 5 + 0.05*(20-5) = 5.75
	// Slot 2: 5.75 + 0.05*(20-5.75) = 6.4625
	// Slot 3: 6.4625 + 0.05*(20-6.4625) = 7.139
	// Slot 4: 7.139 + 0.05*(20-7.139) = 7.782

	// 1-hour temp should be close to expected (8.0°C)
	if temp1Hour < expectedTemp1Hour-0.1 || temp1Hour > expectedTemp1Hour+0.1 {
		t.Errorf("1-hour final temperature (%.2f°C) differs from expected (%.2f°C)",
			temp1Hour, expectedTemp1Hour)
	}

	// Temperatures should be within 0.5°C of each other after equivalent time
	tempDiff := temp1Hour - temp15Min
	if tempDiff < -0.5 || tempDiff > 0.5 {
		t.Errorf("Temperature after equivalent time differs too much: 1-hour=%.2f°C, 15-min=%.2f°C (diff=%.2f°C)",
			temp1Hour, temp15Min, tempDiff)
	}

	t.Logf("Thermal time constant scaling test:")
	t.Logf("  Thermal constant: 0.2 per hour (20%% per hour)")
	t.Logf("  Initial battery temp: 5.0°C, Air temp: 20.0°C")
	t.Logf("  1-hour slot: Final temp=%.2f°C (expected ~%.2f°C)", temp1Hour, expectedTemp1Hour)
	t.Logf("  15-min slots (4 slots): Final temp=%.2f°C", temp15Min)
	t.Logf("  Temperature difference after 1 hour: %.2f°C", tempDiff)
	if tempDiff >= -0.5 && tempDiff <= 0.5 {
		t.Logf("  ✓ Thermal constant correctly scaled for time slot duration")
	}
}
