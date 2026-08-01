#!/usr/bin/env bash
#
# Build the release assets for one MobFI version: the prebuilt CLI binaries
# the self-updater looks for, plus SHA256SUMS.txt over them.
#
# Asset names are load-bearing: internal/selfupdate.platformAssetName derives
# "mfi_v<version>_<goos>_<goarch>[.exe]" and the updater silently reports
# "no prebuilt binary" for any platform whose asset is missing or renamed, so
# the target list and naming here must stay in lock-step with that function.
#
# Usage:
#   scripts/dist.sh [options]
#     --require-version X.Y.Z  fail unless the source version is exactly X.Y.Z
#                              (the release workflow passes the pushed tag)
#     --targets "os/arch ..."  override the default GOOS/GOARCH list
#     --out DIR                output directory (default: dist)
#     --skip-verify            skip checksum re-check and the host smoke run
#     -h, --help               show this help
#
# The build stamps commit and date, and embeds the release-signing public key
# from .mobfi-pubkey.b64 when that file exists (see SIGNING.md). Binaries
# built without the key refuse to self-apply binary updates.
#
# After a successful run, sign the checksums on the offline machine:
#   scripts/release-sign.sh -k <private-key> dist/SHA256SUMS.txt

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# Default target list: every platform the updater is expected to serve.
TARGETS="darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64"
OUT_DIR="dist"
REQUIRE_VERSION=""
VERIFY=1

# --- pretty output -----------------------------------------------------------
if [ -t 1 ]; then
  B=$'\033[1m'; G=$'\033[32m'; Y=$'\033[33m'; R=$'\033[31m'; C=$'\033[36m'; N=$'\033[0m'
else
  B=""; G=""; Y=""; R=""; C=""; N=""
fi
step() { printf "%s==>%s %s%s%s\n" "$C" "$N" "$B" "$*" "$N"; }
ok()   { printf "  %s+%s %s\n" "$G" "$N" "$*"; }
warn() { printf "  %s!%s %s\n" "$Y" "$N" "$*" >&2; }
die()  { printf "%serror:%s %s\n" "$R" "$N" "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

# --- args --------------------------------------------------------------------
while [ $# -gt 0 ]; do
  case "$1" in
    --require-version)   REQUIRE_VERSION="${2:-}"; shift ;;
    --require-version=*) REQUIRE_VERSION="${1#*=}" ;;
    --targets)           TARGETS="${2:-}"; shift ;;
    --targets=*)         TARGETS="${1#*=}" ;;
    --out)               OUT_DIR="${2:-}"; shift ;;
    --out=*)             OUT_DIR="${1#*=}" ;;
    --skip-verify)       VERIFY=0 ;;
    -h|--help)           sed -n '3,25p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *)                   die "unknown option: $1 (try --help)" ;;
  esac
  shift
done
[ -n "$TARGETS" ] || die "--targets must not be empty"
[ -n "$OUT_DIR" ] || die "--out must not be empty"

have go || die "Go toolchain not found (run: make setup)"

# --- version consistency -----------------------------------------------------
# The version lives in two places (internal/version/version.go is canonical,
# cmd/mfi-gui/wails.json mirrors it for OS bundle metadata). A mismatch ships
# binaries that disagree with the bundle, so refuse to build one.
VERSION="$(awk '$1 == "Version" && $2 == "=" { gsub(/"/, "", $3); print $3; exit }' \
  internal/version/version.go)"
[ -n "$VERSION" ] || die "could not read Version from internal/version/version.go"

MIRROR="$(awk -F'"' '/"productVersion"/ { print $4; exit }' cmd/mfi-gui/wails.json)"
[ -n "$MIRROR" ] || die "could not read productVersion from cmd/mfi-gui/wails.json"

step "MobFI v${VERSION}"
[ "$VERSION" = "$MIRROR" ] || die "version mismatch: internal/version/version.go says ${VERSION} but cmd/mfi-gui/wails.json says ${MIRROR}; update both (see the release checklist)"
ok "internal/version/version.go and cmd/mfi-gui/wails.json agree"

if [ -n "$REQUIRE_VERSION" ]; then
  [ "$VERSION" = "$REQUIRE_VERSION" ] || die "source version is ${VERSION} but ${REQUIRE_VERSION} was required (tag and internal/version/version.go must match)"
  ok "matches required version ${REQUIRE_VERSION}"
fi

# --- ldflags -----------------------------------------------------------------
VERSION_PKG="github.com/integrisec/MobFI/internal/version"
UPDATE_PKG="github.com/integrisec/MobFI/internal/selfupdate"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || true)"
DATE="$(date -u +%Y-%m-%d)"
LDFLAGS="-X ${VERSION_PKG}.Commit=${COMMIT} -X ${VERSION_PKG}.Date=${DATE}"

# Embed the release-signing public key so shipped binaries can verify
# SHA256SUMS.sig before self-applying an update (SIGNING.md section 2).
PUBKEY_FILE="$ROOT/.mobfi-pubkey.b64"
if [ -f "$PUBKEY_FILE" ]; then
  PUBKEY="$(tr -d ' \n\r\t' < "$PUBKEY_FILE")"
  [ -n "$PUBKEY" ] || die "$PUBKEY_FILE exists but is empty; a corrupt trust anchor would brick the updater"
  if have openssl; then
    KEYLEN="$(printf '%s' "$PUBKEY" | openssl base64 -d -A 2>/dev/null | wc -c | tr -d ' ')"
    [ "$KEYLEN" = "32" ] || die "$PUBKEY_FILE does not decode to a 32-byte ed25519 key (got ${KEYLEN} bytes); regenerate it with scripts/release-keygen.sh"
  fi
  LDFLAGS="${LDFLAGS} -X ${UPDATE_PKG}.pubKeyBase64=${PUBKEY}"
  ok "embedding release-signing public key from .mobfi-pubkey.b64"
else
  warn "no .mobfi-pubkey.b64 at the repo root: these binaries cannot verify"
  warn "release signatures and will refuse to self-apply binary updates."
  warn "Generate the trust anchor with scripts/release-keygen.sh (SIGNING.md)."
fi

# --- build -------------------------------------------------------------------
mkdir -p "$OUT_DIR"
rm -f "$OUT_DIR"/mfi_v"${VERSION}"_* "$OUT_DIR"/SHA256SUMS.txt

ASSETS=()
step "Cross-compiling ${OUT_DIR}/"
for target in $TARGETS; do
  goos="${target%/*}"
  goarch="${target#*/}"
  { [ -n "$goos" ] && [ -n "$goarch" ] && [ "$goos" != "$target" ]; } \
    || die "malformed target '${target}' (want os/arch)"
  name="mfi_v${VERSION}_${goos}_${goarch}"
  [ "$goos" = "windows" ] && name="${name}.exe"
  # CGO_ENABLED=0: the CLI is cgo-free, and disabling cgo makes every target
  # cross-compile from one host and keeps the Linux binaries libc-independent.
  # -trimpath keeps the maintainer's filesystem layout out of the binaries.
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags "$LDFLAGS" -o "$OUT_DIR/$name" ./cmd/mfi
  ok "$name"
  ASSETS+=("$name")
done

# --- checksums ---------------------------------------------------------------
# The standard shasum format ("<hex>  <name>", names bare) is what
# internal/selfupdate.checksumFor parses; keep it exactly.
if have sha256sum; then SHA_BIN="sha256sum"; SHA_ARGS=""
elif have shasum;   then SHA_BIN="shasum"; SHA_ARGS="-a 256"
else die "need sha256sum or shasum to produce SHA256SUMS.txt"
fi
step "Writing SHA256SUMS.txt"
# shellcheck disable=SC2086  # SHA_ARGS is deliberately word-split ("-a 256")
( cd "$OUT_DIR" && "$SHA_BIN" $SHA_ARGS "${ASSETS[@]}" > SHA256SUMS.txt )
ok "$OUT_DIR/SHA256SUMS.txt ($(wc -l < "$OUT_DIR/SHA256SUMS.txt" | tr -d ' ') entries)"

# --- verify ------------------------------------------------------------------
if [ "$VERIFY" -eq 1 ]; then
  step "Verifying"
  # shellcheck disable=SC2086  # SHA_ARGS is deliberately word-split ("-a 256")
  ( cd "$OUT_DIR" && "$SHA_BIN" $SHA_ARGS -c SHA256SUMS.txt >/dev/null )
  ok "checksums re-verify"

  # The linker silently ignores -X against a symbol that does not exist, so
  # prove the key actually landed in the binaries rather than assuming it.
  # internal/selfupdate.pubKeyBase64 arrives with MFI-UPD-01; a tree without
  # it builds binaries that never verify signatures, and embedding is a no-op.
  if [ -n "${PUBKEY:-}" ]; then
    if grep -aqF "$PUBKEY" "$OUT_DIR/${ASSETS[0]}"; then
      ok "release-signing public key is embedded (checked ${ASSETS[0]})"
    else
      warn "the public key did NOT land in the binaries: this source tree has"
      warn "no internal/selfupdate.pubKeyBase64 symbol (pre-MFI-UPD-01)."
      warn "Harmless for these binaries -- they do not verify signatures -- but"
      warn "publish SHA256SUMS.sig anyway so verifying builds can update to them."
    fi
  fi

  # Smoke-run the asset matching this host, if one was built: it must report
  # exactly the version we think we released.
  host_goos="$(go env GOOS)"; host_goarch="$(go env GOARCH)"
  host_name="mfi_v${VERSION}_${host_goos}_${host_goarch}"
  [ "$host_goos" = "windows" ] && host_name="${host_name}.exe"
  for name in "${ASSETS[@]}"; do
    if [ "$name" = "$host_name" ]; then
      out="$("$OUT_DIR/$name" version)"
      printf '%s\n' "$out" | grep -qF "v${VERSION}" \
        || die "smoke run: '$name version' reported '${out}', expected v${VERSION}"
      ok "smoke run: $name reports ${out}"
    fi
  done
fi

# --- summary -----------------------------------------------------------------
step "Done: $((${#ASSETS[@]})) assets + SHA256SUMS.txt in ${OUT_DIR}/"
echo "  Next (release checklist):"
echo "    1. sign on the offline machine:  scripts/release-sign.sh -k <private-key> ${OUT_DIR}/SHA256SUMS.txt"
echo "    2. verify what you will publish: scripts/release-verify.sh ${OUT_DIR}/SHA256SUMS.txt ${OUT_DIR}/SHA256SUMS.sig ${OUT_DIR}/mfi_v${VERSION}_*"
echo "    3. upload every asset plus SHA256SUMS.txt and SHA256SUMS.sig to the GitHub release"
