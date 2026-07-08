package unihedron

import (
	"math"
	"testing"
	"time"
)

func init() { replyTimeout = 500 * time.Millisecond }

// fakeT is a canned Transport: it answers each written command with a scripted reply,
// matched by the command string. Unmatched writes yield no bytes (command() times out).
type fakeT struct {
	replies map[string][]byte
	out     []byte // pending bytes to hand back on Read
}

func (f *fakeT) Write(p []byte) (int, error) {
	if r, ok := f.replies[string(p)]; ok {
		f.out = append(f.out, r...)
	}
	return len(p), nil
}

func (f *fakeT) Read(p []byte) (int, error) {
	if len(f.out) == 0 {
		return 0, nil // idle, like a real port past its read timeout
	}
	n := copy(p, f.out)
	f.out = f.out[n:]
	return n, nil
}

func (f *fakeT) Close() error { return nil }

func newFake(replies map[string][]byte) *SQM {
	return New(&fakeT{replies: replies}, DeviceInfo{Port: "fake"})
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestReading(t *testing.T) {
	// Exact bytes captured from a live SQM-LU (serial 5533), including the FTDI
	// power-on noise (0x00 0xFE 0x00) that precedes the first reply.
	s := newFake(map[string][]byte{
		"rx": []byte("\x00\xfe\x00r, 10.79m,0000004690Hz,0000000000c,0000000.000s, 023.2C\r\n"),
	})
	r, err := s.Reading()
	if err != nil {
		t.Fatalf("Reading: %v", err)
	}
	if !approx(r.MagPerArcsec2, 10.79) {
		t.Errorf("mag = %v, want 10.79", r.MagPerArcsec2)
	}
	if r.FrequencyHz != 4690 {
		t.Errorf("freq = %v, want 4690", r.FrequencyHz)
	}
	if r.PeriodCounts != 0 {
		t.Errorf("counts = %v, want 0", r.PeriodCounts)
	}
	if !approx(r.TempC, 23.2) {
		t.Errorf("temp = %v, want 23.2", r.TempC)
	}
}

func TestReadingManualExample(t *testing.T) {
	// The exact example string from the manual (§8.2.1), with non-zero counts.
	s := newFake(map[string][]byte{
		"rx": []byte("r, 06.70m,0000022921Hz,0000000020c,0000000.000s, 039.4C\r\n"),
	})
	r, err := s.Reading()
	if err != nil {
		t.Fatalf("Reading: %v", err)
	}
	if !approx(r.MagPerArcsec2, 6.70) || r.FrequencyHz != 22921 || r.PeriodCounts != 20 || !approx(r.TempC, 39.4) {
		t.Errorf("unexpected decode: %+v", r)
	}
}

func TestReadingTrailingFields(t *testing.T) {
	// r1x appends unaveraged mag + freshness; parseReading must ignore the extras.
	s := newFake(map[string][]byte{
		"r1x": []byte("r, 06.70m,0000022921Hz,0000000020c,0000000.000s, 039.4C, 06.53m,F\r\n"),
	})
	line, err := s.Command('r', "r1x")
	if err != nil {
		t.Fatalf("Command: %v", err)
	}
	r, err := parseReading(line)
	if err != nil {
		t.Fatalf("parseReading: %v", err)
	}
	if !approx(r.MagPerArcsec2, 6.70) || r.FrequencyHz != 22921 {
		t.Errorf("unexpected decode with trailing fields: %+v", r)
	}
}

func TestUnitInfo(t *testing.T) {
	s := newFake(map[string][]byte{
		"ix": []byte("i,00000004,00000003,00000076,00005533\r\n"),
	})
	u, err := s.UnitInfo()
	if err != nil {
		t.Fatalf("UnitInfo: %v", err)
	}
	if u.Protocol != 4 || u.Model != 3 || u.Feature != 76 || u.Serial != 5533 {
		t.Errorf("unexpected unit info: %+v", u)
	}
}

func TestCalibration(t *testing.T) {
	s := newFake(map[string][]byte{
		"cx": []byte("c,00000019.96m,0000199.361s, 019.0C,00000008.71m, 020.3C\r\n"),
	})
	c, err := s.Calibration()
	if err != nil {
		t.Fatalf("Calibration: %v", err)
	}
	if !approx(c.LightCalOffset, 19.96) || !approx(c.DarkCalPeriod, 199.361) ||
		!approx(c.LightCalTempC, 19.0) || !approx(c.SensorOffset, 8.71) || !approx(c.DarkCalTempC, 20.3) {
		t.Errorf("unexpected calibration: %+v", c)
	}
}

func TestIntervalSettings(t *testing.T) {
	s := newFake(map[string][]byte{
		"Ix": []byte("I,0000000360s,0000000360s,00000017.60m,00000017.60m\r\n"),
	})
	is, err := s.IntervalSettings()
	if err != nil {
		t.Fatalf("IntervalSettings: %v", err)
	}
	if is.PeriodEEPROM != 360 || is.PeriodRAM != 360 || !approx(is.ThresholdEEPROM, 17.60) || !approx(is.ThresholdRAM, 17.60) {
		t.Errorf("unexpected interval settings: %+v", is)
	}
}

func TestNegativeMagnitude(t *testing.T) {
	// Bright-sky readings can be negative; the field carries a leading '-'.
	s := newFake(map[string][]byte{
		"rx": []byte("r,-01.23m,0000099999Hz,0000000001c,0000000.002s,-005.0C\r\n"),
	})
	r, err := s.Reading()
	if err != nil {
		t.Fatalf("Reading: %v", err)
	}
	if !approx(r.MagPerArcsec2, -1.23) || !approx(r.TempC, -5.0) {
		t.Errorf("negative decode: %+v", r)
	}
}

func TestTimeout(t *testing.T) {
	s := newFake(map[string][]byte{}) // nothing answers
	if _, err := s.Reading(); err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}
