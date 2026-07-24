//go:build windows

package sysproc

import (
	"os/exec"
	"syscall"
)

// createNoWindow is the Windows CREATE_NO_WINDOW process-creation flag: the
// child runs without allocating a console, so no window flashes.
const createNoWindow = 0x08000000

func configure(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
