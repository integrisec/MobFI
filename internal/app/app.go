// Package app is the framework-agnostic core of MFI. Both the CLI
// (cmd/mfi) and the Wails GUI (cmd/mfi-gui) construct an *App and drive
// it; no device, extraction, scanning, diffing, or reporting logic lives
// in the frontends.
package app

import (
	"context"
	"fmt"

	"github.com/integrisec/MobFI/internal/device"
	"github.com/integrisec/MobFI/internal/diff"
	"github.com/integrisec/MobFI/internal/extract"
	"github.com/integrisec/MobFI/internal/report"
	"github.com/integrisec/MobFI/internal/secrets"
	"github.com/integrisec/MobFI/internal/transport"
)

// App wires the pluggable subsystems together and exposes the high-level
// operations shared by every frontend.
type App struct {
	Detectors  *device.Registry
	Transports *transport.Registry
	Scanner    *secrets.Scanner
}

// New returns an App with the default detectors, transports and rules
// registered.
func New() *App {
	return &App{
		Detectors:  device.DefaultRegistry(),
		Transports: transport.DefaultRegistry(),
		Scanner:    secrets.NewScanner(secrets.DefaultRules()),
	}
}

// DetectDevices enumerates all reachable Android/iOS devices across every
// registered transport.
func (a *App) DetectDevices(ctx context.Context) ([]device.Device, error) {
	return a.Detectors.DetectAll(ctx)
}

// ExtractApp copies the on-device file tree of the target application to
// dst, using the transport appropriate for the device.
func (a *App) ExtractApp(ctx context.Context, d device.Device, bundleID, dst string) (*extract.Result, error) {
	root, err := appDataRoot(d.Platform, bundleID)
	if err != nil {
		return nil, err
	}
	conn, err := a.Transports.Connect(ctx, d)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return extract.Run(ctx, conn, extract.Request{
		BundleID:   bundleID,
		SourceRoot: root,
		Dest:       dst,
	})
}

// appDataRoot returns the on-device directory holding an app's private
// data. The Android path requires root or a debuggable app (run-as); iOS
// containers are reached differently (house arrest over AFC) and are not
// wired up yet.
func appDataRoot(p device.Platform, bundleID string) (string, error) {
	switch p {
	case device.Android:
		return "/data/data/" + bundleID, nil
	case device.IOS:
		return "", fmt.Errorf("extract: iOS app extraction not yet supported")
	default:
		return "", fmt.Errorf("extract: unknown platform %q", p)
	}
}

// ScanSecrets walks root and reports any secret matches.
func (a *App) ScanSecrets(ctx context.Context, root string) ([]secrets.Finding, error) {
	return a.Scanner.ScanTree(ctx, root)
}

// AddKnownSecrets loads a user-supplied list of literal secrets and adds
// them to the scanner's rule set.
func (a *App) AddKnownSecrets(path string) error {
	rules, err := secrets.LoadKnownSecrets(path)
	if err != nil {
		return err
	}
	a.Scanner.AddRules(rules...)
	return nil
}

// Diff compares two extracted file roots at a native/semantic level.
func (a *App) Diff(ctx context.Context, rootA, rootB string) (*diff.Result, error) {
	return diff.Trees(ctx, rootA, rootB)
}

// Report aggregates findings and a diff into an actionable report.
func (a *App) Report(findings []secrets.Finding, d *diff.Result) *report.Report {
	return report.Build(findings, d)
}
