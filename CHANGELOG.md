# Changelog

All notable changes to MobFI are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Add new changes under `## [Unreleased]` as you go; on release, rename that
heading to the version and date and start a fresh `Unreleased` section.

## [Unreleased]

## [1.0.0] - 2026-07-25

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
  Secrets are redacted by default; an opt-in (`mfi report -show-secrets` / the
  GUI's **Unredacted** checkbox, behind a confirm prompt) includes raw values
  for authorized local analysis, with a plaintext warning in the output.

### Frontends
- Guided wizard by default with an advanced path, in both the GUI and the CLI.
- Wails desktop GUI (vanilla HTML/JS/CSS frontend over the Go core) with
  persisted window geometry and toggles, sortable tables, side-by-side file
  diff, and a global Dependencies panel. Device detection is resilient -- one
  failing detector never blanks the list.
- `mfi doctor` reports the presence of the runtime device tools.
- Launch update check with in-place update: detects a newer GitHub release
  (works for prebuilt binaries) and, in a git checkout, how many commits behind
  upstream. The GUI shows a dismissable banner and the CLI prints a notice /
  `mfi update`. Updating in place is a click (**Update now**) or
  `mfi update -apply`: a source checkout runs `git pull` + rebuild, a prebuilt
  binary downloads the release asset, verifies its SHA-256, and swaps itself.
  The GUI update is done out-of-process -- MobFI closes, a detached worker
  updates and rebuilds, then relaunches the app automatically and toasts the
  result. The installer records the source-checkout path so this works even
  when the app runs from `/Applications` or a shortcut.
  `MFI_NO_UPDATE_CHECK=1` silences the launch check.

### Packaging & tooling
- Cross-platform install scripts: `scripts/install.sh` (macOS/Linux) and
  `scripts/install.ps1` (Windows) that resolve Go, the Wails toolchain, adb,
  and libimobiledevice, then build both binaries.
- Windows-on-ARM support: emulated amd64 GUI build, scoop bootstrap, prebuilt
  libimobiledevice, and Apple Mobile Device Support for iOS-over-USB.
- macOS: the installer copies the app to `/Applications` and bakes an
  `LSEnvironment` PATH into the bundle so a Finder/Dock-launched GUI finds
  Homebrew-installed adb / libimobiledevice and the toolchain (otherwise the
  bundle's minimal PATH hides them and every device shows as missing).
- Linux: GUI builds against webkit2gtk-4.1 (the `webkit2_41` tag) when 4.0 is
  absent (Debian bookworm / Raspberry Pi OS); the window/taskbar icon is set at
  runtime and the installer registers a `.desktop` launcher + icon with a baked
  PATH (so a menu-launched GUI finds the device tools and the update toolchain).
- Prebuilt, version-stamped `mfi` CLI binaries for macOS, Linux and Windows are
  attached to the release; version reporting is shared by both binaries via
  `internal/version`.

[Unreleased]: https://github.com/integrisec/MobFI/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/integrisec/MobFI/releases/tag/v1.0.0
