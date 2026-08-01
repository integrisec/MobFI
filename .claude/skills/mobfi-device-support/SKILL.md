---
name: mobfi-device-support
description: >
  Add or fix the code that finds devices, reaches their filesystems, lists
  their apps, and pulls app data. Triggers on "add a device detector", "add a
  transport", "support <device type>", "detection does not find my device",
  "add an extraction scope", "extraction returns zero files", "app listing is
  empty", or edits under internal/device, internal/transport, internal/extract,
  or internal/backup. Covers the Detector, AppLister, Connector, Conn and
  TarStreamer interfaces, the access-mechanism probe order for Android, the
  iOS scope model, and the degradation conventions detection must follow.
---

# Device support

## Purpose

Work on the layers between a physical device and an extracted tree:
detection, connection, app listing, and extraction.

## The four layers

| Layer | Package | Interface | Answers |
|---|---|---|---|
| Detection | `internal/device` | `Detector` | What devices exist? |
| App listing | `internal/device` | `AppLister` | What is installed? |
| Transport | `internal/transport` | `Connector` -> `Conn` | How do I read its files? |
| Extraction | `internal/extract`, `internal/backup` | (functions) | Copy the tree to disk |

`internal/app` sequences them. A change that spans layers usually
belongs in `internal/app`, not inside one of the layer packages.

## Detectors

```go
type Detector interface {
    Name() string
    Detect(ctx context.Context) ([]Device, error)
}
```

Registered in `device.DefaultRegistry()`: ADB, iOS
(libimobiledevice), simctl. Order does not matter, because
`DetectAll` merges every detector's results.

### The degradation contract

`DetectAll` runs every detector, collects failures with
`errors.Join`, and returns the devices that were found alongside the
joined error. **A detector that fails must not suppress the others.**
A host without `adb` still lists iOS devices.

Consequences for a new detector:

- **Return `nil, err` on a real failure**, not a panic, and not a
  silent empty slice. The error names the detector for the user.
- **Return an empty slice and `nil` when the tool is absent.** A
  missing tool is not an error condition; it means "no devices of
  this kind". Follow how `simctlUnavailable` distinguishes "xcrun
  cannot find simctl" from "simctl failed".
- **Do not block indefinitely.** Honour the context; device tools
  hang when a device is in a bad state.

### Device fields

```go
type Device struct {
    ID        string    // adb serial or iOS UDID: what -device takes
    Name      string    // human label
    Platform  Platform  // Android | IOS
    Transport Transport // USB | TCP | Emulator | Simulator
    Address   string    // host:port for TCP transports
    State     string    // "ready", "offline", "unauthorized", "unpaired"
}
```

`ID` is the user-facing handle: it must be stable and it must be
what the underlying tool accepts, because it is passed straight back
to `adb -s` or `idevice* -u`.

Pass tool-reported states through `normalizeState`, which maps adb's
bare `device` to `ready` and leaves everything else alone. A state
string the user does not recognise is a support burden; if you
introduce one, document it in `docs/handbook/04-devices.md`.

## App listers

```go
type AppLister interface {
    Supports(d Device) bool
    List(ctx context.Context, d Device, includeSystem bool) ([]InstalledApp, error)
}
```

`device.DefaultAppListers()` returns ADB, simctl, and iOS listers;
the first whose `Supports` returns true wins, so order matters.
Simulator must be checked before generic iOS, because a Simulator is
an iOS device with a different access path.

Honour `includeSystem`: the default (false) shows user apps only,
because a stock handset lists hundreds of system packages and the
target is almost never among them.

## Transports

```go
type Conn interface {
    Exec(ctx context.Context, cmd string, args ...string) ([]byte, error)
    Open(ctx context.Context, path string) (io.ReadCloser, error)
    Walk(ctx context.Context, root string, fn fs.WalkDirFunc) error
    Close() error
}

type Connector interface {
    Supports(d device.Device) bool
    Connect(ctx context.Context, d device.Device) (Conn, error)
}
```

Optional capability, detected with a type assertion:

```go
type TarStreamer interface {
    TarReader(ctx context.Context, root string) (io.ReadCloser, error)
}
```

A `Conn` that can stream a whole directory as one tar should
implement it: `app.ExtractApp` type-asserts and prefers it, because
one process for a tree beats one `cat` per file by a wide margin on
the small-file trees mobile apps produce. The fallback is automatic
when the assertion fails or the stream yields nothing.

### Walk conventions

`Walk` visits directories and regular files only. Symlinks, sockets,
and FIFOs are skipped deliberately: `cat` on a FIFO blocks forever,
and following a symlink walks out of the app's tree. Honour
`fs.SkipDir` and `fs.SkipAll`.

Tolerate partial enumeration. A permission denial deep in a tree
should not abort the walk; the caller records it as a skipped path.

## Android access mechanisms

`app.androidConn` probes three mechanisms against the real data
directory and uses the first that can read it:

1. **`run-as <pkg>`** runs as the app's own uid. Works on any device
   for a **debuggable** app. No root.
2. **`su -c`** as root. Reaches any app on a rooted device. A
   superuser prompt may appear on the device.
3. **Plain shell**, for `userdebug`/`eng` builds where the shell
   user already has access.

The probe is `canReadDir`, and it checks **command output**, not
exit status:

```go
out, err := conn.Exec(ctx, "ls", "-d", dataDir)
if err != nil {
    return false
}
return strings.TrimSpace(string(out)) == dataDir
```

`adb` does not reliably propagate a remote command's exit code, so
an inaccessible or non-existent directory otherwise looks like
success and produces a **silent empty extract**. Any new probe
against a device must validate output the same way.

When all three fail, the error names both likely causes, which is
the difference between a bug report and a fix:

```
cannot read /data/data/<pkg>: check the package name is exactly right and
the app is installed; a non-debuggable app needs root (approve the
su/superuser prompt on the device)
```

## iOS scopes

| Scope | Mechanism | Reaches |
|---|---|---|
| `container` (default) | AFC house arrest | Dev-signed and jailbroken only |
| `documents` | AFC house arrest | Any app with file sharing enabled |
| `backup` | `idevicebackup2` + Manifest.db reconstruction | Production App Store apps |

A booted **Simulator** bypasses AFC entirely: containers live on the
host filesystem, so `app.ExtractApp` resolves the container path and
uses a local `Conn`.

When the `container` scope fails, the error tells the user the two
alternatives rather than leaving them to guess. Preserve that
behaviour if you touch the path.

### The backup scope

`internal/backup` drives `idevicebackup2` to produce a full device
backup, then reads `Manifest.db` and reconstructs the target app's
domains: its own `AppDomain`, plus any domain referencing the bundle
id (app groups, extensions, keychain-shared containers).

Constraints to keep in mind when changing it:

- It is slow and disk-hungry: a full device backup precedes
  reconstruction. Estimate up front (`EstimateBackupSize`).
- Backup entries are records, not files on your host. `flags`
  distinguishes file, directory, and symlink; symlinks are recorded
  as skipped rather than recreated, because they point at on-device
  paths that do not exist locally.
- Manifest contents are attacker-controlled. See
  `mobfi-untrusted-input`.

## Extraction

`internal/extract` mirrors a tree and owns the path-safety guards:

- **Destination containment**: a device path resolving outside the
  destination is refused and recorded as skipped.
- **Host-legal filenames**: names legal on the device but illegal on
  the host are percent-encoded per component, preserving directory
  structure.
- **Skip tracking**: `Result.Skipped` records path and reason for
  everything not copied.

Never weaken those to make an extraction "work". A capture that
silently drops files is worse than one that reports gaps.

## Testing without a device

Stub the process boundary. `adbConn` carries an injectable `run`
field for exactly this:

```go
a := &adbConn{bin: "adb", serial: "SER", run: func(_ context.Context, name string, args ...string) ([]byte, error) {
    return []byte(fixtureFindOutput), nil
}}
```

Existing examples: `internal/transport/adb_test.go` (argv shape and
walk behaviour), `internal/device/*_test.go` (parsing tool output),
`internal/backup/backup_test.go` (a synthetic `Manifest.db` built
with `database/sql`).

Test tool-output parsing against **real captured output**, including
the odd cases: an unauthorised device line, a device with spaces in
its name, an empty list.

## Documentation

`docs/handbook/04-devices.md` (detection, states, connection
methods) and `docs/handbook/06-extraction.md` (mechanisms and
scopes) describe this behaviour to operators. A new transport,
scope, or state string changes those chapters in the same pull
request.

## Cross-references

- `mobfi-untrusted-input`: device output is hostile input; argv
  construction and path handling rules live there
- `mobfi-architecture`: layer boundaries and the registry pattern
- `mobfi-file-formats`: what happens to files once extracted
