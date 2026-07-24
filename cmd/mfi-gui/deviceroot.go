package main

import (
	"os/exec"
	"strings"

	"github.com/integrisec/MobFI/internal/plist"
)

// jailbreakApps are bundle ids of common jailbreak package managers / tools.
// Their presence is a strong signal the device is jailbroken.
var jailbreakApps = map[string]bool{
	"com.saurik.Cydia":        true,
	"org.coolstar.SileoStore": true,
	"org.coolstar.sileo":      true,
	"xyz.willy.Zebra":         true,
	"com.opa334.Dopamine":     true,
	"com.opa334.TrollStore":   true,
	"com.opa334.trollstore":   true,
	"science.xnu.undecimus":   true,
	"com.tigisoftware.Filza":  true,
	"org.coolstar.electra":    true,
	"com.electrateam.chimera": true,
	"me.qwertyoruiop.jelbrek": true,
}

// DeviceRoot reports whether a device appears rooted (Android) or
// jailbroken (iOS). Values: "rooted"/"not rooted", "jailbroken"/"not
// detected", or "unknown".
func (g *GUI) DeviceRoot(deviceID, platform, transport string) (string, error) {
	if deviceID == "" {
		return "unknown", nil
	}
	// A simulator is a sandboxed host process, not a jailbroken device; the
	// jailbreak-app probe doesn't apply (and ideviceinstaller can't reach it).
	if transport == "simulator" {
		return "n/a", nil
	}
	if platform == "ios" {
		return iosJailbroken(g, deviceID), nil
	}
	return androidRooted(g, deviceID), nil
}

// androidRooted checks for su binaries and Magisk without invoking su (which
// could prompt/hang).
func androidRooted(g *GUI, serial string) string {
	const probe = "ls -d /data/adb/magisk /sbin/.magisk /system/bin/su /system/xbin/su /sbin/su /su/bin/su 2>/dev/null; command -v su"
	out, err := exec.CommandContext(g.ctx, "adb", "-s", serial, "shell", probe).Output()
	if err != nil && len(out) == 0 {
		return "unknown"
	}
	if strings.TrimSpace(string(out)) != "" {
		return "rooted"
	}
	return "not rooted"
}

// iosJailbroken looks for a known jailbreak manager among installed apps.
func iosJailbroken(g *GUI, udid string) string {
	out, err := exec.CommandContext(g.ctx, "ideviceinstaller", "-u", udid, "list", "--all", "--xml").Output()
	if err != nil {
		return "unknown"
	}
	v, err := plist.DecodeAny(out)
	if err != nil {
		return "unknown"
	}
	arr, _ := v.([]any)
	for _, it := range arr {
		m, _ := it.(map[string]any)
		if jailbreakApps[str(m["CFBundleIdentifier"])] {
			return "jailbroken"
		}
	}
	return "not detected"
}
