// Package device provides high-level abstractions for hardware controlled
// by the FIREBAT AM02 Embedded Controller.
package device

import (
	"fmt"

	"github.com/Trigus42/firebat-am02-ec/pkg/ec"
	"github.com/Trigus42/firebat-am02-ec/pkg/register"
)

// Fan controls a single fan connected to the EC.
type Fan struct {
	ec       *ec.EC
	register byte
}

// NewFan1 returns a handle to the primary fan.
func NewFan1(ec *ec.EC) *Fan {
	return &Fan{ec: ec, register: register.FAN1}
}

// NewFan2 returns a handle to the secondary fan.
func NewFan2(ec *ec.EC) *Fan {
	return &Fan{ec: ec, register: register.FAN2}
}

// SetDuty puts the fan in manual mode and sets the PWM duty cycle.
// Duty is clamped to 0–100 percent.
func (f *Fan) SetDuty(percent int) error {
	duty := clamp(percent, 0, register.FanMaxDuty)
	value := register.FanManualBit | byte(duty)
	return f.ec.Write(f.register, value)
}

// SetAuto returns the fan to EC automatic control.
// The EC will manage the fan speed based on its internal thermal table.
func (f *Fan) SetAuto() error {
	return f.ec.Write(f.register, 0x00)
}

// Status reads the current fan register and returns the duty cycle
// and whether manual mode is active.
type FanStatus struct {
	DutyPercent int
	ManualMode  bool
}

func (f *Fan) Status() (FanStatus, error) {
	val, err := f.ec.Read(f.register)
	if err != nil {
		return FanStatus{}, fmt.Errorf("fan status: %w", err)
	}

	return FanStatus{
		ManualMode:  val&register.FanManualBit != 0,
		DutyPercent: int(val & register.FanDutyMask),
	}, nil
}

func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
