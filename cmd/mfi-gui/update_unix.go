//go:build !windows

package main

import (
	"os"
	"os/exec"
	"runtime"
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

// launchApp reopens the app: `open` the bundle on macOS, exec the binary on
// Linux (detached so it outlives the worker).
func launchApp(target string) {
	if runtime.GOOS == "darwin" {
		_ = exec.Command("open", target).Start()
		return
	}
	c := exec.Command(target)
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	_ = c.Start()
}
