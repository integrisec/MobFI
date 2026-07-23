package main

import (
	"archive/zip"
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// AppMeta is an app's real display name and launcher icon (a data: URL),
// resolved from its APK. Either field may be empty when resolution isn't
// possible (no aapt, adaptive-only icon, etc.); the frontend then keeps its
// placeholder.
type AppMeta struct {
	Name string `json:"name"`
	Icon string `json:"icon"`
}

// AppMeta resolves an Android app's label and icon without pulling the whole
// APK: it extracts only AndroidManifest.xml + resources.arsc from the device
// (a few MB), runs aapt on a reconstructed mini-zip to read the label and
// icon path, then pulls just that icon entry from the device.
func (g *GUI) AppMeta(deviceID, bundleID, apkPath string) (AppMeta, error) {
	var meta AppMeta
	aapt := findAapt()
	if aapt == "" || deviceID == "" || apkPath == "" {
		return meta, nil
	}

	manifest, err := deviceUnzipEntry(g.ctx, deviceID, apkPath, "AndroidManifest.xml")
	if err != nil || len(manifest) == 0 {
		return meta, nil
	}
	arsc, err := deviceUnzipEntry(g.ctx, deviceID, apkPath, "resources.arsc")
	if err != nil || len(arsc) == 0 {
		return meta, nil
	}
	mini, err := writeMiniZip(manifest, arsc)
	if err != nil {
		return meta, nil
	}
	defer os.Remove(mini)

	badging, err := exec.CommandContext(g.ctx, aapt, "dump", "badging", mini).Output()
	if err != nil {
		return meta, nil
	}
	name, iconEntry := parseBadging(string(badging))
	meta.Name = name

	if iconEntry != "" {
		if data, err := deviceUnzipEntry(g.ctx, deviceID, apkPath, iconEntry); err == nil && len(data) > 0 {
			mime := "image/png"
			if strings.HasSuffix(iconEntry, ".webp") {
				mime = "image/webp"
			}
			meta.Icon = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
		}
	}
	return meta, nil
}

// deviceUnzipEntry extracts a single entry from an on-device APK to stdout
// (exec-out keeps the bytes raw).
func deviceUnzipEntry(ctx context.Context, deviceID, apk, entry string) ([]byte, error) {
	return exec.CommandContext(ctx, "adb", "-s", deviceID, "exec-out", "unzip", "-p", apk, entry).Output()
}

// writeMiniZip builds a temporary zip containing just the manifest and
// resources table, which is enough for `aapt dump badging`.
func writeMiniZip(manifest, arsc []byte) (string, error) {
	tmp, err := os.CreateTemp("", "mobfi-mini-*.zip")
	if err != nil {
		return "", err
	}
	defer tmp.Close()

	zw := zip.NewWriter(tmp)
	for _, e := range []struct {
		name string
		data []byte
	}{{"AndroidManifest.xml", manifest}, {"resources.arsc", arsc}} {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: zip.Store})
		if err != nil {
			zw.Close()
			os.Remove(tmp.Name())
			return "", err
		}
		if _, err := w.Write(e.data); err != nil {
			zw.Close()
			os.Remove(tmp.Name())
			return "", err
		}
	}
	if err := zw.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

// findAapt locates aapt (preferred) or aapt2 in the Android SDK build-tools.
func findAapt() string {
	var roots []string
	for _, e := range []string{os.Getenv("ANDROID_HOME"), os.Getenv("ANDROID_SDK_ROOT")} {
		if e != "" {
			roots = append(roots, e)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots,
			filepath.Join(home, "Library", "Android", "sdk"),
			filepath.Join(home, "Android", "Sdk"),
		)
	}
	best, bestVer := "", ""
	for _, root := range roots {
		bt := filepath.Join(root, "build-tools")
		entries, err := os.ReadDir(bt)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			for _, name := range []string{"aapt", "aapt2"} {
				cand := filepath.Join(bt, e.Name(), name)
				if _, err := os.Stat(cand); err == nil {
					if e.Name() > bestVer {
						best, bestVer = cand, e.Name()
					}
					break // prefer aapt over aapt2 within a version
				}
			}
		}
	}
	if best != "" {
		return best
	}
	if p, err := exec.LookPath("aapt"); err == nil {
		return p
	}
	return ""
}

// parseBadging extracts the application label and the best raster icon entry
// (highest density .png/.webp) from `aapt dump badging` output.
func parseBadging(out string) (name, icon string) {
	bestDpi := -1
	for _, ln := range strings.Split(out, "\n") {
		ln = strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(ln, "application-label:'"):
			name = firstQuoted(ln[len("application-label:"):])
		case strings.HasPrefix(ln, "application-icon-"):
			rest := ln[len("application-icon-"):]
			colon := strings.IndexByte(rest, ':')
			if colon < 0 {
				continue
			}
			dpi, _ := strconv.Atoi(rest[:colon])
			p := firstQuoted(rest[colon+1:])
			// 65534/65535 are ANYDPI sentinels, not real densities — skip
			// them so we pick the highest actual-resolution raster icon.
			if (strings.HasSuffix(p, ".png") || strings.HasSuffix(p, ".webp")) && dpi > bestDpi && dpi < 65534 {
				bestDpi, icon = dpi, p
			}
		}
	}
	if name == "" {
		if i := strings.Index(out, "label='"); i >= 0 {
			rest := out[i+len("label='"):]
			if j := strings.IndexByte(rest, '\''); j >= 0 {
				name = rest[:j]
			}
		}
	}
	return name, icon
}

func firstQuoted(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "'")
	if i := strings.IndexByte(s, '\''); i >= 0 {
		return s[:i]
	}
	return s
}
