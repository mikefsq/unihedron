//go:build !darwin

package unihedron

import (
	"fmt"
	"strings"

	"go.bug.st/serial/enumerator"
)

// vidFTDI is the SQM-LU's FTDI bridge USB vendor ID, as the enumerator reports it
// (a hex string; matched case-insensitively).
const vidFTDI = "0403"

// enumeratePorts lists FTDI (VID 0x0403) USB serial ports via go.bug.st/serial's
// enumerator, which is pure Go on every non-darwin OS and reports USB VID/PID.
func enumeratePorts() ([]DeviceInfo, error) {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, fmt.Errorf("unihedron: enumerate ports: %w", err)
	}
	var out []DeviceInfo
	for _, p := range ports {
		if p.IsUSB && strings.EqualFold(p.VID, vidFTDI) {
			// Capture the USB serial / product from the enumerator — read here, before
			// the port is opened, so callers can pick a specific unit without I/O.
			out = append(out, DeviceInfo{Port: p.Name, Serial: p.SerialNumber, Product: p.Product})
		}
	}
	return out, nil
}
