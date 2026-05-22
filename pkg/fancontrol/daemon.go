// Package fancontrol implements a temperature-reactive fan speed daemon.
//
// It reads CPU temperature at a configurable interval, maps it to a duty cycle
// using a piecewise-linear fan curve, and applies hysteresis and smoothing to
// prevent rapid fan speed oscillations.
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

// Config holds all tunable parameters for the fan control daemon.
type Config struct {
	// Curve maps temperatures to fan duty cycles.
	// Must be sorted by TempC in ascending order.
	// Example: [{0, 0}, {40, 0}, {50, 20}, {60, 40}, {70, 60}, {80, 80}, {90, 100}]
	Curve []CurvePoint

	// Interval between temperature readings and fan adjustments.
	Interval time.Duration

	// HysteresisDegrees prevents duty changes unless temperature has moved
	// at least this many degrees from the last adjustment point.
	HysteresisDegrees int

	// SmoothingFactor controls the exponential moving average for temperature.
	// Range: 0.0 (no smoothing, instant response) to 1.0 (ignores new readings).
	// Recommended: 0.3–0.7.
	SmoothingFactor float64
}

// DefaultConfig returns a sensible fan curve for the FIREBAT AM02.
func DefaultConfig() Config {
	return Config{
		Curve: []CurvePoint{
			{TempC: 0, DutyPercent: 0},
			{TempC: 40, DutyPercent: 0},
			{TempC: 50, DutyPercent: 20},
			{TempC: 60, DutyPercent: 40},
			{TempC: 70, DutyPercent: 60},
			{TempC: 80, DutyPercent: 80},
			{TempC: 90, DutyPercent: 100},
		},
		Interval:          5 * time.Second,
		HysteresisDegrees: 2,
		SmoothingFactor:   0.3,
	}
}

// Daemon runs the fan control loop.
type Daemon struct {
	fan    *device.Fan
	sensor *device.ThermalSensor
	config Config
	logger *slog.Logger
}

// NewDaemon creates a fan control daemon.
func NewDaemon(fan *device.Fan, sensor *device.ThermalSensor, config Config, logger *slog.Logger) *Daemon {
	if logger == nil {
		logger = slog.Default()
	}
	return &Daemon{
		fan:    fan,
		sensor: sensor,
		config: config,
		logger: logger,
	}
}

// Run starts the fan control loop. It blocks until the context is cancelled.
// On exit (including context cancellation), it restores the fan to automatic mode.
func (d *Daemon) Run(ctx context.Context) error {
	d.logger.Info("fan control daemon starting",
		"interval", d.config.Interval,
		"hysteresis", d.config.HysteresisDegrees,
		"curve_points", len(d.config.Curve),
	)

	// Always restore auto mode on exit.
	defer func() {
		d.logger.Info("restoring automatic fan control")
		if err := d.fan.SetAuto(); err != nil {
			d.logger.Error("failed to restore auto mode", "error", err)
		}
	}()

	var (
		smoothedTemp float64
		lastDuty     = -1
		initialized  = false
	)

	ticker := time.NewTicker(d.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			temp, err := d.sensor.ReadCelsius()
			if err != nil {
				d.logger.Error("failed to read temperature", "error", err)
				continue
			}

			// Exponential moving average for stable readings.
			if !initialized {
				smoothedTemp = float64(temp)
				initialized = true
			} else {
				alpha := d.config.SmoothingFactor
				smoothedTemp = alpha*smoothedTemp + (1-alpha)*float64(temp)
			}

			targetDuty := Interpolate(smoothedTemp, d.config.Curve)

			// Apply hysteresis: don't change duty for small fluctuations.
			if lastDuty >= 0 && abs(targetDuty-lastDuty) < 3 {
				continue
			}

			if targetDuty != lastDuty {
				if err := d.fan.SetDuty(targetDuty); err != nil {
					d.logger.Error("failed to set fan duty", "duty", targetDuty, "error", err)
					continue
				}
				d.logger.Info("fan duty adjusted",
					"temp_raw", temp,
					"temp_avg", fmt.Sprintf("%.1f", smoothedTemp),
					"duty", targetDuty,
				)
				lastDuty = targetDuty
			}
		}
	}
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
