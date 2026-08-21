// sqmsnap is a small CLI over the unihedron driver: it opens a Unihedron SQM (by port,
// by USB serial, or the first one found), prints unit info + calibration + a reading,
// and can poll readings at an interval. JSON output is available for scripting.
//
// A meter that has just been opened does not always answer its first command, and one
// that has gone quiet has been seen to stay quiet until left alone for a while. So every
// acquisition here is retried with backoff, and the port is closed while waiting rather
// than held open — waiting is the part that appears to help, and holding the port open
// is not waiting. Use -ports when you want to know whether the FTDI bridge is present
// without touching the meter at all.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/mikefsq/unihedron"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "sqmsnap:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		port      = flag.String("port", "", "serial port (e.g. /dev/cu.usbserial-XXXX); default: first found")
		serial    = flag.String("serial", "", "open the SQM whose USB-bridge serial matches this")
		list      = flag.Bool("list", false, "probe FTDI ports and list the ones that answer as SQMs, then exit")
		ports     = flag.Bool("ports", false, "list attached FTDI serial ports from their USB descriptors and exit (opens nothing)")
		asJSON    = flag.Bool("json", false, "emit JSON instead of text")
		watch     = flag.Duration("watch", 0, "poll a reading every interval (e.g. 5s); 0 = single shot")
		unavg     = flag.Bool("unaveraged", false, "use the un-averaged reading (ux) instead of rx")
		attempts  = flag.Int("attempts", 4, "attempts before giving up (each reopens the port)")
		retryWait = flag.Duration("retry-wait", 5*time.Second, "wait before the first retry; doubles each attempt")
	)
	flag.Parse()

	if *attempts < 1 {
		return fmt.Errorf("-attempts must be at least 1, got %d", *attempts)
	}

	// -ports is descriptor-only: it never opens a port, so it can always be trusted to
	// say whether the bridge is attached even when the meter is not answering.
	if *ports {
		found, err := unihedron.Enumerate()
		if err != nil {
			return err
		}
		for _, d := range found {
			fmt.Printf("%s\tserial=%s\tproduct=%s\n", d.Port, d.Serial, d.Product)
		}
		if len(found) == 0 {
			fmt.Fprintln(os.Stderr, "no FTDI serial ports found")
		}
		return nil
	}

	if *list {
		var found []unihedron.Discovered
		err := retry(*attempts, *retryWait, func() error {
			var err error
			// Discover probes each FTDI port and returns only the ones that are SQMs.
			found, err = unihedron.Discover()
			if err != nil {
				return err
			}
			if len(found) == 0 {
				return errors.New("no Unihedron SQM answered")
			}
			return nil
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "no Unihedron SQM found")
			return nil
		}
		for _, d := range found {
			fmt.Printf("%s\tbridge-serial=%s\tunit-serial=%d\tprotocol=%d\tfeature=%d\n",
				d.Port, d.Serial, d.Unit.Serial, d.Unit.Protocol, d.Unit.Feature)
		}
		return nil
	}

	// Acquisition covers the open and the first two commands, because a meter that
	// accepts the open can still fail to answer; retrying only the open would not help.
	var (
		sqm  *unihedron.SQM
		info unihedron.UnitInfo
		cal  unihedron.Calibration
	)
	err := retry(*attempts, *retryWait, func() error {
		var err error
		if sqm, err = open(*port, *serial); err != nil {
			return err
		}
		if info, err = sqm.UnitInfo(); err != nil {
			sqm.Close()
			return err
		}
		if cal, err = sqm.Calibration(); err != nil {
			sqm.Close()
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	defer sqm.Close()

	read := sqm.Reading
	if *unavg {
		read = sqm.UnaveragedReading
	}

	if !*asJSON {
		fmt.Printf("port           %s\n", sqm.Info().Port)
		fmt.Printf("unit           protocol=%d model=%d feature=%d serial=%d\n", info.Protocol, info.Model, info.Feature, info.Serial)
		fmt.Printf("calibration    light-offset=%.2f dark-period=%.3fs sensor-offset=%.2f\n", cal.LightCalOffset, cal.DarkCalPeriod, cal.SensorOffset)
	}

	for {
		r, err := read()
		if err != nil {
			// In watch mode a dropped reading should not end the run: the meter often
			// answers again later, and the caller wants the series to continue.
			if *watch > 0 {
				fmt.Fprintln(os.Stderr, "sqmsnap: reading failed:", err)
				time.Sleep(*watch)
				continue
			}
			return err
		}
		if *asJSON {
			emitJSON(info, cal, r)
		} else {
			fmt.Printf("reading        %s\n", r)
		}
		if *watch <= 0 {
			return nil
		}
		time.Sleep(*watch)
	}
}

// retry runs fn up to attempts times, waiting between tries with the port closed —
// fn is responsible for opening and for closing again on failure. The wait doubles each
// time, so the default of 4 attempts spans roughly 35s before giving up. Progress goes
// to stderr so it does not corrupt -json output.
func retry(attempts int, wait time.Duration, fn func() error) error {
	var err error
	for i := 1; ; i++ {
		if err = fn(); err == nil {
			return nil
		}
		if i >= attempts {
			return fmt.Errorf("gave up after %d attempts: %w", attempts, err)
		}
		fmt.Fprintf(os.Stderr, "sqmsnap: attempt %d/%d failed (%v); retrying in %s\n", i, attempts, err, wait)
		time.Sleep(wait)
		wait *= 2
	}
}

func open(port, serial string) (*unihedron.SQM, error) {
	switch {
	case serial != "":
		return unihedron.OpenBySerial(serial)
	case port != "":
		return unihedron.OpenPort(port)
	default:
		return unihedron.OpenFirst()
	}
}

func emitJSON(info unihedron.UnitInfo, cal unihedron.Calibration, r unihedron.Reading) {
	b, _ := json.Marshal(struct {
		Unit        unihedron.UnitInfo    `json:"unit"`
		Calibration unihedron.Calibration `json:"calibration"`
		Reading     unihedron.Reading     `json:"reading"`
	}{info, cal, r})
	fmt.Println(string(b))
}
