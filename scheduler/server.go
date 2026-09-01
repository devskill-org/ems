package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/sixdouglas/suncalc"
)

// WebServer provides HTTP endpoints for health checking, monitoring, and web UI
type WebServer struct {
	scheduler *MinerScheduler
	server    *http.Server
	port      int
	startTime time.Time
	upgrader  websocket.Upgrader
	clients   sync.Map
	broadcast chan []byte
	done      chan struct{}
}

// StatusResponse represents the health check response
type StatusResponse struct {
	Status    string       `json:"status"`
	Timestamp string       `json:"timestamp"`
	Version   string       `json:"version,omitempty"`
	Scheduler Health       `json:"scheduler"`
	System    SystemHealth `json:"system"`
	EMS       EMSHealth    `json:"ems"`
	Sun       SunInfo      `json:"sun"`
}

// Health represents scheduler-specific health information
type Health struct {
	IsRunning          bool              `json:"is_running"`
	MinersCount        int               `json:"miners_count"`
	LastCheck          *time.Time        `json:"last_check,omitempty"`
	HasMarketData      bool              `json:"has_market_data"`
	LastDocumentTime   *time.Time        `json:"last_document_time,omitempty"`
	PriceLimit         float64           `json:"price_limit"`
	Network            string            `json:"network"`
	CheckPriceInterval string            `json:"check_price_interval"`
	MPCDecisions       []MPCDecisionInfo `json:"mpc_decisions,omitempty"`
}

// MPCDecisionInfo represents MPC optimization decision information for API
type MPCDecisionInfo struct {
	Hour                  int     `json:"hour"`
	Timestamp             int64   `json:"timestamp"`
	BatteryChargeFromPV   float64 `json:"battery_charge_from_pv"`
	BatteryChargeFromGrid float64 `json:"battery_charge_from_grid"`
	BatteryDischarge      float64 `json:"battery_discharge"`
	GridImport            float64 `json:"grid_import"`
	GridExport            float64 `json:"grid_export"`
	BatterySOC            float64 `json:"battery_soc"`
	Profit                float64 `json:"profit"`
	BatteryPreHeatActive  bool    `json:"battery_preheat_active"`
	// Forecast data used for this decision
	ImportPrice        float64 `json:"import_price"`
	ExportPrice        float64 `json:"export_price"`
	SolarForecast      float64 `json:"solar_forecast"`
	LoadForecast       float64 `json:"load_forecast"`
	CloudCoverage      float64 `json:"cloud_coverage"`
	WeatherSymbol      string  `json:"weather_symbol"`
	BatteryAvgCellTemp float64 `json:"battery_avg_cell_temp"`
	AirTemperature     float64 `json:"air_temperature"`
}

// SystemHealth represents system-level health information
type SystemHealth struct {
	Uptime     string `json:"uptime"`
	Memory     string `json:"memory,omitempty"`
	Goroutines int    `json:"goroutines,omitempty"`
}

// EMSHealth represents energy management system health information
type EMSHealth struct {
	CurrentPVPower        float64 `json:"current_pv_power"`
	ESSPower              float64 `json:"ess_power"`
	ESSSOC                float64 `json:"ess_soc"`
	GridSensorStatus      uint16  `json:"grid_sensor_status"`
	GridSensorActivePower float64 `json:"grid_sensor_active_power"`
	PlantActivePower      float64 `json:"plant_active_power"`
	DCChargerOutputPower  float64 `json:"dc_charger_output_power"`
	DCChargerVehicleSOC   float64 `json:"dc_charger_vehicle_soc"`
}

// SunInfo represents solar position and timing information
type SunInfo struct {
	SolarAngle float64 `json:"solar_angle"`
	Sunrise    string  `json:"sunrise"`
	Sunset     string  `json:"sunset"`
}

// MetricsSummary represents aggregated metrics data
type MetricsSummary struct {
	TotalImportCost float64 `json:"total_import_cost"`
	TotalExportCost float64 `json:"total_export_cost"`
	TotalImportKWh  float64 `json:"total_import_kwh"`
	TotalExportKWh  float64 `json:"total_export_kwh"`
	StartTime       string  `json:"start_time"`
	EndTime         string  `json:"end_time"`
}

// NewWebServer creates a new web server with health endpoints and static file serving
func NewWebServer(scheduler *MinerScheduler, port int) *WebServer {
	if port <= 0 {
		return nil // Health server disabled
	}

	mux := http.NewServeMux()
	hs := &WebServer{
		scheduler: scheduler,
		port:      port,
		startTime: time.Now(),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(_ *http.Request) bool {
				return true // Allow all origins in development
			},
		},
		broadcast: make(chan []byte, 256),
		done:      make(chan struct{}),
		server: &http.Server{
			Addr:         fmt.Sprintf(":%d", port),
			Handler:      mux,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
	}

	// Register API routes
	mux.HandleFunc("/api/health", hs.healthHandler)
	mux.HandleFunc("/api/ready", hs.readinessHandler)
	mux.HandleFunc("/api/ws", hs.wsHandler)
	mux.HandleFunc("/api/metrics/summary", hs.metricsSummaryHandler)
	mux.HandleFunc("/api/miners/discover", hs.minersDiscoverHandler)
	mux.HandleFunc("/api/config", hs.configHandler)
	mux.HandleFunc("/api/market-data/upload", hs.marketDataUploadHandler)
	mux.HandleFunc("/api/market-data/cache", hs.marketDataCacheHandler)
	mux.HandleFunc("/api/market-data/download", hs.marketDataDownloadHandler)

	// Serve static files from web folder
	fs := http.FileServer(http.Dir("./web/dist"))
	mux.Handle("/", fs)

	return hs
}

// Start starts the web server
func (hs *WebServer) Start() error {
	if hs == nil {
		return nil // Web server disabled
	}

	// Start the broadcast handler
	go hs.handleBroadcasts()

	// Start periodic status broadcaster
	go hs.broadcastStatus()

	go func() {
		if err := hs.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Log error but don't crash the main application
			fmt.Printf("Web server error: %v\n", err)
		}
	}()

	return nil
}

// Stop gracefully stops the web server
func (hs *WebServer) Stop(ctx context.Context) error {
	if hs == nil {
		return nil // Web server disabled
	}

	// Signal goroutines to stop
	close(hs.done)

	// Close all WebSocket connections
	hs.clients.Range(func(key, _ any) bool {
		if conn, ok := key.(*websocket.Conn); ok {
			conn.Close() //nolint:gosec
		}
		return true
	})

	return hs.server.Shutdown(ctx)
}

// healthHandler handles the /api/health endpoint
func (hs *WebServer) healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := hs.scheduler.GetStatus()

	// Get MPC decisions and convert to API format
	mpcDecisions := hs.scheduler.GetMPCDecisions()
	mpcDecisionsInfo := make([]MPCDecisionInfo, 0, len(mpcDecisions))
	for _, dec := range mpcDecisions {
		mpcDecisionsInfo = append(mpcDecisionsInfo, MPCDecisionInfo{
			Hour:                  dec.Hour,
			Timestamp:             dec.Timestamp,
			BatteryChargeFromPV:   dec.BatteryChargeFromPV,
			BatteryChargeFromGrid: dec.BatteryChargeFromGrid,
			BatteryDischarge:      dec.BatteryDischarge,
			GridImport:            dec.GridImport,
			GridExport:            dec.GridExport,
			BatterySOC:            dec.BatterySOC,
			Profit:                dec.Profit,
			BatteryPreHeatActive:  dec.BatteryPreHeatActive,
			ImportPrice:           dec.ImportPrice,
			ExportPrice:           dec.ExportPrice,
			SolarForecast:         dec.SolarForecast,
			LoadForecast:          dec.LoadForecast,
			CloudCoverage:         dec.CloudCoverage,
			WeatherSymbol:         dec.WeatherSymbol,
			BatteryAvgCellTemp:    dec.BatteryAvgCellTemp,
			AirTemperature:        dec.AirTemperature,
		})
	}

	response := StatusResponse{
		Status:    "healthy",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Version:   "1.0.0",
		Scheduler: Health{
			IsRunning:     status.IsRunning,
			MinersCount:   status.MinersCount,
			HasMarketData: status.HasMarketData,
			PriceLimit:    hs.scheduler.GetConfig().PriceLimit,
			Network:       hs.scheduler.GetConfig().Network,
			MPCDecisions:  mpcDecisionsInfo,
		},
		System: SystemHealth{
			Uptime:     formatUptime(time.Since(hs.startTime)),
			Goroutines: 0, // Placeholder - would need runtime.NumGoroutine()
		},
	}

	// Determine overall health status
	w.Header().Set("Content-Type", "application/json")
	if !status.IsRunning {
		response.Status = "unhealthy"
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("healthHandler: failed to encode response: %v", err)
	}
}

// readinessHandler handles the /api/ready endpoint
func (hs *WebServer) readinessHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := hs.scheduler.GetStatus()

	ready := map[string]any{
		"ready":     status.IsRunning,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")

	if !status.IsRunning {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	if err := json.NewEncoder(w).Encode(ready); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// minersDiscoverHandler handles the POST /api/miners/discover endpoint
// It triggers an immediate miner discovery run in the background.
func (hs *WebServer) minersDiscoverHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := hs.scheduler.RunMinerDiscovery(ctx); err != nil {
			fmt.Printf("On-demand miner discovery error: %v\n", err)
		}
		hs.scheduler.refreshMinersState(ctx)
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "discovery started"})
}

// configHandler handles GET and PUT /api/config endpoints.
// GET returns the current scheduler configuration as JSON.
// PUT accepts a full or partial JSON body, merges it over the current config,
// validates it, and applies it to the running scheduler in memory.
func (hs *WebServer) configHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		cfg := hs.scheduler.GetConfig()
		if err := json.NewEncoder(w).Encode(cfg); err != nil {
			http.Error(w, "Failed to encode config", http.StatusInternalServerError)
		}

	case http.MethodPut:
		// Decode the incoming JSON on top of a copy of the current config so
		// fields that are not present in the request body keep their values.
		current := hs.scheduler.GetConfig()

		// Marshal current config to JSON then unmarshal into a generic map so
		// we can merge the incoming fields without losing duration formatting.
		currentJSON, err := json.Marshal(current)
		if err != nil {
			http.Error(w, `{"error":"failed to read current config"}`, http.StatusInternalServerError)
			return
		}

		var merged map[string]any
		if err := json.Unmarshal(currentJSON, &merged); err != nil {
			http.Error(w, `{"error":"failed to parse current config"}`, http.StatusInternalServerError)
			return
		}

		var incoming map[string]any
		if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
			http.Error(w, `{"error":"invalid JSON body"}`, http.StatusBadRequest)
			return
		}

		// Shallow-merge: incoming keys overwrite current keys.
		for k, v := range incoming {
			merged[k] = v
		}

		// Re-encode the merged map and decode it through Config's custom
		// UnmarshalJSON so that duration strings are handled correctly.
		mergedJSON, err := json.Marshal(merged)
		if err != nil {
			http.Error(w, `{"error":"failed to merge config"}`, http.StatusInternalServerError)
			return
		}

		newCfg := &Config{}
		if err := json.Unmarshal(mergedJSON, newCfg); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		if err := newCfg.Validate(); err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		hs.scheduler.SetConfig(newCfg)

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(newCfg)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// metricsSummaryHandler handles the /api/metrics/summary endpoint
func (hs *WebServer) metricsSummaryHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse query parameters for time range
	startTimeStr := r.URL.Query().Get("start_time")
	endTimeStr := r.URL.Query().Get("end_time")

	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" && endTimeStr != "" {
		// Use provided time range
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			http.Error(w, "Invalid start_time format. Use RFC3339 format", http.StatusBadRequest)
			return
		}
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			http.Error(w, "Invalid end_time format. Use RFC3339 format", http.StatusBadRequest)
			return
		}
	} else {
		// Default to yesterday (calendar past date - midnight to midnight)
		now := time.Now()
		yesterday := now.AddDate(0, 0, -1)
		startTime = time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 0, 0, 0, 0, yesterday.Location())
		endTime = time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(), 23, 59, 59, 999999999, yesterday.Location())
	}

	// Query the database for aggregated metrics
	db := hs.scheduler.db
	if db == nil {
		http.Error(w, "Database not available", http.StatusServiceUnavailable)
		return
	}

	fmt.Println("Fetching data from", startTime, endTime)

	var summary MetricsSummary
	err = db.QueryRow(`
		SELECT
			COALESCE(SUM(grid_import_cost), 0) as total_import_cost,
			COALESCE(SUM(grid_export_cost), 0) as total_export_cost,
			COALESCE(SUM(grid_import_power), 0) as total_import_kwh,
			COALESCE(SUM(grid_export_power), 0) as total_export_kwh
		FROM metrics
		WHERE timestamp >= $1 AND timestamp <= $2
		AND metric_name = 'energy_flow'
	`, startTime, endTime).Scan(
		&summary.TotalImportCost,
		&summary.TotalExportCost,
		&summary.TotalImportKWh,
		&summary.TotalExportKWh,
	)

	if err != nil {
		fmt.Printf("Failed to query metrics: %v\n", err)
		http.Error(w, "Failed to query metrics", http.StatusInternalServerError)
		return
	}

	summary.StartTime = startTime.Format(time.RFC3339)
	summary.EndTime = endTime.Format(time.RFC3339)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(summary); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// wsHandler handles WebSocket connections
func (hs *WebServer) wsHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := hs.upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Printf("WebSocket upgrade error: %v\n", err)
		return
	}

	// Register new client
	mu := &sync.Mutex{}
	hs.clients.Store(conn, mu)

	clientCount := 0
	hs.clients.Range(func(_, _ any) bool {
		clientCount++
		return true
	})
	fmt.Printf("New WebSocket client connected. Total clients: %d\n", clientCount)

	// Send initial data immediately
	mu.Lock()
	hs.sendStatusToClient(conn)
	mu.Unlock()

	// Handle client disconnection
	defer func() {
		hs.clients.Delete(conn)
		conn.Close() //nolint:gosec

		clientCount := 0
		hs.clients.Range(func(_, _ any) bool {
			clientCount++
			return true
		})
		fmt.Printf("WebSocket client disconnected. Total clients: %d\n", clientCount)
	}()

	// Read messages from client (ping/pong, close)
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				fmt.Printf("WebSocket error: %v\n", err)
			}
			break
		}
	}
}

// handleBroadcasts sends messages to all connected clients
func (hs *WebServer) handleBroadcasts() {
	for {
		select {
		case message := <-hs.broadcast:
			hs.clients.Range(func(key, val any) bool {
				conn, ok := key.(*websocket.Conn)
				if !ok {
					return true
				}
				mu, ok := val.(*sync.Mutex)
				if !ok {
					return true
				}

				mu.Lock()
				err := conn.WriteMessage(websocket.TextMessage, message)
				mu.Unlock()
				if err != nil {
					fmt.Printf("WebSocket write error: %v\n", err)
					conn.Close() //nolint:gosec
					hs.clients.Delete(conn)
				}
				return true
			})
		case <-hs.done:
			return
		}
	}
}

// broadcastStatus periodically broadcasts status updates
func (hs *WebServer) broadcastStatus() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			hasClients := false
			hs.clients.Range(func(_, _ any) bool {
				hasClients = true
				return false // Stop after finding first client
			})

			if hasClients {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				data := hs.buildStatusData(ctx)
				cancel()
				message, err := json.Marshal(data)
				if err != nil {
					fmt.Printf("Failed to marshal status data: %v\n", err)
					continue
				}
				hs.broadcast <- message
			}
		case <-hs.done:
			return
		}
	}
}

// sendStatusToClient sends status data to a specific client
func (hs *WebServer) sendStatusToClient(conn *websocket.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data := hs.buildStatusData(ctx)
	if err := conn.WriteJSON(data); err != nil {
		fmt.Printf("Failed to send initial data: %v\n", err)
	}
}

// buildStatusData builds combined health and status data
func (hs *WebServer) buildStatusData(ctx context.Context) map[string]any {
	status := hs.scheduler.GetStatus()
	miners := hs.scheduler.GetDiscoveredMiners()
	doc := hs.scheduler.GetPricesMarketData()

	// Build miners list with detailed status
	minersList := make([]map[string]any, 0, len(miners))
	minersHealthy := true

	for _, miner := range miners {
		minerStatus := "Unknown"

		if miner.LastStats != nil {
			state := miner.LastStats.State.String()
			workMode := miner.LastStats.WorkMode.String()
			minerStatus = fmt.Sprintf("%s (%s)", state, workMode)
		} else if miner.LastStatsError != nil {
			minerStatus = "Error"
			minersHealthy = false
		}

		minerEntry := map[string]any{
			"ip":     miner.Address,
			"status": minerStatus,
		}
		if miner.LastStats != nil {
			minerEntry["dna"] = miner.LastStats.DNA
			minerEntry["fan_r"] = miner.LastStats.FanR
			// Filter is a cumulative odometer (seconds) counting total air-filter
			// runtime since manufacture/last reset. 120 000 s (~33 h) is the
			// Avalon-recommended cleaning interval, so we express usage as a
			// percentage of that lifetime threshold.
			const filterLifetimeSeconds = 120_000
			filterUsage := int(math.Round(float64(miner.LastStats.Filter) / filterLifetimeSeconds * 100))
			minerEntry["filter_usage"] = filterUsage
		}
		minersList = append(minersList, minerEntry)
	}

	// Determine overall health status
	overallStatus := "healthy"
	if !status.IsRunning {
		overallStatus = "unhealthy"
	} else if len(miners) > 0 && !minersHealthy {
		overallStatus = "degraded"
	}

	// Get MPC decisions and convert to API format
	mpcDecisions := hs.scheduler.GetMPCDecisions()
	mpcDecisionsInfo := make([]MPCDecisionInfo, 0, len(mpcDecisions))
	for _, dec := range mpcDecisions {
		mpcDecisionsInfo = append(mpcDecisionsInfo, MPCDecisionInfo{
			Hour:                  dec.Hour,
			Timestamp:             dec.Timestamp,
			BatteryChargeFromPV:   dec.BatteryChargeFromPV,
			BatteryChargeFromGrid: dec.BatteryChargeFromGrid,
			BatteryDischarge:      dec.BatteryDischarge,
			GridImport:            dec.GridImport,
			GridExport:            dec.GridExport,
			BatterySOC:            dec.BatterySOC,
			Profit:                dec.Profit,
			BatteryPreHeatActive:  dec.BatteryPreHeatActive,
			ImportPrice:           dec.ImportPrice,
			ExportPrice:           dec.ExportPrice,
			SolarForecast:         dec.SolarForecast,
			LoadForecast:          dec.LoadForecast,
			CloudCoverage:         dec.CloudCoverage,
			WeatherSymbol:         dec.WeatherSymbol,
			BatteryAvgCellTemp:    dec.BatteryAvgCellTemp,
			AirTemperature:        dec.AirTemperature,
		})
	}

	health := StatusResponse{
		Status:    overallStatus,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Version:   "1.0.0",
		Scheduler: Health{
			IsRunning:     status.IsRunning,
			MinersCount:   status.MinersCount,
			HasMarketData: status.HasMarketData,
			PriceLimit:    hs.scheduler.GetConfig().PriceLimit,
			Network:       hs.scheduler.GetConfig().Network,
			MPCDecisions:  mpcDecisionsInfo,
		},
		System: SystemHealth{
			Uptime:     formatUptime(time.Since(hs.startTime)),
			Goroutines: 0,
		},
	}

	info := hs.scheduler.GetPlantRunningInfo(ctx)
	if info != nil {
		health.EMS = EMSHealth{
			CurrentPVPower:        info.PhotovoltaicPower,
			ESSPower:              info.ESSPower,
			ESSSOC:                info.ESSSOC,
			GridSensorStatus:      info.GridSensorStatus,
			GridSensorActivePower: info.GridSensorActivePower,
			PlantActivePower:      info.PlantActivePower,
			DCChargerOutputPower:  info.DCChargerOutputPower,
			DCChargerVehicleSOC:   info.DCChargerVehicleSOC,
		}
	}

	// Calculate sun information
	config := hs.scheduler.GetConfig()
	now := time.Now()
	sunTimes := suncalc.GetTimes(now, config.Latitude, config.Longitude)
	sunPos := suncalc.GetPosition(now, config.Latitude, config.Longitude)

	health.Sun = SunInfo{
		SolarAngle: sunPos.Altitude * 180 / math.Pi, // Convert radians to degrees
		Sunrise:    sunTimes["sunrise"].Value.Format(time.RFC3339),
		Sunset:     sunTimes["sunset"].Value.Format(time.RFC3339),
	}

	priceData := map[string]any{
		"has_document": doc != nil,
	}

	if doc != nil {
		priceData["document_id"] = doc.MRID
		priceData["created_at"] = doc.CreatedDateTime

		if price, found := doc.LookupPriceByTime(time.Now()); found {
			priceData["current_price"] = price
			priceData["current"] = price
			priceData["limit"] = hs.scheduler.GetConfig().PriceLimit
		}
	}

	return map[string]any{
		"type":   "status_update",
		"health": health,
		"status": map[string]any{
			"scheduler_status": status,
			"miners": map[string]any{
				"count": len(miners),
				"list":  minersList,
			},
			"price_data": priceData,
			"timestamp":  time.Now().UTC().Format(time.RFC3339),
		},
	}
}

// Helper functions

// formatUptime formats a duration as a string with seconds rounded to integer
// marketDataUploadHandler handles POST /api/market-data/upload.
// It expects a multipart/form-data body with two fields:
//   - "date" – a date string in YYYY-MM-DD format.
//   - "file" – the XML document (Publication_MarketDocument).
//
// On success the parsed document is stored in the XML document cache so that
// DownloadPublicationMarketData will use it instead of fetching from ENTSO-E.
func (hs *WebServer) marketDataUploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Limit request body to 10 MB to prevent memory exhaustion.
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)

	// Parse multipart form – limit body to 10 MB.
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to parse form: " + err.Error()})
		return
	}

	// Validate date field.
	date := r.FormValue("date")
	if date == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "date field is required"})
		return
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid date format, expected YYYY-MM-DD"})
		return
	}

	// Retrieve uploaded file.
	file, _, err := r.FormFile("file")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "file field is required"})
		return
	}
	defer file.Close()

	xmlData, err := io.ReadAll(file)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to read uploaded file"})
		return
	}

	// Parse the XML and store it in the cache.
	if err := hs.scheduler.StoreMarketDataXML(date, xmlData); err != nil {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to parse XML: " + err.Error()})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "date": date})
}

// marketDataCacheHandler handles GET and DELETE /api/market-data/cache.
//
// GET returns a JSON object with an "entries" array listing all cached documents.
// DELETE removes a single entry identified by the "date" query parameter (YYYY-MM-DD).
func (hs *WebServer) marketDataCacheHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		type EntryInfo struct {
			Date       string    `json:"date"`
			UploadedAt time.Time `json:"uploaded_at"`
			Source     string    `json:"source"`
		}

		raw := hs.scheduler.GetXMLCacheEntries()
		result := make([]EntryInfo, 0, len(raw))
		for _, e := range raw {
			result = append(result, EntryInfo{
				Date:       e.Date,
				UploadedAt: e.UploadedAt,
				Source:     string(e.Source),
			})
		}
		sort.Slice(result, func(i, j int) bool {
			return result[i].Date < result[j].Date
		})

		_ = json.NewEncoder(w).Encode(map[string]any{"entries": result})

	case http.MethodDelete:
		date := r.URL.Query().Get("date")
		if date == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "date query parameter is required"})
			return
		}

		hs.scheduler.DeleteMarketDataXML(date)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "date": date})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// marketDataDownloadHandler handles GET /api/market-data/download.
// Optional query parameter: ?date=YYYY-MM-DD
// Returns the raw XML document for the specified date (or today's date if omitted).
func (hs *WebServer) marketDataDownloadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	date := r.URL.Query().Get("date")
	if date == "" {
		location, err := time.LoadLocation(hs.scheduler.config.Location)
		if err != nil {
			location = time.UTC
		}
		date = time.Now().In(location).Format("2006-01-02")
	}

	rawXML, ok := hs.scheduler.xmlCache.GetRaw(date)
	if !ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("no XML document found for date %s", date)})
		return
	}

	w.Header().Set("Content-Type", "application/xml")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"Energy_Prices_%s.xml\"", date))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(rawXML)
}

func formatUptime(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%dh%dm%ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
