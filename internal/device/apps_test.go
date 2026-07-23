package device

import (
	"context"
	"os/exec"
	"testing"
)

func TestParseADBPackages(t *testing.T) {
	// Note the '=' inside the modern base.apk path — the split must be on
	// the last '='. The first line carries a versionCode suffix; the second
	// does not (must still parse).
	out := `package:/data/app/~~aB==/com.example.app-xY==/base.apk=com.example.app versionCode:12345
package:/data/app/Other/base.apk=com.other.thing
`
	apps := parseADBPackages([]byte(out))
	if len(apps) != 2 {
		t.Fatalf("got %d apps, want 2: %+v", len(apps), apps)
	}
	if apps[0].BundleID != "com.example.app" {
		t.Errorf("bundle = %q", apps[0].BundleID)
	}
	if apps[0].InstallPath != "/data/app/~~aB==/com.example.app-xY==/base.apk" {
		t.Errorf("apk = %q", apps[0].InstallPath)
	}
	if apps[0].DataPath != "/data/data/com.example.app" {
		t.Errorf("data = %q", apps[0].DataPath)
	}
	if apps[0].Version != "12345" {
		t.Errorf("version = %q, want 12345", apps[0].Version)
	}
	if apps[0].Platform != Android {
		t.Errorf("platform = %q", apps[0].Platform)
	}
	if apps[1].Version != "" {
		t.Errorf("app1 version = %q, want empty", apps[1].Version)
	}
}

func TestADBAppListerMissingBinary(t *testing.T) {
	l := &ADBAppLister{run: func(context.Context, string, ...string) ([]byte, error) {
		return nil, exec.ErrNotFound
	}}
	apps, err := l.List(context.Background(), Device{Platform: Android, ID: "S"}, false)
	if err != nil {
		t.Fatalf("missing adb should not error, got %v", err)
	}
	if apps != nil {
		t.Fatalf("missing adb should yield no apps, got %+v", apps)
	}
}

func TestADBAppListerSystemFlag(t *testing.T) {
	var gotArgs []string
	l := &ADBAppLister{run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		gotArgs = args
		return nil, nil
	}}
	d := Device{Platform: Android, ID: "S"}

	l.List(context.Background(), d, false)
	if !containsArg(gotArgs, "-3") {
		t.Errorf("user-only listing should pass -3, got %v", gotArgs)
	}
	l.List(context.Background(), d, true)
	if containsArg(gotArgs, "-3") {
		t.Errorf("include-system listing should omit -3, got %v", gotArgs)
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func TestParseIOSApps(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><array>
<dict>
	<key>CFBundleIdentifier</key><string>com.example.app</string>
	<key>CFBundleDisplayName</key><string>Example</string>
	<key>CFBundleShortVersionString</key><string>1.2</string>
	<key>Path</key><string>/private/var/containers/Bundle/Application/UUID/Example.app</string>
</dict>
<dict>
	<key>CFBundleIdentifier</key><string>com.other.thing</string>
	<key>CFBundleName</key><string>Thing</string>
	<key>Path</key><string>/private/var/containers/Bundle/Application/UUID2/Thing.app</string>
</dict>
</array></plist>`
	apps, err := parseIOSApps([]byte(xml))
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 2 {
		t.Fatalf("got %d apps, want 2", len(apps))
	}
	if apps[0].BundleID != "com.example.app" || apps[0].Name != "Example" || apps[0].Version != "1.2" {
		t.Errorf("app0 = %+v", apps[0])
	}
	if apps[0].InstallPath == "" || apps[0].Platform != IOS {
		t.Errorf("app0 path/platform = %+v", apps[0])
	}
	// Falls back to CFBundleName when DisplayName is absent.
	if apps[1].Name != "Thing" {
		t.Errorf("app1 name = %q, want Thing", apps[1].Name)
	}
}
