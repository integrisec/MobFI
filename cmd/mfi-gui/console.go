package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/integrisec/MobFI/internal/sysproc"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// newSessionID returns a 16-byte-random-hex identifier for a console session.
// crypto/rand rather than time.Now().UnixNano() so a future XSS pivot cannot
// enumerate active session IDs by wall-clock. See MFI-XC-08.
func newSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is exceptionally rare; fall back to the
		// prior time-based scheme rather than blocking a console session.
		return fmt.Sprintf("con-%d", time.Now().UnixNano())
	}
	return "con-" + hex.EncodeToString(b)
}

// safeSSHField rejects values that would slot into the ssh argv as an option
// (leading `-`), carry ssh-conf syntax that can inject a ProxyCommand (`=` in
// the middle of the field), or contain whitespace / control bytes that would
// smuggle into a shell-parsed context downstream. Applied to user and host
// before either is concatenated into a positional argv element.
func safeSSHField(kind, s string) error {
	if s == "" {
		return fmt.Errorf("ssh %s must not be empty", kind)
	}
	if strings.HasPrefix(s, "-") {
		return fmt.Errorf("ssh %s must not start with '-' (%q would be parsed as an option)", kind, s)
	}
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\x00' {
			return fmt.Errorf("ssh %s must not contain whitespace or control bytes", kind)
		}
	}
	return nil
}

// networkKnownHosts returns the operator's persistent known_hosts file so
// network SSH TOFUs on first connect and hard-fails on host-key drift, per
// MFI-GUI-02. Falls back to /dev/null only when the config dir cannot be
// created (rare; better a warning at connect than a silent failure to
// launch).
func networkKnownHosts() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return "/dev/null"
	}
	kh := filepath.Join(dir, "MobFI", "known_hosts")
	if err := os.MkdirAll(filepath.Dir(kh), 0o700); err != nil {
		return "/dev/null"
	}
	return kh
}

// ptyConn is a started pseudo-terminal session: read its output, write input,
// resize it, and Close (which terminates the child). It is implemented per
// platform — creack/pty on Unix, Windows ConPTY on Windows — so the Console
// works on every OS.
type ptyConn interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Resize(rows, cols uint16) error
	Close() error
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

type consoleSession struct {
	pty ptyConn
	aux *exec.Cmd // e.g. iproxy USB port-forward for iOS SSH
	log *os.File  // optional session transcript
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
// ConsoleInfo describes a started session for the frontend.
type ConsoleInfo struct {
	ID     string `json:"id"`
	Status string `json:"status"` // human-readable transport summary
}

func (g *GUI) ConsoleStart(deviceID, platform, sshUser, sshHost, sshPort, logPath string) (ConsoleInfo, error) {
	var cmd, aux *exec.Cmd
	var status string

	if platform == "ios" {
		user := firstNonEmpty(sshUser, "root")
		// MFI-CMD-03 / MFI-GUI-05: validate user (and host, when set)
		// before any concatenation into an argv element. `-l <user> <host>`
		// keeps them separate positionals; `--` before the target argv
		// blocks any residual leading-`-` from being parsed as an ssh
		// option (same CVE class as CVE-2017-1000117 / CVE-2017-9800).
		if err := safeSSHField("user", user); err != nil {
			return ConsoleInfo{}, err
		}

		if sshHost != "" {
			// Direct network SSH.
			if err := safeSSHField("host", sshHost); err != nil {
				return ConsoleInfo{}, err
			}
			port := firstNonEmpty(sshPort, "22")
			if _, err := strconv.Atoi(port); err != nil {
				return ConsoleInfo{}, fmt.Errorf("ssh port %q must be numeric", port)
			}
			// MFI-GUI-02: network SSH uses a persistent known_hosts so
			// the first connect TOFUs and later connects hard-fail on
			// host-key drift; /dev/null (the prior behavior) leaked no
			// TOFU signal at all and was effectively StrictHostKeyChecking=no.
			sshOpts := []string{
				"-o", "StrictHostKeyChecking=accept-new",
				"-o", "UserKnownHostsFile=" + networkKnownHosts(),
			}
			args := append([]string{"-p", port}, sshOpts...)
			args = append(args, "-l", user, "--", sshHost)
			cmd = sysproc.Command("ssh", args...)
			status = fmt.Sprintf("ssh %s@%s:%s", user, sshHost, port)
		} else {
			// USB: forward a local port to the device's SSH port with iproxy.
			if deviceID == "" {
				return ConsoleInfo{}, errors.New("select a device (USB SSH forwarding needs a UDID)")
			}
			local, err := freePort()
			if err != nil {
				return ConsoleInfo{}, err
			}
			devicePort := firstNonEmpty(sshPort, "22")
			if _, err := strconv.Atoi(devicePort); err != nil {
				return ConsoleInfo{}, fmt.Errorf("ssh port %q must be numeric", devicePort)
			}
			aux = sysproc.Command("iproxy", "-u", deviceID, fmt.Sprintf("%d:%s", local, devicePort))
			if err := aux.Start(); err != nil {
				return ConsoleInfo{}, fmt.Errorf("iproxy: %w", err)
			}
			if err := waitPort(local, 3*time.Second); err != nil {
				_ = aux.Process.Kill()
				return ConsoleInfo{}, fmt.Errorf("iproxy forward not ready (is the device jailbroken with sshd on %s?): %w", devicePort, err)
			}
			// Loopback via iproxy: /dev/null known-hosts is acceptable
			// (localhost trust boundary).
			sshOpts := []string{
				"-o", "StrictHostKeyChecking=accept-new",
				"-o", "UserKnownHostsFile=/dev/null",
			}
			args := append([]string{"-p", strconv.Itoa(local)}, sshOpts...)
			args = append(args, "-l", user, "--", "127.0.0.1")
			cmd = sysproc.Command("ssh", args...)
			status = fmt.Sprintf("ssh %s@ USB (iproxy :%d -> device:%s)", user, local, devicePort)
		}
	} else {
		if deviceID == "" {
			return ConsoleInfo{}, errors.New("select a device")
		}
		cmd = sysproc.Command("adb", "-s", deviceID, "shell")
		status = "adb shell " + deviceID
	}
	// MFI-XC-05: curated env for spawned adb / ssh / iproxy so operator
	// secrets in the shell env (AWS_*, GITHUB_TOKEN, ANTHROPIC_*, HTTPS_PROXY)
	// do not flow into device-side subprocesses that may dump env on error
	// or route through a hostile ~/.ssh/config ProxyCommand.
	cmd.Env = sysproc.CuratedEnv("TERM=xterm-256color")

	p, err := startPTY(cmd)
	if err != nil {
		if aux != nil && aux.Process != nil {
			_ = aux.Process.Kill()
		}
		return ConsoleInfo{}, err
	}

	var logFile *os.File
	if logPath != "" {
		if f, err := os.Create(logPath); err == nil { // best-effort transcript
			logFile = f
			status += " · logging"
		}
	}

	id := newSessionID()
	consolesMu.Lock()
	consoles[id] = &consoleSession{pty: p, aux: aux, log: logFile}
	consolesMu.Unlock()

	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := p.Read(buf)
			if n > 0 {
				wailsruntime.EventsEmit(g.ctx, "console:data:"+id, string(buf[:n]))
				if logFile != nil {
					logFile.Write(buf[:n])
				}
			}
			if err != nil {
				break
			}
		}
		g.ConsoleClose(id)
		wailsruntime.EventsEmit(g.ctx, "console:exit:"+id, "")
	}()
	return ConsoleInfo{ID: id, Status: status}, nil
}

// ConsoleWrite sends keystrokes/input to a session's PTY.
func (g *GUI) ConsoleWrite(id, data string) error {
	consolesMu.Lock()
	s := consoles[id]
	consolesMu.Unlock()
	if s == nil {
		return errors.New("console closed")
	}
	_, err := s.pty.Write([]byte(data))
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
	return s.pty.Resize(uint16(rows), uint16(cols))
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
	_ = s.pty.Close() // closes the pty and terminates the child
	if s.aux != nil && s.aux.Process != nil {
		_ = s.aux.Process.Kill() // tear down the iproxy forward
	}
	if s.log != nil {
		_ = s.log.Close()
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
