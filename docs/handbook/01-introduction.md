# Introduction

MobFI (Mobile Filesystem Inspector) inspects the on-device file
structures of installed Android and iOS applications. Use it to
discover devices, pull an app's private data to your workstation,
hunt for secrets in what you pulled, diff two captures, browse
databases, render native file formats, and summarise the result
into a report.

This handbook is the complete operator reference. It covers every
workflow in both frontends, the constraints that decide which
workflow is even possible on a given device, and what to do when a
step fails.

## Who this is for

You are running MobFI against a device you are authorised to test:
your own handset, a lab device, a client device inside an agreed
scope. MobFI does not bypass platform security. Everything it
reaches, it reaches through documented mechanisms (`adb`,
`libimobiledevice`, backups) that the device owner has enabled by
unlocking the device, authorising the host, and (where required)
rooting or jailbreaking it.

Prior knowledge assumed: a terminal, basic Android/iOS concepts
(package name vs bundle id, USB debugging, device pairing). No Go
knowledge required unless you plan to build from source.

## The two frontends

One shared core library in Go (`internal/`) holds every piece of
logic: device I/O, extraction, scanning, diffing, database access,
rendering, reporting. Two thin frontends drive it.

| Frontend | Binary | Best for |
|---|---|---|
| CLI | `mfi` | Scripting, remote/headless hosts, reproducible captures, piping into other tools |
| Desktop GUI | `mfi-gui` | Exploration, browsing extracted trees, reading rendered files, interactive triage |

Every capability is available from both, because both call the same
core methods. The CLI is cgo-free and ships as a prebuilt binary;
the GUI is a [Wails](https://wails.io) app (Go backend, dependency-free
HTML/JS/CSS UI) that you build locally because it needs per-OS
WebKit/WebView toolchains.

When this handbook shows a workflow, it gives the CLI invocation
first (precise, copy-pasteable) and the GUI equivalent second.

## The trust model

Read this section before pointing MobFI at a device you did not
personally provision.

**The device is untrusted input.** Everything that crosses from the
device into MobFI is attacker-controlled if the device is
compromised or if you are examining a malicious app:

- File and directory names returned by on-device enumeration.
- File contents: SQLite databases, property lists, JSON, images,
  PDFs, source-looking text.
- App metadata: bundle ids, icon resource names, `dumpsys` output.
- Backup manifests, when you use the iOS backup extraction scope.

MobFI treats that data as hostile, but the guarantee is not
absolute: parsing arbitrary attacker-controlled formats is
inherently risky. Practical guidance:

- Extract into a **fresh, dedicated directory** per capture. Do not
  reuse a destination across engagements or across devices.
- Prefer a **disposable analysis VM** when examining known-malicious
  apps.
- Treat the extracted tree as evidence: do not execute anything out
  of it. The GUI's "Open externally" hands a file to your operating
  system's default handler, which will happily run an executable.

**Your workstation is the thing being defended.** MobFI's job is to
copy data off a device without letting the device reach back into
your host.

**Extracted data is client data.** A capture from a real engagement
contains credentials, tokens, personal information, and session
material belonging to whoever owns the device. Handle the
destination directory with the same care as any other evidence
artifact: encrypted volume, retention policy, secure destruction.

## What MobFI does not do

- **It does not exploit.** No jailbreak, no root exploit, no
  sandbox escape. If a device is stock and locked, MobFI's reach is
  limited to what backups and debuggable apps expose.
- **It does not modify the device.** Extraction is read-only from
  the device's perspective. The one exception is the iOS backup
  scope, which asks the device to produce a backup (a normal,
  user-initiated operation).
- **It does not decrypt what the platform will not release.**
  Hardware-backed Android Keystore private keys and iOS Secure
  Enclave keys are non-exportable by design. MobFI inventories
  them; it cannot dump them.
- **It does not phone home.** The only outbound network traffic is
  the update check (which you can disable with `MFI_NO_UPDATE_CHECK`)
  and the opt-in secret verification described in the scanning
  chapter.

## How capability depends on device state

More than anything else, what you can extract is decided by the
device's state. This table is the single most useful thing in the
handbook; the extraction chapter expands each row.

| Platform | Device state | App type | What you can reach |
|---|---|---|---|
| Android | Any (USB debugging on) | Debuggable (`android:debuggable="true"`) | Full private data dir via `run-as` |
| Android | Rooted | Any | Full private data dir via `su` |
| Android | userdebug/eng build | Any | Full private data dir via the shell user |
| Android | Stock, locked | Non-debuggable | Nothing private. Public storage only |
| iOS | Any (paired + trusted) | File-sharing enabled | The app's `Documents` directory |
| iOS | Any (paired + trusted) | Dev-signed / debug build | The full app container |
| iOS | Any (paired + trusted) | Any, incl. App Store | App data via an encrypted device backup |
| iOS | Jailbroken | Any | Full container, plus keychain via SSH |
| iOS | Simulator (macOS) | Any installed | Container directly off the host filesystem |

If a workflow in this handbook fails, check this table first. Most
"MobFI cannot see my app's data" reports are a device-state
mismatch, not a bug.

## How to read this handbook

- **New to MobFI**: read Installation, then First Run, then stop.
  That is enough to complete a capture end to end.
- **Doing a specific job**: jump to the chapter for that job.
  Each is self-contained and states its own prerequisites.
- **Something broke**: Troubleshooting, then the chapter for the
  workflow that broke.
- **Looking up a flag**: the CLI Reference chapter lists every
  command and every flag.

Conventions used throughout:

- `$ command` denotes a shell prompt on your workstation. The `$`
  is not part of the command.
- `<angle-brackets>` mark a value you substitute.
- Blocks labelled **OPSEC** flag behaviour that is observable on the
  device, on the network, or by a third party.
- Blocks labelled **Evidence** flag behaviour that matters for
  chain of custody.

## Getting help

- `mfi help` lists every command.
- `mfi <command> -h` lists the flags for one command.
- `mfi doctor` reports which external tools are installed and how
  to install the missing ones.
- The repository `README.md` covers build and install specifics
  that change more often than this handbook.
