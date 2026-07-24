// Package doctor reports which external runtime tools MobFI can find on the
// host. MobFI shells out to these for device access; none are required for the
// binary to run (features that need a missing tool are simply unavailable), so
// this is a diagnostic to help users install what a given workflow needs.
package doctor

import (
	"os/exec"
	"runtime"
)

// Tool is an external program MobFI may invoke, with per-OS install guidance.
type Tool struct {
	Name     string `json:"name"`     // executable looked up on PATH
	Purpose  string `json:"purpose"`  // what MobFI uses it for
	Optional bool   `json:"optional"` // true = nice-to-have, not core to a platform
	Found    bool   `json:"found"`
	Path     string `json:"path,omitempty"` // resolved path when found
	Hint     string `json:"hint,omitempty"` // install hint for this OS when missing

	onlyOS  []string          // if set, only checked on these GOOS values
	install map[string]string // GOOS -> install hint
}

// catalog is the full set of tools MobFI knows how to use. Names match the
// binaries invoked in internal/device, internal/transport and the GUI.
var catalog = []Tool{
	{
		Name:    "adb",
		Purpose: "Android device access (detect, extract, console)",
		install: map[string]string{
			"darwin":  "brew install --cask android-platform-tools",
			"linux":   "apt install android-tools-adb  (or your distro's android-tools)",
			"windows": "winget install Google.PlatformTools",
		},
	},
	{
		Name:    "idevice_id",
		Purpose: "iOS device discovery (libimobiledevice)",
		install: liDevice,
	},
	{
		Name:    "ideviceinfo",
		Purpose: "iOS device details (libimobiledevice)",
		install: liDevice,
	},
	{
		Name:    "ideviceinstaller",
		Purpose: "iOS installed-app listing",
		install: map[string]string{
			"darwin":  "brew install ideviceinstaller",
			"linux":   "apt install ideviceinstaller",
			"windows": "scoop install libimobiledevice",
		},
	},
	{
		Name:    "afcclient",
		Purpose: "iOS app-container extraction (AFC house arrest)",
		install: liDevice,
	},
	{
		Name:     "iproxy",
		Purpose:  "iOS Console over USB (SSH port forward)",
		Optional: true,
		install: map[string]string{
			"darwin":  "brew install libusbmuxd",
			"linux":   "apt install libusbmuxd-tools",
			"windows": "scoop install libimobiledevice",
		},
	},
	{
		Name:    "ssh",
		Purpose: "iOS Console (SSH to a jailbroken device)",
		install: map[string]string{
			"darwin":  "(preinstalled with macOS)",
			"linux":   "apt install openssh-client",
			"windows": "winget install Microsoft.OpenSSH.Beta  (or the built-in OpenSSH client)",
		},
	},
	{
		Name:     "aapt",
		Purpose:  "GUI: real Android app icon / name / version",
		Optional: true,
		install: map[string]string{
			"darwin":  "brew install --cask android-commandlinetools, then sdkmanager \"build-tools;34.0.0\"",
			"linux":   "install Android SDK build-tools (sdkmanager)",
			"windows": "install Android SDK build-tools (sdkmanager)",
		},
	},
	{
		Name:    "xcrun",
		Purpose: "iOS Simulator support (Xcode: simctl)",
		onlyOS:  []string{"darwin"},
		install: map[string]string{"darwin": "xcode-select --install  (or install Xcode)"},
	},
	{
		Name:    "plutil",
		Purpose: "iOS Simulator app listing (plist decode)",
		onlyOS:  []string{"darwin"},
		install: map[string]string{"darwin": "(preinstalled with macOS)"},
	},
}

// liDevice is the shared install hint for the core libimobiledevice tools.
var liDevice = map[string]string{
	"darwin":  "brew install libimobiledevice",
	"linux":   "apt install libimobiledevice-utils",
	"windows": "scoop install libimobiledevice  (also needs Apple's USB driver / iTunes)",
}

// Check resolves every catalog tool relevant to the current OS via PATH.
func Check() []Tool {
	goos := runtime.GOOS
	out := make([]Tool, 0, len(catalog))
	for _, t := range catalog {
		if len(t.onlyOS) > 0 && !contains(t.onlyOS, goos) {
			continue
		}
		if p, err := exec.LookPath(t.Name); err == nil {
			t.Found = true
			t.Path = p
		} else {
			t.Hint = t.install[goos]
		}
		out = append(out, t)
	}
	return out
}

// MissingCore returns the names of non-optional tools that were not found.
func MissingCore(tools []Tool) []string {
	var missing []string
	for _, t := range tools {
		if !t.Found && !t.Optional {
			missing = append(missing, t.Name)
		}
	}
	return missing
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
