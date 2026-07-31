---
name: mobfi-architecture
description: >
  Orientation for working in the MobFI codebase: the core/frontend split,
  which package owns what, and where a new piece of work belongs. Triggers
  when someone asks "where does X go", "how is MobFI structured", "which
  package should this live in", "how do the CLI and GUI share code", "add a
  feature to MobFI", or opens the repo without knowing the layout. Also
  triggers when a change is about to put logic in cmd/ that belongs in
  internal/. Routes to the specialised sibling skills for the actual work:
  mobfi-secret-rules (detection rules), mobfi-file-formats (renderers and
  differs), mobfi-device-support (detectors, transports, extraction),
  mobfi-gui-binding (Wails bindings and frontend), mobfi-untrusted-input
  (device-facing security review), mobfi-release (cutting a release).
---

# MobFI architecture

## Purpose

Answer "where does this go" before code gets written in the wrong
place, and route to the skill that owns the specific job.

## The one rule that matters

**All logic lives in `internal/`. The frontends are thin.**

```
cmd/mfi/       CLI:  flag parsing, output formatting. No logic.
cmd/mfi-gui/   GUI:  Wails bindings, event plumbing. No logic.
internal/app/  The orchestrator both frontends call.
internal/*/    The capabilities.
```

If a behaviour exists in one frontend and not the other, that is a
bug unless it is purely presentational (a progress bar, a table
layout, a confirmation dialog). Both frontends construct an
`*app.App` and drive it; neither reaches past `internal/app` into a
capability package for anything a user would call a feature.

Test for a misplaced change: **could the CLI do this if the GUI
can?** If the answer is no because the logic lives in `cmd/mfi-gui/`,
move it to `internal/`.

Two acceptable exceptions, both already in the tree:

- **Console** (`cmd/mfi-gui/console.go`): a PTY attached to a
  webview terminal has no CLI equivalent, because the CLI user
  already has a terminal. Run `adb shell` directly.
- **Window state, update worker, path refresh**
  (`cmd/mfi-gui/windowstate.go`, `update.go`, `path_*.go`): desktop
  app plumbing with no core meaning.

## Package map

| Package | Owns | Do not put here |
|---|---|---|
| `internal/app` | Orchestration: wiring subsystems, choosing a transport, sequencing an extraction | Format parsing, device I/O details |
| `internal/device` | Device models, detectors (`adb`, libimobiledevice, simctl), app listers | File transfer |
| `internal/transport` | Reaching a device's filesystem: `Conn`, `Connector`, adb and AFC implementations | Deciding which app to pull |
| `internal/extract` | Mirroring a tree to disk, path-safety guards | How to reach the device |
| `internal/backup` | iOS backup production and Manifest.db reconstruction | Generic extraction |
| `internal/secrets` | Rule catalog, scanner, live verification | Report formatting |
| `internal/diff` | Tree diff and structural differs | Rendering |
| `internal/dbview` | Read-only SQLite access | Anything that writes |
| `internal/render` | Per-format human-readable views | Diffing |
| `internal/plist` | Binary and XML property-list decoding | Rendering decisions |
| `internal/decode` | Base64 / hex / URL decoders | File I/O |
| `internal/keystore` | Keychain and Keystore recovery | Generic crypto |
| `internal/report` | Text / JSON / HTML output, redaction | Scanning |
| `internal/doctor` | External-tool detection and install hints | Invoking those tools |
| `internal/selfupdate` | Update check and in-place apply | Version constants |
| `internal/version` | Version, commit, date, repo identity | Update logic |
| `internal/sysproc` | Spawning processes without console windows on Windows | Command construction |

## Where does this go: decision tree

**Adding a way to recognise a secret**
-> `internal/secrets`, rule catalog. Use `mobfi-secret-rules`.

**Adding support for a file format** (view it, diff it, decode it)
-> `internal/render` and/or `internal/diff` and/or `internal/decode`.
Use `mobfi-file-formats`.

**Adding a way to find or reach a device**
-> `internal/device` (find) or `internal/transport` (reach). Use
`mobfi-device-support`.

**Adding a GUI feature**
-> The capability goes in `internal/`; the binding goes in
`cmd/mfi-gui/gui.go`; the UI goes in `frontend/dist/app.js`. Use
`mobfi-gui-binding`.

**Adding a CLI subcommand**
-> `cmd/mfi/commands.go` (a `runX` function) plus a `dispatch` case
and a `usage` line in `cmd/mfi/main.go`. The `runX` function parses
flags and formats output; it calls one `core.X(...)` method that
already exists or that you add to `internal/app`.

**Touching anything that reads device data**
-> Use `mobfi-untrusted-input` before writing the code, not after.

**Shipping a version**
-> Use `mobfi-release`.

## The registry pattern

Five subsystems are pluggable through the same shape: an interface
with a `Handles`/`Supports` predicate, a slice of implementations in
priority order, and a `DefaultRegistry()`-style constructor.

| Subsystem | Interface | Registry | Order matters? |
|---|---|---|---|
| Detectors | `device.Detector` | `device.DefaultRegistry()` | No: results merge |
| Transports | `transport.Connector` | `transport.DefaultRegistry()` | Yes: first `Supports` wins |
| App listers | `device.AppLister` | `device.DefaultAppListers()` | Yes: first `Supports` wins |
| Renderers | `render.Renderer` | `render.DefaultRegistry()` | Yes: first `Handles` wins, hex dump last |
| Structural differs | `diff.FileDiffer` | `diff.defaultFileDiffers` | Yes: first `Handles` wins |

Adding to any of them is: implement the interface, append to the
registry constructor in the right position, add a test. No
registration side-effects, no `init()` magic, no plugin loading.

## Error and degradation conventions

MobFI runs against devices and tools that are frequently absent,
half-working, or lying. The codebase has settled conventions for
that, and new code should match them.

**Partial results beat no results.** `device.Registry.DetectAll`
runs every detector, collects errors with `errors.Join`, and returns
the devices that were found alongside the joined error. A missing
`adb` must not hide an iOS device.

**Record what you could not do, do not drop it.**
`extract.Result.Skipped` carries a path and a reason for every file
that could not be copied, so a report consumer sees the gap.
Silently omitting an unreadable file produces a capture that lies.

**Explain the fix in the error.** Errors a user can act on say how:

```go
return nil, fmt.Errorf("cannot read %s: check the package name is exactly right "+
    "and the app is installed; a non-debuggable app needs root "+
    "(approve the su/superuser prompt on the device)", dataDir)
```

That is not verbosity, it is the difference between a bug report and
a solved problem.

**Degrade with a reason rather than failing.** `keystore.Dump`
returns a `Result` with `Degraded: true` and populated `Limitations`
when no method can run, so the UI always has something actionable.

**Check output, not just exit status,** when talking to `adb`. It
does not reliably propagate a remote command's exit code. See
`app.canReadDir`, which compares `ls -d <dir>` output against the
requested path rather than trusting a zero exit.

## Build, test, verify

```sh
make build              # CLI to ./bin/mfi
make test               # go test ./...
make vet                # go vet ./...
make fmt                # gofmt -w .
make check-ascii        # .ps1 files must stay pure ASCII
```

CI gates on `go build ./cmd/mfi`, `go vet` and `go test` over
`./cmd/mfi ./internal/...`, and `make check-ascii`. The GUI
(`cmd/mfi-gui`) links Wails and WebKit through cgo and is not built
in CI; build it locally when you change it.

**The `check-ascii` rule is not cosmetic.** Windows PowerShell 5.1
reads BOM-less files as Windows-1252, so a stray non-ASCII byte in a
`.ps1` (an em-dash pasted from a document, most often) breaks the
Windows installer for every user. Keep `scripts/*.ps1` pure ASCII.

## Testing conventions

- **Table tests** with a `cases` slice for pure functions
  (`internal/decode`, redaction, version comparison).
- **`t.TempDir()`** for anything touching the filesystem. Never a
  hardcoded `/tmp` path.
- **Stub the process boundary, not the package.** `adbConn` carries
  a `run func(ctx, name, args ...string) ([]byte, error)` field
  precisely so tests can substitute `adb` without a device:

  ```go
  a := &adbConn{bin: "adb", serial: "SER", run: func(_ context.Context, name string, args ...string) ([]byte, error) {
      return []byte(fixtureOutput), nil
  }}
  ```

- **Synthesise fixtures in the test** where the format allows:
  `internal/backup` builds a real `Manifest.db` with `database/sql`
  rather than committing a binary. Commit fixtures only when
  construction is impractical.
- **Assert on behaviour, not log text.** The exception is an error
  message that is itself the feature, as with the device-state
  guidance above.

## Cross-references

| Job | Skill |
|---|---|
| Detection rules and verifiers | `mobfi-secret-rules` |
| Renderers, differs, decoders | `mobfi-file-formats` |
| Detectors, transports, extraction | `mobfi-device-support` |
| Wails bindings and frontend | `mobfi-gui-binding` |
| Device-facing security review | `mobfi-untrusted-input` |
| Cutting a release | `mobfi-release` |

User-facing behaviour is documented in `docs/handbook/`. When a
change alters what an operator sees or can do, update the relevant
chapter in the same pull request: the handbook is generated and CI
checks it stays in sync with its sources.
