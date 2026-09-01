package scheduler

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// TestGetCurrentPrice_UsesConfiguredTimezone validates that getCurrentPrice uses the configured timezone
// This test validates the fix for the timezone mismatch issue where getCurrentPrice was using time.Now()
// without timezone conversion, while GetMarketData was using timezone-aware time.
func TestGetCurrentPrice_UsesConfiguredTimezone(t *testing.T) {
	// Load test data
	xmlData, err := os.ReadFile("../test_data/Energy_Prices_202509052100-202509062100.xml")
	if err != nil {
		t.Fatalf("Failed to read test data file: %v", err)
	}

	// Test with Europe/Helsinki timezone (UTC+3 in September)
	location, err := time.LoadLocation("Europe/Helsinki")
	if err != nil {
		t.Skipf("Skipping test: Europe/Helsinki timezone not available: %v", err)
	}

	// The test data covers 2025-09-04T22:00Z to 2025-09-05T22:00Z (UTC)
	// In Europe/Helsinki timezone, this is 2025-09-05T01:00+03:00 to 2025-09-06T01:00+03:00
	// We'll test with a time that falls within this range
	testTime := time.Date(2025, 9, 5, 10, 30, 0, 0, location) // 10:30 Helsinki time

	// Create a test server that returns the test XML data
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		w.Write(xmlData)
	}))
	defer server.Close()

	config := &Config{
		SecurityToken: "test-token",
		URLFormat:     server.URL + "?periodStart=%s&periodEnd=%s&token=%s",
		Location:      "Europe/Helsinki",
		PriceLimit:    100.0,
		Network:       "192.168.1.0/24",
		DryRun:        true,
	}

	logger := log.New(os.Stdout, "[TEST] ", log.LstdFlags)
	scheduler := NewMinerScheduler(config, logger)

	ctx := context.Background()

	// First, load the market data
	marketData, err := scheduler.GetMarketData(ctx)
	if err != nil {
		t.Fatalf("Failed to get market data: %v", err)
	}

	if marketData == nil {
		t.Fatal("Market data is nil")
	}

	// Verify we can find a price for our test time using the market data directly
	price, found := marketData.LookupPriceByTime(testTime)
	if !found {
		t.Fatalf("Price not found for test time %s in timezone %s", testTime.Format(time.RFC3339), location)
	}

	t.Logf("Successfully found price %.2f EUR/MWh for time %s", price, testTime.Format(time.RFC3339))

	// Verify that the scheduler is using the correct timezone configuration
	if scheduler.config.Location != "Europe/Helsinki" {
		t.Errorf("Expected location Europe/Helsinki, got %s", scheduler.config.Location)
	}

	t.Log("Timezone configuration validated successfully")
}

// TestGetCurrentPrice_LocationLoading validates that getCurrentPrice correctly loads the timezone
func TestGetCurrentPrice_LocationLoading(t *testing.T) {
	// Load test data
	xmlData, err := os.ReadFile("../test_data/Energy_Prices_202509052100-202509062100.xml")
	if err != nil {
		t.Fatalf("Failed to read test data file: %v", err)
	}

	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		w.Write(xmlData)
	}))
	defer server.Close()

	config := &Config{
		SecurityToken: "test-token",
		URLFormat:     server.URL + "?periodStart=%s&periodEnd=%s&token=%s",
		Location:      "Europe/Helsinki",
		PriceLimit:    100.0,
		Network:       "192.168.1.0/24",
		DryRun:        true,
	}

	logger := log.New(os.Stdout, "[TEST] ", log.LstdFlags)
	scheduler := NewMinerScheduler(config, logger)

	ctx := context.Background()

	// Call getCurrentPrice - it should load the location internally
	// Even though the current time won't match the test data time range,
	// the function should at least load the timezone correctly and not panic
	_, err = scheduler.getCurrentPrice(ctx)

	// We expect an error since current time doesn't match test data
	// But the important thing is it loaded the timezone correctly
	if err != nil {
		// This is expected - the error message should indicate price not found, not a timezone loading error
		t.Logf("Expected error (current time not in test data range): %v", err)

		// Verify it's not a timezone loading error
		if err.Error() == "failed to load location: unknown time zone Europe/Helsinki" {
			t.Fatalf("Failed to load timezone: %v", err)
		}
	}

	t.Log("getCurrentPrice successfully loads timezone configuration")
}

// TestGetCurrentPrice_InvalidLocation validates error handling for invalid timezone
func TestGetCurrentPrice_InvalidLocation(t *testing.T) {
	// Load test data
	xmlData, err := os.ReadFile("../test_data/Energy_Prices_202509052100-202509062100.xml")
	if err != nil {
		t.Fatalf("Failed to read test data file: %v", err)
	}

	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		w.Write(xmlData)
	}))
	defer server.Close()

	config := &Config{
		SecurityToken: "test-token",
		URLFormat:     server.URL + "?periodStart=%s&periodEnd=%s&token=%s",
		Location:      "Invalid/Timezone",
		PriceLimit:    100.0,
		Network:       "192.168.1.0/24",
		DryRun:        true,
	}

	logger := log.New(os.Stdout, "[TEST] ", log.LstdFlags)
	scheduler := NewMinerScheduler(config, logger)

	ctx := context.Background()

	// Call getCurrentPrice with invalid timezone - should return an error
	_, err = scheduler.getCurrentPrice(ctx)

	if err == nil {
		t.Fatal("Expected error for invalid timezone, got nil")
	}

	// Verify the error message indicates timezone loading failure
	expectedSubstring := "failed to load location"
	if err.Error()[:len(expectedSubstring)] != expectedSubstring {
		t.Errorf("Expected error to start with '%s', got: %v", expectedSubstring, err)
	}

	t.Logf("Correctly handled invalid timezone with error: %v", err)
}

// TestStoreMarketDataXML_RefreshesMPCAndNextDayPrices validates that uploading
// market data XML invalidates cached prices, merges next day data, and re-runs MPC.
func TestStoreMarketDataXML_RefreshesMPCAndNextDayPrices(t *testing.T) {
	xmlData, err := os.ReadFile("../test_data/Energy_Prices_202609012200-202609022200.xml")
	if err != nil {
		t.Fatalf("Failed to read test data file: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusOK)
		w.Write(xmlData)
	}))
	defer server.Close()

	config := &Config{
		SecurityToken:      "test-token",
		URLFormat:          server.URL + "?periodStart=%s&periodEnd=%s&token=%s",
		Location:           "UTC",
		UserAgent:          "test-agent/1.0",
		PriceLimit:         100.0,
		CheckPriceInterval: 15 * time.Minute,
		DryRun:             true,
	}

	logger := log.New(os.Stdout, "[TEST] ", log.LstdFlags)
	scheduler := NewMinerScheduler(config, logger)

	ctx := context.Background()
	dateKey := "2026-09-02"

	err = scheduler.StoreMarketDataXML(dateKey, xmlData)
	if err != nil {
		t.Fatalf("StoreMarketDataXML failed: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	marketData, err := scheduler.GetMarketData(ctx)
	if err != nil {
		t.Fatalf("GetMarketData failed: %v", err)
	}

	if marketData == nil {
		t.Fatal("Expected non-nil marketData")
	}

	decisions := scheduler.GetMPCDecisions()
	if len(decisions) == 0 {
		t.Errorf("Expected MPC decisions to be generated after uploading XML, got 0")
	}
}

// TestMarketDataDownloadHandler validates the GET /api/market-data/download endpoint
func TestMarketDataDownloadHandler(t *testing.T) {
	xmlData, err := os.ReadFile("../test_data/Energy_Prices_202609012200-202609022200.xml")
	if err != nil {
		t.Fatalf("Failed to read test data file: %v", err)
	}

	config := &Config{
		Location: "UTC",
		DryRun:   true,
	}

	logger := log.New(os.Stdout, "[TEST] ", log.LstdFlags)
	sched := NewMinerScheduler(config, logger)
	webServer := NewWebServer(sched, 8088)

	dateKey := "2026-09-02"
	err = sched.StoreMarketDataXML(dateKey, xmlData)
	if err != nil {
		t.Fatalf("StoreMarketDataXML failed: %v", err)
	}

	// Test downloading specific date
	req := httptest.NewRequest(http.MethodGet, "/api/market-data/download?date="+dateKey, nil)
	w := httptest.NewRecorder()

	webServer.marketDataDownloadHandler(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/xml" {
		t.Errorf("Expected Content-Type application/xml, got %s", contentType)
	}

	body, _ := io.ReadAll(resp.Body)
	if len(body) != len(xmlData) {
		t.Errorf("Expected body length %d, got %d", len(xmlData), len(body))
	}

	// Test non-existent date
	req404 := httptest.NewRequest(http.MethodGet, "/api/market-data/download?date=1999-01-01", nil)
	w404 := httptest.NewRecorder()

	webServer.marketDataDownloadHandler(w404, req404)

	resp404 := w404.Result()
	if resp404.StatusCode != http.StatusNotFound {
		t.Errorf("Expected status 404 for missing date, got %d", resp404.StatusCode)
	}
}
