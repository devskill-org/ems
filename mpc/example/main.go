// Package main provides an example of using the MPC (Model Predictive Control) optimization.
package main

import (
	"fmt"
	"math"
	"time"

	"github.com/devskill-org/ems/mpc"
)

// Example usage
func main() {
	// Configure the system
	config := mpc.SystemConfig{
		BatteryCapacity:        10.0, // 10 kWh
		BatteryMaxCharge:       5.0,  // 5 kW
		BatteryMaxDischarge:    5.0,  // 5 kW
		BatteryMinSOC:          0.2,  // 20%
		BatteryMaxSOC:          1.0,  // 100%
		BatteryEfficiency:      0.92, // 92% round-trip
		BatteryDegradationCost: 0.05, // $0.05 per kWh cycled
		MaxGridImport:          10.0, // 10 kW
		MaxGridExport:          10.0, // 10 kW
		TimeSlotDuration:       1.0,  // 1 hour per time slot (use 0.25 for 15-minute intervals)
	}

	// Create MPC controller with 24-hour horizon using hourly time slots
	// Note: In production, this can be configured to use 15-minute intervals (96 slots for 24 hours)
	// or any other interval based on check_price_interval configuration
	mpcController := mpc.NewController(config, 24, 0.5) // 24 hourly slots, start at 50% SOC

	// Create 24-hour forecast with hourly time slots
	// Note: For 15-minute intervals, you would create 96 slots (24 hours * 4 slots/hour)
	// with prices and forecasts for each 15-minute period
	forecast := make([]mpc.TimeSlot, 24)
	for i := range 24 {
		// Example: cheap at night, expensive during day
		importPrice := 0.10
		exportPrice := 0.08
		if i >= 8 && i <= 20 {
			importPrice = 0.25
			exportPrice = 0.20
		}

		// Solar production during day
		solarForecast := 0.0
		if i >= 6 && i <= 18 {
			// Simple sine curve for solar
			solarPeak := 5.0 // 5 kW peak
			angle := float64(i-6) / 12.0 * math.Pi
			solarForecast = solarPeak * math.Sin(angle)
		}

		// Load varies throughout day
		loadForecast := 1.5 // Base load
		if i >= 7 && i <= 22 {
			loadForecast = 2.5 // Higher during active hours
		}

		futureTime := time.Now().Add(time.Duration(i) * time.Hour)
		forecast[i] = mpc.TimeSlot{
			Hour:          i,
			Timestamp:     futureTime.Unix(),
			ImportPrice:   importPrice,
			ExportPrice:   exportPrice,
			SolarForecast: solarForecast,
			LoadForecast:  loadForecast,
		}
	}

	// Run optimization
	fmt.Println("Solar Inverter MPC Optimization (Hourly Example)")
	fmt.Println("=================================================")
	fmt.Printf("Initial SOC: %.1f%%\n", mpcController.CurrentSOC*100)
	fmt.Printf("Time slots: %d (hourly intervals)\n\n", len(forecast))

	startTime := time.Now()
	decisions := mpcController.Optimize(forecast)
	duration := time.Since(startTime)

	fmt.Printf("Optimization completed in %v\n\n", duration)

	// Display results
	totalProfit := 0.0
	fmt.Println("Hour | Solar | Load  | Import$ | Export$ | Batt→ | Grid→ | SOC   | Profit")
	fmt.Println("-----|-------|-------|---------|---------|-------|-------|-------|--------")

	for _, dec := range decisions {
		slot := forecast[dec.Hour]
		totalProfit += dec.Profit

		charge := dec.BatteryChargeFromPV + dec.BatteryChargeFromGrid
		battAction := "idle"
		if charge > 0.1 {
			battAction = fmt.Sprintf("+%.1fkW", charge)
		} else if dec.BatteryDischarge > 0.1 {
			battAction = fmt.Sprintf("-%.1fkW", dec.BatteryDischarge)
		}

		gridAction := "idle"
		if dec.GridImport > 0.1 {
			gridAction = fmt.Sprintf("←%.1fkW", dec.GridImport)
		} else if dec.GridExport > 0.1 {
			gridAction = fmt.Sprintf("→%.1fkW", dec.GridExport)
		}

		fmt.Printf("%4d | %5.1f | %5.1f | $%.3f  | $%.3f  | %-6s| %-6s| %4.0f%% | $%.3f\n",
			dec.Hour,
			slot.SolarForecast,
			slot.LoadForecast,
			slot.ImportPrice,
			slot.ExportPrice,
			battAction,
			gridAction,
			dec.BatterySOC*100,
			dec.Profit,
		)
	}

	fmt.Printf("\nTotal Profit (24h): $%.2f\n", totalProfit)
	fmt.Printf("Final SOC: %.1f%%\n", decisions[len(decisions)-1].BatterySOC*100)

	// Show strategy summary
	fmt.Println("\nStrategy Summary:")
	fmt.Println("- Charges battery from grid during cheap night hours")
	fmt.Println("- Charges battery from solar during day")
	fmt.Println("- Discharges battery during expensive peak hours")
	fmt.Println("- Exports excess solar when prices are favorable")
	fmt.Println("\nNote: This example uses hourly time slots.")
	fmt.Println("For 15-minute intervals (as configured in production with check_price_interval=15m),")
	fmt.Println("create 96 time slots with specific prices for each 15-minute period.")
}
