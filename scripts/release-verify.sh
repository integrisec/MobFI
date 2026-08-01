#!/usr/bin/env bash
#
# Verify a MobFI release asset set the way the self-updater will: check the
# ed25519 signature over SHA256SUMS.txt against the committed trust anchor,
# then check each downloaded asset against its checksum line.
#
# Use it as the maintainer's pre-publish gate, or as an operator verifying a
# manual download.
#
# Usage:
#   scripts/release-verify.sh [options] <SHA256SUMS.txt> <SHA256SUMS.sig> [asset...]
#     -p, --pubkey FILE  base64 raw-32-byte ed25519 public key
#                        (default: .mobfi-pubkey.b64 at the repo root)
#     -h, --help         show this help
#
# The signature file may hold raw 64 bytes or base64, mirroring what
# internal/selfupdate.parseSignature accepts. Exit status is non-zero on any
# failure, so the release workflow can gate on it.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

PUBKEY_FILE="$ROOT/.mobfi-pubkey.b64"
SUMS=""
SIG=""
ASSETS=()

if [ -t 1 ]; then
  B=$'\033[1m'; G=$'\033[32m'; R=$'\033[31m'; C=$'\033[36m'; N=$'\033[0m'
else
  B=""; G=""; R=""; C=""; N=""
fi
step() { printf "%s==>%s %s%s%s\n" "$C" "$N" "$B" "$*" "$N"; }
ok()   { printf "  %s+%s %s\n" "$G" "$N" "$*"; }
die()  { printf "%serror:%s %s\n" "$R" "$N" "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

while [ $# -gt 0 ]; do
  case "$1" in
    -p|--pubkey) PUBKEY_FILE="${2:-}"; shift ;;
    --pubkey=*)  PUBKEY_FILE="${1#*=}" ;;
    -h|--help)   sed -n '3,19p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    -*)          die "unknown option: $1 (try --help)" ;;
    *)           if   [ -z "$SUMS" ]; then SUMS="$1"
                 elif [ -z "$SIG" ];  then SIG="$1"
                 else ASSETS+=("$1")
                 fi ;;
  esac
  shift
done

[ -n "$SUMS" ] || die "missing SHA256SUMS.txt argument (try --help)"
[ -n "$SIG" ]  || die "missing SHA256SUMS.sig argument (try --help)"
[ -s "$SUMS" ] || die "checksums file missing or empty: $SUMS"
[ -s "$SIG" ]  || die "signature file missing or empty: $SIG"
[ -f "$PUBKEY_FILE" ] || die "no public key at $PUBKEY_FILE; pass -p, or generate the trust anchor with scripts/release-keygen.sh (SIGNING.md)"

have openssl || die "openssl not found; ed25519 needs OpenSSL 1.1.1 or newer"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# --- signature over SHA256SUMS.txt ------------------------------------------
step "Verifying $(basename "$SIG") over $(basename "$SUMS")"

B64="$(tr -d ' \n\r\t' < "$PUBKEY_FILE")"
# Rebuild the DER SubjectPublicKeyInfo openssl wants: the fixed 12-byte
# ed25519 header followed by the raw 32-byte key.
{
  printf '\x30\x2a\x30\x05\x06\x03\x2b\x65\x70\x03\x21\x00'
  printf '%s' "$B64" | openssl base64 -d -A
} > "$TMP/pub.der"
[ "$(wc -c < "$TMP/pub.der" | tr -d ' ')" = "44" ] \
  || die "$PUBKEY_FILE does not decode to a 32-byte ed25519 key"

# Accept raw 64-byte or base64 signatures, like the updater does.
if [ "$(wc -c < "$SIG" | tr -d ' ')" = "64" ]; then
  cp "$SIG" "$TMP/sig.raw"
else
  tr -d ' \n\r\t' < "$SIG" | openssl base64 -d -A > "$TMP/sig.raw" \
    || die "signature is neither raw 64 bytes nor valid base64"
  [ "$(wc -c < "$TMP/sig.raw" | tr -d ' ')" = "64" ] \
    || die "decoded signature is not 64 bytes"
fi

openssl pkeyutl -verify -pubin -keyform DER -inkey "$TMP/pub.der" \
  -rawin -in "$SUMS" -sigfile "$TMP/sig.raw" >/dev/null 2>&1 \
  || die "SIGNATURE VERIFICATION FAILED: $SUMS is not signed by the trusted key. Do not publish or install these assets."
ok "signature valid against $(basename "$PUBKEY_FILE")"

# --- per-asset checksums -----------------------------------------------------
if [ "${#ASSETS[@]}" -gt 0 ]; then
  if have sha256sum; then SHA_BIN="sha256sum"; SHA_ARGS=""
  elif have shasum;   then SHA_BIN="shasum"; SHA_ARGS="-a 256"
  else die "need sha256sum or shasum to verify assets"
  fi

  step "Verifying ${#ASSETS[@]} asset(s) against $(basename "$SUMS")"
  for asset in "${ASSETS[@]}"; do
    [ -f "$asset" ] || die "asset not found: $asset"
    name="$(basename "$asset")"
    expected="$(awk -v n="$name" '$2 == n { print $1; exit }' "$SUMS")"
    [ -n "$expected" ] || die "$name has no entry in $SUMS; the updater would refuse it"
    # shellcheck disable=SC2086  # SHA_ARGS is deliberately word-split ("-a 256")
    actual="$("$SHA_BIN" $SHA_ARGS "$asset" | awk '{ print $1 }')"
    [ "$actual" = "$expected" ] \
      || die "CHECKSUM MISMATCH for $name: expected $expected, got $actual. Do not publish or install this asset."
    ok "$name"
  done
fi

step "All checks passed"
