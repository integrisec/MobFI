package main

import (
	"context"
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
)

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

	relaunch := os.Getenv(envRelaunch)
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

	refreshPath() // resolve go/git/wails via the login-shell / registry PATH
	if pid, err := strconv.Atoi(os.Getenv(envPPID)); err == nil {
		lg.Printf("waiting for GUI (pid %d) to exit...", pid)
		waitForExit(pid, 45*time.Second)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	lg.Printf("applying update (PATH=%s)", os.Getenv("PATH"))
	res, err := app.New().ApplyUpdate(ctx, target, func(msg string) { lg.Printf("  %s", msg) })
	writeUpdateStatus(os.Getenv(envStatus), res, err)
	if err != nil {
		lg.Printf("update FAILED: %v", err)
	} else {
		lg.Printf("update OK: %+v", res)
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

func updateLogPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "MobFI", "update.log")
}

// startUpdateWorker copies this executable to a temp location and launches it
// detached as the update worker, so the original binary/bundle can be replaced
// while the worker runs. It returns after spawning; the caller then quits.
func startUpdateWorker(target string) error {
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
	)
	sysDetach(cmd) // platform-specific: detach from the GUI, no console window
	return cmd.Start()
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
			strings.HasPrefix(e, envStatus+"=") {
			continue
		}
		out = append(out, e)
	}
	return out
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

func copyToTemp(src string) (string, error) {
	name := "mobfi-update-worker"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dst := filepath.Join(os.TempDir(), name)
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
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
