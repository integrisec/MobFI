package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/integrisec/MobFI/internal/app"
	"github.com/integrisec/MobFI/internal/device"
	"github.com/integrisec/MobFI/internal/diff"
	"github.com/integrisec/MobFI/internal/doctor"
	"github.com/integrisec/MobFI/internal/extract"
	"github.com/integrisec/MobFI/internal/secrets"
)

func usage(w io.Writer) {
	fmt.Fprint(w, `mfi - MobFI, the Mobile Filesystem Inspector

Usage:
  mfi [command]

With no command, mfi launches the guided wizard. Advanced users can run
any subcommand directly.

Commands:
  wizard    Guided, step-by-step workflow (default)
  detect    List reachable Android/iOS devices
  apps      List installed apps on a device (bundle ids + paths)
  extract   Copy a target app's file tree to a local directory
  scan      Scan an extracted tree for secrets
  diff      Compare two extracted file roots
  report    Scan and/or diff, then summarise into a report
  db        Inspect a SQLite database file (read-only)
  render    Render a file (XML, JSON, plist, SQLite, text, hex)
  doctor    Check for the external device tools (adb, libimobiledevice)
  update    Check whether a newer MobFI release is available
  version   Print the MobFI version
  help      Show this help
`)
}

// noticeUpdate prints a one-line hint at wizard launch if a newer release
// exists or the git checkout is behind. It is best-effort and time-boxed so it
// never noticeably delays startup; set MFI_NO_UPDATE_CHECK to disable it.
func noticeUpdate(ctx context.Context, core *app.App, w io.Writer) {
	if os.Getenv("MFI_NO_UPDATE_CHECK") != "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()
	info, err := core.CheckUpdate(ctx)
	if err != nil || info == nil {
		return // offline / rate-limited: stay quiet
	}
	switch {
	case info.Available && info.Latest != "":
		fmt.Fprintf(w, "\nUpdate available: MobFI v%s (you have v%s) - run `mfi update` for details.\n", info.Latest, info.Current)
	case info.GitCheckout && info.GitBehind > 0:
		fmt.Fprintf(w, "\nYour git checkout is %d commit(s) behind upstream - run `mfi update` for details.\n", info.GitBehind)
	}
}

// runUpdate checks for a newer release (and a git checkout behind upstream) and
// prints how to update. It only reports -- it changes nothing on disk.
func runUpdate(ctx context.Context, core *app.App, args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "output the check as JSON")
	apply := fs.Bool("apply", false, "perform the update: git pull + rebuild in a checkout, or download+replace a prebuilt binary")
	if err := fs.Parse(args); err != nil {
		return err
	}

	info, err := core.CheckUpdate(ctx)
	if err != nil && (info == nil || info.Latest == "") {
		return fmt.Errorf("update check failed: %w", err)
	}

	if *apply {
		if !*asJSON && info != nil && !info.Available && !(info.GitCheckout && info.GitBehind > 0) {
			fmt.Println("Already up to date; nothing to apply.")
			return nil
		}
		res, err := core.ApplyUpdate(ctx, "cli", func(msg string) { fmt.Println("  " + msg) })
		if err != nil {
			return fmt.Errorf("update failed: %w", err)
		}
		fmt.Printf("\n%s\n", res.Message)
		if res.RestartRequired {
			fmt.Println("Re-run mfi to use the updated version.")
		}
		return nil
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}

	fmt.Printf("Current: v%s\n", info.Current)
	if info.Latest != "" {
		fmt.Printf("Latest:  v%s\n", info.Latest)
	} else {
		fmt.Println("Latest:  (could not reach GitHub)")
	}

	updated := true
	if info.Available {
		updated = false
		fmt.Printf("\nA newer release is available: v%s\n", info.Latest)
		if info.ReleaseURL != "" {
			fmt.Printf("  %s\n", info.ReleaseURL)
		}
		fmt.Println("  Update now:  mfi update -apply")
	}
	if info.GitCheckout && info.GitBehind > 0 {
		updated = false
		branch := info.GitBranch
		if branch == "" {
			branch = "your branch"
		}
		fmt.Printf("\nThis git checkout is %d commit(s) behind %s's upstream.\n", info.GitBehind, branch)
		fmt.Println("  Update now:  mfi update -apply")
	}
	if updated && info.Latest != "" {
		fmt.Println("\nYou are on the latest release.")
	}
	return nil
}

func runDoctor(ctx context.Context, core *app.App, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "output the check as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_ = ctx
	tools := core.Doctor()

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(tools)
	}

	fmt.Printf("MobFI dependency check (%s/%s)\n\n", runtime.GOOS, runtime.GOARCH)
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "STATUS\tTOOL\tPURPOSE\tLOCATION / INSTALL")
	for _, t := range tools {
		status, loc := "ok", t.Path
		if !t.Found {
			status = "MISSING"
			if t.Optional {
				status = "optional"
			}
			hint := t.Hint
			if hint == "" {
				hint = "(see README)"
			}
			loc = "-> " + hint
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", status, t.Name, t.Purpose, loc)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	fmt.Println()
	if missing := doctor.MissingCore(tools); len(missing) == 0 {
		fmt.Println("All core device tools are present.")
	} else {
		fmt.Printf("%d core tool(s) missing: %s\n", len(missing), strings.Join(missing, ", "))
		fmt.Println("Install the ones you need (see the INSTALL column), or run scripts/install.sh.")
	}
	fmt.Println("Runtime tools are optional — a feature that needs a missing tool is simply unavailable.")
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

func runApps(ctx context.Context, core *app.App, args []string) error {
	fs := flag.NewFlagSet("apps", flag.ContinueOnError)
	deviceID := fs.String("device", "", "device ID to list apps from")
	all := fs.Bool("all", false, "include system apps (default: user apps only)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *deviceID == "" {
		return fmt.Errorf("apps requires -device")
	}

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

	apps, err := core.ListApps(ctx, *target, *all)
	if err != nil {
		return err
	}
	if len(apps) == 0 {
		fmt.Println("no apps found")
		return nil
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "BUNDLE ID\tNAME\tVERSION\tDATA PATH\tINSTALL PATH")
	for _, a := range apps {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", a.BundleID, a.Name, a.Version, a.DataPath, a.InstallPath)
	}
	return tw.Flush()
}

func runExtract(ctx context.Context, core *app.App, args []string) error {
	fs := flag.NewFlagSet("extract", flag.ContinueOnError)
	var (
		deviceID = fs.String("device", "", "device ID to extract from")
		bundle   = fs.String("app", "", "target application package/bundle id")
		dst      = fs.String("out", "", "local destination directory")
		scope    = fs.String("scope", "container", "iOS scope: container, documents, or backup (iOS only)")
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

	// Live progress on stderr so stdout stays a clean summary.
	progress := func(p extract.Progress) {
		fmt.Fprintf(os.Stderr, "\r  %d file(s), %d byte(s)…", p.Files, p.Bytes)
	}
	res, err := core.ExtractApp(ctx, *target, *bundle, *dst, *scope, progress)
	if err != nil {
		fmt.Fprintln(os.Stderr)
		return err
	}
	fmt.Fprintln(os.Stderr)
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
	findings, err := core.ScanSecrets(ctx, *root, nil)
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
	res, err := core.Diff(ctx, *a, *b, nil)
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
		out   = fs.String("out", "", "also write the report to this file (format by extension: .html, .txt, else JSON)")
		show  = fs.Bool("show-secrets", false, "include raw, UNREDACTED secrets in the report (authorized local analysis only; do not share the output)")
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
		if findings, err = core.ScanSecrets(ctx, *root, nil); err != nil {
			return err
		}
	}

	var d *diff.Result
	if *a != "" && *b != "" {
		var err error
		if d, err = core.Diff(ctx, *a, *b, nil); err != nil {
			return err
		}
	}

	rep := core.Report(findings, d, *show)
	if err := rep.WriteText(os.Stdout); err != nil {
		return err
	}
	if *out != "" {
		// Format follows the extension: .html/.htm -> HTML, .txt -> text,
		// anything else -> JSON.
		kind, err := writeReportFile(rep, *out)
		if err != nil {
			return err
		}
		fmt.Printf("\nwrote %s report to %s\n", kind, *out)
	}
	return nil
}

func runDB(ctx context.Context, core *app.App, args []string) error {
	fs := flag.NewFlagSet("db", flag.ContinueOnError)
	var (
		file  = fs.String("file", "", "SQLite database file to inspect")
		table = fs.String("table", "", "table to dump (omit to list tables)")
		limit = fs.Int("limit", 100, "max rows to read")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return fmt.Errorf("db requires -file")
	}

	if *table == "" {
		tables, err := core.DBTables(ctx, *file)
		if err != nil {
			return err
		}
		fmt.Printf("%d table(s):\n", len(tables))
		for _, t := range tables {
			fmt.Printf("  %s\n", t)
		}
		return nil
	}

	t, err := core.DBRead(ctx, *file, *table, *limit)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(t.Columns, "\t"))
	for _, row := range t.Rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Printf("(%d row(s))\n", len(t.Rows))
	return nil
}

func runRender(ctx context.Context, core *app.App, args []string) error {
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	file := fs.String("file", "", "file to render")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return fmt.Errorf("render requires -file")
	}
	v, err := core.Render(ctx, *file)
	if err != nil {
		return err
	}
	fmt.Printf("# %s\n\n", v.MIME)
	fmt.Print(v.Text)
	if !strings.HasSuffix(v.Text, "\n") {
		fmt.Println()
	}
	return nil
}
