// Package app is the framework-agnostic core of MFI. Both the CLI
// (cmd/mfi) and the Wails GUI (cmd/mfi-gui) construct an *App and drive
// it; no device, extraction, scanning, diffing, or reporting logic lives
// in the frontends.
package app

import (
	"context"
	"fmt"

	"github.com/integrisec/MobFI/internal/dbview"
	"github.com/integrisec/MobFI/internal/device"
	"github.com/integrisec/MobFI/internal/diff"
	"github.com/integrisec/MobFI/internal/extract"
	"github.com/integrisec/MobFI/internal/render"
	"github.com/integrisec/MobFI/internal/report"
	"github.com/integrisec/MobFI/internal/secrets"
	"github.com/integrisec/MobFI/internal/transport"
)

// App wires the pluggable subsystems together and exposes the high-level
// operations shared by every frontend.
type App struct {
	Detectors  *device.Registry
	Transports *transport.Registry
	AFC        *transport.AFCConnector // iOS app-container access (app-scoped)
	AppListers []device.AppLister
	Scanner    *secrets.Scanner
	Renderers  *render.Registry
}

// New returns an App with the default detectors, transports, listers, rules
// and renderers registered.
func New() *App {
	return &App{
		Detectors:  device.DefaultRegistry(),
		Transports: transport.DefaultRegistry(),
		AFC:        transport.NewAFCConnector(),
		AppListers: device.DefaultAppListers(),
		Scanner:    secrets.NewScanner(secrets.DefaultRules()),
		Renderers:  render.DefaultRegistry(),
	}
}

// DetectDevices enumerates all reachable Android/iOS devices across every
// registered transport.
func (a *App) DetectDevices(ctx context.Context) ([]device.Device, error) {
	return a.Detectors.DetectAll(ctx)
}

// ListApps enumerates the applications on a device. When includeSystem is
// true, system apps are listed alongside user-installed ones.
func (a *App) ListApps(ctx context.Context, d device.Device, includeSystem bool) ([]device.InstalledApp, error) {
	for _, l := range a.AppListers {
		if l.Supports(d) {
			return l.List(ctx, d, includeSystem)
		}
	}
	return nil, fmt.Errorf("no app lister for platform %q", d.Platform)
}

// ExtractApp copies the on-device file tree of the target application to
// dst, using the transport appropriate for the platform: adb for Android
// (data root /data/data/<pkg>, which needs root or a debuggable app), and
// AFC house arrest for iOS (the app container root "/", scoped to the
// bundle id). afcScope selects the iOS house-arrest area ("container" or
// "documents"); it is ignored for Android.
func (a *App) ExtractApp(ctx context.Context, d device.Device, bundleID, dst, afcScope string) (*extract.Result, error) {
	switch d.Platform {
	case device.Android:
		conn, err := a.Transports.Connect(ctx, d)
		if err != nil {
			return nil, err
		}
		defer conn.Close()
		return extract.Run(ctx, conn, extract.Request{
			BundleID:   bundleID,
			SourceRoot: "/data/data/" + bundleID,
			Dest:       dst,
		})
	case device.IOS:
		conn, err := a.AFC.Connect(ctx, d, bundleID, afcScope)
		if err != nil {
			return nil, err
		}
		defer conn.Close()
		return extract.Run(ctx, conn, extract.Request{
			BundleID:   bundleID,
			SourceRoot: "/",
			Dest:       dst,
		})
	default:
		return nil, fmt.Errorf("extract: unknown platform %q", d.Platform)
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

// DBTables lists the tables in a database file (opened read-only).
func (a *App) DBTables(ctx context.Context, path string) ([]string, error) {
	db, err := dbview.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return db.Tables(ctx)
}

// DBRead reads up to limit rows from a table in a database file.
func (a *App) DBRead(ctx context.Context, path, table string, limit int) (*dbview.Table, error) {
	db, err := dbview.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return db.Read(ctx, table, limit)
}

// Render produces a human-readable view of a single file.
func (a *App) Render(ctx context.Context, path string) (*render.View, error) {
	return a.Renderers.Render(ctx, path)
}

// Report aggregates findings and a diff into an actionable report.
func (a *App) Report(findings []secrets.Finding, d *diff.Result) *report.Report {
	return report.Build(findings, d)
}
