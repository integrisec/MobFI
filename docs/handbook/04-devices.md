# Devices

Everything starts with a device MobFI can see. This chapter covers
detection, the connection methods for each platform, what each
reported state means, and how to fix a device that does not appear.

## Detecting

```sh
$ mfi detect
```

```
ID              NAME             PLATFORM  TRANSPORT  STATE
emulator-5554   Pixel_7_API_34   android   emulator   ready
39GH2A0MC1      SM-G991B         android   usb        ready
00008030-0011   Test iPhone      ios       usb        ready
```

MobFI runs three detectors and merges the results:

| Detector | Finds | Requires |
|---|---|---|
| adb | Android over USB, emulator, adb-over-TCP | `adb` |
| libimobiledevice | iOS over USB and network | `idevice_id`, `ideviceinfo` |
| simctl | iOS Simulators (macOS) | `xcrun` |

A detector that fails does not suppress the others. If `adb` is
missing, iOS devices still list, and the error naming the failed
detector is reported alongside the devices that were found. This is
deliberate: a partial answer beats no answer.

In the GUI, the Devices tab polls on a timer, so plugging a cable
updates the list without a manual refresh.

## Columns

| Column | Meaning |
|---|---|
| `ID` | adb serial (Android) or UDID (iOS). This is what you pass to `-device` |
| `NAME` | Human-friendly label reported by the device |
| `PLATFORM` | `android` or `ios` |
| `TRANSPORT` | `usb`, `tcp`, `emulator`, or `simulator` |
| `STATE` | Readiness, see below |

## States

| State | Meaning | What to do |
|---|---|---|
| `ready` | Connected and authorised. Extraction can proceed | Nothing |
| `unauthorized` | Android: the USB debugging prompt has not been accepted | Unlock the device, accept the prompt, tick "always allow" |
| `offline` | adb sees the device but cannot talk to it | Replug, or `adb kill-server && adb start-server` |
| `unpaired` | iOS: the host is not trusted by the device | Unlock the device, replug, tap "Trust", enter the passcode |

MobFI reports adb's raw `device` state as `ready`, because "device"
as a state next to a column called PLATFORM reads as noise.

## Android connection methods

### USB

The default. Enable Developer Options and USB debugging on the
handset, plug it in, and accept the prompt.

```sh
$ mfi detect
```

If the state is `unauthorized`, the prompt was dismissed or never
appeared. Unlock the screen and replug. If the prompt still does not
appear, revoke existing authorisations on the device (Developer
Options, "Revoke USB debugging authorisations") and replug.

### Emulator

An Android emulator registers with `adb` automatically. It shows up
with transport `emulator` and an `emulator-NNNN` serial. Emulators
are the easiest way to practise: everything is debuggable and
rooting is a non-issue.

### adb over TCP (wireless debugging)

Two distinct steps on Android 11 and later, and people routinely
conflate them.

**Pairing** happens once, using the pairing dialog's host:port and
its six-digit code:

```sh
$ adb pair 192.168.1.42:37129 123456
```

In the GUI, use the Devices tab's Pair control.

**Connecting** uses a *different* port, shown on the Wireless
debugging screen itself:

```sh
$ adb connect 192.168.1.42:5555
$ mfi detect
```

In the GUI, use the Connect control.

Gotchas that account for most wireless-debugging failures:

- **The pairing values expire fast** and change every time the
  dialog is reopened. Read them and type them promptly.
- **The pairing port is not the connect port.** Pairing succeeds,
  then `adb connect` to the same port fails. Use the port from the
  main Wireless debugging screen.
- **Same network, no client isolation.** Guest Wi-Fi and many
  corporate SSIDs block client-to-client traffic.
- **VPNs hijack RFC1918 routes.** If a VPN captures the route to
  the phone's address, the connection silently fails. Check with
  `route -n get <ip>` on macOS or `ip route get <ip>` on Linux.

**OPSEC**: adb over TCP exposes an authenticated debugging channel
on the local network for as long as it is enabled. Turn wireless
debugging off on the device when you are done.

## iOS connection methods

### USB

Pair and trust first:

1. Unlock the device.
2. Plug it in.
3. Tap **Trust** on the "Trust This Computer?" prompt.
4. Enter the device passcode.

Then:

```sh
$ mfi detect
$ ideviceinfo -u <udid> | head       # sanity check libimobiledevice itself
```

A device in state `unpaired` has not completed that flow. Replug
with the screen unlocked. If the prompt never appears, reset the
trust relationship on the device (Settings, General, Transfer or
Reset, Reset Location & Privacy) and replug.

On Linux, `usbmuxd` must be running. On Windows, the equivalent
comes from Apple Mobile Device Support, which
`scripts\install.ps1` installs via winget.

### Network

libimobiledevice can reach a paired device over the network when
the device has network pairing enabled. It appears with transport
`tcp`. Pair over USB first; network detection reuses the existing
pairing record.

### Simulator (macOS only)

Booted iOS Simulators are detected through `xcrun simctl` and appear
with transport `simulator`. Simulators are the friendliest iOS
target: the container lives on your host filesystem, so MobFI copies
it directly instead of going through AFC, and every app is
reachable.

```sh
$ xcrun simctl list devices booted   # confirm a simulator is running
$ mfi detect
```

If simulator detection fails with an `xcrun` error, `xcode-select`
is probably pointing at the Command Line Tools rather than a full
Xcode. MobFI works around this by looking for `simctl` inside
`/Applications/Xcode.app` (and `Xcode-beta.app`) directly, but a
correct `xcode-select -s` is the cleaner fix.

## No devices detected

Work through this in order.

1. **Is the tool installed?** `mfi doctor`. No `adb` means no
   Android devices, ever.
2. **Does the underlying tool see it?** This separates a MobFI
   problem from an environment problem:

   ```sh
   $ adb devices -l          # Android
   $ idevice_id -l           # iOS
   $ xcrun simctl list devices booted   # Simulator
   ```

   If the native tool does not see the device, MobFI cannot either.
   Fix the environment first.
3. **Is the screen unlocked?** Both platforms gate authorisation
   prompts behind an unlocked screen.
4. **Is it a charge-only cable?** A surprising number of cables
   carry power but not data. Swap it.
5. **Restart the daemon.** `adb kill-server && adb start-server`.
   On Linux, `sudo systemctl restart usbmuxd` for iOS.
6. **Check udev rules** on Linux for Android: without them, `adb`
   sees the device as `no permissions`.

## Choosing a device for a workflow

Once detected, pass the `ID` column to any command that needs a
device:

```sh
$ mfi apps    -device <id>
$ mfi extract -device <id> -app <bundle-id> -out <dir>
$ mfi keys    -device <id> -platform android -state rooted
```

The ID must match exactly. `mfi` resolves it against a fresh
detection pass and errors with "device not found; run `mfi detect`"
if it cannot.

## Next

- Apps: enumerate what is installed and pick a target.
- Extraction: pull the target's data.
