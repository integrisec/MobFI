// Package app is the framework-agnostic core of MFI. Both the CLI
// (cmd/mfi) and the Wails GUI (cmd/mfi-gui) construct an *App and drive
// it; no device, extraction, scanning, diffing, or reporting logic lives
// in the frontends.
package app

import (
	"context"

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
	conn, err := a.Transports.Connect(ctx, d)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return extract.Run(ctx, conn, extract.Request{BundleID: bundleID, Dest: dst})
}

// ScanSecrets walks root and reports any secret matches.
func (a *App) ScanSecrets(ctx context.Context, root string) ([]secrets.Finding, error) {
	return a.Scanner.ScanTree(ctx, root)
}

// Diff compares two extracted file roots at a native/semantic level.
func (a *App) Diff(ctx context.Context, rootA, rootB string) (*diff.Result, error) {
	return diff.Trees(ctx, rootA, rootB)
}

// Report aggregates findings and a diff into an actionable report.
func (a *App) Report(findings []secrets.Finding, d *diff.Result) *report.Report {
	return report.Build(findings, d)
}
