# Appendix

## Repository layout

```
cmd/
  mfi/            CLI frontend
  mfi-gui/        Wails desktop frontend (Go + frontend/dist)
internal/
  app/            Framework-agnostic core; both frontends drive this
  backup/         iOS backup extraction and reconstruction
  dbview/         Read-only SQLite access
  decode/         Base64 / hex / URL decoders
  device/         Device models and detectors (adb, libimobiledevice, simctl)
  diff/           Tree diff plus SQLite / JSON / plist structural differs
  doctor/         External-tool detection and install hints
  extract/        Tree mirroring and path-safety guards
  keystore/       iOS Keychain and Android Keystore recovery
  plist/          Binary and XML property-list decoding
  render/         Per-format renderers with a hex-dump fallback
  report/         Text / JSON / HTML reporting
  secrets/        Rule catalog, scanner, live verification
  selfupdate/     Update check and in-place apply
  sysproc/        Process spawning (hides console windows on Windows)
  transport/      adb and AFC transports
  version/        Version constants
docs/
  handbook/       This handbook's chapter sources
  handbook.md     Generated: single-file handbook
  handbook.pdf    Generated: print artifact
scripts/
  build-handbook.sh   Handbook generator
  install.sh          macOS / Linux installer
  install.ps1         Windows installer
```

All logic lives in `internal/`. The frontends are thin: if a
behaviour differs between the CLI and the GUI, it is a frontend
presentation difference, not a capability difference.

## On-device paths worth knowing

### Android

| Path | Contents |
|---|---|
| `/data/data/<pkg>/` | App private data root (extraction target) |
| `/data/data/<pkg>/shared_prefs/` | XML preference files. Frequent source of plaintext tokens |
| `/data/data/<pkg>/databases/` | SQLite databases |
| `/data/data/<pkg>/files/` | Arbitrary app files |
| `/data/data/<pkg>/cache/` | Caches. Noisy in diffs |
| `/data/app/<pkg>/` | Installed APK |
| `/data/misc/keystore/` | Keystore blobs (root only) |
| `/sdcard/Android/data/<pkg>/` | External app storage, world-readable historically |

### iOS

| Path | Contents |
|---|---|
| `Documents/` | User documents. Exposed when file sharing is enabled |
| `Library/Preferences/` | Property lists. Frequent source of plaintext tokens |
| `Library/Caches/` | Caches. Noisy in diffs |
| `Library/Application Support/` | App data, often SQLite |
| `tmp/` | Temporary files |

Within a backup, data is organised by **domain**
(`AppDomain-<bundle-id>`, `AppDomainGroup-*`, `KeychainDomain`)
rather than by directory tree. MobFI's backup scope reconstructs the
target's domains into a normal directory layout.

## Glossary

**AFC** - Apple File Conduit. The protocol libimobiledevice uses to
read files from an iOS device. "House arrest" is the AFC variant
scoped to a single app's container.

**adb** - Android Debug Bridge. The tool MobFI drives for all
Android access.

**Bundle id / package name** - The app's unique identifier
(`com.example.target`). Called a package name on Android and a
bundle id on iOS; MobFI uses them interchangeably in flags.

**Container** - On iOS, the app's private directory tree. Reachable
over AFC only for dev-signed apps or on a jailbroken device.

**Debuggable** - An Android app built with
`android:debuggable="true"`. Readable with `run-as` on any device,
no root required.

**Extraction scope** - Which part of an iOS app MobFI pulls:
`container`, `documents`, or `backup`.

**Finding** - One secret match: a rule id, a file, a line, and a
redacted fingerprint.

**Fingerprint (redacted)** - The safe form of a matched secret:
first four characters plus total length.

**Keybag** - The iOS structure holding the class keys that protect
keychain items and backup files. Unlocked with the backup password.

**Keychain** - iOS credential store. Recoverable from an encrypted
backup, or on a jailbroken device.

**Keystore** - Android credential store. Blobs live under
`/data/misc/keystore`; root required.

**Protection class** - How strictly a keychain item is protected
(`WhenUnlocked`, `AfterFirstUnlock`, `...ThisDeviceOnly`).

**run-as** - The Android shell command that runs as a debuggable
app's own uid.

**Secure Enclave / StrongBox / TEE** - Hardware-backed key storage
on iOS and Android. Key material is non-exportable by design.

**Simulator vs emulator** - iOS *Simulators* run on macOS and keep
containers on the host filesystem. Android *emulators* are full
virtual devices reached through `adb`.

**Structural diff** - A semantic comparison (rows, fields) rather
than a byte comparison, applied to SQLite, JSON, and plists.

**udid** - Unique Device Identifier. The iOS equivalent of an adb
serial.

**usbmuxd** - The daemon multiplexing USB connections to iOS
devices. Required on Linux; provided by Apple Mobile Device Support
on Windows.

## Further reading

- **OWASP Mobile Application Security Testing Guide (MASTG)** -
  the reference methodology for mobile testing. MobFI covers the
  data-storage portions.
- **OWASP Mobile Top 10** - vulnerability classes to map findings
  onto in a report.
- **Apple Platform Security guide** - authoritative on Keychain
  protection classes, keybags, and the Secure Enclave.
- **Android Keystore documentation** - authoritative on security
  levels and what hardware backing does and does not guarantee.
- **libimobiledevice documentation** - the tools underneath every
  iOS workflow here.

## Handbook maintenance

This handbook is generated from per-chapter sources in
`docs/handbook/`. To change it, edit the chapter, not the generated
output:

```sh
$ $EDITOR docs/handbook/07-scanning.md
$ make handbook
```

The generated `docs/handbook.md` and `docs/handbook.pdf` are
regenerated automatically when you commit a chapter change, via the
repository's pre-commit hook. See `docs/handbook/README.md` for the
full workflow, including how to add screenshots and how to install
the PDF toolchain.
