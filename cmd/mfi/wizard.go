package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/integrisec/MobFI/internal/app"
	"github.com/integrisec/MobFI/internal/device"
	"github.com/integrisec/MobFI/internal/diff"
	"github.com/integrisec/MobFI/internal/extract"
	"github.com/integrisec/MobFI/internal/report"
	"github.com/integrisec/MobFI/internal/secrets"
)

// runWizard drives the guided, step-by-step workflow: detect a device, pick a
// target app, extract it, then scan/diff and review a report. It reuses the
// same core methods the subcommands call. `q` (or EOF/Ctrl-D) at any prompt
// exits cleanly.
func runWizard(ctx context.Context, core *app.App) error {
	printLogo(os.Stdout)
	fmt.Println("MobFI guided wizard  —  type `q` (or Ctrl-D) at any prompt to quit.")
	fmt.Println("Advanced users can run subcommands directly; see `mfi help`.")

	w := &wizardIO{in: bufio.NewReader(os.Stdin), out: os.Stdout}
	maybeUpdate(ctx, core, w)

	dev, ok, err := w.selectDevice(ctx, core)
	if err != nil || !ok {
		return err
	}

	target, ok, err := w.selectApp(ctx, core, dev)
	if err != nil || !ok {
		return err
	}

	root, ok, err := w.extractApp(ctx, core, dev, target)
	if err != nil || !ok {
		return err
	}

	return w.scanDiffReport(ctx, core, root)
}

// maybeUpdate offers to update at wizard launch when an update is available and
// stdin is an interactive terminal: it prompts, applies the update with live
// progress, and re-execs the freshly-built binary so the wizard continues on
// the new version. When stdin is not a terminal (piped) it only prints the
// one-line notice, like the one-shot subcommands. Skipped when
// MFI_NO_UPDATE_CHECK is set, or MFI_UPDATED (we just re-execed after updating).
func maybeUpdate(ctx context.Context, core *app.App, w *wizardIO) {
	if os.Getenv("MFI_NO_UPDATE_CHECK") != "" || os.Getenv("MFI_UPDATED") != "" {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	info, err := core.CheckUpdate(cctx)
	cancel()
	if err != nil || info == nil {
		return
	}
	available := info.Available && info.Latest != ""
	behind := info.GitCheckout && info.GitBehind > 0
	if !available && !behind {
		return
	}

	if available {
		fmt.Fprintf(w.out, "\nUpdate available: MobFI v%s (you have v%s).\n", info.Latest, info.Current)
	} else {
		fmt.Fprintf(w.out, "\nYour git checkout is %d commit(s) behind upstream.\n", info.GitBehind)
	}
	if !stdinIsTTY() {
		fmt.Fprintln(w.out, "Run `mfi update -apply` to update.")
		return
	}

	ans, ok := w.ask("  Update now? [y/N]: ")
	if !ok || !isYes(ans) {
		fmt.Fprintln(w.out, "  Continuing without updating (run `mfi update -apply` any time).")
		return
	}

	fmt.Fprintln(w.out, "\nUpdating -- this can take a minute:")
	res, err := core.ApplyUpdate(context.Background(), "cli", func(msg string) {
		fmt.Fprintln(w.out, "  "+msg)
	})
	if err != nil {
		fmt.Fprintf(w.out, "\nUpdate failed: %v\nContinuing on the current version.\n", err)
		return
	}
	fmt.Fprintf(w.out, "\n%s\n", res.Message)

	// Re-exec the (now-updated) binary so the wizard restarts on the new build.
	exe, e := os.Executable()
	if e != nil {
		os.Exit(0)
	}
	fmt.Fprintln(w.out, "Relaunching mfi on the new version...")
	if err := execReplace(exe); err != nil {
		fmt.Fprintf(w.out, "(could not relaunch automatically: %v -- re-run mfi)\n", err)
	}
	os.Exit(0) // execReplace does not return on success
}

// stdinIsTTY reports whether standard input is an interactive terminal (so a
// blocking prompt is appropriate). False when piped or redirected.
func stdinIsTTY() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && (fi.Mode()&os.ModeCharDevice) != 0
}

func isYes(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "y" || s == "yes"
}

// wizardIO wraps buffered stdin + an output writer with small prompt helpers.
type wizardIO struct {
	in  *bufio.Reader
	out io.Writer
}

// ask prints prompt and returns the trimmed reply. ok is false on EOF with no
// input (e.g. stdin is not a terminal), signalling the caller to bail out.
func (w *wizardIO) ask(prompt string) (reply string, ok bool) {
	fmt.Fprint(w.out, prompt)
	line, err := w.in.ReadString('\n')
	line = strings.TrimSpace(line)
	if err != nil && line == "" {
		fmt.Fprintln(w.out)
		return "", false
	}
	return line, true
}

// quit reports whether the reply (or EOF) means the user wants to stop.
func quit(reply string, ok bool) bool {
	return !ok || strings.EqualFold(reply, "q") || strings.EqualFold(reply, "quit")
}

// yesNo prompts a yes/no question with a default (used on blank/EOF).
func (w *wizardIO) yesNo(question string, def bool) bool {
	hint := " [y/N]: "
	if def {
		hint = " [Y/n]: "
	}
	reply, ok := w.ask(question + hint)
	if !ok || reply == "" {
		return def
	}
	return strings.HasPrefix(strings.ToLower(reply), "y")
}

// selectDevice detects devices and lets the user pick one, with re-detect.
func (w *wizardIO) selectDevice(ctx context.Context, core *app.App) (device.Device, bool, error) {
	for {
		fmt.Fprintln(w.out, "\nStep 1 — Detecting devices…")
		devices, derr := core.DetectDevices(ctx)
		if len(devices) == 0 {
			if derr != nil {
				fmt.Fprintf(w.out, "  detection error: %v\n", derr)
			} else {
				fmt.Fprintln(w.out, "  no devices detected — plug in a device or start an emulator/simulator.")
			}
			reply, ok := w.ask("  [r]e-detect or [q]uit: ")
			if quit(reply, ok) {
				return device.Device{}, false, nil
			}
			continue
		}

		fmt.Fprintln(w.out)
		for i, d := range devices {
			fmt.Fprintf(w.out, "  %d. %-24s %s  (%s/%s, %s)\n", i+1, d.ID, d.Name, d.Platform, d.Transport, d.State)
		}
		reply, ok := w.ask(fmt.Sprintf("Select a device [1-%d], [r]e-detect, or [q]uit: ", len(devices)))
		if quit(reply, ok) {
			return device.Device{}, false, nil
		}
		if strings.EqualFold(reply, "r") {
			continue
		}
		if n, err := strconv.Atoi(reply); err == nil && n >= 1 && n <= len(devices) {
			return devices[n-1], true, nil
		}
		fmt.Fprintln(w.out, "  please enter a listed number.")
	}
}

// selectApp lists installed apps and lets the user pick one; it can widen the
// list to include system apps.
func (w *wizardIO) selectApp(ctx context.Context, core *app.App, d device.Device) (device.InstalledApp, bool, error) {
	includeSystem := false
	for {
		fmt.Fprintf(w.out, "\nStep 2 — Listing apps on %s…\n", d.Name)
		apps, err := core.ListApps(ctx, d, includeSystem)
		if err != nil {
			return device.InstalledApp{}, false, err
		}
		if len(apps) == 0 {
			if !includeSystem {
				fmt.Fprintln(w.out, "  no user apps found — including system apps.")
				includeSystem = true
				continue
			}
			fmt.Fprintln(w.out, "  no apps found on this device.")
			return device.InstalledApp{}, false, nil
		}

		fmt.Fprintln(w.out)
		for i, a := range apps {
			name := a.Name
			if name == "" {
				name = a.BundleID
			}
			fmt.Fprintf(w.out, "  %d. %-32s %s\n", i+1, name, a.BundleID)
		}
		prompt := fmt.Sprintf("Select an app [1-%d]", len(apps))
		if !includeSystem {
			prompt += ", [a]ll (incl. system)"
		}
		prompt += ", or [q]uit: "
		reply, ok := w.ask(prompt)
		if quit(reply, ok) {
			return device.InstalledApp{}, false, nil
		}
		if !includeSystem && strings.EqualFold(reply, "a") {
			includeSystem = true
			continue
		}
		if n, err := strconv.Atoi(reply); err == nil && n >= 1 && n <= len(apps) {
			return apps[n-1], true, nil
		}
		fmt.Fprintln(w.out, "  please enter a listed number.")
	}
}

// extractApp asks for a destination (and iOS scope) and mirrors the app tree.
func (w *wizardIO) extractApp(ctx context.Context, core *app.App, d device.Device, a device.InstalledApp) (string, bool, error) {
	fmt.Fprintf(w.out, "\nStep 3 — Extract %s\n", a.BundleID)
	dst, ok := w.ask("  Destination directory: ")
	if quit(dst, ok) || dst == "" {
		return "", false, nil
	}
	dst = expandPath(dst)

	scope := "container"
	if d.Platform == device.IOS && d.Transport != device.Simulator {
		reply, ok := w.ask("  iOS AFC scope — [c]ontainer (default) or [d]ocuments: ")
		if !ok {
			return "", false, nil
		}
		if strings.HasPrefix(strings.ToLower(reply), "d") {
			scope = "documents"
		}
	}

	progress := func(p extract.Progress) {
		fmt.Fprintf(os.Stderr, "\r  %d file(s), %d byte(s)…", p.Files, p.Bytes)
	}
	res, err := core.ExtractApp(ctx, d, a.BundleID, dst, scope, progress)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", false, err
	}
	fmt.Fprintf(w.out, "  extracted %d file(s), %d byte(s) to %s\n", res.FileCount, res.ByteCount, res.Root)
	if len(res.Skipped) > 0 {
		fmt.Fprintf(w.out, "  skipped %d path(s) (permission or traversal guards).\n", len(res.Skipped))
	}
	return res.Root, true, nil
}

// scanDiffReport runs the optional secrets scan and diff, then prints (and
// optionally saves) the report.
func (w *wizardIO) scanDiffReport(ctx context.Context, core *app.App, root string) error {
	fmt.Fprintln(w.out, "\nStep 4 — Scan and/or diff")

	var findings []secrets.Finding
	if w.yesNo("  Scan the extracted tree for secrets?", true) {
		if kp, ok := w.ask("  Known-secrets file to also search (optional, blank to skip): "); ok && kp != "" {
			if err := core.AddKnownSecrets(expandPath(kp)); err != nil {
				fmt.Fprintf(w.out, "  could not load known secrets: %v\n", err)
			}
		}
		fmt.Fprintln(w.out, "  scanning…")
		f, err := core.ScanSecrets(ctx, root, nil)
		if err != nil {
			return err
		}
		findings = f
		fmt.Fprintf(w.out, "  %d finding(s).\n", len(findings))
	}

	var d *diff.Result
	if w.yesNo("  Diff this capture against another extracted root?", false) {
		if other, ok := w.ask("  Second root to diff against: "); ok && other != "" {
			r, err := core.Diff(ctx, root, expandPath(other), nil)
			if err != nil {
				return err
			}
			d = r
			fmt.Fprintf(w.out, "  %d change(s).\n", len(d.Changes))
		}
	}

	fmt.Fprintln(w.out, "\nStep 5 — Report")
	fmt.Fprintln(w.out)
	rep := core.Report(findings, d, false) // wizard reports stay redacted; use `mfi report -show-secrets` for raw
	if err := rep.WriteText(w.out); err != nil {
		return err
	}
	if out, ok := w.ask("\n  Write report to a file? (path, or blank to skip): "); ok && out != "" {
		out = expandPath(out)
		kind, err := writeReportFile(rep, out)
		if err != nil {
			return err
		}
		fmt.Fprintf(w.out, "  wrote %s report to %s\n", kind, out)
	}
	return nil
}

// writeReportFile writes rep to path, choosing the format from the extension:
// .html/.htm -> HTML, .txt -> text, anything else -> JSON.
func writeReportFile(rep *report.Report, path string) (kind string, err error) {
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".htm":
		return "HTML", rep.WriteHTML(f)
	case ".txt":
		return "text", rep.WriteText(f)
	default:
		return "JSON", rep.WriteJSON(f)
	}
}

// expandPath expands a leading ~ to the user's home directory.
func expandPath(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}
