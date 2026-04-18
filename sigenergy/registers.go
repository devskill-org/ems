package sigenergy

import "fmt"

// PlantRunningInfo represents the plant running information (Section 5.1)
type PlantRunningInfo struct {
	SystemTime                      uint32  // Epoch seconds
	SystemTimeZone                  int16   // minutes
	EMSWorkMode                     uint16  // 0: Max self consumption, 1: AI Mode, 2: TOU, 7: Remote EMS
	GridSensorStatus                uint16  // 0: not connected, 1: connected
	GridSensorActivePower           float64 // kW
	GridSensorReactivePower         float64 // kVar
	OnOffGridStatus                 uint16  // 0: on grid, 1: off grid (auto), 2: off grid (manual)
	MaxActivePower                  float64 // kW
	MaxApparentPower                float64 // kVar
	ESSSOC                          float64 // %
	PlantPhaseAActivePower          float64 // kW
	PlantPhaseBActivePower          float64 // kW
	PlantPhaseCActivePower          float64 // kW
	PlantPhaseAReactivePower        float64 // kVar
	PlantPhaseBReactivePower        float64 // kVar
	PlantPhaseCReactivePower        float64 // kVar
	GeneralAlarm1                   uint16
	GeneralAlarm2                   uint16
	GeneralAlarm3                   uint16
	GeneralAlarm4                   uint16
	PlantActivePower                float64 // kW
	PlantReactivePower              float64 // kVar
	PhotovoltaicPower               float64 // kW
	ESSPower                        float64 // kW (<0: discharging, >0: charging)
	AvailableMaxActivePower         float64 // kW
	AvailableMinActivePower         float64 // kW
	AvailableMaxReactivePower       float64 // kVar
	AvailableMinReactivePower       float64 // kVar
	ESSAvailableMaxChargingPower    float64 // kW
	ESSAvailableMaxDischargingPower float64 // kW
	PlantRunningState               uint16
	ESSRatedEnergyCapacity          float64 // kWh
	ESSChargeOffSOC                 float64 // %
	ESSDischargeOffSOC              float64 // %
	ESSSOH                          float64 // %
	ESSAvgCellTemperature           float64 // °C
	DCChargerOutputPower            float64 // kW
	DCChargerVehicleSOC             float64 // %
}

// ReadPlantRunningInfo reads plant running information (slave address 247)
func (c *SigenModbusClient) ReadPlantRunningInfo() (*PlantRunningInfo, error) {
	c.SetSlaveID(PlantAddress)

	// Read main block (30000-30051, 52 registers)
	data, err := c.client.ReadInputRegisters(30000, 52)
	if err != nil {
		return nil, fmt.Errorf("failed to read plant running info: %v", err)
	}

	info := &PlantRunningInfo{
		SystemTime:                      bytesToU32(data[0:4]),
		SystemTimeZone:                  bytesToS16(data[4:6]),
		EMSWorkMode:                     bytesToU16(data[6:8]),
		GridSensorStatus:                bytesToU16(data[8:10]),
		GridSensorActivePower:           float64(bytesToS32(data[10:14])) / 1000.0,
		GridSensorReactivePower:         float64(bytesToS32(data[14:18])) / 1000.0,
		OnOffGridStatus:                 bytesToU16(data[18:20]),
		MaxActivePower:                  float64(bytesToU32(data[20:24])) / 1000.0,
		MaxApparentPower:                float64(bytesToU32(data[24:28])) / 1000.0,
		ESSSOC:                          float64(bytesToU16(data[28:30])) / 10.0,
		PlantPhaseAActivePower:          float64(bytesToS32(data[30:34])) / 1000.0,
		PlantPhaseBActivePower:          float64(bytesToS32(data[34:38])) / 1000.0,
		PlantPhaseCActivePower:          float64(bytesToS32(data[38:42])) / 1000.0,
		PlantPhaseAReactivePower:        float64(bytesToS32(data[42:46])) / 1000.0,
		PlantPhaseBReactivePower:        float64(bytesToS32(data[46:50])) / 1000.0,
		PlantPhaseCReactivePower:        float64(bytesToS32(data[50:54])) / 1000.0,
		GeneralAlarm1:                   bytesToU16(data[54:56]),
		GeneralAlarm2:                   bytesToU16(data[56:58]),
		GeneralAlarm3:                   bytesToU16(data[58:60]),
		GeneralAlarm4:                   bytesToU16(data[60:62]),
		PlantActivePower:                float64(bytesToS32(data[62:66])) / 1000.0,
		PlantReactivePower:              float64(bytesToS32(data[66:70])) / 1000.0,
		PhotovoltaicPower:               float64(bytesToS32(data[70:74])) / 1000.0,
		ESSPower:                        float64(bytesToS32(data[74:78])) / 1000.0,
		AvailableMaxActivePower:         float64(bytesToU32(data[78:82])) / 1000.0,
		AvailableMinActivePower:         float64(bytesToU32(data[82:86])) / 1000.0,
		AvailableMaxReactivePower:       float64(bytesToU32(data[86:90])) / 1000.0,
		AvailableMinReactivePower:       float64(bytesToU32(data[90:94])) / 1000.0,
		ESSAvailableMaxChargingPower:    float64(bytesToU32(data[94:98])) / 1000.0,
		ESSAvailableMaxDischargingPower: float64(bytesToU32(data[98:102])) / 1000.0,
		PlantRunningState:               bytesToU16(data[102:104]),
	}

	// Read additional ESS data (30083-30087)
	data2, err := c.client.ReadInputRegisters(30083, 5)
	if err == nil {
		info.ESSRatedEnergyCapacity = float64(bytesToU32(data2[0:4])) / 100.0
		info.ESSChargeOffSOC = float64(bytesToU16(data2[4:6])) / 10.0
		info.ESSDischargeOffSOC = float64(bytesToU16(data2[6:8])) / 10.0
		info.ESSSOH = float64(bytesToU16(data2[8:10])) / 10.0
	}

	// Read DC Charger and ESS cell temperature from first inverter (slave address 1)
	// Registers 31502-31504 are Hybrid Inverter registers (section 5.3) and only respond
	// to slave addresses 1-246, NOT the plant address 247.
	// Note: This assumes at least one hybrid inverter is present with slave ID 1
	c.SetSlaveID(2)

	// Read DC Charger data (31502-31504)
	data3, err := c.client.ReadInputRegisters(31502, 3)
	if err == nil {
		info.DCChargerOutputPower = float64(bytesToS32(data3[0:4])) / 1000.0
		info.DCChargerVehicleSOC = float64(bytesToU16(data3[4:6])) / 10.0
	}

	data4, err := c.client.ReadInputRegisters(30603, 1)
	if err == nil {
		info.ESSAvgCellTemperature = float64(bytesToS16(data4[0:2])) / 10.0
	}
	// Reset to plant address
	c.SetSlaveID(PlantAddress)

	return info, nil
}

// PlantParameters represents the plant parameter settings (Section 5.2)
type PlantParameters struct {
	ActivePowerFixedTarget   float64 // kW
	ReactivePowerFixedTarget float64 // kVar
	ActivePowerPercentTarget float64 // %
	QSAdjustmentTarget       float64 // %
	PowerFactorTarget        float64
	RemoteEMSEnable          bool
	RemoteEMSControlMode     uint16
	ESSMaxChargingLimit      float64 // kW
	ESSMaxDischargingLimit   float64 // kW
	PVMaxPowerLimit          float64 // kW
	GridPointMaxExportLimit  float64 // kW
	GridPointMaxImportLimit  float64 // kW
	PCSMaxExportLimit        float64 // kW
	PCSMaxImportLimit        float64 // kW
}

// StartPlant starts the plant (slave address 247)
func (c *SigenModbusClient) StartPlant() error {
	c.SetSlaveID(PlantAddress)
	_, err := c.client.WriteSingleRegister(40000, 1)
	return err
}

// StopPlant stops the plant (slave address 247)
func (c *SigenModbusClient) StopPlant() error {
	c.SetSlaveID(PlantAddress)
	_, err := c.client.WriteSingleRegister(40000, 0)
	return err
}

// SetActivePowerFixed sets fixed active power target (kW)
func (c *SigenModbusClient) SetActivePowerFixed(powerKW float64) error {
	c.SetSlaveID(PlantAddress)
	value := int32(powerKW * 1000)
	_, err := c.client.WriteMultipleRegisters(40001, 2, s32ToBytes(value))
	return err
}

// SetReactivePowerFixed sets fixed reactive power target (kVar)
func (c *SigenModbusClient) SetReactivePowerFixed(powerKVar float64) error {
	c.SetSlaveID(PlantAddress)
	value := int32(powerKVar * 1000)
	_, err := c.client.WriteMultipleRegisters(40003, 2, s32ToBytes(value))
	return err
}

// SetActivePowerPercent sets active power percentage target (-100.00 to 100.00%)
func (c *SigenModbusClient) SetActivePowerPercent(percent float64) error {
	c.SetSlaveID(PlantAddress)
	value := int16(percent * 100)
	// #nosec G115 -- Modbus register requires uint16, intentional conversion from signed value
	_, err := c.client.WriteSingleRegister(40005, uint16(value))
	return err
}

// SetPowerFactor sets power factor adjustment target (-1 to 1, range: (-1, -0.8] U [0.8, 1])
func (c *SigenModbusClient) SetPowerFactor(pf float64) error {
	c.SetSlaveID(PlantAddress)
	value := int16(pf * 1000)
	// #nosec G115 -- Modbus register requires uint16, intentional conversion from signed value
	_, err := c.client.WriteSingleRegister(40007, uint16(value))
	return err
}

// EnableRemoteEMS enables or disables remote EMS control
func (c *SigenModbusClient) EnableRemoteEMS(enable bool) error {
	c.SetSlaveID(PlantAddress)
	var value uint16
	if enable {
		value = 1
	}
	_, err := c.client.WriteSingleRegister(40029, value)
	return err
}

// SetRemoteEMSMode sets the remote EMS control mode
// 0: PCS remote control, 1: Standby, 2: Maximum self-consumption
// 3: Command charging (grid first), 4: Command charging (PV first)
// 5: Command discharging (PV first), 6: Command discharging (ESS first)
func (c *SigenModbusClient) SetRemoteEMSMode(mode uint16) error {
	c.SetSlaveID(PlantAddress)
	_, err := c.client.WriteSingleRegister(40031, mode)
	return err
}

// SetESSMaxChargingLimit sets ESS max charging limit (kW)
func (c *SigenModbusClient) SetESSMaxChargingLimit(powerKW float64) error {
	c.SetSlaveID(PlantAddress)
	value := uint32(powerKW * 1000)
	_, err := c.client.WriteMultipleRegisters(40032, 2, u32ToBytes(value))
	return err
}

// SetESSMaxDischargingLimit sets ESS max discharging limit (kW)
func (c *SigenModbusClient) SetESSMaxDischargingLimit(powerKW float64) error {
	c.SetSlaveID(PlantAddress)
	value := uint32(powerKW * 1000)
	_, err := c.client.WriteMultipleRegisters(40034, 2, u32ToBytes(value))
	return err
}

// SetPVMaxPowerLimit sets PV max power limit (kW)
func (c *SigenModbusClient) SetPVMaxPowerLimit(powerKW float64) error {
	c.SetSlaveID(PlantAddress)
	value := uint32(powerKW * 1000)
	_, err := c.client.WriteMultipleRegisters(40036, 2, u32ToBytes(value))
	return err
}

// HybridInverterInfo represents the hybrid inverter running information (Section 5.3)
type HybridInverterInfo struct {
	ModelType                 string
	SerialNumber              string
	FirmwareVersion           string
	RatedActivePower          float64 // kW
	MaxApparentPower          float64 // kVA
	MaxActivePower            float64 // kW
	MaxAbsorptionPower        float64 // kW
	RatedBatteryCapacity      float64 // kWh
	ESSRatedChargePower       float64 // kW
	ESSRatedDischargePower    float64 // kW
	RunningState              uint16
	ActivePower               float64 // kW
	ReactivePower             float64 // kVar
	ESSChargeOrDischargePower float64 // kW
	ESSSOC                    float64 // %
	ESSSOH                    float64 // %
	ESSAvgCellTemperature     float64 // °C
	ESSAvgCellVoltage         float64 // V
	Alarm1                    uint16
	Alarm2                    uint16
	Alarm3                    uint16
	Alarm4                    uint16
	Alarm5                    uint16
	RatedGridVoltage          float64 // V
	RatedGridFrequency        float64 // Hz
	GridFrequency             float64 // Hz
	PCSInternalTemperature    float64 // °C
	OutputType                uint16  // 0: L/N, 1: L1/L2/L3, 2: L1/L2/L3/N, 3: L1/L2/N
	PhaseAVoltage             float64 // V
	PhaseBVoltage             float64 // V
	PhaseCVoltage             float64 // V
	PhaseACurrent             float64 // A
	PhaseBCurrent             float64 // A
	PhaseCCurrent             float64 // A
	PowerFactor               float64
	PVPower                   float64 // kW
	InsulationResistance      float64 // MΩ
}

// ReadHybridInverterInfo reads hybrid inverter information
func (c *SigenModbusClient) ReadHybridInverterInfo(slaveID byte) (*HybridInverterInfo, error) {
	if slaveID < MinSlaveAddress || slaveID > MaxSlaveAddress {
		return nil, fmt.Errorf("invalid slave ID: must be between %d and %d", MinSlaveAddress, MaxSlaveAddress)
	}
	c.SetSlaveID(slaveID)

	// Read device info (30540-30552)
	data, err := c.client.ReadInputRegisters(30540, 13)
	if err != nil {
		return nil, fmt.Errorf("failed to read inverter info: %v", err)
	}

	info := &HybridInverterInfo{
		RatedActivePower:       float64(bytesToU32(data[0:4])) / 1000.0,
		MaxApparentPower:       float64(bytesToU32(data[4:8])) / 1000.0,
		MaxActivePower:         float64(bytesToU32(data[8:12])) / 1000.0,
		MaxAbsorptionPower:     float64(bytesToU32(data[12:16])) / 1000.0,
		RatedBatteryCapacity:   float64(bytesToU32(data[16:20])) / 100.0,
		ESSRatedChargePower:    float64(bytesToU32(data[20:24])) / 1000.0,
		ESSRatedDischargePower: float64(bytesToU32(data[24:28])) / 1000.0,
	}

	// Read running state and power (30578-30609)
	data2, err := c.client.ReadInputRegisters(30578, 32)
	if err != nil {
		return nil, fmt.Errorf("failed to read running state: %v", err)
	}

	info.RunningState = bytesToU16(data2[0:2])
	info.ActivePower = float64(bytesToS32(data2[18:22])) / 1000.0
	info.ReactivePower = float64(bytesToS32(data2[22:26])) / 1000.0
	info.ESSChargeOrDischargePower = float64(bytesToS32(data2[42:46])) / 1000.0
	info.ESSSOC = float64(bytesToU16(data2[46:48])) / 10.0
	info.ESSSOH = float64(bytesToU16(data2[48:50])) / 10.0
	info.ESSAvgCellTemperature = float64(bytesToS16(data2[50:52])) / 10.0
	info.ESSAvgCellVoltage = float64(bytesToU16(data2[52:54])) / 1000.0
	info.Alarm1 = bytesToU16(data2[54:56])
	info.Alarm2 = bytesToU16(data2[56:58])
	info.Alarm3 = bytesToU16(data2[58:60])
	info.Alarm4 = bytesToU16(data2[60:62])
	info.Alarm5 = bytesToU16(data2[62:64])

	// Read grid and phase info (31000-31035)
	data3, err := c.client.ReadInputRegisters(31000, 36)
	if err != nil {
		return nil, fmt.Errorf("failed to read grid info: %v", err)
	}

	info.RatedGridVoltage = float64(bytesToU16(data3[0:2])) / 10.0
	info.RatedGridFrequency = float64(bytesToU16(data3[2:4])) / 100.0
	info.GridFrequency = float64(bytesToU16(data3[4:6])) / 100.0
	info.PCSInternalTemperature = float64(bytesToS16(data3[6:8])) / 10.0
	info.OutputType = bytesToU16(data3[8:10])
	info.PhaseAVoltage = float64(bytesToU32(data3[22:26])) / 100.0
	info.PhaseBVoltage = float64(bytesToU32(data3[26:30])) / 100.0
	info.PhaseCVoltage = float64(bytesToU32(data3[30:34])) / 100.0
	info.PhaseACurrent = float64(bytesToS32(data3[34:38])) / 100.0
	info.PhaseBCurrent = float64(bytesToS32(data3[38:42])) / 100.0
	info.PhaseCCurrent = float64(bytesToS32(data3[42:46])) / 100.0
	info.PowerFactor = float64(bytesToU16(data3[46:48])) / 1000.0
	info.PVPower = float64(bytesToS32(data3[70:74])) / 1000.0
	info.InsulationResistance = float64(bytesToU16(data3[74:76])) / 1000.0

	return info, nil
}

// StartInverter starts a specific inverter
func (c *SigenModbusClient) StartInverter(slaveID byte) error {
	if slaveID < MinSlaveAddress || slaveID > MaxSlaveAddress {
		return fmt.Errorf("invalid slave ID: must be between %d and %d", MinSlaveAddress, MaxSlaveAddress)
	}
	c.SetSlaveID(slaveID)
	_, err := c.client.WriteSingleRegister(40500, 1)
	return err
}

// StopInverter stops a specific inverter
func (c *SigenModbusClient) StopInverter(slaveID byte) error {
	if slaveID < MinSlaveAddress || slaveID > MaxSlaveAddress {
		return fmt.Errorf("invalid slave ID: must be between %d and %d", MinSlaveAddress, MaxSlaveAddress)
	}
	c.SetSlaveID(slaveID)
	_, err := c.client.WriteSingleRegister(40500, 0)
	return err
}

// ACChargerInfo represents the AC-Charger information (Section 5.5)
type ACChargerInfo struct {
	SystemState              uint16  // System state according to IEC61851-1
	TotalEnergyConsumed      float64 // kWh
	ChargingPower            float64 // kW
	RatedPower               float64 // kW
	RatedCurrent             float64 // A
	RatedVoltage             float64 // V
	InputBreakerRatedCurrent float64 // A
	Alarm1                   uint16
	Alarm2                   uint16
	Alarm3                   uint16
}

// ReadACChargerInfo reads AC charger information
func (c *SigenModbusClient) ReadACChargerInfo(slaveID byte) (*ACChargerInfo, error) {
	if slaveID < MinSlaveAddress || slaveID > MaxSlaveAddress {
		return nil, fmt.Errorf("invalid slave ID: must be between %d and %d", MinSlaveAddress, MaxSlaveAddress)
	}
	c.SetSlaveID(slaveID)

	data, err := c.client.ReadInputRegisters(32000, 15)
	if err != nil {
		return nil, fmt.Errorf("failed to read AC charger info: %v", err)
	}

	info := &ACChargerInfo{
		SystemState:              bytesToU16(data[0:2]),
		TotalEnergyConsumed:      float64(bytesToU32(data[2:6])) / 100.0,
		ChargingPower:            float64(bytesToS32(data[6:10])) / 1000.0,
		RatedPower:               float64(bytesToU32(data[10:14])) / 1000.0,
		RatedCurrent:             float64(bytesToS32(data[14:18])) / 100.0,
		RatedVoltage:             float64(bytesToU16(data[18:20])) / 10.0,
		InputBreakerRatedCurrent: float64(bytesToS32(data[20:24])) / 100.0,
		Alarm1:                   bytesToU16(data[24:26]),
		Alarm2:                   bytesToU16(data[26:28]),
		Alarm3:                   bytesToU16(data[28:30]),
	}

	return info, nil
}

// StartACCharger starts AC charger
func (c *SigenModbusClient) StartACCharger(slaveID byte) error {
	if slaveID < MinSlaveAddress || slaveID > MaxSlaveAddress {
		return fmt.Errorf("invalid slave ID: must be between %d and %d", MinSlaveAddress, MaxSlaveAddress)
	}
	c.SetSlaveID(slaveID)
	_, err := c.client.WriteSingleRegister(42000, 0)
	return err
}

// StopACCharger stops AC charger
func (c *SigenModbusClient) StopACCharger(slaveID byte) error {
	if slaveID < MinSlaveAddress || slaveID > MaxSlaveAddress {
		return fmt.Errorf("invalid slave ID: must be between %d and %d", MinSlaveAddress, MaxSlaveAddress)
	}
	c.SetSlaveID(slaveID)
	_, err := c.client.WriteSingleRegister(42000, 1)
	return err
}

// SetACChargerOutputCurrent sets AC charger output current
func (c *SigenModbusClient) SetACChargerOutputCurrent(slaveID byte, current float64) error {
	if slaveID < MinSlaveAddress || slaveID > MaxSlaveAddress {
		return fmt.Errorf("invalid slave ID: must be between %d and %d", MinSlaveAddress, MaxSlaveAddress)
	}
	c.SetSlaveID(slaveID)
	value := uint32(current * 100)
	_, err := c.client.WriteMultipleRegisters(42001, 2, u32ToBytes(value))
	return err
}
