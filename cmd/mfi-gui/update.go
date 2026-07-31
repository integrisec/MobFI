package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/integrisec/MobFI/internal/app"
	"github.com/integrisec/MobFI/internal/selfupdate"
)

// The GUI updates itself out-of-process so it can be fully closed while its
// files are replaced, then reopened. A detached copy of this binary runs as the
// "update worker": it waits for the GUI to exit, performs the update, writes a
// status file, and relaunches the app. The env vars below wire that up; they
// are stripped from the relaunched app's environment (see relaunchEnv) so it
// never restarts as another worker.
const (
	envWorker   = "MOBFI_UPDATE_WORKER"   // set on the worker process
	envPPID     = "MOBFI_UPDATE_PPID"     // GUI pid the worker waits to exit
	envTarget   = "MOBFI_UPDATE_TARGET"   // "gui" (or "cli")
	envRelaunch = "MOBFI_UPDATE_RELAUNCH" // app/bundle path to reopen afterwards
	envStatus   = "MOBFI_UPDATE_STATUS"   // status file the GUI reads on next launch
	envToken    = "MOBFI_UPDATE_TOKEN"    // one-time approval token (see approval token)
)

// approvalTTL is how long a written approval token stays valid.
const approvalTTL = 2 * time.Minute

// updateStatus is the outcome of a worker run, surfaced as a toast on relaunch.
type updateStatus struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// runUpdateWorkerIfRequested reports whether this process was launched as the
// detached update worker; if so it performs the update and the caller (main)
// must exit without opening a window.
func runUpdateWorkerIfRequested() bool {
	if os.Getenv(envWorker) == "" {
		return false
	}
	updateWorker()
	return true
}

// updateWorker waits for the GUI to exit, runs the update, records the result,
// and relaunches the app. It never opens a Wails window. Everything is logged
// to updateLogPath() and the app is relaunched even on panic, so a failure is
// never silent and the user is never left without the app.
func updateWorker() {
	lg := newUpdateLog()
	defer lg.Close()
	target := os.Getenv(envTarget)
	if target == "" {
		target = "gui"
	}
	lg.Printf("worker start: pid=%d ppid=%s target=%s", os.Getpid(), os.Getenv(envPPID), target)

	// Approval gate: the update NEVER runs without explicit user approval. The
	// GUI writes a one-time token when the user clicks Update now and confirms,
	// and passes it via env. Without a matching, recent, unused token (e.g. a
	// leaked env var or a stray relaunch), refuse and exit WITHOUT relaunching.
	if !consumeApprovalToken(os.Getenv(envToken)) {
		lg.Printf("no valid approval token; refusing to update (updates require explicit user approval)")
		return
	}

	// Hard re-entry guard: if another worker ran seconds ago, a relaunch loop is
	// underway (e.g. the worker env leaked into a relaunched app). Abort WITHOUT
	// relaunching -- that breaks the loop instead of spawning yet another
	// instance. Legitimate updates are minutes apart, so this never trips them.
	if recentWorkerRun() {
		lg.Printf("re-entry guard tripped (a worker ran <%s ago): aborting to prevent a relaunch loop", workerReentryWindow)
		return
	}
	markWorkerRun()

	// MFI-UPD-08: only relaunch a target that resolves to the same install
	// prefix as the currently-running worker executable. An attacker with a
	// stolen approval token would otherwise set envRelaunch to /tmp/evil
	// and get the worker to exec their binary once the update completes.
	relaunch := validateRelaunchTarget(os.Getenv(envRelaunch), lg)
	// Guarantee a recorded status and a relaunch, even on panic, so clicking
	// Update never leaves the user with a closed app and no explanation.
	defer func() {
		if r := recover(); r != nil {
			lg.Printf("PANIC: %v", r)
			writeUpdateStatus(os.Getenv(envStatus), nil, fmt.Errorf("update crashed: %v", r))
		}
		if relaunch != "" {
			lg.Printf("relaunching: %s", relaunch)
			if err := launchApp(relaunch); err != nil {
				lg.Printf("relaunch error: %v", err)
			}
		}
		lg.Printf("worker done")
	}()

	// On Windows, show progress in a dedicated console window (the GUI had to
	// close so its .exe could be replaced, so it can't stream into the app).
	console := workerConsole()
	cprintf := func(format string, a ...any) {
		if console != nil {
			fmt.Fprintf(console, format+"\n", a...)
		}
	}
	if console != nil {
		defer console.Close()
	}
	cprintf("MobFI updater")
	cprintf("Updating %s. This window closes and MobFI reopens automatically when done.\n", target)

	refreshPath() // resolve go/git/wails via the login-shell / registry PATH
	if pid, err := strconv.Atoi(os.Getenv(envPPID)); err == nil {
		lg.Printf("waiting for GUI (pid %d) to exit...", pid)
		cprintf("Waiting for MobFI to close...")
		waitForExit(pid, 45*time.Second)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	lg.Printf("applying update (PATH=%s)", os.Getenv("PATH"))
	res, err := app.New().ApplyUpdate(ctx, target, func(msg string) {
		lg.Printf("  %s", msg)
		cprintf("%s", msg)
	})
	writeUpdateStatus(os.Getenv(envStatus), res, err)
	if err != nil {
		lg.Printf("update FAILED: %v", err)
		cprintf("\nUpdate FAILED: %v", err)
	} else {
		lg.Printf("update OK: %+v", res)
		cprintf("\nUpdate complete. Reopening MobFI...")
	}
	if console != nil {
		time.Sleep(5 * time.Second) // let the user read the result before the window closes
	}
}

// updateLog is a tiny timestamped logger for the detached worker.
type updateLog struct{ f *os.File }

func newUpdateLog() *updateLog {
	p := updateLogPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	f, _ := os.Create(p) // best effort; nil f is tolerated
	return &updateLog{f: f}
}

func (l *updateLog) Printf(format string, a ...any) {
	if l == nil || l.f == nil {
		return
	}
	fmt.Fprintf(l.f, time.Now().Format("15:04:05")+" "+format+"\n", a...)
}

func (l *updateLog) Close() {
	if l != nil && l.f != nil {
		_ = l.f.Close()
	}
}

// updateLogPath is where the worker writes its trace (handy for diagnosing a
// failed update): <UserConfigDir>/MobFI/update.log.
// updateLogPath is where the worker writes its trace (handy for diagnosing a
// failed update): <UserConfigDir>/MobFI/update.log.
func updateLogPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "MobFI", "update.log")
}

// workerReentryWindow is how recently a prior worker run makes a new one abort
// (see the re-entry guard). Legitimate updates are far more than this apart.
const workerReentryWindow = 25 * time.Second

func workerMarkerPath() string {
	return filepath.Join(filepath.Dir(updateLogPath()), "worker-run.txt")
}

// recentWorkerRun reports whether a worker recorded a run within the guard
// window -- the signature of a relaunch loop.
func recentWorkerRun() bool {
	b, err := os.ReadFile(workerMarkerPath())
	if err != nil {
		return false
	}
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(b)))
	if err != nil {
		return false
	}
	return time.Since(t) < workerReentryWindow
}

func markWorkerRun() {
	p := workerMarkerPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.WriteFile(p, []byte(time.Now().Format(time.RFC3339Nano)), 0o644)
}

// startUpdateWorker copies this executable to a temp location and launches it
// detached as the update worker, so the original binary/bundle can be replaced
// while the worker runs. It returns after spawning; the caller then quits.
func startUpdateWorker(target, token string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if r, err := filepath.EvalSymlinks(self); err == nil {
		self = r
	}
	worker, err := copyToTemp(self)
	if err != nil {
		return err
	}

	status := updateStatusPath()
	_ = os.MkdirAll(filepath.Dir(status), 0o755)
	_ = os.Remove(status)

	cmd := exec.Command(worker)
	cmd.Env = append(os.Environ(),
		envWorker+"=1",
		envPPID+"="+strconv.Itoa(os.Getpid()),
		envTarget+"="+target,
		envRelaunch+"="+relaunchTarget(self),
		envStatus+"="+status,
		envToken+"="+token,
	)
	sysDetach(cmd) // platform-specific: detach from the GUI, no console window
	return cmd.Start()
}

// approveUpdate records a fresh one-time approval token (called when the user
// confirms Update now) and returns it to pass to the worker.
func approveUpdate() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b)
	p := approvalTokenPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(p, []byte(tok), 0o600); err != nil {
		return "", err
	}
	return tok, nil
}

// consumeApprovalToken reports whether envTok matches a recent, unused approval
// token, and deletes it (one-time use) whether or not it matched.
func consumeApprovalToken(envTok string) bool {
	p := approvalTokenPath()
	defer os.Remove(p) // one-time use, always
	if envTok == "" {
		return false
	}
	fi, err := os.Stat(p)
	if err != nil || time.Since(fi.ModTime()) > approvalTTL {
		return false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(b)) == envTok
}

func approvalTokenPath() string {
	return filepath.Join(filepath.Dir(updateLogPath()), "update-approval.token")
}

// relaunchEnv is the environment for the relaunched app with the worker control
// vars stripped, so it starts as a NORMAL GUI. If these leaked through, the
// relaunched process would run as another update worker and relaunch again --
// an infinite loop (a fork bomb of app instances).
func relaunchEnv() []string {
	var out []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, envWorker+"=") ||
			strings.HasPrefix(e, envPPID+"=") ||
			strings.HasPrefix(e, envTarget+"=") ||
			strings.HasPrefix(e, envRelaunch+"=") ||
			strings.HasPrefix(e, envStatus+"=") ||
			strings.HasPrefix(e, envToken+"=") {
			continue
		}
		out = append(out, e)
	}
	return out
}

// validateRelaunchTarget returns target if it resolves under the same
// install prefix as the currently-running worker binary; empty string
// otherwise. Prevents an attacker with a stolen approval token from
// steering the post-update relaunch to their own executable.
func validateRelaunchTarget(target string, lg *updateLog) string {
	if target == "" {
		return ""
	}
	self, err := os.Executable()
	if err != nil {
		lg.Printf("cannot resolve os.Executable, refusing relaunch: %v", err)
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	installPrefix := filepath.Dir(self) // usually /Applications/MobFI.app/Contents/MacOS or the install bindir
	// On macOS the target is typically the .app bundle two levels up.
	// Accept any target that starts with the install prefix's parent-of-
	// parent (bundle root) or the install prefix itself.
	bundleRoot := filepath.Dir(filepath.Dir(installPrefix))
	tgt, err := filepath.EvalSymlinks(target)
	if err != nil {
		tgt = filepath.Clean(target)
	}
	if strings.HasPrefix(tgt+string(filepath.Separator), installPrefix+string(filepath.Separator)) ||
		strings.HasPrefix(tgt+string(filepath.Separator), bundleRoot+string(filepath.Separator)) {
		return target
	}
	lg.Printf("refusing relaunch to unrelated target %q (install prefix %q)", target, installPrefix)
	return ""
}

// relaunchTarget is what launchApp should reopen: the .app bundle on macOS
// (so LaunchServices applies its environment), otherwise the executable.
func relaunchTarget(exe string) string {
	if runtime.GOOS == "darwin" {
		app := filepath.Dir(filepath.Dir(filepath.Dir(exe))) // MacOS/<bin> -> .app
		if strings.HasSuffix(app, ".app") {
			return app
		}
	}
	return exe
}

// copyToTemp copies src to a fresh randomly-suffixed file in a per-run
// tempdir (0o700). MFI-UPD-02: a fixed name in os.TempDir() opened without
// O_EXCL is a classic /tmp symlink-race primitive on any shared Unix host
// -- a local attacker planting /tmp/mobfi-update-worker as a symlink to
// e.g. ~/.bashrc would cause the operator's next "Update now" to truncate
// their rc file and write the MobFI binary bytes over it.
func copyToTemp(src string) (string, error) {
	dir, err := os.MkdirTemp("", "mobfi-update-")
	if err != nil {
		return "", err
	}
	name := "worker"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dst := filepath.Join(dir, name)
	in, err := os.Open(src)
	if err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	defer in.Close()
	// O_EXCL guarantees the file did not exist (impossible inside a
	// freshly-minted MkdirTemp with 0o700, but retained as defence in depth
	// against a namespace-collision on future refactors).
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.RemoveAll(dir)
		return "", err
	}
	if err := out.Close(); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	return dst, nil
}

func writeUpdateStatus(path string, res *selfupdate.Result, err error) {
	if path == "" {
		return
	}
	st := updateStatus{}
	switch {
	case err != nil:
		st.OK, st.Message = false, "Update failed: "+err.Error()
	case res != nil:
		st.OK, st.Message = true, res.Message+" MobFI restarted with the new version."
	default:
		st.OK, st.Message = true, "Update complete."
	}
	if b, err := json.Marshal(st); err == nil {
		_ = os.WriteFile(path, b, 0o644)
	}
}

// takeUpdateStatus reads and deletes the status file left by a worker run.
func takeUpdateStatus() (updateStatus, bool) {
	b, err := os.ReadFile(updateStatusPath())
	if err != nil {
		return updateStatus{}, false
	}
	_ = os.Remove(updateStatusPath())
	var st updateStatus
	if json.Unmarshal(b, &st) != nil {
		return updateStatus{}, false
	}
	return st, true
}

func updateStatusPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "MobFI", "last-update.json")
}
