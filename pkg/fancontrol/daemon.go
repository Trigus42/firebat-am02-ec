// Package fancontrol implements a temperature-reactive fan speed daemon.
//
// It reads CPU temperature at a configurable interval, maps it to a duty cycle
// using a piecewise-linear fan curve, and applies hysteresis and smoothing to
// prevent rapid fan speed oscillations. Each fan can have its own curve and
// temperature source.
package fancontrol

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Trigus42/firebat-am02-ec/pkg/device"
)

// CurvePoint defines one point on the fan curve.
// The daemon linearly interpolates between adjacent points.
type CurvePoint struct {
	TempC       int // Temperature threshold in degrees Celsius.
	DutyPercent int // Fan duty cycle at this temperature (0–100).
}

// FanConfig holds the curve and settings for a single fan.
type FanConfig struct {
	// Name is used in log messages (e.g. "fan1", "fan2").
	Name string

	// Fan is the hardware fan to control.
	Fan *device.Fan

	// Sensor is the temperature source for this fan's curve.
	Sensor *device.ThermalSensor

	// Curve maps temperatures to fan duty cycles.
	// Must be sorted by TempC in ascending order.
	Curve []CurvePoint

	// HysteresisDegrees prevents duty changes unless temperature has moved
	// at least this many degrees from the last adjustment point.
	HysteresisDegrees int

	// SmoothingFactor controls the exponential moving average for temperature.
	// Range: 0.0 (no smoothing, instant response) to 1.0 (ignores new readings).
	// Recommended: 0.3–0.7.
	SmoothingFactor float64
}

// Config holds all tunable parameters for the fan control daemon.
type Config struct {
	// Fans contains the configuration for each fan to control.
	Fans []FanConfig

	// Interval between temperature readings and fan adjustments.
	Interval time.Duration
}

// DefaultConfig returns a sensible fan curve for the FIREBAT AM02.
// Both fans use the same default curve with CPU temperature.
func DefaultConfig() Config {
	defaultCurve := []CurvePoint{
		{TempC: 0, DutyPercent: 0},
		{TempC: 40, DutyPercent: 0},
		{TempC: 50, DutyPercent: 20},
		{TempC: 60, DutyPercent: 40},
		{TempC: 70, DutyPercent: 60},
		{TempC: 80, DutyPercent: 80},
		{TempC: 90, DutyPercent: 100},
	}

	return Config{
		Fans: []FanConfig{
			{
				Name:              "fan1",
				Curve:             defaultCurve,
				HysteresisDegrees: 2,
				SmoothingFactor:   0.3,
			},
			{
				Name:              "fan2",
				Curve:             defaultCurve,
				HysteresisDegrees: 2,
				SmoothingFactor:   0.3,
			},
		},
		Interval: 5 * time.Second,
	}
}

// fanState tracks the runtime state for one fan.
type fanState struct {
	smoothedTemp float64
	lastDuty     int
	initialized  bool
}

// Daemon runs the fan control loop.
type Daemon struct {
	config Config
	logger *slog.Logger
}

// NewDaemon creates a fan control daemon.
func NewDaemon(config Config, logger *slog.Logger) *Daemon {
	if logger == nil {
		logger = slog.Default()
	}
	return &Daemon{
		config: config,
		logger: logger,
	}
}

// Run starts the fan control loop. It blocks until the context is cancelled.
// On exit (including context cancellation), it restores all fans to automatic mode.
func (d *Daemon) Run(ctx context.Context) error {
	d.logger.Info("fan control daemon starting",
		"interval", d.config.Interval,
		"fans", len(d.config.Fans),
	)

	// Always restore auto mode on exit.
	defer func() {
		d.logger.Info("restoring automatic fan control")
		for _, fc := range d.config.Fans {
			if err := fc.Fan.SetAuto(); err != nil {
				d.logger.Error("failed to restore auto mode", "fan", fc.Name, "error", err)
			}
		}
	}()

	states := make([]fanState, len(d.config.Fans))
	for i := range states {
		states[i].lastDuty = -1
	}

	ticker := time.NewTicker(d.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			for i, fc := range d.config.Fans {
				d.updateFan(&states[i], &fc)
			}
		}
	}
}

func (d *Daemon) updateFan(state *fanState, fc *FanConfig) {
	temp, err := fc.Sensor.ReadCelsius()
	if err != nil {
		d.logger.Error("failed to read temperature", "fan", fc.Name, "error", err)
		return
	}

	// Exponential moving average for stable readings.
	if !state.initialized {
		state.smoothedTemp = float64(temp)
		state.initialized = true
	} else {
		alpha := fc.SmoothingFactor
		state.smoothedTemp = alpha*state.smoothedTemp + (1-alpha)*float64(temp)
	}

	targetDuty := Interpolate(state.smoothedTemp, fc.Curve)

	// Always enforce the target duty. This corrects any external manual
	// changes (e.g. via "fan set") and ensures the daemon stays in control.
	// Only apply hysteresis to suppress log spam — we still write the duty.
	shouldLog := state.lastDuty < 0 || abs(targetDuty-state.lastDuty) >= 3

	if err := fc.Fan.SetDuty(targetDuty); err != nil {
		d.logger.Error("failed to set fan duty", "fan", fc.Name, "duty", targetDuty, "error", err)
		return
	}

	if shouldLog && targetDuty != state.lastDuty {
		d.logger.Info("fan duty adjusted",
			"fan", fc.Name,
			"temp_raw", temp,
			"temp_avg", fmt.Sprintf("%.1f", state.smoothedTemp),
			"duty", targetDuty,
		)
	}
	state.lastDuty = targetDuty
}

// Interpolate maps a temperature to a duty cycle using piecewise linear interpolation.
func Interpolate(temp float64, curve []CurvePoint) int {
	if len(curve) == 0 {
		return 0
	}

	// Below the first point.
	if temp <= float64(curve[0].TempC) {
		return curve[0].DutyPercent
	}

	// Above the last point.
	if temp >= float64(curve[len(curve)-1].TempC) {
		return curve[len(curve)-1].DutyPercent
	}

	// Find the segment and interpolate.
	for i := 0; i < len(curve)-1; i++ {
		t0 := float64(curve[i].TempC)
		t1 := float64(curve[i+1].TempC)

		if temp >= t0 && temp <= t1 {
			d0 := float64(curve[i].DutyPercent)
			d1 := float64(curve[i+1].DutyPercent)
			ratio := (temp - t0) / (t1 - t0)
			return int(d0 + ratio*(d1-d0))
		}
	}

	return curve[len(curve)-1].DutyPercent
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
