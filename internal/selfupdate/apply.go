package selfupdate

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/integrisec/MobFI/internal/sysproc"
	"github.com/integrisec/MobFI/internal/version"
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

	// Pull from the public HTTPS URL rather than the configured remote (often
	// SSH), so the unattended worker needs no SSH key/agent -- which it may not
	// have in a GUI/LaunchServices session. The repo is public, so HTTPS fetches
	// anonymously. Fast-forward the current branch onto the fetched tip.
	branch, _ := gitOut(ctx, git, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if branch == "" || branch == "HEAD" {
		branch = "main"
	}
	httpsURL := fmt.Sprintf("https://github.com/%s/%s.git", version.RepoOwner, version.RepoName)
	progress("Pulling latest changes (git pull --ff-only, HTTPS)...")
	if err := runInStream(ctx, dir, gitEnv(), progress, git, "pull", "--ff-only", httpsURL, branch); err != nil {
		return nil, fmt.Errorf("git pull failed: %w", err)
	}

	// MFI-UPD-03: verify the pulled HEAD carries a valid maintainer
	// signature before running install.sh / install.ps1. Without this,
	// anyone who subverted the HTTPS path to github.com (rogue enterprise CA,
	// state-issued sub-CA, GitHub-side compromise) delivers attacker commits
	// and MobFI executes them under the operator's account via bash /
	// powershell. `git verify-commit HEAD` succeeds only when HEAD is signed
	// by a key in the operator's gpg / ssh trust set -- so the operator
	// controls the trust anchor, not the transport.
	progress("Verifying pulled HEAD is a signed commit...")
	if out, err := gitOut(ctx, git, dir, "verify-commit", "HEAD"); err != nil {
		return nil, fmt.Errorf("refusing to build: git verify-commit HEAD failed (%s): %w. Configure gpg / ssh trust for the release-signing key before running the git-update path", strings.TrimSpace(out), err)
	}

	name, args := rebuildCmd(target)
	progress(fmt.Sprintf("Rebuilding %s via %s (this can take a minute)...", target, name))
	if err := runInStream(ctx, dir, nil, progress, name, args...); err != nil {
		return nil, fmt.Errorf("rebuild failed: %w", err)
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

// updateClient is the shared http.Client for every fetch the updater makes
// (checksums, signature, binary asset). It sets a TLS floor of 1.2, ignores
// HTTP(S)_PROXY env (see MFI-UPD-06), and refuses redirects to hosts outside
// the GitHub release-serving set (MFI-UPD-06 CheckRedirect).
var updateClient = func() *http.Client {
	allowed := map[string]bool{
		"api.github.com":                    true,
		"objects.githubusercontent.com":     true,
		"github-releases.githubusercontent.com": true,
		"release-assets.githubusercontent.com":  true,
		"codeload.github.com":               true,
	}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		ForceAttemptHTTP2:     true,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if !allowed[req.URL.Host] {
				return fmt.Errorf("update: refused redirect to unallowed host %s", req.URL.Host)
			}
			if len(via) >= 5 {
				return fmt.Errorf("update: too many redirects")
			}
			return nil
		},
	}
}()

// versionFloorPath returns the path to the persisted "highest version ever
// installed" marker for MFI-UPD-05 downgrade defence. Empty on any error;
// the floor check treats that as "no floor known".
func versionFloorPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "MobFI", "version-floor.txt")
}

// readVersionFloor returns the highest version ever installed by this app,
// or "" if unknown / unreadable.
func readVersionFloor() string {
	p := versionFloorPath()
	if p == "" {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// writeVersionFloor persists v as the new floor if it is > any existing
// floor. Failures are silent -- the check is defence in depth, not a
// primary security control.
func writeVersionFloor(v string) {
	p := versionFloorPath()
	if p == "" || v == "" {
		return
	}
	if cur := readVersionFloor(); cur != "" && Compare(v, cur) <= 0 {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(p, []byte(v+"\n"), 0o600)
}

// pubKeyBase64 is the base64-encoded ed25519 public key that signs the
// SHA256SUMS.txt release asset. Set at build time via ldflags:
//
//	go build -ldflags "-X 'github.com/integrisec/MobFI/internal/selfupdate.pubKeyBase64=<base64>'" ./cmd/mfi ./cmd/mfi-gui
//
// An empty value causes applyBinary to refuse to install any update. This is
// the fail-safe default demanded by MFI-UPD-01 -- an updater is a code
// execution primitive and must never install content whose provenance it
// cannot verify.
//
// Rotation is by re-shipping a build with a new key. Multi-key trust (for
// staggered rotation) requires code changes here.
var pubKeyBase64 = ""

// loadPubKey parses pubKeyBase64 or reports why signature verification cannot
// proceed.
func loadPubKey() (ed25519.PublicKey, error) {
	if pubKeyBase64 == "" {
		return nil, errors.New("release-signing public key is not configured in this build; refusing to install an unsigned update. Rebuild with -ldflags \"-X 'github.com/integrisec/MobFI/internal/selfupdate.pubKeyBase64=<base64>'\"")
	}
	raw, err := base64.StdEncoding.DecodeString(pubKeyBase64)
	if err != nil {
		return nil, fmt.Errorf("release-signing public key is malformed base64: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("release-signing public key wrong size (%d, want %d)", len(raw), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(raw), nil
}

// parseSignature accepts either raw 64-byte ed25519 signature bytes or the
// base64 encoding thereof (with or without a trailing newline).
func parseSignature(body []byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == ed25519.SignatureSize {
		return trimmed, nil
	}
	dec, err := base64.StdEncoding.DecodeString(string(trimmed))
	if err == nil && len(dec) == ed25519.SignatureSize {
		return dec, nil
	}
	return nil, fmt.Errorf("release signature is not %d bytes or valid base64", ed25519.SignatureSize)
}

// verifyChecksums returns nil iff signature is a valid ed25519 signature by
// pubKey over the exact bytes of checksums.
func verifyChecksums(checksums, signature []byte, pubKey ed25519.PublicKey) error {
	if !ed25519.Verify(pubKey, checksums, signature) {
		return errors.New("SHA256SUMS.txt signature verification failed")
	}
	return nil
}

// checksumFor extracts the hex sha256 for assetName from the raw bytes of a
// SHA256SUMS.txt file. Lines are "<hex>  <name>".
func checksumFor(body []byte, assetName string) (string, error) {
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == assetName {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no checksum listed for %s", assetName)
}

// applyBinary downloads the release asset for this platform, verifies its
// ed25519-signed SHA-256, and atomically replaces the running executable.
// Signature verification is unconditional (MFI-UPD-01): the SHA-256 from the
// release is only trusted after ed25519.Verify accepts it against the
// build-time public key.
func applyBinary(ctx context.Context, info *Info, progress func(string)) (*Result, error) {
	if progress == nil {
		progress = func(string) {}
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	// MFI-UPD-05: refuse to install a version that is not strictly greater
	// than the highest version ever installed on this machine. Rolls back
	// the "GitHub /releases/latest was briefly manipulated" attack that
	// would otherwise silently downgrade a client into a known-vulnerable
	// tag.
	if floor := readVersionFloor(); floor != "" && Compare(info.Latest, floor) <= 0 {
		return nil, fmt.Errorf("refusing downgrade: latest advertised %q is not newer than installed floor %q", info.Latest, floor)
	}

	// Verify signature FIRST so a malformed / missing pubKey aborts before we
	// download hundreds of MB of asset data. loadPubKey enforces MFI-UPD-01's
	// fail-safe default.
	progress("Verifying signing key...")
	pubKey, err := loadPubKey()
	if err != nil {
		return nil, fmt.Errorf("aborting update: %w", err)
	}
	if info.SignatureURL == "" {
		return nil, errors.New("aborting update: release does not publish " + signatureAsset + "; nothing to verify against")
	}

	progress("Fetching signed checksums...")
	checksumsBody, err := fetchBody(ctx, info.ChecksumsURL, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("could not fetch checksums: %w", err)
	}
	sigBody, err := fetchBody(ctx, info.SignatureURL, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("could not fetch signature: %w", err)
	}
	sig, err := parseSignature(sigBody)
	if err != nil {
		return nil, fmt.Errorf("aborting update: %w", err)
	}
	if err := verifyChecksums(checksumsBody, sig, pubKey); err != nil {
		return nil, fmt.Errorf("aborting update: %w", err)
	}
	want, err := checksumFor(checksumsBody, info.AssetName)
	if err != nil {
		return nil, fmt.Errorf("could not verify download: %w", err)
	}

	progress("Downloading " + info.AssetName + "...")
	tmp := exe + ".new"
	if err := download(ctx, info.AssetURL, tmp); err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	defer os.Remove(tmp) // no-op once renamed into place

	progress("Verifying checksum...")
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
	// Persist the new floor after a successful swap so subsequent downgrade
	// attempts see the higher watermark.
	writeVersionFloor(info.Latest)
	return &Result{
		Method:          "binary",
		Message:         fmt.Sprintf("Updated to v%s.", info.Latest),
		RestartRequired: true,
	}, nil
}

// replaceExecutable swaps newFile in for exe. On Unix a same-filesystem rename
// is atomic even while the old binary runs. Windows cannot overwrite a running
// image, so the old one is moved aside first.
//
// MFI-UPD-09: the Windows rename fails while another MobFI process still
// holds the .exe open. Retries with exponential backoff so a slow GUI
// shutdown (waitForExit is 45s but a hung GUI can outlast that) does not
// silently return success without actually updating.
func replaceExecutable(exe, newFile string) error {
	if runtime.GOOS == "windows" {
		old := exe + ".old"
		_ = os.Remove(old)
		var lastErr error
		for i, wait := range []time.Duration{0, 500 * time.Millisecond, 1 * time.Second, 2 * time.Second, 4 * time.Second} {
			if wait > 0 {
				time.Sleep(wait)
			}
			if err := os.Rename(exe, old); err != nil {
				lastErr = err
				continue
			}
			if err := os.Rename(newFile, exe); err != nil {
				_ = os.Rename(old, exe) // roll back
				lastErr = err
				continue
			}
			if i > 0 {
				// Success after retry: leave a breadcrumb the operator can see.
				return nil
			}
			return nil
		}
		return fmt.Errorf("windows rename failed after retries: %w", lastErr)
	}
	return os.Rename(newFile, exe)
}

// maxBinaryBytes caps the update download size. A MobFI release binary is
// tens of MB; 512 MB is a generous ceiling that still stops a compromised
// release host or MITM from streaming multi-GB and filling the install
// volume (MFI-UPD-07).
const maxBinaryBytes = 512 << 20

func download(ctx context.Context, url, dst string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := updateClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	if resp.ContentLength > maxBinaryBytes {
		return fmt.Errorf("GET %s: content-length %d exceeds the %d byte cap", url, resp.ContentLength, maxBinaryBytes)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, maxBinaryBytes+1))
	if err == nil && n > maxBinaryBytes {
		err = fmt.Errorf("GET %s: body exceeds the %d byte cap", url, maxBinaryBytes)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst)
	}
	return err
}

// fetchBody GETs url and returns up to limit bytes of the body.
func fetchBody(ctx context.Context, url string, limit int64) ([]byte, error) {
	if url == "" {
		return nil, fmt.Errorf("empty URL")
	}
	ctx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := updateClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
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

// runInStream runs a command in dir, streaming each output line to progress as
// it appears (so a caller/log sees live output and can tell where a slow or
// hung rebuild is). env, when non-nil, replaces the child environment (nil
// inherits the parent's). A long timeout is allowed because GUI rebuilds are
// slow. On failure the error carries the last lines of output.
func runInStream(ctx context.Context, dir string, env []string, progress func(string), name string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	cmd := sysproc.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	w := &progressWriter{progress: progress}
	cmd.Stdout = w
	cmd.Stderr = w
	err := cmd.Run()
	w.flush()
	if err != nil {
		return fmt.Errorf("%w\n%s", err, w.tailStr())
	}
	return nil
}

// progressWriter splits writes into lines, forwarding each to progress and
// keeping the last few for error context. Safe for concurrent stdout+stderr.
type progressWriter struct {
	progress func(string)
	mu       sync.Mutex
	buf      []byte
	tail     []string
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		w.emit(strings.TrimRight(string(w.buf[:i]), "\r"))
		w.buf = w.buf[i+1:]
	}
	return len(p), nil
}

func (w *progressWriter) emit(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	if w.progress != nil {
		w.progress(line)
	}
	w.tail = append(w.tail, line)
	if len(w.tail) > 40 {
		w.tail = w.tail[len(w.tail)-40:]
	}
}

func (w *progressWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		w.emit(string(w.buf))
		w.buf = nil
	}
}

func (w *progressWriter) tailStr() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.Join(w.tail, "\n")
}
