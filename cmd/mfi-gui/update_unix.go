//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// waitForExit polls until process pid is gone or timeout elapses. Signal 0
// does not deliver a signal; it only checks that the process still exists.
func waitForExit(pid int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		p, err := os.FindProcess(pid)
		if err != nil {
			return
		}
		if err := p.Signal(syscall.Signal(0)); err != nil {
			return // process no longer exists
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// sysDetach puts the child in its own session so it survives the GUI exiting.
func sysDetach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// launchApp reopens the app: `open -n` the bundle on macOS, exec the binary on
// Linux (detached so it outlives the worker). Returns any launch error.
func launchApp(target string) error {
	if runtime.GOOS == "darwin" {
		// Run (not Start) so `open`'s non-zero exit surfaces as an error.
		out, err := exec.Command("open", "-n", target).CombinedOutput()
		if err == nil {
			return nil
		}
		openErr := fmt.Errorf("open %s: %v: %s", target, err, strings.TrimSpace(string(out)))
		// `open` can fail from a detached (setsid) process with no Aqua session.
		// Fall back to exec'ing the bundle's inner binary directly -- with the
		// worker env stripped so it starts as a normal GUI, not another worker.
		if bin := macAppBinary(target); bin != "" {
			c := exec.Command(bin)
			c.Env = relaunchEnv()
			c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			if e2 := c.Start(); e2 == nil {
				return nil
			}
		}
		return openErr
	}
	c := exec.Command(target)
	c.Env = relaunchEnv()
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return c.Start()
}

// macAppBinary returns the first executable inside an .app's Contents/MacOS,
// or "" if none -- used as a fallback when `open` cannot launch the bundle.
func macAppBinary(appPath string) string {
	dir := filepath.Join(appPath, "Contents", "MacOS")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}
