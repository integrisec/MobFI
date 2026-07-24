//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"

	"github.com/UserExistsError/conpty"
)

// winPTY drives a session through a Windows pseudo console (ConPTY, available
// on Windows 10 1809+). ConPTY handles VT sequences natively, so xterm.js in
// the frontend behaves the same as on a Unix PTY.
type winPTY struct {
	cpty *conpty.ConPty
}

// startPTY launches cmd attached to a new pseudo console. It starts at a
// default 80x24; the frontend issues a Resize once the terminal is measured.
func startPTY(cmd *exec.Cmd) (ptyConn, error) {
	if !conpty.IsConPtyAvailable() {
		return nil, fmt.Errorf("the Console needs ConPTY (Windows 10 1809 or newer)")
	}
	if cmd.Err != nil { // exec.Command couldn't resolve the executable on PATH
		return nil, cmd.Err
	}
	cpty, err := conpty.Start(
		buildCommandLine(cmd),
		conpty.ConPtyDimensions(80, 24),
		conpty.ConPtyEnv(cmd.Env),
	)
	if err != nil {
		return nil, err
	}
	return &winPTY{cpty: cpty}, nil
}

func (p *winPTY) Read(b []byte) (int, error)  { return p.cpty.Read(b) }
func (p *winPTY) Write(b []byte) (int, error) { return p.cpty.Write(b) }

// Resize maps the terminal's rows/cols onto ConPTY's width/height arguments.
func (p *winPTY) Resize(rows, cols uint16) error {
	return p.cpty.Resize(int(cols), int(rows))
}

// Close destroys the pseudo console, which signals the attached child to exit.
func (p *winPTY) Close() error { return p.cpty.Close() }

// buildCommandLine renders an *exec.Cmd as a properly quoted Windows command
// line. cmd.Path is the resolved executable (exec.Command runs LookPath);
// cmd.Args[1:] are its arguments.
func buildCommandLine(cmd *exec.Cmd) string {
	parts := make([]string, 0, len(cmd.Args))
	parts = append(parts, syscall.EscapeArg(cmd.Path))
	for _, a := range cmd.Args[1:] {
		parts = append(parts, syscall.EscapeArg(a))
	}
	return strings.Join(parts, " ")
}
