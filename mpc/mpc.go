package mpc

import (
	"math"
)

// SystemConfig holds the inverter system configuration
type SystemConfig struct {
	BatteryCapacity        float64 // kWh
	BatteryMaxCharge       float64 // kW
	BatteryMaxDischarge    float64 // kW
	BatteryMinSOC          float64 // percentage (0-1)
	BatteryMaxSOC          float64 // percentage (0-1)
	BatteryEfficiency      float64 // round-trip efficiency (0-1)
	BatteryDegradationCost float64 // $/kWh cycled
	MaxGridImport          float64 // kW
	MaxGridExport          float64 // kW
}

// TimeSlot represents one hour of operation
type TimeSlot struct {
	Hour          int
	Timestamp     int64   // Unix timestamp when this time slot begins
	ImportPrice   float64 // $/kWh
	ExportPrice   float64 // $/kWh
	SolarForecast float64 // kW average for the hour
	LoadForecast  float64 // kW average for the hour
	CloudCoverage float64 // % cloud coverage (0-100)
	WeatherSymbol string  // weather condition symbol
}

// ControlDecision represents the optimal control for one time slot
type ControlDecision struct {
	Hour             int
	Timestamp        int64   // Unix timestamp when this time slot begins
	BatteryCharge    float64 // kW (positive = charging)
	BatteryDischarge float64 // kW (positive = discharging)
	GridImport       float64 // kW (positive = importing)
	GridExport       float64 // kW (positive = exporting)
	BatterySOC       float64 // percentage (0-1)
	Profit           float64 // $ for this hour
	// Forecast data used for this decision
	ImportPrice   float64 // $/kWh
	ExportPrice   float64 // $/kWh
	SolarForecast float64 // kW average for the hour
	LoadForecast  float64 // kW average for the hour
	CloudCoverage float64 // % cloud coverage (0-100)
	WeatherSymbol string  // weather condition symbol
}

// MPCController implements Model Predictive Control
type MPCController struct {
	Config     SystemConfig
	Horizon    int // hours to look ahead
	CurrentSOC float64
}

// NewMPCController creates a new MPC controller
func NewMPCController(config SystemConfig, horizon int, initialSOC float64) *MPCController {
	return &MPCController{
		Config:     config,
		Horizon:    horizon,
		CurrentSOC: initialSOC,
	}
}

// Optimize finds the optimal control strategy using dynamic programming
func (mpc *MPCController) Optimize(forecast []TimeSlot) []ControlDecision {
	if len(forecast) == 0 {
		return nil
	}

	// Use dynamic programming for optimization
	// State: SOC level, Time: hour
	// We'll discretize SOC into steps for tractability
	socSteps := 200
	socStep := (mpc.Config.BatteryMaxSOC - mpc.Config.BatteryMinSOC) / float64(socSteps)

	// DP table: [time][soc_index] -> (best_profit, best_decision)
	type dpState struct {
		profit   float64
		decision ControlDecision
		prevSOC  int
	}

	dp := make([][]dpState, len(forecast)+1)
	for i := range dp {
		dp[i] = make([]dpState, socSteps+1)
		for j := range dp[i] {
			dp[i][j].profit = math.Inf(-1)
		}
	}

	// Initialize with current SOC
	startSOCIndex := mpc.socToIndex(mpc.CurrentSOC, socStep)
	dp[0][startSOCIndex].profit = 0

	// Forward pass - build DP table
	for t := range forecast {
		slot := forecast[t]

		for socIdx := 0; socIdx <= socSteps; socIdx++ {
			if math.IsInf(dp[t][socIdx].profit, -1) {
				continue
			}

			currentSOC := mpc.indexToSOC(socIdx, socStep)

			// Try different control decisions
			decisions := mpc.generateFeasibleDecisions(currentSOC, slot)

			for _, dec := range decisions {
				newSOC := mpc.calculateNewSOC(currentSOC, dec.BatteryCharge, dec.BatteryDischarge)
				newSOCIdx := mpc.socToIndex(newSOC, socStep)

				if newSOCIdx < 0 || newSOCIdx > socSteps {
					continue
				}

				profit := mpc.calculateProfit(dec, slot)
				totalProfit := dp[t][socIdx].profit + profit

				if totalProfit > dp[t+1][newSOCIdx].profit {
					dp[t+1][newSOCIdx].profit = totalProfit
					dp[t+1][newSOCIdx].decision = dec
					dp[t+1][newSOCIdx].decision.BatterySOC = newSOC
					dp[t+1][newSOCIdx].decision.Profit = profit
					dp[t+1][newSOCIdx].decision.Timestamp = slot.Timestamp
					dp[t+1][newSOCIdx].decision.ImportPrice = slot.ImportPrice
					dp[t+1][newSOCIdx].decision.ExportPrice = slot.ExportPrice
					dp[t+1][newSOCIdx].decision.SolarForecast = slot.SolarForecast
					dp[t+1][newSOCIdx].decision.LoadForecast = slot.LoadForecast
					dp[t+1][newSOCIdx].decision.CloudCoverage = slot.CloudCoverage
					dp[t+1][newSOCIdx].decision.WeatherSymbol = slot.WeatherSymbol
					dp[t+1][newSOCIdx].prevSOC = socIdx
				}
			}
		}
	}

	// Backward pass - reconstruct optimal path
	bestFinalSOC := 0
	bestFinalProfit := math.Inf(-1)
	for socIdx := 0; socIdx <= socSteps; socIdx++ {
		if dp[len(forecast)][socIdx].profit > bestFinalProfit {
			bestFinalProfit = dp[len(forecast)][socIdx].profit
			bestFinalSOC = socIdx
		}
	}

	// Trace back the path
	path := make([]ControlDecision, len(forecast))
	currentIdx := bestFinalSOC
	for t := len(forecast) - 1; t >= 0; t-- {
		path[t] = dp[t+1][currentIdx].decision
		currentIdx = dp[t+1][currentIdx].prevSOC
	}

	return path
}

// generateFeasibleDecisions creates a set of feasible control decisions
func (mpc *MPCController) generateFeasibleDecisions(currentSOC float64, slot TimeSlot) []ControlDecision {
	decisions := []ControlDecision{}

	// Discretize battery actions (charge, discharge, idle)
	batteryActions := []struct {
		charge    float64
		discharge float64
	}{
		{0, 0}, // Idle
	}

	// Add charge options (more granular for better arbitrage opportunities)
	for i := 1; i <= 10; i++ {
		charge := float64(i) * mpc.Config.BatteryMaxCharge / 10.0
		if mpc.canCharge(currentSOC, charge) {
			batteryActions = append(batteryActions, struct {
				charge    float64
				discharge float64
			}{charge, 0})
		}
	}

	// Add discharge options (more granular for better arbitrage opportunities)
	for i := 1; i <= 10; i++ {
		discharge := float64(i) * mpc.Config.BatteryMaxDischarge / 10.0
		if mpc.canDischarge(currentSOC, discharge) {
			batteryActions = append(batteryActions, struct {
				charge    float64
				discharge float64
			}{0, discharge})
		}
	}

	// For each battery action, calculate power balance
	for _, action := range batteryActions {
		dec := ControlDecision{
			Hour:             slot.Hour,
			Timestamp:        slot.Timestamp,
			BatteryCharge:    action.charge,
			BatteryDischarge: action.discharge,
		}

		// Power balance: Solar + GridImport + BatteryDischarge = Load + GridExport + BatteryCharge
		netSolar := slot.SolarForecast
		netLoad := slot.LoadForecast + action.charge/mpc.Config.BatteryEfficiency
		netSupply := netSolar + action.discharge*mpc.Config.BatteryEfficiency

		balance := netSupply - netLoad

		if balance > 0 {
			// Excess power - can export
			dec.GridExport = math.Min(balance, mpc.Config.MaxGridExport)
			dec.GridImport = 0
		} else {
			// Deficit - need to import
			dec.GridImport = math.Min(-balance, mpc.Config.MaxGridImport)
			dec.GridExport = 0
		}

		// Check if decision is feasible
		if mpc.isFeasible(dec) {
			decisions = append(decisions, dec)
		}
	}

	return decisions
}

// calculateProfit computes the profit for a decision
// This accounts for:
// 1. Charging battery when prices are low (cost reflected in GridImport)
// 2. Discharging battery to support load when prices are high (savings reflected in reduced GridImport)
// 3. Exporting excess power when export prices are favorable
func (mpc *MPCController) calculateProfit(dec ControlDecision, slot TimeSlot) float64 {
	// Revenue from exporting to grid
	revenue := dec.GridExport * slot.ExportPrice

	// Cost of importing from grid (includes charging battery from grid when prices are low)
	importCost := dec.GridImport * slot.ImportPrice

	// Calculate the value of battery operations for arbitrage
	// When we discharge battery to support load, we avoid importing at current price
	// When we charge battery from grid, we pay current price but can use it later at higher prices

	// Effective energy delivered from battery (accounting for efficiency)
	batteryDischargeEnergy := dec.BatteryDischarge * mpc.Config.BatteryEfficiency

	// Calculate how much battery energy went to LOAD vs EXPORT
	// From power balance: Solar + BatteryDischarge*eff = Load + BatteryCharge/eff + GridExport - GridImport
	// Battery discharge that supports load (avoiding imports):
	netLoad := slot.LoadForecast + dec.BatteryCharge/mpc.Config.BatteryEfficiency
	netSupplyBeforeBattery := slot.SolarForecast
	loadDeficit := math.Max(0, netLoad-netSupplyBeforeBattery)
	batteryDischargeToLoad := math.Min(batteryDischargeEnergy, loadDeficit)

	// The discharge value: we avoid importing power at the current price for the energy used to support load
	// Energy exported is already valued in the revenue calculation, so we exclude it here
	dischargeValue := batteryDischargeToLoad * slot.ImportPrice

	// The charge cost: we store energy for future use
	// This is beneficial when import price is low (to use when prices are high)
	// The cost is already included in importCost, but we account for storage losses
	chargeLoss := dec.BatteryCharge * (1 - mpc.Config.BatteryEfficiency) * slot.ImportPrice

	// Battery degradation cost (wear and tear from cycling)
	batteryThroughput := dec.BatteryCharge + dec.BatteryDischarge
	degradationCost := batteryThroughput * mpc.Config.BatteryDegradationCost

	// Net profit calculation:
	// + Revenue from exports (includes any battery energy exported)
	// - Cost of imports (includes grid charging of battery)
	// + Value from discharging battery to support load (avoids expensive imports)
	// - Cost of charging losses
	// - Battery degradation
	profit := revenue - importCost + dischargeValue - chargeLoss - degradationCost

	return profit
}

// Helper functions
func (mpc *MPCController) canCharge(soc, charge float64) bool {
	newSOC := soc + (charge / mpc.Config.BatteryCapacity)
	return newSOC <= mpc.Config.BatteryMaxSOC
}

func (mpc *MPCController) canDischarge(soc, discharge float64) bool {
	newSOC := soc - (discharge / mpc.Config.BatteryCapacity)
	return newSOC >= mpc.Config.BatteryMinSOC
}

func (mpc *MPCController) calculateNewSOC(currentSOC, charge, discharge float64) float64 {
	chargeEnergy := charge * mpc.Config.BatteryEfficiency
	socChange := (chargeEnergy - discharge) / mpc.Config.BatteryCapacity
	newSOC := currentSOC + socChange
	return math.Max(mpc.Config.BatteryMinSOC, math.Min(mpc.Config.BatteryMaxSOC, newSOC))
}

func (mpc *MPCController) socToIndex(soc float64, socStep float64) int {
	return int(math.Round((soc - mpc.Config.BatteryMinSOC) / socStep))
}

func (mpc *MPCController) indexToSOC(index int, socStep float64) float64 {
	return mpc.Config.BatteryMinSOC + float64(index)*socStep
}

func (mpc *MPCController) isFeasible(dec ControlDecision) bool {
	// Check all constraints are satisfied
	if dec.BatteryCharge > mpc.Config.BatteryMaxCharge {
		return false
	}
	if dec.BatteryDischarge > mpc.Config.BatteryMaxDischarge {
		return false
	}
	if dec.GridImport > mpc.Config.MaxGridImport {
		return false
	}
	if dec.GridExport > mpc.Config.MaxGridExport {
		return false
	}
	return true
}

// ExecuteControl applies the first decision and returns it
func (mpc *MPCController) ExecuteControl(forecast []TimeSlot) *ControlDecision {
	decisions := mpc.Optimize(forecast)
	if len(decisions) == 0 {
		return nil
	}

	// Execute first decision
	decision := decisions[0]
	mpc.CurrentSOC = decision.BatterySOC

	return &decision
}
