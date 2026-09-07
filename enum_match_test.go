//go:build !darwin

package unihedron

import "testing"

// The SQM's bridge shares vendor 0x0403 with every other FTDI adapter, so matching the
// vendor alone made discovery open and poke whatever else was on the bus — an MGPBox, a
// Pegasus focuser — disturbing a session another driver already held. These pin the
// narrowing, and guard against a PID typo, which would leave real hardware
// undiscoverable with no error anywhere.
func TestMatchesFTDI(t *testing.T) {
	for _, tc := range []struct {
		name     string
		vid, pid string
		want     bool
	}{
		{"the stock FT232R bridge", "0403", "6001", true},
		{"vendor cased differently", "0403", "6001", true},
		{"vendor matches but the part does not (FT231X: an MGPBox)", "0403", "6015", false},
		{"a different vendor entirely", "04D8", "6001", false},
		{"no pid reported", "0403", "", false},
		{"nothing reported", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesFTDI(tc.vid, tc.pid); got != tc.want {
				t.Fatalf("matchesFTDI(%q, %q) = %v, want %v", tc.vid, tc.pid, got, tc.want)
			}
		})
	}
}

// TestMatchesFTDICaseInsensitive: the enumerator's hex casing is not guaranteed across
// platforms, and a case-sensitive compare would fail to find the device on one of them.
// The stock ids are all digits, so this needs a letter-bearing PID to mean anything.
func TestMatchesFTDICaseInsensitive(t *testing.T) {
	orig := pidsFTDI
	t.Cleanup(func() { pidsFTDI = orig })
	pidsFTDI = []string{"ED77"}
	for _, pid := range []string{"ED77", "ed77", "Ed77"} {
		if !matchesFTDI("0403", pid) {
			t.Fatalf("pid %q not matched; casing must not decide", pid)
		}
	}
}

// TestPIDSetIsExtensible: the set exists because an OEM can reprogram an FTDI part's PID.
// A unit reporting something else is added here, not fixed by widening back to the vendor.
func TestPIDSetIsExtensible(t *testing.T) {
	if len(pidsFTDI) == 0 {
		t.Fatal("no PIDs listed: enumeration would find nothing")
	}
	orig := pidsFTDI
	t.Cleanup(func() { pidsFTDI = orig })
	pidsFTDI = append(append([]string(nil), orig...), "6011")
	if !matchesFTDI("0403", "6011") {
		t.Fatal("an added PID was not matched")
	}
	if !matchesFTDI("0403", "6001") {
		t.Fatal("adding a PID dropped the stock one")
	}
}
