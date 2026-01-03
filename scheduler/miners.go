package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/devskill-org/ems/miners"
)

// discoverMiners discovers Avalon miners on the network and stores them
func (s *MinerScheduler) discoverMiners(ctx context.Context) error {
	s.logger.Printf("Discovering miners on network: %s", s.config.Network)

	// Use injected discovery function for testing, otherwise use default
	var newlyDiscoveredMiners []*miners.AvalonQHost
	if s.minerDiscoveryFunc != nil {
		newlyDiscoveredMiners = s.minerDiscoveryFunc(ctx, s.config.Network)
	} else {
		newlyDiscoveredMiners = miners.Discover(ctx, s.config.Network)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Add only new miners that don't already exist
	newMinersCount := 0
	for _, newMiner := range newlyDiscoveredMiners {
		key := fmt.Sprintf("%s:%d", newMiner.Address, newMiner.Port)
		if _, exists := s.discoveredMiners[key]; !exists {
			s.discoveredMiners[key] = newMiner
			newMinersCount++
			s.logger.Printf("  New miner discovered: %s:%d", newMiner.Address, newMiner.Port)
		}
	}

	totalMiners := len(s.discoveredMiners)
	s.logger.Printf("Discovery complete: %d total miners (%d newly discovered)", totalMiners, newMinersCount)

	return nil
}

// RunMinerDiscovery runs the miner discovery process as a scheduled task
func (s *MinerScheduler) RunMinerDiscovery(ctx context.Context) {
	s.logger.Printf("Starting miner discovery task at %s", time.Now().Format(time.RFC3339))

	if err := s.discoverMiners(ctx); err != nil {
		s.logger.Printf("Error discovering miners: %v", err)
		return
	}

	s.logger.Printf("Miner discovery task completed successfully")
}

// getMinerPowerConsumption returns the power consumption in kW for a given miner state and work mode
func (s *MinerScheduler) getMinerPowerConsumption(state miners.AvalonState, workMode miners.AvalonWorkMode) float64 {
	if state == miners.AvalonStateStandBy {
		return s.config.MinerPowerStandby
	}

	switch workMode {
	case miners.AvalonEcoMode:
		return s.config.MinerPowerEco
	case miners.AvalonStandardMode:
		return s.config.MinerPowerStandard
	case miners.AvalonSuperMode:
		return s.config.MinerPowerSuper
	default:
		return s.config.MinerPowerStandby
	}
}

// calculateTotalPowerConsumption calculates total power consumption of all miners in kW
func (s *MinerScheduler) calculateTotalPowerConsumption(minersList []*miners.AvalonQHost) float64 {
	var totalPower float64
	for _, miner := range minersList {
		if miner.LastStatsError == nil && miner.LastStats != nil {
			power := s.getMinerPowerConsumption(miner.LastStats.State, miner.LastStats.WorkMode)
			totalPower += power
		}
	}
	return totalPower
}

// refreshMinersState refreshes the state of all discovered miners and returns miners list
func (s *MinerScheduler) refreshMinersState(ctx context.Context) []*miners.AvalonQHost {
	var wg sync.WaitGroup
	minersList := s.GetDiscoveredMiners()
	for _, miner := range minersList {
		wg.Add(1)
		go func(m *miners.AvalonQHost) {
			defer wg.Done()

			// Get current stats
			m.RefreshLiteStats(ctx)
		}(miner)
	}
	wg.Wait()
	return minersList
}

func (s *MinerScheduler) getEffecivePowerLimit() float64 {
	info := s.GetPlantRunningInfo()
	availablePower := 0.0
	if info != nil {
		availablePower = info.PhotovoltaicPower // in kW
	}
	powerLimit := s.config.MinersPowerLimit // in kW
	s.logger.Printf("PV Power Control: Available PV power: %.2f kW, Miners power limit: %.2f kW", availablePower, powerLimit)

	// Use the minimum of available PV power and configured power limit
	effectiveLimit := powerLimit
	if availablePower < powerLimit {
		effectiveLimit = availablePower
	}
	return effectiveLimit
}

// manageMiners manages miner states based on current price vs price limit and power consumption
func (s *MinerScheduler) manageMiners(ctx context.Context, currentPrice float64) error {
	priceLimit := s.config.PriceLimit
	minersList := s.refreshMinersState(ctx)

	if len(minersList) == 0 {
		s.logger.Printf("No miners to manage")
		return nil
	}

	isDryRun := s.config.DryRun
	if isDryRun {
		s.logger.Printf("DRY-RUN MODE: Actions will be simulated only")
	}

	// Check if PV power control is enabled
	usePowerControl := s.config.UsePVPowerControl
	var effectiveLimit float64
	var totalPower float64

	if usePowerControl {
		effectiveLimit = s.getEffecivePowerLimit()
		totalPower = s.calculateTotalPowerConsumption(minersList)
		s.logger.Printf("Current total power consumption: %.2f kW, Effective limit: %.2f kW", totalPower, effectiveLimit)
	}

	// Standard price-based control
	var wg sync.WaitGroup
	var powerMu sync.Mutex // Mutex to protect totalPower updates
	errChan := make(chan error, len(minersList))

	for _, miner := range minersList {
		wg.Add(1)
		go func(m *miners.AvalonQHost) {
			defer wg.Done()

			// Get current stats
			if m.LastStatsError != nil {
				errChan <- m.LastStatsError
				return
			}

			currentState := m.LastStats.State
			s.logger.Printf("Miner %s:%d current state: %s", m.Address, m.Port, currentState.String())

			// Decision logic based on price comparison
			if currentPrice <= priceLimit {
				// Price is low enough - wake up miners (if power allows)
				if currentState == miners.AvalonStateStandBy {
					// Check if we have power budget for waking up this miner
					if usePowerControl {
						additionalPower := s.config.MinerPowerEco // Wake up in Eco mode

						// Lock to safely check and update totalPower
						powerMu.Lock()
						if totalPower+additionalPower > effectiveLimit {
							s.logger.Printf("Miner %s:%d cannot wake up: would exceed power limit (%.2f + %.2f > %.2f kW)",
								m.Address, m.Port, totalPower, additionalPower, effectiveLimit)
							powerMu.Unlock()
							return
						}
						// Reserve power for this miner
						totalPower += additionalPower
						powerMu.Unlock()
					}

					if isDryRun {
						s.logger.Printf("DRY-RUN: Would wake up miner %s:%d (price %.2f <= limit %.2f)",
							m.Address, m.Port, currentPrice, priceLimit)
						return
					}
					s.logger.Printf("Price (%.2f) <= limit (%.2f), waking up miner %s:%d",
						currentPrice, priceLimit, m.Address, m.Port)

					response, err := m.WakeUp(ctx)
					if err != nil {
						errChan <- fmt.Errorf("failed to wake up miner %s:%d: %w", m.Address, m.Port, err)
						return
					}
					s.logger.Printf("WakeUp response for miner %s:%d: %s", m.Address, m.Port, response)
					// Reserve power for this miner
					if usePowerControl {
						powerMu.Lock()
						totalPower += s.config.MinerPowerEco
						powerMu.Unlock()
					}
				} else {
					s.logger.Printf("Miner %s:%d is already in %s state, no action needed",
						m.Address, m.Port, currentState.String())
				}
			} else {
				// Price is too high - put active miners into standby
				if currentState != miners.AvalonStateStandBy {
					if isDryRun {
						s.logger.Printf("DRY-RUN: Would put miner %s:%d into standby (price %.2f > limit %.2f)",
							m.Address, m.Port, currentPrice, priceLimit)
					} else {
						s.logger.Printf("Price (%.2f) > limit (%.2f), putting miner %s:%d into standby",
							currentPrice, priceLimit, m.Address, m.Port)

						response, err := m.Standby(ctx)
						if err != nil {
							errChan <- fmt.Errorf("failed to put miner %s:%d into standby: %w", m.Address, m.Port, err)
							return
						}

						// Update totalPower after successful standby
						if usePowerControl {
							powerMu.Lock()
							releasedPower := s.getMinerPowerConsumption(currentState, m.LastStats.WorkMode)
							totalPower -= releasedPower
							totalPower += s.config.MinerPowerStandby
							powerMu.Unlock()
						}

						s.logger.Printf("Standby response for miner %s:%d: %s", m.Address, m.Port, response)
					}
				} else {
					s.logger.Printf("Miner %s:%d is already in standby, no action needed",
						m.Address, m.Port)
				}
			}
		}(miner)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errChan)

	// Collect any errors
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		s.logger.Printf("Encountered %d errors while managing miners:", len(errors))
		for _, err := range errors {
			s.logger.Printf("  - %v", err)
		}
		return fmt.Errorf("encountered %d errors while managing miners", len(errors))
	}

	if isDryRun {
		s.logger.Printf("DRY-RUN: Successfully simulated management of %d miners", len(minersList))
	} else {
		s.logger.Printf("Successfully managed %d miners", len(minersList))
	}
	return nil
}

// controlMiner returns a new miner state and mode
func (s *MinerScheduler) controlMiner(m *miners.AvalonQHost, totalPower float64, effectiveLimit float64) (miners.AvalonState, miners.AvalonWorkMode) {
	fanR := m.LastStats.FanR
	currentWorkMode := miners.AvalonWorkMode(m.LastStats.WorkMode)
	currentState := m.LastStats.State
	if fanR > s.config.FanRHighThreshold || totalPower > effectiveLimit {
		// Decrease work mode
		newWorkMode := currentWorkMode - 1
		newTotalPower := totalPower - s.getMinerPowerConsumption(currentState, currentWorkMode) + s.getMinerPowerConsumption(currentState, newWorkMode)
		if newWorkMode < 0 || newTotalPower > effectiveLimit {
			return miners.AvalonStateStandBy, miners.AvalonEcoMode
		}
		return currentState, newWorkMode
	} else if fanR < s.config.FanRLowThreshold && totalPower <= effectiveLimit {
		// Increase work mode only if all LiteStatsHistory fanR values match criteria
		if len(m.LiteStatsHistory) < 5 || currentWorkMode == miners.AvalonSuperMode {
			return currentState, currentWorkMode
		}
		for _, stat := range m.LiteStatsHistory {
			if stat.FanR >= s.config.FanRLowThreshold {
				return currentState, currentWorkMode
			}
		}
		newWorkMode := currentWorkMode + 1
		newTotalPower := totalPower - s.getMinerPowerConsumption(currentState, currentWorkMode) + s.getMinerPowerConsumption(currentState, newWorkMode)
		if newTotalPower <= effectiveLimit {
			return currentState, newWorkMode
		}
	}
	return currentState, currentWorkMode
}

// runStateCheck executes the state monitoring task for miners
func (s *MinerScheduler) runStateCheck(ctx context.Context) {
	minersList := s.refreshMinersState(ctx)
	if len(minersList) == 0 {
		return
	}

	isDryRun := s.config.DryRun

	// Check if PV power control is enabled
	usePowerControl := s.config.UsePVPowerControl
	var effectiveLimit float64
	var totalPower float64

	if usePowerControl {
		effectiveLimit = s.getEffecivePowerLimit()
		totalPower = s.calculateTotalPowerConsumption(minersList)
		s.logger.Printf("Current total power consumption: %.2f kW, Effective limit: %.2f kW", totalPower, effectiveLimit)
	}

	var wg sync.WaitGroup
	var powerMu sync.Mutex // Mutex to protect totalPower updates
	errChan := make(chan error, len(minersList))

	for _, miner := range minersList {
		wg.Add(1)
		go func(m *miners.AvalonQHost) {
			defer wg.Done()

			// Get current stats
			if m.LastStatsError != nil {
				errChan <- m.LastStatsError
				return
			}

			fanR := m.LastStats.FanR
			currentWorkMode := miners.AvalonWorkMode(m.LastStats.WorkMode)
			currentState := m.LastStats.State
			hbiTemp := m.LastStats.HBITemp
			hboTemp := m.LastStats.HBOTemp
			iTemp := m.LastStats.ITemp

			if currentState != miners.AvalonStateMining {
				return
			}

			s.logger.Printf("Miner %s:%d - FanR: %d%%, HBITemp:%d, HBOTemp:%d, ITemp:%d, WorkMode: %d",
				m.Address,
				m.Port,
				fanR,
				hbiTemp,
				hboTemp,
				iTemp,
				currentWorkMode)

			powerMu.Lock()
			newState, newMode := s.controlMiner(m, totalPower, effectiveLimit)
			powerMu.Unlock()
			if newState == currentState && newMode == currentWorkMode {
				return
			}
			if isDryRun {
				s.logger.Printf("DRY-RUN: Would set miner %s:%d to set %s state and %d mode (FanR %d%%)",
					m.Address, m.Port, newState.String(), newMode, fanR)
			} else {
				var response string
				var err error
				if newState != currentState {
					if newState == miners.AvalonStateMining {
						response, err = m.WakeUp(ctx)
					}
					if newState == miners.AvalonStateStandBy {
						response, err = m.Standby(ctx)
					}
				}
				if newMode != currentWorkMode {
					response, err = m.SetWorkMode(ctx, newMode, newMode > currentWorkMode)
				}
				s.logger.Printf("Control miner %s:%d to set %s state and %d mode (FanR %d%%)",
					m.Address, m.Port, newState.String(), newMode, fanR)
				if err != nil {
					errChan <- fmt.Errorf("failed to control miner %s:%d: %w", m.Address, m.Port, err)
					return
				}
				powerMu.Lock()
				totalPower += s.getMinerPowerConsumption(newState, newMode) - s.getMinerPowerConsumption(currentState, currentWorkMode)
				s.logger.Printf("Current total power consumption: %.2f kW, Effective limit: %.2f kW", totalPower, effectiveLimit)
				powerMu.Unlock()
				s.logger.Printf("Control response for miner %s:%d: %s", m.Address, m.Port, response)

			}
		}(miner)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(errChan)

	// Collect any errors
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		s.logger.Printf("Encountered %d errors during state check:", len(errors))
		for _, err := range errors {
			s.logger.Printf("  - %v", err)
		}
	}
}
