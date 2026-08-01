#!/usr/bin/env bash
#
# Sign a release's SHA256SUMS.txt with the offline ed25519 signing key
# (SIGNING.md section 3), producing the SHA256SUMS.sig asset the updater
# demands before self-applying any binary update.
#
# Run this ON THE OFFLINE SIGNING MACHINE, against the exact SHA256SUMS.txt
# that scripts/dist.sh produced (the signature covers the exact bytes).
#
# Usage:
#   scripts/release-sign.sh -k <private-key> <SHA256SUMS.txt> [options]
#     -k, --key FILE   ed25519 private key from scripts/release-keygen.sh
#     -o, --out FILE   signature output (default: SHA256SUMS.sig next to input)
#     -h, --help       show this help
#
# The signature is emitted base64-encoded (internal/selfupdate.parseSignature
# accepts raw 64 bytes or base64; base64 is diff- and eyeball-friendly).
# Before writing anything the script verifies its own signature and, when the
# repo's .mobfi-pubkey.b64 anchor is present, proves the signing key matches
# it -- signing a release with a key the shipped binaries do not trust would
# otherwise be discovered by users.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

KEY=""
OUT=""
SUMS=""

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
    -k|--key)   KEY="${2:-}"; shift ;;
    --key=*)    KEY="${1#*=}" ;;
    -o|--out)   OUT="${2:-}"; shift ;;
    --out=*)    OUT="${1#*=}" ;;
    -h|--help)  sed -n '3,22p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    -*)         die "unknown option: $1 (try --help)" ;;
    *)          [ -z "$SUMS" ] || die "exactly one SHA256SUMS.txt argument expected"
                SUMS="$1" ;;
  esac
  shift
done

[ -n "$KEY" ]  || die "missing -k <private-key> (from scripts/release-keygen.sh)"
[ -n "$SUMS" ] || die "missing SHA256SUMS.txt argument (from scripts/dist.sh)"
[ -f "$KEY" ]  || die "private key not found: $KEY"
[ -s "$SUMS" ] || die "checksums file missing or empty: $SUMS"
[ -n "$OUT" ]  || OUT="$(dirname "$SUMS")/SHA256SUMS.sig"

have openssl || die "openssl not found; ed25519 needs OpenSSL 1.1.1 or newer"

# A wrong input here produces a valid signature over the wrong content, which
# is worse than no signature; insist the input looks like shasum output.
grep -qE '^[0-9a-f]{64}  ' "$SUMS" \
  || die "$SUMS does not look like shasum output (<64 hex>  <name> per line)"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

step "Signing $(basename "$SUMS") ($(wc -l < "$SUMS" | tr -d ' ') entries)"
openssl pkeyutl -sign -inkey "$KEY" -rawin -in "$SUMS" -out "$TMP/sig.raw" \
  || die "signing failed (is $KEY an ed25519 private key?)"

# Self-verify with the public half derived from the private key.
openssl pkey -in "$KEY" -pubout -out "$TMP/pub.pem"
openssl pkeyutl -verify -pubin -inkey "$TMP/pub.pem" -rawin -in "$SUMS" -sigfile "$TMP/sig.raw" >/dev/null \
  || die "self-verification failed; not writing a signature"
ok "signature self-verifies"

# Prove this key is the one the shipped binaries embed. A mismatch means the
# release would publish a signature no client accepts.
DERIVED_B64="$(openssl pkey -in "$KEY" -pubout -outform DER | tail -c 32 | openssl base64 -A)"
ANCHOR="$ROOT/.mobfi-pubkey.b64"
if [ -f "$ANCHOR" ]; then
  [ "$(tr -d ' \n\r\t' < "$ANCHOR")" = "$DERIVED_B64" ] \
    || die "this key does not match the committed trust anchor .mobfi-pubkey.b64; every shipped binary would reject the signature (rotating? see SIGNING.md section 5)"
  ok "key matches the committed .mobfi-pubkey.b64 anchor"
else
  warn "no .mobfi-pubkey.b64 in the checkout; could not cross-check the key"
  warn "against the trust anchor the binaries embed"
fi

openssl base64 -A < "$TMP/sig.raw" > "$OUT"
printf '\n' >> "$OUT"
ok "$OUT"

step "Next steps"
echo "  1. Re-check the full asset set you are about to publish:"
echo "       scripts/release-verify.sh $SUMS $OUT <asset...>"
echo "  2. Upload $(basename "$OUT") to the GitHub release alongside"
echo "     SHA256SUMS.txt and every binary asset, then publish the draft."
