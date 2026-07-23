package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

type consoleSession struct {
	ptmx *os.File
	cmd  *exec.Cmd
}

var (
	consolesMu sync.Mutex
	consoles   = map[string]*consoleSession{}
)

// ConsoleStart opens an interactive PTY session and streams its output to the
// frontend as "console:data:<id>" events (and "console:exit:<id>" on end).
// Android uses `adb -s <serial> shell`; iOS uses `ssh <user>@<host>` and
// therefore needs a jailbroken device running sshd (password prompts are
// handled interactively in the terminal). Returns the session id.
func (g *GUI) ConsoleStart(deviceID, platform, sshUser, sshHost, sshPort string) (string, error) {
	var cmd *exec.Cmd
	if platform == "ios" {
		if sshHost == "" {
			return "", errors.New("SSH host required — the iOS console needs a jailbroken device running sshd")
		}
		user := firstNonEmpty(sshUser, "root")
		port := firstNonEmpty(sshPort, "22")
		cmd = exec.Command("ssh",
			"-p", port,
			"-o", "StrictHostKeyChecking=accept-new",
			"-o", "UserKnownHostsFile=/dev/null",
			user+"@"+sshHost)
	} else {
		if deviceID == "" {
			return "", errors.New("select a device")
		}
		cmd = exec.Command("adb", "-s", deviceID, "shell")
	}
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return "", err
	}

	id := fmt.Sprintf("con-%d", time.Now().UnixNano())
	consolesMu.Lock()
	consoles[id] = &consoleSession{ptmx: ptmx, cmd: cmd}
	consolesMu.Unlock()

	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				wailsruntime.EventsEmit(g.ctx, "console:data:"+id, string(buf[:n]))
			}
			if err != nil {
				break
			}
		}
		g.ConsoleClose(id)
		wailsruntime.EventsEmit(g.ctx, "console:exit:"+id, "")
	}()
	return id, nil
}

// ConsoleWrite sends keystrokes/input to a session's PTY.
func (g *GUI) ConsoleWrite(id, data string) error {
	consolesMu.Lock()
	s := consoles[id]
	consolesMu.Unlock()
	if s == nil {
		return errors.New("console closed")
	}
	_, err := s.ptmx.Write([]byte(data))
	return err
}

// ConsoleResize updates the PTY window size.
func (g *GUI) ConsoleResize(id string, rows, cols int) error {
	consolesMu.Lock()
	s := consoles[id]
	consolesMu.Unlock()
	if s == nil || rows <= 0 || cols <= 0 {
		return nil
	}
	return pty.Setsize(s.ptmx, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})
}

// ConsoleClose ends a session.
func (g *GUI) ConsoleClose(id string) error {
	consolesMu.Lock()
	s := consoles[id]
	delete(consoles, id)
	consolesMu.Unlock()
	if s == nil {
		return nil
	}
	_ = s.ptmx.Close()
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	return nil
}
