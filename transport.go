// Package unihedron is a pure-Go driver for Unihedron Sky Quality Meter (SQM)
// devices over their FTDI USB-serial link: the SQM-LU and (over the same ASCII
// protocol) the SQM-LU-DL and SQM-LE. Like the Pegasus focusers — and unlike the
// ZWO/Astroasis USB-HID accessories — the SQM-LU presents itself as an FTDI virtual
// COM port (an OS serial device), and the protocol is plain ASCII: single-character
// command types (e.g. "rx", "ix", "cx"), each reply terminated with "\r\n".
//
// The command set is Unihedron's published SQM-LU serial protocol (section 8,
// "Commands and responses", of the SQM-LU Operator's Manual). The transport opens the
// FTDI virtual COM port at 115200 8N1 and does line I/O via go.bug.st/serial — no
// vendor library, no raw-USB FTDI reimplementation. It uses only the library's pure-Go
// paths (port I/O everywhere; the USB-VID enumerator off macOS, device-name matching on
// macOS), so it builds for any target with CGO_ENABLED=0.
package unihedron

// FTDI vendor ID (the SQM-LU's USB-serial bridge) and the SQM-LU line speed.
const (
	VID  uint16 = 0x0403
	Baud        = 115200
)

// Transport is a byte-level serial channel to the FTDI VCP port (satisfied by a
// go.bug.st/serial port); the device logic frames ASCII commands over it. Read
// should block up to a short timeout and return 0 bytes (not an error) when
// nothing is available, so command() can poll to a deadline.
type Transport interface {
	Write(p []byte) (int, error)
	Read(p []byte) (int, error)
	Close() error
}

// DeviceInfo identifies an opened serial port plus the USB-descriptor properties the
// enumerator reports for it before the port is opened. Serial is the FTDI bridge's USB
// iSerialNumber — a stable per-unit identity that disambiguates several FTDI devices
// sharing VID 0x0403 and survives replug / port renumbering. Note this is the USB
// bridge's serial, not the SQM's own serial number (which UnitInfo returns).
type DeviceInfo struct {
	Port    string // e.g. /dev/cu.usbserial-XXXX, /dev/ttyUSB0, COM3
	Serial  string // USB iSerialNumber (from the enumerator); "" if unavailable
	Product string // USB iProduct string (from the enumerator); "" if unavailable
}
