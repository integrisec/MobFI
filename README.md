# MobFI

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
  Android uses `/data/data/<pkg>` over adb; iOS uses AFC house arrest
  (`afcclient`) with a selectable scope (`container` or `documents`).
  Unreadable files are recorded, not silently dropped, and a path-escape guard
  prevents hostile device paths from writing outside the destination.
- **Secrets scanning** — Trufflehog-style built-in detectors (AWS keys, GitHub
  tokens, Google API keys, Slack tokens, Stripe keys, private keys, JWTs, and a
  generic key/secret assignment) plus a user-supplied list of known secrets.
  Findings carry only a **redacted fingerprint**, never the raw secret.
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

See [`CLAUDE.md`](./CLAUDE.md) for the architecture rationale and
[`intial_claude_prompt.md`](./intial_claude_prompt.md) for the original spec.

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
  dev-signed app or a jailbroken device — `documents` scope works more broadly)

**To build the desktop GUI (in addition to the above):**

- The **Wails** CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- **Node.js** and a C toolchain / platform webview
  (macOS: Xcode Command Line Tools). Run `wails doctor` to check.

MobFI does not need the runtime tools to build or run — features degrade
gracefully when a tool is absent (e.g. no `adb` simply means no Android
devices are found).

---

## Installation

```sh
git clone https://github.com/integrisec/MobFI.git
cd MobFI
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

# iOS (choose the AFC scope)
mfi extract -device <udid> -app com.example.app -out ./capture -scope documents
```

On Android, an app's private `/data/data/<pkg>` is read via `run-as <pkg>`,
which works for **debuggable** apps on a non-rooted device; on a rooted device
it falls back to a direct shell session. Non-debuggable apps on a non-rooted
device can't be read (that's an OS restriction, not a MobFI limitation).

### Scan for secrets

```sh
mfi scan -root ./capture
mfi scan -root ./capture -known ./known-secrets.txt   # also match literal secrets
```

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

```sh
# scan and/or diff, print a summary, and optionally export JSON
mfi report -root ./capture -a ./logged-out -b ./logged-in -out report.mfi-report.json
```

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
status, with tabs for Apps, Extract, Scan, Diff, Database and Render.

The **Render** tab opens a single file or a whole folder (an Explorer-style
tree on the left, render pane on the right). It detects the type and renders
natively — images and PDFs inline, code (XML/JSON/JS/…) with syntax
highlighting, property lists decoded — with a Hex-view toggle.

---

## Updating

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
go test ./internal/secrets/ -run TestScanTree    # a single package / test
```

Repository layout:

- `cmd/mfi/` — CLI frontend
- `cmd/mfi-gui/` — Wails desktop app (`gui.go` bindings, `frontend/dist/` UI)
- `internal/app/` — the core orchestrator both frontends call
- `internal/device/`, `internal/transport/` — device discovery and connection
- `internal/extract/`, `internal/secrets/`, `internal/diff/`, `internal/dbview/`,
  `internal/render/`, `internal/report/`, `internal/plist/` — the capabilities

New device detectors, transports, secret rules, structural differs and file
renderers are all pluggable — see the registries in the relevant packages.

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
