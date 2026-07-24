package extract

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// fakeConn is a transport.Conn backed by in-memory tables, so extract.Run
// can be tested without a device.
type fakeConn struct {
	entries  []fakeEntry       // ordered as Walk should visit them
	contents map[string]string // on-device path -> file bytes
	openErr  map[string]error  // on-device path -> error returned by Open
}

type fakeEntry struct {
	path string
	dir  bool
}

func (c *fakeConn) Exec(context.Context, string, ...string) ([]byte, error) { return nil, nil }
func (c *fakeConn) Close() error                                            { return nil }

func (c *fakeConn) Walk(_ context.Context, _ string, fn fs.WalkDirFunc) error {
	for _, e := range c.entries {
		if err := fn(e.path, dirEntry(e), nil); err != nil {
			return err
		}
	}
	return nil
}

func (c *fakeConn) Open(_ context.Context, path string) (io.ReadCloser, error) {
	if err := c.openErr[path]; err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader(c.contents[path])), nil
}

// dirEntry adapts fakeEntry to fs.DirEntry.
type dirEntry fakeEntry

func (e dirEntry) Name() string { return filepath.Base(e.path) }
func (e dirEntry) IsDir() bool  { return e.dir }
func (e dirEntry) Type() fs.FileMode {
	if e.dir {
		return fs.ModeDir
	}
	return 0
}
func (e dirEntry) Info() (fs.FileInfo, error) { return nil, errors.New("no info") }

func TestRunMirrorsTree(t *testing.T) {
	const root = "/data/data/com.x"
	conn := &fakeConn{
		entries: []fakeEntry{
			{root, true},
			{root + "/files", true},
			{root + "/files/a.txt", false},
			{root + "/shared_prefs", true},
			{root + "/shared_prefs/p.xml", false},
			{root + "/secret.key", false},
		},
		contents: map[string]string{
			root + "/files/a.txt":        "hello",
			root + "/shared_prefs/p.xml": "<x/>",
		},
		openErr: map[string]error{
			root + "/secret.key": errors.New("permission denied"),
		},
	}

	dest := t.TempDir()
	res, err := Run(context.Background(), conn, Request{BundleID: "com.x", SourceRoot: root, Dest: dest})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if res.FileCount != 2 {
		t.Errorf("FileCount = %d, want 2", res.FileCount)
	}
	if res.ByteCount != int64(len("hello")+len("<x/>")) {
		t.Errorf("ByteCount = %d, want %d", res.ByteCount, len("hello")+len("<x/>"))
	}
	if len(res.Skipped) != 1 || res.Skipped[0].Path != root+"/secret.key" {
		t.Errorf("Skipped = %+v, want the unreadable secret.key", res.Skipped)
	}

	got := readTree(t, dest)
	want := map[string]string{
		"files/a.txt":        "hello",
		"shared_prefs/p.xml": "<x/>",
	}
	if len(got) != len(want) {
		t.Fatalf("wrote files %v, want %v", keys(got), keys(want))
	}
	for name, content := range want {
		if got[name] != content {
			t.Errorf("%s = %q, want %q", name, got[name], content)
		}
	}
}

func TestRunRejectsPathEscape(t *testing.T) {
	const root = "/data/data/com.x"
	conn := &fakeConn{
		entries: []fakeEntry{
			{root, true},
			{root + "/../../etc/evil", false}, // attempts to escape Dest
		},
		contents: map[string]string{root + "/../../etc/evil": "pwned"},
	}
	dest := t.TempDir()
	res, err := Run(context.Background(), conn, Request{SourceRoot: root, Dest: dest})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.FileCount != 0 {
		t.Errorf("FileCount = %d, want 0 (escape must be skipped)", res.FileCount)
	}
	if len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0].Reason, "escape") {
		t.Errorf("Skipped = %+v, want a path-escape entry", res.Skipped)
	}
}

func TestRunRequiresSourceAndDest(t *testing.T) {
	if _, err := Run(context.Background(), &fakeConn{}, Request{Dest: "x"}); err == nil {
		t.Error("expected error when SourceRoot is empty")
	}
	if _, err := Run(context.Background(), &fakeConn{}, Request{SourceRoot: "/x"}); err == nil {
		t.Error("expected error when Dest is empty")
	}
}

// readTree returns a map of slash-separated relative path -> file contents
// for every file under dir.
func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRunTar(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	writeTar := func(name string, typ byte, body string) {
		hdr := &tar.Header{Name: name, Typeflag: typ, Mode: 0o600, Size: int64(len(body))}
		if typ == tar.TypeDir {
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if body != "" {
			tw.Write([]byte(body))
		}
	}
	writeTar("./files/", tar.TypeDir, "")
	writeTar("./files/a.txt", tar.TypeReg, "hello")
	writeTar("./shared_prefs/p.xml", tar.TypeReg, "<x/>")
	writeTar("./link", tar.TypeSymlink, "")     // must be skipped, not recreated
	writeTar("../escape", tar.TypeReg, "pwned") // must be rejected
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	res, err := RunTar(context.Background(), &buf, Request{Dest: dest})
	if err != nil {
		t.Fatal(err)
	}
	if res.FileCount != 2 {
		t.Errorf("FileCount = %d, want 2", res.FileCount)
	}
	if len(res.Skipped) != 2 {
		t.Errorf("Skipped = %+v, want the symlink and the escape", res.Skipped)
	}

	got := readTree(t, dest)
	if got["files/a.txt"] != "hello" || got["shared_prefs/p.xml"] != "<x/>" {
		t.Errorf("wrote %v", got)
	}
	if len(got) != 2 {
		t.Errorf("wrote %d files, want 2: %v", len(got), keys(got))
	}
}

func keys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func TestWindowsSafeName(t *testing.T) {
	cases := map[string]string{
		"frc_1:248:android:abc_firebase_defaults.json": "frc_1%3A248%3Aandroid%3Aabc_firebase_defaults.json",
		"normal.json":  "normal.json",
		"a<b>c|d?e*f":  "a%3Cb%3Ec%7Cd%3Fe%2Af",
		"trailing.":    "trailing",
		"trailing ":    "trailing",
		"CON":          "_CON",
		"NUL.txt":      "_NUL.txt",
		"com1":         "_com1",
		"not_reserved": "not_reserved",
		".":            ".",
		"..":           "..",
	}
	for in, want := range cases {
		if got := windowsSafeName(in); got != want {
			t.Errorf("windowsSafeName(%q) = %q, want %q", in, got, want)
		}
	}
}
