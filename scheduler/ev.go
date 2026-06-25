package scheduler

import (
	"context"
	"fmt"

	"github.com/devskill-org/ems/sigenergy"
)

// runEVControl is the single owner of all DC-charger EV session logic.
// It runs at PVPollInterval (default 5 s) — fast enough to react within the
// EV protocol handshake window — and is responsible for:
//
//  1. Detecting cable plug-in via DCChargerVehicleVoltage (register 31500).
//     Voltage is at or above the plug-in threshold for the entire session
//     (negotiation, BMS pauses, thermal holds, full battery) whereas output
//     power drops to zero in all those cases and would incorrectly appear as
//     "no EV present".  A 10 V floor filters out the ~1–2 V residual bus
//     charge that persists briefly after physical cable removal.
//
//  2. Maintaining the sticky evSessionActive flag with a 2-reading debounce
//     on session-end to survive transient Modbus glitches.
//
//  3. Applying Mode 2 (Maximum self-consumption) with full battery charge and
//     discharge limits whenever a session is active, so the battery buffers
//     intermittent PV and prevents power gaps from reaching the DC charger.
//
// executeMPCDecision reads evSessionActive as a read-only guard and yields
// control to this task for the duration of the EV session.  No other function
// writes evSessionActive or issues EV-related inverter commands.
func (s *MinerScheduler) runEVControl(ctx context.Context) error {
	config := s.GetConfig()

	if config.PlantModbusAddress == "" {
		return nil
	}

	client, err := sigenergy.NewTCPClient(ctx, config.PlantModbusAddress, sigenergy.PlantAddress)
	if err != nil {
		return fmt.Errorf("EV control: failed to connect to Plant Modbus: %w", err)
	}
	defer client.Close()

	// ReadPlantRunningInfo now returns an error when the DC charger registers
	// cannot be read (fail-safe), so a Modbus failure here causes this task to
	// return an error and be retried on the next tick rather than silently
	// treating the EV as absent.
	info, err := client.ReadPlantRunningInfo(byte(config.DCChargerSlaveID)) //nolint:gosec // SlaveID is expected to be in [0,255] range
	if err != nil {
		return fmt.Errorf("EV control: failed to read plant info: %w", err)
	}

	// Update sticky session state.
	s.mu.Lock()
	if info.EVPluggedIn {
		s.evSessionActive = true
		s.evSessionClearCount = 0
	} else if s.evSessionActive {
		// Require two consecutive zero-voltage readings before declaring the
		// session ended.  This prevents a single transient Modbus glitch from
		// releasing battery control mid-session.
		s.evSessionClearCount++
		if s.evSessionClearCount >= 2 {
			s.evSessionActive = false
			s.evSessionClearCount = 0
			s.logger.Printf("EV control: session ended (2 consecutive zero-voltage readings)")
		} else {
			s.logger.Printf("EV control: zero vehicle voltage detected (pending session-end debounce %d/2)", s.evSessionClearCount)
		}
	}
	evActive := s.evSessionActive
	s.mu.Unlock()

	if !evActive {
		return nil
	}

	// Session is active — apply Mode 2 so the battery supports intermittent PV
	// and prevents power gaps from reaching the DC charger.
	s.logger.Printf("EV control: session active - Mode 2 battery support (VehicleVoltage: %.0f V, ChargePower: %.1f kW, VehicleSOC: %.1f%%)",
		info.DCChargerVehicleVoltage, info.DCChargerOutputPower, info.DCChargerVehicleSOC)

	if config.DryRun {
		s.logger.Printf("EV control: DRY-RUN - would apply Mode 2 (chargeLimit: %.1f kW, dischargeLimit: %.1f kW)",
			config.BatteryMaxCharge, config.BatteryMaxDischarge)
		return nil
	}

	return s.applyInverterMode(client, 2, config.BatteryMaxCharge, config.BatteryMaxDischarge, true, true)
}
