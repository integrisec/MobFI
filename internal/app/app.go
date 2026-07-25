// Package app is the framework-agnostic core of MFI. Both the CLI
// (cmd/mfi) and the Wails GUI (cmd/mfi-gui) construct an *App and drive
// it; no device, extraction, scanning, diffing, or reporting logic lives
// in the frontends.
package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/integrisec/MobFI/internal/backup"
	"github.com/integrisec/MobFI/internal/dbview"
	"github.com/integrisec/MobFI/internal/device"
	"github.com/integrisec/MobFI/internal/diff"
	"github.com/integrisec/MobFI/internal/doctor"
	"github.com/integrisec/MobFI/internal/extract"
	"github.com/integrisec/MobFI/internal/render"
	"github.com/integrisec/MobFI/internal/report"
	"github.com/integrisec/MobFI/internal/secrets"
	"github.com/integrisec/MobFI/internal/selfupdate"
	"github.com/integrisec/MobFI/internal/transport"
)

// ScopeBackup extracts an iOS app's data from a full device backup (for
// non-jailbroken, non-dev-signed apps). It sits alongside the AFC house-arrest
// scopes transport.ScopeContainer / transport.ScopeDocuments.
const ScopeBackup = "backup"

// App wires the pluggable subsystems together and exposes the high-level
// operations shared by every frontend.
type App struct {
	Detectors  *device.Registry
	Transports *transport.Registry
	ADB        *transport.ADBConnector // Android access (supports run-as)
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
		ADB:        transport.NewADBConnector(),
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

// Doctor reports which external runtime tools (adb, libimobiledevice, ...) are
// present on the host, with install hints for those that are missing.
func (a *App) Doctor() []doctor.Tool { return doctor.Check() }

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
func (a *App) ExtractApp(ctx context.Context, d device.Device, bundleID, dst, afcScope string, progress func(extract.Progress)) (*extract.Result, error) {
	switch d.Platform {
	case device.Android:
		conn, err := a.androidConn(ctx, d, bundleID)
		if err != nil {
			return nil, err
		}
		defer conn.Close()
		root := "/data/data/" + bundleID
		req := extract.Request{BundleID: bundleID, SourceRoot: root, Dest: dst, Progress: progress}
		// Fast path: a single tar stream. Fall back to per-file copying if
		// the device has no `tar` (the stream yields nothing) or it fails.
		if ts, ok := conn.(transport.TarStreamer); ok {
			if res, ok := tarExtract(ctx, ts, root, req); ok {
				return res, nil
			}
		}
		return extract.Run(ctx, conn, req)
	case device.IOS:
		// An iOS Simulator keeps its containers on the host filesystem, so
		// copy them directly rather than going through AFC.
		if d.Transport == device.Simulator {
			root, err := device.SimulatorDataContainer(ctx, d.ID, bundleID)
			if err != nil {
				return nil, err
			}
			conn := transport.NewLocalConn()
			defer conn.Close()
			return extract.Run(ctx, conn, extract.Request{
				BundleID:   bundleID,
				SourceRoot: root,
				Dest:       dst,
				Progress:   progress,
			})
		}
		// "backup" scope pulls the app's data out of a full device backup —
		// the only way to reach a non-dev-signed (App Store) app's private
		// data on a stock, non-jailbroken device.
		if afcScope == ScopeBackup {
			return backup.Options{UDID: d.ID, BundleID: bundleID, Dest: dst, Progress: progress}.Run(ctx)
		}
		conn, err := a.AFC.Connect(ctx, d, bundleID, afcScope)
		if err != nil {
			return nil, err
		}
		defer conn.Close()
		res, err := extract.Run(ctx, conn, extract.Request{
			BundleID:   bundleID,
			SourceRoot: "/",
			Dest:       dst,
			Progress:   progress,
		})
		if err != nil && (afcScope == "" || afcScope == transport.ScopeContainer) {
			// The full container is only vended for dev-signed apps; App Store
			// apps deny it. Point the user at the alternatives.
			return res, fmt.Errorf("%w\n(full container access needs a dev-signed/debug app or a jailbreak; "+
				"try the 'documents' scope, or 'backup' scope to pull a production app's data)", err)
		}
		return res, err
	default:
		return nil, fmt.Errorf("extract: unknown platform %q", d.Platform)
	}
}

// tarExtract streams a tar of root and untars it into req.Dest. It reports
// success only when the stream produced files, so a device without `tar`
// (empty/garbage stream) falls back to per-file extraction.
func tarExtract(ctx context.Context, ts transport.TarStreamer, root string, req extract.Request) (*extract.Result, bool) {
	r, err := ts.TarReader(ctx, root)
	if err != nil {
		return nil, false
	}
	defer r.Close() // ignores tar's non-zero exit on partially-readable trees
	res, err := extract.RunTar(ctx, r, req)
	if err != nil || res.FileCount == 0 {
		return nil, false
	}
	return res, true
}

// androidConn picks how to reach an app's private data, probing each option
// against the actual data dir and using the first that can read it:
//  1. `run-as <pkg>` — a debuggable app on any device (the app's own uid);
//  2. `su -c` as root — a non-debuggable app on a rooted device;
//  3. a plain shell — a device where the shell user already has access
//     (adb running as root, e.g. userdebug/eng builds).
func (a *App) androidConn(ctx context.Context, d device.Device, pkg string) (transport.Conn, error) {
	dataDir := "/data/data/" + pkg
	if runas, err := a.ADB.ConnectAs(ctx, d, pkg); err == nil {
		if canReadDir(ctx, runas, dataDir) {
			return runas, nil
		}
		runas.Close()
	}
	if root, err := a.ADB.ConnectAsRoot(ctx, d); err == nil {
		if canReadDir(ctx, root, dataDir) {
			return root, nil
		}
		root.Close()
	}
	if plain, err := a.ADB.Connect(ctx, d); err == nil {
		if canReadDir(ctx, plain, dataDir) {
			return plain, nil
		}
		plain.Close()
	}
	return nil, fmt.Errorf("cannot read %s: check the package name is exactly right and the app is installed; "+
		"a non-debuggable app needs root (approve the su/superuser prompt on the device)", dataDir)
}

// canReadDir reports whether conn can actually read dataDir. It checks the
// command output rather than only the exit status, because adb does not always
// propagate a remote command's exit code -- so an inaccessible or non-existent
// directory can otherwise look like success and yield a silent empty extract.
func canReadDir(ctx context.Context, conn transport.Conn, dataDir string) bool {
	out, err := conn.Exec(ctx, "ls", "-d", dataDir)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == dataDir
}

// ScanSecrets walks root and reports any secret matches. progress, if
// non-nil, is called as files are scanned.
func (a *App) ScanSecrets(ctx context.Context, root string, progress func(secrets.Progress)) ([]secrets.Finding, error) {
	return a.Scanner.ScanTree(ctx, root, progress)
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
// progress, if non-nil, is called as files are compared.
func (a *App) Diff(ctx context.Context, rootA, rootB string, progress func(diff.Progress)) (*diff.Result, error) {
	return diff.Trees(ctx, rootA, rootB, progress)
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

// CheckUpdate reports whether a newer MobFI release is available and/or the
// local git checkout is behind its upstream. It never modifies anything -- the
// frontends surface the result and let the user choose how to update.
func (a *App) CheckUpdate(ctx context.Context) (*selfupdate.Info, error) {
	return selfupdate.Check(ctx)
}

// Report aggregates findings and a diff into an actionable report. When
// unredacted is true the raw secrets are retained in the report (for
// authorized local analysis); the safe default (false) keeps only redacted
// fingerprints so the report can be shared.
func (a *App) Report(findings []secrets.Finding, d *diff.Result, unredacted bool) *report.Report {
	return report.BuildWith(findings, d, unredacted)
}
