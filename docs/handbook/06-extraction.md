# Extraction

Extraction mirrors an app's on-device file tree into a local
directory. This is the core operation: everything downstream
(scanning, diffing, database browsing, rendering) works on what you
pull here.

```sh
$ mfi extract -device <device-id> \
              -app <bundle-id> \
              -out <destination-dir> \
              [-scope container|documents|backup]
```

| Flag | Required | Meaning |
|---|---|---|
| `-device` | yes | Device ID from `mfi detect` |
| `-app` | yes | Package name (Android) or bundle id (iOS) |
| `-out` | yes | Local destination directory, created if absent |
| `-scope` | iOS only | `container` (default), `documents`, or `backup` |

Progress streams to stderr, so stdout stays a clean summary you can
redirect:

```sh
$ mfi extract -device <id> -app com.example.target -out ./cap > summary.txt
```

**Evidence**: extract into a fresh directory per capture. Reusing a
destination mixes two devices' data into one tree and destroys the
provenance of everything in it.

## Android

Android extraction targets `/data/data/<package>`, the app's private
data directory. MobFI probes three access mechanisms in order and
uses the first that can actually read the directory:

1. **`run-as <package>`** runs commands as the app's own uid. Works
   on any device, no root, but only for a **debuggable** app (one
   built with `android:debuggable="true"`). This is the normal path
   for testing your own builds.
2. **`su -c` as root** on a rooted device. Reaches any app's data
   regardless of debuggability. A Magisk or superuser prompt may
   appear on the device; approve it.
3. **A plain shell**, for devices where the shell user already has
   access (`adb root` on userdebug/eng builds).

The probe checks command *output*, not just exit status: `adb` does
not reliably propagate a remote command's exit code, so a directory
that does not exist can otherwise look like success and produce a
silent empty extract.

If none of the three can read the directory, extraction fails with:

```
cannot read /data/data/com.example.target: check the package name is
exactly right and the app is installed; a non-debuggable app needs
root (approve the su/superuser prompt on the device)
```

That message is almost always literally correct. Check the package
name first (copy it from `mfi apps`), then device state.

### Transfer mechanism

Where the device has `tar` (toybox, Android 6 and later), MobFI
streams the whole tree in a single `adb exec-out tar` process rather
than one `cat` per file. That is dramatically faster on trees with
many small files, which describes almost every app.

If the device has no `tar`, or the stream produces nothing, MobFI
falls back automatically to per-file copying. You do not choose;
there is no flag. A capture that took the fallback path simply takes
longer.

Symlinks, sockets, and FIFOs are skipped rather than followed. That
avoids `cat` blocking forever on a special file and avoids following
a symlink out of the app's tree.

## iOS

iOS offers three scopes, and picking the right one is the whole
game.

### `container` (default)

The full app container over AFC house arrest.

```sh
$ mfi extract -device <udid> -app com.example.target -out ./cap
```

Works for **dev-signed and debug builds**. App Store apps deny it.
When the container scope fails, MobFI appends guidance to the error
rather than leaving you guessing:

```
(full container access needs a dev-signed/debug app or a jailbreak;
try the 'documents' scope, or 'backup' scope to pull a production
app's data)
```

### `documents`

Just the app's `Documents` directory, over the same AFC mechanism.

```sh
$ mfi extract -device <udid> -app com.example.target -out ./cap -scope documents
```

Works more broadly than `container`, because any app that enables
file sharing (`UIFileSharingEnabled`) vends its Documents directory
to the host. You get less data, but you get it from apps that would
otherwise refuse entirely. Try this before reaching for a backup.

### `backup`

Reconstructs the app's data from a **full device backup** made with
`idevicebackup2`.

```sh
$ mfi extract -device <udid> -app com.example.target -out ./cap -scope backup
```

This is the only way to reach a production App Store app's private
data on a stock, non-jailbroken device. MobFI drives
`idevicebackup2` to produce a backup, then reads the backup's
`Manifest.db` and reconstructs just the target app's domains into
your destination: the app's own `AppDomain`, plus any related domain
referencing the bundle id (app groups, extensions and plugins,
keychain-shared containers).

What to expect:

- **It is slow.** A full backup of a device with photos and media
  can run for tens of minutes and consume tens of gigabytes of
  temporary space before reconstruction begins. MobFI estimates the
  size up front from `ideviceinfo`'s disk-usage domain.
- **It needs free disk.** Budget for the whole device backup, not
  just the app's share.
- **Symlinks in the backup are recorded, not recreated**, since a
  backup symlink points at an on-device path that does not exist
  locally.
- **Encrypted backups are required for keychain data.** The
  extraction itself works either way, but see the Keys chapter: the
  keychain is only present in encrypted backups.

**OPSEC**: a backup is a user-visible operation. It appears in the
device's backup history and can trigger "iPhone backup" indications
on the device. Do not use this scope when a covert capture matters.

### Simulator

For a booted iOS Simulator (transport `simulator`), MobFI bypasses
AFC entirely and copies the container straight off the host
filesystem. No scope applies, every installed app is reachable, and
it is fast. This is the best target for building familiarity.

## Choosing the scope: decision table

| Your situation | Scope |
|---|---|
| iOS Simulator | (automatic) |
| Your own dev/debug build on a device | `container` |
| Jailbroken device | `container` |
| App Store app, file sharing enabled | `documents` |
| App Store app, need everything | `backup` |
| Documents scope returned too little | `backup` |

## Reading the result

```
extracted 148 file(s), 2317884 byte(s) to /home/op/captures/target
skipped 2 path(s):
  /data/data/com.example.target/cache/locked.db: Permission denied
  /data/data/com.example.target/files/sock: unsupported entry type
```

Three things to check every time:

- **File count is non-zero.** Zero files means the mechanism could
  not read the tree. Re-check bundle id and device state.
- **Skipped paths.** MobFI records what it could not copy instead of
  silently dropping it. Permission denials on individual files are
  normal (some app caches are locked while the app is running);
  wholesale denials mean the wrong access mechanism was used.
- **Byte count is plausible.** A 4 KB "capture" of an app you know
  holds a large database means you got a skeleton, not the data.

Skipped entries are recorded in the JSON report too, so a report
consumer can see the gap rather than assume a clean tree.

## Path safety

Device-supplied names are not trusted. MobFI applies two guards:

- **Destination containment.** A device path that would resolve
  outside the destination directory is refused and recorded as
  skipped, rather than written.
- **Host-legal filenames.** Names that are legal on the device but
  illegal on your host (a Firebase config file containing `:` on
  Windows, for example) are percent-encoded per component so the
  tree still writes, with directory structure preserved.

## Practical patterns

**Baseline and post-action captures**, the setup for diffing:

```sh
$ mfi extract -device <id> -app <bundle> -out ./cap-loggedout
# ... log into the app on the device ...
$ mfi extract -device <id> -app <bundle> -out ./cap-loggedin
$ mfi diff -a ./cap-loggedout -b ./cap-loggedin
```

**Capture then immediately scan**:

```sh
$ mfi extract -device <id> -app <bundle> -out ./cap && mfi scan -root ./cap
```

**Several apps in one pass**:

```sh
$ for pkg in com.example.a com.example.b com.example.c; do
    mfi extract -device <id> -app "$pkg" -out "./caps/$pkg"
  done
```

## In the GUI

The Extract tab exposes the same options: device picker, app picker,
destination browser, and (on iOS) a scope selector. Progress shows
as a live file and byte count with a cancel button. Cancelling stops
the transfer; files already written stay on disk.

<!-- screenshot: extract-tab-progress.png -->

## Next

- Scanning: find secrets in what you pulled.
- Diffing: compare two captures.
