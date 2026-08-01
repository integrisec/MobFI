#!/usr/bin/env sh
# Fail if any PowerShell script contains non-ASCII bytes.
#
# Windows PowerShell 5.1 reads a BOM-less file as the system ANSI code page
# (Windows-1252), not UTF-8. A byte >0x7F -- an em-dash or a "smart" quote
# pasted from a doc -- then decodes to a character PowerShell treats as a
# string delimiter, which breaks parsing in ways that surface far from the
# real line (see the install.ps1 history). PowerShell 7 reads UTF-8 and hides
# the bug, so keep .ps1 files pure ASCII to stay encoding-independent.
set -eu

root="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"

# Nothing to check?
if [ -z "$(find "$root" -name '*.ps1' -not -path '*/.git/*' | head -n1)" ]; then
	echo "check-ascii: no .ps1 files to check"
	exit 0
fi

# LC_ALL=C makes perl treat input as raw bytes; close(ARGV) resets $. per file
# so line numbers are correct across multiple files.
# shellcheck disable=SC2016  # $ARGV and $. are Perl variables, not shell
bad="$(
	find "$root" -name '*.ps1' -not -path '*/.git/*' -print0 |
		LC_ALL=C xargs -0 perl -ne 'print "$ARGV:$.: $_" if /[^\x00-\x7F]/; close ARGV if eof'
)"

if [ -n "$bad" ]; then
	echo "Non-ASCII characters found in PowerShell script(s):"
	echo "$bad"
	echo
	echo "Windows PowerShell 5.1 mis-parses bytes >0x7F (em-dashes, smart quotes,"
	echo "etc.) in BOM-less files. Replace them with ASCII equivalents."
	exit 1
fi

echo "check-ascii: all .ps1 files are pure ASCII"
