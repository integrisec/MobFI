package transport

import (
	"context"
	"io/fs"
	"reflect"
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
	want := []string{"adb", "-s", "SER", "shell", "id", "-u"}
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
	want := []string{"adb", "-s", "SER", "shell", "run-as", "com.x", "id", "-u"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("run-as Exec args = %v, want %v", got, want)
	}

	a.find(context.Background(), "/data/data/com.x", "-type", "d")
	wantFind := []string{"adb", "-s", "SER", "shell", "run-as", "com.x", "find", "/data/data/com.x", "-type", "d"}
	if !reflect.DeepEqual(got, wantFind) {
		t.Errorf("run-as find args = %v, want %v", got, wantFind)
	}
}

// In su mode the whole on-device command must be grouped and passed to adb as
// a single argument, so adb does not re-split it and break `su -c`.
func TestADBSuWrapsCommands(t *testing.T) {
	var got []string
	a := &adbConn{bin: "adb", serial: "SER", su: true, run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		got = append([]string{name}, args...)
		return nil, nil
	}}
	a.Exec(context.Background(), "ls", "/data/data/com.x")
	want := []string{"adb", "-s", "SER", "shell", "su -c 'ls /data/data/com.x'"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("su Exec args = %v, want %v", got, want)
	}

	a.find(context.Background(), "/data/data/com.x", "-type", "d")
	wantFind := []string{"adb", "-s", "SER", "shell", "su -c 'find /data/data/com.x -type d'"}
	if !reflect.DeepEqual(got, wantFind) {
		t.Errorf("su find args = %v, want %v", got, wantFind)
	}
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
		for i, a := range args {
			if a == "-type" && i+1 < len(args) {
				switch args[i+1] {
				case "d":
					return []byte(walkDirs), nil
				case "f":
					return []byte(walkFiles), nil
				}
			}
		}
		return []byte(walkAll), nil
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
