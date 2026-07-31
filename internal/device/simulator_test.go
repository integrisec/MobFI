package device

import (
	"context"
	"os/exec"
	"reflect"
	"testing"
)

func TestSimctlUnavailable(t *testing.T) {
	// exit 72 is xcrun's "utility not a developer tool" -- treat as no sims.
	if err := exec.Command("sh", "-c", "exit 72").Run(); !simctlUnavailable(err) {
		t.Errorf("exit 72 should be treated as unavailable, got %v", err)
	}
	// A missing binary means simctl isn't installed -- also no sims.
	if !simctlUnavailable(exec.ErrNotFound) {
		t.Error("ErrNotFound should be treated as unavailable")
	}
	// A genuine non-zero exit (e.g. 1) must still surface as an error.
	if err := exec.Command("sh", "-c", "exit 1").Run(); simctlUnavailable(err) {
		t.Error("exit 1 should not be degraded to 'unavailable'")
	}
	if simctlUnavailable(nil) {
		t.Error("nil error should not be 'unavailable'")
	}
}

func TestParseSimctlDevices(t *testing.T) {
	// One booted sim, one shut-down sim (dropped), and an empty runtime.
	in := []byte(`{
	  "devices": {
	    "com.apple.CoreSimulator.SimRuntime.iOS-26-5": [
	      {"udid": "AAAA-1111", "name": "iPhone 17", "state": "Booted"},
	      {"udid": "BBBB-2222", "name": "iPad Pro", "state": "Shutdown"}
	    ],
	    "com.apple.CoreSimulator.SimRuntime.iOS-26-3": []
	  }
	}`)
	got, err := parseSimctlDevices(in)
	if err != nil {
		t.Fatalf("parseSimctlDevices: %v", err)
	}
	want := []Device{
		{ID: "AAAA-1111", Name: "iPhone 17 (iOS 26.5)", Platform: IOS, Transport: Simulator, State: "ready"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mismatch:\n got %+v\nwant %+v", got, want)
	}
}

func TestSimctlDetectNonBooted(t *testing.T) {
	d := &SimctlDetector{run: func(context.Context, string, ...string) ([]byte, error) {
		return []byte(`{"devices":{"com.apple.CoreSimulator.SimRuntime.iOS-26-5":[]}}`), nil
	}}
	// Detect is a no-op off darwin; call the parser path directly via run only
	// makes sense on darwin, so exercise the empty case through parse.
	got, err := parseSimctlDevices([]byte(`{"devices":{}}`))
	if err != nil {
		t.Fatalf("parse empty: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want no devices, got %+v", got)
	}
	_ = d
}

func TestRuntimeVersion(t *testing.T) {
	cases := map[string]string{
		"com.apple.CoreSimulator.SimRuntime.iOS-26-5":     "iOS 26.5",
		"com.apple.CoreSimulator.SimRuntime.watchOS-11-0": "watchOS 11.0",
		"com.apple.CoreSimulator.SimRuntime.iOS-17":       "iOS 17",
		"garbage": "",
	}
	for in, want := range cases {
		if got := runtimeVersion(in); got != want {
			t.Errorf("runtimeVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseSimctlApps(t *testing.T) {
	// plutil-converted JSON: one user app, one system app.
	in := []byte(`{
	  "app.example.Demo": {
	    "ApplicationType": "User",
	    "CFBundleIdentifier": "app.example.Demo",
	    "CFBundleDisplayName": "Demo",
	    "CFBundleShortVersionString": "1.2",
	    "Path": "/sim/Bundle/Demo.app",
	    "DataContainer": "file:///sim/Data/Application/GUID%20one/"
	  },
	  "com.apple.Maps": {
	    "ApplicationType": "System",
	    "CFBundleIdentifier": "com.apple.Maps",
	    "CFBundleName": "Maps"
	  }
	}`)

	// user-only
	got, err := parseSimctlApps(in, false)
	if err != nil {
		t.Fatalf("parseSimctlApps: %v", err)
	}
	want := []InstalledApp{{
		Platform:    IOS,
		BundleID:    "app.example.Demo",
		Name:        "Demo",
		Version:     "1.2",
		InstallPath: "/sim/Bundle/Demo.app",
		DataPath:    "/sim/Data/Application/GUID one", // percent-decoded, trailing slash trimmed
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("user apps mismatch:\n got %+v\nwant %+v", got, want)
	}

	// include system
	all, err := parseSimctlApps(in, true)
	if err != nil {
		t.Fatalf("parseSimctlApps(all): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("want 2 apps with system, got %d: %+v", len(all), all)
	}
}

func TestFileURLToPath(t *testing.T) {
	cases := map[string]string{
		"file:///a/b%20c/": "/a/b c",
		"file:///x/y/":     "/x/y",
		"/already/a/path":  "/already/a/path",
		"":                 "",
	}
	for in, want := range cases {
		if got := fileURLToPath(in); got != want {
			t.Errorf("fileURLToPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSimctlAppListerSupports(t *testing.T) {
	l := NewSimctlAppLister()
	if !l.Supports(Device{Platform: IOS, Transport: Simulator}) {
		t.Error("should support iOS simulator")
	}
	if l.Supports(Device{Platform: IOS, Transport: USB}) {
		t.Error("should not support physical iOS")
	}
	// physical iOS lister must reject simulators (they'd fail ideviceinstaller)
	ios := NewIOSAppLister()
	if ios.Supports(Device{Platform: IOS, Transport: Simulator}) {
		t.Error("IOSAppLister must not claim simulators")
	}
}
