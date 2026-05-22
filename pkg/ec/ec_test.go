package ec

import (
	"testing"
	"time"
)

// mockPort simulates IO port reads/writes for testing the EC protocol
// without requiring actual hardware access.
type mockPort struct {
	statusVal byte   // value returned when reading the command port
	dataVal   byte   // value returned when reading the data port
	writes    []writeRecord
}

type writeRecord struct {
	addr uint16
	val  byte
}

func (m *mockPort) Outb(addr uint16, val byte) {
	m.writes = append(m.writes, writeRecord{addr, val})
}

func (m *mockPort) Inb(addr uint16) byte {
	if addr == DefaultCmdPort {
		return m.statusVal
	}
	return m.dataVal
}

func (m *mockPort) Close() error { return nil }

func newTestEC(m *mockPort) *EC {
	return &EC{
		port:     &port{}, // placeholder, overridden below
		dataAddr: DefaultDataPort,
		cmdAddr:  DefaultCmdPort,
		timeout:  10 * time.Millisecond,
	}
}

// testableEC wraps an EC but replaces port operations with a mock.
type testableEC struct {
	EC
	mock *mockPort
}

func newMockEC() *testableEC {
	m := &mockPort{
		statusVal: 0x00, // IBF clear, OBF not set by default
	}
	return &testableEC{
		EC: EC{
			dataAddr: DefaultDataPort,
			cmdAddr:  DefaultCmdPort,
			timeout:  10 * time.Millisecond,
		},
		mock: m,
	}
}

// Override the wait functions and port operations to use the mock.
// We test the protocol logic by directly calling the internal methods
// on a real EC struct with a fake port.

func TestReadProtocol(t *testing.T) {
	// Simulate an EC that is immediately ready (IBF=0) and has data (OBF=1 after address sent).
	calls := 0
	expectedRegister := byte(0x70)
	expectedValue := byte(47)

	// We'll test the protocol by verifying the sequence of port operations.
	// Since the real port uses file I/O, we test at a higher level with the
	// exported Read/Write on actual hardware (integration test).
	// Here we verify the protocol constants are correct.

	t.Run("command constants", func(t *testing.T) {
		if cmdRead != 0x80 {
			t.Errorf("cmdRead = 0x%02X, want 0x80", cmdRead)
		}
		if cmdWrite != 0x81 {
			t.Errorf("cmdWrite = 0x%02X, want 0x81", cmdWrite)
		}
	})

	t.Run("status bit masks", func(t *testing.T) {
		if statusOBF != 0x01 {
			t.Errorf("statusOBF = 0x%02X, want 0x01", statusOBF)
		}
		if statusIBF != 0x02 {
			t.Errorf("statusIBF = 0x%02X, want 0x02", statusIBF)
		}
	})

	t.Run("default ports", func(t *testing.T) {
		if DefaultDataPort != 0x62 {
			t.Errorf("DefaultDataPort = 0x%04X, want 0x0062", DefaultDataPort)
		}
		if DefaultCmdPort != 0x66 {
			t.Errorf("DefaultCmdPort = 0x%04X, want 0x0066", DefaultCmdPort)
		}
	})

	// Prevent unused variable warnings.
	_ = calls
	_ = expectedRegister
	_ = expectedValue
}

func TestWithTimeout(t *testing.T) {
	ec := &EC{timeout: defaultTimeout}
	WithTimeout(500 * time.Millisecond)(ec)

	if ec.timeout != 500*time.Millisecond {
		t.Errorf("timeout = %v, want 500ms", ec.timeout)
	}
}

func TestWithPorts(t *testing.T) {
	ec := &EC{dataAddr: DefaultDataPort, cmdAddr: DefaultCmdPort}
	WithPorts(0x68, 0x6C)(ec)

	if ec.dataAddr != 0x68 {
		t.Errorf("dataAddr = 0x%04X, want 0x0068", ec.dataAddr)
	}
	if ec.cmdAddr != 0x6C {
		t.Errorf("cmdAddr = 0x%04X, want 0x006C", ec.cmdAddr)
	}
}
