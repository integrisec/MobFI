//go:build !windows

package sysproc

import "os/exec"

// configure is a no-op off Windows: there is no console window to hide.
func configure(*exec.Cmd) {}
