//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// refreshPath rebuilds the process PATH from the Windows registry (machine +
// user) and prepends the current value. Explorer caches PATH at login, so a
// shortcut-launched GUI otherwise misses tools installed since (adb via winget,
// the libimobiledevice bundle) until the next logoff. Rebuilding here lets the
// bound core resolve them immediately.
func refreshPath() {
	var parts []string
	parts = append(parts, filepath.SplitList(os.Getenv("PATH"))...)
	parts = append(parts, registryPath(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`)...)
	parts = append(parts, registryPath(registry.CURRENT_USER, `Environment`)...)

	seen := make(map[string]bool)
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		key := strings.ToLower(p)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	if len(out) > 0 {
		os.Setenv("PATH", strings.Join(out, string(os.PathListSeparator)))
	}
}

// registryPath reads the "Path" value under a registry key, expanding any
// %VAR% references (REG_EXPAND_SZ), and splits it into directories. A missing
// key or value yields nothing.
func registryPath(root registry.Key, path string) []string {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	val, valType, err := k.GetStringValue("Path")
	if err != nil {
		return nil
	}
	if valType == registry.EXPAND_SZ {
		if expanded, err := registry.ExpandString(val); err == nil {
			val = expanded
		}
	}
	return filepath.SplitList(val)
}
