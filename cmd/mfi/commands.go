package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/integrisec/MobFI/internal/app"
	"github.com/integrisec/MobFI/internal/device"
	"github.com/integrisec/MobFI/internal/diff"
	"github.com/integrisec/MobFI/internal/secrets"
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
  report    Scan and/or diff, then summarise into a report
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
	// Resolve the serial to a detected device. Detection errors are
	// tolerated as long as the requested device turns up.
	devices, _ := core.DetectDevices(ctx)
	var target *device.Device
	for i := range devices {
		if devices[i].ID == *deviceID {
			target = &devices[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("device %q not found; run `mfi detect`", *deviceID)
	}

	res, err := core.ExtractApp(ctx, *target, *bundle, *dst)
	if err != nil {
		return err
	}
	fmt.Printf("extracted %d file(s), %d byte(s) to %s\n", res.FileCount, res.ByteCount, res.Root)
	if len(res.Skipped) > 0 {
		fmt.Printf("skipped %d path(s):\n", len(res.Skipped))
		for _, s := range res.Skipped {
			fmt.Printf("  %s: %s\n", s.Path, s.Reason)
		}
	}
	return nil
}

func runScan(ctx context.Context, core *app.App, args []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	root := fs.String("root", "", "extracted file tree to scan")
	known := fs.String("known", "", "file of known secrets to also search for (one per line)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *root == "" {
		return fmt.Errorf("scan requires -root")
	}
	if *known != "" {
		if err := core.AddKnownSecrets(*known); err != nil {
			return err
		}
	}
	findings, err := core.ScanSecrets(ctx, *root)
	if err != nil {
		return err
	}
	fmt.Printf("%d finding(s)\n", len(findings))
	for _, f := range findings {
		fmt.Printf("  [%s] %s:%d  %s\n", f.RuleID, f.Path, f.Line, f.Match)
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
	for _, c := range res.Changes {
		if c.Detail != "" {
			fmt.Printf("  %-8s %s (%s)\n", c.Kind, c.Path, c.Detail)
		} else {
			fmt.Printf("  %-8s %s\n", c.Kind, c.Path)
		}
	}
	return nil
}

func runReport(ctx context.Context, core *app.App, args []string) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	var (
		root  = fs.String("root", "", "extracted tree to scan for secrets")
		known = fs.String("known", "", "known-secrets file to add to the scan")
		a     = fs.String("a", "", "first root to diff")
		b     = fs.String("b", "", "second root to diff")
		out   = fs.String("out", "", "also write the report as JSON to this file")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *root == "" && (*a == "" || *b == "") {
		return fmt.Errorf("report needs -root (to scan) and/or both -a and -b (to diff)")
	}

	var findings []secrets.Finding
	if *root != "" {
		if *known != "" {
			if err := core.AddKnownSecrets(*known); err != nil {
				return err
			}
		}
		var err error
		if findings, err = core.ScanSecrets(ctx, *root); err != nil {
			return err
		}
	}

	var d *diff.Result
	if *a != "" && *b != "" {
		var err error
		if d, err = core.Diff(ctx, *a, *b); err != nil {
			return err
		}
	}

	rep := core.Report(findings, d)
	if err := rep.WriteText(os.Stdout); err != nil {
		return err
	}
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := rep.WriteJSON(f); err != nil {
			return err
		}
		fmt.Printf("\nwrote JSON report to %s\n", *out)
	}
	return nil
}
