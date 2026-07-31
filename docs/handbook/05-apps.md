# Enumerating apps

Before extracting, you need the target's exact bundle id. A wrong
or approximate id is the second most common cause of an empty
extraction (after device state).

## Listing

```sh
$ mfi apps -device <device-id>
```

```
BUNDLE ID             NAME             VERSION  DATA PATH                      INSTALL PATH
com.example.target    Example Target   1.4.2    /data/data/com.example.target  /data/app/~~ab12../base.apk
com.example.other     Other App        0.9.1    /data/data/com.example.other   /data/app/~~cd34../base.apk
```

By default only user-installed apps are listed. Include the system
apps too:

```sh
$ mfi apps -device <device-id> -all
```

System apps are excluded by default because a stock handset lists
several hundred of them and the target is almost never among them.
Include them when you are auditing preinstalled software, chasing an
OEM component, or the app you want genuinely ships in the system
image.

## Columns

| Column | Android source | iOS source |
|---|---|---|
| `BUNDLE ID` | Package name from the package manager | `CFBundleIdentifier` |
| `NAME` | Application label | `CFBundleDisplayName` |
| `VERSION` | `versionName` | `CFBundleShortVersionString` |
| `DATA PATH` | `/data/data/<pkg>` | App container path |
| `INSTALL PATH` | APK path under `/data/app` | Bundle path |

`DATA PATH` is what extraction targets. `INSTALL PATH` points at the
installed binary/APK, which is useful when you want to pull the app
package itself rather than its data.

## Finding the target

The bundle id is rarely the app's display name. Filter the list:

```sh
# Android: everything from one vendor
$ mfi apps -device <id> | grep -i acme

# By display name when you only know the marketing name
$ mfi apps -device <id> | grep -i "mobile banking"
```

If the app is not in the default list, try `-all`. If it is still
absent, it is not installed on that device, or the platform tool
cannot enumerate it (see Troubleshooting).

## In the GUI

The Apps tab adds interactive affordances the CLI cannot:

- A **search box** filtering by bundle id or name as you type.
- An **Include system apps** checkbox, matching `-all`.
- **Sortable and resizable columns**.
- **Per-row Copy** for the bundle id.
- **Real app icons, names, and versions**, resolved lazily from each
  APK with `aapt` (from the Android SDK build-tools). Without
  `aapt`, the GUI falls back to a monogram avatar and a name derived
  from the bundle id. This is cosmetic: extraction does not need
  `aapt`.

Clicking a row opens a details panel.

**Android** (from `dumpsys package`): version, SDK levels, ABI,
first-install and last-update timestamps, on-disk sizes, package
flags, APK signing version, data and code paths, and the full
permission list.

**iOS** (from `ideviceinstaller`): version, application type, minimum
iOS version, signer identity, paths, and entitlements.

The permission and entitlement lists are the fastest way to judge
whether an app is worth extracting: an app holding
`READ_EXTERNAL_STORAGE`, `ACCESS_FINE_LOCATION`, and a keychain
sharing entitlement is a richer target than a static utility.

<!-- screenshot: apps-tab-details.png -->

## What the app type tells you

On iOS the details panel's application type decides whether AFC
extraction will work at all:

| Type | Meaning | Container reachable over AFC? |
|---|---|---|
| `User` | App Store or enterprise-signed | No, unless file sharing is enabled |
| `Developer` | Dev-signed / debug build | Yes |
| `System` | Apple system app | No |

On Android the equivalent signal is the `pkgFlags` list in the
details panel: a package carrying `DEBUGGABLE` can be read with
`run-as` on any device, no root required.

Both are covered in detail in the Extraction chapter.

## Next

Extraction: pull the target's private data.
