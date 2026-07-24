// Package sysproc builds exec.Cmd values that never flash a console window on
// Windows. A GUI process has no console of its own, so each child console
// program (adb, idevice*, aapt, ...) would otherwise pop a visible window --
// and MobFI polls devices on a timer, so those windows spawn continuously.
// On non-Windows platforms these are thin pass-throughs to os/exec.
package sysproc

import (
	"context"
	"os/exec"
)

// Command is exec.Command, but hides the child's console window on Windows.
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	configure(cmd)
	return cmd
}

// CommandContext is exec.CommandContext with the same Windows behaviour.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	configure(cmd)
	return cmd
}
