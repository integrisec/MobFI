#!/usr/bin/env bash
#
# MobFI installer for macOS and Linux.
#
# Resolves everything a newcomer needs — the Go toolchain, the Wails CLI and
# its native GUI toolchain, and the runtime tools MobFI shells out to (Android
# platform-tools / adb, libimobiledevice for iOS) — then builds the CLI and
# GUI. Safe to re-run: anything already present is left alone.
#
# Usage:
#   scripts/install.sh [options]
#     --cli-only            build the CLI, skip the GUI (no Wails/webkit needed)
#     --gui-only            build the GUI, skip the standalone CLI
#     --no-runtime-tools    skip adb/libimobiledevice (toolchain + build only)
#     --launch <cli|gui>    after building, run the CLI wizard or open the GUI
#     -h, --help            show this help
#
# Runtime tools are best-effort: a package that won't install prints a warning
# and the script continues (MobFI degrades gracefully when a tool is absent).

set -euo pipefail

GO_MIN="1.23"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

BUILD_CLI=1
BUILD_GUI=1
INSTALL_RUNTIME=1
LAUNCH=""

# --- pretty output -----------------------------------------------------------
if [ -t 1 ]; then
  B=$'\033[1m'; G=$'\033[32m'; Y=$'\033[33m'; R=$'\033[31m'; C=$'\033[36m'; N=$'\033[0m'
else
  B=""; G=""; Y=""; R=""; C=""; N=""
fi
step() { printf "%s==>%s %s%s%s\n" "$C" "$N" "$B" "$*" "$N"; }
ok()   { printf "  %s✓%s %s\n" "$G" "$N" "$*"; }
warn() { printf "  %s!%s %s\n" "$Y" "$N" "$*" >&2; }
die()  { printf "%serror:%s %s\n" "$R" "$N" "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

# --- args --------------------------------------------------------------------
while [ $# -gt 0 ]; do
  case "$1" in
    --cli-only)         BUILD_GUI=0 ;;
    --gui-only)         BUILD_CLI=0 ;;
    --no-runtime-tools) INSTALL_RUNTIME=0 ;;
    --launch)           LAUNCH="${2:-}"; shift ;;
    --launch=*)         LAUNCH="${1#*=}" ;;
    -h|--help)          sed -n '3,20p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *)                  die "unknown option: $1 (try --help)" ;;
  esac
  shift
done
[ -n "$LAUNCH" ] && [ "$LAUNCH" != "cli" ] && [ "$LAUNCH" != "gui" ] && die "--launch expects 'cli' or 'gui'"

OS="$(uname -s)"
SUDO=""
[ "$(id -u)" -ne 0 ] && have sudo && SUDO="sudo"

# --- Go ----------------------------------------------------------------------
# version_ge A B -> true if version A >= B (dotted numeric compare)
version_ge() { [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -1)" = "$2" ]; }

go_version() { go env GOVERSION 2>/dev/null | sed 's/^go//'; }

ensure_go() {
  if have go && version_ge "$(go_version)" "$GO_MIN"; then
    ok "Go $(go_version) (>= $GO_MIN)"
    return
  fi
  step "Installing Go (>= $GO_MIN)"
  case "$OS" in
    Darwin)
      ensure_brew
      brew install go
      ;;
    Linux)
      local ver arch tgz
      ver="$(curl -fsSL 'https://go.dev/VERSION?m=text' | head -1)"   # e.g. go1.23.5
      [ -n "$ver" ] || die "could not determine the latest Go version"
      case "$(uname -m)" in
        x86_64|amd64)   arch=amd64 ;;
        aarch64|arm64)  arch=arm64 ;;
        armv6l|armv7l)  arch=armv6l ;;
        *)              die "unsupported CPU architecture: $(uname -m)" ;;
      esac
      tgz="${ver}.linux-${arch}.tar.gz"
      curl -fsSL "https://go.dev/dl/${tgz}" -o "/tmp/${tgz}"
      $SUDO rm -rf /usr/local/go
      $SUDO tar -C /usr/local -xzf "/tmp/${tgz}"
      rm -f "/tmp/${tgz}"
      export PATH="/usr/local/go/bin:$PATH"
      ;;
    *) die "unsupported OS: $OS" ;;
  esac
  have go && version_ge "$(go_version)" "$GO_MIN" || die "Go install did not take effect; open a new shell and re-run"
  ok "Go $(go_version)"
}

# Make `go install` binaries (wails) reachable this session.
add_gobin_to_path() {
  local gobin; gobin="$(go env GOBIN 2>/dev/null)"
  [ -n "$gobin" ] || gobin="$(go env GOPATH)/bin"
  export PATH="$gobin:$PATH"
  GOBIN_DIR="$gobin"
}

# --- Homebrew (macOS) --------------------------------------------------------
ensure_brew() {
  have brew && return
  step "Installing Homebrew"
  /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
  # Make brew available for the rest of this run.
  if [ -x /opt/homebrew/bin/brew ]; then eval "$(/opt/homebrew/bin/brew shellenv)"
  elif [ -x /usr/local/bin/brew ]; then eval "$(/usr/local/bin/brew shellenv)"; fi
  have brew || die "Homebrew install did not take effect"
}

# --- Linux package manager ---------------------------------------------------
detect_pm() {
  for pm in apt-get dnf pacman zypper; do have "$pm" && { echo "$pm"; return; }; done
  echo ""
}

# best-effort: install packages one at a time so a single missing name doesn't
# abort the batch.
pm_install_each() {
  local pm="$1"; shift
  local p
  for p in "$@"; do
    case "$pm" in
      apt-get) $SUDO apt-get install -y "$p" >/dev/null 2>&1 && ok "$p" || warn "could not install '$p' (skipping)";;
      dnf)     $SUDO dnf install -y "$p"     >/dev/null 2>&1 && ok "$p" || warn "could not install '$p' (skipping)";;
      pacman)  $SUDO pacman -S --needed --noconfirm "$p" >/dev/null 2>&1 && ok "$p" || warn "could not install '$p' (skipping)";;
      zypper)  $SUDO zypper --non-interactive install "$p" >/dev/null 2>&1 && ok "$p" || warn "could not install '$p' (skipping)";;
    esac
  done
}

# --- GUI toolchain (Wails + native webview deps) -----------------------------
ensure_gui_toolchain() {
  step "GUI build toolchain (Wails)"
  case "$OS" in
    Darwin)
      if ! xcode-select -p >/dev/null 2>&1; then
        warn "Xcode Command Line Tools missing — launching the installer (finish it, then re-run if the GUI build fails)"
        xcode-select --install || true
      else
        ok "Xcode Command Line Tools"
      fi
      ;;
    Linux)
      local pm; pm="$(detect_pm)"
      [ -n "$pm" ] || { warn "no supported package manager; install GTK3 + WebKit2GTK dev packages manually"; return; }
      step "Installing GTK3 + WebKit2GTK development packages"
      case "$pm" in
        apt-get)
          $SUDO apt-get update -y >/dev/null 2>&1 || true
          pm_install_each "$pm" build-essential pkg-config libgtk-3-dev
          # WebKit dev package name differs across releases; try newest first.
          pm_install_each "$pm" libwebkit2gtk-4.1-dev || true
          have pkg-config && pkg-config --exists webkit2gtk-4.1 2>/dev/null || pm_install_each "$pm" libwebkit2gtk-4.0-dev
          ;;
        dnf)    pm_install_each "$pm" gcc pkgconf-pkg-config gtk3-devel webkit2gtk4.1-devel ;;
        pacman) pm_install_each "$pm" base-devel pkgconf gtk3 webkit2gtk ;;
        zypper) pm_install_each "$pm" gcc pkg-config gtk3-devel webkit2gtk3-soup2-devel ;;
      esac
      ;;
  esac

  add_gobin_to_path
  if have wails; then
    ok "Wails $(wails version 2>/dev/null | tr -d '\n')"
  else
    step "Installing the Wails CLI"
    go install github.com/wailsapp/wails/v2/cmd/wails@latest
    have wails && ok "Wails installed to ${GOBIN_DIR}" || warn "wails not on PATH; add ${GOBIN_DIR} to PATH"
  fi
}

# --- Runtime tools (adb + libimobiledevice) ----------------------------------
ensure_runtime_tools() {
  [ "$INSTALL_RUNTIME" -eq 1 ] || { warn "skipping runtime tools (--no-runtime-tools)"; return; }
  step "Device tools (Android adb + iOS libimobiledevice)"
  case "$OS" in
    Darwin)
      ensure_brew
      have adb || brew install --cask android-platform-tools >/dev/null 2>&1 && ok "adb" || warn "install adb: brew install --cask android-platform-tools"
      # libimobiledevice provides idevice_id/ideviceinfo/afcclient; iproxy from
      # libusbmuxd; ideviceinstaller is its own formula.
      brew install libimobiledevice ideviceinstaller libusbmuxd >/dev/null 2>&1 \
        && ok "libimobiledevice + ideviceinstaller + iproxy" \
        || warn "some iOS tools failed: brew install libimobiledevice ideviceinstaller libusbmuxd"
      ;;
    Linux)
      local pm; pm="$(detect_pm)"
      [ -n "$pm" ] || { warn "no supported package manager; install adb + libimobiledevice manually"; return; }
      case "$pm" in
        apt-get) pm_install_each "$pm" android-tools-adb libimobiledevice6 libimobiledevice-utils ideviceinstaller usbmuxd libusbmuxd-tools ;;
        dnf)     pm_install_each "$pm" android-tools libimobiledevice libimobiledevice-utils ideviceinstaller usbmuxd ;;
        pacman)  pm_install_each "$pm" android-tools libimobiledevice usbmuxd ideviceinstaller ;;
        zypper)  pm_install_each "$pm" android-tools libimobiledevice-tools ideviceinstaller usbmuxd ;;
      esac
      ;;
  esac
}

# --- build -------------------------------------------------------------------
build_cli() {
  step "Building the CLI -> bin/mfi"
  ( cd "$ROOT" && go build -o bin/mfi ./cmd/mfi )
  ok "bin/mfi"
}

# wails_tags echoes any extra `-tags` args Wails needs on this system.
# On Linux, Wails compiles against webkit2gtk-4.0 by default, but modern distros
# (Debian bookworm / recent Ubuntu, incl. Raspberry Pi OS) ship only
# webkit2gtk-4.1; the `webkit2_41` tag links against whatever is actually present.
wails_tags() {
  [ "$OS" = "Linux" ] && have pkg-config || return 0
  if ! pkg-config --exists webkit2gtk-4.0 2>/dev/null && pkg-config --exists webkit2gtk-4.1 2>/dev/null; then
    printf -- '-tags webkit2_41'
  fi
}

build_gui() {
  step "Building the GUI (Wails)"
  add_gobin_to_path
  have wails || { warn "wails unavailable; skipping GUI build"; return 1; }

  local tags; tags="$(wails_tags)"
  [ -n "$tags" ] && ok "using webkit2gtk-4.1 (webkit2_41 build tag)"
  # shellcheck disable=SC2086 -- word-splitting of $tags into flags is intended.
  ( cd "$ROOT/cmd/mfi-gui" && wails build $tags )
  case "$OS" in
    Darwin) ok "cmd/mfi-gui/build/bin/MobFI.app" ;;
    *)      ok "cmd/mfi-gui/build/bin/" ;;
  esac
}

# --- run ---------------------------------------------------------------------
main() {
  printf "%sMobFI installer%s — %s\n\n" "$B" "$N" "$OS"
  ensure_go
  add_gobin_to_path
  [ "$BUILD_GUI" -eq 1 ] && ensure_gui_toolchain
  ensure_runtime_tools
  [ "$BUILD_CLI" -eq 1 ] && build_cli
  [ "$BUILD_GUI" -eq 1 ] && build_gui || true

  echo
  step "Done"
  [ "$BUILD_CLI" -eq 1 ] && echo "  CLI:  ${ROOT}/bin/mfi        (try: ./bin/mfi detect)"
  case "$OS" in
    Darwin) [ "$BUILD_GUI" -eq 1 ] && echo "  GUI:  open ${ROOT}/cmd/mfi-gui/build/bin/MobFI.app" ;;
    *)      [ "$BUILD_GUI" -eq 1 ] && echo "  GUI:  ${ROOT}/cmd/mfi-gui/build/bin/" ;;
  esac
  echo "  If 'go'/'wails' aren't found in new shells, add these to your profile:"
  echo "    export PATH=\"/usr/local/go/bin:\$(go env GOPATH)/bin:\$PATH\""

  case "$LAUNCH" in
    cli) step "Launching the CLI"; ( cd "$ROOT" && exec ./bin/mfi ) ;;
    gui)
      step "Launching the GUI"
      case "$OS" in
        Darwin) open "${ROOT}/cmd/mfi-gui/build/bin/MobFI.app" ;;
        # shellcheck disable=SC2086 -- word-splitting of tags into flags is intended.
        *)      ( cd "$ROOT/cmd/mfi-gui" && exec wails dev $(wails_tags) ) ;;
      esac
      ;;
  esac
}

main "$@"
