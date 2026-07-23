package device

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/integrisec/MobFI/internal/plist"
)

// InstalledApp is an application installed on a device, as seen by an
// AppLister.
type InstalledApp struct {
	BundleID    string   `json:"bundle_id"` // package name / CFBundleIdentifier
	Name        string   `json:"name"`      // display name (may be empty)
	Platform    Platform `json:"platform"`
	DataPath    string   `json:"data_path"`    // on-device data dir (empty for iOS: use AFC)
	InstallPath string   `json:"install_path"` // APK path (Android) or .app bundle path (iOS)
	Version     string   `json:"version,omitempty"`
}

// AppLister enumerates the user-installed applications on a device.
type AppLister interface {
	Supports(d Device) bool
	List(ctx context.Context, d Device) ([]InstalledApp, error)
}

// DefaultAppListers returns the built-in listers for Android and iOS.
func DefaultAppListers() []AppLister {
	return []AppLister{NewADBAppLister(), NewIOSAppLister()}
}

// --- Android (adb) ---

// ADBAppLister lists third-party apps via `adb shell pm list packages -f -3`.
type ADBAppLister struct {
	Bin string
	run func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// NewADBAppLister returns an ADBAppLister using adb from PATH.
func NewADBAppLister() *ADBAppLister { return &ADBAppLister{} }

func (l *ADBAppLister) Supports(d Device) bool { return d.Platform == Android }

func (l *ADBAppLister) bin() string {
	if l.Bin != "" {
		return l.Bin
	}
	return "adb"
}

func (l *ADBAppLister) exec(ctx context.Context, args ...string) ([]byte, error) {
	if l.run != nil {
		return l.run(ctx, l.bin(), args...)
	}
	return exec.CommandContext(ctx, l.bin(), args...).Output()
}

// List returns the third-party (user-installed) apps. A missing adb yields
// no apps rather than an error.
func (l *ADBAppLister) List(ctx context.Context, d Device) ([]InstalledApp, error) {
	out, err := l.exec(ctx, "-s", d.ID, "shell", "pm", "list", "packages", "-f", "-3")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return parseADBPackages(out), nil
}

// parseADBPackages parses `pm list packages -f` lines of the form
// "package:<apkPath>=<packageName>". The APK path may itself contain '=',
// so the split is on the last '='.
func parseADBPackages(out []byte) []InstalledApp {
	var apps []InstalledApp
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(sc.Text()), "package:"))
		if line == "" {
			continue
		}
		apk, pkg := "", line
		if i := strings.LastIndex(line, "="); i >= 0 {
			apk, pkg = line[:i], line[i+1:]
		}
		if pkg == "" {
			continue
		}
		apps = append(apps, InstalledApp{
			BundleID:    pkg,
			Platform:    Android,
			DataPath:    "/data/data/" + pkg,
			InstallPath: apk,
		})
	}
	return apps
}

// --- iOS (ideviceinstaller) ---

// IOSAppLister lists user apps via `ideviceinstaller list -o xml`, decoding
// the returned property list.
type IOSAppLister struct {
	Bin string
	run func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// NewIOSAppLister returns an IOSAppLister using ideviceinstaller from PATH.
func NewIOSAppLister() *IOSAppLister { return &IOSAppLister{} }

func (l *IOSAppLister) Supports(d Device) bool { return d.Platform == IOS }

func (l *IOSAppLister) bin() string {
	if l.Bin != "" {
		return l.Bin
	}
	return "ideviceinstaller"
}

func (l *IOSAppLister) exec(ctx context.Context, args ...string) ([]byte, error) {
	if l.run != nil {
		return l.run(ctx, l.bin(), args...)
	}
	return exec.CommandContext(ctx, l.bin(), args...).Output()
}

// List returns the user-installed iOS apps. Missing ideviceinstaller yields
// no apps rather than an error.
func (l *IOSAppLister) List(ctx context.Context, d Device) ([]InstalledApp, error) {
	out, err := l.exec(ctx, "-u", d.ID, "list", "-o", "xml", "-o", "list_user")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return parseIOSApps(out)
}

// parseIOSApps decodes the installation_proxy property list emitted by
// `ideviceinstaller list -o xml` (an array of app dicts).
func parseIOSApps(out []byte) ([]InstalledApp, error) {
	v, err := plist.DecodeAny(out)
	if err != nil {
		return nil, err
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("device: unexpected ideviceinstaller output")
	}
	var apps []InstalledApp
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		app := InstalledApp{
			Platform:    IOS,
			BundleID:    plistStr(m, "CFBundleIdentifier"),
			Name:        firstNonEmpty(plistStr(m, "CFBundleDisplayName"), plistStr(m, "CFBundleName")),
			Version:     firstNonEmpty(plistStr(m, "CFBundleShortVersionString"), plistStr(m, "CFBundleVersion")),
			InstallPath: plistStr(m, "Path"),
			DataPath:    plistStr(m, "Container"), // often absent; data is reached via AFC
		}
		if app.BundleID != "" {
			apps = append(apps, app)
		}
	}
	return apps, nil
}

func plistStr(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
