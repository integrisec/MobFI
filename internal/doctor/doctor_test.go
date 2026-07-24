package doctor

import (
	"reflect"
	"runtime"
	"testing"
)

func TestMissingCore(t *testing.T) {
	tools := []Tool{
		{Name: "a", Found: true},
		{Name: "b", Found: false, Optional: false},
		{Name: "c", Found: false, Optional: true}, // optional: not core
		{Name: "d", Found: false, Optional: false},
	}
	got := MissingCore(tools)
	want := []string{"b", "d"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MissingCore = %v, want %v", got, want)
	}
}

func TestCheckPopulates(t *testing.T) {
	tools := Check()
	if len(tools) == 0 {
		t.Fatal("Check returned no tools")
	}
	for _, tl := range tools {
		if tl.Name == "" || tl.Purpose == "" {
			t.Errorf("tool has empty Name/Purpose: %+v", tl)
		}
		if tl.Found && tl.Path == "" {
			t.Errorf("found tool %q has no resolved path", tl.Name)
		}
		if !tl.Found && tl.Path != "" {
			t.Errorf("missing tool %q unexpectedly has a path %q", tl.Name, tl.Path)
		}
	}
}

func TestCheckFiltersByOS(t *testing.T) {
	names := map[string]bool{}
	for _, tl := range Check() {
		names[tl.Name] = true
	}
	// xcrun/plutil are macOS-only (iOS Simulator support).
	wantMac := runtime.GOOS == "darwin"
	if names["xcrun"] != wantMac {
		t.Errorf("xcrun present=%v, want %v on %s", names["xcrun"], wantMac, runtime.GOOS)
	}
}
