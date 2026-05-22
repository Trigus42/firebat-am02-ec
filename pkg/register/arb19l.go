// Package register defines the EC register map for the FIREBAT AM02 (ARB19L-F board).
//
// These offsets were reverse-engineered from the board's ACPI DSDT table.
// The DSDT defines an OperationRegion "ERAX" at physical address 0xFE0B0400
// with these fields. The same registers are accessible via the standard ACPI EC
// protocol on IO ports 0x62/0x66.
//
// # How this was discovered
//
// See docs/reverse-engineering.md for the full methodology. In brief:
//  1. Dump DSDT: cat /sys/firmware/acpi/tables/DSDT > dsdt.dat
//  2. Decompile: iasl -d dsdt.dat
//  3. Find EC device (PNP0C09) and its OperationRegion/Field definitions
//  4. Cross-reference with ACPI methods that read/write fan and thermal registers
//  5. Validate by reading registers via /dev/port and matching expected temps
package register

// Fan control registers.
//
// Writing 0x00 returns the fan to automatic EC control.
// Writing (0x80 | duty) sets manual mode where duty is 0–100 (percent).
const (
	FAN1 = 0x33 // Fan 1 duty cycle control.
	FAN2 = 0x34 // Fan 2 duty cycle control.
)

// Fan tachometer registers (read-only, 16-bit little-endian).
// RPM can be estimated as: RPM = divisor / (FNxH<<8 | FNxL).
// A value of 0 typically means no tachometer is connected.
const (
	FN1L = 0x35 // Fan 1 tachometer count, low byte.
	FN1H = 0x36 // Fan 1 tachometer count, high byte.
	FN2L = 0x37 // Fan 2 tachometer count, low byte.
	FN2H = 0x38 // Fan 2 tachometer count, high byte.
)

// Fan mode registers.
const (
	FCMO = 0x31 // Fan control mode output (current mode reported by EC).
	FCMI = 0x32 // Fan control mode input (write to change EC fan profile).
)

// Fan mode input values (written to FCMI).
const (
	FCMIPerformanceFan1 = 0x81 // Set fan 1 to performance profile.
	FCMIPerformanceFan2 = 0x82 // Set fan 2 to performance profile.
)

// Fan duty encoding.
const (
	FanManualBit = 0x80 // Bit 7: set to enable manual duty control.
	FanDutyMask  = 0x7F // Bits 6:0: duty cycle percentage (0–100).
	FanMaxDuty   = 100  // Maximum valid duty cycle percentage.
)

// Temperature registers (read-only, degrees Celsius).
const (
	CPUT = 0x70 // CPU temperature.
	GPUT = 0x71 // GPU/iGPU temperature.
	CPAD = 0x72 // Additional thermal reading (package/ambient).
)

// System state registers.
const (
	OSFG = 0x04 // OS flag.
	CTSD = 0x0E // Critical thermal shutdown (1 = shutdown triggered).
	ECOK = 0x00 // EC ready flag.
)

// Keyboard/LED registers.
const (
	KBFlags    = 0x39 // Keyboard backlight flags (bit field).
	KBLight    = 0x3A // Keyboard backlight enable (bit 0 = on/off).
	KBColorHi  = 0x3C // LED color high byte.
	KBColorLo  = 0x3D // LED color low byte.
)

// Display port detection bits (register 0x56, individual bits).
const (
	DisplayPorts = 0x56 // Bit field: DP10, DP15, DP25, DP35, DP45, DP54, DP20.
)

// Power state.
const (
	ACType = 0x4A // AC adapter presence (bit 0).
)

// Board identification.
const (
	BoardName         = "ARB19L-F"
	BoardManufacturer = "Firebat Computer"
	ProductName       = "AM02"
)
