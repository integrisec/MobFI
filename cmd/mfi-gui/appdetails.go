package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/integrisec/MobFI/internal/plist"
	"github.com/integrisec/MobFI/internal/sysproc"
)

// DetailField is one labelled row in the app details panel.
type DetailField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// AppDetails is the extended metadata shown when an app is selected, as an
// ordered list of fields plus a permission/entitlement list, so the same
// panel renders for both platforms.
type AppDetails struct {
	BundleID    string        `json:"bundle_id"`
	Platform    string        `json:"platform"`
	Fields      []DetailField `json:"fields"`
	Permissions []string      `json:"permissions"`
}

// AppDetails gathers extended info for one app. platform selects the source:
// dumpsys for Android, ideviceinstaller for iOS.
func (g *GUI) AppDetails(deviceID, bundleID, apkPath, platform string) (AppDetails, error) {
	d := AppDetails{BundleID: bundleID, Platform: platform}
	if deviceID == "" || bundleID == "" {
		return d, nil
	}
	if platform == "ios" {
		d.Fields, d.Permissions = iosDetails(g, deviceID, bundleID)
	} else {
		d.Fields, d.Permissions = androidDetails(g, deviceID, bundleID)
	}
	return d, nil
}

// --- Android (dumpsys) ---

type androidPkg struct {
	VersionName, VersionCode, MinSDK, TargetSDK, ABI string
	FirstInstall, LastUpdate, Installer, UID         string
	DataDir, CodePath, Flags, SigningVer             string
	Permissions                                      []string
}

func androidDetails(g *GUI, deviceID, bundleID string) ([]DetailField, []string) {
	out, err := sysproc.CommandContext(g.ctx, "adb", "-s", deviceID, "shell", "dumpsys", "package", bundleID).Output()
	if err != nil {
		return nil, nil
	}
	var p androidPkg
	parseDumpsys(string(out), &p)

	var f []DetailField
	add := func(label, value string) {
		if value != "" {
			f = append(f, DetailField{label, value})
		}
	}
	ver := p.VersionName
	if p.VersionCode != "" {
		ver = strings.TrimSpace(ver + " (code " + p.VersionCode + ")")
	}
	add("Version", ver)
	if p.MinSDK != "" || p.TargetSDK != "" {
		add("SDK", "min "+orDash(p.MinSDK)+" → target "+orDash(p.TargetSDK))
	}
	add("ABI", p.ABI)
	add("Installer", p.Installer)
	add("First install", p.FirstInstall)
	add("Last update", p.LastUpdate)
	if p.CodePath != "" {
		add("APK size", formatBytes(dirSizeBytes(g, deviceID, "", p.CodePath)))
	}
	if p.DataDir != "" {
		add("Data size", formatBytes(dirSizeBytes(g, deviceID, bundleID, p.DataDir)))
	}
	add("UID", p.UID)
	if p.SigningVer != "" {
		add("Signing", "v"+p.SigningVer)
	}
	add("Flags", p.Flags)
	add("Data dir", p.DataDir)
	add("Code path", p.CodePath)
	return f, p.Permissions
}

var permRe = regexp.MustCompile(`[A-Za-z0-9_.]+\.permission\.[A-Za-z0-9_]+`)

func parseDumpsys(out string, d *androidPkg) {
	perms := map[string]struct{}{}
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "versionName="):
			d.VersionName = strings.TrimPrefix(line, "versionName=")
		case strings.HasPrefix(line, "primaryCpuAbi="):
			d.ABI = token(line, "primaryCpuAbi=")
		case strings.HasPrefix(line, "userId="):
			d.UID = token(line, "userId=")
		case strings.HasPrefix(line, "dataDir="):
			d.DataDir = strings.TrimPrefix(line, "dataDir=")
		case strings.HasPrefix(line, "codePath="):
			d.CodePath = strings.TrimPrefix(line, "codePath=")
		case strings.HasPrefix(line, "firstInstallTime="):
			d.FirstInstall = strings.TrimPrefix(line, "firstInstallTime=")
		case strings.HasPrefix(line, "lastUpdateTime="):
			d.LastUpdate = strings.TrimPrefix(line, "lastUpdateTime=")
		case strings.HasPrefix(line, "installerPackageName="):
			d.Installer = token(line, "installerPackageName=")
		case strings.HasPrefix(line, "apkSigningVersion="):
			d.SigningVer = token(line, "apkSigningVersion=")
		case strings.HasPrefix(line, "versionCode="):
			d.VersionCode = token(line, "versionCode=")
			if v := token(line, "minSdk="); v != "" {
				d.MinSDK = v
			}
			if v := token(line, "targetSdk="); v != "" {
				d.TargetSDK = v
			}
		case strings.HasPrefix(line, "pkgFlags=[") && d.Flags == "":
			d.Flags = bracketed(line)
		}
		for _, p := range permRe.FindAllString(line, -1) {
			perms[p] = struct{}{}
		}
	}
	for p := range perms {
		d.Permissions = append(d.Permissions, p)
	}
	sort.Strings(d.Permissions)
}

// --- iOS (ideviceinstaller) ---

func iosDetails(g *GUI, udid, bundleID string) ([]DetailField, []string) {
	out, err := sysproc.CommandContext(g.ctx, "ideviceinstaller", "-u", udid, "list", "--all", "--xml").Output()
	if err != nil {
		return nil, nil
	}
	v, err := plist.DecodeAny(out)
	if err != nil {
		return nil, nil
	}
	arr, _ := v.([]any)
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok || str(m["CFBundleIdentifier"]) != bundleID {
			continue
		}

		var f []DetailField
		add := func(label, key string) {
			if s := str(m[key]); s != "" {
				f = append(f, DetailField{label, s})
			}
		}
		ver := str(m["CFBundleShortVersionString"])
		if b := str(m["CFBundleVersion"]); b != "" && b != ver {
			ver = strings.TrimSpace(ver + " (build " + b + ")")
		}
		if ver != "" {
			f = append(f, DetailField{"Version", ver})
		}
		add("Type", "ApplicationType")
		add("Min iOS", "MinimumOSVersion")
		if p := str(m["DTPlatformName"]); p != "" {
			f = append(f, DetailField{"Built with SDK", strings.TrimSpace(p + " " + str(m["DTPlatformVersion"]))})
		}
		add("Signer", "SignerIdentity")
		add("Executable", "CFBundleExecutable")
		add("Region", "CFBundleDevelopmentRegion")
		if dsid, ok := m["ApplicationDSID"].(int64); ok {
			f = append(f, DetailField{"DSID", strconv.FormatInt(dsid, 10)})
		}
		add("Bundle path", "Path")
		add("Data container", "Container")

		// iOS "permissions": entitlement keys plus privacy usage strings.
		var perms []string
		if ent, ok := m["Entitlements"].(map[string]any); ok {
			for k := range ent {
				perms = append(perms, k)
			}
		}
		for k := range m {
			if strings.HasSuffix(k, "UsageDescription") {
				perms = append(perms, k)
			}
		}
		sort.Strings(perms)
		return f, perms
	}
	return nil, nil
}

// --- helpers ---

func str(v any) string { s, _ := v.(string); return s }

func orDash(s string) string {
	if s == "" {
		return "?"
	}
	return s
}

func formatBytes(n int64) string {
	if n <= 0 {
		return "—"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	f := float64(n)
	i := 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if i > 0 && f < 10 {
		return fmt.Sprintf("%.1f %s", f, units[i])
	}
	return fmt.Sprintf("%.0f %s", f, units[i])
}

// token returns the whitespace-delimited value following key on a line.
func token(line, key string) string {
	i := strings.Index(line, key)
	if i < 0 {
		return ""
	}
	rest := line[i+len(key):]
	if j := strings.IndexAny(rest, " \t"); j >= 0 {
		rest = rest[:j]
	}
	return rest
}

// bracketed returns the contents between the first '[' and last ']'.
func bracketed(line string) string {
	i := strings.IndexByte(line, '[')
	j := strings.LastIndexByte(line, ']')
	if i < 0 || j <= i {
		return ""
	}
	return strings.TrimSpace(line[i+1 : j])
}

// dirSizeBytes runs `du -sk` (optionally via run-as) and returns bytes.
func dirSizeBytes(g *GUI, deviceID, runAs, dir string) int64 {
	args := []string{"-s", deviceID, "shell"}
	if runAs != "" {
		args = append(args, "run-as", runAs)
	}
	args = append(args, "du", "-sk", dir)
	// Ignore the exit code: du prints the summary total to stdout even when
	// it exits non-zero because a subdirectory (e.g. oat/) was unreadable.
	out, _ := sysproc.CommandContext(g.ctx, "adb", args...).Output()
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return 0
	}
	kb, _ := strconv.ParseInt(fields[0], 10, 64)
	return kb * 1024
}
