// Copyright 2014 Quoc-Viet Nguyen. All rights reserved.
// This software may be modified and distributed under the terms
// of the BSD license. See the LICENSE file for details.

/*
Package modbus provides a client for MODBUS TCP and RTU/ASCII.
Vendored from github.com/goburrow/modbus v0.1.0; the TCP transporter has been
rewritten to use net.DialContext so connections respect context cancellation
and deadlines.
*/
package modbus

import (
	"fmt"
)

const (
	// FuncCodeReadDiscreteInputs is the function code for reading discrete inputs (bit access).
	FuncCodeReadDiscreteInputs = 2
	// FuncCodeReadCoils is the function code for reading coils.
	FuncCodeReadCoils          = 1
	// FuncCodeWriteSingleCoil is the function code for writing a single coil.
	FuncCodeWriteSingleCoil    = 5
	// FuncCodeWriteMultipleCoils is the function code for writing multiple coils.
	FuncCodeWriteMultipleCoils = 15

	// FuncCodeReadInputRegisters is the function code for reading input registers (16-bit access).
	FuncCodeReadInputRegisters         = 4
	// FuncCodeReadHoldingRegisters is the function code for reading holding registers.
	FuncCodeReadHoldingRegisters       = 3
	// FuncCodeWriteSingleRegister is the function code for writing a single register.
	FuncCodeWriteSingleRegister        = 6
	// FuncCodeWriteMultipleRegisters is the function code for writing multiple registers.
	FuncCodeWriteMultipleRegisters     = 16
	// FuncCodeReadWriteMultipleRegisters is the function code for read/write of multiple registers.
	FuncCodeReadWriteMultipleRegisters = 23
	// FuncCodeMaskWriteRegister is the function code for mask-writing a register.
	FuncCodeMaskWriteRegister          = 22
	// FuncCodeReadFIFOQueue is the function code for reading a FIFO queue.
	FuncCodeReadFIFOQueue              = 24
)

const (
	// ExceptionCodeIllegalFunction indicates the function code is not supported.
	ExceptionCodeIllegalFunction                    = 1
	// ExceptionCodeIllegalDataAddress indicates the data address is not allowed.
	ExceptionCodeIllegalDataAddress                 = 2
	// ExceptionCodeIllegalDataValue indicates a value in the request is not allowed.
	ExceptionCodeIllegalDataValue                   = 3
	// ExceptionCodeServerDeviceFailure indicates an unrecoverable error occurred in the server.
	ExceptionCodeServerDeviceFailure                = 4
	// ExceptionCodeAcknowledge indicates the server has accepted the request and is processing it.
	ExceptionCodeAcknowledge                        = 5
	// ExceptionCodeServerDeviceBusy indicates the server is busy processing a long-duration command.
	ExceptionCodeServerDeviceBusy                   = 6
	// ExceptionCodeMemoryParityError indicates a parity error was detected in the extended memory.
	ExceptionCodeMemoryParityError                  = 8
	// ExceptionCodeGatewayPathUnavailable indicates the gateway path is unavailable.
	ExceptionCodeGatewayPathUnavailable             = 10
	// ExceptionCodeGatewayTargetDeviceFailedToRespond indicates the target device failed to respond.
	ExceptionCodeGatewayTargetDeviceFailedToRespond = 11
)

// Error represents a Modbus exception response from a remote device.
type Error struct {
	FunctionCode  byte
	ExceptionCode byte
}

// Error converts known modbus exception code to error message.
func (e *Error) Error() string {
	var name string
	switch e.ExceptionCode {
	case ExceptionCodeIllegalFunction:
		name = "illegal function"
	case ExceptionCodeIllegalDataAddress:
		name = "illegal data address"
	case ExceptionCodeIllegalDataValue:
		name = "illegal data value"
	case ExceptionCodeServerDeviceFailure:
		name = "server device failure"
	case ExceptionCodeAcknowledge:
		name = "acknowledge"
	case ExceptionCodeServerDeviceBusy:
		name = "server device busy"
	case ExceptionCodeMemoryParityError:
		name = "memory parity error"
	case ExceptionCodeGatewayPathUnavailable:
		name = "gateway path unavailable"
	case ExceptionCodeGatewayTargetDeviceFailedToRespond:
		name = "gateway target device failed to respond"
	default:
		name = "unknown"
	}
	return fmt.Sprintf("modbus: exception '%v' (%s), function '%v'", e.ExceptionCode, name, e.FunctionCode)
}

// ProtocolDataUnit (PDU) is independent of underlying communication layers.
type ProtocolDataUnit struct {
	FunctionCode byte
	Data         []byte
}

// Packager specifies the communication layer.
type Packager interface {
	Encode(pdu *ProtocolDataUnit) (adu []byte, err error)
	Decode(adu []byte) (pdu *ProtocolDataUnit, err error)
	Verify(aduRequest []byte, aduResponse []byte) (err error)
}

// Transporter specifies the transport layer.
type Transporter interface {
	Send(aduRequest []byte) (aduResponse []byte, err error)
}
