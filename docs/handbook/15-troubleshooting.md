# Troubleshooting

Symptoms, causes, and fixes. Work top-down: the ordering roughly
matches how often each cause turns out to be the real one.

## No devices detected

**1. The platform tool is missing.**

```sh
$ mfi doctor
```

No `adb` means no Android devices, ever. No `idevice_id` means no
iOS devices.

**2. The platform tool cannot see it either.** This separates a
MobFI problem from an environment problem:

```sh
$ adb devices -l
$ idevice_id -l
$ xcrun simctl list devices booted
```

If the native tool does not list the device, fix that first. MobFI
cannot see what its tools cannot see.

**3. The screen is locked.** Both platforms gate authorisation
prompts behind an unlocked screen.

**4. The cable is charge-only.** Swap it.

**5. The daemon is wedged.**

```sh
$ adb kill-server && adb start-server
$ sudo systemctl restart usbmuxd        # Linux, iOS
```

**6. Linux udev rules are missing** for Android. Without them `adb`
reports `no permissions`.

## Device shows `unauthorized` (Android)

The USB debugging prompt was never accepted, or the host key was
revoked.

1. Unlock the device.
2. Replug.
3. Accept the prompt, ticking "always allow from this computer".

If no prompt appears: Developer Options, "Revoke USB debugging
authorisations", then replug.

## Device shows `unpaired` (iOS)

The host is not trusted by the device.

1. Unlock the device.
2. Replug.
3. Tap **Trust**, enter the passcode.

If no prompt appears: Settings, General, Transfer or Reset iPhone,
Reset, Reset Location & Privacy, then replug.

## Extraction returns 0 files

The single most common issue. In order of likelihood:

**Wrong bundle id.** Copy it exactly from `mfi apps`. A near-miss
(`com.example.app` vs `com.example.app.debug`) produces exactly this
symptom.

**App is not debuggable and the device is not rooted** (Android).
`run-as` refuses, `su` is unavailable, the shell user has no access.
Check the Apps tab details panel for a `DEBUGGABLE` package flag.
Options: use a debuggable build, root the device, or accept that
private data is out of reach.

**Wrong iOS scope.** `container` only works for dev-signed and
jailbroken. Try `-scope documents`, then `-scope backup`.

**App is not installed on that device.** Verify with `mfi apps`.

## Extraction is very slow

**Per-file fallback is in use.** The device has no `tar`, so MobFI
copies file by file. Nothing to configure; it is slower by nature.

**The `backup` scope is running.** A full device backup precedes
reconstruction and can take tens of minutes on a device with media.
This is expected.

**A huge cache directory.** Some apps cache hundreds of megabytes of
images. The capture includes them.

## `cannot read /data/data/<pkg>`

The full message names the two likely causes, and it is usually
right:

```
check the package name is exactly right and the app is installed;
a non-debuggable app needs root (approve the su/superuser prompt
on the device)
```

Check the package name character by character, then watch the
device screen during the extract: a superuser prompt waiting for
approval will block the attempt silently from the host's side.

## iOS container extraction denied

```
(full container access needs a dev-signed/debug app or a jailbreak;
try the 'documents' scope, or 'backup' scope to pull a production
app's data)
```

Follow the advice in the order given: `documents` is fast and often
sufficient; `backup` is slow but reaches production apps.

## Backup extraction fails or hangs

**Not enough disk space.** A full backup needs room for the entire
device's backed-up content, not just the target app.

**The device locked mid-backup.** Keep the device unlocked and
plugged in for the duration.

**A backup password is set but not supplied.** For keychain
recovery, the password is required (see the Keys chapter). For file
extraction, an encrypted backup still works, but decrypting the
manifest requires the password.

**`idevicebackup2` is missing.** It is optional in `mfi doctor`
precisely because only this scope needs it. Install libimobiledevice
in full.

## Scan finds nothing

**The data is binary.** The scanner skips files whose first 512
bytes contain a NUL. Secrets inside compiled binaries, images, or
compressed archives are not found. Extract strings yourself and scan
that output.

**Files exceed the 16 MB cap.** Larger files are skipped entirely.

**The extraction was empty or partial.** Check the extract's file
count first: scanning an empty tree finds nothing, correctly.

**The app genuinely stores nothing in plaintext.** This is a good
result, and worth reporting as one. Confirm it by checking whether
the app uses the platform credential store instead (see the Keys
chapter).

## Scan finds far too much

Generic rules (`generic-secret-assignment`, `bearer-token`) are
broad by design. Filter to the high-confidence rules for a first
pass:

```sh
$ mfi scan -root ./cap | grep -vE 'generic-secret-assignment|bearer-token'
```

Then triage the generics separately.

## Verification reports `unknown`

The check could not complete. Causes: no network, the provider's API
is unreachable, a proxy is interfering, or the provider changed its
endpoint. `unknown` is not evidence that the credential is dead.

## Keys reports `unavailable`

Expected on stock devices. The limitations in the output name the
route that would work:

- **iOS, non-jailbroken**: make an **encrypted** backup, then point
  `mfi keys -backup <dir> -password <pw>` at it.
- **Android, not rooted**: no route exists; the keystore is
  root-only.

## Backup keychain decryption fails

**The backup is not encrypted.** Only encrypted backups contain the
keychain. Enable backup encryption on the device and make a new one.

**Wrong password.** MobFI detects this as a keybag unlock failure
and reports "wrong backup password?" rather than producing garbage.

**Items are `ThisDeviceOnly`.** Those are excluded from backups by
iOS. Nothing recovers them from a backup, by design.

## GUI will not start

**Missing WebKit toolchain** (Linux). The install script installs
GTK3 and WebKit2GTK packages; if you built manually, install them.

**Missing WebView2** (Windows). Installed by the install script via
winget.

**Blank window on Linux.** A known WebKit issue with some GPU
drivers. Try `WEBKIT_DISABLE_COMPOSITING_MODE=1 mfi-gui`.

## GUI shows no app icons

`aapt` is missing. Cosmetic only: extraction, scanning, and
everything else work identically. Install the Android SDK
build-tools if you want real icons and names.

## Console will not connect (iOS)

**The device is not jailbroken.** Stock iOS has no SSH server.

**`sshd` is not running** on the jailbroken device, or listens on a
non-default port. Set the port in the Console tab.

**`iproxy` is missing.** It is optional in `mfi doctor`; install
`libusbmuxd` (macOS) or `libusbmuxd-tools` (Linux).

## adb over TCP will not connect

**Pairing port used as the connect port.** They differ. Pair with
the dialog's host:port, then connect with the port from the main
Wireless debugging screen.

**Values expired.** They rotate every time the pairing dialog is
reopened. Read and use them promptly.

**Client isolation on the network.** Common on guest and corporate
Wi-Fi. Use a network without it, or USB.

**A VPN captured the route.** Check with `route -n get <ip>`
(macOS) or `ip route get <ip>` (Linux).

## Filing a bug

Include:

- `mfi version` output.
- `mfi doctor` output.
- The exact command and its full output.
- Device platform, OS version, and state (rooted, jailbroken,
  stock).
- Whether the underlying tool (`adb`, `idevice_id`) sees the device.

Never include raw captured data, raw secrets, or an unredacted
report in a bug report.
