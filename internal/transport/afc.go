package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/integrisec/MobFI/internal/device"
	"github.com/integrisec/MobFI/internal/sysproc"
)

// ErrUnsupported is returned by a Conn for operations its transport cannot
// perform (e.g. running a shell command over AFC).
var ErrUnsupported = errors.New("transport: operation not supported")

// errSkipAll unwinds a Walk when the callback returns fs.SkipAll.
var errSkipAll = errors.New("skip all")

type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

func execRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	return sysproc.CommandContext(ctx, name, args...).Output()
}

// House-arrest scopes for AFC access.
const (
	// ScopeContainer sees the whole app sandbox (needs a dev-signed app or
	// a jailbroken device).
	ScopeContainer = "container"
	// ScopeDocuments sees only Documents/, but works for more apps.
	ScopeDocuments = "documents"
)

// AFCConnector reaches an iOS application's data container over AFC (Apple
// File Conduit), via libimobiledevice's `afcclient` house-arrest client.
// Unlike a device-wide connector, an AFC session is scoped to one app, so
// Connect takes the bundle id.
type AFCConnector struct {
	// Bin is the afcclient executable. Empty means "afcclient" from PATH.
	Bin string
	// Scope is the default house-arrest area when Connect is passed an
	// empty scope. Empty means ScopeContainer.
	Scope string
	run   runner // stubbable in tests; nil means os/exec
}

// NewAFCConnector returns an AFCConnector using afcclient from PATH.
func NewAFCConnector() *AFCConnector { return &AFCConnector{} }

// Supports reports whether d is an iOS device this connector can reach.
func (c *AFCConnector) Supports(d device.Device) bool {
	return d.Platform == device.IOS
}

// Connect opens an AFC session to the app identified by bundleID on d. An
// empty scope falls back to the connector's default (then ScopeContainer).
func (c *AFCConnector) Connect(_ context.Context, d device.Device, bundleID, scope string) (Conn, error) {
	if !c.Supports(d) {
		return nil, ErrNoConnector
	}
	if bundleID == "" {
		return nil, errors.New("afc: bundle id is required")
	}
	if scope == "" {
		scope = c.Scope
	}
	if scope == "" {
		scope = ScopeContainer
	}
	if scope != ScopeContainer && scope != ScopeDocuments {
		return nil, fmt.Errorf("afc: invalid scope %q (want %q or %q)", scope, ScopeContainer, ScopeDocuments)
	}
	bin := c.Bin
	if bin == "" {
		bin = "afcclient"
	}
	run := c.run
	if run == nil {
		run = execRun
	}
	return &afcConn{bin: bin, udid: d.ID, bundleID: bundleID, scope: scope, run: run}, nil
}

// afcConn is a transport.Conn backed by afcclient. Every operation is a
// separate `afcclient -u <udid> --<scope> <bundleID> <cmd> ...` invocation.
type afcConn struct {
	bin      string
	udid     string
	bundleID string
	scope    string
	run      runner
}

func (c *afcConn) afc(ctx context.Context, args ...string) ([]byte, error) {
	full := append([]string{"-u", c.udid, "--" + c.scope, c.bundleID}, args...)
	return c.run(ctx, c.bin, full...)
}

// Exec is not supported: AFC exposes files, not a shell.
func (c *afcConn) Exec(context.Context, string, ...string) ([]byte, error) {
	return nil, ErrUnsupported
}

// Open downloads a file from the container to a temp file and streams it;
// afcclient writes to a local path rather than stdout. The temp file is
// removed on Close.
func (c *afcConn) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	tmp, err := os.CreateTemp("", "mobfi-afc-*")
	if err != nil {
		return nil, err
	}
	tmp.Close()
	if _, err := c.afc(ctx, "get", path, tmp.Name()); err != nil {
		os.Remove(tmp.Name())
		return nil, err
	}
	f, err := os.Open(tmp.Name())
	if err != nil {
		os.Remove(tmp.Name())
		return nil, err
	}
	return &tmpFileReader{f: f, path: tmp.Name()}, nil
}

// Walk traverses the container depth-first. Failure to list the root is
// fatal (surfaces a missing afcclient or an inaccessible container);
// deeper enumeration failures are tolerated so a single unreadable
// directory does not abort the extraction.
func (c *afcConn) Walk(ctx context.Context, root string, fn fs.WalkDirFunc) error {
	if root == "" {
		root = "/"
	}
	if err := fn(root, &afcDirEntry{name: deviceBase(root), dir: true}, nil); err != nil {
		if err == fs.SkipDir || err == fs.SkipAll {
			return nil
		}
		return err
	}
	children, err := c.list(ctx, root)
	if err != nil {
		return err
	}
	for _, ch := range children {
		if err := c.walkChild(ctx, deviceJoin(root, ch.name), ch.dir, fn); err != nil {
			if err == errSkipAll {
				return nil
			}
			return err
		}
	}
	return nil
}

func (c *afcConn) walkChild(ctx context.Context, path string, isDir bool, fn fs.WalkDirFunc) error {
	if err := fn(path, &afcDirEntry{name: deviceBase(path), dir: isDir}, nil); err != nil {
		switch err {
		case fs.SkipDir:
			return nil
		case fs.SkipAll:
			return errSkipAll
		default:
			return err
		}
	}
	if !isDir {
		return nil
	}
	children, err := c.list(ctx, path)
	if err != nil {
		return nil // tolerate an unreadable subdirectory
	}
	for _, ch := range children {
		if err := c.walkChild(ctx, deviceJoin(path, ch.name), ch.dir, fn); err != nil {
			return err
		}
	}
	return nil
}

// Close releases the session. afcclient invocations are stateless.
func (c *afcConn) Close() error { return nil }

type afcEntry struct {
	name string
	dir  bool
}

// list returns the entries directly under path, classifying each as file
// or directory via `afcclient info`.
func (c *afcConn) list(ctx context.Context, path string) ([]afcEntry, error) {
	out, err := c.afc(ctx, "ls", path)
	if err != nil {
		return nil, err
	}
	var entries []afcEntry
	for _, name := range parseLines(out) {
		if name == "." || name == ".." {
			continue
		}
		dir, err := c.isDir(ctx, deviceJoin(path, name))
		if err != nil {
			continue // skip entries we cannot stat
		}
		entries = append(entries, afcEntry{name: name, dir: dir})
	}
	return entries, nil
}

// isDir reports whether path is a directory, from `afcclient info` output
// (which includes a line like "st_ifmt: S_IFDIR").
func (c *afcConn) isDir(ctx context.Context, path string) (bool, error) {
	out, err := c.afc(ctx, "info", path)
	if err != nil {
		return false, err
	}
	return bytes.Contains(out, []byte("S_IFDIR")), nil
}

// tmpFileReader reads a downloaded temp file and deletes it on Close.
type tmpFileReader struct {
	f    *os.File
	path string
}

func (r *tmpFileReader) Read(p []byte) (int, error) { return r.f.Read(p) }

func (r *tmpFileReader) Close() error {
	err := r.f.Close()
	os.Remove(r.path)
	return err
}

// afcDirEntry is a minimal fs.DirEntry for a container path.
type afcDirEntry struct {
	name string
	dir  bool
}

func (e *afcDirEntry) Name() string { return e.name }
func (e *afcDirEntry) IsDir() bool  { return e.dir }

func (e *afcDirEntry) Type() fs.FileMode {
	if e.dir {
		return fs.ModeDir
	}
	return 0
}

func (e *afcDirEntry) Info() (fs.FileInfo, error) { return afcFileInfo{e}, nil }

type afcFileInfo struct{ e *afcDirEntry }

func (i afcFileInfo) Name() string       { return i.e.name }
func (i afcFileInfo) Size() int64        { return 0 }
func (i afcFileInfo) Mode() fs.FileMode  { return i.e.Type() }
func (i afcFileInfo) ModTime() time.Time { return time.Time{} }
func (i afcFileInfo) IsDir() bool        { return i.e.dir }
func (i afcFileInfo) Sys() any           { return nil }

func deviceJoin(dir, name string) string {
	if strings.HasSuffix(dir, "/") {
		return dir + name
	}
	return dir + "/" + name
}
