package main

import (
	"context"
	"fmt"

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

// ExtractApp resolves a device by serial and mirrors the target app's file
// tree to dest.
func (g *GUI) ExtractApp(deviceID, bundleID, dest string) (*extract.Result, error) {
	devices, _ := g.app.DetectDevices(g.ctx)
	for i := range devices {
		if devices[i].ID == deviceID {
			return g.app.ExtractApp(g.ctx, devices[i], bundleID, dest)
		}
	}
	return nil, fmt.Errorf("device %q not found; re-run detection", deviceID)
}

// ScanSecrets scans an extracted tree for secrets.
func (g *GUI) ScanSecrets(root string) ([]secrets.Finding, error) {
	return g.app.ScanSecrets(g.ctx, root)
}

// AddKnownSecrets adds a user-supplied known-secrets file to the scanner.
func (g *GUI) AddKnownSecrets(path string) error {
	return g.app.AddKnownSecrets(path)
}

// Diff compares two extracted roots.
func (g *GUI) Diff(rootA, rootB string) (*diff.Result, error) {
	return g.app.Diff(g.ctx, rootA, rootB)
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
