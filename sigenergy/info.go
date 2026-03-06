// Package sigenergy provides Modbus client functionality for Sigenergy inverter systems.
package sigenergy

import (
	"context"
	"fmt"
	"time"
)

// ShowPlantInfo displays detailed information about the plant in a formatted table
func ShowPlantInfo(ctx context.Context, plantModbusAddress string) error {
	if plantModbusAddress == "" {
		return fmt.Errorf("PlantModbusAddress is not configured")
	}

	// Create TCP modbus client (PlantModbusAddress already includes port)
	client, err := NewTCPClient(ctx, plantModbusAddress, PlantAddress)
	if err != nil {
		return fmt.Errorf("error connecting to plant modbus server at %s: %w", plantModbusAddress, err)
	}
	defer client.Close()

	// Read plant running info
	info, err := client.ReadPlantRunningInfo()
	if err != nil {
		return fmt.Errorf("error reading plant information: %w", err)
	}

	// Display plant information
	fmt.Println()
	fmt.Println("======================== PLANT RUNNING INFORMATION ========================")
	fmt.Println()

	// System Information
	systemTime := time.Unix(int64(info.SystemTime), 0)
	fmt.Println("SYSTEM INFORMATION")
	fmt.Println("--------------------------------------------------")
	fmt.Printf("  System Time:                    %s\n", systemTime.Format("2006-01-02 15:04:05"))
	fmt.Printf("  System Timezone:                %d minutes\n", info.SystemTimeZone)
	fmt.Printf("  EMS Work Mode:                  %s\n", getEMSWorkMode(info.EMSWorkMode))
	fmt.Printf("  On/Off Grid Status:             %s\n", getOnOffGridStatus(info.OnOffGridStatus))
	fmt.Printf("  Plant Running State:            %d\n", info.PlantRunningState)
	fmt.Println()

	// Grid Sensor Information
	fmt.Println("GRID SENSOR")
	fmt.Println("--------------------------------------------------")
	fmt.Printf("  Grid Sensor Status:             %s\n", getGridSensorStatus(info.GridSensorStatus))
	fmt.Printf("  Grid Sensor Active Power:       %.3f kW\n", info.GridSensorActivePower)
	fmt.Printf("  Grid Sensor Reactive Power:     %.3f kVar\n", info.GridSensorReactivePower)
	fmt.Println()

	// Plant Power Information
	fmt.Println("PLANT POWER")
	fmt.Println("--------------------------------------------------")
	fmt.Printf("  Plant Active Power:             %.3f kW\n", info.PlantActivePower)
	fmt.Printf("  Plant Reactive Power:           %.3f kVar\n", info.PlantReactivePower)
	fmt.Printf("  Phase A Active Power:           %.3f kW\n", info.PlantPhaseAActivePower)
	fmt.Printf("  Phase B Active Power:           %.3f kW\n", info.PlantPhaseBActivePower)
	fmt.Printf("  Phase C Active Power:           %.3f kW\n", info.PlantPhaseCActivePower)
	fmt.Printf("  Phase A Reactive Power:         %.3f kVar\n", info.PlantPhaseAReactivePower)
	fmt.Printf("  Phase B Reactive Power:         %.3f kVar\n", info.PlantPhaseBReactivePower)
	fmt.Printf("  Phase C Reactive Power:         %.3f kVar\n", info.PlantPhaseCReactivePower)
	fmt.Println()

	// Power Generation
	// Load power = PV power + grid import - ESS charging
	// GridSensorActivePower: >0 buying from grid, <0 selling to grid
	// ESSPower: >0 charging, <0 discharging
	loadPower := info.PhotovoltaicPower + info.GridSensorActivePower - info.ESSPower

	fmt.Println("POWER GENERATION")
	fmt.Println("--------------------------------------------------")
	fmt.Printf("  Photovoltaic Power:             %.3f kW\n", info.PhotovoltaicPower)
	fmt.Printf("  ESS Power:                      %.3f kW %s\n", info.ESSPower, getESSPowerStatus(info.ESSPower))
	fmt.Printf("  Load Power:                     %.3f kW\n", loadPower)
	fmt.Println()

	// ESS Information
	fmt.Println("ENERGY STORAGE SYSTEM (ESS)")
	fmt.Println("--------------------------------------------------")
	fmt.Printf("  ESS SOC:                        %.1f %%\n", info.ESSSOC)
	fmt.Printf("  ESS SOH:                        %.1f %%\n", info.ESSSOH)
	fmt.Printf("  ESS Average Cell Temperature:   %.1f °C\n", info.ESSAvgCellTemperature)
	fmt.Printf("  ESS Rated Energy Capacity:      %.2f kWh\n", info.ESSRatedEnergyCapacity)
	fmt.Printf("  ESS Charge Off SOC:             %.1f %%\n", info.ESSChargeOffSOC)
	fmt.Printf("  ESS Discharge Off SOC:          %.1f %%\n", info.ESSDischargeOffSOC)
	fmt.Printf("  ESS Max Charging Power:         %.3f kW\n", info.ESSAvailableMaxChargingPower)
	fmt.Printf("  ESS Max Discharging Power:      %.3f kW\n", info.ESSAvailableMaxDischargingPower)
	fmt.Println()

	// Power Limits
	fmt.Println("POWER LIMITS")
	fmt.Println("--------------------------------------------------")
	fmt.Printf("  Max Active Power:               %.3f kW\n", info.MaxActivePower)
	fmt.Printf("  Max Apparent Power:             %.3f kVA\n", info.MaxApparentPower)
	fmt.Printf("  Available Max Active Power:     %.3f kW\n", info.AvailableMaxActivePower)
	fmt.Printf("  Available Min Active Power:     %.3f kW\n", info.AvailableMinActivePower)
	fmt.Printf("  Available Max Reactive Power:   %.3f kVar\n", info.AvailableMaxReactivePower)
	fmt.Printf("  Available Min Reactive Power:   %.3f kVar\n", info.AvailableMinReactivePower)
	fmt.Println()

	// DC Charger Information
	fmt.Println("DC CHARGER")
	fmt.Println("--------------------------------------------------")
	fmt.Printf("  Output Power:                   %.3f kW\n", info.DCChargerOutputPower)
	fmt.Printf("  Vehicle SOC:                    %.1f %%\n", info.DCChargerVehicleSOC)
	fmt.Println()

	// Alarms
	if info.GeneralAlarm1 != 0 || info.GeneralAlarm2 != 0 || info.GeneralAlarm3 != 0 || info.GeneralAlarm4 != 0 {
		fmt.Println("ALARMS")
		fmt.Println("--------------------------------------------------")
		fmt.Printf("  General Alarm 1:                0x%04X\n", info.GeneralAlarm1)
		fmt.Printf("  General Alarm 2:                0x%04X\n", info.GeneralAlarm2)
		fmt.Printf("  General Alarm 3:                0x%04X\n", info.GeneralAlarm3)
		fmt.Printf("  General Alarm 4:                0x%04X\n", info.GeneralAlarm4)
		fmt.Println()
	}

	fmt.Println("===========================================================================")
	fmt.Println()

	return nil
}

func getEMSWorkMode(mode uint16) string {
	switch mode {
	case 0:
		return "Max Self Consumption"
	case 1:
		return "AI Mode"
	case 2:
		return "TOU (Time of Use)"
	case 7:
		return "Remote EMS"
	default:
		return fmt.Sprintf("Unknown (%d)", mode)
	}
}

func getGridSensorStatus(status uint16) string {
	switch status {
	case 0:
		return "Not Connected"
	case 1:
		return "Connected"
	default:
		return fmt.Sprintf("Unknown (%d)", status)
	}
}

func getOnOffGridStatus(status uint16) string {
	switch status {
	case 0:
		return "On Grid"
	case 1:
		return "Off Grid (Auto)"
	case 2:
		return "Off Grid (Manual)"
	default:
		return fmt.Sprintf("Unknown (%d)", status)
	}
}

func getESSPowerStatus(power float64) string {
	if power < -0.01 {
		return "(Discharging)"
	} else if power > 0.01 {
		return "(Charging)"
	}
	return "(Idle)"
}
