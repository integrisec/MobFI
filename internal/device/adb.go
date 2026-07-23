package device

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
)

// ADBDetector discovers Android devices and emulators via the Android
// Debug Bridge command-line tool (`adb devices -l`). Devices reached over
// USB, an emulator, or adb-over-TCP are all reported.
type ADBDetector struct {
	// Bin is the adb executable to invoke. Defaults to "adb" (resolved
	// from PATH) when empty.
	Bin string
	// run executes a command and returns its stdout. It is a field so
	// tests can stub the adb invocation; nil means use os/exec.
	run func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// NewADBDetector returns an ADBDetector that invokes adb from PATH.
func NewADBDetector() *ADBDetector { return &ADBDetector{} }

// Name identifies the detector.
func (d *ADBDetector) Name() string { return "adb" }

func (d *ADBDetector) bin() string {
	if d.Bin != "" {
		return d.Bin
	}
	return "adb"
}

func (d *ADBDetector) exec(ctx context.Context, args ...string) ([]byte, error) {
	if d.run != nil {
		return d.run(ctx, d.bin(), args...)
	}
	return exec.CommandContext(ctx, d.bin(), args...).Output()
}

// Detect lists the Android devices adb can currently see. If adb is not
// installed it reports no devices (and no error) so that other detectors
// can still run; any other failure is returned.
func (d *ADBDetector) Detect(ctx context.Context) ([]Device, error) {
	out, err := d.exec(ctx, "devices", "-l")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return parseADBDevices(out), nil
}

// parseADBDevices parses the output of `adb devices -l`. Each device line
// is "<serial> <state> [key:value ...]"; the header and "* daemon ..."
// status lines are skipped.
func parseADBDevices(out []byte) []Device {
	var devices []Device
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" ||
			strings.HasPrefix(line, "List of devices") ||
			strings.HasPrefix(line, "*") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		serial := fields[0]
		dev := Device{
			ID:        serial,
			Name:      serial,
			Platform:  Android,
			Transport: transportForSerial(serial),
			State:     fields[1],
		}
		if dev.Transport == TCP {
			dev.Address = serial
		}
		// Trailing "key:value" tags carry a friendlier model name.
		for _, f := range fields[2:] {
			if k, v, ok := strings.Cut(f, ":"); ok && k == "model" && v != "" {
				dev.Name = strings.ReplaceAll(v, "_", " ")
			}
		}
		devices = append(devices, dev)
	}
	return devices
}

// transportForSerial infers how adb reached a device from its serial:
// emulator serials are "emulator-NNNN", adb-over-TCP serials are
// "host:port", everything else is treated as USB.
func transportForSerial(serial string) Transport {
	switch {
	case strings.HasPrefix(serial, "emulator-"):
		return Emulator
	case strings.Contains(serial, ":"):
		return TCP
	default:
		return USB
	}
}
