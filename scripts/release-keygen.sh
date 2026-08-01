#!/usr/bin/env bash
#
# Generate the MobFI release-signing keypair (SIGNING.md section 1).
#
# Run this ON THE OFFLINE SIGNING MACHINE. It produces:
#   <out-dir>/mobfi-release.private      ed25519 private key -- NEVER leaves
#                                        that machine, never enters the repo
#   <out-dir>/mobfi-release.public.pem   public key, PEM (convenience copy)
#   <out-dir>/mobfi-release.pubkey.b64   raw 32-byte public key, base64
#   <repo>/.mobfi-pubkey.b64             the committed trust anchor every
#                                        build embeds via ldflags
#
# Usage:
#   scripts/release-keygen.sh [options]
#     --out-dir DIR   where to write the keypair
#                     (default: $HOME/mobfi-release-keys)
#     --no-anchor     do not write <repo>/.mobfi-pubkey.b64
#     --rotate        allow replacing an existing .mobfi-pubkey.b64
#     -h, --help      show this help
#
# Rotation (SIGNING.md section 5): the updater holds a single-key slot, so
# binaries built against the old anchor reject signatures from the new key.
# Rotate by shipping a release built with the new anchor while publishing
# checksums signed by both keys for one transition release.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

OUT_DIR="${HOME}/mobfi-release-keys"
WRITE_ANCHOR=1
ROTATE=0

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

while [ $# -gt 0 ]; do
  case "$1" in
    --out-dir)   OUT_DIR="${2:-}"; shift ;;
    --out-dir=*) OUT_DIR="${1#*=}" ;;
    --no-anchor) WRITE_ANCHOR=0 ;;
    --rotate)    ROTATE=1 ;;
    -h|--help)   sed -n '3,27p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *)           die "unknown option: $1 (try --help)" ;;
  esac
  shift
done
[ -n "$OUT_DIR" ] || die "--out-dir must not be empty"

have openssl || die "openssl not found; ed25519 needs OpenSSL 1.1.1 or newer"

PRIV="$OUT_DIR/mobfi-release.private"
PUB_PEM="$OUT_DIR/mobfi-release.public.pem"
PUB_B64="$OUT_DIR/mobfi-release.pubkey.b64"
ANCHOR="$ROOT/.mobfi-pubkey.b64"

# Never overwrite a signing key: losing the original means every distributed
# binary must be rebuilt against a new anchor.
[ -e "$PRIV" ] && die "$PRIV already exists; refusing to overwrite a signing key (pick another --out-dir, or rotate per SIGNING.md section 5)"

# Probe ed25519 support before creating anything (LibreSSL and OpenSSL < 1.1.1
# lack it, and the failure mode mid-run is confusing).
if ! openssl genpkey -algorithm ed25519 2>/dev/null | grep -q "PRIVATE KEY"; then
  die "this openssl ($(openssl version 2>/dev/null)) cannot generate ed25519 keys; need OpenSSL 1.1.1+"
fi

step "Generating the ed25519 release-signing keypair"
mkdir -p "$OUT_DIR"
chmod 700 "$OUT_DIR"
umask 077
openssl genpkey -algorithm ed25519 -out "$PRIV"
chmod 600 "$PRIV"
ok "$PRIV (mode 600 -- keep OFFLINE: password manager or hardware token)"

openssl pkey -in "$PRIV" -pubout -out "$PUB_PEM"
ok "$PUB_PEM"

# The raw 32-byte key is the last 32 bytes of the 44-byte DER SubjectPublicKeyInfo;
# base64 of those raw bytes is what internal/selfupdate.pubKeyBase64 expects.
B64="$(openssl pkey -in "$PRIV" -pubout -outform DER | tail -c 32 | openssl base64 -A)"
KEYLEN="$(printf '%s' "$B64" | openssl base64 -d -A | wc -c | tr -d ' ')"
[ "$KEYLEN" = "32" ] || die "internal error: extracted public key is ${KEYLEN} bytes, want 32"
printf '%s\n' "$B64" > "$PUB_B64"
ok "$PUB_B64"

# Prove the pair signs and verifies before anyone trusts it.
step "Round-trip check (sign + verify a probe message)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
printf 'mobfi keygen probe\n' > "$TMP/msg"
openssl pkeyutl -sign -inkey "$PRIV" -rawin -in "$TMP/msg" -out "$TMP/sig"
openssl pkeyutl -verify -pubin -inkey "$PUB_PEM" -rawin -in "$TMP/msg" -sigfile "$TMP/sig" >/dev/null \
  || die "round-trip verification failed; do not use this keypair"
ok "keypair verifies"

if [ "$WRITE_ANCHOR" -eq 1 ]; then
  if [ -f "$ANCHOR" ] && [ "$(tr -d ' \n\r\t' < "$ANCHOR")" != "$B64" ] && [ "$ROTATE" -ne 1 ]; then
    die "$ANCHOR already holds a different key; pass --rotate if you really are rotating (SIGNING.md section 5: old binaries will reject the new key's signatures)"
  fi
  printf '%s\n' "$B64" > "$ANCHOR"
  ok "$ANCHOR (commit this file; it is the public half only)"
fi

step "Next steps"
echo "  1. Store $PRIV offline (hardware token or password manager); it never"
echo "     enters the repo, CI, or any networked backup."
echo "  2. Commit .mobfi-pubkey.b64 so builds embed the trust anchor"
echo "     (scripts/dist.sh and the installers pick it up automatically)."
echo "  3. Sign each release's checksums on this machine:"
echo "       scripts/release-sign.sh -k $PRIV dist/SHA256SUMS.txt"
