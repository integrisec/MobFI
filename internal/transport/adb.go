package transport

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os/exec"
	"strings"
	"time"

	"github.com/integrisec/MobFI/internal/device"
	"github.com/integrisec/MobFI/internal/sysproc"
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

// ConnectAsRoot binds a session that runs every command through `su -c` as
// root. It reads a non-debuggable app's private data on a rooted device,
// where run-as is unavailable but the shell can escalate via su. The device
// must grant root to the shell (a Magisk/superuser prompt may appear).
func (c *ADBConnector) ConnectAsRoot(_ context.Context, d device.Device) (Conn, error) {
	if !c.Supports(d) {
		return nil, ErrNoConnector
	}
	if d.ID == "" {
		return nil, errors.New("adb: device has no serial")
	}
	return &adbConn{bin: c.bin(), serial: d.ID, su: true}, nil
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
	// su, when true, runs every on-device command through `su -c` as root.
	// Mutually exclusive with asPackage.
	su bool
	// run executes a command and buffers its stdout. It is a field so
	// tests can stub adb; nil means os/exec. Streaming (Open) always uses
	// os/exec directly.
	run func(ctx context.Context, name string, args ...string) ([]byte, error)
}

func (a *adbConn) exec(ctx context.Context, args ...string) ([]byte, error) {
	if a.run != nil {
		return a.run(ctx, a.bin, args...)
	}
	return sysproc.CommandContext(ctx, a.bin, args...).Output()
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

// wrap builds the argv passed to adb for a device-side command. sub is the adb
// subcommand ("shell" or "exec-out"). `adb shell` and `adb exec-out` always
// concatenate the remaining argv with spaces on the wire and hand the result
// to /system/bin/sh -c on the device, so an unquoted metacharacter (`;`, `|`,
// `` ` ``, `$(...)`, redirects, newlines) in any argv element executes as
// on-device shell code. To defend the device shell from device- or
// operator-supplied strings (filenames returned by `find`, bundle ids,
// argv-injected paths) every argv element is single-quoted for exactly one
// layer of shell parsing and joined with spaces. In su mode the wrapper uses
// `su 0 <argv>` rather than `su -c 'joined'` so su exec's the argv directly
// instead of invoking a second sh -c (which was the "double-shell" defect
// tracked as MFI-CMD-01).
func (a *adbConn) wrap(sub, cmd string, args ...string) []string {
	argv := a.shellCmd(cmd, args...)
	if a.su {
		argv = append([]string{"su", "0"}, argv...)
	}
	return []string{"-s", a.serial, sub, quoteArgv(argv)}
}

// shellQuote single-quotes s for a POSIX shell so it survives one layer of
// sh parsing as one token. An embedded single quote becomes '\'' (close,
// escaped, reopen) which every POSIX sh understands.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// quoteArgv wraps each argv element in shellQuote and joins with spaces so
// the resulting single string tokenises back into the original argv when the
// device's sh -c parses it. Panics on a NUL byte in any element because Go's
// os/exec transports argv through NUL-terminated C strings, so a NUL would be
// silently truncated by exec even before it reached the device.
func quoteArgv(argv []string) string {
	q := make([]string, len(argv))
	for i, s := range argv {
		if strings.IndexByte(s, 0) != -1 {
			panic("transport/adb: NUL byte in argv element (call site must validate first)")
		}
		q[i] = shellQuote(s)
	}
	return strings.Join(q, " ")
}

// Exec runs a shell command on the device via `adb shell`.
func (a *adbConn) Exec(ctx context.Context, cmd string, args ...string) ([]byte, error) {
	return a.exec(ctx, a.wrap("shell", cmd, args...)...)
}

// Open streams a file off the device. `adb exec-out cat` is used rather
// than `adb shell` so the byte stream is not mangled by pseudo-terminal
// newline translation.
func (a *adbConn) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	args := a.wrap("exec-out", "cat", path)
	return startStream(ctx, a.bin, args)
}

// Walk visits every entry under root on the device. It enumerates paths
// with `find` (a second `find -type d` distinguishes directories) and
// honours fs.SkipDir / fs.SkipAll. Permission errors from find are
// tolerated: whatever paths were listed are still walked. A hard transport
// failure (adb died, device unplugged mid-walk) is surfaced as a callback
// with err set on the root so extract.Run records a "partial" outcome
// rather than silently missing half the tree. See MFI-XC-06.
func (a *adbConn) Walk(ctx context.Context, root string, fn fs.WalkDirFunc) error {
	all, err := a.find(ctx, root)
	if len(all) == 0 && err != nil {
		return fn(root, nil, fmt.Errorf("transport find failed at root: %w", err))
	}
	// If find returned SOMETHING but also an error, the underlying transport
	// may have died partway. Report the error via the callback so callers
	// see a non-nil err (rather than silently accepting the partial listing
	// as authoritative).
	if err != nil {
		if werr := fn(root, nil, fmt.Errorf("transport find returned partial results: %w", err)); werr != nil {
			return werr
		}
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
	args := a.wrap("exec-out", "tar", "-cf", "-", "-C", root, ".")
	return startStream(ctx, a.bin, args)
}

// startStream runs an adb streaming command and returns its stdout as a
// ReadCloser, capturing stderr so a non-zero exit surfaces the device's error
// message (e.g. "Permission denied", "run-as: not found") instead of a bare
// "exit status 1".
func startStream(ctx context.Context, bin string, args []string) (io.ReadCloser, error) {
	cmd := sysproc.CommandContext(ctx, bin, args...)
	var errbuf bytes.Buffer
	cmd.Stderr = &errbuf
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &cmdReader{cmd: cmd, stdout: stdout, stderr: &errbuf}, nil
}

// Close releases the session. adb invocations are stateless, so there is
// nothing to tear down.
func (a *adbConn) Close() error { return nil }

func (a *adbConn) find(ctx context.Context, root string, extra ...string) ([]string, error) {
	args := a.wrap("shell", "find", append([]string{root}, extra...)...)
	out, err := a.exec(ctx, args...)
	return parseLines(out), err
}

// mustFind runs find and returns whatever paths it listed, ignoring errors
// (a partial listing due to permission denials is still useful). Callers
// that need to distinguish transport-gone (empty + error) from partial-
// permission (some paths + error) should call find directly. See
// MFI-XC-06: Walk uses find (not mustFind) for the primary listing so a
// transport failure surfaces as an error rather than a silent partial.
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
// the process on Close. If stderr is set, a non-zero exit is annotated with
// the captured stderr text.
type cmdReader struct {
	cmd    *exec.Cmd
	stdout io.ReadCloser
	stderr *bytes.Buffer
}

func (r *cmdReader) Read(p []byte) (int, error) { return r.stdout.Read(p) }

func (r *cmdReader) Close() error {
	_ = r.stdout.Close()
	err := r.cmd.Wait()
	if err != nil && r.stderr != nil {
		if msg := strings.TrimSpace(r.stderr.String()); msg != "" {
			return fmt.Errorf("%w: %s", err, firstLine(msg))
		}
	}
	return err
}

// firstLine returns the first non-empty line of s, so a multi-line stderr
// collapses to a concise reason.
func firstLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			return ln
		}
	}
	return s
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
