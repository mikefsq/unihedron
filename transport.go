// Package unihedron reads Sky Quality Meters using the SQM-LU serial protocol.
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

// DeviceInfo contains serial-port discovery metadata.
// Serial identifies the USB bridge, not a protocol-level device serial.
type DeviceInfo struct {
	Port    string // e.g. /dev/cu.usbserial-XXXX, /dev/ttyUSB0, COM3
	Serial  string // USB iSerialNumber (from the enumerator); "" if unavailable
	Product string // USB iProduct string (from the enumerator); "" if unavailable
}
