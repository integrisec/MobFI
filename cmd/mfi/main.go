// Command mfi is the CLI frontend for MobFI, the Mobile Filesystem Inspector.
// It is a thin wrapper over internal/app; the Wails GUI wraps the same core.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/integrisec/MobFI/internal/app"
	"github.com/integrisec/MobFI/internal/version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "mfi:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	ctx := context.Background()
	core := app.New()

	if len(args) == 0 {
		// The default entry point is the guided wizard.
		return runWizard(ctx, core)
	}

	switch args[0] {
	case "wizard":
		return runWizard(ctx, core)
	case "detect":
		return runDetect(ctx, core, args[1:])
	case "apps":
		return runApps(ctx, core, args[1:])
	case "extract":
		return runExtract(ctx, core, args[1:])
	case "scan":
		return runScan(ctx, core, args[1:])
	case "diff":
		return runDiff(ctx, core, args[1:])
	case "report":
		return runReport(ctx, core, args[1:])
	case "db":
		return runDB(ctx, core, args[1:])
	case "render":
		return runRender(ctx, core, args[1:])
	case "doctor":
		return runDoctor(ctx, core, args[1:])
	case "version", "--version", "-v":
		fmt.Println("mfi " + version.String())
		return nil
	case "help", "-h", "--help":
		printLogo(os.Stdout)
		usage(os.Stdout)
		return nil
	default:
		usage(os.Stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}
