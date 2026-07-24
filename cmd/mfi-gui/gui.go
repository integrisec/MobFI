package main

import (
	"context"
	"fmt"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/integrisec/MobFI/internal/app"
	"github.com/integrisec/MobFI/internal/dbview"
	"github.com/integrisec/MobFI/internal/device"
	"github.com/integrisec/MobFI/internal/diff"
	"github.com/integrisec/MobFI/internal/extract"
	"github.com/integrisec/MobFI/internal/render"
	"github.com/integrisec/MobFI/internal/secrets"
)

// GUI is the object bound to the frontend. Each exported method becomes a
// window.go.main.GUI.<Method> promise in JavaScript. Methods return
// JSON-serialisable values from the core plus an error, which surfaces as a
// promise rejection.
type GUI struct {
	app *app.App
	ctx context.Context
}

// NewGUI constructs the bindings over a fresh core App.
func NewGUI() *GUI { return &GUI{app: app.New()} }

// startup receives the Wails runtime context; the core uses it so work is
// cancelled when the window closes.
func (g *GUI) startup(ctx context.Context) { g.ctx = ctx }

// DetectDevices lists reachable Android/iOS devices.
func (g *GUI) DetectDevices() ([]device.Device, error) {
	return g.app.DetectDevices(g.ctx)
}

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
			var last time.Time
			progress := func(p extract.Progress) {
				if time.Since(last) < 120*time.Millisecond {
					return
				}
				last = time.Now()
				wailsruntime.EventsEmit(g.ctx, "extract:progress", p)
			}
			return g.app.ExtractApp(g.ctx, devices[i], bundleID, dest, afcScope, progress)
		}
	}
	return nil, fmt.Errorf("device %q not found; re-run detection", deviceID)
}

// ScanSecrets scans an extracted tree for secrets, relaying throttled
// progress as "scan:progress" events.
func (g *GUI) ScanSecrets(root string) ([]secrets.Finding, error) {
	var last time.Time
	return g.app.ScanSecrets(g.ctx, root, func(p secrets.Progress) {
		if time.Since(last) < 120*time.Millisecond {
			return
		}
		last = time.Now()
		wailsruntime.EventsEmit(g.ctx, "scan:progress", p)
	})
}

// AddKnownSecrets adds a user-supplied known-secrets file to the scanner.
func (g *GUI) AddKnownSecrets(path string) error {
	return g.app.AddKnownSecrets(path)
}

// Diff compares two extracted roots, relaying throttled progress as
// "diff:progress" events.
func (g *GUI) Diff(rootA, rootB string) (*diff.Result, error) {
	var last time.Time
	return g.app.Diff(g.ctx, rootA, rootB, func(p diff.Progress) {
		if time.Since(last) < 120*time.Millisecond {
			return
		}
		last = time.Now()
		wailsruntime.EventsEmit(g.ctx, "diff:progress", p)
	})
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
