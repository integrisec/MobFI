// Package version records the MobFI release version, shared by both the CLI
// and the GUI so they always report the same number.
package version

// Version is the canonical semantic version of MobFI (no leading "v"). Bump it
// here on each release — it is the single source of truth the binaries report.
//
// Commit and Date are optional build stamps injected at build time via, e.g.:
//
//	go build -ldflags "\
//	  -X github.com/integrisec/MobFI/internal/version.Commit=$(git rev-parse --short HEAD) \
//	  -X github.com/integrisec/MobFI/internal/version.Date=$(date -u +%Y-%m-%d)" ./cmd/mfi
//
// A plain `go build` leaves them empty and reports just the version.
var (
	Version = "1.0.0"
	Commit  = ""
	Date    = ""
)

// Repo identifies the GitHub repository MobFI releases are published to, used
// by the update check to find the latest release.
const (
	RepoOwner = "integrisec"
	RepoName  = "MobFI"
)

// String renders the version for display (e.g. "v1.0.0" or, when stamped,
// "v1.0.0 (a1b2c3d, 2026-07-24)").
func String() string {
	s := "v" + Version
	switch {
	case Commit != "" && Date != "":
		s += " (" + Commit + ", " + Date + ")"
	case Commit != "":
		s += " (" + Commit + ")"
	case Date != "":
		s += " (" + Date + ")"
	}
	return s
}
