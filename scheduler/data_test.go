package scheduler

import (
	"testing"
	"time"
)

func TestDataSamples_IntegrateSamplesWithPeriodBoundary(t *testing.T) {
	samples := &DataSamples{}
	pollInterval := 10 * time.Second
	integrationPeriod := 1 * time.Minute

	// Create a base time for testing
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	// Add samples for first period (12:00:00 to 12:00:50)
	// 6 samples: 0s, 10s, 20s, 30s, 40s, 50s
	for i := range 6 {
		ts := baseTime.Add(time.Duration(i) * pollInterval)
		samples.AddSample(100.0, 50.0, 30.0, 10.0, 80.0, 25.0, ts)
	}

	// Add samples for second period (12:01:00 to 12:01:50)
	// 6 samples: 60s, 70s, 80s, 90s, 100s, 110s
	for i := range 6 {
		ts := baseTime.Add(integrationPeriod).Add(time.Duration(i) * pollInterval)
		samples.AddSample(200.0, 100.0, 60.0, 20.0, 75.0, 26.0, ts)
	}

	// Integrate first period only (cutoff at 12:01:00)
	// This includes samples at 0s, 10s, 20s, 30s, 40s, 50s (6 samples)
	// The sample at exactly 60s (12:01:00) is also included because we use <=
	cutoffTime := baseTime.Add(integrationPeriod)
	data := samples.IntegrateSamples(pollInterval, cutoffTime)

	// Verify samples were integrated (6 from first period + 1 at boundary = 7)
	if data.sampleCount != 7 {
		t.Errorf("Expected 7 samples integrated, got %d", data.sampleCount)
	}

	// Verify timestamp is the cutoff time
	if !data.timestamp.Equal(cutoffTime) {
		t.Errorf("Expected timestamp %v, got %v", cutoffTime, data.timestamp)
	}

	// Verify energy calculations (6 samples from first period + 1 at boundary from second period)
	// First 6 samples: 100W each, last sample: 200W
	expectedEnergy := (100.0*6 + 200.0*1) * (pollInterval.Seconds() / 3600.0)
	if data.pvTotalPower < expectedEnergy-0.001 || data.pvTotalPower > expectedEnergy+0.001 {
		t.Errorf("Expected PV power ~%.3f kWh, got %.3f kWh", expectedEnergy, data.pvTotalPower)
	}

	// Verify samples are still present (not cleared)
	if samples.IsEmpty() {
		t.Error("Samples should not be cleared after integration")
	}
}

func TestDataSamples_ClearBefore(t *testing.T) {
	samples := &DataSamples{}
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	// Add samples across two periods (0s to 110s)
	// 12 samples: 0s, 10s, 20s, 30s, 40s, 50s, 60s, 70s, 80s, 90s, 100s, 110s
	for i := range 12 {
		ts := baseTime.Add(time.Duration(i) * 10 * time.Second)
		samples.AddSample(100.0, 50.0, 30.0, 10.0, 80.0, 25.0, ts)
	}

	// Clear samples before and at 1 minute mark (60s)
	// This removes samples at: 0s, 10s, 20s, 30s, 40s, 50s, 60s (7 samples)
	cutoffTime := baseTime.Add(1 * time.Minute)
	samples.ClearBefore(cutoffTime)

	samples.mu.Lock()
	sampleCount := len(samples.samples)
	firstSampleTime := samples.samples[0].ts
	samples.mu.Unlock()

	// Should have 5 samples remaining (samples at 70s, 80s, 90s, 100s, 110s)
	if sampleCount != 5 {
		t.Errorf("Expected 5 samples remaining, got %d", sampleCount)
	}

	// First remaining sample should be after cutoff
	if !firstSampleTime.After(cutoffTime) {
		t.Errorf("First remaining sample at %v should be after cutoff %v", firstSampleTime, cutoffTime)
	}
}

func TestDataSamples_IntegrationPreservesForRetry(t *testing.T) {
	samples := &DataSamples{}
	pollInterval := 10 * time.Second
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	// Add 6 samples for first period (0s to 50s)
	for i := range 6 {
		ts := baseTime.Add(time.Duration(i) * pollInterval)
		samples.AddSample(100.0, 50.0, 30.0, 10.0, 80.0, 25.0, ts)
	}

	// Cutoff at 50s (not 60s) to avoid including second period samples
	cutoffTime := baseTime.Add(50 * time.Second)

	// First integration attempt
	data1 := samples.IntegrateSamples(pollInterval, cutoffTime)

	// Simulate failure - don't clear samples

	// Add more samples for second period (60s to 110s)
	for i := range 6 {
		ts := baseTime.Add(60 * time.Second).Add(time.Duration(i) * pollInterval)
		samples.AddSample(200.0, 100.0, 60.0, 20.0, 75.0, 26.0, ts)
	}

	// Retry integration for first period (same cutoff)
	data2 := samples.IntegrateSamples(pollInterval, cutoffTime)

	// Both integrations should produce identical results
	if data1.sampleCount != data2.sampleCount {
		t.Errorf("Retry produced different sample count: first=%d, retry=%d", data1.sampleCount, data2.sampleCount)
	}

	if data1.pvTotalPower != data2.pvTotalPower {
		t.Errorf("Retry produced different PV power: first=%.3f, retry=%.3f", data1.pvTotalPower, data2.pvTotalPower)
	}

	if !data1.timestamp.Equal(data2.timestamp) {
		t.Errorf("Retry produced different timestamp: first=%v, retry=%v", data1.timestamp, data2.timestamp)
	}

	// Both should have integrated exactly 6 samples (first period only, 0s-50s)
	if data2.sampleCount != 6 {
		t.Errorf("Expected 6 samples on retry, got %d", data2.sampleCount)
	}

	// Verify no samples from second period were included
	// First period has 6 samples at 100W each
	maxFirstPeriodPower := 100.0 * (pollInterval.Seconds() / 3600.0) * 6
	if data2.pvTotalPower > maxFirstPeriodPower+0.001 {
		t.Errorf("Second period samples should not be included in first period integration: expected max %.6f, got %.6f",
			maxFirstPeriodPower, data2.pvTotalPower)
	}

	// Now clear first period and integrate second period
	samples.ClearBefore(cutoffTime)
	cutoffTime2 := baseTime.Add(110 * time.Second)
	data3 := samples.IntegrateSamples(pollInterval, cutoffTime2)

	// Second period should also have 6 samples (60s-110s)
	if data3.sampleCount != 6 {
		t.Errorf("Expected 6 samples for second period, got %d", data3.sampleCount)
	}

	// Second period should have different (higher) values
	if data3.pvTotalPower <= data1.pvTotalPower {
		t.Errorf("Second period PV power (%.3f) should be higher than first period (%.3f)",
			data3.pvTotalPower, data1.pvTotalPower)
	}
}

func TestDataSamples_EmptyIntegration(t *testing.T) {
	samples := &DataSamples{}
	pollInterval := 10 * time.Second
	cutoffTime := time.Now()

	data := samples.IntegrateSamples(pollInterval, cutoffTime)

	if data.sampleCount != 0 {
		t.Errorf("Expected 0 samples, got %d", data.sampleCount)
	}

	if data.pvTotalPower != 0 {
		t.Errorf("Expected 0 PV power, got %.3f", data.pvTotalPower)
	}
}

func TestDataSamples_BoundaryConditions(t *testing.T) {
	samples := &DataSamples{}
	pollInterval := 10 * time.Second
	baseTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	// Add sample exactly at boundary
	samples.AddSample(100.0, 50.0, 30.0, 10.0, 80.0, 25.0, baseTime)

	// Add sample 1 nanosecond after boundary
	samples.AddSample(200.0, 100.0, 60.0, 20.0, 75.0, 26.0, baseTime.Add(1))

	// Integrate with cutoff at baseTime
	data := samples.IntegrateSamples(pollInterval, baseTime)

	// Should include the sample at exactly the boundary (<=)
	if data.sampleCount != 1 {
		t.Errorf("Expected 1 sample at boundary, got %d", data.sampleCount)
	}

	// Clear that sample
	samples.ClearBefore(baseTime)

	samples.mu.Lock()
	remaining := len(samples.samples)
	samples.mu.Unlock()

	// Should have 1 sample remaining (the one after boundary)
	if remaining != 1 {
		t.Errorf("Expected 1 sample remaining after clear, got %d", remaining)
	}
}

func TestIntegratedData_EnergyCalculations(t *testing.T) {
	samples := &DataSamples{}
	pollInterval := 10 * time.Second
	baseTime := time.Now()

	// Add samples with known values
	// Grid: positive = import, negative = export
	// Battery: positive = charge, negative = discharge
	samples.AddSample(
		1000.0, // PV: 1000W
		500.0,  // Grid import: 500W
		300.0,  // Battery charge: 300W
		100.0,  // EVDC: 100W
		85.0,   // SOC: 85%
		25.0,   // Battery temp: 25°C
		baseTime,
	)

	samples.AddSample(
		1000.0, // PV: 1000W
		-200.0, // Grid export: 200W
		-150.0, // Battery discharge: 150W
		0.0,    // EVDC: 0W
		83.0,   // SOC: 83%
		26.0,   // Battery temp: 26°C
		baseTime.Add(pollInterval),
	)

	cutoffTime := baseTime.Add(pollInterval)
	data := samples.IntegrateSamples(pollInterval, cutoffTime)

	// Energy in kWh = Power in W * time in hours / 1000
	// pollInterval = 10s = 10/3600 hours
	energyPerSample := pollInterval.Seconds() / 3600.0

	expectedPV := (1000.0 + 1000.0) * energyPerSample // 2 samples * 1000W
	expectedGridImport := 500.0 * energyPerSample
	expectedGridExport := 200.0 * energyPerSample
	expectedBatteryCharge := 300.0 * energyPerSample
	expectedBatteryDischarge := 150.0 * energyPerSample

	tolerance := 0.0001

	if abs(data.pvTotalPower-expectedPV) > tolerance {
		t.Errorf("PV power: expected %.6f, got %.6f", expectedPV, data.pvTotalPower)
	}

	if abs(data.gridImportPower-expectedGridImport) > tolerance {
		t.Errorf("Grid import: expected %.6f, got %.6f", expectedGridImport, data.gridImportPower)
	}

	if abs(data.gridExportPower-expectedGridExport) > tolerance {
		t.Errorf("Grid export: expected %.6f, got %.6f", expectedGridExport, data.gridExportPower)
	}

	if abs(data.batteryChargePower-expectedBatteryCharge) > tolerance {
		t.Errorf("Battery charge: expected %.6f, got %.6f", expectedBatteryCharge, data.batteryChargePower)
	}

	if abs(data.batteryDischargePower-expectedBatteryDischarge) > tolerance {
		t.Errorf("Battery discharge: expected %.6f, got %.6f", expectedBatteryDischarge, data.batteryDischargePower)
	}

	// Last SOC should be from last sample
	if data.batterySoc != 83.0 {
		t.Errorf("Battery SOC: expected 83.0, got %.1f", data.batterySoc)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func TestDataSamples_AveragePVPowerLast_Empty(t *testing.T) {
	samples := &DataSamples{}
	result := samples.AveragePVPowerLast(5 * time.Minute)
	if result != 0 {
		t.Errorf("expected 0 for empty buffer, got %.2f", result)
	}
}

func TestDataSamples_AveragePVPowerLast_AllInWindow(t *testing.T) {
	samples := &DataSamples{}
	now := time.Now()

	// Three readings within the last 5 minutes: 6, 8, 10 kW → avg 8
	samples.AddSample(6.0, 0, 0, 0, 80, 25, now.Add(-4*time.Minute))
	samples.AddSample(8.0, 0, 0, 0, 80, 25, now.Add(-2*time.Minute))
	samples.AddSample(10.0, 0, 0, 0, 80, 25, now.Add(-30*time.Second))

	result := samples.AveragePVPowerLast(5 * time.Minute)
	if abs(result-8.0) > 0.001 {
		t.Errorf("expected average 8.0, got %.4f", result)
	}
}

func TestDataSamples_AveragePVPowerLast_SmoothsCloudDip(t *testing.T) {
	// Simulates a sunny day where a cloud blocks the sun for one reading.
	// Previous readings: 10 kW; cloud dip: 0.5 kW; all within 5-minute window.
	// The average should be well above the instantaneous dip.
	samples := &DataSamples{}
	now := time.Now()

	for i := range 4 {
		samples.AddSample(10.0, 0, 0, 0, 80, 25, now.Add(-time.Duration(4-i)*time.Minute))
	}
	// Cloud dip: most recent reading is very low
	samples.AddSample(0.5, 0, 0, 0, 80, 25, now.Add(-10*time.Second))

	result := samples.AveragePVPowerLast(5 * time.Minute)

	// Average of [10, 10, 10, 10, 0.5] = 8.1; must be much higher than 0.5
	if result <= 5.0 {
		t.Errorf("cloud dip not smoothed: expected average > 5.0, got %.4f", result)
	}
}

func TestDataSamples_AveragePVPowerLast_FallbackToLatestWhenNoneInWindow(t *testing.T) {
	// All samples are older than the window — should fall back to most recent.
	samples := &DataSamples{}
	now := time.Now()

	samples.AddSample(5.0, 0, 0, 0, 80, 25, now.Add(-10*time.Minute))
	samples.AddSample(7.0, 0, 0, 0, 80, 25, now.Add(-8*time.Minute))

	result := samples.AveragePVPowerLast(5 * time.Minute)

	// No samples within 5-minute window → fall back to most recent reading (7.0)
	if abs(result-7.0) > 0.001 {
		t.Errorf("expected fallback to most recent reading 7.0, got %.4f", result)
	}
}

func TestDataSamples_AveragePVPowerLast_MixedWindowBoundary(t *testing.T) {
	// One sample just outside the window, two inside — only in-window samples averaged.
	samples := &DataSamples{}
	now := time.Now()

	samples.AddSample(100.0, 0, 0, 0, 80, 25, now.Add(-6*time.Minute)) // outside 5-min window
	samples.AddSample(4.0, 0, 0, 0, 80, 25, now.Add(-3*time.Minute))   // inside
	samples.AddSample(6.0, 0, 0, 0, 80, 25, now.Add(-1*time.Minute))   // inside

	result := samples.AveragePVPowerLast(5 * time.Minute)

	// Average of [4.0, 6.0] = 5.0; the 100 kW outlier must not be included
	if abs(result-5.0) > 0.001 {
		t.Errorf("expected average 5.0 (in-window only), got %.4f", result)
	}
}
