package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/integrisec/MobFI/internal/app"
	"github.com/integrisec/MobFI/internal/dbview"
	"github.com/integrisec/MobFI/internal/decode"
	"github.com/integrisec/MobFI/internal/device"
	"github.com/integrisec/MobFI/internal/diff"
	"github.com/integrisec/MobFI/internal/doctor"
	"github.com/integrisec/MobFI/internal/extract"
	"github.com/integrisec/MobFI/internal/keystore"
	"github.com/integrisec/MobFI/internal/render"
	"github.com/integrisec/MobFI/internal/report"
	"github.com/integrisec/MobFI/internal/secrets"
	"github.com/integrisec/MobFI/internal/selfupdate"
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

// CheckForUpdate reports whether a newer MobFI release exists and/or the local
// git checkout is behind upstream. It modifies nothing; the frontend shows a
// banner and lets the user open the release page. Errors (e.g. offline) are
// returned so the frontend can silently ignore them.
func (g *GUI) CheckForUpdate() (*selfupdate.Info, error) {
	ctx, cancel := context.WithTimeout(g.ctx, 20*time.Second)
	defer cancel()
	return g.app.CheckUpdate(ctx)
}

// OpenURL opens a URL in the user's default browser (used by the update banner
// to reach the release page).
func (g *GUI) OpenURL(url string) {
	wailsruntime.BrowserOpenURL(g.ctx, url)
}

// StartUpdate performs the update, reachable only after the user clicked Update
// now and confirmed (the point of approval). On macOS/Linux it runs in-process
// so the window can show LIVE progress (streamed as "update:progress" events,
// finishing with "update:done"), then relaunches -- overwriting the running
// binary is fine there. On Windows the running .exe cannot be overwritten, so
// it delegates to a detached worker that updates after this GUI exits and
// relaunches (the frontend shows a "closing to update" message).
func (g *GUI) StartUpdate() error {
	// MFI-UPD-04: gate every OS path on a NATIVE MessageDialog before
	// starting anything. The webview-side JS Confirm is not a security
	// boundary -- an XSS in a rendered file preview can invoke StartUpdate
	// bypassing it. A native modal from the Go layer is the operator's own
	// yes/no click, not a JS-produced value.
	ok, err := g.Confirm("Update MobFI", "Install the latest MobFI update?\n\nThis replaces the running binary and relaunches. It can take a minute.")
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("update cancelled by operator")
	}

	if runtime.GOOS == "windows" {
		token, err := approveUpdate()
		if err != nil {
			return err
		}
		if err := startUpdateWorker("gui", token); err != nil {
			return err
		}
		// The running .exe can't be overwritten, so MobFI closes and the detached
		// worker updates in its own console window. Tell the user clearly (in the
		// overlay) before we quit, and hold long enough to read it.
		emit := func(m string) { wailsruntime.EventsEmit(g.ctx, "update:progress", m) }
		emit("MobFI will now close to update (its running program file can't be replaced while open).")
		emit("A separate \"MobFI updater\" window will show the progress.")
		emit("MobFI reopens automatically when it finishes (about a minute) -- please don't relaunch it manually.")
		go func() {
			time.Sleep(3 * time.Second)
			wailsruntime.Quit(g.ctx)
		}()
		return nil
	}
	go g.inProcessUpdate()
	return nil
}

// inProcessUpdate runs the update in this process, streaming progress to the
// window (the update overlay), then relaunches a fresh instance and quits.
// macOS/Linux only -- see StartUpdate for the Windows path.
func (g *GUI) inProcessUpdate() {
	emit := func(msg string) { wailsruntime.EventsEmit(g.ctx, "update:progress", msg) }
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()

	emit("Starting update...")
	res, err := g.app.ApplyUpdate(ctx, "gui", emit)

	done := updateStatus{OK: true, Message: "Update complete."}
	switch {
	case err != nil:
		done = updateStatus{OK: false, Message: "Update failed: " + err.Error()}
	case res != nil:
		done = updateStatus{OK: true, Message: res.Message}
	}
	wailsruntime.EventsEmit(g.ctx, "update:done", done)
	if !done.OK {
		return // leave the window open so the user can read the error and retry
	}

	// Relaunch a fresh instance -- reliable from this in-session process -- then
	// quit. relaunchEnv (in launchApp) strips any worker vars.
	relaunch := ""
	if exe, e := os.Executable(); e == nil {
		if r, e2 := filepath.EvalSymlinks(exe); e2 == nil {
			exe = r
		}
		relaunch = relaunchTarget(exe)
	}
	time.Sleep(1600 * time.Millisecond) // let the user read "complete"
	if relaunch != "" {
		_ = launchApp(relaunch)
	}
	time.Sleep(400 * time.Millisecond)
	wailsruntime.Quit(g.ctx)
}

// TakeUpdateResult returns (and clears) the outcome of an update performed by
// the worker before this launch, or nil if there was none. The frontend calls
// it once at startup to toast the result.
func (g *GUI) TakeUpdateResult() *updateStatus {
	if st, ok := takeUpdateStatus(); ok {
		return &st
	}
	return nil
}

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

// saveGeometry snapshots the window's current size, (screen-relative) position,
// and fullscreen status. While fullscreen, WindowGetSize/Position report the
// screen -- not the windowed geometry -- so keep the last windowed values and
// only record the fullscreen flag, so leaving fullscreen restores the right
// size/position next launch.
func saveGeometry(ctx context.Context) {
	if wailsruntime.WindowIsFullscreen(ctx) {
		ws, ok := loadWindowState()
		if !ok {
			ws = windowState{Width: defaultWidth, Height: defaultHeight}
		}
		ws.Fullscreen = true
		saveWindowState(ws)
		return
	}
	w, h := wailsruntime.WindowGetSize(ctx)
	x, y := wailsruntime.WindowGetPosition(ctx)
	saveWindowState(windowState{Width: w, Height: h, X: x, Y: y, Placed: true, Fullscreen: false})
}

// Decode runs the Base64/hex/URL decoders over s for the Decode tab.
func (g *GUI) Decode(s string) []decode.Result { return g.app.Decode(s) }

// DumpKeys recovers keychain/keystore secrets for the Keys tab, degrading to
// what the device state allows.
func (g *GUI) DumpKeys(platform, deviceID, transport, state, backupDir, password string, reveal bool) (*keystore.Result, error) {
	return g.app.DumpKeys(g.ctx, keystore.Options{
		Platform:  platform,
		DeviceID:  deviceID,
		Transport: transport,
		State:     state,
		BackupDir: backupDir,
		Password:  password,
		Reveal:    reveal,
	})
}

// DetectDevices lists reachable Android/iOS devices.
func (g *GUI) DetectDevices() ([]device.Device, error) {
	devices, err := g.app.DetectDevices(g.ctx)
	// DetectAll returns partial results plus a joined error when some detectors
	// fail (e.g. simctl can't reach CoreSimulator). Wails rejects the JS promise
	// on any non-nil error, which would blank the list and hide the devices the
	// other detectors DID find. So return what we have, but surface the reason
	// (event -> a notice in the Devices view) so a missing device is explainable
	// rather than silently absent.
	warn := ""
	if err != nil {
		warn = err.Error()
		wailsruntime.LogWarningf(g.ctx, "device detection (partial): %v", err)
	}
	wailsruntime.EventsEmit(g.ctx, "detect:warning", warn)
	return devices, nil
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

// VerifyFindings LIVE-verifies the last scan's findings by calling each
// service's API, returning them with Verified set. It makes network calls that
// send matched secrets to their services, so the frontend guards it behind an
// explicit opt-in + confirmation. Cancellable via the "scan" op.
func (g *GUI) VerifyFindings() ([]secrets.Finding, error) {
	g.reportMu.Lock()
	findings := g.lastFindings
	g.reportMu.Unlock()
	if len(findings) == 0 {
		return findings, nil
	}
	ctx, done := g.opContext("scan")
	defer done()
	verified := g.app.VerifyFindings(ctx, findings)
	g.reportMu.Lock()
	g.lastFindings = verified
	g.reportMu.Unlock()
	return verified, nil
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
// By default secrets are redacted so exports are safe to share; when
// unredacted is true the raw secrets are included (authorized local analysis).
func (g *GUI) ExportReport(scope, format string, unredacted bool) (string, error) {
	g.reportMu.Lock()
	findings, scanned, d := g.lastFindings, g.scanned, g.lastDiff
	g.reportMu.Unlock()

	var rep *report.Report
	switch scope {
	case "scan":
		if !scanned {
			return "", fmt.Errorf("run a scan first")
		}
		rep = g.app.Report(findings, nil, unredacted)
	case "diff":
		if d == nil {
			return "", fmt.Errorf("run a diff first")
		}
		rep = g.app.Report(nil, d, unredacted)
	case "combined":
		// Whatever has run: scan findings and/or diff in one report.
		if !scanned && d == nil {
			return "", fmt.Errorf("run a scan and/or a diff first")
		}
		if !scanned {
			findings = nil
		}
		rep = g.app.Report(findings, d, unredacted)
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
