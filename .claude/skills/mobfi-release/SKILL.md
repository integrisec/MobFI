---
name: mobfi-release
description: >
  Cut a MobFI release: bump the version in the two places that hold it, verify
  the gates, tag, build the platform assets, publish checksums, and confirm the
  self-updater can see the result. Triggers on "cut a release", "release
  MobFI", "bump the version", "tag a version", "publish a release", "prepare
  v1.2.0", or edits to internal/version or the version field in
  cmd/mfi-gui/wails.json. Covers the asset naming the update check expects,
  the SHA256SUMS.txt requirement, what the update flow does for a git checkout
  versus a prebuilt binary, and the post-release verification.
---

# Cutting a release

## Purpose

Ship a version without breaking the self-updater, which depends on
exact asset names and a checksums file.

## Version lives in two places

**Canonical**: `internal/version/version.go`

```go
var (
    Version = "1.0.0"   // no leading "v"
    Commit  = ""        // stamped at build time
    Date    = ""        // stamped at build time
)
```

**Mirror**: `cmd/mfi-gui/wails.json`

```json
"info": {
  "productVersion": "1.0.0"
}
```

The mirror feeds OS bundle metadata (the version shown in
Finder/Explorer properties). It is not read by Go code, so a stale
value fails silently: the binaries report the right version while
the bundle claims the old one. Update both.

`Commit` and `Date` are injected by `make build` through ldflags. A
plain `go build` leaves them empty and reports just the version.
That is intended; do not hardcode them.

## Semantic versioning

MobFI follows semver, judged from the operator's perspective:

| Change | Bump |
|---|---|
| New detection rule, renderer, differ, device support | Minor |
| New CLI flag or subcommand | Minor |
| Removing or renaming a flag, subcommand, or rule id | Major |
| Changing a report's JSON field names | Major |
| Bug fix, error-message improvement, doc change | Patch |
| Security fix with no interface change | Patch |

Rule ids are API: they appear in reports and in users' grep
pipelines. Renaming one is a breaking change.

## Pre-release checklist

```sh
make fmt
make vet
make test
make check-ascii
go build ./cmd/mfi
cd cmd/mfi-gui && wails build && cd ../..
```

CI covers everything except the GUI build, which needs cgo and
per-OS toolchains. **Build the GUI locally before tagging**: a
release that ships a broken GUI build is discovered by users.

Also confirm:

- [ ] `docs/handbook/` reflects any behaviour change, and
      `make handbook-check` passes
- [ ] `CHANGELOG.md` has an entry describing the change in operator
      terms, not commit terms
- [ ] Version bumped in `internal/version/version.go` **and**
      `cmd/mfi-gui/wails.json`
- [ ] `mfi version` reports the new version from a `make build`

## Tag

```sh
git commit -m "release: v1.2.0"
git tag v1.2.0
git push origin main --tags
```

The tag carries a leading `v`; the constant in
`internal/version/version.go` does not. The update check strips the
prefix when comparing, so both forms must be exactly consistent
apart from that `v`.

## Assets the updater expects

`selfupdate.platformAssetName` builds the name it will look for:

```go
name := fmt.Sprintf("mfi_v%s_%s_%s", ver, runtime.GOOS, runtime.GOARCH)
if runtime.GOOS == "windows" {
    name += ".exe"
}
```

So for v1.2.0 the release must carry exactly:

```
mfi_v1.2.0_darwin_arm64
mfi_v1.2.0_darwin_amd64
mfi_v1.2.0_linux_amd64
mfi_v1.2.0_linux_arm64
mfi_v1.2.0_windows_amd64.exe
SHA256SUMS.txt
```

**The names are load-bearing.** A renamed or missing asset makes the
in-place update silently unavailable for that platform: the check
still reports a newer version, but `CanApply` is false because no
matching asset was found.

`SHA256SUMS.txt` is equally load-bearing. `applyBinary` fetches it,
looks up the line for the asset it downloaded, and refuses to
install on mismatch. Its format is the standard `shasum` output:

```
<hex>  mfi_v1.2.0_darwin_arm64
```

A release without it cannot be applied at all.

The GUI is not published as a prebuilt asset: it needs per-OS cgo
and WebKit toolchains, so users build it through the install
scripts.

## Building the assets

Cross-compile the CLI (cgo-free, so this works from one host):

```sh
VERSION=1.2.0
COMMIT=$(git rev-parse --short HEAD)
DATE=$(date -u +%Y-%m-%d)
PKG=github.com/integrisec/MobFI/internal/version
LDFLAGS="-X $PKG.Commit=$COMMIT -X $PKG.Date=$DATE"

for target in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64; do
  GOOS=${target%/*} GOARCH=${target#*/}
  out="mfi_v${VERSION}_${GOOS}_${GOARCH}"
  [ "$GOOS" = windows ] && out="$out.exe"
  GOOS=$GOOS GOARCH=$GOARCH go build -ldflags "$LDFLAGS" -o "dist/$out" ./cmd/mfi
done

cd dist && shasum -a 256 mfi_v${VERSION}_* > SHA256SUMS.txt
```

Verify before publishing:

```sh
cd dist && shasum -a 256 -c SHA256SUMS.txt
./mfi_v1.2.0_linux_amd64 version    # matches the tag, has commit and date
```

## Publish

Create the GitHub release on the tag, attach every asset plus
`SHA256SUMS.txt`, and write release notes an operator can act on:
what changed, anything that breaks, and anything requiring manual
steps.

The update check reads the repository identified by
`version.RepoOwner` / `version.RepoName`, and looks at the **latest**
release. A draft or pre-release is not picked up.

## Post-release verification

The update path is easy to break and hard to notice, so check it:

```sh
mfi update            # sees the new version, prints the release URL
mfi update -json      # assetUrl and checksumsUrl are populated
```

An empty `assetUrl` means the asset name does not match what
`platformAssetName` builds: fix the asset name on the release rather
than the code.

Then test an actual apply from a previous version, on at least one
platform:

```sh
mfi update -apply
mfi version           # reports the new version
```

Both update paths deserve a look when either changes:

- **Git checkout**: `git pull --ff-only` from the public HTTPS URL,
  then a rebuild via the install script. HTTPS rather than the
  configured remote, so an unattended update needs no SSH key.
- **Prebuilt binary**: download, verify SHA-256, replace the running
  executable. Atomic rename on Unix; on Windows the old binary is
  moved aside first and rolled back if the swap fails.

## Handbook and changelog

`docs/handbook/14-updating.md` documents the update flow to
operators. `CHANGELOG.md` is the operator-facing log: entries
describe capability changes, not implementation detail.

## Cross-references

- `mobfi-architecture`: the build and CI gates
- `mobfi-gui-binding`: build the GUI before tagging
