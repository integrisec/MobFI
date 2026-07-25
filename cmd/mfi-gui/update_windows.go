//go:build windows

package main

import (
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// waitForExit blocks until process pid exits or timeout elapses. If the process
// is already gone, OpenProcess fails and we return immediately.
func waitForExit(pid int, timeout time.Duration) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_, _ = windows.WaitForSingleObject(h, uint32(timeout/time.Millisecond))
}

// sysDetach launches the child detached from the GUI and without a console
// window, so it survives the GUI exiting and never flashes a terminal.
func sysDetach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW,
	}
}

// launchApp reopens the GUI executable, detached.
func launchApp(target string) {
	cmd := exec.Command(target)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NO_WINDOW,
	}
	_ = cmd.Start()
}
