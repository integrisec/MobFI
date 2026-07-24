package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/integrisec/MobFI/internal/app"
	"github.com/integrisec/MobFI/internal/dbview"
	"github.com/integrisec/MobFI/internal/device"
	"github.com/integrisec/MobFI/internal/diff"
	"github.com/integrisec/MobFI/internal/doctor"
	"github.com/integrisec/MobFI/internal/extract"
	"github.com/integrisec/MobFI/internal/render"
	"github.com/integrisec/MobFI/internal/report"
	"github.com/integrisec/MobFI/internal/secrets"
	"github.com/integrisec/MobFI/internal/version"
)

// GUI is the object bound to the frontend. Each exported method becomes a
// window.go.main.GUI.<Method> promise in JavaScript. Methods return
// JSON-serialisable values from the core plus an error, which surfaces as a
// promise rejection.
type GUI struct {
	app *app.App
	ctx context.Context

	// Last scan/diff results, cached so the Export buttons can build a report
	// without the frontend round-tripping the data back.
	reportMu     sync.Mutex
	lastFindings []secrets.Finding
	scanned      bool
	lastDiff     *diff.Result
}

// NewGUI constructs the bindings over a fresh core App.
func NewGUI() *GUI { return &GUI{app: app.New()} }

// Version returns the MobFI release version (e.g. "v1.0.0") for display in the UI.
func (g *GUI) Version() string { return version.String() }

// startup receives the Wails runtime context; the core uses it so work is
// cancelled when the window closes. It also restores the saved window size and
// position, clamped/validated against the current screen so geometry saved on
// a larger or now-disconnected display never opens off-screen.
func (g *GUI) startup(ctx context.Context) {
	g.ctx = ctx
	ws, ok := loadWindowState()
	applyWindowGeometry(ctx, ws, ok)
}

// shutdown persists the final window geometry as the app closes.
func (g *GUI) shutdown(ctx context.Context) { saveGeometry(ctx) }

// PersistWindow records the current window geometry. The frontend calls this
// (debounced) on resize so geometry survives even if it can't be read during
// teardown.
func (g *GUI) PersistWindow() {
	if g.ctx == nil {
		return
	}
	saveGeometry(g.ctx)
}

// saveGeometry snapshots the window's current size and (screen-relative)
// position.
func saveGeometry(ctx context.Context) {
	w, h := wailsruntime.WindowGetSize(ctx)
	x, y := wailsruntime.WindowGetPosition(ctx)
	saveWindowState(windowState{Width: w, Height: h, X: x, Y: y, Placed: true})
}

// DetectDevices lists reachable Android/iOS devices.
func (g *GUI) DetectDevices() ([]device.Device, error) {
	return g.app.DetectDevices(g.ctx)
}

// Doctor reports which external device tools (adb, libimobiledevice, ...) are
// installed on the host, with install hints for those that are missing.
func (g *GUI) Doctor() []doctor.Tool { return g.app.Doctor() }

// ListApps enumerates the installed apps on a device (resolved by serial).
// includeSystem also lists system apps.
func (g *GUI) ListApps(deviceID string, includeSystem bool) ([]device.InstalledApp, error) {
	devices, _ := g.app.DetectDevices(g.ctx)
	for i := range devices {
		if devices[i].ID == deviceID {
			return g.app.ListApps(g.ctx, devices[i], includeSystem)
		}
	}
	return nil, fmt.Errorf("device %q not found; re-run detection", deviceID)
}

// ExtractApp resolves a device by serial and mirrors the target app's file
// tree to dest. afcScope ("container"/"documents") applies to iOS only.
// Progress is streamed to the frontend as "extract:progress" events
// (throttled) so a long extraction shows a live count.
func (g *GUI) ExtractApp(deviceID, bundleID, dest, afcScope string) (*extract.Result, error) {
	devices, _ := g.app.DetectDevices(g.ctx)
	for i := range devices {
		if devices[i].ID == deviceID {
			ctx, done := g.opContext("extract")
			defer done()
			var last time.Time
			progress := func(p extract.Progress) {
				if time.Since(last) < 120*time.Millisecond {
					return
				}
				last = time.Now()
				wailsruntime.EventsEmit(g.ctx, "extract:progress", p)
			}
			return g.app.ExtractApp(ctx, devices[i], bundleID, dest, afcScope, progress)
		}
	}
	return nil, fmt.Errorf("device %q not found; re-run detection", deviceID)
}

// --- cancellable operations ---

var (
	opMu     sync.Mutex
	opCancel = map[string]context.CancelFunc{}
)

// opContext returns a cancellable context for a named, one-at-a-time
// operation, and a done func to release it. CancelOp(name) cancels it.
func (g *GUI) opContext(name string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(g.ctx)
	opMu.Lock()
	if prev := opCancel[name]; prev != nil {
		prev() // supersede a stale one
	}
	opCancel[name] = cancel
	opMu.Unlock()
	return ctx, func() {
		opMu.Lock()
		delete(opCancel, name)
		opMu.Unlock()
		cancel()
	}
}

// CancelOp cancels an in-flight operation ("scan", "diff", "extract").
func (g *GUI) CancelOp(name string) {
	opMu.Lock()
	c := opCancel[name]
	opMu.Unlock()
	if c != nil {
		c()
	}
}

// Confirm shows a native Yes/No dialog and reports whether Yes was chosen.
func (g *GUI) Confirm(title, message string) (bool, error) {
	res, err := wailsruntime.MessageDialog(g.ctx, wailsruntime.MessageDialogOptions{
		Type:          wailsruntime.QuestionDialog,
		Title:         title,
		Message:       message,
		Buttons:       []string{"Yes", "No"},
		DefaultButton: "No",
		CancelButton:  "No",
	})
	return res == "Yes", err
}

// ScanSecrets scans an extracted tree for secrets, relaying throttled
// progress as "scan:progress" events. Cancellable via CancelOp("scan").
func (g *GUI) ScanSecrets(root string) ([]secrets.Finding, error) {
	ctx, done := g.opContext("scan")
	defer done()
	var last time.Time
	findings, err := g.app.ScanSecrets(ctx, root, func(p secrets.Progress) {
		if time.Since(last) < 120*time.Millisecond {
			return
		}
		last = time.Now()
		wailsruntime.EventsEmit(g.ctx, "scan:progress", p)
	})
	if err == nil {
		g.reportMu.Lock()
		g.lastFindings, g.scanned = findings, true
		g.reportMu.Unlock()
	}
	return findings, err
}

// AddKnownSecrets adds a user-supplied known-secrets file to the scanner.
func (g *GUI) AddKnownSecrets(path string) error {
	return g.app.AddKnownSecrets(path)
}

// Diff compares two extracted roots, relaying throttled progress as
// "diff:progress" events. Cancellable via CancelOp("diff").
func (g *GUI) Diff(rootA, rootB string) (*diff.Result, error) {
	ctx, done := g.opContext("diff")
	defer done()
	var last time.Time
	res, err := g.app.Diff(ctx, rootA, rootB, func(p diff.Progress) {
		if time.Since(last) < 120*time.Millisecond {
			return
		}
		last = time.Now()
		wailsruntime.EventsEmit(g.ctx, "diff:progress", p)
	})
	if err == nil {
		g.reportMu.Lock()
		g.lastDiff = res
		g.reportMu.Unlock()
	}
	return res, err
}

// ExportReport builds a report from the last scan ("scan") or diff ("diff")
// results, prompts for a destination, and writes it in the given format
// ("html", "json" or "text"). Returns the saved path (empty if cancelled).
// Secrets are redacted by report.Build, so exports are safe to share.
func (g *GUI) ExportReport(scope, format string) (string, error) {
	g.reportMu.Lock()
	findings, scanned, d := g.lastFindings, g.scanned, g.lastDiff
	g.reportMu.Unlock()

	var rep *report.Report
	switch scope {
	case "scan":
		if !scanned {
			return "", fmt.Errorf("run a scan first")
		}
		rep = g.app.Report(findings, nil)
	case "diff":
		if d == nil {
			return "", fmt.Errorf("run a diff first")
		}
		rep = g.app.Report(nil, d)
	case "combined":
		// Whatever has run: scan findings and/or diff in one report.
		if !scanned && d == nil {
			return "", fmt.Errorf("run a scan and/or a diff first")
		}
		if !scanned {
			findings = nil
		}
		rep = g.app.Report(findings, d)
	default:
		return "", fmt.Errorf("unknown export scope %q", scope)
	}

	ext := map[string]string{"html": ".html", "json": ".json", "text": ".txt"}[format]
	if ext == "" {
		return "", fmt.Errorf("unknown format %q", format)
	}
	name := fmt.Sprintf("mobfi-%s-%s%s", scope, time.Now().Format("2006-01-02"), ext)
	path, err := wailsruntime.SaveFileDialog(g.ctx, wailsruntime.SaveDialogOptions{
		Title:           "Export report",
		DefaultFilename: name,
	})
	if err != nil || path == "" {
		return "", err // cancelled
	}

	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	switch format {
	case "html":
		err = rep.WriteHTML(f)
	case "json":
		err = rep.WriteJSON(f)
	case "text":
		err = rep.WriteText(f)
	}
	if err != nil {
		return "", err
	}
	return path, nil
}

// DBTables lists the tables in a SQLite file.
func (g *GUI) DBTables(path string) ([]string, error) {
	return g.app.DBTables(g.ctx, path)
}

// DBRead reads up to limit rows from a table.
func (g *GUI) DBRead(path, table string, limit int) (*dbview.Table, error) {
	return g.app.DBRead(g.ctx, path, table, limit)
}

// Render produces a human-readable view of a file.
func (g *GUI) Render(path string) (*render.View, error) {
	return g.app.Render(g.ctx, path)
}

// PickDirectory opens a native folder chooser and returns the selected path
// (empty if cancelled).
func (g *GUI) PickDirectory() (string, error) {
	return wailsruntime.OpenDirectoryDialog(g.ctx, wailsruntime.OpenDialogOptions{Title: "Select a folder"})
}

// PickFile opens a native file chooser and returns the selected path (empty
// if cancelled).
func (g *GUI) PickFile() (string, error) {
	return wailsruntime.OpenFileDialog(g.ctx, wailsruntime.OpenDialogOptions{Title: "Select a file"})
}

// Copy places text on the system clipboard.
func (g *GUI) Copy(text string) error {
	return wailsruntime.ClipboardSetText(g.ctx, text)
}

// ClipboardGet returns the current clipboard text.
func (g *GUI) ClipboardGet() (string, error) {
	return wailsruntime.ClipboardGetText(g.ctx)
}

// PickSaveFile opens a native save dialog and returns the chosen path (empty
// if cancelled).
func (g *GUI) PickSaveFile(defaultName string) (string, error) {
	return wailsruntime.SaveFileDialog(g.ctx, wailsruntime.SaveDialogOptions{
		Title:           "Save session log",
		DefaultFilename: defaultName,
	})
}
