package main

import (
	"context"
	"encoding/json"
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
// status file, and relaunches the app. The env vars below wire that up.
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
// and relaunches the app. It never opens a Wails window.
func updateWorker() {
	refreshPath() // Windows: pick up registry PATH so go/git/wails resolve

	if pid, err := strconv.Atoi(os.Getenv(envPPID)); err == nil {
		waitForExit(pid, 45*time.Second)
	}
	target := os.Getenv(envTarget)
	if target == "" {
		target = "gui"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	defer cancel()
	res, err := app.New().ApplyUpdate(ctx, target, func(string) {})
	writeUpdateStatus(os.Getenv(envStatus), res, err)

	// Relaunch regardless of success so the user is never left without the app.
	if relaunch := os.Getenv(envRelaunch); relaunch != "" {
		launchApp(relaunch)
	}
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
