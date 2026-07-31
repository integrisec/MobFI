package device

import (
	"bufio"
	"bytes"
	"context"
	"strings"

	"github.com/integrisec/MobFI/internal/sysproc"
)

// IOSDetector discovers iOS devices via the libimobiledevice tools:
// `idevice_id` lists the UDIDs of attached (USB) and network devices, and
// `ideviceinfo` resolves a friendly name and confirms the device is paired
// and unlocked.
type IOSDetector struct {
	// IDeviceIDBin and IDeviceInfoBin are the executables to invoke.
	// Empty means "idevice_id" / "ideviceinfo" resolved from PATH.
	IDeviceIDBin   string
	IDeviceInfoBin string
	// run executes a command and returns its stdout. It is a field so
	// tests can stub the libimobiledevice invocations; nil means os/exec.
	run func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// NewIOSDetector returns an IOSDetector that invokes the libimobiledevice
// tools from PATH.
func NewIOSDetector() *IOSDetector { return &IOSDetector{} }

// Name identifies the detector.
func (d *IOSDetector) Name() string { return "libimobiledevice" }

func (d *IOSDetector) idBin() string {
	if d.IDeviceIDBin != "" {
		return d.IDeviceIDBin
	}
	return "idevice_id"
}

func (d *IOSDetector) infoBin() string {
	if d.IDeviceInfoBin != "" {
		return d.IDeviceInfoBin
	}
	return "ideviceinfo"
}

func (d *IOSDetector) exec(ctx context.Context, name string, args ...string) ([]byte, error) {
	if d.run != nil {
		return d.run(ctx, name, args...)
	}
	return sysproc.CommandContext(ctx, name, args...).Output()
}

// Detect lists iOS devices reachable over USB and the network. If the
// libimobiledevice tools are absent, or present but unable to enumerate
// (e.g. on Windows with no Apple Mobile Device Service / usbmuxd running,
// where idevice_id exits non-zero), it reports no devices and no error so
// other detectors still run and the UI stays quiet. Only cancellation of the
// caller's context is propagated.
func (d *IOSDetector) Detect(ctx context.Context) ([]Device, error) {
	usb, err := d.listUDIDs(ctx, "-l")
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// ErrNotFound (tool missing) or a non-zero exit (no daemon / no
		// devices) both mean there is nothing to report here.
		return nil, nil
	}
	// Network listing is best-effort: older idevice_id builds lack "-n",
	// so a failure here should not drop the USB results.
	net, _ := d.listUDIDs(ctx, "-n")

	var devices []Device
	for _, udid := range usb {
		devices = append(devices, d.describe(ctx, udid, USB))
	}
	for _, udid := range net {
		devices = append(devices, d.describe(ctx, udid, TCP))
	}
	return devices, nil
}

func (d *IOSDetector) listUDIDs(ctx context.Context, flag string) ([]string, error) {
	out, err := d.exec(ctx, d.idBin(), flag)
	if err != nil {
		return nil, err
	}
	return parseUDIDs(out), nil
}

// describe resolves a device's name and pairing state via ideviceinfo. A
// device whose info cannot be read (not paired, or locked) is reported as
// "unpaired" so the wizard can prompt the user to trust the host.
func (d *IOSDetector) describe(ctx context.Context, udid string, t Transport) Device {
	dev := Device{ID: udid, Name: udid, Platform: IOS, Transport: t}
	args := []string{"-u", udid, "-k", "DeviceName"}
	if t == TCP {
		args = append([]string{"-n"}, args...)
	}
	out, err := d.exec(ctx, d.infoBin(), args...)
	if err != nil {
		dev.State = "unpaired"
		return dev
	}
	dev.State = "ready"
	if name := strings.TrimSpace(string(out)); name != "" {
		dev.Name = name
	}
	return dev
}

// parseUDIDs reads one UDID per line from idevice_id output, tolerating a
// trailing "(USB)"/"(Network)" annotation by taking the first field.
func parseUDIDs(out []byte) []string {
	var ids []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		ids = append(ids, strings.Fields(line)[0])
	}
	return ids
}
