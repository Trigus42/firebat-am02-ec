package device

import (
	"fmt"

	"github.com/Trigus42/firebat-am02-ec/pkg/ec"
	"github.com/Trigus42/firebat-am02-ec/pkg/register"
)

// ThermalSensor reads temperature from an EC register.
type ThermalSensor struct {
	ec       *ec.EC
	register byte
	name     string
}

// NewCPUTempSensor returns a sensor for the CPU temperature.
func NewCPUTempSensor(ec *ec.EC) *ThermalSensor {
	return &ThermalSensor{ec: ec, register: register.CPUT, name: "CPU"}
}

// NewGPUTempSensor returns a sensor for the integrated GPU temperature.
func NewGPUTempSensor(ec *ec.EC) *ThermalSensor {
	return &ThermalSensor{ec: ec, register: register.GPUT, name: "GPU"}
}

// ReadCelsius returns the current temperature in degrees Celsius.
func (s *ThermalSensor) ReadCelsius() (int, error) {
	val, err := s.ec.Read(s.register)
	if err != nil {
		return 0, fmt.Errorf("thermal %s: %w", s.name, err)
	}
	return int(val), nil
}

// Name returns a human-readable label for this sensor.
func (s *ThermalSensor) Name() string {
	return s.name
}
