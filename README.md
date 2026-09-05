# unihedron

Go driver for Unihedron Sky Quality Meters using the SQM-LU USB-serial
protocol. The built-in transport opens serial ports; it does not provide
an Ethernet connection for SQM-LE devices.

## Build and run

Requires Go 1.25 or later.

```sh
go build -o sqmsnap ./cmd/sqmsnap
./sqmsnap -list
./sqmsnap -serial 5533
./sqmsnap -watch 5s -json
```

The default run reads unit information, calibration, and one sky measurement.
`-serial` accepts a USB bridge serial or the meter's unit serial. Use `-port`
for an explicit port, `-ports` to list candidates without opening them, and
`-unaveraged` for the latest sample without boxcar averaging.

## Use the library

```go
package main

import (
    "fmt"
    "log"

    "github.com/mikefsq/unihedron"
)

func run() error {
    device, err := unihedron.OpenFirst()
    if err != nil {
        return err
    }
    defer device.Close()

    value, err := device.Reading()
    if err != nil {
        return err
    }
    fmt.Println(value)
    return nil
}

func main() {
    if err := run(); err != nil {
        log.Fatal(err)
    }
}
```

Readings include `MagPerArcsec2`, `FrequencyHz`, `PeriodCounts`,
`PeriodSeconds`, and sensor `TempC`. `UnitInfo` returns the meter identity;
`Calibration` reads calibration constants. `IntervalSettings` requires
firmware feature level 13 or later.

`OpenBySerial` selects a particular meter. Discovery probes candidate serial
ports with the identity command to distinguish SQMs from other FTDI devices.

## Platforms and development

Serial I/O supports Linux, macOS, and Windows without cgo. The service user
must have permission to open the port.

```sh
go test -race ./...
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build ./...
```

The driver wraps reading, identity, calibration-read, and interval queries.
`Command` exposes raw commands for applications needing other operations.
Tests use fake transports and captured reply formats.

## License

[MIT](LICENSE).
