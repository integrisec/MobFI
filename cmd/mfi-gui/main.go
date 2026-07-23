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

func main() {
	gui := NewGUI()
	err := wails.Run(&options.App{
		Title:       "MobFI",
		Width:       1100,
		Height:      780,
		MinWidth:    900,
		MinHeight:   600,
		AssetServer: &assetserver.Options{Assets: assets},
		OnStartup:   gui.startup,
		Bind:        []any{gui},
	})
	if err != nil {
		log.Fatal(err)
	}
}
