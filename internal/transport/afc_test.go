package transport

import (
	"context"
	"io"
	"io/fs"
	"os"
	"sort"
	"testing"

	"github.com/integrisec/MobFI/internal/device"
)

// fakeAFC simulates afcclient over an in-memory container:
//
//	/
//	  Documents/          (dir)
//	    a.txt             (file "hello")
//	  Library/            (dir)
//	    Preferences/      (dir, empty)
func fakeAFC(t *testing.T) runner {
	dirs := map[string]bool{
		"/":                    true,
		"/Documents":           true,
		"/Library":             true,
		"/Library/Preferences": true,
	}
	ls := map[string]string{
		"/":                    "Documents\nLibrary\n",
		"/Documents":           "a.txt\n",
		"/Library":             "Preferences\n",
		"/Library/Preferences": "",
	}
	return func(_ context.Context, _ string, args ...string) ([]byte, error) {
		// args = [-u UDID --container BUNDLE <cmd> <path> [dst]]
		cmd, path := args[4], ""
		if len(args) > 5 {
			path = args[5]
		}
		switch cmd {
		case "ls":
			return []byte(ls[path]), nil
		case "info":
			if dirs[path] {
				return []byte("st_ifmt: S_IFDIR\n"), nil
			}
			return []byte("st_ifmt: S_IFREG\nst_size: 5\n"), nil
		case "get":
			if err := os.WriteFile(args[6], []byte("hello"), 0o600); err != nil {
				t.Fatal(err)
			}
			return nil, nil
		}
		return nil, nil
	}
}

func afcTestConn(t *testing.T) *afcConn {
	return &afcConn{bin: "afcclient", udid: "UDID", bundleID: "com.x", scope: "container", run: fakeAFC(t)}
}

func TestAFCConnectorSupports(t *testing.T) {
	c := NewAFCConnector()
	if !c.Supports(device.Device{Platform: device.IOS}) {
		t.Error("should support iOS")
	}
	if c.Supports(device.Device{Platform: device.Android}) {
		t.Error("should not support Android")
	}
}

func TestAFCConnectRequiresBundleID(t *testing.T) {
	c := NewAFCConnector()
	if _, err := c.Connect(context.Background(), device.Device{Platform: device.IOS, ID: "U"}, "", ScopeContainer); err == nil {
		t.Error("expected error when bundle id is empty")
	}
}

func TestAFCConnectRejectsBadScope(t *testing.T) {
	c := NewAFCConnector()
	d := device.Device{Platform: device.IOS, ID: "U"}
	if _, err := c.Connect(context.Background(), d, "com.x", "wrong"); err == nil {
		t.Error("expected error for an invalid scope")
	}
	if _, err := c.Connect(context.Background(), d, "com.x", ScopeDocuments); err != nil {
		t.Errorf("documents scope should be accepted, got %v", err)
	}
}

func TestAFCConnectDefaultsScope(t *testing.T) {
	c := NewAFCConnector()
	conn, err := c.Connect(context.Background(), device.Device{Platform: device.IOS, ID: "U"}, "com.x", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := conn.(*afcConn).scope; got != ScopeContainer {
		t.Errorf("default scope = %q, want %q", got, ScopeContainer)
	}
}

func TestAFCExecUnsupported(t *testing.T) {
	if _, err := afcTestConn(t).Exec(context.Background(), "ls"); err != ErrUnsupported {
		t.Errorf("Exec err = %v, want ErrUnsupported", err)
	}
}

func TestAFCWalk(t *testing.T) {
	var visited []string
	dirs := map[string]bool{}
	err := afcTestConn(t).Walk(context.Background(), "/", func(p string, d fs.DirEntry, err error) error {
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
	sort.Strings(visited)
	want := []string{"/", "/Documents", "/Documents/a.txt", "/Library", "/Library/Preferences"}
	if len(visited) != len(want) {
		t.Fatalf("visited %v, want %v", visited, want)
	}
	for i := range want {
		if visited[i] != want[i] {
			t.Fatalf("visited %v, want %v", visited, want)
		}
	}
	if dirs["/Documents/a.txt"] || !dirs["/Library/Preferences"] {
		t.Errorf("directory classification wrong: %v", dirs)
	}
}

func TestAFCOpen(t *testing.T) {
	rc, err := afcTestConn(t).Open(context.Background(), "/Documents/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello" {
		t.Errorf("read %q, want hello", b)
	}
}
