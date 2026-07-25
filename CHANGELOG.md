# Changelog

All notable changes to MobFI are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Add new changes under `## [Unreleased]` as you go; on release, rename that
heading to the version and date and start a fresh `Unreleased` section.

## [Unreleased]

### Added
- Update check at launch: MobFI now detects when a newer **GitHub release** is
  available (works for prebuilt binaries) and, in a git checkout, how many
  commits you are **behind upstream**. The GUI shows a dismissable banner with
  a "View release" button; the CLI wizard prints a one-line notice and
  `mfi update` reports on demand (`-json` supported). Advisory only -- it never
  modifies anything; set `MFI_NO_UPDATE_CHECK=1` to silence the launch check.
- Option to include raw, **unredacted** secrets in a report, for authorized
  local analysis: `mfi report -show-secrets` on the CLI, and an **Unredacted**
  checkbox (with a confirm prompt) next to the GUI's Export controls. Reports
  stay redacted by default; unredacted output carries a plaintext warning
  banner and is not meant to be shared.

### Fixed
- Linux: the GUI window/taskbar showed a blank icon. The app icon is now
  supplied to Wails at runtime (`linux.Options.Icon`) and the installer
  registers a per-user `.desktop` launcher + icon (with `StartupWMClass`) so
  the app menu and panel pair the correct icon.

## [1.0.0] - 2026-07-24

First official release. MobFI is a cross-platform (macOS, Windows, Linux)
mobile-forensics and secrets-inspection tool for exploring the file structures
of installed Android and iOS apps, available as both a desktop GUI and a CLI
built on a single shared Go core.

### Device discovery & access
- Detect active Android devices (adb) and iOS devices (libimobiledevice) across
  USB and TCP, plus booted iOS Simulators (simctl).
- Manual TCP connect and Android 11+ wireless `adb pair` from the GUI.
- Console tab: interactive `adb shell` (Android) and SSH-over-USB (iOS, auto
  `iproxy` forwarding) in a real PTY, with history, copy/paste, logging, and
  font-size controls.

### App targeting & extraction
- Enumerate installed apps (`adb pm list packages` / `ideviceinstaller`), with
  real app icon/name/version in the GUI.
- Android extraction reads `/data/data/<pkg>`: `run-as` for debuggable apps,
  falling back to `su` on rooted devices, then a plain shell; prefers a single
  tar stream and falls back to per-file walking.
- iOS extraction via AFC house arrest with a selectable scope: `container`,
  `documents`, or `backup` (full-device backup + `Manifest.db` reconstruction
  to reach a production App Store app's private data on a non-jailbroken
  device).
- Live progress with cancellation; destination path-traversal guards and
  Windows-safe filename handling.

### Inspection & analysis
- Secrets scanning: Trufflehog-style built-in rules plus a user-supplied
  known-secret list, with redacted findings.
- Native semantic diffing of two or more captures (e.g. logged-in vs.
  logged-out): tree + content diff with structural differs for SQLite
  (row-level), JSON and plist (field-level, across binary/XML plist forms).
- Read-only, immutable SQLite database viewing.
- Native rendering per file type: JSON, XML, binary + XML plists, SQLite
  summaries, text, hex, images, and PDFs, with syntax highlighting.
- Reporting: aggregate scan + diff results into text, JSON, and HTML exports.

### Frontends
- Guided wizard by default with an advanced path, in both the GUI and the CLI.
- Wails desktop GUI (vanilla HTML/JS/CSS frontend over the Go core) with
  persisted window geometry and toggles, sortable tables, side-by-side file
  diff, and a global Dependencies panel.
- `mfi doctor` reports the presence of the runtime device tools.

### Packaging & tooling
- Cross-platform install scripts: `scripts/install.sh` (macOS/Linux) and
  `scripts/install.ps1` (Windows) that resolve Go, the Wails toolchain, adb,
  and libimobiledevice, then build both binaries.
- Windows-on-ARM support: emulated amd64 GUI build, scoop bootstrap, prebuilt
  libimobiledevice, and Apple Mobile Device Support for iOS-over-USB.
- Linux GUI builds against webkit2gtk-4.1 (the `webkit2_41` tag) when 4.0 is
  absent, e.g. on Debian bookworm and Raspberry Pi OS.
- Version reporting shared by both binaries via `internal/version`.

[Unreleased]: https://github.com/integrisec/MobFI/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/integrisec/MobFI/releases/tag/v1.0.0
