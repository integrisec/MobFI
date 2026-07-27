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
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

//go:embed all:frontend/dist
var assets embed.FS

// appIcon is the window / taskbar icon. macOS and Windows bake build/appicon
// into the app bundle at package time, but on Linux Wails needs the icon bytes
// at runtime (via linux.Options.Icon) or the window shows a blank placeholder.
//
//go:embed build/appicon.png
var appIcon []byte

const (
	defaultWidth  = 1100
	defaultHeight = 780
	minWidth      = 900
	minHeight     = 600
)

func main() {
	// If we were launched as the detached update worker, do the update (waiting
	// for the old GUI to exit, replacing files, relaunching) and exit -- never
	// open a window in that mode.
	if runUpdateWorkerIfRequested() {
		return
	}

	// A GUI launched from a shortcut/Finder inherits a minimal, login-time PATH
	// that can predate tools installed since (adb, the libimobiledevice bundle).
	// Refresh it (registry on Windows, login-shell + standard dirs on macOS) so
	// the bound core finds those tools without requiring a re-login.
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
		// Linux needs the window/taskbar icon supplied at runtime; ProgramName
		// sets the WM class so a matching .desktop entry can pair its icon.
		Linux: &linux.Options{
			Icon:        appIcon,
			ProgramName: "MobFI",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
