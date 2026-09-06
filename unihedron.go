package unihedron

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// replyTimeout bounds how long command() waits for a "\r\n"-terminated reply. This
// matches the vendor UDM's SendGet default (header_utils.pas, Timeout=3000), which it
// applies to readings as well as identity queries. A var so tests can shrink it.
var replyTimeout = 3 * time.Second

// maxDrainReads bounds drain()'s read loop so a unit streaming timed-interval reports
// (firmware Feature ≥13) cannot hold the drain open indefinitely.
const maxDrainReads = 16

// flusher is implemented by transports that can discard already-buffered input — a
// go.bug.st/serial port does. It is the counterpart of the vendor's ser.Purge.
type flusher interface{ ResetInputBuffer() error }

// drain flushes buffered input and reads any remaining bytes before issuing a command.
func (s *SQM) drain() {
	if f, ok := s.t.(flusher); ok {
		_ = f.ResetInputBuffer()
	}
	buf := make([]byte, 128)
	for i := 0; i < maxDrainReads; i++ {
		n, err := s.t.Read(buf)
		if err != nil || n == 0 {
			return
		}
	}
}

// SQM is an opened Unihedron Sky Quality Meter.
type SQM struct {
	t    Transport
	info DeviceInfo

	mu sync.Mutex // serializes command/response transactions

	// timeout overrides the package reply timeout for this handle when > 0. Discovery
	// sets a short value so probing a non-SQM FTDI port fails fast.
	timeout time.Duration
}

// New wraps an already-open Transport. Most callers use OpenFirst / OpenPort; New is
// for a custom Transport (alternate backend, or a fake for testing).
func New(t Transport, info DeviceInfo) *SQM { return &SQM{t: t, info: info} }

// OpenFirst finds and opens the first attached SQM (FTDI) serial port.
func OpenFirst() (*SQM, error) {
	t, info, err := openFirst()
	if err != nil {
		return nil, err
	}
	return New(t, info), nil
}

// OpenPort opens the SQM on a specific serial port (from Enumerate).
func OpenPort(port string) (*SQM, error) {
	t, info, err := openPort(port)
	if err != nil {
		return nil, err
	}
	return New(t, info), nil
}

// OpenBySerial opens the SQM whose USB-bridge serial number matches serial
// (case-insensitive). The serial comes from the USB descriptor via the enumerator —
// read before opening — so it disambiguates several FTDI devices sharing VID 0x0403
// and binds the same physical unit across replug / port renumbering.
func OpenBySerial(serial string) (*SQM, error) {
	t, info, err := openBySerial(serial)
	if err != nil {
		return nil, err
	}
	return New(t, info), nil
}

// Info returns the port/USB descriptor of the opened device. For the SQM's own
// firmware/serial identity, use UnitInfo.
func (s *SQM) Info() DeviceInfo { return s.info }
func (s *SQM) Close() error     { return s.t.Close() }

// Reading is a decoded "rx"/"ux" reading response (SQM-LU manual §8.2.1/§8.2.2).
type Reading struct {
	MagPerArcsec2 float64 // sky brightness in mag/arcsec² (darker sky → larger value; 0.00 means the sensor is saturated by light)
	FrequencyHz   int     // light-sensor output frequency, Hz
	PeriodCounts  int64   // sensor period in counts of the 460.8 kHz (14.7456 MHz/32) clock
	PeriodSeconds float64 // sensor period in seconds (PeriodCounts / 460800)
	TempC         float64 // temperature at the light sensor, °C
}

func (r Reading) String() string {
	return fmt.Sprintf("%.2f mag/arcsec², %d Hz, %.3fs, %.1f°C", r.MagPerArcsec2, r.FrequencyHz, r.PeriodSeconds, r.TempC)
}

// Reading requests an averaged reading with the "rx" command (last-8 boxcar average in
// period mode; frequency mode is inherently averaged over a 1 s sample).
func (s *SQM) Reading() (Reading, error) { return s.readingCmd('r', "rx") }

// UnaveragedReading requests the most recent, un-averaged reading with the "ux" command.
func (s *SQM) UnaveragedReading() (Reading, error) { return s.readingCmd('u', "ux") }

func (s *SQM) readingCmd(prefix byte, cmd string) (Reading, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	line, err := s.command(prefix, cmd)
	if err != nil {
		return Reading{}, err
	}
	return parseReading(line)
}

// parseReading decodes an "r,…"/"u,…" response. It is field-delimited (comma-separated)
// rather than fixed-column so it tolerates the optional trailing fields (freshness,
// serial number, linear count) that r1x/Rx/rFx/interval reports append after column 54.
func parseReading(line string) (Reading, error) {
	f := strings.Split(line, ",")
	if len(f) < 6 {
		return Reading{}, fmt.Errorf("unihedron: malformed reading response %q", line)
	}
	var (
		r   Reading
		err error
	)
	if r.MagPerArcsec2, err = parseFloatUnit(f[1], "m"); err != nil {
		return Reading{}, fmt.Errorf("unihedron: reading magnitude %q: %w", f[1], err)
	}
	if r.FrequencyHz, err = parseIntUnit(f[2], "Hz"); err != nil {
		return Reading{}, fmt.Errorf("unihedron: reading frequency %q: %w", f[2], err)
	}
	c, err := parseInt64Unit(f[3], "c")
	if err != nil {
		return Reading{}, fmt.Errorf("unihedron: reading counts %q: %w", f[3], err)
	}
	r.PeriodCounts = c
	if r.PeriodSeconds, err = parseFloatUnit(f[4], "s"); err != nil {
		return Reading{}, fmt.Errorf("unihedron: reading period %q: %w", f[4], err)
	}
	if r.TempC, err = parseFloatUnit(f[5], "C"); err != nil {
		return Reading{}, fmt.Errorf("unihedron: reading temperature %q: %w", f[5], err)
	}
	return r, nil
}

// UnitInfo is a decoded "ix" unit-information response (SQM-LU manual §8.2.5). All four
// are firmware-reported identity numbers; Feature gates optional capabilities (≥13
// interval reporting, ≥14 serial in interval reports, ≥44 I²C accessories).
type UnitInfo struct {
	Protocol int // data-protocol revision (independent of Feature)
	Model    int // hardware model the firmware targets
	Feature  int // software feature revision
	Serial   int // the SQM's own unique serial number
}

// UnitInfo requests firmware/identity details with the "ix" command.
func (s *SQM) UnitInfo() (UnitInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	line, err := s.command('i', "ix")
	if err != nil {
		return UnitInfo{}, err
	}
	f := strings.Split(line, ",")
	if len(f) < 5 {
		return UnitInfo{}, fmt.Errorf("unihedron: malformed unit-info response %q", line)
	}
	var u UnitInfo
	for i, dst := range []*int{&u.Protocol, &u.Model, &u.Feature, &u.Serial} {
		n, err := strconv.Atoi(strings.TrimSpace(f[i+1]))
		if err != nil {
			return UnitInfo{}, fmt.Errorf("unihedron: unit-info field %d %q: %w", i+1, f[i+1], err)
		}
		*dst = n
	}
	return u, nil
}

// Calibration is a decoded "cx" calibration-information response (SQM-LU manual §8.3.1).
type Calibration struct {
	LightCalOffset float64 // light calibration offset, mag/arcsec²
	DarkCalPeriod  float64 // dark calibration time period, seconds
	LightCalTempC  float64 // temperature during light calibration, °C
	SensorOffset   float64 // factory reference light-source offset (≈8.71), mag/arcsec²
	DarkCalTempC   float64 // temperature during dark calibration, °C
}

// Calibration requests the stored calibration constants with the "cx" command.
func (s *SQM) Calibration() (Calibration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	line, err := s.command('c', "cx")
	if err != nil {
		return Calibration{}, err
	}
	f := strings.Split(line, ",")
	if len(f) < 6 {
		return Calibration{}, fmt.Errorf("unihedron: malformed calibration response %q", line)
	}
	var (
		c    Calibration
		err2 error
	)
	units := []string{"m", "s", "C", "m", "C"}
	dst := []*float64{&c.LightCalOffset, &c.DarkCalPeriod, &c.LightCalTempC, &c.SensorOffset, &c.DarkCalTempC}
	for i := range dst {
		if *dst[i], err2 = parseFloatUnit(f[i+1], units[i]); err2 != nil {
			return Calibration{}, fmt.Errorf("unihedron: calibration field %d %q: %w", i+1, f[i+1], err2)
		}
	}
	return c, nil
}

// IntervalSettings is a decoded "Ix" interval-report settings response (manual §8.7.3,
// firmware Feature ≥13). Period is in seconds, Threshold in mag/arcsec².
type IntervalSettings struct {
	PeriodEEPROM    int     // interval period stored in EEPROM (boot value), seconds
	PeriodRAM       int     // interval period in RAM (live value), seconds
	ThresholdEEPROM float64 // report threshold stored in EEPROM, mag/arcsec²
	ThresholdRAM    float64 // report threshold in RAM, mag/arcsec²
}

// IntervalSettings requests the timed-interval reporting parameters with the "Ix"
// command (firmware Feature ≥13; older units will time out).
func (s *SQM) IntervalSettings() (IntervalSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	line, err := s.command('I', "Ix")
	if err != nil {
		return IntervalSettings{}, err
	}
	f := strings.Split(line, ",")
	if len(f) < 5 {
		return IntervalSettings{}, fmt.Errorf("unihedron: malformed interval response %q", line)
	}
	var is IntervalSettings
	if is.PeriodEEPROM, err = parseIntUnit(f[1], "s"); err != nil {
		return IntervalSettings{}, fmt.Errorf("unihedron: interval period(EEPROM) %q: %w", f[1], err)
	}
	if is.PeriodRAM, err = parseIntUnit(f[2], "s"); err != nil {
		return IntervalSettings{}, fmt.Errorf("unihedron: interval period(RAM) %q: %w", f[2], err)
	}
	if is.ThresholdEEPROM, err = parseFloatUnit(f[3], "m"); err != nil {
		return IntervalSettings{}, fmt.Errorf("unihedron: interval threshold(EEPROM) %q: %w", f[3], err)
	}
	if is.ThresholdRAM, err = parseFloatUnit(f[4], "m"); err != nil {
		return IntervalSettings{}, fmt.Errorf("unihedron: interval threshold(RAM) %q: %w", f[4], err)
	}
	return is, nil
}

// Command sends cmd verbatim (no line terminator) and returns the first reply line
// starting with prefix, trimmed of its "\r\n". The escape hatch for commands with
// no typed method.
func (s *SQM) Command(prefix byte, cmd string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.command(prefix, cmd)
}

// command writes cmd verbatim and returns the first "\r\n"-terminated line whose
// leading byte is prefix, with the trailing "\r\n" stripped. Stale input is drained
// first and the scan then skips to prefix, so neither the FTDI bridge's power-on noise
// nor a reply left over from an earlier transaction can satisfy this one. Caller holds mu.
func (s *SQM) command(prefix byte, cmd string) (string, error) {
	s.drain()
	if _, err := s.t.Write([]byte(cmd)); err != nil {
		return "", fmt.Errorf("unihedron: write %q: %w", cmd, err)
	}
	to := replyTimeout
	if s.timeout > 0 {
		to = s.timeout
	}
	deadline := time.Now().Add(to)
	var buf []byte
	tmp := make([]byte, 128)
	for time.Now().Before(deadline) {
		n, err := s.t.Read(tmp)
		if err != nil {
			return "", fmt.Errorf("unihedron: read reply to %q: %w", cmd, err)
		}
		if n == 0 {
			time.Sleep(2 * time.Millisecond)
			continue
		}
		buf = append(buf, tmp[:n]...)
		// Skip any bytes before the expected response prefix (FTDI startup noise, or a
		// stray earlier reply), then return once that line is "\n"-terminated.
		if i := bytes.IndexByte(buf, prefix); i >= 0 {
			if j := bytes.IndexByte(buf[i:], '\n'); j >= 0 {
				return strings.TrimRight(string(buf[i:i+j]), "\r\n"), nil
			}
		}
	}
	return "", fmt.Errorf("unihedron: timeout waiting for %q reply to %q", string(prefix), cmd)
}

// parseFloatUnit trims surrounding space and a trailing unit suffix (e.g. "m", "Hz",
// "s", "C") then parses the remainder as a float. SQM numeric fields are fixed-width,
// zero-padded, and may carry a leading space (positive) or '-' (negative).
func parseFloatUnit(field, unit string) (float64, error) {
	return strconv.ParseFloat(trimUnit(field, unit), 64)
}

func parseIntUnit(field, unit string) (int, error) {
	return strconv.Atoi(trimUnit(field, unit))
}

func parseInt64Unit(field, unit string) (int64, error) {
	return strconv.ParseInt(trimUnit(field, unit), 10, 64)
}

func trimUnit(field, unit string) string {
	s := strings.TrimSpace(field)
	s = strings.TrimSuffix(s, unit)
	return strings.TrimSpace(s)
}
