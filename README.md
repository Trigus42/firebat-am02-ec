# firebat-am02-ec

Go library and CLI for controlling the Embedded Controller on the **FIREBAT AM02** mini PC (board: ARB19L-F).

Provides direct fan speed control, temperature monitoring, and a fan curve daemon — all via the EC's IO port interface, no kernel driver required.

## Supported Hardware

| Field | Value |
|-------|-------|
| Product | FIREBAT AM02 |
| Board | ARB19L-F |
| EC access | IO ports 0x62/0x66 (ACPI standard) |
| OS | Any Linux (tested on Talos Linux / Kubernetes) |

## Requirements

- Privileged access to `/dev/port` (root, or a privileged container)
- Go 1.24+ (for building)

## Installation

```bash
go install github.com/Trigus42/firebat-am02-ec/cmd/firebat-ec@latest
```

## CLI Usage

```bash
# Read temperatures
firebat-ec temp
# CPU: 47°C
# GPU: 45°C

# Check fan status
firebat-ec fan status
# Mode: automatic (EC controlled)

# Set fan to 60% manual
firebat-ec fan set 60
# Fan set to 60% (manual mode)

# Return to automatic
firebat-ec fan auto

# Run fan curve daemon (default curve)
firebat-ec daemon

# Run with custom config
firebat-ec daemon -config /etc/fan-control/config.json
```

## Library Usage

```go
package main

import (
    "fmt"
    "log"

    "github.com/Trigus42/firebat-am02-ec/pkg/device"
    "github.com/Trigus42/firebat-am02-ec/pkg/ec"
)

func main() {
    controller, err := ec.Open()
    if err != nil {
        log.Fatal(err)
    }
    defer controller.Close()

    // Read CPU temperature
    sensor := device.NewCPUTempSensor(controller)
    temp, _ := sensor.ReadCelsius()
    fmt.Printf("CPU: %d°C\n", temp)

    // Set fan to 40%
    fan := device.NewFan1(controller)
    fan.SetDuty(40)

    // Restore automatic control
    fan.SetAuto()
}
```

## Daemon Configuration

```json
{
  "fan_curve": [
    {"temp_c": 0,  "duty_percent": 0},
    {"temp_c": 40, "duty_percent": 0},
    {"temp_c": 50, "duty_percent": 20},
    {"temp_c": 60, "duty_percent": 40},
    {"temp_c": 70, "duty_percent": 60},
    {"temp_c": 80, "duty_percent": 80},
    {"temp_c": 90, "duty_percent": 100}
  ],
  "interval_seconds": 5,
  "hysteresis_degrees": 2,
  "smoothing_factor": 0.3
}
```

## How It Works

The FIREBAT AM02's fan is controlled by an Embedded Controller (EC) accessible through standard ACPI IO ports. This library speaks the EC command protocol directly:

1. Send read/write command to port `0x66`
2. Send register address to port `0x62`
3. Read/write data on port `0x62`

The register map was reverse-engineered from the board's ACPI DSDT table. See [docs/reverse-engineering.md](docs/reverse-engineering.md) for the full methodology.

### Key EC Registers

| Offset | Name | Description |
|--------|------|-------------|
| 0x33 | FAN1 | Fan 1 duty (0x00=auto, 0x80\|N=manual N%) |
| 0x34 | FAN2 | Fan 2 duty (same encoding) |
| 0x70 | CPUT | CPU temperature (°C) |
| 0x71 | GPUT | GPU temperature (°C) |

## Safety

- The daemon **always restores automatic fan control** on shutdown (SIGTERM/SIGINT)
- Writing `0x00` to the fan register returns control to the EC's built-in thermal table
- The EC has its own critical thermal shutdown at ~105°C regardless of fan register state

## Project Structure

```
cmd/firebat-ec/     CLI tool
pkg/ec/             Low-level EC IO port access and protocol
pkg/register/       EC register map constants (board-specific)
pkg/device/         High-level device abstractions (fan, thermal)
pkg/fancontrol/     Fan curve daemon logic
docs/               Methodology documentation
```

## License

MIT
