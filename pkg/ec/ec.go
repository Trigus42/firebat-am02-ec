// Package ec provides low-level access to the ACPI Embedded Controller (EC)
// via x86 IO ports.
//
// The EC is a microcontroller on the motherboard that manages hardware such as
// fans, LEDs, and thermal sensors. Communication uses a simple command/data
// protocol over two IO ports (typically 0x62 for data, 0x66 for command/status).
//
// This package requires privileged access to /dev/port.
package ec

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

const (
	// Standard ACPI EC IO port addresses.
	DefaultDataPort = 0x62
	DefaultCmdPort  = 0x66

	// EC commands sent to the command port.
	cmdRead  = 0x80
	cmdWrite = 0x81

	// EC status register bit masks (read from command port).
	statusOBF = 0x01 // Output Buffer Full — EC has data for us to read.
	statusIBF = 0x02 // Input Buffer Full — EC is still processing our last byte.

	// How long to wait for the EC to become ready.
	defaultTimeout = 100 * time.Millisecond
)

// EC communicates with an ACPI Embedded Controller through IO ports.
// All operations are serialized with a mutex since the EC protocol
// requires atomic multi-byte command sequences.
type EC struct {
	port    *port
	dataAddr uint16
	cmdAddr  uint16
	timeout time.Duration
	mu      sync.Mutex
}

// Option configures an EC instance.
type Option func(*EC)

// WithTimeout sets the maximum time to wait for EC readiness.
func WithTimeout(d time.Duration) Option {
	return func(ec *EC) {
		ec.timeout = d
	}
}

// WithPorts overrides the default EC IO port addresses.
func WithPorts(dataPort, cmdPort uint16) Option {
	return func(ec *EC) {
		ec.dataAddr = dataPort
		ec.cmdAddr = cmdPort
	}
}

// Open creates a new EC handle using /dev/port for IO access.
func Open(opts ...Option) (*EC, error) {
	p, err := openPort()
	if err != nil {
		return nil, fmt.Errorf("ec: open /dev/port: %w", err)
	}

	ec := &EC{
		port:     p,
		dataAddr: DefaultDataPort,
		cmdAddr:  DefaultCmdPort,
		timeout:  defaultTimeout,
	}
	for _, opt := range opts {
		opt(ec)
	}
	return ec, nil
}

// newFromPort creates an EC from an existing port (used in tests).
func newFromPort(p *port, opts ...Option) *EC {
	ec := &EC{
		port:     p,
		dataAddr: DefaultDataPort,
		cmdAddr:  DefaultCmdPort,
		timeout:  defaultTimeout,
	}
	for _, opt := range opts {
		opt(ec)
	}
	return ec
}

// Close releases the underlying file descriptor.
func (ec *EC) Close() error {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	return ec.port.Close()
}

// Read retrieves one byte from an EC register.
//
// Protocol:
//  1. Wait until EC input buffer is empty (IBF clear)
//  2. Send read command (0x80) to command port
//  3. Wait until EC input buffer is empty
//  4. Send register address to data port
//  5. Wait until EC output buffer is full (OBF set)
//  6. Read result from data port
func (ec *EC) Read(register byte) (byte, error) {
	ec.mu.Lock()
	defer ec.mu.Unlock()

	if err := ec.waitIBFClear(); err != nil {
		return 0, fmt.Errorf("ec: read 0x%02X: pre-cmd wait: %w", register, err)
	}
	ec.port.Outb(ec.cmdAddr, cmdRead)

	if err := ec.waitIBFClear(); err != nil {
		return 0, fmt.Errorf("ec: read 0x%02X: pre-addr wait: %w", register, err)
	}
	ec.port.Outb(ec.dataAddr, register)

	if err := ec.waitOBFSet(); err != nil {
		return 0, fmt.Errorf("ec: read 0x%02X: data wait: %w", register, err)
	}
	return ec.port.Inb(ec.dataAddr), nil
}

// Write stores one byte into an EC register.
//
// Protocol:
//  1. Wait until EC input buffer is empty (IBF clear)
//  2. Send write command (0x81) to command port
//  3. Wait until EC input buffer is empty
//  4. Send register address to data port
//  5. Wait until EC input buffer is empty
//  6. Send value to data port
func (ec *EC) Write(register, value byte) error {
	ec.mu.Lock()
	defer ec.mu.Unlock()

	if err := ec.waitIBFClear(); err != nil {
		return fmt.Errorf("ec: write 0x%02X: pre-cmd wait: %w", register, err)
	}
	ec.port.Outb(ec.cmdAddr, cmdWrite)

	if err := ec.waitIBFClear(); err != nil {
		return fmt.Errorf("ec: write 0x%02X: pre-addr wait: %w", register, err)
	}
	ec.port.Outb(ec.dataAddr, register)

	if err := ec.waitIBFClear(); err != nil {
		return fmt.Errorf("ec: write 0x%02X: pre-data wait: %w", register, err)
	}
	ec.port.Outb(ec.dataAddr, value)

	return nil
}

// waitIBFClear spins until the EC's input buffer is empty, meaning it's
// ready to accept a new byte from us.
func (ec *EC) waitIBFClear() error {
	deadline := time.Now().Add(ec.timeout)
	for time.Now().Before(deadline) {
		status := ec.port.Inb(ec.cmdAddr)
		if status&statusIBF == 0 {
			return nil
		}
	}
	return errors.New("timeout waiting for EC input buffer to clear (IBF)")
}

// waitOBFSet spins until the EC's output buffer has data for us to read.
func (ec *EC) waitOBFSet() error {
	deadline := time.Now().Add(ec.timeout)
	for time.Now().Before(deadline) {
		status := ec.port.Inb(ec.cmdAddr)
		if status&statusOBF != 0 {
			return nil
		}
	}
	return errors.New("timeout waiting for EC output buffer full (OBF)")
}

// port wraps a file descriptor to /dev/port for byte-level IO access.
type port struct {
	f *os.File
}

func openPort() (*port, error) {
	f, err := os.OpenFile("/dev/port", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	return &port{f: f}, nil
}

func (p *port) Close() error {
	return p.f.Close()
}

// Outb writes a single byte to the given IO port address.
func (p *port) Outb(addr uint16, val byte) {
	p.f.Seek(int64(addr), 0)
	p.f.Write([]byte{val})
}

// Inb reads a single byte from the given IO port address.
func (p *port) Inb(addr uint16) byte {
	p.f.Seek(int64(addr), 0)
	buf := []byte{0}
	p.f.Read(buf)
	return buf[0]
}
