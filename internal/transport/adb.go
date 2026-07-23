package transport

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os/exec"
	"strings"
	"time"

	"github.com/integrisec/MobFI/internal/device"
)

// ADBConnector opens sessions to Android devices over the adb bridge. Each
// operation is a separate `adb -s <serial> ...` invocation, so there is no
// long-lived connection to manage.
type ADBConnector struct {
	// Bin is the adb executable to invoke. Empty means "adb" from PATH.
	Bin string
}

// NewADBConnector returns an ADBConnector that invokes adb from PATH.
func NewADBConnector() *ADBConnector { return &ADBConnector{} }

func (c *ADBConnector) bin() string {
	if c.Bin != "" {
		return c.Bin
	}
	return "adb"
}

// Supports reports whether d is an Android device this connector can reach.
func (c *ADBConnector) Supports(d device.Device) bool {
	return d.Platform == device.Android
}

// Connect binds a device-wide session to d (commands run as the `shell`
// user). It does not spawn a process; failures surface when an operation
// actually runs.
func (c *ADBConnector) Connect(_ context.Context, d device.Device) (Conn, error) {
	return c.connect(d, "")
}

// ConnectAs binds a session that runs every command through
// `run-as <pkg>`, executing as the app's own uid. This is how a
// non-rooted device reads a debuggable app's private /data/data/<pkg>
// files; on a device where the shell user already has access, use Connect.
func (c *ADBConnector) ConnectAs(_ context.Context, d device.Device, pkg string) (Conn, error) {
	if pkg == "" {
		return nil, errors.New("adb: package required for run-as")
	}
	return c.connect(d, pkg)
}

func (c *ADBConnector) connect(d device.Device, pkg string) (Conn, error) {
	if !c.Supports(d) {
		return nil, ErrNoConnector
	}
	if d.ID == "" {
		return nil, errors.New("adb: device has no serial")
	}
	return &adbConn{bin: c.bin(), serial: d.ID, asPackage: pkg}, nil
}

// adbConn is a transport.Conn backed by the adb CLI.
type adbConn struct {
	bin    string
	serial string
	// asPackage, when set, wraps every on-device command in
	// `run-as <asPackage>` so it executes as that app's uid.
	asPackage string
	// run executes a command and buffers its stdout. It is a field so
	// tests can stub adb; nil means os/exec. Streaming (Open) always uses
	// os/exec directly.
	run func(ctx context.Context, name string, args ...string) ([]byte, error)
}

func (a *adbConn) exec(ctx context.Context, args ...string) ([]byte, error) {
	if a.run != nil {
		return a.run(ctx, a.bin, args...)
	}
	return exec.CommandContext(ctx, a.bin, args...).Output()
}

// shellCmd builds the on-device command, prefixing run-as when app-scoped.
func (a *adbConn) shellCmd(cmd string, args ...string) []string {
	out := make([]string, 0, len(args)+3)
	if a.asPackage != "" {
		out = append(out, "run-as", a.asPackage)
	}
	out = append(out, cmd)
	return append(out, args...)
}

// Exec runs a shell command on the device via `adb shell`.
func (a *adbConn) Exec(ctx context.Context, cmd string, args ...string) ([]byte, error) {
	full := append([]string{"-s", a.serial, "shell"}, a.shellCmd(cmd, args...)...)
	return a.exec(ctx, full...)
}

// Open streams a file off the device. `adb exec-out cat` is used rather
// than `adb shell` so the byte stream is not mangled by pseudo-terminal
// newline translation.
func (a *adbConn) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	args := append([]string{"-s", a.serial, "exec-out"}, a.shellCmd("cat", path)...)
	cmd := exec.CommandContext(ctx, a.bin, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &cmdReader{cmd: cmd, stdout: stdout}, nil
}

// Walk visits every entry under root on the device. It enumerates paths
// with `find` (a second `find -type d` distinguishes directories) and
// honours fs.SkipDir / fs.SkipAll. Permission errors from find are
// tolerated: whatever paths were listed are still walked.
func (a *adbConn) Walk(ctx context.Context, root string, fn fs.WalkDirFunc) error {
	all, err := a.find(ctx, root)
	if len(all) == 0 && err != nil {
		return fn(root, nil, err)
	}
	isDir := toBoolSet(a.mustFind(ctx, root, "-type", "d"))
	isFile := toBoolSet(a.mustFind(ctx, root, "-type", "f"))

	skipPrefix := ""
	for _, p := range all {
		if skipPrefix != "" && strings.HasPrefix(p, skipPrefix) {
			continue
		}
		skipPrefix = ""
		dir := isDir[p]
		// Only visit directories and regular files. Skipping symlinks,
		// sockets and FIFOs avoids `cat` blocking forever on a special file
		// (and avoids following symlinks out of the tree).
		if !dir && !isFile[p] {
			continue
		}
		entry := &adbDirEntry{name: deviceBase(p), dir: dir}
		switch werr := fn(p, entry, nil); werr {
		case nil:
			// continue
		case fs.SkipDir:
			if entry.dir {
				skipPrefix = ensureSlash(p)
			}
		case fs.SkipAll:
			return nil
		default:
			return werr
		}
	}
	return nil
}

// TarReader streams a tar of root's contents from the device in a single
// `adb exec-out [run-as <pkg>] tar` process — one process for the whole
// tree rather than one `cat` per file. Entries are relative to root (via
// `-C`). Requires `tar` on the device (toybox, Android 6+); callers should
// fall back to Walk/Open if this yields nothing.
func (a *adbConn) TarReader(ctx context.Context, root string) (io.ReadCloser, error) {
	args := append([]string{"-s", a.serial, "exec-out"}, a.shellCmd("tar", "-cf", "-", "-C", root, ".")...)
	cmd := exec.CommandContext(ctx, a.bin, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &cmdReader{cmd: cmd, stdout: stdout}, nil
}

// Close releases the session. adb invocations are stateless, so there is
// nothing to tear down.
func (a *adbConn) Close() error { return nil }

func (a *adbConn) find(ctx context.Context, root string, extra ...string) ([]string, error) {
	args := append([]string{"-s", a.serial, "shell"}, a.shellCmd("find", append([]string{root}, extra...)...)...)
	out, err := a.exec(ctx, args...)
	return parseLines(out), err
}

// mustFind runs find and returns whatever paths it listed, ignoring errors
// (a partial listing due to permission denials is still useful).
func (a *adbConn) mustFind(ctx context.Context, root string, extra ...string) []string {
	paths, _ := a.find(ctx, root, extra...)
	return paths
}

func toBoolSet(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

// cmdReader adapts a running command's stdout to an io.ReadCloser, reaping
// the process on Close.
type cmdReader struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
}

func (r *cmdReader) Read(p []byte) (int, error) { return r.stdout.Read(p) }

func (r *cmdReader) Close() error {
	_ = r.stdout.Close()
	return r.cmd.Wait()
}

// adbDirEntry is a minimal fs.DirEntry for a path listed on-device. Only
// name and directory-ness are known from `find`; richer metadata would
// need a per-file stat.
type adbDirEntry struct {
	name string
	dir  bool
}

func (e *adbDirEntry) Name() string { return e.name }
func (e *adbDirEntry) IsDir() bool  { return e.dir }

func (e *adbDirEntry) Type() fs.FileMode {
	if e.dir {
		return fs.ModeDir
	}
	return 0
}

func (e *adbDirEntry) Info() (fs.FileInfo, error) { return adbFileInfo{e}, nil }

type adbFileInfo struct{ e *adbDirEntry }

func (i adbFileInfo) Name() string       { return i.e.name }
func (i adbFileInfo) Size() int64        { return 0 }
func (i adbFileInfo) Mode() fs.FileMode  { return i.e.Type() }
func (i adbFileInfo) ModTime() time.Time { return time.Time{} }
func (i adbFileInfo) IsDir() bool        { return i.e.dir }
func (i adbFileInfo) Sys() any           { return nil }

// parseLines splits command output into non-empty, trimmed lines.
func parseLines(out []byte) []string {
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if line := strings.TrimRight(sc.Text(), "\r"); strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// deviceBase is filepath.Base for always-'/'-separated device paths (it
// must not use the host's separator).
func deviceBase(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

func ensureSlash(p string) string {
	if strings.HasSuffix(p, "/") {
		return p
	}
	return p + "/"
}
