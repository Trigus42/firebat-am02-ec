# Reverse Engineering EC Fan Control

This document explains how the EC register map for the FIREBAT AM02 (ARB19L-F) was
discovered. The same methodology applies to most x86 systems with ACPI Embedded
Controllers.

## Background Concepts

### What is an Embedded Controller (EC)?

A small microcontroller soldered on the motherboard, separate from the main CPU.
It manages:
- Fan speed (PWM signals)
- Keyboard backlight / LEDs
- Power button behavior
- Battery charging (on laptops)
- Thermal shutdown

### What is ACPI?

Advanced Configuration and Power Interface — a specification defining how the OS
discovers and manages hardware. The BIOS/UEFI places **tables** in memory at boot
that describe devices, their IO ports, and methods to interact with them.

### What is the DSDT?

The Differentiated System Description Table — the main ACPI table. It's a program
written in AML bytecode (compiled from ASL source) that describes:
- What devices exist
- Which IO ports/memory they use
- Methods (routines) the OS can invoke (e.g., "set fan speed")

### IO Ports

On x86, there are two address spaces:
- **Memory space** — normal RAM addresses
- **IO space** — a separate 64KB space (0x0000–0xFFFF) accessed via `in`/`out` CPU instructions

The EC communicates through IO ports. The standard ACPI EC sits at:
- **Port 0x62** — data register
- **Port 0x66** — command/status register

These are hardwired on the LPC/eSPI bus and mandated by the ACPI specification.

## Step-by-Step Methodology

### 1. Get a Privileged Shell

You need raw access to IO ports and firmware tables. Options:
- Root shell on the host
- Privileged container with `/dev` mounted
- A VM with hardware passthrough

Verify access:
```bash
ls /dev/port               # IO port access
ls /sys/firmware/acpi/tables/DSDT  # ACPI tables
```

### 2. Dump and Decompile the DSDT

```bash
# Copy the raw binary table
cat /sys/firmware/acpi/tables/DSDT > /tmp/dsdt.dat

# Install the ACPI decompiler
apt install -y acpica-tools

# Decompile AML bytecode to human-readable ASL
iasl -d /tmp/dsdt.dat
# Produces: /tmp/dsdt.dsl
```

### 3. Find the EC Device

Search for the Embedded Controller's hardware ID:
```bash
grep -n 'PNP0C09' /tmp/dsdt.dsl
```

This finds the `Device (EC0)` block. Look for its `_CRS` method to confirm IO ports:
```asl
IO (Decode16, 0x0062, 0x0062, 0x01, 0x01)  // Data port
IO (Decode16, 0x0066, 0x0066, 0x01, 0x01)  // Command port
```

### 4. Find the Register Map

Within the EC device, look for `OperationRegion` and `Field` definitions:
```bash
grep -A200 'OperationRegion.*EC\|OperationRegion.*ERA' /tmp/dsdt.dsl
```

This reveals the register layout:
```asl
OperationRegion (ERAX, SystemMemory, 0xFE0B0400, 0xFF)
Field (ERAX, ByteAcc, Lock, Preserve)
{
    Offset (0x33),
    FAN1, 8,        // Fan 1 duty at offset 0x33
    Offset (0x34),
    FAN2, 8,        // Fan 2 duty at offset 0x34
    Offset (0x70),
    CPUT, 8,        // CPU temperature at offset 0x70
}
```

Even though the OperationRegion says `SystemMemory`, these same registers are
accessible via the EC command protocol (ports 0x62/0x66) using the same offsets.

### 5. Understand the Encoding

Search for methods that write to fan registers:
```bash
grep -B5 -A15 'FAN1\|FAN2\|FCMI' /tmp/dsdt.dsl
```

On the AM02, this revealed:
```asl
// Manual fan control: set bit 7 + duty percentage
EC0.FAN1 = (0x80 | FADC)  // FADC = duty 0–101
```

So the encoding is:
- **0x00** = automatic mode (EC controls fan)
- **0x80 | N** = manual mode at N% duty (N = 0–100)

### 6. Validate with Direct Reads

Use Python (or any language with raw IO access) to confirm:
```python
import os

fd = os.open('/dev/port', os.O_RDWR)

def outb(port, val):
    os.lseek(fd, port, os.SEEK_SET)
    os.write(fd, bytes([val]))

def inb(port):
    os.lseek(fd, port, os.SEEK_SET)
    return os.read(fd, 1)[0]

def ec_read(reg):
    # Wait for IBF clear
    while inb(0x66) & 0x02: pass
    outb(0x66, 0x80)          # Read command
    while inb(0x66) & 0x02: pass
    outb(0x62, reg)           # Register address
    while not (inb(0x66) & 0x01): pass
    return inb(0x62)          # Read result

# Should match CPU temp from lm-sensors
print(f"CPU temp: {ec_read(0x70)}°C")
```

### 7. Test Writes Carefully

Start with a safe value (moderate fan speed), verify the register holds it,
and check that the hardware responds:
```python
def ec_write(reg, val):
    while inb(0x66) & 0x02: pass
    outb(0x66, 0x81)          # Write command
    while inb(0x66) & 0x02: pass
    outb(0x62, reg)           # Register address
    while inb(0x66) & 0x02: pass
    outb(0x62, val)           # Value

# Set fan to 50% manual
ec_write(0x33, 0x80 | 50)

# Restore automatic control
ec_write(0x33, 0x00)
```

### 8. Monitor Dynamic Registers

To discover temperature/tachometer registers without DSDT hints, dump all 256
registers twice with a delay and look for values that change:
```python
import time

snap1 = [ec_read(i) for i in range(256)]
time.sleep(2)
snap2 = [ec_read(i) for i in range(256)]

for i in range(256):
    if snap1[i] != snap2[i] or snap1[i] != 0:
        print(f"EC[0x{i:02X}] = {snap1[i]} -> {snap2[i]}")
```

Registers showing values in the 30–90 range that correlate with known temperature
sensors are likely thermal readings.

## Tips

- **Always restore auto mode** (write 0x00) when done testing. Leaving the fan
  at a low duty while the CPU is loaded risks thermal shutdown.
- **DSDT methods reveal encoding** — don't guess register formats when you can
  read how the BIOS itself uses them.
- **SystemMemory OperationRegions** might not be accessible via `/dev/mem` if the
  kernel has `CONFIG_STRICT_DEVMEM` enabled. The EC port interface (0x62/0x66)
  usually works regardless.
- **WMI methods** (`_WMI`, `WMIB`, `WMAA`) wrap EC access for Windows utilities.
  They show which registers the vendor's own tools use.
- **Be careful with registers you don't understand** — writing to power management
  or shutdown registers could brick or immediately power off the system.
