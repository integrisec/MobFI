package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
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
	aux  *exec.Cmd // e.g. iproxy USB port-forward for iOS SSH
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
	var cmd, aux *exec.Cmd
	sshOpts := []string{"-o", "StrictHostKeyChecking=accept-new", "-o", "UserKnownHostsFile=/dev/null"}

	if platform == "ios" {
		user := firstNonEmpty(sshUser, "root")
		if sshHost != "" {
			// Direct network SSH.
			args := append([]string{"-p", firstNonEmpty(sshPort, "22")}, sshOpts...)
			cmd = exec.Command("ssh", append(args, user+"@"+sshHost)...)
		} else {
			// USB: forward a local port to the device's SSH port with iproxy.
			if deviceID == "" {
				return "", errors.New("select a device (USB SSH forwarding needs a UDID)")
			}
			local, err := freePort()
			if err != nil {
				return "", err
			}
			devicePort := firstNonEmpty(sshPort, "22")
			aux = exec.Command("iproxy", "-u", deviceID, fmt.Sprintf("%d:%s", local, devicePort))
			if err := aux.Start(); err != nil {
				return "", fmt.Errorf("iproxy: %w", err)
			}
			if err := waitPort(local, 3*time.Second); err != nil {
				_ = aux.Process.Kill()
				return "", fmt.Errorf("iproxy forward not ready (is the device jailbroken with sshd on %s?): %w", devicePort, err)
			}
			args := append([]string{"-p", strconv.Itoa(local)}, sshOpts...)
			cmd = exec.Command("ssh", append(args, user+"@127.0.0.1")...)
		}
	} else {
		if deviceID == "" {
			return "", errors.New("select a device")
		}
		cmd = exec.Command("adb", "-s", deviceID, "shell")
	}
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		if aux != nil && aux.Process != nil {
			_ = aux.Process.Kill()
		}
		return "", err
	}

	id := fmt.Sprintf("con-%d", time.Now().UnixNano())
	consolesMu.Lock()
	consoles[id] = &consoleSession{ptmx: ptmx, cmd: cmd, aux: aux}
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
	if s.aux != nil && s.aux.Process != nil {
		_ = s.aux.Process.Kill() // tear down the iproxy forward
	}
	return nil
}

// freePort returns an available local TCP port.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// waitPort blocks until 127.0.0.1:port accepts a connection or the timeout
// elapses (used to wait for iproxy's listener to come up).
func waitPort(port int, timeout time.Duration) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
		if err == nil {
			c.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("timeout")
}
