//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

var (
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	procAllocConsole = kernel32.NewProc("AllocConsole")
)

// workerConsole allocates a console window for the detached update worker and
// returns a writer to it. On Windows the GUI must close before the update can
// overwrite its own .exe, so -- unlike macOS/Linux, which stream progress into
// the app window -- the worker shows its progress (git pull + rebuild) in this
// separate console instead. Returns nil if a console can't be allocated.
func workerConsole() *os.File {
	if r, _, _ := procAllocConsole.Call(); r == 0 {
		return nil
	}
	f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		return nil
	}
	return f
}

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

// sysDetach launches the update worker detached from the GUI (so it survives
// the GUI exiting) as its own process group. It no longer suppresses a console:
// the worker allocates one (workerConsole) to show the update progress.
func sysDetach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
	}
}

// launchApp reopens the GUI executable, detached. Returns any launch error.
func launchApp(target string) error {
	cmd := exec.Command(target)
	cmd.Env = relaunchEnv() // strip worker vars so it starts as a normal GUI
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NO_WINDOW,
	}
	return cmd.Start()
}
