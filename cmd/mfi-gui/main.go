// Command mfi-gui is the Wails desktop frontend for MobFI. It binds the
// same internal/app core the CLI uses; all logic lives in the core, this
// package is only the window + JS bindings.
//
// Develop with `wails dev` and package with `wails build` (run from this
// directory). The vanilla frontend under frontend/dist needs no npm build.
package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

const (
	defaultWidth  = 1100
	defaultHeight = 780
	minWidth      = 900
	minHeight     = 600
)

func main() {
	// A GUI launched from a Start Menu / Desktop shortcut inherits Explorer's
	// login-time PATH, which can predate tools installed since (adb via winget,
	// the libimobiledevice bundle). Refresh it from the registry so the bound
	// core finds those tools without requiring a logoff. No-op off Windows.
	refreshPath()

	gui := NewGUI()

	// Restore the last window size; startup clamps it to the current screen.
	width, height := defaultWidth, defaultHeight
	if ws, ok := loadWindowState(); ok {
		width, height = ws.Width, ws.Height
	}

	err := wails.Run(&options.App{
		Title:       "MobFI",
		Width:       width,
		Height:      height,
		MinWidth:    minWidth,
		MinHeight:   minHeight,
		AssetServer: &assetserver.Options{Assets: assets},
		OnStartup:   gui.startup,
		OnShutdown:  gui.shutdown,
		Bind:        []any{gui},
	})
	if err != nil {
		log.Fatal(err)
	}
}
