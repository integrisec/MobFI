#!/usr/bin/env bash
#
# MobFI operator handbook generator.
#
# Concatenates the per-chapter sources in docs/handbook/ into a single
# handbook and renders a PDF.
#
# Outputs:
#   docs/handbook.md    combined markdown (source of truth for reading
#                       with working links; tracked in git)
#   docs/handbook.pdf   print artifact (tracked in git, when a PDF
#                       engine is available)
#
# Chapter sources are docs/handbook/NN-slug.md, ordered by the numeric
# prefix. Adding a chapter is: create the file with the next number.
# Nothing else needs editing; the chapter list is discovered by glob.
#
# Degrades:
#   pandoc missing        -> markdown only, exit 0
#   no PDF engine         -> markdown only, exit 0 (with install hints)
#   xelatex present       -> PDF via xelatex (preferred: better typography)
#   weasyprint present    -> PDF via weasyprint (CSS-based fallback)
#
# The markdown output is always produced, so a host without a document
# toolchain can still regenerate and commit a correct handbook.md.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC_DIR="${REPO_ROOT}/docs/handbook"
OUT_DIR="${REPO_ROOT}/docs"
OUT_MD="${OUT_DIR}/handbook.md"
OUT_PDF="${OUT_DIR}/handbook.pdf"

TITLE="MobFI Operator Handbook"
SUBTITLE="Mobile Filesystem Inspector - complete operator reference"

usage() {
  cat <<EOF
Usage: $(basename "$0") [--list-sources|--check|-h]

With no arguments, regenerates docs/handbook.md and docs/handbook.pdf
from the chapter sources in docs/handbook/.

Options:
  --list-sources   Print chapter source paths (one per line) and exit.
                   Consumed by .githooks/pre-commit for change detection.
  --check          Regenerate into a temp dir and diff against the
                   committed artifacts. Exit 1 if they differ. Used by CI
                   to catch a chapter edit committed without regenerating.
  -h, --help       This message.

Requirements:
  pandoc       required for PDF; markdown works without it
  xelatex      optional, preferred PDF engine (texlive-xetex)
  weasyprint   optional, CSS-based PDF engine fallback
EOF
}

# Chapter sources in numeric order. The glob sorts lexically, and the
# NN- prefix makes lexical order match intended order (up to 99 chapters).
list_sources() {
  local f
  for f in "${SRC_DIR}"/[0-9][0-9]-*.md; do
    [[ -e "${f}" ]] || continue
    echo "docs/handbook/$(basename "${f}")"
  done
}

MODE="build"
case "${1:-}" in
  -h|--help)      usage; exit 0 ;;
  --list-sources) MODE="list" ;;
  --check)        MODE="check" ;;
  "")             ;;
  *)              echo "unknown option: $1" >&2; usage >&2; exit 1 ;;
esac

if [[ "${MODE}" == "list" ]]; then
  list_sources
  exit 0
fi

mapfile -t SOURCES < <(list_sources)
if [[ "${#SOURCES[@]}" -eq 0 ]]; then
  echo "no chapter sources found in ${SRC_DIR}" >&2
  exit 1
fi

# Reproducible build date: the most recent commit touching any chapter
# source, so a rebuild on unchanged content is byte-identical to what is
# committed. Falls back to today when git is unavailable (e.g. a tarball
# export), which only affects the cover date.
compute_source_date_epoch() {
  if ! command -v git >/dev/null 2>&1; then
    date +%s
    return
  fi
  local epoch
  epoch=$(cd "${REPO_ROOT}" && git log -1 --format=%ct -- "${SOURCES[@]}" 2>/dev/null || true)
  if [[ -n "${epoch}" ]]; then
    echo "${epoch}"
  else
    date +%s
  fi
}

SOURCE_DATE_EPOCH="$(compute_source_date_epoch)"
export SOURCE_DATE_EPOCH
BUILD_DATE="$(date -u -d "@${SOURCE_DATE_EPOCH}" +%Y-%m-%d 2>/dev/null \
            || date -u -r "${SOURCE_DATE_EPOCH}" +%Y-%m-%d 2>/dev/null \
            || date -u +%Y-%m-%d)"

# Version stamp from the repo, so a printed handbook says which MobFI it
# documents.
MOBFI_VERSION="$(cd "${REPO_ROOT}" && git describe --tags --always --dirty 2>/dev/null || echo "unreleased")"

# generate_markdown <destination>
#
# Concatenates chapters with a YAML frontmatter block. Each chapter's
# first H1 gets a stable {#chapter-<slug>} identifier so cross-chapter
# links resolve in both the markdown and the PDF.
generate_markdown() {
  local dest="$1"
  {
    cat <<EOF
---
title: "${TITLE}"
subtitle: "${SUBTITLE}"
date: "${BUILD_DATE}"
titlepage: true
titlepage-rule-color: "4c9ffe"
toc: true
toc-own-page: true
toc-depth: 2
toc-title: "Contents"
numbersections: false
colorlinks: true
linkcolor: black
urlcolor: black
toccolor: black
papersize: letter
documentclass: report
geometry: margin=1in
author: "MobFI ${MOBFI_VERSION}"
---

EOF

    local first=true entry slug
    for entry in "${SOURCES[@]}"; do
      if [[ "${first}" == "false" ]]; then
        printf '\n\n\\newpage\n\n'
      fi
      first=false

      # docs/handbook/07-scanning.md -> scanning
      slug="$(basename "${entry}" .md)"
      slug="${slug#[0-9][0-9]-}"

      # Tag the chapter's first H1 with a stable anchor. Subsequent H1s
      # (there generally are none) pass through untouched.
      awk -v chapter="${slug}" '
        BEGIN { done = 0 }
        done == 0 && /^#[[:space:]]/ {
          print $0 " {#chapter-" chapter "}"
          done = 1
          next
        }
        { print }
      ' "${REPO_ROOT}/${entry}"
    done
  } > "${dest}"
}

# --check: regenerate to a temp file and compare against the committed
# artifact.
#
# The comparison ignores the `date:` and `author:` frontmatter lines.
# Both are derived from git state, which legitimately differs between
# the moment the pre-commit hook runs (the commit does not exist yet, so
# git reports the PREVIOUS commit) and the moment CI checks the tree out
# (git reports the new commit). Those two lines are cover-page stamps;
# the chapter content is what the check exists to protect.
if [[ "${MODE}" == "check" ]]; then
  tmp_md="$(mktemp -t handbook-check-XXXXXX.md)"
  tmp_a="$(mktemp -t handbook-check-a-XXXXXX.md)"
  tmp_b="$(mktemp -t handbook-check-b-XXXXXX.md)"
  trap 'rm -f "${tmp_md}" "${tmp_a}" "${tmp_b}"' EXIT

  if [[ ! -f "${OUT_MD}" ]]; then
    echo "handbook check: ${OUT_MD} is missing. Run: make handbook" >&2
    exit 1
  fi
  generate_markdown "${tmp_md}"

  strip_volatile() { grep -vE '^(date|author): ' "$1"; }
  strip_volatile "${OUT_MD}" > "${tmp_a}"
  strip_volatile "${tmp_md}" > "${tmp_b}"

  if ! diff -q "${tmp_a}" "${tmp_b}" >/dev/null; then
    echo "handbook check: docs/handbook.md is out of date with the chapter sources." >&2
    echo "Run: make handbook   (and commit the regenerated artifacts)" >&2
    diff -u "${tmp_a}" "${tmp_b}" | head -40 >&2
    exit 1
  fi
  echo "handbook check: docs/handbook.md is in sync with docs/handbook/*.md"
  exit 0
fi

mkdir -p "${OUT_DIR}"
generate_markdown "${OUT_MD}"
printf 'markdown: %s  (%s lines, %s chapters)\n' \
  "${OUT_MD}" "$(wc -l <"${OUT_MD}")" "${#SOURCES[@]}"

if ! command -v pandoc >/dev/null 2>&1; then
  echo "note: pandoc not installed - markdown only, PDF skipped."
  echo "      Install with: sudo apt install pandoc  (or: brew install pandoc)"
  exit 0
fi

# Disable TeX math parsing: extracted-data examples contain $-heavy
# strings (shell vars, Braintree tokens like access_token$production$...)
# that would otherwise be mis-parsed as LaTeX math. raw_tex is left
# enabled for the \newpage separators the generator emits.
PANDOC_FROM="markdown-tex_math_dollars-tex_math_single_backslash-implicit_figures"

PDF_ENGINE=""
if command -v xelatex >/dev/null 2>&1; then
  PDF_ENGINE="xelatex"
elif command -v weasyprint >/dev/null 2>&1; then
  PDF_ENGINE="weasyprint"
fi

if [[ -z "${PDF_ENGINE}" ]]; then
  echo "note: no PDF engine found - PDF skipped."
  echo "      Install one of:"
  echo "        sudo apt install texlive-xetex   (preferred)"
  echo "        pipx install weasyprint          (lighter)"
  exit 0
fi

if [[ "${PDF_ENGINE}" == "weasyprint" ]]; then
  # weasyprint renders HTML+CSS, so \newpage would appear literally.
  # Convert the separators to a CSS page break and style with the
  # handbook stylesheet.
  tmp_html_md="$(mktemp -t handbook-weasy-XXXXXX.md)"
  trap 'rm -f "${tmp_html_md}"' EXIT
  sed 's|^\\newpage$|<div class="page-break"></div>|' "${OUT_MD}" > "${tmp_html_md}"
  pandoc "${tmp_html_md}" \
    --from="${PANDOC_FROM}" \
    --standalone \
    --toc \
    --toc-depth=2 \
    --pdf-engine=weasyprint \
    --css="${REPO_ROOT}/scripts/lib/handbook.css" \
    --highlight-style=tango \
    -o "${OUT_PDF}"
else
  pandoc "${OUT_MD}" \
    --from="${PANDOC_FROM}" \
    --standalone \
    --toc \
    --toc-depth=2 \
    --pdf-engine=xelatex \
    --highlight-style=tango \
    -V mainfont="DejaVu Serif" \
    -V sansfont="DejaVu Sans" \
    -V monofont="DejaVu Sans Mono" \
    -o "${OUT_PDF}"
fi

printf 'pdf:      %s  (via %s)\n' "${OUT_PDF}" "${PDF_ENGINE}"
