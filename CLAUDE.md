# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project status

Early build. The package boundaries and interfaces are in place. Implemented so far: device detection (adb + libimobiledevice), the adb transport connector, `extract.Run` (mirrors an app's on-device tree to disk), and the `secrets` scanner (Trufflehog-style built-in rules + user-supplied known-secret lists, with redacted findings), `diff.Trees` (tree + content diff, with pluggable structural differs — SQLite row-level, JSON and plist field-level; plist diffs across binary/XML forms), and `report` (aggregates scan + diff into a text summary and a JSON export), `dbview` (read-only, immutable SQLite inspection), and `render` (JSON, XML, binary + XML plist, SQLite summary, text, hex — pluggable per file type) — plus app enumeration (`adb pm list packages` / `ideviceinstaller list`, via `device.AppLister`) — all wired end-to-end through the CLI (`mfi detect` / `apps` / `extract` / `scan` / `diff` / `report` / `db` / `render`) **and** the Wails desktop GUI, which binds the same core. Extraction works for both platforms: adb for Android (reads `/data/data/<pkg>` via `run-as <pkg>` for debuggable apps, falling back to a direct shell session on rooted devices — see `App.androidConn`) and AFC house arrest (`afcclient`) for iOS, with a selectable scope. Android prefers a single-process **tar stream** (`transport.TarStreamer` → `extract.RunTar`) and falls back to per-file `Walk`/`Open` if the device has no `tar`. Extraction reports progress via an `extract.Progress` callback (the GUI relays it as `extract:progress` events). — `container` or `documents` — via `mfi extract -scope` or the GUI's iOS-scope dropdown. Every core capability from the spec now has a working first implementation; grep for `TODO` for the known follow-ups (more file-type renderers/differs). Property lists (binary and XML) are decoded by `internal/plist` (`DecodeAny`), used by both `render` and the plist `FileDiffer`. New structural differs plug into `defaultFileDiffers` in `internal/diff` (implement `FileDiffer`). Read `intial_claude_prompt.md` (the founding spec) for the full requirement set.

Go module path: `github.com/integrisec/MobFI` (remote: `https://github.com/integrisec/MobFI.git`). Requires Go 1.23+ (dev machine has 1.26.5). Both binaries build clean and `go vet ./...` passes; there are no tests yet.

## Commands

```sh
make build        # -> bin/mfi (compiles ./cmd/mfi)
make run          # runs the CLI; with no args it launches the wizard
make test         # go test ./...
make vet          # go vet ./...
make fmt          # gofmt -w .
go test ./internal/secrets/ -run TestScanTree   # a single package / test
go run ./cmd/mfi detect                          # exercise a subcommand
```

GUI (from `cmd/mfi-gui/`, needs the `wails` CLI — `go install github.com/wailsapp/wails/v2/cmd/wails@latest`):

```sh
cd cmd/mfi-gui
wails dev      # live-reload dev window
wails build    # package build/bin/MobFI.app (macOS); cgo/WebKit toolchain required
```

The GUI frontend under `cmd/mfi-gui/frontend/dist/` is **vanilla HTML/JS/CSS** — no npm/build step. It calls the Go bindings at `window.go.main.GUI.*`, which the Wails runtime injects from the exported methods on the `GUI` type (`cmd/mfi-gui/gui.go`). Generated `wailsjs/` bindings are gitignored; don't rely on them.

The one external dependency so far is `modernc.org/sqlite` (used by `internal/dbview`) — chosen because it is **cgo-free**, so cross-compiling to Windows/Linux needs no C toolchain. Keep new deps cgo-free for that reason. After changing imports, run `make tidy`.

## Repository layout

- `cmd/mfi/` — CLI frontend (thin; wizard is the default command, subcommands are the advanced path).
- `cmd/mfi-gui/` — the Wails desktop app. `main.go` runs the window; `gui.go` is the bound `GUI` type (thin wrappers over `internal/app`); `frontend/dist/` is the vanilla UI; `build/` holds packaging config (icon, Info.plist).
- `internal/app/` — **the core orchestrator.** Both frontends construct `app.New()` and call its methods; keep all real logic here or in the packages below, never in a frontend.
- `internal/device/`, `internal/transport/` — device discovery and connection, each built around a pluggable registry (`Add` a `Detector`/`Connector`).
- `internal/extract/`, `internal/secrets/`, `internal/diff/`, `internal/dbview/`, `internal/render/`, `internal/report/` — the capability packages; `render` and `dbview` also use registries for per-file-type handlers.

## Architecture decision: Go core + CLI, thin GUI

**Decision (2026-07):** Build a single shared **core library in Go**, consumed by both a Go CLI and a thin desktop GUI. The GUI framework is **Wails** (Go backend + web frontend) unless a later spike shows that rich custom views (hex/diff renderers) demand Flutter-over-FFI instead.

**Why Go:**
- This tool is ~80% systems backend + CLI and ~20% GUI, so the language must serve the backend first. Go is a first-class fit for the dominant work: process orchestration (`adb`, `libimobiledevice`/`idevice*`), USB/TCP/SSH transports, SQLite, binary/plist parsing, and high-throughput secrets scanning.
- **Trufflehog is written in Go.** The spec calls for Trufflehog-style detection explicitly — Go lets us reuse or port its detectors directly instead of reimplementing them.
- Trivial cross-compilation to macOS/Windows/Linux, and an idiomatic CLI story so the CLI is a real first-class frontend, not an afterthought.

**Rejected alternatives:**
- *Flutter as the whole app* — forces the backend into Dart, the weakest part of its ecosystem for forensics/systems work (thin libs for plist/SSH/USB, no Trufflehog reuse). Still viable as a GUI-only layer over the Go core via FFI/IPC if richer custom rendering is needed later.
- *Per-platform native thick clients* — 2–3 UIs and skill sets to maintain; only justified by best-in-class per-OS integration, which this tool does not require.

**Implications for structure:**
- Keep all device I/O, extraction, secrets scanning, diffing, and reporting in the Go core (`internal/` or a `core` package). The CLI and GUI must call the same core — no logic in frontend code.
- The GUI (Wails) is a thin binding layer; the CLI is a thin subcommand wrapper (stdlib `flag` for now). Both expose the wizard and advanced flows.

## What this project is

MFI is a desktop GUI application (macOS + Windows, Linux later) **plus a CLI** for inspecting and exploring the file structures of installed Android and iOS applications. It is a mobile-forensics / secrets-inspection tool. The GUI and CLI are expected to expose the same core capabilities.

## Core capabilities the design must support

These come directly from `intial_claude_prompt.md` and should anchor architecture decisions:

- **Dual entry model**: a guided wizard workflow by default, with an "advanced" exit path for direct control. Both the wizard and advanced modes must be reachable from GUI and CLI.
- **Device discovery & connection**: detect active Android/iOS devices across transports — `adb`, emulators, USB, and TCP bridges — and accept credentials/IP/port when needed.
- **Native device access**: shell / SSH / console into a device's file system.
- **App targeting & extraction**: select an installed app on the device and copy its file structure to a user-specified location.
- **Secrets scanning**: detect secret patterns (Trufflehog-style rulesets and more) across extracted files, and accept a user-supplied file of known secrets to search for.
- **Native diffing**: load two or more file roots (e.g., logged-in vs. logged-out captures) and diff them at a native/semantic level — databases, binaries, XML, config — not just raw bytes.
- **Database mounting**: open/mount database files (SQLite, etc.) for local viewing.
- **Native rendering**: render common and proprietary Android/iOS file types (XML, web caches, databases, config, etc.).
- **Reporting**: summarize findings into actionable reports.

## Architectural implications to keep in mind

- **Pluggable transports and file-type handlers**: transports (adb/USB/TCP/SSH) and file-type renderers/parsers (SQLite, XML, plists, proprietary formats) will grow over time; design them as extensible interfaces from the start.
- **Handles untrusted/sensitive data**: this tool extracts real application data and hunts for secrets. Treat extracted files as untrusted input, and keep discovered secrets and reports out of logs and out of version control.
