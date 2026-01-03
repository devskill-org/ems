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
	_ "github.com/lib/pq"
)

// PeriodicTask represents a task that runs periodically with an optional initial delay
type PeriodicTask struct {
	name         string
	initialDelay time.Duration
	interval     time.Duration
	runFunc      func()
}

// run executes the periodic task in a loop, respecting the initial delay and context cancellation
func (pt *PeriodicTask) run(ctx context.Context, stopChan <-chan struct{}, logger *log.Logger) {
	// Wait for initial delay
	if pt.initialDelay > 0 {
		logger.Printf("[%s] Waiting for initial delay: %v", pt.name, pt.initialDelay)
		select {
		case <-time.After(pt.initialDelay):
			// Initial delay passed, run the task
			logger.Printf("[%s] Initial delay passed, running first iteration", pt.name)
			pt.runFunc()
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
		pt.runFunc()
	}

	// Create ticker for periodic execution
	ticker := time.NewTicker(pt.interval)
	defer ticker.Stop()

	logger.Printf("[%s] Started with interval: %v", pt.name, pt.interval)

	for {
		select {
		case <-ticker.C:
			pt.runFunc()
		case <-ctx.Done():
			logger.Printf("[%s] Stopped due to context cancellation", pt.name)
			return
		case <-stopChan:
			logger.Printf("[%s] Stopped due to stop signal", pt.name)
			return
		}
	}
}

type MinerScheduler struct {
	// Configuration
	config *Config

	// State
	discoveredMiners       map[string]*miners.AvalonQHost
	pricesMarketData       *entsoe.PublicationMarketData
	pricesMarketDataExpiry time.Time
	isRunning              bool
	stopChan               chan struct{}
	mu                     sync.RWMutex

	// Weather forecast cache
	weatherCache WeatherForecastCache

	// MPC optimization results
	mpcDecisions          []mpc.ControlDecision
	lastMPCExecutionError error // Tracks if last MPC execution failed

	// Web server
	webServer *WebServer

	// Database connection
	db *sql.DB

	// Logging
	logger *log.Logger

	// Test hooks for dependency injection
	minerDiscoveryFunc func(ctx context.Context, network string) []*miners.AvalonQHost
}

// NewMinerScheduler creates a new scheduler instance
func NewMinerScheduler(config *Config, logger *log.Logger) *MinerScheduler {
	if logger == nil {
		logger = log.Default()
	}

	scheduler := &MinerScheduler{
		config:           config,
		discoveredMiners: make(map[string]*miners.AvalonQHost),
		stopChan:         make(chan struct{}),
		logger:           logger,
		weatherCache: WeatherForecastCache{
			cacheDuration: 2 * time.Hour,
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
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Convert map to slice and return a copy
	minersCopy := make([]*miners.AvalonQHost, 0, len(s.discoveredMiners))
	for _, miner := range s.discoveredMiners {
		minersCopy = append(minersCopy, miner)
	}
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

	// Calculate initial delays
	now := time.Now()
	minersControlInitialDelay := s.getInitialDelay(now, config.CheckPriceInterval) + time.Second
	pvDataInitialDelay := s.getInitialDelay(now, config.PVIntegrationPeriod)
	stateCheckInitialDelay := s.getInitialDelay(now, config.MinersStateCheckInterval)
	mpcExecutionInitialDelay := s.getInitialDelay(now, config.MPCExecutionInterval) + 2*time.Second

	// Create periodic tasks
	tasks := []PeriodicTask{
		{
			name:         "MinerDiscovery",
			initialDelay: 0, // Run immediately
			interval:     config.MinerDiscoveryInterval,
			runFunc: func() {
				s.RunMinerDiscovery(ctx)
			},
		},
		{
			name:         "PriceCheckAndMPC",
			initialDelay: minersControlInitialDelay,
			interval:     config.CheckPriceInterval,
			runFunc: func() {
				s.runPriceCheck(ctx)
				s.RunMPCOptimize(ctx)
			},
		},
		{
			name:         "StateCheck",
			initialDelay: stateCheckInitialDelay,
			interval:     config.MinersStateCheckInterval,
			runFunc: func() {
				s.runStateCheck(ctx)
			},
		},
		{
			name:         "DataPoll",
			initialDelay: pvDataInitialDelay,
			interval:     config.PVPollInterval,
			runFunc: func() {
				s.runDataPoll(dataSamples)
			},
		},
		{
			name:         "DataIntegration",
			initialDelay: pvDataInitialDelay,
			interval:     config.PVIntegrationPeriod,
			runFunc: func() {
				s.runDataIntegration(dataSamples, config.PVPollInterval, dataDB, config.DeviceID, config.DryRun)
			},
		},
		{
			name:         "MPCExecution",
			initialDelay: mpcExecutionInitialDelay,
			interval:     config.MPCExecutionInterval,
			runFunc: func() {
				s.runMPCExecution()
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
func (s *MinerScheduler) GetStatus() SchedulerStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return SchedulerStatus{
		IsRunning:     s.isRunning,
		MinersCount:   len(s.discoveredMiners),
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

// SchedulerStatus represents the current status of the scheduler
type SchedulerStatus struct {
	IsRunning     bool `json:"is_running"`
	MinersCount   int  `json:"miners_count"`
	HasMarketData bool `json:"has_latest_document"`
}
