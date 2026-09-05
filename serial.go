package unihedron

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	bugst "go.bug.st/serial"
)

// readTimeout is the per-read timeout set on the port. With it, Read returns
// promptly when bytes arrive and returns (0, nil) once idle past the timeout,
// which command()'s deadline loop handles.
const readTimeout = 100 * time.Millisecond

// probeTimeout bounds the ix identification probe per port during discovery. A real SQM
// answers ix from stored state in well under a second.
const probeTimeout = 2 * time.Second

// openPort opens the serial port at 115200 baud, 8N1, and drains startup noise.
func openPort(dev string) (Transport, DeviceInfo, error) {
	port, err := bugst.Open(dev, &bugst.Mode{
		BaudRate: Baud,
		DataBits: 8,
		Parity:   bugst.NoParity,
		StopBits: bugst.OneStopBit,
	})
	if err != nil {
		return nil, DeviceInfo{}, fmt.Errorf("unihedron: open %s: %w", dev, err)
	}
	if err := port.SetReadTimeout(readTimeout); err != nil {
		port.Close()
		return nil, DeviceInfo{}, fmt.Errorf("unihedron: set read timeout on %s: %w", dev, err)
	}
	// Give the FTDI bridge a moment to settle, then discard its power-on noise.
	time.Sleep(50 * time.Millisecond)
	_ = port.ResetInputBuffer()
	return port, DeviceInfo{Port: dev}, nil
}

// Enumerate lists candidate FTDI ports without opening them. Discover identifies SQMs.
func Enumerate() ([]DeviceInfo, error) { return enumeratePorts() }

// Discovered is a Unihedron identified by probing an FTDI serial port: the enumerated
// port and USB-bridge descriptor (DeviceInfo), plus the SQM's own identity from ix
// (Unit.Serial is the meter's serial number, distinct from the FTDI bridge's Serial).
type Discovered struct {
	DeviceInfo
	Unit UnitInfo
}

// Discover probes candidate ports with ix and returns those identifying as SQMs.
// Busy ports and nonmatching replies are skipped.
func Discover() ([]Discovered, error) {
	ports, err := enumeratePorts()
	if err != nil {
		return nil, err
	}
	var out []Discovered
	for _, d := range ports {
		info, err := probe(d.Port)
		if err != nil {
			continue // busy, not an SQM, or no reply within probeTimeout
		}
		out = append(out, Discovered{DeviceInfo: d, Unit: info})
	}
	return out, nil
}

// probe opens port, asks ix with the short probe timeout, and closes it, returning the
// unit identity iff the device answers as a Unihedron. An open error (e.g. busy) or a
// non-SQM reply/timeout is returned as an error so Discover can skip the port.
func probe(port string) (UnitInfo, error) {
	t, _, err := openPort(port)
	if err != nil {
		return UnitInfo{}, err
	}
	s := New(t, DeviceInfo{Port: port})
	s.timeout = probeTimeout
	defer s.Close()
	return s.UnitInfo()
}

// openFirst probes the attached FTDI ports and opens the first one that identifies as a
// Unihedron SQM — so a bare open works even when other FTDI devices are present.
func openFirst() (Transport, DeviceInfo, error) {
	found, err := Discover()
	if err != nil {
		return nil, DeviceInfo{}, err
	}
	if len(found) == 0 {
		return nil, DeviceInfo{}, errors.New("unihedron: no SQM found")
	}
	return openInfo(found[0].DeviceInfo)
}

// openInfo opens the port named by an enumerated DeviceInfo and returns that same info
// (so the USB serial/product captured during enumeration is preserved on the handle).
func openInfo(d DeviceInfo) (Transport, DeviceInfo, error) {
	t, _, err := openPort(d.Port)
	if err != nil {
		return nil, DeviceInfo{}, err
	}
	return t, d, nil
}

// openBySerial matches either the USB bridge serial or the meter’s unit serial.
// Matching ignores case, surrounding whitespace, and unit-serial zero padding.
func openBySerial(serial string) (Transport, DeviceInfo, error) {
	want := strings.TrimSpace(strings.ToLower(serial))
	if want == "" {
		return nil, DeviceInfo{}, errors.New("unihedron: empty serial")
	}
	ports, err := enumeratePorts()
	if err != nil {
		return nil, DeviceInfo{}, err
	}
	// Fast path: match the FTDI USB-bridge serial from the descriptor (no open needed).
	for _, d := range ports {
		if strings.ToLower(strings.TrimSpace(d.Serial)) == want {
			return openInfo(d)
		}
	}
	// Fall back: probe each port and match the SQM's own unit serial number.
	for _, d := range ports {
		info, err := probe(d.Port)
		if err != nil {
			continue
		}
		if want == strconv.Itoa(info.Serial) || want == fmt.Sprintf("%08d", info.Serial) {
			return openInfo(d)
		}
	}
	return nil, DeviceInfo{}, fmt.Errorf("unihedron: no SQM with serial %q found", serial)
}
