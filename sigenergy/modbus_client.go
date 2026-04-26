package sigenergy

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/devskill-org/ems/modbus"
)

// Modbus client configuration
const (
	PlantAddress     = 247
	BroadcastAddress = 0
	MinSlaveAddress  = 1
	MaxSlaveAddress  = 246
)

// SigenModbusClient represents the Sigenergy Modbus client
type SigenModbusClient struct {
	client     modbus.Client
	handler    *modbus.RTUClientHandler
	tcpHandler *modbus.TCPClientHandler
	// cancelWatch is set for TCP clients; calling it stops the context-watcher
	// goroutine that closes the connection on context cancellation.
	cancelWatch context.CancelFunc
}

// NewRTUClient creates a new Sigenergy Modbus RTU client
func NewRTUClient(device string, baudRate int, slaveID byte) (*SigenModbusClient, error) {
	handler := modbus.NewRTUClientHandler(device)
	handler.BaudRate = baudRate
	handler.DataBits = 8
	handler.Parity = "N"
	handler.StopBits = 1
	handler.SlaveID = slaveID
	handler.Timeout = 1 * time.Second

	err := handler.Connect()
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %v", err)
	}

	return &SigenModbusClient{
		client:  modbus.NewClient(handler),
		handler: handler,
	}, nil
}

// NewTCPClient creates a new Sigenergy Modbus TCP client. The provided context
// governs both the dial phase and the lifetime of the connection: if the
// context is cancelled or its deadline expires, the underlying TCP connection
// is closed immediately, which unblocks any in-flight register read or write.
func NewTCPClient(ctx context.Context, address string, slaveID byte) (*SigenModbusClient, error) {
	handler := modbus.NewTCPClientHandler(address)
	handler.SlaveID = slaveID
	handler.Timeout = 1 * time.Second

	if err := handler.ConnectWithContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", address, err)
	}

	// Detached watch context so we can stop the goroutine independently of
	// the parent context (e.g. when Close is called before cancellation).
	watchCtx, cancelWatch := context.WithCancel(context.Background())

	c := &SigenModbusClient{
		client:      modbus.NewClient(handler),
		tcpHandler:  handler,
		cancelWatch: cancelWatch,
	}

	// Close the TCP connection as soon as either the caller's context is done
	// or Close() is called (which calls cancelWatch). This immediately
	// unblocks any in-flight conn.Write / io.ReadFull inside Send().
	go func() {
		select {
		case <-ctx.Done():
			_ = handler.Close()
		case <-watchCtx.Done():
			// Close() was called first — nothing to do, connection already closed.
		}
	}()

	return c, nil
}

// Close closes the Modbus connection and stops the context-watcher goroutine.
func (c *SigenModbusClient) Close() error {
	if c.cancelWatch != nil {
		c.cancelWatch()
	}
	if c.handler != nil {
		return c.handler.Close()
	}
	if c.tcpHandler != nil {
		return c.tcpHandler.Close()
	}
	return nil
}

// SetSlaveID changes the slave ID for subsequent operations
func (c *SigenModbusClient) SetSlaveID(slaveID byte) {
	if c.handler != nil {
		c.handler.SlaveID = slaveID
	}
	if c.tcpHandler != nil {
		c.tcpHandler.SlaveID = slaveID
	}
}

func bytesToU16(b []byte) uint16 {
	return binary.BigEndian.Uint16(b)
}

func bytesToS16(b []byte) int16 {
	return int16(binary.BigEndian.Uint16(b)) //nolint:gosec // reinterpreting raw register bytes as signed 16-bit value by design
}

func bytesToU32(b []byte) uint32 {
	return binary.BigEndian.Uint32(b)
}

func bytesToS32(b []byte) int32 {
	return int32(binary.BigEndian.Uint32(b)) //nolint:gosec // reinterpreting raw register bytes as signed 32-bit value by design
}

func u32ToBytes(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

func s32ToBytes(v int32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(v)) //nolint:gosec // reinterpreting signed 32-bit value as raw register bytes by design
	return b
}