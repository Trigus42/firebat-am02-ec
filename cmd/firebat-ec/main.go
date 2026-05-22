// Command firebat-ec provides CLI access to the FIREBAT AM02 Embedded Controller.
//
// Usage:
//
//	firebat-ec <command> [flags]
//
// Commands:
//
//	temp              Print CPU and GPU temperatures
//	fan status [1|2]  Show current fan mode and duty cycle
//	fan set [1|2] <N> Set fan to manual mode at N% duty (0–100)
//	fan auto [1|2]    Return fan to automatic EC control
//	daemon            Run the fan curve control loop
//
// The daemon command accepts an optional -config flag pointing to a JSON file.
// Without it, the built-in default curve is used.
//
// Requires privileged access to /dev/port (run as root or in a privileged container).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Trigus42/firebat-am02-ec/pkg/device"
	"github.com/Trigus42/firebat-am02-ec/pkg/ec"
	"github.com/Trigus42/firebat-am02-ec/pkg/fancontrol"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "temp":
		runTemp()
	case "fan":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "Usage: firebat-ec fan <status|set|auto> [1|2]\n")
			os.Exit(1)
		}
		runFan(os.Args[2], os.Args[3:])
	case "daemon":
		runDaemon(os.Args[2:])
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `firebat-ec — FIREBAT AM02 Embedded Controller tool

Usage:
  firebat-ec temp                Print CPU and GPU temperatures
  firebat-ec fan status [1|2]    Show current fan mode and duty cycle
  firebat-ec fan set [1|2] <N>   Set fan to manual mode (0–100%%)
  firebat-ec fan auto [1|2]      Return fan to automatic EC control
  firebat-ec daemon [flags]      Run the fan curve control daemon

Fan number is optional; omit to control both fans.

Daemon flags:
  -config <path>    JSON config file (optional, uses defaults if omitted)
  -interval <dur>   Override polling interval (e.g. "5s", "2s")

Requires privileged access to /dev/port.
`)
}

// runTemp prints current CPU and GPU temperatures.
func runTemp() {
	controller, err := ec.Open()
	if err != nil {
		fatal(err)
	}
	defer controller.Close()

	cpu := device.NewCPUTempSensor(controller)
	gpu := device.NewGPUTempSensor(controller)

	cpuTemp, err := cpu.ReadCelsius()
	if err != nil {
		fatal(err)
	}

	gpuTemp, err := gpu.ReadCelsius()
	if err != nil {
		fatal(err)
	}

	fmt.Printf("CPU: %d°C\n", cpuTemp)
	fmt.Printf("GPU: %d°C\n", gpuTemp)
}

// runFan handles fan subcommands.
func runFan(subcmd string, args []string) {
	controller, err := ec.Open()
	if err != nil {
		fatal(err)
	}
	defer controller.Close()

	// Parse optional fan number from args.
	// Returns the selected fans and remaining args.
	fans, remaining := parseFanArgs(controller, args)

	switch subcmd {
	case "status":
		for _, f := range fans {
			status, err := f.fan.Status()
			if err != nil {
				fatal(err)
			}
			if len(fans) > 1 {
				fmt.Printf("Fan %d: ", f.id)
			}
			if status.ManualMode {
				fmt.Printf("Mode: manual, Duty: %d%%\n", status.DutyPercent)
			} else {
				fmt.Printf("Mode: automatic (EC controlled)\n")
			}
		}

	case "set":
		if len(remaining) < 1 {
			fmt.Fprintf(os.Stderr, "Usage: firebat-ec fan set [1|2] <percent>\n")
			os.Exit(1)
		}
		percent, err := strconv.Atoi(remaining[0])
		if err != nil || percent < 0 || percent > 100 {
			fmt.Fprintf(os.Stderr, "Invalid duty: %q (must be 0–100)\n", remaining[0])
			os.Exit(1)
		}
		for _, f := range fans {
			if err := f.fan.SetDuty(percent); err != nil {
				fatal(err)
			}
			fmt.Printf("Fan %d set to %d%% (manual mode)\n", f.id, percent)
		}

	case "auto":
		for _, f := range fans {
			if err := f.fan.SetAuto(); err != nil {
				fatal(err)
			}
			fmt.Printf("Fan %d returned to automatic mode\n", f.id)
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown fan subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

type fanRef struct {
	fan *device.Fan
	id  int
}

// parseFanArgs checks if the first arg is a fan number.
// If so, returns only that fan; otherwise returns all fans.
func parseFanArgs(controller *ec.EC, args []string) ([]fanRef, []string) {
	if len(args) > 0 {
		if id, err := strconv.Atoi(args[0]); err == nil {
			fan, ferr := device.NewFan(controller, id)
			if ferr != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", ferr)
				os.Exit(1)
			}
			return []fanRef{{fan: fan, id: id}}, args[1:]
		}
	}

	// No fan number specified — return all fans.
	var fans []fanRef
	for id := 1; id <= device.NumFans(); id++ {
		fan, _ := device.NewFan(controller, id)
		fans = append(fans, fanRef{fan: fan, id: id})
	}
	return fans, args
}

// daemonConfig is the JSON configuration file format.
type daemonConfig struct {
	// Legacy single-curve format (applies to all fans).
	Curve             []curvePointJSON `json:"fan_curve,omitempty"`
	IntervalSeconds   int              `json:"interval_seconds"`
	HysteresisDegrees int              `json:"hysteresis_degrees,omitempty"`
	SmoothingFactor   float64          `json:"smoothing_factor,omitempty"`

	// Per-fan configuration (takes precedence over top-level curve).
	Fans []fanConfigJSON `json:"fans,omitempty"`
}

type fanConfigJSON struct {
	Name              string           `json:"name"`
	Fan               int              `json:"fan"`    // 1 or 2
	Sensor            string           `json:"sensor"` // "cpu" or "gpu"
	Curve             []curvePointJSON `json:"fan_curve"`
	HysteresisDegrees *int             `json:"hysteresis_degrees,omitempty"`
	SmoothingFactor   *float64         `json:"smoothing_factor,omitempty"`
}

type curvePointJSON struct {
	TempC       int `json:"temp_c"`
	DutyPercent int `json:"duty_percent"`
}

// runDaemon starts the fan control loop.
func runDaemon(args []string) {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	configPath := fs.String("config", "", "path to JSON config file")
	interval := fs.Duration("interval", 0, "override polling interval")
	fs.Parse(args)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	controller, err := ec.Open()
	if err != nil {
		fatal(err)
	}
	defer controller.Close()

	cfg := defaultDaemonConfig(controller)

	if *configPath != "" {
		fileCfg, err := loadDaemonConfig(*configPath, controller)
		if err != nil {
			fatal(fmt.Errorf("load config: %w", err))
		}
		cfg = fileCfg
	}

	if *interval > 0 {
		cfg.Interval = *interval
	}

	daemon := fancontrol.NewDaemon(cfg, logger)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	if err := daemon.Run(ctx); err != nil && err != context.Canceled {
		fatal(err)
	}
}

// defaultDaemonConfig builds a Config that controls all known fans with the CPU sensor.
func defaultDaemonConfig(controller *ec.EC) fancontrol.Config {
	cfg := fancontrol.DefaultConfig()
	cpuSensor := device.NewCPUTempSensor(controller)
	for i := range cfg.Fans {
		fan, _ := device.NewFan(controller, i+1)
		cfg.Fans[i].Fan = fan
		cfg.Fans[i].Sensor = cpuSensor
	}
	return cfg
}

func loadDaemonConfig(path string, controller *ec.EC) (fancontrol.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return fancontrol.Config{}, err
	}

	var raw daemonConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return fancontrol.Config{}, fmt.Errorf("parse JSON: %w", err)
	}

	cfg := fancontrol.Config{}

	if raw.IntervalSeconds > 0 {
		cfg.Interval = time.Duration(raw.IntervalSeconds) * time.Second
	} else {
		cfg.Interval = 5 * time.Second
	}

	// Defaults for per-fan settings.
	defaultHysteresis := 2
	defaultSmoothing := 0.3
	if raw.HysteresisDegrees > 0 {
		defaultHysteresis = raw.HysteresisDegrees
	}
	if raw.SmoothingFactor > 0 {
		defaultSmoothing = raw.SmoothingFactor
	}

	if len(raw.Fans) > 0 {
		// Per-fan configuration.
		for _, fc := range raw.Fans {
			fan, ferr := device.NewFan(controller, fc.Fan)
			if ferr != nil {
				return fancontrol.Config{}, fmt.Errorf("fan config %q: %w", fc.Name, ferr)
			}
			sensor := resolveSensor(controller, fc.Sensor)

			hysteresis := defaultHysteresis
			if fc.HysteresisDegrees != nil {
				hysteresis = *fc.HysteresisDegrees
			}
			smoothing := defaultSmoothing
			if fc.SmoothingFactor != nil {
				smoothing = *fc.SmoothingFactor
			}

			var curve []fancontrol.CurvePoint
			for _, p := range fc.Curve {
				curve = append(curve, fancontrol.CurvePoint{
					TempC:       p.TempC,
					DutyPercent: p.DutyPercent,
				})
			}

			name := fc.Name
			if name == "" {
				name = fmt.Sprintf("fan%d", fc.Fan)
			}

			cfg.Fans = append(cfg.Fans, fancontrol.FanConfig{
				Name:              name,
				Fan:               fan,
				Sensor:            sensor,
				Curve:             curve,
				HysteresisDegrees: hysteresis,
				SmoothingFactor:   smoothing,
			})
		}
	} else {
		// Legacy single-curve format: apply to all fans.
		var curve []fancontrol.CurvePoint
		for _, p := range raw.Curve {
			curve = append(curve, fancontrol.CurvePoint{
				TempC:       p.TempC,
				DutyPercent: p.DutyPercent,
			})
		}

		cpuSensor := device.NewCPUTempSensor(controller)
		for id := 1; id <= device.NumFans(); id++ {
			fan, _ := device.NewFan(controller, id)
			cfg.Fans = append(cfg.Fans, fancontrol.FanConfig{
				Name:              fmt.Sprintf("fan%d", id),
				Fan:               fan,
				Sensor:            cpuSensor,
				Curve:             curve,
				HysteresisDegrees: defaultHysteresis,
				SmoothingFactor:   defaultSmoothing,
			})
		}
	}

	return cfg, nil
}

func resolveSensor(controller *ec.EC, name string) *device.ThermalSensor {
	switch name {
	case "gpu":
		return device.NewGPUTempSensor(controller)
	default:
		return device.NewCPUTempSensor(controller)
	}
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}
