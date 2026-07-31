# Keychain and Keystore

Apps that handle credentials properly do not leave them in
`shared_prefs` or a plist. They put them in the platform credential
store: the iOS Keychain or the Android Keystore. This chapter covers
what MobFI can recover from each, and the hard platform limits that
decide what is recoverable at all.

```sh
$ mfi keys -platform ios     -device <udid>  -state jailbroken
$ mfi keys -platform ios     -backup <dir>   -password <pw>
$ mfi keys -platform android -device <serial> -state rooted
```

| Flag | Meaning |
|---|---|
| `-platform` | `ios` or `android`. Inferred as `ios` when `-backup` is given |
| `-device` | UDID (iOS) or serial (Android) |
| `-state` | `jailbroken` (iOS) or `rooted` (Android) |
| `-backup` | iOS: path to an **encrypted** backup directory |
| `-password` | iOS: backup password (or set `MFI_BACKUP_PASSWORD`) |
| `-reveal` | Include raw secret values. Default is redacted |

## The hard limits

Read this before planning work around key recovery.

**iOS Secure Enclave keys are non-exportable.** Not "hard to
extract": the private key material never leaves the Enclave, by
design. MobFI can tell you a key exists and what it is used for. It
can never dump it.

**Android hardware-backed keys (TEE/StrongBox) are
non-exportable.** Same principle. Even with root, you get an
inventory of which keys exist, not the key material.

**iOS `ThisDeviceOnly` keychain items are excluded from backups**
by iOS itself, so the backup route cannot recover them regardless of
password.

If your goal is the plaintext key an app *uses* rather than the
stored key material, storage dumping is the wrong technique. Hook
the app at runtime with Frida or objection and capture the key at
the point of use.

## iOS: the two routes

MobFI picks the best available method and degrades gracefully. When
no method can run, it returns an explanation rather than an error,
so you always get actionable guidance.

### Encrypted backup (works on stock devices)

The only route that works on a normal, non-jailbroken iPhone.

1. **Enable backup encryption** on the device (Finder/iTunes, or via
   the device's own settings) and set a backup password. Backup
   encryption is what causes the keychain to be included at all: an
   unencrypted backup contains no keychain.
2. **Produce a backup.** MobFI's extraction `backup` scope does this
   (see the Extraction chapter), or use `idevicebackup2` directly.
3. **Point `mfi keys` at the backup directory** with the password.

```sh
$ export MFI_BACKUP_PASSWORD='the-backup-password'
$ mfi keys -backup ~/captures/backup-dir
```

The pipeline: parse `Manifest.plist`, unlock the backup keybag with
the password, decrypt `Manifest.db`, locate and decrypt the keychain
file, then parse its items. Every stage degrades and reports rather
than failing hard.

If the backup is not encrypted, MobFI says so plainly:

```
this backup is not encrypted; the keychain is only present in
encrypted backups (enable backup encryption and re-run)
```

### Jailbroken device over USB

On a jailbroken device running `sshd` with `keychain_dumper`
installed, MobFI dumps the keychain directly over a USB port
forward.

```sh
$ mfi keys -platform ios -device <udid> -state jailbroken
```

You must tell MobFI the device is jailbroken with `-state
jailbroken`. It does not probe for a jailbreak.

If both a backup and `-state jailbroken` are available, the backup
route is tried first, and the jailbreak route is used as a fallback
if backup decryption fails.

## Android: rooted device

```sh
$ mfi keys -platform android -device <serial> -state rooted
```

The Android Keystore's blobs live under `/data/misc/keystore`, owned
by root, so root is mandatory. MobFI reads the keystore2 database
(Android 12 and later) and reports the key entries with their
security levels: software, TEE, or StrongBox.

Interpreting the security level is the point of the exercise:

| Level | Meaning | Recoverable? |
|---|---|---|
| Software | Key material in software, protected by the keystore | Potentially |
| TEE | Key lives in the Trusted Execution Environment | Inventory only |
| StrongBox | Key lives in a dedicated secure element | Inventory only |

An app storing a signing key at software security level on a rooted
device is a finding. The same app on a device where the key is
StrongBox-backed is not.

## Reading the output

```
Method: encrypted backup
  note: Unlocked the backup keybag.
  note: Recovered 24 keychain item(s).
Items: 24
  [Generic Password] keychain service="com.example.target" account="session" accessible=AfterFirstUnlock value=[hidden text, 213 bytes]
  [Internet Password] keychain service="api.example.com" account="user@example.com" accessible=WhenUnlocked value=[hidden text, 32 bytes]
  [Key] keychain label="com.example.target.signing" accessible=AfterFirstUnlockThisDeviceOnly value=[hidden binary, 256 bytes]
  limitation: Items protected 'ThisDeviceOnly' and Secure Enclave keys are excluded from backups by iOS and cannot be recovered here.
```

| Field | Meaning |
|---|---|
| `Method` | Which route ran. `unavailable` means none could |
| `Degraded` | Marked when the method could not run at all |
| `note` | What happened during the dump |
| `limitation` | What this method structurally cannot reach |
| `accessible` | The item's protection class |

### Protection classes

The `accessible` field is often the finding, independent of the
value:

| Class | Available when | Risk signal |
|---|---|---|
| `Always` | Always, even locked | Deprecated by Apple, weakest |
| `AfterFirstUnlock` | After first unlock since boot | Readable by background access post-boot |
| `WhenUnlocked` | Only while unlocked | Reasonable default |
| `*ThisDeviceOnly` | As above, never leaves the device | Strongest, excluded from backups |

A session token stored `Always` or `AfterFirstUnlock` without the
`ThisDeviceOnly` suffix is reachable in more circumstances (and
survives into backups) than most developers intend.

## Revealing values

Values are redacted by default, shown as `[hidden text, N bytes]` or
`[hidden binary, N bytes]`. Add `-reveal` to include them:

```sh
$ mfi keys -backup ~/captures/backup-dir -reveal
```

Binary values print as a hex preview, capped at 512 bytes.

**Treat revealed output as raw credential material.** Do not paste
it into a ticket, do not commit it, and do not leave it in shell
history. On a shared host, prefer redirecting to a file with tight
permissions over letting it scroll in a terminal that may be logged.

In the GUI, the Keys tab has a **Reveal secrets** checkbox, gated
behind a confirmation dialog, and the setting persists across dumps
so you do not have to re-tick it every time.

<!-- screenshot: keys-tab-items.png -->

## When nothing is recoverable

The degraded result tells you exactly what to change:

**iOS, non-jailbroken, no backup**: make an encrypted backup and
re-run against it.

**Android, not rooted**: no route exists. The keystore is root-only.
Consider runtime hooking instead.

Neither is a MobFI failure; both are the platform working as
designed.

## Next

- Reporting: aggregate findings into a shareable artifact.
- Console: interactive shell access for follow-up.
