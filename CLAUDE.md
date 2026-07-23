# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project status

Early build. The package boundaries and interfaces are in place. Implemented so far: device detection (adb + libimobiledevice), the adb transport connector, `extract.Run` (mirrors an app's on-device tree to disk), and the `secrets` scanner (Trufflehog-style built-in rules + user-supplied known-secret lists, with redacted findings), `diff.Trees` (tree + content diff of two extracted roots; structural differs still to come), and `report` (aggregates scan + diff into a text summary and a JSON export), `dbview` (read-only, immutable SQLite inspection), and `render` (JSON/XML/plist/SQLite-summary/text/hex, pluggable per file type) — all wired end-to-end through `mfi detect` / `mfi extract` / `mfi scan` / `mfi diff` / `mfi report` / `mfi db` / `mfi render`. Every core capability from the spec now has a working first implementation; grep for `TODO` for the known follow-ups (iOS/AFC extraction, structural diffs, binary-plist decoding, the Wails GUI). Read `intial_claude_prompt.md` (the founding spec) for the full requirement set.

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

The one external dependency so far is `modernc.org/sqlite` (used by `internal/dbview`) — chosen because it is **cgo-free**, so cross-compiling to Windows/Linux needs no C toolchain. Keep new deps cgo-free for that reason. After changing imports, run `make tidy`.

## Repository layout

- `cmd/mfi/` — CLI frontend (thin; wizard is the default command, subcommands are the advanced path).
- `cmd/mfi-gui/` — placeholder for the Wails desktop frontend; run `wails init` to generate `frontend/` + `wails.json`.
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
