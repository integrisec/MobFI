<p align="center">
  <img src="mobfi-logo-full.png" alt="MobFI — Mobile Filesystem Inspector" width="420" />
</p>

# MobFI — Mobile Filesystem Inspector

**MobFI** is a cross-platform **desktop app and CLI** for inspecting and
exploring the file structures of installed **Android and iOS** applications.
It is a mobile-forensics / app-data-inspection tool: discover devices, pull an
app's on-device data, hunt for secrets, diff two captures, browse databases,
and render native file formats — then summarise it all into a report.

The GUI and the CLI are thin frontends over one shared Go core, so every
capability is available from both.

---

## Features

- **Device discovery** — Android via `adb` (USB, emulator, adb-over-TCP) and
  iOS via `libimobiledevice` (USB + network); reports each device's pairing
  state (`device` / `unauthorized` / `unpaired`).
- **App enumeration** — list the installed apps on a device with their bundle
  ids and on-device paths, to pick an extraction target.
- **App extraction** — mirror an app's on-device file tree to a local folder.
  Android uses `/data/data/<pkg>` over adb (with `run-as`, then `su` on a
  rooted device); iOS uses AFC house arrest (`afcclient`) with a selectable
  scope (`container` / `documents`), or a `backup` scope (`idevicebackup2`)
  that reconstructs a production app's data from a full device backup.
  Unreadable files are recorded, not silently dropped, and a path-escape guard
  prevents hostile device paths from writing outside the destination.
- **Secrets scanning** — a broad set of Trufflehog-style built-in detectors
  (~45 rules): cloud/CI keys (AWS, GCP, DigitalOcean, Databricks, Terraform),
  version-control & package tokens (GitHub, GitLab, npm, PyPI), AI providers
  (OpenAI, Anthropic, Hugging Face), payments (Stripe, Square, Braintree),
  communication/email (Slack, Discord, Telegram, Twilio, SendGrid, Mailgun,
  Mailchimp), SaaS APIs (Shopify, Notion, Linear, Airtable, New Relic, Grafana),
  JWTs, PEM private keys, and connection strings / URLs with embedded
  credentials (MongoDB, SQL, Redis, HTTP basic-auth) — plus a user-supplied list
  of known secrets. Findings carry only a **redacted fingerprint**, never the
  raw secret. Optional **live verification** (opt-in) calls each service's API
  to confirm whether a found key is still active.
- **Native diffing** — compare two extracted captures (e.g. logged-in vs
  logged-out). Beyond add/remove/modify at the file level, structural differs
  report **SQLite** row-level changes, and **JSON** and **property list** (binary
  *and* XML) field-level changes — even across formats.
- **Database viewing** — open SQLite files **read-only and immutable** (evidence
  is never mutated); list tables and dump rows.
- **File rendering** — pretty-print JSON, reindent XML, decode **binary property
  lists**, summarise SQLite, show text, or fall back to a hex dump.
- **Reporting** — aggregate scan and diff results into a text summary and an
  exportable JSON report.

---

## How it works

A single shared **core library in Go** (`internal/`) holds all logic — device
I/O, extraction, scanning, diffing, database access, rendering and reporting.
Two thin frontends drive it:

- `cmd/mfi` — the CLI
- `cmd/mfi-gui` — the [Wails](https://wails.io) desktop app (Go backend + a
  dependency-free HTML/JS/CSS UI)

---

## Prerequisites

**To build:**

- **Go 1.23+**

**To inspect real devices (runtime tools, install what you need):**

- **Android** — the Android platform tools (`adb`):
  `brew install --cask android-platform-tools`
- **iOS** — `libimobiledevice` (`idevice_id`, `ideviceinfo`, `afcclient`):
  `brew install libimobiledevice`
  (pair and *trust* the device first; the AFC `container` scope needs a
  dev-signed app or a jailbroken device, `documents` scope works more broadly,
  and the `backup` scope — via `idevicebackup2` — reaches production apps)

**To build the desktop GUI (in addition to the above):**

- The **Wails** CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- **Node.js** and a C toolchain / platform webview
  (macOS: Xcode Command Line Tools). Run `wails doctor` to check.

MobFI does not need the runtime tools to build or run — features degrade
gracefully when a tool is absent (e.g. no `adb` simply means no Android
devices are found).

---

## Installation

### Download a prebuilt CLI (fastest)

Grab a ready-to-run `mfi` CLI from the
[latest release](https://github.com/integrisec/MobFI/releases/latest) — no Go
toolchain needed. Binaries are cgo-free and version-stamped.

| OS | Chip | Asset |
|----|------|-------|
| macOS | Apple Silicon | [`mfi_v1.0.0_darwin_arm64`](https://github.com/integrisec/MobFI/releases/download/v1.0.0/mfi_v1.0.0_darwin_arm64) |
| macOS | Intel | [`mfi_v1.0.0_darwin_amd64`](https://github.com/integrisec/MobFI/releases/download/v1.0.0/mfi_v1.0.0_darwin_amd64) |
| Linux | arm64 (incl. Raspberry Pi) | [`mfi_v1.0.0_linux_arm64`](https://github.com/integrisec/MobFI/releases/download/v1.0.0/mfi_v1.0.0_linux_arm64) |
| Linux | x86-64 | [`mfi_v1.0.0_linux_amd64`](https://github.com/integrisec/MobFI/releases/download/v1.0.0/mfi_v1.0.0_linux_amd64) |
| Windows | x86-64 (runs on ARM via emulation) | [`mfi_v1.0.0_windows_amd64.exe`](https://github.com/integrisec/MobFI/releases/download/v1.0.0/mfi_v1.0.0_windows_amd64.exe) |

```sh
# macOS / Linux: make it runnable, verify, and check the version
chmod +x mfi_v1.0.0_*                              # then rename to `mfi` if you like
shasum -a 256 -c SHA256SUMS.txt --ignore-missing   # SHA256SUMS.txt is on the release
./mfi_v1.0.0_darwin_arm64 version                  # -> mfi v1.0.0 (…)
```

On macOS, clear the download quarantine if Gatekeeper blocks it:
`xattr -d com.apple.quarantine mfi_v1.0.0_darwin_arm64`.

The **desktop GUI** is not shipped prebuilt (it needs per-OS cgo/WebKit
toolchains) — build it with the install scripts below.

### Quick install (recommended if you don't build Go projects)

One command resolves **everything** — the Go toolchain, the Wails CLI and its
native GUI toolchain, and the device tools (`adb`, `libimobiledevice`) — then
builds the CLI and GUI. Safe to re-run; anything already present is skipped.

```sh
git clone https://github.com/integrisec/MobFI.git
cd MobFI

# macOS / Linux
./scripts/install.sh            # or: make setup

# Windows (PowerShell)
powershell -ExecutionPolicy Bypass -File scripts\install.ps1
```

Useful flags (both scripts): `--cli-only` / `-CliOnly`, `--gui-only` /
`-GuiOnly`, `--no-runtime-tools` / `-NoRuntimeTools`, and
`--launch cli|gui` / `-Launch cli|gui` to run the app when it's built.
Windows-only: `-NoShortcuts` skips creating the Start Menu / Desktop shortcuts.

- **macOS** uses Homebrew (installed if missing) and copies the built
  **MobFI.app to `/Applications`**.
- **Linux** auto-detects `apt`/`dnf`/`pacman`/`zypper` and installs the GTK3 +
  WebKit2GTK build packages; Go is fetched from go.dev if the system's is older
  than 1.23.
- **Windows** uses `winget` (Go, platform-tools/`adb`, WebView2) and bootstraps
  `scoop` for **gcc** (Wails links WebView2 via cgo, so the GUI build needs a C
  compiler — the CLI is cgo-free and does not). For iOS it downloads prebuilt
  **libimobiledevice** binaries (there is no winget/scoop package) and adds them
  to PATH, and (for iOS over USB) installs **Apple Mobile Device Support** via
  winget — the `usbmuxd` service libimobiledevice needs — so you don't have to
  install full iTunes. It then builds both binaries and adds Start Menu /
  Desktop shortcuts to the GUI. After it finishes, plug in the device and
  *trust* the computer; Android works without any of the iOS pieces. `scoop`
  bootstrapping requires a non-elevated PowerShell.
  - **iOS tools, manual fallback:** the installer pulls the core tools
    (`idevice_id`, `ideviceinfo`, `afcclient`) from the
    [jrjr/libimobiledevice-windows](https://github.com/jrjr/libimobiledevice-windows/releases)
    suite and `ideviceinstaller` from the
    [imobiledevice-net](https://github.com/libimobiledevice-win32/imobiledevice-net/releases)
    `win-x64` bundle. If a download fails, grab that zip manually, extract it,
    and add the folder to your PATH. MobFI needs `idevice_id`, `ideviceinfo`,
    `afcclient` (extraction) and `ideviceinstaller` (app listing).
  - **Windows on ARM64:** scoop's `gcc` is x86-64, so the installer builds the
    GUI as `windows/amd64` and it runs under the OS's x64 emulation. (A native
    arm64 GUI would need an aarch64 mingw toolchain, e.g. llvm-mingw.) The
    cgo-free CLI builds natively as arm64. **iOS over USB is the exception:**
    Apple's Mobile Device USB driver is x64 *kernel-mode*, and Windows on ARM
    cannot load x64 kernel drivers — so iPhones may not enumerate even with the
    service installed. Use a Mac or x64 host for iOS; Android works fine on ARM.

### Manual build

```sh
make build            # compiles the CLI to ./bin/mfi
```

Optionally put it on your PATH:

```sh
cp bin/mfi /usr/local/bin/     # or add ./bin to $PATH
```

---

## Usage — CLI

Run with no arguments for the guided wizard, or use a subcommand directly:

```sh
mfi                 # guided wizard (advanced users can run subcommands)
mfi help            # list all commands
```

### Check device tools

See which external tools (`adb`, `libimobiledevice`, …) are installed, with
install hints for any that are missing:

```sh
mfi doctor          # human-readable report
mfi doctor -json    # machine-readable, for scripts
```

### Check the version

```sh
mfi version         # or: mfi --version / mfi -v  ->  mfi v1.0.0
```

### Detect devices

```sh
mfi detect
```

### List installed apps

Show the installed apps on a device with their bundle ids, version and
on-device paths (handy for picking an extraction target). Add `-all` to
include system apps:

```sh
mfi apps -device <serial-or-udid>
mfi apps -device <serial-or-udid> -all      # also list system apps
```

In the GUI, the Apps view has a search box (filter by bundle id or name), an
"Include system apps" checkbox, sortable/resizable columns, and per-row Copy.
Real app icons, names and versions are resolved lazily from each APK using
`aapt` (from the Android SDK build-tools); without it, a monogram avatar and a
name derived from the bundle id are shown instead. Click a row to show a
details panel: on Android from `dumpsys package` (version, SDK, ABI,
install/update dates, sizes, flags, signing, paths, permissions); on iOS from
`ideviceinstaller` (version, type, min iOS, signer, paths, entitlements).

### Extract an app's data

```sh
# Android
mfi extract -device <serial> -app com.example.app -out ./capture

# iOS: choose the scope
#   container - full app sandbox   (needs a dev-signed/debug app or a jailbreak)
#   documents - the app's Documents (needs UIFileSharingEnabled)
#   backup    - pull the app's data out of a full device backup
#               (works for production App Store apps on a stock device)
mfi extract -device <udid> -app com.example.app -out ./capture -scope backup
```

On a non-jailbroken device, iOS only vends a **production** app's private data
via a device **backup**. `-scope backup` runs `idevicebackup2` (a full,
unencrypted device backup — disable *Encrypt Local Backup* first), then
reconstructs just the target app's files from the backup's `Manifest.db` into a
readable tree. iOS backs up the **whole device** even to extract one app, so
the backup is staged under `-out` and needs **free disk space for the entire
device** (tens of GB) — point `-out` at a drive with room. `container` /
`documents` still cover dev-signed and file-sharing apps without that overhead.

On Android, an app's private `/data/data/<pkg>` is read via `run-as <pkg>`,
which works for **debuggable** apps on a non-rooted device; on a rooted device
it falls back to a direct shell session. Non-debuggable apps on a non-rooted
device can't be read (that's an OS restriction, not a MobFI limitation).

### Scan for secrets

```sh
mfi scan -root ./capture
mfi scan -root ./capture -known ./known-secrets.txt   # also match literal secrets
mfi scan -root ./capture -verify                      # live-verify findings (see below)
```

**Live verification** (`-verify`, also `mfi report -verify` and the GUI's
**Verify live** checkbox) confirms whether a matched key is still active by
calling the service's read-only "whoami" endpoint (GitHub, GitLab, npm, OpenAI,
Anthropic, Hugging Face, SendGrid, DigitalOcean, Stripe, Slack, Postman, Notion,
Airtable), reporting each as **active** / **inactive** / **unknown**. It is
**off by default**: it makes authenticated network calls that send the matched
secret to its service, so use it only for authorized testing.

### Diff two captures

```sh
mfi diff -a ./logged-out -b ./logged-in
# e.g. modified app.db (sqlite: creds: +1 -1 rows)
#      modified state.plist (plist: 1 changed, 0 added, 1 removed field(s))
```

### Inspect a SQLite database (read-only)

```sh
mfi db -file ./capture/databases/app.db                 # list tables
mfi db -file ./capture/databases/app.db -table users    # dump rows
mfi db -file ./capture/databases/app.db -table users -limit 500
```

### Render a file

```sh
mfi render -file ./capture/Library/Preferences/com.example.app.plist
```

### Build a report

Scan and/or diff, print a text summary, and optionally export a file. The
export format follows the `-out` extension: `.html` → styled HTML, `.txt` →
text, anything else → JSON. Discovered secrets are redacted in every format.

```sh
mfi report -root ./capture -a ./logged-out -b ./logged-in -out report.html
mfi report -root ./capture -out report.mfi-report.json
```

Secrets are **redacted by default** (leading characters + length). To include
the raw, unredacted secret values — for authorized local analysis only — add
`-show-secrets`; the resulting report contains plaintext secrets, so do not
share it:

```sh
mfi report -root ./capture -show-secrets -out report.html
```

In the GUI, the **Scan** and **Diff** tabs each have an **Export** control —
choose the scope (that tab's results, or a **Combined** scan + diff report)
and the format (HTML / JSON / Text) and save. Tick **Unredacted** next to
Export (and confirm the prompt) to include raw secrets in that export.

---

## Usage — Desktop GUI

The GUI wraps the same core. From the `cmd/mfi-gui` directory:

```sh
cd cmd/mfi-gui
wails dev      # live-reload development window
wails build    # package a native app (e.g. build/bin/MobFI.app on macOS)
```

The window opens on the **Devices** step, which auto-refreshes (a manual
Detect button forces a re-check) and shows each device's rooted/jailbroken
status, with tabs for Apps, Extract, Scan, Diff, Database, Render and Console
(an interactive `adb shell` for Android, or SSH to a jailbroken iOS device,
in a real xterm.js terminal). In the
**Diff** view each changed file can be sent to Render, and modified files open
a side-by-side line diff (highlighting, synced scroll, jump between changes).

The **Render** tab opens a single file or a whole folder (an Explorer-style
tree on the left, render pane on the right, with a resizable divider). It
detects the type and renders natively — images and PDFs inline, code
(XML/JSON/JS/…) with syntax highlighting, property lists decoded — with
**Wrap**, **Prettify** (indent JSON/XML regardless of extension) and **Hex**
toggles. It also shows the file size, an **Open externally** button (OS
default app), and — when the file is a SQLite database — an **Open in
Database** button that jumps to the Database tab. GUI-only extras (real app
icons/details, syntax highlighting) shell out to `aapt` (Android SDK
build-tools) and use Chroma; they degrade gracefully when unavailable.

---

## Updating

MobFI checks for a newer version at launch (the GUI shows a banner; the wizard
prints a one-line notice) and `mfi update` reports it on demand:

```sh
mfi update             # is a newer release out? is this checkout behind?
mfi update -json       # machine-readable
mfi update -apply      # perform the update now
```

It compares the built-in version against the latest **GitHub release** (works
for prebuilt binaries too) and, when run from a git checkout, how many commits
you are **behind upstream**. Set `MFI_NO_UPDATE_CHECK=1` to silence the launch
check.

**Updating in place** — `mfi update -apply` (CLI) or the **Update now** button
on the GUI banner performs the update for you:

- **Source checkout**: runs `git pull --ff-only` and rebuilds via the install
  script. The **GUI does this out-of-process** — it closes, a detached worker
  updates and rebuilds, then the app relaunches automatically and toasts the
  result. (The installer records the checkout path so this works even when the
  app runs from `/Applications`.)
- **Prebuilt binary**: downloads the matching release asset, verifies its
  SHA-256, and atomically replaces the running executable.

To update manually instead:

```sh
git pull
go mod download        # if dependencies changed
make build             # rebuild the CLI

# rebuild the desktop app
cd cmd/mfi-gui && wails build
```

To update the Wails CLI itself:

```sh
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

---

## Development

```sh
make test                                        # go test ./...
make vet                                         # go vet ./...
make fmt                                         # gofmt -w .
make check-ascii                                 # .ps1 files must be pure ASCII
go test ./internal/secrets/ -run TestScanTree    # a single package / test
```

CI (`.github/workflows/ci.yml`) runs `go vet`/`go test` on the cgo-free core +
CLI and enforces `make check-ascii` — PowerShell scripts must stay pure ASCII,
since Windows PowerShell 5.1 reads BOM-less files as Windows-1252 and
mis-parses stray non-ASCII (e.g. an em-dash pasted from a doc).

Repository layout:

- `cmd/mfi/` — CLI frontend
- `cmd/mfi-gui/` — Wails desktop app (`gui.go` bindings, `frontend/dist/` UI)
- `internal/app/` — the core orchestrator both frontends call
- `internal/device/`, `internal/transport/` — device discovery and connection
- `internal/extract/`, `internal/secrets/`, `internal/diff/`, `internal/dbview/`,
  `internal/render/`, `internal/report/`, `internal/plist/` — the capabilities

New device detectors, transports, secret rules, structural differs and file
renderers are all pluggable — see the registries in the relevant packages.

### Versioning

MobFI follows [semantic versioning](https://semver.org). The canonical version
lives in one place — `internal/version` (`Version`) — and is reported by both
the CLI (`mfi version`) and the GUI (top-bar badge). Bump it there on a release
and mirror it in `cmd/mfi-gui/wails.json` (`info.productVersion`) for the OS
bundle metadata, then tag the commit (`git tag v<x.y.z>`). `make build` stamps
the short git commit and build date on top of the version; a plain `go build`
reports just the version.

---

## Safety & handling of sensitive data

MobFI works with real application data and secrets, so it is built to be
careful:

- Databases are opened **read-only and immutable** — source files are never
  modified.
- Discovered secrets are **redacted** in findings and reports (leading
  characters + length only).
- Extraction **guards against path traversal** from untrusted device paths.
- Binary parsers (property lists) are bounds-checked and never panic on
  malformed input.
- `.gitignore` excludes extracted data, reports and known-secret files so they
  are not committed by accident.

Only use MobFI on devices and applications you are **authorized** to inspect.

---

## License

Released under the [MIT License](./LICENSE) — Copyright (c) 2026 integrisec.
