//go:build !windows

package main

import (
	"os"
	"syscall"
)

// execReplace replaces the current process image with exe (re-run with the same
// args), setting MFI_UPDATED so the relaunched process skips the update check.
// On success it does not return; it returns an error only if exec fails.
func execReplace(exe string) error {
	return syscall.Exec(exe, os.Args, append(os.Environ(), "MFI_UPDATED=1"))
}
