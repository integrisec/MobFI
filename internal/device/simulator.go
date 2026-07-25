package device

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"sort"
	"strings"

	"github.com/integrisec/MobFI/internal/sysproc"
)

// SimctlDetector discovers booted iOS Simulators via `xcrun simctl`. Unlike
// physical iOS devices (libimobiledevice), a simulator's data lives directly
// on the host filesystem, so extraction is a local copy rather than an AFC
// transfer. Only booted simulators are reported — a shut-down simulator has
// no running process to inspect and would clutter the list.
type SimctlDetector struct {
	// run executes a command and returns its stdout. Overridable in tests;
	// nil means os/exec.
	run func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// NewSimctlDetector returns a SimctlDetector that invokes `xcrun simctl`.
func NewSimctlDetector() *SimctlDetector { return &SimctlDetector{} }

// Name identifies the detector.
func (d *SimctlDetector) Name() string { return "simctl" }

func (d *SimctlDetector) exec(ctx context.Context, name string, args ...string) ([]byte, error) {
	if d.run != nil {
		return d.run(ctx, name, args...)
	}
	return sysproc.CommandContext(ctx, name, args...).Output()
}

// Detect lists booted simulators. Simulators exist only on macOS, and a
// missing Xcode/command-line-tools install yields no devices (and no error)
// so the other detectors still run.
func (d *SimctlDetector) Detect(ctx context.Context) ([]Device, error) {
	if runtime.GOOS != "darwin" {
		return nil, nil
	}
	out, err := d.exec(ctx, "xcrun", "simctl", "list", "devices", "booted", "--json")
	if err != nil {
		if simctlUnavailable(err) {
			return nil, nil
		}
		return nil, err
	}
	return parseSimctlDevices(out)
}

// simctlUnavailable reports whether the error means simctl simply is not
// available on this host (so there are no simulators to report), as opposed to
// a real failure. This is common on machines with only the Command Line Tools
// and no full Xcode: `xcrun` runs but exits 72 with "unable to find utility
// simctl", which must not surface as a device-detection error.
func simctlUnavailable(err error) bool {
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ee.ExitCode() == 72 { // xcrun EX_OSERR: utility not a developer tool
			return true
		}
		msg := strings.ToLower(string(ee.Stderr))
		if strings.Contains(msg, "unable to find utility") ||
			strings.Contains(msg, "not a developer tool") ||
			strings.Contains(msg, "requires xcode") ||
			strings.Contains(msg, "no developer tools") {
			return true
		}
	}
	return false
}

// simctlList models `simctl list devices --json`: a map of runtime id to the
// simulators created for that runtime.
type simctlList struct {
	Devices map[string][]simctlDevice `json:"devices"`
}

type simctlDevice struct {
	UDID  string `json:"udid"`
	Name  string `json:"name"`
	State string `json:"state"`
}

func parseSimctlDevices(out []byte) ([]Device, error) {
	var l simctlList
	if err := json.Unmarshal(out, &l); err != nil {
		return nil, fmt.Errorf("simctl: %w", err)
	}
	var devices []Device
	for runtimeID, sims := range l.Devices {
		ver := runtimeVersion(runtimeID)
		for _, s := range sims {
			if !strings.EqualFold(s.State, "Booted") || s.UDID == "" {
				continue
			}
			name := s.Name
			if ver != "" {
				name = s.Name + " (" + ver + ")"
			}
			devices = append(devices, Device{
				ID:        s.UDID,
				Name:      name,
				Platform:  IOS,
				Transport: Simulator,
				State:     "device",
			})
		}
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].Name < devices[j].Name })
	return devices, nil
}

// runtimeVersion turns a CoreSimulator runtime id such as
// "com.apple.CoreSimulator.SimRuntime.iOS-26-5" into "iOS 26.5".
func runtimeVersion(id string) string {
	i := strings.LastIndex(id, ".")
	if i < 0 {
		return ""
	}
	seg := id[i+1:] // e.g. "iOS-26-5"
	parts := strings.Split(seg, "-")
	if len(parts) < 2 {
		return seg
	}
	return parts[0] + " " + strings.Join(parts[1:], ".")
}

// SimulatorDataContainer returns the on-disk data-container path for an app
// installed on a booted simulator, via `simctl get_app_container ... data`.
// This is the root that extraction mirrors.
func SimulatorDataContainer(ctx context.Context, udid, bundleID string) (string, error) {
	out, err := sysproc.CommandContext(ctx, "xcrun", "simctl", "get_app_container", udid, bundleID, "data").Output()
	if err != nil {
		return "", fmt.Errorf("simctl get_app_container %s: %w", bundleID, err)
	}
	p := strings.TrimSpace(string(out))
	if p == "" {
		return "", fmt.Errorf("no data container for %q on simulator %s", bundleID, udid)
	}
	return p, nil
}

// --- app listing ---

// SimctlAppLister lists apps installed on a booted simulator via
// `xcrun simctl listapps`, whose old-style ASCII plist is converted to JSON
// with `plutil` before parsing.
type SimctlAppLister struct {
	// listapps returns the app dictionary as JSON for a udid. Overridable in
	// tests; nil runs simctl + plutil.
	listapps func(ctx context.Context, udid string) ([]byte, error)
}

// NewSimctlAppLister returns a SimctlAppLister using `xcrun simctl`.
func NewSimctlAppLister() *SimctlAppLister { return &SimctlAppLister{} }

func (l *SimctlAppLister) Supports(d Device) bool {
	return d.Platform == IOS && d.Transport == Simulator
}

// List returns apps installed on the simulator (user apps only unless
// includeSystem). A missing simctl/plutil yields no apps rather than an error.
func (l *SimctlAppLister) List(ctx context.Context, d Device, includeSystem bool) ([]InstalledApp, error) {
	raw, err := l.listappsJSON(ctx, d.ID)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return parseSimctlApps(raw, includeSystem)
}

func (l *SimctlAppLister) listappsJSON(ctx context.Context, udid string) ([]byte, error) {
	if l.listapps != nil {
		return l.listapps(ctx, udid)
	}
	ascii, err := sysproc.CommandContext(ctx, "xcrun", "simctl", "listapps", udid).Output()
	if err != nil {
		return nil, err
	}
	return plutilToJSON(ctx, ascii)
}

// plutilToJSON converts an ASCII/binary plist on stdin to JSON.
func plutilToJSON(ctx context.Context, in []byte) ([]byte, error) {
	cmd := sysproc.CommandContext(ctx, "plutil", "-convert", "json", "-", "-o", "-")
	cmd.Stdin = bytes.NewReader(in)
	return cmd.Output()
}

// simctlApp is one entry in the listapps dictionary (keyed by bundle id).
type simctlApp struct {
	ApplicationType            string `json:"ApplicationType"`
	CFBundleIdentifier         string `json:"CFBundleIdentifier"`
	CFBundleDisplayName        string `json:"CFBundleDisplayName"`
	CFBundleName               string `json:"CFBundleName"`
	CFBundleShortVersionString string `json:"CFBundleShortVersionString"`
	CFBundleVersion            string `json:"CFBundleVersion"`
	Path                       string `json:"Path"`
	DataContainer              string `json:"DataContainer"`
}

func parseSimctlApps(raw []byte, includeSystem bool) ([]InstalledApp, error) {
	var m map[string]simctlApp
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("simctl listapps: %w", err)
	}
	var apps []InstalledApp
	for _, a := range m {
		if !includeSystem && !strings.EqualFold(a.ApplicationType, "User") {
			continue
		}
		if a.CFBundleIdentifier == "" {
			continue
		}
		apps = append(apps, InstalledApp{
			Platform:    IOS,
			BundleID:    a.CFBundleIdentifier,
			Name:        firstNonEmpty(a.CFBundleDisplayName, a.CFBundleName),
			Version:     firstNonEmpty(a.CFBundleShortVersionString, a.CFBundleVersion),
			InstallPath: a.Path,
			DataPath:    fileURLToPath(a.DataContainer),
		})
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].BundleID < apps[j].BundleID })
	return apps, nil
}

// fileURLToPath turns a "file://" URL (as emitted by simctl listapps) into a
// filesystem path, decoding percent-escapes. Non-file inputs pass through.
func fileURLToPath(u string) string {
	if u == "" {
		return ""
	}
	if p, err := url.Parse(u); err == nil && p.Scheme == "file" {
		return strings.TrimRight(p.Path, "/")
	}
	return u
}
