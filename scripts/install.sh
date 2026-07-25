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
    Darwin) ok "cmd/mfi-gui/build/bin/MobFI.app"; set_macos_app_path; install_macos_app ;;
    *)      ok "cmd/mfi-gui/build/bin/"; install_linux_desktop_entry ;;
  esac
}

# set_macos_app_path teaches the built .app where the runtime tools live. A GUI
# launched from Finder/Dock inherits only a minimal PATH (/usr/bin:/bin:...),
# so Homebrew-installed adb / libimobiledevice are invisible and every device
# shows as "missing". We bake an LSEnvironment PATH into the bundle's Info.plist
# -- built from where the tools ACTUALLY resolve now, plus the usual Homebrew /
# MacPorts locations -- so LaunchServices hands the GUI a PATH that finds them.
set_macos_app_path() {
  [ "$OS" = "Darwin" ] || return 0
  local app plist pb
  app="$ROOT/cmd/mfi-gui/build/bin/MobFI.app"
  plist="$app/Contents/Info.plist"
  pb="/usr/libexec/PlistBuddy"
  [ -f "$plist" ] && [ -x "$pb" ] || { warn "could not locate app Info.plist; GUI may not find tools when launched from Finder"; return 0; }

  # Collect directories: where each tool resolves now, then standard locations.
  local dirs=() seen=" " d p
  for t in adb idevice_id ideviceinfo ideviceinstaller afcclient idevicebackup2 iproxy ssh aapt plutil xcrun; do
    p="$(command -v "$t" 2>/dev/null)" || continue
    d="$(cd "$(dirname "$p")" && pwd)" || continue
    case "$seen" in *" $d "*) ;; *) dirs+=("$d"); seen="$seen$d " ;; esac
  done
  # Standard tool locations, plus the Go toolchain dirs so a GUI-initiated
  # "Update now" (which shells out to this script to rebuild) can find go/wails.
  local gopath goroot
  gopath="$(go env GOPATH 2>/dev/null)"; goroot="$(go env GOROOT 2>/dev/null)"
  for d in "$(brew --prefix 2>/dev/null)/bin" /opt/homebrew/bin /usr/local/bin /opt/local/bin \
           ${goroot:+"$goroot/bin"} /usr/local/go/bin ${gopath:+"$gopath/bin"} "$HOME/go/bin" \
           /usr/bin /bin /usr/sbin /sbin; do
    [ -n "$d" ] && [ -d "$d" ] || continue
    case "$seen" in *" $d "*) ;; *) dirs+=("$d"); seen="$seen$d " ;; esac
  done

  local path_value; path_value="$(IFS=:; echo "${dirs[*]}")"
  "$pb" -c "Delete :LSEnvironment" "$plist" >/dev/null 2>&1 || true
  "$pb" -c "Add :LSEnvironment dict" "$plist" >/dev/null 2>&1 || true
  if "$pb" -c "Add :LSEnvironment:PATH string $path_value" "$plist" >/dev/null 2>&1; then
    # Re-register so LaunchServices picks up the new environment immediately.
    local lsreg="/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
    [ -x "$lsreg" ] && "$lsreg" -f "$app" >/dev/null 2>&1 || true
    ok "app PATH set for Finder launch (adb / libimobiledevice discoverable)"
  else
    warn "could not set app PATH; if the GUI shows tools as missing, launch it from a terminal"
  fi
}

# install_linux_desktop_entry adds a per-user .desktop launcher + icon so the
# GUI shows up (with its icon) in the application menu, and so the desktop
# environment can pair the running window's icon via StartupWMClass. Wails
# already supplies the in-window icon at runtime; this covers the menu/taskbar.
install_linux_desktop_entry() {
  [ "$OS" = "Linux" ] || return 0
  local bin icon_src apps icons desktop icon_dst
  bin="$(find "$ROOT/cmd/mfi-gui/build/bin" -maxdepth 1 -type f -perm -u+x 2>/dev/null | head -1)"
  [ -n "$bin" ] || { warn "GUI binary not found; skipping desktop entry"; return 0; }
  icon_src="$ROOT/cmd/mfi-gui/build/appicon.png"

  apps="${XDG_DATA_HOME:-$HOME/.local/share}/applications"
  icons="${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor/256x256/apps"
  mkdir -p "$apps" "$icons" 2>/dev/null || { warn "could not create desktop dirs; skipping"; return 0; }

  icon_dst="$icons/mobfi.png"
  [ -f "$icon_src" ] && cp -f "$icon_src" "$icon_dst" 2>/dev/null || icon_dst="$icon_src"

  # Bake a PATH into the launcher so a menu-launched GUI (which otherwise gets
  # only the session PATH) can find adb / libimobiledevice AND the toolchain
  # (go, wails, git) it shells out to for an in-app "Update now" rebuild.
  local pathdirs=() seen=" " d p gopath goroot launch_path
  for p in adb idevice_id ideviceinfo ideviceinstaller afcclient idevicebackup2 iproxy ssh git go wails aapt; do
    d="$(command -v "$p" 2>/dev/null)" || continue
    d="$(cd "$(dirname "$d")" && pwd)" || continue
    case "$seen" in *" $d "*) ;; *) pathdirs+=("$d"); seen="$seen$d " ;; esac
  done
  gopath="$(go env GOPATH 2>/dev/null)"; goroot="$(go env GOROOT 2>/dev/null)"
  for d in /usr/local/go/bin ${goroot:+"$goroot/bin"} ${gopath:+"$gopath/bin"} "$HOME/go/bin" \
           /usr/local/bin /usr/bin /bin /usr/sbin /sbin; do
    [ -d "$d" ] || continue
    case "$seen" in *" $d "*) ;; *) pathdirs+=("$d"); seen="$seen$d " ;; esac
  done
  launch_path="$(IFS=:; echo "${pathdirs[*]}")"

  desktop="$apps/mobfi.desktop"
  cat > "$desktop" <<EOF
[Desktop Entry]
Type=Application
Name=MobFI
GenericName=Mobile Filesystem Inspector
Comment=Inspect Android and iOS app file structures
Exec=/usr/bin/env PATH=$launch_path "$bin"
Icon=$icon_dst
Terminal=false
Categories=Development;Utility;
StartupWMClass=MobFI
EOF
  chmod +x "$desktop" 2>/dev/null || true
  have update-desktop-database && update-desktop-database "$apps" >/dev/null 2>&1 || true
  ok "desktop entry: $desktop"
}

# install_macos_app copies the built bundle to /Applications (the conventional
# location) and re-registers it. The plist PATH patch from set_macos_app_path
# travels with the copy. MACOS_APP_DST records where the app ended up.
MACOS_APP_DST=""
install_macos_app() {
  [ "$OS" = "Darwin" ] || return 0
  local src dst lsreg
  src="$ROOT/cmd/mfi-gui/build/bin/MobFI.app"
  [ -d "$src" ] || return 0
  dst="/Applications/MobFI.app"
  if rm -rf "$dst" 2>/dev/null && cp -R "$src" "$dst" 2>/dev/null; then
    lsreg="/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
    [ -x "$lsreg" ] && "$lsreg" -f "$dst" >/dev/null 2>&1 || true
    MACOS_APP_DST="$dst"
    ok "installed to /Applications/MobFI.app"
  else
    MACOS_APP_DST="$src"
    warn "could not copy to /Applications (permissions?); using $src"
  fi
}

# record_source_repo saves this checkout's path so an in-app "Update now" can
# git-pull + rebuild even when the GUI runs from /Applications (detached from
# the source tree). Written under the OS config dir to match os.UserConfigDir().
record_source_repo() {
  local cfg
  case "$OS" in
    Darwin) cfg="$HOME/Library/Application Support/MobFI" ;;
    *)      cfg="${XDG_CONFIG_HOME:-$HOME/.config}/MobFI" ;;
  esac
  mkdir -p "$cfg" 2>/dev/null || return 0
  printf '%s\n' "$ROOT" > "$cfg/source-repo.txt" 2>/dev/null || true
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
  record_source_repo

  echo
  step "Done"
  [ "$BUILD_CLI" -eq 1 ] && echo "  CLI:  ${ROOT}/bin/mfi        (try: ./bin/mfi detect)"
  case "$OS" in
    Darwin) [ "$BUILD_GUI" -eq 1 ] && echo "  GUI:  open ${MACOS_APP_DST:-/Applications/MobFI.app}" ;;
    *)      [ "$BUILD_GUI" -eq 1 ] && echo "  GUI:  ${ROOT}/cmd/mfi-gui/build/bin/" ;;
  esac
  echo "  If 'go'/'wails' aren't found in new shells, add these to your profile:"
  echo "    export PATH=\"/usr/local/go/bin:\$(go env GOPATH)/bin:\$PATH\""

  case "$LAUNCH" in
    cli) step "Launching the CLI"; ( cd "$ROOT" && exec ./bin/mfi ) ;;
    gui)
      step "Launching the GUI"
      case "$OS" in
        Darwin) open "${MACOS_APP_DST:-/Applications/MobFI.app}" ;;
        # shellcheck disable=SC2086 -- word-splitting of tags into flags is intended.
        *)      ( cd "$ROOT/cmd/mfi-gui" && exec wails dev $(wails_tags) ) ;;
      esac
      ;;
  esac
}

main "$@"
