//go:build darwin

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// refreshPath augments the process PATH so the bound core can find command-line
// tools (adb, libimobiledevice, ...) regardless of how the app was launched.
//
// A macOS app launched from Finder/Dock/LaunchServices inherits only a minimal
// PATH (/usr/bin:/bin:/usr/sbin:/sbin). We bake an LSEnvironment PATH into the
// bundle, but LaunchServices does not always honor it, so we also fix PATH here
// at runtime: merge the user's login-shell PATH (matching what the Terminal
// sees) and guarantee the standard Homebrew / MacPorts / toolchain locations.
func refreshPath() {
	seen := map[string]bool{}
	var dirs []string
	add := func(d string) {
		if d == "" || seen[d] {
			return
		}
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			return
		}
		seen[d] = true
		dirs = append(dirs, d)
	}

	// Whatever we already have (minimal when Finder-launched) stays available.
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		add(d)
	}
	// The user's login-shell PATH -- exactly what their Terminal resolves, so a
	// non-standard adb (e.g. Android SDK platform-tools) is covered too.
	for _, d := range filepath.SplitList(loginShellPath()) {
		add(d)
	}
	// Guarantee the common locations even if the shell probe found nothing.
	home := os.Getenv("HOME")
	for _, d := range []string{
		"/opt/homebrew/bin", "/opt/homebrew/sbin",
		"/usr/local/bin", "/usr/local/sbin",
		"/opt/local/bin",
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, "go", "bin"),
		"/usr/local/go/bin",
	} {
		add(d)
	}

	os.Setenv("PATH", strings.Join(dirs, string(os.PathListSeparator)))
}

// loginShellPath returns the PATH a login instance of the user's shell would
// have, or "" if it cannot be determined. Bounded so startup never stalls.
func loginShellPath() string {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// -l sources login files (e.g. .zprofile with `brew shellenv`); printf keeps
	// the output to just the PATH value with no prompt/rc noise.
	out, err := exec.CommandContext(ctx, sh, "-lc", `printf %s "$PATH"`).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
