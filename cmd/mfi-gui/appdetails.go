package main

import (
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// AppDetails is the extended metadata shown when an app is selected. Fields
// that couldn't be read are left empty/zero.
type AppDetails struct {
	BundleID     string   `json:"bundle_id"`
	VersionName  string   `json:"version_name"`
	VersionCode  string   `json:"version_code"`
	MinSDK       string   `json:"min_sdk"`
	TargetSDK    string   `json:"target_sdk"`
	ABI          string   `json:"abi"`
	FirstInstall string   `json:"first_install"`
	LastUpdate   string   `json:"last_update"`
	Installer    string   `json:"installer"`
	UID          string   `json:"uid"`
	DataDir      string   `json:"data_dir"`
	CodePath     string   `json:"code_path"`
	Flags        string   `json:"flags"`
	SigningVer   string   `json:"signing_version"`
	APKSize      int64    `json:"apk_size"`  // bytes (code path)
	DataSize     int64    `json:"data_size"` // bytes (data dir)
	Permissions  []string `json:"permissions"`
}

var permRe = regexp.MustCompile(`[A-Za-z0-9_.]+\.permission\.[A-Za-z0-9_]+`)

// AppDetails gathers extended info for one app from `dumpsys package` plus
// `du` sizes (data dir read via run-as).
func (g *GUI) AppDetails(deviceID, bundleID, apkPath string) (AppDetails, error) {
	d := AppDetails{BundleID: bundleID}
	if deviceID == "" || bundleID == "" {
		return d, nil
	}
	out, err := exec.CommandContext(g.ctx, "adb", "-s", deviceID, "shell", "dumpsys", "package", bundleID).Output()
	if err != nil {
		return d, nil
	}
	parseDumpsys(string(out), &d)

	if d.CodePath != "" {
		d.APKSize = dirSizeBytes(g, deviceID, "", d.CodePath)
	}
	if d.DataDir != "" {
		d.DataSize = dirSizeBytes(g, deviceID, bundleID, d.DataDir) // run-as for access
	}
	return d, nil
}

func parseDumpsys(out string, d *AppDetails) {
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

// bracketed returns the contents between the first '[' and ']'.
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
	out, _ := exec.CommandContext(g.ctx, "adb", args...).Output()
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return 0
	}
	kb, _ := strconv.ParseInt(fields[0], 10, 64)
	return kb * 1024
}
