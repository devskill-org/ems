package scheduler

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/devskill-org/ems/entsoe"
	"github.com/devskill-org/ems/miners"
	"github.com/devskill-org/ems/mpc"
	_ "github.com/lib/pq" // PostgreSQL driver
)

// PeriodicTask represents a task that runs periodically with an optional initial delay
type PeriodicTask struct {
	name          string
	initialDelay  time.Duration
	interval      time.Duration
	runFunc       func() error
	retryInterval *time.Duration
	err           error
}

// nextAlignedTick returns the duration until the next wall-clock boundary that is
// a multiple of interval from the Unix epoch (i.e. aligned to absolute time, not
// relative to when the timer was created).
func nextAlignedTick(now time.Time, interval time.Duration) time.Duration {
	if interval <= 0 {
		return interval
	}
	// How far into the current interval are we?
	elapsed := now.UnixNano() % int64(interval)
	remaining := int64(interval) - elapsed
	return time.Duration(remaining)
}

// run executes the periodic task in a loop, respecting the initial delay and context cancellation.
// It uses wall-clock-aligned timers instead of a plain ticker so that execution boundaries
// stay in sync with absolute time regardless of how long each task run takes.
func (pt *PeriodicTask) run(ctx context.Context, stopChan <-chan struct{}, logger *log.Logger) {
	// Wait for initial delay
	if pt.initialDelay > 0 {
		logger.Printf("[%s] Waiting for initial delay: %v", pt.name, pt.initialDelay)
		select {
		case <-time.After(pt.initialDelay):
			// Initial delay passed, run the task
			logger.Printf("[%s] Initial delay passed, running first iteration", pt.name)
			pt.err = pt.runFunc()
		case <-ctx.Done():
			logger.Printf("[%s] Stopped during initial delay due to context cancellation", pt.name)
			return
		case <-stopChan:
			logger.Printf("[%s] Stopped during initial delay due to stop signal", pt.name)
			return
		}
	} else {
		// No initial delay, run immediately
		logger.Printf("[%s] Running immediately (no initial delay)", pt.name)
		pt.err = pt.runFunc()
	}

	// Use a wall-clock-aligned timer rather than a plain ticker.
	// After each tick we reset the timer to fire exactly at the next interval
	// boundary, compensating for any time spent inside runFunc and eliminating
	// cumulative drift.
	timer := time.NewTimer(nextAlignedTick(time.Now(), pt.interval))
	defer timer.Stop()

	// Retry timer — disabled (fires once per hour and is gated by pt.retryInterval==nil)
	// when retries are not configured.
	retryInterval := time.Hour
	if pt.retryInterval != nil {
		retryInterval = *pt.retryInterval
	}
	retryTimer := time.NewTimer(retryInterval)
	defer retryTimer.Stop()

	logger.Printf("[%s] Started with interval: %v", pt.name, pt.interval)

	for {
		select {
		case <-timer.C:
			pt.err = pt.runFunc()
			// Re-arm for the next aligned boundary.
			timer.Reset(nextAlignedTick(time.Now(), pt.interval))
		case <-retryTimer.C:
			if pt.retryInterval != nil && pt.err != nil {
				pt.err = pt.runFunc()
			}
			// Re-arm retry timer.
			retryTimer.Reset(retryInterval)
		case <-ctx.Done():
			logger.Printf("[%s] Stopped due to context cancellation", pt.name)
			return
		case <-stopChan:
			logger.Printf("[%s] Stopped due to stop signal", pt.name)
			return
		}
	}
}

// MinerScheduler manages energy system optimization, miner control, and scheduling tasks.
type MinerScheduler struct {
	// Configuration
	config *Config

	// State
	discoveredMiners       sync.Map // map[string]*miners.AvalonQHost
	pricesMarketData       *entsoe.PublicationMarketData
	pricesMarketDataExpiry time.Time
	isRunning              bool
	stopChan               chan struct{}
	mu                     sync.RWMutex

	// XML document cache for manually uploaded market data (bypasses ENTSO-E API).
	xmlCache *entsoe.XMLDocumentCache

	// Weather forecast cache
	weatherCache WeatherForecastCache

	// Solar irradiance forecast cache (Open-Meteo)
	solarForecastCache SolarForecastCache

	// MPC optimization results
	mpcDecisions         []mpc.ControlDecision
	lastExecutedDecision *mpc.ControlDecision // Tracks the last successfully executed decision

	// Web server
	webServer *WebServer

	// Database connection (kept for metrics, not used for MPC decisions)
	db *sql.DB

	// Data-service HTTP client for MPC decisions
	dataServiceClient *dataServiceClient

	// Logging
	logger *log.Logger

	// Test hooks for dependency injection
	minerDiscoveryFunc func(ctx context.Context, network string) []*miners.AvalonQHost
	openMeteoBaseURL   string // overrides Open-Meteo base URL when non-empty (testing only)
}

// NewMinerScheduler creates a new scheduler instance
func NewMinerScheduler(config *Config, logger *log.Logger) *MinerScheduler {
	if logger == nil {
		logger = log.Default()
	}

	scheduler := &MinerScheduler{
		config:            config,
		stopChan:          make(chan struct{}),
		logger:            logger,
		xmlCache:          entsoe.NewXMLDocumentCache(),
		dataServiceClient: newDataServiceClient(config),
		weatherCache: WeatherForecastCache{
			cacheDuration: 2 * time.Hour,
		},
		solarForecastCache: SolarForecastCache{
			cacheDuration: 1 * time.Hour,
		},
	}

	return scheduler
}

// NewMinerSchedulerWithHealthCheck creates a new scheduler instance with health check server
func NewMinerSchedulerWithHealthCheck(config *Config, logger *log.Logger) *MinerScheduler {
	scheduler := NewMinerScheduler(config, logger)
	scheduler.webServer = NewWebServer(scheduler, config.HealthCheckPort)
	return scheduler
}

// SetConfig updates the configuration for miner management
func (s *MinerScheduler) SetConfig(config *Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = config
}

// GetConfig returns the current configuration
func (s *MinerScheduler) GetConfig() *Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// GetDiscoveredMiners returns a copy of the currently discovered miners
func (s *MinerScheduler) GetDiscoveredMiners() []*miners.AvalonQHost {
	// Convert sync.Map to slice
	minersCopy := make([]*miners.AvalonQHost, 0)
	s.discoveredMiners.Range(func(_, value any) bool {
		if miner, ok := value.(*miners.AvalonQHost); ok {
			minersCopy = append(minersCopy, miner)
		}
		return true
	})
	return minersCopy
}

func (s *MinerScheduler) getInitialDelay(now time.Time, delayInterval time.Duration) time.Duration {
	top := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location())
	delay := now.Sub(top)
	for delay > 0 {
		delay = delay - delayInterval
	}
	return -delay
}

// Start begins the scheduler's periodic task
func (s *MinerScheduler) Start(ctx context.Context, serverOnly bool) error {
	s.mu.Lock()
	if s.isRunning {
		s.mu.Unlock()
		return fmt.Errorf("scheduler is already running")
	}
	s.isRunning = true
	s.stopChan = make(chan struct{})
	s.mu.Unlock()

	if s.config.DryRun {
		s.logger.Printf("DRY-RUN MODE ENABLED: Actions will be simulated only")
	}

	// Start web server if configured
	if s.webServer != nil {
		err := s.webServer.Start()
		if err != nil {
			s.logger.Printf("Failed to start web server: %v", err)
		} else {
			s.logger.Printf("Web server started on port %d", s.webServer.port)
		}
		if serverOnly {
			return err
		}
	}

	config := s.GetConfig()

	// Data integration state
	dataSamples := &DataSamples{}
	var dataDB *sql.DB
	var dataDBErr error
	if s.config.PostgresConnString != "" {
		dataDB, dataDBErr = sql.Open("postgres", s.config.PostgresConnString)
		if dataDBErr != nil {
			s.logger.Printf("Data integration: failed to connect to DB: %v", dataDBErr)
			dataDB = nil
		} else {
			s.db = dataDB
		}
	}

	// Load latest MPC decisions from data-service on startup
	for attempt := range 3 {
		if decisions, err := s.loadLatestMPCDecisions(ctx); err != nil {
			s.logger.Printf("Warning: Attempt %d failed to load MPC decisions from data-service: %v", attempt, err)
			time.Sleep(2 * time.Second)
		} else if len(decisions) > 0 {
			s.mu.Lock()
			s.mpcDecisions = decisions
			s.mu.Unlock()
			s.logger.Printf("Loaded %d MPC decisions from data-service on startup", len(decisions))
			break
		}
	}

	// Calculate initial delays
	now := time.Now()
	minersControlInitialDelay := s.getInitialDelay(now, config.CheckPriceInterval) + time.Second
	pvDataInitialDelay := s.getInitialDelay(now, config.PVIntegrationPeriod)
	stateCheckInitialDelay := s.getInitialDelay(now, config.MinersStateCheckInterval)
	mpcExecutionInitialDelay := s.getInitialDelay(now, config.MPCExecutionInterval) + 2*time.Second

	taskRetryInterval := time.Minute

	// Create periodic tasks
	tasks := []PeriodicTask{
		{
			name:         "MinerDiscovery",
			initialDelay: 0, // Run immediately
			interval:     config.MinerDiscoveryInterval,
			runFunc: func() error {
				return s.RunMinerDiscovery(ctx)
			},
		},
		{
			name:          "PriceCheck",
			initialDelay:  minersControlInitialDelay,
			interval:      config.CheckPriceInterval,
			retryInterval: &taskRetryInterval,
			runFunc: func() error {
				return s.runPriceCheck(ctx)
			},
		},
		{
			name:          "MarketDataRefresh",
			initialDelay:  0, // Run immediately to warm the cache on startup
			interval:      time.Minute,
			retryInterval: &taskRetryInterval,
			runFunc: func() error {
				return s.runMarketDataRefresh(ctx)
			},
		},
		{
			name:          "MPC",
			initialDelay:  minersControlInitialDelay,
			interval:      config.CheckPriceInterval,
			retryInterval: &taskRetryInterval,
			runFunc: func() error {
				return s.RunMPCOptimize(ctx)
			},
		},
		{
			name:         "StateCheck",
			initialDelay: stateCheckInitialDelay,
			interval:     config.MinersStateCheckInterval,
			runFunc: func() error {
				return s.runStateCheck(ctx)
			},
		},
		{
			name:         "DataPoll",
			initialDelay: 0,
			interval:     config.PVPollInterval,
			runFunc: func() error {
				return s.runDataPoll(ctx, dataSamples)
			},
		},
		{
			name:          "DataIntegration",
			initialDelay:  pvDataInitialDelay,
			interval:      config.PVIntegrationPeriod,
			retryInterval: &taskRetryInterval,
			runFunc: func() error {
				return s.runDataIntegration(dataSamples, config.PVPollInterval, dataDB, config.DeviceID, config.DryRun)
			},
		},
		{
			name:         "MPCExecution",
			initialDelay: mpcExecutionInitialDelay,
			interval:     config.MPCExecutionInterval,
			runFunc: func() error {
				return s.runMPCExecution(ctx)
			},
		},
	}

	// Start each periodic task in its own goroutine
	var wg sync.WaitGroup
	for _, task := range tasks {
		wg.Add(1)
		task := task // capture loop variable
		go func() {
			defer wg.Done()
			task.run(ctx, s.stopChan, s.logger)
		}()
	}

	// Wait for all tasks to complete
	wg.Wait()

	s.logger.Printf("All periodic tasks stopped")
	s.stop()
	return nil
}

// Stop gracefully stops the scheduler
func (s *MinerScheduler) Stop() {
	s.stop()
}

func (s *MinerScheduler) stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isRunning {
		return
	}

	s.isRunning = false

	// Close stopChan if it's not already closed
	select {
	case <-s.stopChan:
		// Already closed
	default:
		close(s.stopChan)
	}

	// Stop web server if running
	if s.webServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.webServer.Stop(ctx); err != nil {
			s.logger.Printf("Error stopping web server: %v", err)
		}
	}
}

// IsRunning returns whether the scheduler is currently running
func (s *MinerScheduler) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isRunning
}

// GetStatus returns the current status of the scheduler
func (s *MinerScheduler) GetStatus() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Count miners in sync.Map
	minersCount := 0
	s.discoveredMiners.Range(func(_, _ any) bool {
		minersCount++
		return true
	})

	return Status{
		IsRunning:     s.isRunning,
		MinersCount:   minersCount,
		HasMarketData: s.pricesMarketData != nil,
	}
}

// GetMPCDecisions returns a copy of the stored MPC decisions
func (s *MinerScheduler) GetMPCDecisions() []mpc.ControlDecision {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.mpcDecisions == nil {
		return nil
	}

	// Return a copy
	decisionsCopy := make([]mpc.ControlDecision, len(s.mpcDecisions))
	copy(decisionsCopy, s.mpcDecisions)
	return decisionsCopy
}

// Status represents the current status of the scheduler
type Status struct {
	IsRunning     bool `json:"is_running"`
	MinersCount   int  `json:"miners_count"`
	HasMarketData bool `json:"has_latest_document"`
}
