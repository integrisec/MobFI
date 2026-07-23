package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/integrisec/MobFI/internal/app"
)

func usage(w io.Writer) {
	fmt.Fprint(w, `mfi - mobile-app file inspector

Usage:
  mfi [command]

With no command, mfi launches the guided wizard. Advanced users can run
any subcommand directly.

Commands:
  wizard    Guided, step-by-step workflow (default)
  detect    List reachable Android/iOS devices
  extract   Copy a target app's file tree to a local directory
  scan      Scan an extracted tree for secrets
  diff      Compare two extracted file roots
  help      Show this help
`)
}

func runWizard(ctx context.Context, core *app.App) error {
	// TODO: drive the user through detect -> select device -> select app
	// -> extract -> scan/diff -> report. For now, list the steps.
	fmt.Println("MFI guided wizard (advanced users can run subcommands directly; see `mfi help`).")
	fmt.Println("  1. Detect devices")
	fmt.Println("  2. Select a device and target application")
	fmt.Println("  3. Extract the application file tree")
	fmt.Println("  4. Scan for secrets and/or diff against another capture")
	fmt.Println("  5. Review the report")
	_ = ctx
	_ = core
	return nil
}

func runDetect(ctx context.Context, core *app.App, args []string) error {
	fs := flag.NewFlagSet("detect", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	// err may be non-nil while devices is still populated: one detector
	// can fail while others succeed. Print what we found, then surface it.
	devices, err := core.DetectDevices(ctx)
	if len(devices) == 0 {
		if err != nil {
			return err
		}
		fmt.Println("no devices detected")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tPLATFORM\tTRANSPORT\tSTATE")
	for _, d := range devices {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", d.ID, d.Name, d.Platform, d.Transport, d.State)
	}
	if flushErr := tw.Flush(); flushErr != nil {
		return flushErr
	}
	return err
}

func runExtract(ctx context.Context, core *app.App, args []string) error {
	fs := flag.NewFlagSet("extract", flag.ContinueOnError)
	var (
		deviceID = fs.String("device", "", "device ID to extract from")
		bundle   = fs.String("app", "", "target application package/bundle id")
		dst      = fs.String("out", "", "local destination directory")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *deviceID == "" || *bundle == "" || *dst == "" {
		return fmt.Errorf("extract requires -device, -app and -out")
	}
	// TODO: resolve -device to a device.Device (via cached detection) and
	// call core.ExtractApp.
	_ = ctx
	_ = core
	return fmt.Errorf("extract: not implemented yet")
}

func runScan(ctx context.Context, core *app.App, args []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	root := fs.String("root", "", "extracted file tree to scan")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *root == "" {
		return fmt.Errorf("scan requires -root")
	}
	findings, err := core.ScanSecrets(ctx, *root)
	if err != nil {
		return err
	}
	fmt.Printf("%d finding(s)\n", len(findings))
	for _, f := range findings {
		fmt.Printf("  [%s] %s:%d\n", f.RuleID, f.Path, f.Line)
	}
	return nil
}

func runDiff(ctx context.Context, core *app.App, args []string) error {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	var (
		a = fs.String("a", "", "first extracted root")
		b = fs.String("b", "", "second extracted root")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *a == "" || *b == "" {
		return fmt.Errorf("diff requires -a and -b")
	}
	res, err := core.Diff(ctx, *a, *b)
	if err != nil {
		return err
	}
	fmt.Printf("%d change(s) between %s and %s\n", len(res.Changes), res.RootA, res.RootB)
	return nil
}
