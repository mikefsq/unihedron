//go:build darwin

package unihedron

import (
	"fmt"
	"strings"

	bugst "go.bug.st/serial"
)

// enumeratePorts lists the FTDI VCP ports on macOS by the device-name convention
// (/dev/cu.usbserial-*). Reading the USB VID on macOS would require the enumerator's
// cgo (IOKit) path, which has no CGO_ENABLED=0 fallback and so would break
// cross-compilation to darwin; GetPortsList is pure Go, so discovery here is
// name-based, matching the FTDI virtual COM port (/dev/cu.usbserial-*).
func enumeratePorts() ([]DeviceInfo, error) {
	names, err := bugst.GetPortsList()
	if err != nil {
		return nil, fmt.Errorf("unihedron: list ports: %w", err)
	}
	var out []DeviceInfo
	for _, n := range names {
		if strings.HasPrefix(n, "/dev/cu.") && strings.Contains(n, "usbserial") {
			// macOS names the FTDI node "/dev/cu.usbserial-<SERIAL>", so the USB serial
			// is recoverable from the name without the enumerator's cgo (IOKit) path.
			serial := ""
			if i := strings.Index(n, "usbserial-"); i >= 0 {
				serial = n[i+len("usbserial-"):]
			}
			out = append(out, DeviceInfo{Port: n, Serial: serial})
		}
	}
	return out, nil
}
