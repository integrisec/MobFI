// Command mfi-gui will host the Wails desktop frontend. It wraps the same
// internal/app core as the CLI. The Wails project (frontend/, wails.json)
// has not been generated yet — run `wails init` to add it, then bind the
// *app.App methods to the frontend.
package main

import (
	"fmt"

	"github.com/integrisec/MobFI/internal/app"
)

func main() {
	_ = app.New()
	fmt.Println("mfi-gui: Wails frontend not scaffolded yet; run `wails init`.")
}
