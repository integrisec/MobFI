#!/usr/bin/env bash
#
# Print the CHANGELOG.md section for one version, for use as GitHub release
# notes. The release workflow runs this against the pushed tag, so cutting a
# release without a changelog entry fails fast instead of publishing an empty
# notes body.
#
# Usage:
#   scripts/release-notes.sh <version | vX.Y.Z> [changelog]
#     version    with or without the leading "v" (v1.2.0 and 1.2.0 both work)
#     changelog  path to the changelog (default: CHANGELOG.md at the repo root)
#
# Matches a Keep-a-Changelog heading of the form "## [X.Y.Z] - date" and
# prints everything up to the next "## [" heading, trimmed of surrounding
# blank lines.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

die() { printf 'error: %s\n' "$*" >&2; exit 1; }

[ $# -ge 1 ] || die "usage: scripts/release-notes.sh <version> [changelog] (try --help)"
case "$1" in
  -h|--help) sed -n '3,15p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
esac

VERSION="${1#v}"
CHANGELOG="${2:-$ROOT/CHANGELOG.md}"
[ -f "$CHANGELOG" ] || die "changelog not found: $CHANGELOG"

NOTES="$(awk -v ver="$VERSION" '
  index($0, "## [" ver "]") == 1 { grab = 1; next }
  grab && /^## \[/              { exit }
  grab                          { lines[++n] = $0 }
  END {
    while (n > 0 && lines[n] ~ /^[[:space:]]*$/) n--
    s = 1
    while (s <= n && lines[s] ~ /^[[:space:]]*$/) s++
    for (i = s; i <= n; i++) print lines[i]
  }
' "$CHANGELOG")"

if [ -z "$NOTES" ]; then
  {
    printf 'error: no "## [%s]" section in %s\n' "$VERSION" "$CHANGELOG"
    printf 'The release checklist requires a changelog entry per release. Sections present:\n'
    grep '^## \[' "$CHANGELOG" | sed 's/^/  /' || printf '  (none)\n'
  } >&2
  exit 1
fi

printf '%s\n' "$NOTES"
