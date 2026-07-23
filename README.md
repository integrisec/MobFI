# MFI

A cross-platform desktop + CLI tool for inspecting and exploring the file
structures of installed Android and iOS applications: device discovery,
app-data extraction, secrets scanning, native-level diffing, database
viewing, and reporting.

See [`intial_claude_prompt.md`](./intial_claude_prompt.md) for the full
specification and [`CLAUDE.md`](./CLAUDE.md) for the architecture decision.

## Architecture

A single shared **core library in Go** (`internal/`) is driven by two thin
frontends: a Go CLI (`cmd/mfi`) and a Wails desktop GUI (`cmd/mfi-gui`, not
yet generated). All device I/O, extraction, scanning, diffing, and reporting
live in the core — never in a frontend.

## Prerequisites

- **Go 1.23+** (not currently installed on this machine — `brew install go`)
- For the GUI: the [Wails](https://wails.io) CLI and Node.js (added later)

## Build & run

```sh
make build        # -> bin/mfi
make run          # runs the CLI wizard
make test         # go test ./...
make vet          # go vet ./...
go run ./cmd/mfi detect       # list reachable devices
go run ./cmd/mfi help         # all subcommands
```

## Status

Scaffold only. Every subsystem exposes its interfaces and package boundaries
but the implementations return `ErrNotImplemented` — search for `TODO` to find
the next work items.
