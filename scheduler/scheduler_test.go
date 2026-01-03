package scheduler

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/devskill-org/ems/entsoe"
	"github.com/devskill-org/ems/miners"
)

// mockEnergyPricesServer creates a mock HTTP server that returns valid energy prices XML
func mockEnergyPricesServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a valid XML response with prices
		now := time.Now()
		hourStart := now.Truncate(time.Hour)

		xml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Publication_MarketDocument xmlns="urn:iec62325.351:tc57wg16:451-3:publicationdocument:7:0">
    <mRID>test-document</mRID>
    <revisionNumber>1</revisionNumber>
    <type>A44</type>
    <sender_MarketParticipant.mRID codingScheme="A01">10X1001A1001A450</sender_MarketParticipant.mRID>
    <sender_MarketParticipant.marketRole.type>A32</sender_MarketParticipant.marketRole.type>
    <receiver_MarketParticipant.mRID codingScheme="A01">10X1001A1001A450</receiver_MarketParticipant.mRID>
    <receiver_MarketParticipant.marketRole.type>A33</receiver_MarketParticipant.marketRole.type>
    <createdDateTime>%s</createdDateTime>
    <period.timeInterval>
        <start>%s</start>
        <end>%s</end>
    </period.timeInterval>
    <TimeSeries>
        <mRID>1</mRID>
        <businessType>A62</businessType>
        <in_Domain.mRID codingScheme="A01">10YLV-1001A00074</in_Domain.mRID>
        <out_Domain.mRID codingScheme="A01">10YLV-1001A00074</out_Domain.mRID>
        <currency_Unit.name>EUR</currency_Unit.name>
        <price_Measure_Unit.name>MWH</price_Measure_Unit.name>
        <curveType>A01</curveType>
        <Period>
            <timeInterval>
                <start>%s</start>
                <end>%s</end>
            </timeInterval>
            <resolution>PT1H</resolution>
            <Point>
                <position>1</position>
                <price.amount>45.50</price.amount>
            </Point>
            <Point>
                <position>2</position>
                <price.amount>42.00</price.amount>
            </Point>
            <Point>
                <position>3</position>
                <price.amount>48.75</price.amount>
            </Point>
        </Period>
    </TimeSeries>
</Publication_MarketDocument>`,
			now.Format(time.RFC3339),
			hourStart.Add(-1*time.Hour).Format(time.RFC3339),
			hourStart.Add(3*time.Hour).Format(time.RFC3339),
			hourStart.Add(-1*time.Hour).Format(time.RFC3339),
			hourStart.Add(3*time.Hour).Format(time.RFC3339))

		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(xml))
	}))
}

// mockMinerDiscovery returns an empty list of miners to avoid real network requests
func mockMinerDiscovery(ctx context.Context, network string) []*miners.AvalonQHost {
	return []*miners.AvalonQHost{}
}

// testConfig creates a basic config for testing with all required fields
func testConfig() *Config {
	return testConfigWithServer(nil)
}

// testConfigWithServer creates a test config that uses the provided mock server
func testConfigWithServer(server *httptest.Server) *Config {
	urlFormat := "https://example.com/api?periodStart=%s&periodEnd=%s&token=%s"
	if server != nil {
		urlFormat = server.URL + "?periodStart=%s&periodEnd=%s&token=%s"
	}

	return &Config{
		PriceLimit:               50.0,
		Network:                  "192.168.1.0/24",
		CheckPriceInterval:       time.Minute,
		MinersStateCheckInterval: time.Minute,
		MinerDiscoveryInterval:   10 * time.Minute,
		PVPollInterval:           10 * time.Second,
		PVIntegrationPeriod:      15 * time.Minute,
		APITimeout:               5 * time.Second,
		MinerTimeout:             5 * time.Second,
		MPCExecutionInterval:     time.Minute,
		Location:                 "Europe/Riga",
		SecurityToken:            "test-token",
		UrlFormat:                urlFormat,
	}
}

func TestNewMinerScheduler(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		logger *log.Logger
	}{
		{
			name:   "valid parameters",
			config: &Config{PriceLimit: 50.0, Network: "192.168.1.0/24", MinerDiscoveryInterval: 10 * time.Minute},
			logger: log.New(os.Stdout, "TEST", log.LstdFlags),
		},
		{
			name:   "nil logger",
			config: &Config{PriceLimit: 75.5, Network: "10.0.0.0/16", MinerDiscoveryInterval: 10 * time.Minute},
			logger: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheduler := NewMinerScheduler(tt.config, tt.logger)

			if scheduler == nil {
				t.Fatal("NewMinerScheduler returned nil")
			}

			scheduler.minerDiscoveryFunc = mockMinerDiscovery

			status := scheduler.GetStatus()

			if status.IsRunning {
				t.Error("New scheduler should not be running")
			}

			if tt.logger == nil && scheduler.logger == nil {
				t.Error("Expected default logger when nil provided")
			}
		})
	}
}

func TestDryRunConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		dryRun bool
	}{
		{
			name:   "dry run enabled",
			dryRun: true,
		},
		{
			name:   "dry run disabled",
			dryRun: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := testConfig()
			config.DryRun = tt.dryRun

			scheduler := NewMinerScheduler(config, nil)

			if scheduler == nil {
				t.Fatal("NewMinerScheduler returned nil")
			}

			scheduler.minerDiscoveryFunc = mockMinerDiscovery

			actualConfig := scheduler.GetConfig()
			if actualConfig.DryRun != tt.dryRun {
				t.Errorf("Expected DryRun to be %v, got %v", tt.dryRun, actualConfig.DryRun)
			}
		})
	}
}

func TestSchedulerRunningState(t *testing.T) {
	mockServer := mockEnergyPricesServer()
	defer mockServer.Close()

	scheduler := NewMinerScheduler(testConfigWithServer(mockServer), nil)
	scheduler.minerDiscoveryFunc = mockMinerDiscovery

	// Initially not running
	if scheduler.IsRunning() {
		t.Error("New scheduler should not be running")
	}

	// Test starting and stopping with context cancellation
	ctx, cancel := context.WithCancel(context.Background())

	// Start scheduler in goroutine
	done := make(chan error, 1)
	go func() {
		done <- scheduler.Start(ctx, false)
	}()

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

	if !scheduler.IsRunning() {
		t.Error("Scheduler should be running after Start()")
	}

	// Cancel context to stop
	cancel()

	// Wait for completion
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Unexpected error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Scheduler did not stop within timeout")
	}

	if scheduler.IsRunning() {
		t.Error("Scheduler should not be running after context cancellation")
	}
}

func TestSchedulerDoubleStart(t *testing.T) {
	mockServer := mockEnergyPricesServer()
	defer mockServer.Close()

	scheduler := NewMinerScheduler(testConfigWithServer(mockServer), nil)
	scheduler.minerDiscoveryFunc = mockMinerDiscovery
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start first instance
	done1 := make(chan error, 1)
	go func() {
		done1 <- scheduler.Start(ctx, false)
	}()

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

	// Try to start second instance
	err := scheduler.Start(ctx, false)
	if err == nil {
		t.Error("Expected error when starting scheduler twice")
	}

	// Clean up
	cancel()
	<-done1
}

func TestSchedulerStop(t *testing.T) {
	mockServer := mockEnergyPricesServer()
	defer mockServer.Close()

	scheduler := NewMinerScheduler(testConfigWithServer(mockServer), nil)
	scheduler.minerDiscoveryFunc = mockMinerDiscovery
	ctx := context.Background()

	// Start scheduler
	done := make(chan error, 1)
	go func() {
		done <- scheduler.Start(ctx, false)
	}()

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

	if !scheduler.IsRunning() {
		t.Error("Scheduler should be running")
	}

	// Stop scheduler
	scheduler.Stop()

	// Wait for completion
	select {
	case <-done:
		// Expected
	case <-time.After(2 * time.Second):
		t.Error("Scheduler did not stop within timeout")
	}

	if scheduler.IsRunning() {
		t.Error("Scheduler should not be running after Stop()")
	}
}

func TestGetDiscoveredMiners(t *testing.T) {
	scheduler := NewMinerScheduler(testConfig(), nil)
	scheduler.minerDiscoveryFunc = mockMinerDiscovery

	// Initially empty
	minersList := scheduler.GetDiscoveredMiners()
	if len(minersList) != 0 {
		t.Errorf("Expected 0 miners initially, got %d", len(minersList))
	}

	// Mock some miners
	mockMiners := []*miners.AvalonQHost{
		{Address: "192.168.1.100", Port: 4028},
		{Address: "192.168.1.101", Port: 4028},
	}

	scheduler.mu.Lock()
	scheduler.discoveredMiners = make(map[string]*miners.AvalonQHost)
	for _, miner := range mockMiners {
		key := fmt.Sprintf("%s:%d", miner.Address, miner.Port)
		scheduler.discoveredMiners[key] = miner
	}
	scheduler.mu.Unlock()

	// Test getting miners
	retrievedMiners := scheduler.GetDiscoveredMiners()
	if len(retrievedMiners) != len(mockMiners) {
		t.Errorf("Expected %d miners, got %d", len(mockMiners), len(retrievedMiners))
	}

	// Test that returned slice is a copy (modifying it shouldn't affect original)
	if len(retrievedMiners) > 0 {
		retrievedMiners[0] = nil
		originalMiners := scheduler.GetDiscoveredMiners()
		if originalMiners[0] == nil {
			t.Error("Modifying returned slice should not affect original")
		}
	}
}

func TestDiscoverMinersPreservesExisting(t *testing.T) {
	config := testConfig()
	config.Network = "127.0.0.1/32"
	scheduler := NewMinerScheduler(config, nil)
	// Mock discovery to return no miners (simulating network scan that finds nothing)
	scheduler.minerDiscoveryFunc = mockMinerDiscovery

	// Mock some initial miners
	initialMiners := []*miners.AvalonQHost{
		{Address: "192.168.1.100", Port: 4028},
		{Address: "192.168.1.101", Port: 4028},
	}

	scheduler.mu.Lock()
	scheduler.discoveredMiners = make(map[string]*miners.AvalonQHost)
	for _, miner := range initialMiners {
		key := fmt.Sprintf("%s:%d", miner.Address, miner.Port)
		scheduler.discoveredMiners[key] = miner
	}
	scheduler.mu.Unlock()

	// Run discovery (should not find anything on 127.0.0.1/32, but existing miners should be preserved)
	err := scheduler.discoverMiners(context.Background())
	if err != nil {
		t.Errorf("Discovery failed: %v", err)
	}

	// Check that existing miners are still there
	finalMiners := scheduler.GetDiscoveredMiners()
	if len(finalMiners) != len(initialMiners) {
		t.Errorf("Expected %d miners after discovery, got %d", len(initialMiners), len(finalMiners))
	}

	// Check that the exact same miners are still there (order doesn't matter since they come from a map)
	minerMap := make(map[string]bool)
	for _, miner := range finalMiners {
		key := fmt.Sprintf("%s:%d", miner.Address, miner.Port)
		minerMap[key] = true
	}

	for _, expectedMiner := range initialMiners {
		key := fmt.Sprintf("%s:%d", expectedMiner.Address, expectedMiner.Port)
		if !minerMap[key] {
			t.Errorf("Expected miner %s:%d to be present, but it was not found",
				expectedMiner.Address, expectedMiner.Port)
		}
	}
}

func TestGetStatus(t *testing.T) {
	scheduler := NewMinerScheduler(testConfig(), nil)
	scheduler.minerDiscoveryFunc = mockMinerDiscovery

	status := scheduler.GetStatus()

	if status.IsRunning {
		t.Error("Expected running status false")
	}

	if status.MinersCount != 0 {
		t.Errorf("Expected miners count 0, got %d", status.MinersCount)
	}

	if status.HasMarketData {
		t.Error("Expected has no market data")
	}
}

func TestSchedulerStatus_WithData(t *testing.T) {
	scheduler := NewMinerScheduler(testConfig(), nil)
	scheduler.minerDiscoveryFunc = mockMinerDiscovery

	// Add some mock data
	mockMiners := []*miners.AvalonQHost{
		{Address: "192.168.1.100", Port: 4028},
		{Address: "192.168.1.101", Port: 4028},
	}

	mockDoc := &entsoe.PublicationMarketData{
		MRID:            "test-doc",
		CreatedDateTime: "2024-01-15T10:30:00Z",
	}

	scheduler.mu.Lock()
	scheduler.discoveredMiners = make(map[string]*miners.AvalonQHost)
	for _, miner := range mockMiners {
		key := fmt.Sprintf("%s:%d", miner.Address, miner.Port)
		scheduler.discoveredMiners[key] = miner
	}
	scheduler.pricesMarketData = mockDoc
	scheduler.mu.Unlock()

	status := scheduler.GetStatus()

	if status.MinersCount != len(mockMiners) {
		t.Errorf("Expected miners count %d, got %d", len(mockMiners), status.MinersCount)
	}

	if !status.HasMarketData {
		t.Error("Expected has market data")
	}
}

func TestSchedulerConcurrency(t *testing.T) {
	scheduler := NewMinerScheduler(testConfig(), nil)
	scheduler.minerDiscoveryFunc = mockMinerDiscovery

	// Test concurrent access to methods
	done := make(chan bool, 10)

	// Concurrent readers
	for range 5 {
		go func() {
			defer func() { done <- true }()
			for range 100 {
				_ = scheduler.GetDiscoveredMiners()
				_ = scheduler.GetStatus()
				_ = scheduler.IsRunning()
			}
		}()
	}

	// Concurrent writers
	for i := range 5 {
		go func(id int) {
			defer func() { done <- true }()
			for range 100 {
				scheduler.SetConfig(&Config{PriceLimit: 50.0 + float64(id), Network: "192.168." + string(rune('1'+id)) + ".0/24", MinerDiscoveryInterval: 10 * time.Minute})
			}
		}(i)
	}

	// Wait for all goroutines to complete
	for range 10 {
		select {
		case <-done:
			// OK
		case <-time.After(5 * time.Second):
			t.Fatal("Concurrent test timed out")
		}
	}
}

// Benchmark tests
func BenchmarkSchedulerGetStatus(b *testing.B) {
	scheduler := NewMinerScheduler(testConfig(), nil)
	scheduler.minerDiscoveryFunc = mockMinerDiscovery

	// Add some mock data
	mockMiners := make([]*miners.AvalonQHost, 100)
	for i := range 100 {
		mockMiners[i] = &miners.AvalonQHost{
			Address: "192.168.1." + string(rune(100+i)),
			Port:    4028,
		}
	}
	scheduler.discoveredMiners = make(map[string]*miners.AvalonQHost)
	for _, miner := range mockMiners {
		key := fmt.Sprintf("%s:%d", miner.Address, miner.Port)
		scheduler.discoveredMiners[key] = miner
	}

	for b.Loop() {
		_ = scheduler.GetStatus()
	}
}

func TestGetInitialDelay(t *testing.T) {
	tests := []struct {
		name               string
		priceCheckInterval time.Duration
		stateCheckInterval time.Duration
		now                time.Time
		expectedDelay      time.Duration
	}{
		{
			name:               "at start of hour with 15min interval",
			priceCheckInterval: 15 * time.Minute,
			stateCheckInterval: 1 * time.Minute,
			now:                time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			expectedDelay:      0 * time.Minute,
		},
		{
			name:               "5 minutes into hour with 15min interval",
			priceCheckInterval: 15 * time.Minute,
			stateCheckInterval: 1 * time.Minute,
			now:                time.Date(2024, 1, 15, 10, 5, 0, 0, time.UTC),
			expectedDelay:      10 * time.Minute,
		},
		{
			name:               "exactly at 15min mark with 15min interval",
			priceCheckInterval: 15 * time.Minute,
			stateCheckInterval: 1 * time.Minute,
			now:                time.Date(2024, 1, 15, 10, 15, 0, 0, time.UTC),
			expectedDelay:      0 * time.Minute,
		},
		{
			name:               "17 minutes into hour with 15min interval",
			priceCheckInterval: 15 * time.Minute,
			now:                time.Date(2024, 1, 15, 10, 17, 0, 0, time.UTC),
			expectedDelay:      13 * time.Minute,
		},
		{
			name:               "at 30min mark with 15min interval",
			priceCheckInterval: 15 * time.Minute,
			stateCheckInterval: 1 * time.Minute,
			now:                time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
			expectedDelay:      0 * time.Minute,
		},
		{
			name:               "45 minutes into hour with 15min interval",
			priceCheckInterval: 15 * time.Minute,
			stateCheckInterval: 1 * time.Minute,
			now:                time.Date(2024, 1, 15, 10, 45, 0, 0, time.UTC),
			expectedDelay:      0 * time.Minute,
		},
		{
			name:               "50 minutes into hour with 15min interval",
			priceCheckInterval: 15 * time.Minute,
			stateCheckInterval: 1 * time.Minute,
			now:                time.Date(2024, 1, 15, 10, 50, 0, 0, time.UTC),
			expectedDelay:      10 * time.Minute,
		},
		{
			name:               "at start of hour with 30min interval",
			priceCheckInterval: 30 * time.Minute,
			stateCheckInterval: 1 * time.Minute,
			now:                time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			expectedDelay:      0 * time.Minute,
		},
		{
			name:               "10 minutes into hour with 30min interval",
			priceCheckInterval: 30 * time.Minute,
			stateCheckInterval: 1 * time.Minute,
			now:                time.Date(2024, 1, 15, 10, 10, 0, 0, time.UTC),
			expectedDelay:      20 * time.Minute,
		},
		{
			name:               "exactly at 30min mark with 30min interval",
			priceCheckInterval: 30 * time.Minute,
			now:                time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
			expectedDelay:      0 * time.Minute,
		},
		{
			name:               "40 minutes into hour with 30min interval",
			priceCheckInterval: 30 * time.Minute,
			stateCheckInterval: 1 * time.Minute,
			now:                time.Date(2024, 1, 15, 10, 40, 0, 0, time.UTC),
			expectedDelay:      20 * time.Minute,
		},
		{
			name:               "at start of hour with 1hour interval",
			priceCheckInterval: 60 * time.Minute,
			stateCheckInterval: 1 * time.Minute,
			now:                time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			expectedDelay:      0 * time.Minute,
		},
		{
			name:               "30 minutes into hour with 1hour interval",
			priceCheckInterval: 60 * time.Minute,
			stateCheckInterval: 1 * time.Minute,
			now:                time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
			expectedDelay:      30 * time.Minute,
		},
		{
			name:               "with seconds precision",
			priceCheckInterval: 15 * time.Minute,
			stateCheckInterval: 1 * time.Minute,
			now:                time.Date(2024, 1, 15, 10, 5, 30, 0, time.UTC),
			expectedDelay:      9*time.Minute + 30*time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				PriceLimit:               50.0,
				Network:                  "192.168.1.0/24",
				CheckPriceInterval:       tt.priceCheckInterval,
				MinersStateCheckInterval: tt.stateCheckInterval,
				MinerDiscoveryInterval:   10 * time.Minute,
			}
			scheduler := NewMinerScheduler(config, nil)

			actualDelay := scheduler.getInitialDelay(tt.now, tt.priceCheckInterval)

			if actualDelay != tt.expectedDelay {
				t.Errorf("Expected delay %v, got %v", tt.expectedDelay, actualDelay)
			}

			// Verify that the delay is always positive
			if actualDelay < 0 {
				t.Errorf("Expected positive delay, got %v", actualDelay)
			}

			// Verify that the delay is less than or equal to the check interval
			if actualDelay > tt.priceCheckInterval {
				t.Errorf("Expected delay (%v) to be less than or equal to check interval (%v)", actualDelay, tt.priceCheckInterval)
			}
		})
	}
}

func BenchmarkSchedulerGetDiscoveredMiners(b *testing.B) {
	scheduler := NewMinerScheduler(testConfig(), nil)
	scheduler.minerDiscoveryFunc = mockMinerDiscovery

	// Add mock miners
	mockMiners := make([]*miners.AvalonQHost, 1000)
	for i := range 1000 {
		mockMiners[i] = &miners.AvalonQHost{
			Address: "192.168." + string(rune(i/256)) + "." + string(rune(i%256)),
			Port:    4028,
		}
	}
	scheduler.discoveredMiners = make(map[string]*miners.AvalonQHost)
	for _, miner := range mockMiners {
		key := fmt.Sprintf("%s:%d", miner.Address, miner.Port)
		scheduler.discoveredMiners[key] = miner
	}

	for b.Loop() {
		_ = scheduler.GetDiscoveredMiners()
	}
}

func TestMinersStateCheckInterval(t *testing.T) {
	config := testConfig()
	config.MinersStateCheckInterval = 2 * time.Second
	scheduler := NewMinerScheduler(config, nil)
	scheduler.minerDiscoveryFunc = mockMinerDiscovery

	if scheduler.config.MinersStateCheckInterval != 2*time.Second {
		t.Errorf("Expected MinersStateCheckInterval 2s, got %v", scheduler.config.MinersStateCheckInterval)
	}

	// Test that default is 1 minute
	defaultConfig := DefaultConfig()
	if defaultConfig.MinersStateCheckInterval != 1*time.Minute {
		t.Errorf("Expected default MinersStateCheckInterval 1m, got %v", defaultConfig.MinersStateCheckInterval)
	}
}

func TestRunStateCheckDryRun(t *testing.T) {
	config := testConfig()
	config.DryRun = true
	config.MinersStateCheckInterval = 10 * time.Second

	scheduler := NewMinerScheduler(config, nil)
	scheduler.minerDiscoveryFunc = mockMinerDiscovery

	// Test that runStateCheck doesn't panic with no miners
	scheduler.runStateCheck(context.Background())

	// Verify the method exists and can be called
	if scheduler.config.DryRun != true {
		t.Error("Expected DryRun to be true")
	}
}

func TestMockMinerDiscovery(t *testing.T) {
	config := testConfig()
	scheduler := NewMinerScheduler(config, nil)

	// Create a custom mock that tracks if it was called
	mockCalled := false
	customMock := func(ctx context.Context, network string) []*miners.AvalonQHost {
		mockCalled = true
		// Return some test miners
		return []*miners.AvalonQHost{
			{Address: "192.168.1.100", Port: 4028},
			{Address: "192.168.1.101", Port: 4028},
		}
	}

	scheduler.minerDiscoveryFunc = customMock

	// Run discovery
	err := scheduler.discoverMiners(context.Background())
	if err != nil {
		t.Errorf("discoverMiners failed: %v", err)
	}

	// Verify mock was called
	if !mockCalled {
		t.Error("Expected mock discovery function to be called")
	}

	// Verify miners were discovered
	miners := scheduler.GetDiscoveredMiners()
	if len(miners) != 2 {
		t.Errorf("Expected 2 miners, got %d", len(miners))
	}
}
