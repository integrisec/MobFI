---
name: mobfi-gui-binding
description: >
  Add or change a MobFI desktop-GUI feature: a Wails bound method, its
  frontend wiring, progress events, cancellation, and native dialogs.
  Triggers on "add a GUI feature", "add a button to the GUI", "expose X to the
  frontend", "add a Wails binding", "the GUI needs to show", "add a tab", or
  edits under cmd/mfi-gui. Covers what Wails will and will not bind, the
  ctx-capture pattern, the opContext cancellation convention, progress event
  naming, when a native dialog is required rather than a JS confirm, and the
  frontend rules for rendering attacker-controlled strings.
---

# GUI bindings

## Purpose

Wire a capability into the desktop app without putting logic in the
frontend or rendering device data unsafely.

## Before you start

**The capability belongs in `internal/`.** A bound method should be
a thin wrapper: acquire a context, call one `internal/app` method,
relay progress, return the result. If you are writing extraction,
parsing, or scanning logic inside `cmd/mfi-gui/`, stop and move it.
See `mobfi-architecture`.

## How Wails binding works

`cmd/mfi-gui/main.go` binds a single struct:

```go
err := wails.Run(&options.App{
    // ...
    OnStartup:  gui.startup,
    OnShutdown: gui.shutdown,
    Bind:       []any{gui},
})
```

**Every exported method on that struct becomes callable from
JavaScript.** There is no per-method opt-in. Adding an exported
method to `GUI` adds it to the frontend's API surface, whether you
intended that or not. Unexported helpers are not bound: keep
anything that is not deliberately part of the frontend API
lowercase.

The context arrives at startup and is stored on the struct:

```go
func (g *GUI) startup(ctx context.Context) { g.ctx = ctx }
```

Every runtime call needs that context.

### Method signature rules

- Exported method on the bound struct.
- Parameters and returns must be JSON-serialisable: strings,
  numbers, bools, structs with exported fields, slices, maps.
- Return `(T, error)` and the error surfaces as a rejected promise
  in JS. Return just `T` when failure is impossible.
- Do not take a `context.Context` parameter: use `g.ctx` or
  `g.opContext(...)`.

From JS, bound methods appear under the generated bindings object,
which `app.js` accesses through its `gui()` helper:

```js
const findings = await gui().ScanSecrets(root);
```

## The cancellation convention

Long operations use `opContext`, which gives one cancellable context
per named operation and supersedes a stale one:

```go
func (g *GUI) ScanSecrets(root string) ([]secrets.Finding, error) {
    ctx, done := g.opContext("scan")
    defer done()
    return g.app.ScanSecrets(ctx, root, progressFn)
}
```

`CancelOp("scan")` from the frontend cancels it. Existing operation
names: `"scan"`, `"diff"`, `"extract"`.

Use `opContext` for anything that can run longer than about a
second, which is every device operation. Use `g.ctx` directly only
for instantaneous calls.

## Progress events

Long operations relay progress as Wails events rather than blocking:

```go
wailsruntime.EventsEmit(g.ctx, "scan:progress", p)
```

Frontend side:

```js
window.runtime.EventsOn("scan:progress", (p) => updateProgress(p));
```

Conventions:

- **Name events `<operation>:<phase>`**: `scan:progress`,
  `diff:progress`, `update:progress`, `update:done`,
  `console:data:<id>`, `console:exit:<id>`.
- **Throttle.** Emitting per file floods the IPC channel and stalls
  the UI. The existing pattern drops updates inside a 120 ms window:

  ```go
  var last time.Time
  progress := func(p secrets.Progress) {
      if time.Since(last) < 120*time.Millisecond {
          return
      }
      last = time.Now()
      wailsruntime.EventsEmit(g.ctx, "scan:progress", p)
  }
  ```

- **Emit structured data**, not preformatted strings, so the
  frontend decides presentation.

## Native dialogs

Available through the Wails runtime, all taking the context:

| Call | Use for |
|---|---|
| `wailsruntime.MessageDialog(ctx, opts) (string, error)` | Confirmations and warnings |
| `wailsruntime.OpenDirectoryDialog(ctx, opts) (string, error)` | Picking a capture destination |
| `wailsruntime.OpenFileDialog(ctx, opts) (string, error)` | Picking a file |
| `wailsruntime.SaveFileDialog(ctx, opts) (string, error)` | Choosing a report path |

`GUI.Confirm(title, message)` wraps `MessageDialog` with Yes/No and
a `No` default. Use it rather than rolling another dialog.

**`window.confirm()` does not work.** The WKWebView used on macOS
does not implement the JS dialog functions, which is why the
frontend calls `gui().Confirm(...)` instead. A JS `confirm` will
silently do nothing on a supported platform.

### When a native dialog is required

Use one, in the Go layer, before any action that:

- Sends data off the workstation (live secret verification).
- Reveals raw credential material (unredacted export, reveal
  toggles).
- Replaces the running binary (self-update).
- Deletes anything on disk.

A confirmation implemented purely in JS is a UX affordance, not a
control: any code running in the webview can call the bound method
directly. Where the confirmation is the safety property, it belongs
on the Go side of the boundary.

## Frontend rules

`cmd/mfi-gui/frontend/dist/` is vanilla HTML, CSS, and JS with no
build step. Edit `app.js` directly.

**Never interpolate device-supplied strings into HTML.** File paths,
bundle ids, app names, error text from `adb`, and secret matches all
originate from an untrusted device. Build DOM nodes and assign
`textContent`:

```js
// Good: the value can contain anything, and it stays inert.
const cell = el("td", { textContent: f.path });

// Wrong: a crafted path becomes markup.
cell.innerHTML = "<td>" + f.path + "</td>";
```

`el(tag, props, ...children)` and `textContent` are the established
pattern throughout `app.js`. Reserve `innerHTML` for markup you
constructed yourself from constants.

**Rendered file content is likewise untrusted.** Syntax-highlighted
output and decoded values come from device files.

**Clean up listeners.** `EventsOn` returns an unsubscribe function;
call it when a view is torn down, or repeated tab switches stack
duplicate handlers.

## Adding a feature end to end

Adding "export the current scan as CSV":

1. **Core**: add the CSV writer to `internal/report`, with a test.
2. **Orchestrator**: expose it on `*app.App` if the frontends need
   an entry point that does not exist.
3. **Binding** in `cmd/mfi-gui/gui.go`:

   ```go
   func (g *GUI) ExportScanCSV(path string) error {
       g.reportMu.Lock()
       findings := g.lastFindings
       g.reportMu.Unlock()
       return g.app.WriteScanCSV(findings, path)
   }
   ```

4. **Frontend**: a button whose handler calls
   `gui().SaveFileDialog(...)` then `gui().ExportScanCSV(path)`,
   with the result surfaced through the existing toast helper.
5. **CLI parity**: add the equivalent flag to `cmd/mfi/commands.go`,
   unless the feature is inherently graphical.
6. **Handbook**: update the relevant chapter under `docs/handbook/`.

## Shared state

`GUI` holds state across calls (`lastFindings`, `lastDiff`,
`scanned`) guarded by `reportMu`. Bound methods are invoked from the
webview and can overlap, so any new shared field needs the same
treatment. Take the lock, copy what you need, release, then do the
slow work outside the lock.

## Building and testing

```sh
cd cmd/mfi-gui && wails dev     # live-reload development
cd cmd/mfi-gui && wails build   # package
```

CI does not build the GUI (it needs cgo, WebKit, and per-OS
toolchains), so **build it locally before pushing a GUI change**.
`go vet ./cmd/mfi-gui` catches Go-side mistakes without the full
toolchain. Frontend JS has no test harness: exercise the path in
`wails dev`.

## Cross-references

- `mobfi-architecture`: why the logic goes in `internal/`
- `mobfi-untrusted-input`: what makes device strings hostile
- `mobfi-secret-rules`: the redaction invariant the Scan tab relies on
