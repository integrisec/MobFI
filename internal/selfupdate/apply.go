package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Result describes the outcome of an in-place update.
type Result struct {
	Method          string `json:"method"`          // "git" or "binary"
	Message         string `json:"message"`         // human-readable summary
	RestartRequired bool   `json:"restartRequired"` // caller must relaunch to run the new build
}

// Apply updates MobFI in place. In a git checkout it runs `git pull --ff-only`
// then rebuilds the given target ("cli" or "gui") via the project's install
// script. For a standalone prebuilt binary it downloads the matching release
// asset, verifies its SHA-256, and atomically swaps the running executable.
//
// progress, if non-nil, receives short human-readable status lines. Rebuilds
// can take a while, so callers should pass a context with a generous timeout.
func Apply(ctx context.Context, target string, progress func(string)) (*Result, error) {
	if progress == nil {
		progress = func(string) {}
	}
	info, err := Check(ctx)
	if err != nil && !info.GitCheckout {
		return nil, fmt.Errorf("update check failed: %w", err)
	}

	// A git checkout is the source of truth for a source install (and the only
	// way to update the GUI, which is not shipped as a prebuilt binary).
	if info.GitCheckout {
		return applyGit(ctx, target, progress)
	}
	if info.Available && info.AssetURL != "" {
		return applyBinary(ctx, info, progress)
	}
	if info.Available {
		return nil, fmt.Errorf("no prebuilt binary for %s/%s in the latest release; download it from %s", runtime.GOOS, runtime.GOARCH, info.ReleaseURL)
	}
	return nil, fmt.Errorf("already up to date (v%s)", info.Current)
}

// applyGit pulls the latest commits and rebuilds via the install script.
func applyGit(ctx context.Context, target string, progress func(string)) (*Result, error) {
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("git not found on PATH")
	}
	dir := repoDir(ctx, git)
	if dir == "" {
		return nil, fmt.Errorf("not a MobFI git checkout")
	}

	progress("Pulling latest changes (git pull)...")
	if out, err := runIn(ctx, dir, git, "pull", "--ff-only"); err != nil {
		return nil, fmt.Errorf("git pull failed: %w\n%s", err, tail(out, 12))
	}

	name, args := rebuildCmd(target)
	progress(fmt.Sprintf("Rebuilding %s (this can take a minute)...", target))
	if out, err := runIn(ctx, dir, name, args...); err != nil {
		return nil, fmt.Errorf("rebuild failed: %w\n%s", err, tail(out, 20))
	}
	return &Result{
		Method:          "git",
		Message:         fmt.Sprintf("Updated from git and rebuilt the %s.", target),
		RestartRequired: true,
	}, nil
}

// rebuildCmd returns the install-script invocation that rebuilds one target.
// The install script resolves the toolchain (go, wails) and its PATH, so it is
// more reliable than calling the compilers directly from a GUI subprocess.
func rebuildCmd(target string) (string, []string) {
	only := "--cli-only"
	psOnly := "-CliOnly"
	if target == "gui" {
		only, psOnly = "--gui-only", "-GuiOnly"
	}
	if runtime.GOOS == "windows" {
		return "powershell", []string{"-ExecutionPolicy", "Bypass", "-File", filepath.Join("scripts", "install.ps1"), psOnly, "-NoRuntimeTools"}
	}
	return "bash", []string{filepath.Join("scripts", "install.sh"), only, "--no-runtime-tools"}
}

// applyBinary downloads the release asset for this platform, verifies it, and
// atomically replaces the running executable.
func applyBinary(ctx context.Context, info *Info, progress func(string)) (*Result, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	progress("Downloading " + info.AssetName + "...")
	tmp := exe + ".new"
	if err := download(ctx, info.AssetURL, tmp); err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	defer os.Remove(tmp) // no-op once renamed into place

	progress("Verifying checksum...")
	want, err := fetchChecksum(ctx, info.ChecksumsURL, info.AssetName)
	if err != nil {
		return nil, fmt.Errorf("could not verify download: %w", err)
	}
	got, err := sha256File(tmp)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(got, want) {
		return nil, fmt.Errorf("checksum mismatch (expected %s, got %s); aborting", want, got)
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return nil, err
	}

	progress("Installing...")
	if err := replaceExecutable(exe, tmp); err != nil {
		return nil, fmt.Errorf("could not replace the running binary (%s): %w", exe, err)
	}
	return &Result{
		Method:          "binary",
		Message:         fmt.Sprintf("Updated to v%s.", info.Latest),
		RestartRequired: true,
	}, nil
}

// replaceExecutable swaps newFile in for exe. On Unix a same-filesystem rename
// is atomic even while the old binary runs. Windows cannot overwrite a running
// image, so the old one is moved aside first.
func replaceExecutable(exe, newFile string) error {
	if runtime.GOOS == "windows" {
		old := exe + ".old"
		_ = os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return err
		}
		if err := os.Rename(newFile, exe); err != nil {
			_ = os.Rename(old, exe) // roll back
			return err
		}
		return nil
	}
	return os.Rename(newFile, exe)
}

func download(ctx context.Context, url, dst string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst)
	}
	return err
}

// fetchChecksum downloads the SHA256SUMS file and returns the hex digest listed
// for assetName (lines are "<hex>  <name>").
func fetchChecksum(ctx context.Context, url, assetName string) (string, error) {
	if url == "" {
		return "", fmt.Errorf("no checksums file published")
	}
	ctx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == assetName {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no checksum listed for %s", assetName)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// runIn runs a command in dir, returning combined output. It allows a long
// timeout because rebuilds (especially the GUI) are slow.
func runIn(ctx context.Context, dir, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// tail returns the last n non-empty lines of s (for compact error reporting).
func tail(s string, n int) string {
	var lines []string
	for _, ln := range strings.Split(s, "\n") {
		if strings.TrimSpace(ln) != "" {
			lines = append(lines, ln)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
