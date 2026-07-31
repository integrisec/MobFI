// Command mfi is the CLI frontend for MobFI, the Mobile Filesystem Inspector.
// It is a thin wrapper over internal/app; the Wails GUI wraps the same core.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

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
	// MFI-XC-03: wire SIGINT / SIGTERM into the root context so a
	// mid-extraction Ctrl-C cancels in-flight subprocesses and lets
	// scoped `defer os.RemoveAll` calls run rather than orphaning
	// sensitive tempdirs (decrypted Manifest.db, Android keystore2 DB,
	// update worker files).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	core := app.New()

	if len(args) == 0 {
		// The default entry point is the guided wizard (it does its own
		// interactive update check).
		return runWizard(ctx, core)
	}

	// One-shot subcommands print a non-blocking one-line notice afterward if an
	// update is available (to stderr, so piped stdout is unaffected). The
	// interactive "update now?" prompt is reserved for the wizard.
	err := dispatch(ctx, core, args)
	if wantsUpdateNotice(args[0]) {
		noticeUpdate(ctx, core, os.Stderr)
	}
	return err
}

func dispatch(ctx context.Context, core *app.App, args []string) error {
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
	case "decode":
		return runDecode(ctx, core, args[1:])
	case "keys":
		return runKeys(ctx, core, args[1:])
	case "doctor":
		return runDoctor(ctx, core, args[1:])
	case "update":
		return runUpdate(ctx, core, args[1:])
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

// wantsUpdateNotice reports whether a one-shot subcommand should get the
// post-run update notice. Excludes the wizard (handled interactively) and the
// commands where a network check would be noise (update/version/help).
func wantsUpdateNotice(cmd string) bool {
	switch cmd {
	case "wizard", "update", "version", "--version", "-v", "help", "-h", "--help":
		return false
	}
	return true
}
