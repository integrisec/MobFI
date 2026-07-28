//go:build windows

package main

import (
	"os"
	"os/exec"
)

// execReplace re-runs exe with the same args on Windows (which has no exec-in-
// place), inheriting the console, then exits with the child's status. It sets
// MFI_UPDATED so the relaunched process skips the update check. On success it
// does not return; it returns an error only if the child could not be started.
func execReplace(exe string) error {
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), "MFI_UPDATED=1")
	err := cmd.Run()
	if err == nil {
		os.Exit(0)
	}
	if ee, ok := err.(*exec.ExitError); ok {
		os.Exit(ee.ExitCode())
	}
	return err
}
