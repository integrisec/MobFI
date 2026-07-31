// Package selfupdate checks whether a newer MobFI is available, without
// changing anything on disk. It reports two independent signals:
//
//   - a newer published GitHub release than the running version (works for any
//     install, including prebuilt binaries), and
//   - when running inside the MobFI git checkout, how many commits the local
//     branch is behind its upstream.
//
// The check is entirely advisory: both frontends surface it and let the user
// decide how to update (download the release, or `git pull` + rebuild). No
// files are modified here.
package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/integrisec/MobFI/internal/sysproc"
	"github.com/integrisec/MobFI/internal/version"
)

// Info is the result of an update check. It is JSON-serialisable so the GUI
// binding can hand it straight to the frontend.
type Info struct {
	Current     string    `json:"current"`     // running version (no leading "v")
	Latest      string    `json:"latest"`      // latest release version (no leading "v"); empty if unknown
	Available   bool      `json:"available"`   // a newer release than Current exists
	ReleaseURL  string    `json:"releaseUrl"`  // html_url of the latest release
	ReleaseName string    `json:"releaseName"` // release title
	Notes       string    `json:"notes"`       // release body (truncated)
	PublishedAt time.Time `json:"publishedAt"` // when the latest release was published

	// Source-checkout status, best effort and only when running inside the
	// MobFI git repo. GitBehind > 0 means `git pull` would bring in new commits.
	GitCheckout bool   `json:"gitCheckout"`
	GitBranch   string `json:"gitBranch"`
	GitBehind   int    `json:"gitBehind"`
	GitError    string `json:"gitError,omitempty"`

	// Release asset for THIS platform, used by a binary (non-git) self-update.
	AssetName    string `json:"assetName,omitempty"`
	AssetURL     string `json:"assetUrl,omitempty"`
	ChecksumsURL string `json:"checksumsUrl,omitempty"`
	// SignatureURL is the browser download URL of the ed25519 signature over
	// SHA256SUMS.txt (`SHA256SUMS.sig`). Empty if the release does not
	// publish one; applyBinary refuses to install without it.
	SignatureURL string `json:"signatureUrl,omitempty"`

	// CanApply reports whether Apply can perform the update in place: true in a
	// git checkout, or when a matching prebuilt asset exists for a newer release.
	CanApply bool `json:"canApply"`
}

const (
	releasesAPI    = "https://api.github.com/repos/%s/%s/releases/latest"
	checksumsAsset = "SHA256SUMS.txt"
	signatureAsset = "SHA256SUMS.sig"
	maxNotesLen    = 4000
	httpTimeout    = 6 * time.Second
	gitTimeout     = 8 * time.Second
	userAgent      = "MobFI-update-check"
)

// Check queries the latest release and (best effort) the local git checkout,
// reporting whether an update is available. A failed release lookup returns
// the partial Info plus the error; git problems are recorded in Info.GitError
// rather than failing the whole check. Callers may treat any error as
// "unknown -- skip the notice".
func Check(ctx context.Context) (*Info, error) {
	info := &Info{Current: version.Version}

	// Local git status first: fast, offline-friendly, and useful on its own.
	fillGitStatus(ctx, info)

	rel, err := latestRelease(ctx)
	if err != nil {
		return info, err
	}
	info.Latest = strings.TrimPrefix(rel.TagName, "v")
	info.ReleaseURL = rel.HTMLURL
	info.ReleaseName = rel.Name
	info.Notes = truncate(rel.Body, maxNotesLen)
	info.PublishedAt = rel.PublishedAt
	if info.Latest != "" && Compare(info.Latest, info.Current) > 0 {
		info.Available = true
	}

	// Locate the prebuilt asset for this platform + the checksums file + its
	// ed25519 signature, so a standalone binary install can self-update from
	// the release (applyBinary requires the signature).
	info.AssetName = platformAssetName(info.Latest)
	for _, a := range rel.Assets {
		switch a.Name {
		case info.AssetName:
			info.AssetURL = a.BrowserDownloadURL
		case checksumsAsset:
			info.ChecksumsURL = a.BrowserDownloadURL
		case signatureAsset:
			info.SignatureURL = a.BrowserDownloadURL
		}
	}

	// Apply is possible in a git checkout (pull + rebuild) or, for a newer
	// release, when a matching prebuilt binary asset is available to swap in.
	info.CanApply = info.GitCheckout || (info.Available && info.AssetURL != "")
	return info, nil
}

// platformAssetName is the release asset name for the running OS/arch, matching
// the names published by the release workflow (e.g. mfi_v1.0.0_darwin_arm64,
// mfi_v1.0.0_windows_amd64.exe).
func platformAssetName(ver string) string {
	name := fmt.Sprintf("mfi_v%s_%s_%s", ver, runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	HTMLURL     string    `json:"html_url"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func latestRelease(ctx context.Context) (*ghRelease, error) {
	ctx, cancel := context.WithTimeout(ctx, httpTimeout)
	defer cancel()

	url := fmt.Sprintf(releasesAPI, version.RepoOwner, version.RepoName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := updateClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update check: GitHub returned %s", resp.Status)
	}
	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// fillGitStatus populates the Git* fields when the process is running inside
// the MobFI git checkout. Every failure is soft: it either leaves GitCheckout
// false or records a short reason in GitError.
func fillGitStatus(ctx context.Context, info *Info) {
	git, err := exec.LookPath("git")
	if err != nil {
		return // no git; not a source install we can reason about
	}
	dir := repoDir(ctx, git)
	if dir == "" {
		return // not inside the MobFI repo
	}
	info.GitCheckout = true

	if branch, err := gitOut(ctx, git, dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		info.GitBranch = branch
	}
	// Refresh remote refs so the behind-count reflects reality. Best effort:
	// offline or auth-gated fetches just leave the count based on stale refs.
	_, _ = gitOut(ctx, git, dir, "fetch", "--quiet")

	behind, err := gitOut(ctx, git, dir, "rev-list", "--count", "HEAD..@{upstream}")
	if err != nil {
		info.GitError = "no upstream tracking branch"
		return
	}
	if n, err := strconv.Atoi(behind); err == nil {
		info.GitBehind = n
	}
}

// repoDir returns the top-level directory of the MobFI git checkout the process
// is running from -- trying the working directory then the executable's
// directory -- or "" if neither is inside a MobFI repo. It confirms the origin
// remote points at MobFI so an unrelated repo the user happens to launch from
// is not mistaken for a MobFI source install.
func repoDir(ctx context.Context, git string) string {
	seen := map[string]bool{}
	var candidates []string
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, wd)
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(exe))
	}
	// A GUI installed to /Applications (or launched from a shortcut) runs
	// detached from the source tree, so also consult the checkout path the
	// installer recorded. This lets "Update now" git-pull + rebuild even when
	// the running app is nowhere near the repo.
	if rec := recordedRepoDir(); rec != "" {
		candidates = append(candidates, rec)
	}
	for _, c := range candidates {
		top, err := gitOut(ctx, git, c, "rev-parse", "--show-toplevel")
		if err != nil || top == "" || seen[top] {
			continue
		}
		seen[top] = true
		if origin, err := gitOut(ctx, git, top, "remote", "get-url", "origin"); err == nil {
			if strings.Contains(strings.ToLower(origin), strings.ToLower(version.RepoName)) {
				return top
			}
		}
	}
	return ""
}

// recordedRepoDir returns the source-checkout path the installer saved (see
// scripts/install.*), or "" if none. The file lives under the OS config dir so
// it matches os.UserConfigDir() used by the frontends.
func recordedRepoDir() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, "MobFI", "source-repo.txt"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func gitOut(ctx context.Context, git, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	cmd := sysproc.CommandContext(ctx, git, append([]string{"-C", dir}, args...)...)
	cmd.Env = gitEnv()
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// gitEnv hardens git subprocesses so an unattended fetch/pull never blocks on an
// interactive prompt (host-key, credential, or SSH passphrase) -- there is no
// terminal to answer it in the detached update worker. It fails fast instead,
// which the worker can report, rather than hanging until the timeout. Keychain-
// or agent-provided keys still authenticate (that is non-interactive).
func gitEnv() []string {
	return append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ConnectTimeout=15",
	)
}

// Compare compares two dotted numeric version strings (e.g. "1.2.0" vs
// "1.10.3"), returning -1 if a < b, 0 if equal, and 1 if a > b. Any leading
// "v" is ignored, missing components count as 0, and a pre-release/build
// suffix on a component (after '-' or '+') is dropped before comparing.
func Compare(a, b string) int {
	pa, pb := splitVersion(a), splitVersion(b)
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
	}
	return 0
}

func splitVersion(s string) []int {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "v"))
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ".")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		// Trim a trailing pre-release stuck to a component just in case.
		if i := strings.IndexAny(p, "-+"); i >= 0 {
			p = p[:i]
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			n = 0
		}
		nums = append(nums, n)
	}
	return nums
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return strings.TrimSpace(s[:max]) + "\n..."
}
