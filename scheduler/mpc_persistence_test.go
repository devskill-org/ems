package scheduler

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/devskill-org/ems/mpc"
)

func makeTestDataService(t *testing.T) (string, *httptest.Server) {
	t.Helper()

	var decisions []mpc.ControlDecision

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mpc/save":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			var saved []mpc.ControlDecision
			if err := json.NewDecoder(r.Body).Decode(&saved); err != nil {
				http.Error(w, "invalid JSON", http.StatusBadRequest)
				return
			}
			decisions = saved
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":          "ok",
				"decisions_saved": len(saved),
			})
		case "/mpc/get":
			if r.Method != http.MethodGet {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if len(decisions) == 0 {
				json.NewEncoder(w).Encode([]mpc.ControlDecision{})
				return
			}
			json.NewEncoder(w).Encode(decisions)
		default:
			http.NotFound(w, r)
		}
	}))

	return server.URL, server
}

func TestMPCDataSaveAndLoad(t *testing.T) {
	url, server := makeTestDataService(t)
	defer server.Close()

	config := &Config{
		DataServiceURL: url,
	}
	scheduler := &MinerScheduler{
		config:            config,
		dataServiceClient: newDataServiceClient(config),
		logger:            log.New(os.Stdout, "TEST: ", log.LstdFlags),
	}

	now := time.Now().Unix()
	decisions := []mpc.ControlDecision{
		{
			Hour:                  0,
			Timestamp:             now + 3600,
			BatteryChargeFromPV:   10.5,
			BatteryChargeFromGrid: 5.0,
			BatteryDischarge:      0,
			GridImport:            5.0,
			GridExport:            0,
			BatterySOC:            0.6,
			Profit:                2.5,
			ImportPrice:           0.1,
			ExportPrice:           0.05,
			SolarForecast:         15.0,
			LoadForecast:          10.0,
			CloudCoverage:         30.0,
			WeatherSymbol:         "clearsky_day",
			BatteryAvgCellTemp:    15.0,
			AirTemperature:        18.0,
			BatteryPreHeatActive:  false,
		},
		{
			Hour:                  1,
			Timestamp:             now + 7200,
			BatteryChargeFromPV:   0,
			BatteryChargeFromGrid: 0,
			BatteryDischarge:      8.0,
			GridImport:            0,
			GridExport:            3.0,
			BatterySOC:            0.5,
			Profit:                3.2,
			ImportPrice:           0.12,
			ExportPrice:           0.06,
			SolarForecast:         20.0,
			LoadForecast:          12.0,
			CloudCoverage:         10.0,
			WeatherSymbol:         "fair_day",
			BatteryAvgCellTemp:    8.0,
			AirTemperature:        5.0,
			BatteryPreHeatActive:  true,
		},
	}

	ctx := context.Background()

	// Save decisions
	err := scheduler.saveMPCDecisions(ctx, decisions)
	if err != nil {
		t.Fatalf("Failed to save decisions: %v", err)
	}

	// Load decisions
	loaded, err := scheduler.loadLatestMPCDecisions(ctx)
	if err != nil {
		t.Fatalf("Failed to load decisions: %v", err)
	}

	// Verify loaded decisions
	if len(loaded) != len(decisions) {
		t.Errorf("Expected %d decisions, got %d", len(decisions), len(loaded))
	}

	for i, decision := range loaded {
		if decision.Timestamp != decisions[i].Timestamp {
			t.Errorf("Decision %d: expected timestamp %d, got %d", i, decisions[i].Timestamp, decision.Timestamp)
		}
		if decision.BatteryChargeFromPV != decisions[i].BatteryChargeFromPV {
			t.Errorf("Decision %d: expected battery_charge_from_pv %.2f, got %.2f", i, decisions[i].BatteryChargeFromPV, decision.BatteryChargeFromPV)
		}
		if decision.BatteryChargeFromGrid != decisions[i].BatteryChargeFromGrid {
			t.Errorf("Decision %d: expected battery_charge_from_grid %.2f, got %.2f", i, decisions[i].BatteryChargeFromGrid, decision.BatteryChargeFromGrid)
		}
		if decision.Profit != decisions[i].Profit {
			t.Errorf("Decision %d: expected profit %.2f, got %.2f", i, decisions[i].Profit, decision.Profit)
		}
		if decision.BatteryAvgCellTemp != decisions[i].BatteryAvgCellTemp {
			t.Errorf("Decision %d: expected battery_avg_cell_temp %.2f, got %.2f", i, decisions[i].BatteryAvgCellTemp, decision.BatteryAvgCellTemp)
		}
		if decision.AirTemperature != decisions[i].AirTemperature {
			t.Errorf("Decision %d: expected air_temperature %.2f, got %.2f", i, decisions[i].AirTemperature, decision.AirTemperature)
		}
		if decision.BatteryPreHeatActive != decisions[i].BatteryPreHeatActive {
			t.Errorf("Decision %d: expected battery_preheat_active %v, got %v", i, decisions[i].BatteryPreHeatActive, decision.BatteryPreHeatActive)
		}
	}
}

func TestMPCDataReplaceDecisions(t *testing.T) {
	url, server := makeTestDataService(t)
	defer server.Close()

	config := &Config{
		DataServiceURL: url,
	}
	scheduler := &MinerScheduler{
		config:            config,
		dataServiceClient: newDataServiceClient(config),
		logger:            log.New(os.Stdout, "TEST: ", log.LstdFlags),
	}

	now := time.Now().Unix()
	ctx := context.Background()

	// First save: hours 0-2
	firstDecisions := []mpc.ControlDecision{
		{Hour: 0, Timestamp: now + 3600, Profit: 1.0},
		{Hour: 1, Timestamp: now + 7200, Profit: 2.0},
		{Hour: 2, Timestamp: now + 10800, Profit: 3.0},
	}
	err := scheduler.saveMPCDecisions(ctx, firstDecisions)
	if err != nil {
		t.Fatalf("Failed to save first decisions: %v", err)
	}

	// Verify
	loaded, err := scheduler.loadLatestMPCDecisions(ctx)
	if err != nil {
		t.Fatalf("Failed to load decisions: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("Expected 3 decisions after first save, got %d", len(loaded))
	}
	// Data-service replaces the entire list, so second save replaces all.
	// Second save: hours 3-5 (replaces first 3)
	secondDecisions := []mpc.ControlDecision{
		{Hour: 3, Timestamp: now + 14400, Profit: 40.0},
		{Hour: 4, Timestamp: now + 18000, Profit: 50.0},
		{Hour: 5, Timestamp: now + 21600, Profit: 60.0},
	}
	err = scheduler.saveMPCDecisions(ctx, secondDecisions)
	if err != nil {
		t.Fatalf("Failed to save second decisions: %v", err)
	}

	// Verify: only the new 3 decisions should remain
	loaded, err = scheduler.loadLatestMPCDecisions(ctx)
	if err != nil {
		t.Fatalf("Failed to load decisions: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("Expected 3 decisions after second save (replace), got %d", len(loaded))
	}
	if loaded[0].Hour != 3 || loaded[0].Profit != 40.0 {
		t.Errorf("Expected first decision to be hour 3 profit 40.0, got hour %d profit %.2f", loaded[0].Hour, loaded[0].Profit)
	}
	if loaded[2].Hour != 5 || loaded[2].Profit != 60.0 {
		t.Errorf("Expected last decision to be hour 5 profit 60.0, got hour %d profit %.2f", loaded[2].Hour, loaded[2].Profit)
	}
}

func TestMPCDataNoDataService(t *testing.T) {
	config := &Config{
		DataServiceURL: "",
	}
	scheduler := &MinerScheduler{
		config:            config,
		dataServiceClient: newDataServiceClient(config),
		logger:            log.New(os.Stdout, "TEST: ", log.LstdFlags),
	}

	ctx := context.Background()

	// Saving with no data-service should be a no-op (nil error)
	err := scheduler.saveMPCDecisions(ctx, nil)
	if err != nil {
		t.Fatalf("save with no service should not error: %v", err)
	}

	// Loading with no data-service should return empty (nil error)
	loaded, err := scheduler.loadLatestMPCDecisions(ctx)
	if err != nil {
		t.Fatalf("load with no service should not error: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("Expected empty list, got %d decisions", len(loaded))
	}
}

func TestMPCDataEmptyList(t *testing.T) {
	url, server := makeTestDataService(t)
	defer server.Close()

	config := &Config{
		DataServiceURL: url,
	}
	scheduler := &MinerScheduler{
		config:            config,
		dataServiceClient: newDataServiceClient(config),
		logger:            log.New(os.Stdout, "TEST: ", log.LstdFlags),
	}

	ctx := context.Background()

	// Save empty list
	err := scheduler.saveMPCDecisions(ctx, []mpc.ControlDecision{})
	if err != nil {
		t.Fatalf("Failed to save empty decisions: %v", err)
	}

	// Loading should return empty
	loaded, err := scheduler.loadLatestMPCDecisions(ctx)
	if err != nil {
		t.Fatalf("Failed to load decisions: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("Expected empty list, got %d decisions", len(loaded))
	}
}
