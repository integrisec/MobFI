//go:build !windows

package main

import (
	"os"
	"os/exec"

	"github.com/creack/pty"
)

// unixPTY drives a session through a Unix pseudo-terminal (creack/pty).
type unixPTY struct {
	f   *os.File
	cmd *exec.Cmd
}

// startPTY starts cmd attached to a new pseudo-terminal.
func startPTY(cmd *exec.Cmd) (ptyConn, error) {
	f, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}
	return &unixPTY{f: f, cmd: cmd}, nil
}

func (p *unixPTY) Read(b []byte) (int, error)  { return p.f.Read(b) }
func (p *unixPTY) Write(b []byte) (int, error) { return p.f.Write(b) }

func (p *unixPTY) Resize(rows, cols uint16) error {
	return pty.Setsize(p.f, &pty.Winsize{Rows: rows, Cols: cols})
}

func (p *unixPTY) Close() error {
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	return p.f.Close()
}
