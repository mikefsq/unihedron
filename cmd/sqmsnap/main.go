// sqmsnap is a small CLI over the unihedron driver: it opens a Unihedron SQM (by port,
// by USB serial, or the first one found), prints unit info + calibration + a reading,
// and can poll readings at an interval. JSON output is available for scripting.
package main

import (
	"encoding/json"
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
		port   = flag.String("port", "", "serial port (e.g. /dev/cu.usbserial-XXXX); default: first found")
		serial = flag.String("serial", "", "open the SQM whose USB-bridge serial matches this")
		list   = flag.Bool("list", false, "list attached FTDI serial ports and exit")
		asJSON = flag.Bool("json", false, "emit JSON instead of text")
		watch  = flag.Duration("watch", 0, "poll a reading every interval (e.g. 5s); 0 = single shot")
		unavg  = flag.Bool("unaveraged", false, "use the un-averaged reading (ux) instead of rx")
	)
	flag.Parse()

	if *list {
		// Discover probes each FTDI port and returns only the ones that are SQMs.
		found, err := unihedron.Discover()
		if err != nil {
			return err
		}
		for _, d := range found {
			fmt.Printf("%s\tbridge-serial=%s\tunit-serial=%d\tprotocol=%d\tfeature=%d\n",
				d.Port, d.Serial, d.Unit.Serial, d.Unit.Protocol, d.Unit.Feature)
		}
		if len(found) == 0 {
			fmt.Fprintln(os.Stderr, "no Unihedron SQM found")
		}
		return nil
	}

	sqm, err := open(*port, *serial)
	if err != nil {
		return err
	}
	defer sqm.Close()

	info, err := sqm.UnitInfo()
	if err != nil {
		return err
	}
	cal, err := sqm.Calibration()
	if err != nil {
		return err
	}

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
