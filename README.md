# unihedron

A Go driver for **Unihedron Sky Quality Meter (SQM)** devices over their FTDI
USB-serial link — the **SQM-LU**, and over the same ASCII protocol the **SQM-LU-DL**
and **SQM-LE**.

The SQM-LU presents itself as an FTDI virtual COM port. The protocol is plain ASCII:
single-character command types, each reply terminated with `\r\n`. 

Hardware-validated against a live **SQM-LU** (protocol 4, model 3, feature 76).

## Use

```go
sqm, err := unihedron.OpenFirst()      // or OpenPort("/dev/cu.usbserial-XXXX"), OpenBySerial("AG0JWD3W")
if err != nil { log.Fatal(err) }
defer sqm.Close()

info, _ := sqm.UnitInfo()               // protocol / model / feature / serial
cal,  _ := sqm.Calibration()            // stored calibration constants
r,    _ := sqm.Reading()                // averaged reading
fmt.Println(r)                          // 11.19 mag/arcsec², 3248 Hz, 0.000s, 25.1°C
```

`Reading` carries the sky brightness (`MagPerArcsec2`), sensor `FrequencyHz`,
`PeriodCounts` / `PeriodSeconds`, and sensor `TempC`. `UnaveragedReading()` issues `ux`
for the most-recent (un-boxcar-averaged) sample. `IntervalSettings()` reads the timed-
report parameters (firmware feature ≥13). `Command(prefix, raw)` is the escape hatch for
any command not wrapped by a typed method.

### CLI — `sqmsnap`

```
go build ./cmd/sqmsnap
./sqmsnap                    # unit + calibration + one reading
./sqmsnap -json              # machine-readable
./sqmsnap -watch 5s          # poll every 5s
./sqmsnap -list              # probe FTDI ports; list the ones that are SQMs (bridge + unit serial)
./sqmsnap -unaveraged        # use ux instead of rx
```

## Protocol notes

Command reference: SQM-LU Operator's Manual §8 "Commands and responses"
([unihedron.com](https://www.unihedron.com/projects/darksky/cd/)).

Read-only by design: the driver wraps the reading/info/calibration/interval-query
commands. The calibration-**write** and firmware-upgrade commands (`zcalA/B/D`, `zcal5-8`,
`0x19`, `:`) are intentionally not wrapped; issue them via `Command` if you must.

## License

MIT — see [LICENSE](LICENSE).
