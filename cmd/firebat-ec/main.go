// Command firebat-ec provides CLI access to the FIREBAT AM02 Embedded Controller.
//
// Usage:
//
//	firebat-ec <command> [flags]
//
// Commands:
//
//	temp         Print CPU and GPU temperatures
//	fan status   Show current fan mode and duty cycle
//	fan set <N>  Set fan to manual mode at N% duty (0–100)
//	fan auto     Return fan to automatic EC control
//	daemon       Run the fan curve control loop
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
			fmt.Fprintf(os.Stderr, "Usage: firebat-ec fan <status|set|auto>\n")
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
  firebat-ec temp              Print CPU and GPU temperatures
  firebat-ec fan status        Show current fan mode and duty cycle
  firebat-ec fan set <percent> Set fan to manual mode (0–100%%)
  firebat-ec fan auto          Return fan to automatic EC control
  firebat-ec daemon [flags]    Run the fan curve control daemon

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

	fan := device.NewFan1(controller)

	switch subcmd {
	case "status":
		status, err := fan.Status()
		if err != nil {
			fatal(err)
		}
		if status.ManualMode {
			fmt.Printf("Mode: manual\nDuty: %d%%\n", status.DutyPercent)
		} else {
			fmt.Printf("Mode: automatic (EC controlled)\n")
		}

	case "set":
		if len(args) < 1 {
			fmt.Fprintf(os.Stderr, "Usage: firebat-ec fan set <percent>\n")
			os.Exit(1)
		}
		percent, err := strconv.Atoi(args[0])
		if err != nil || percent < 0 || percent > 100 {
			fmt.Fprintf(os.Stderr, "Invalid duty: %q (must be 0–100)\n", args[0])
			os.Exit(1)
		}
		if err := fan.SetDuty(percent); err != nil {
			fatal(err)
		}
		fmt.Printf("Fan set to %d%% (manual mode)\n", percent)

	case "auto":
		if err := fan.SetAuto(); err != nil {
			fatal(err)
		}
		fmt.Println("Fan returned to automatic mode")

	default:
		fmt.Fprintf(os.Stderr, "Unknown fan subcommand: %s\n", subcmd)
		os.Exit(1)
	}
}

// daemonConfig is the JSON configuration file format.
type daemonConfig struct {
	Curve             []curvePointJSON `json:"fan_curve"`
	IntervalSeconds   int              `json:"interval_seconds"`
	HysteresisDegrees int              `json:"hysteresis_degrees"`
	SmoothingFactor   float64          `json:"smoothing_factor"`
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

	cfg := fancontrol.DefaultConfig()

	if *configPath != "" {
		fileCfg, err := loadDaemonConfig(*configPath)
		if err != nil {
			fatal(fmt.Errorf("load config: %w", err))
		}
		cfg = fileCfg
	}

	if *interval > 0 {
		cfg.Interval = *interval
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	controller, err := ec.Open()
	if err != nil {
		fatal(err)
	}
	defer controller.Close()

	fan := device.NewFan1(controller)
	sensor := device.NewCPUTempSensor(controller)

	daemon := fancontrol.NewDaemon(fan, sensor, cfg, logger)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	if err := daemon.Run(ctx); err != nil && err != context.Canceled {
		fatal(err)
	}
}

func loadDaemonConfig(path string) (fancontrol.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return fancontrol.Config{}, err
	}

	var raw daemonConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return fancontrol.Config{}, fmt.Errorf("parse JSON: %w", err)
	}

	cfg := fancontrol.Config{
		HysteresisDegrees: raw.HysteresisDegrees,
		SmoothingFactor:   raw.SmoothingFactor,
	}

	if raw.IntervalSeconds > 0 {
		cfg.Interval = time.Duration(raw.IntervalSeconds) * time.Second
	} else {
		cfg.Interval = 5 * time.Second
	}

	if raw.SmoothingFactor == 0 {
		cfg.SmoothingFactor = 0.3
	}

	for _, p := range raw.Curve {
		cfg.Curve = append(cfg.Curve, fancontrol.CurvePoint{
			TempC:       p.TempC,
			DutyPercent: p.DutyPercent,
		})
	}

	return cfg, nil
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}
