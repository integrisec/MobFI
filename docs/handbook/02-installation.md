# Installation

MobFI itself is a single static binary. What takes setup is the
external tooling it drives: `adb` for Android, the
`libimobiledevice` suite for iOS. MobFI runs without them, but a
platform whose tools are missing simply reports no devices.

## What you need

| Component | Needed for | Notes |
|---|---|---|
| `mfi` binary | Everything | Prebuilt download, or `make build` |
| `adb` | All Android work | Android platform-tools |
| `idevice_id`, `ideviceinfo` | iOS device discovery | libimobiledevice |
| `afcclient` | iOS container/documents extraction | libimobiledevice |
| `ideviceinstaller` | iOS app listing | Separate package on some platforms |
| `idevicebackup2` | iOS backup extraction (optional) | Needed only for the `backup` scope |
| `iproxy`, `ssh` | iOS Console over USB (optional) | Jailbroken devices |
| `aapt` | Real Android icons/names in the GUI (optional) | Android SDK build-tools |
| `xcrun`, `plutil` | iOS Simulator support (macOS only) | Ships with Xcode / macOS |
| Go 1.23+ | Building from source | Not needed for prebuilt CLI |
| Wails CLI + Node + C toolchain | Building the GUI | See the GUI section |

Nothing here is mandatory up front. Install the tools for the
platform you are actually testing, then run `mfi doctor` to confirm.

## Option 1: prebuilt CLI (fastest)

Download the binary for your platform from the
[latest release](https://github.com/integrisec/MobFI/releases/latest).
Binaries are cgo-free and version-stamped.

```sh
# macOS / Linux
$ chmod +x mfi_v1.0.0_darwin_arm64
$ shasum -a 256 -c SHA256SUMS.txt --ignore-missing
$ ./mfi_v1.0.0_darwin_arm64 version
```

Verify the checksum before running it. `SHA256SUMS.txt` is published
on the same release page.

On macOS, Gatekeeper quarantines downloaded binaries. Clear it:

```sh
$ xattr -d com.apple.quarantine mfi_v1.0.0_darwin_arm64
```

Rename it to `mfi` and put it somewhere on your `PATH`:

```sh
$ mkdir -p ~/.local/bin
$ mv mfi_v1.0.0_darwin_arm64 ~/.local/bin/mfi
```

The desktop GUI is not shipped prebuilt: it needs per-OS cgo and
WebKit toolchains. Build it with the install script below.

## Option 2: install script (recommended for a full setup)

One command resolves everything (Go toolchain, Wails CLI and its
native GUI toolchain, `adb`, `libimobiledevice`) and builds both the
CLI and the GUI. Re-running it is safe; anything already present is
skipped. It puts `mfi` on your `PATH`.

```sh
$ git clone https://github.com/integrisec/MobFI.git
$ cd MobFI

# macOS / Linux
$ ./scripts/install.sh          # or: make setup

# Windows (PowerShell)
$ powershell -ExecutionPolicy Bypass -File scripts\install.ps1
```

Useful flags on both scripts:

| Flag (sh) | Flag (ps1) | Effect |
|---|---|---|
| `--cli-only` | `-CliOnly` | Skip the GUI build |
| `--gui-only` | `-GuiOnly` | Skip the CLI build |
| `--no-runtime-tools` | `-NoRuntimeTools` | Do not install `adb` / libimobiledevice |
| `--launch cli\|gui` | `-Launch cli\|gui` | Run the app once built |
| (n/a) | `-NoShortcuts` | Skip Start Menu / Desktop shortcuts |

### Per-OS behaviour of the installer

**macOS** uses Homebrew (installing it if missing) and copies the
built `MobFI.app` into `/Applications`.

**Linux** auto-detects `apt` / `dnf` / `pacman` / `zypper` and
installs the GTK3 + WebKit2GTK build packages. If the system Go is
older than 1.23 it fetches a current one from go.dev.

**Windows** uses `winget` for Go, platform-tools (`adb`), and
WebView2, and bootstraps `scoop` to get `gcc` (Wails links WebView2
through cgo, so the GUI build needs a C compiler; the CLI is
cgo-free and does not). For iOS it downloads prebuilt
libimobiledevice binaries and installs Apple Mobile Device Support
via winget, which provides the `usbmuxd` service libimobiledevice
needs. `scoop` bootstrapping requires a **non-elevated** PowerShell.

## Option 3: build from source manually

```sh
$ make build            # compiles the CLI to ./bin/mfi
$ make test             # run the test suite
$ make vet              # go vet
```

Put it on your `PATH` by symlink, so a later `git pull` + rebuild is
picked up automatically:

```sh
$ mkdir -p ~/.local/bin && ln -sf "$PWD/bin/mfi" ~/.local/bin/mfi
```

### Building the GUI

In addition to Go, you need:

- The Wails CLI:
  `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- Node.js and a C toolchain / platform webview. On macOS that is the
  Xcode Command Line Tools.

Run `wails doctor` to check your toolchain, then build from
`cmd/mfi-gui`. The frontend under `frontend/dist` is vanilla
HTML/JS/CSS and needs no npm build step.

## Installing the device tools

If you skipped the installer or want to add a platform later:

### Android (`adb`)

```sh
# macOS
$ brew install --cask android-platform-tools

# Debian / Ubuntu
$ sudo apt install android-tools-adb

# Windows
$ winget install Google.PlatformTools
```

### iOS (libimobiledevice)

```sh
# macOS
$ brew install libimobiledevice
$ brew install ideviceinstaller

# Debian / Ubuntu
$ sudo apt install libimobiledevice-utils
$ sudo apt install ideviceinstaller
```

On Windows there is no winget or scoop package for the suite. Run
`scripts\install.ps1`, which pulls the core tools (`idevice_id`,
`ideviceinfo`, `afcclient`) from the
[jrjr/libimobiledevice-windows](https://github.com/jrjr/libimobiledevice-windows/releases)
release bundle and `ideviceinstaller` from the
[imobiledevice-net](https://github.com/libimobiledevice-win32/imobiledevice-net/releases)
`win-x64` bundle. If a download fails, fetch the zip manually,
extract it, and add the folder to your `PATH`.

### Optional extras

```sh
# iOS Console over USB (jailbroken devices)
$ brew install libusbmuxd            # macOS: provides iproxy
$ sudo apt install libusbmuxd-tools  # Debian/Ubuntu

# Real Android app icons and names in the GUI
$ brew install --cask android-commandlinetools
$ sdkmanager "build-tools;34.0.0"
```

## Verify the install

```sh
$ mfi version
$ mfi doctor
```

`mfi doctor` prints a table of every tool MobFI knows how to use,
whether it was found, where, and how to install it if not:

```
MobFI dependency check (darwin/arm64)

STATUS   TOOL              PURPOSE                                        LOCATION / INSTALL
ok       adb               Android device access (detect, extract, ...)   /opt/homebrew/bin/adb
ok       idevice_id        iOS device discovery (libimobiledevice)        /opt/homebrew/bin/idevice_id
MISSING  ideviceinstaller  iOS installed-app listing                      -> brew install ideviceinstaller
optional idevicebackup2    iOS backup-based extraction                    -> brew install libimobiledevice
```

`MISSING` marks a core tool: some platform workflow is unavailable
without it. `optional` marks a nice-to-have whose absence disables
one feature, not a platform.

Machine-readable form for scripts:

```sh
$ mfi doctor -json
```

## Windows on ARM64

Two constraints worth knowing before you plan an engagement around
an ARM Windows host:

- scoop's `gcc` is x86-64, so the installer builds the GUI as
  `windows/amd64` and it runs under the OS's x64 emulation. The
  cgo-free CLI builds natively as arm64.
- **iOS over USB does not work.** Apple's Mobile Device USB driver
  is x64 kernel-mode, and Windows on ARM cannot load x64 kernel
  drivers, so iPhones may not enumerate even with the service
  installed. Use a Mac or an x64 host for iOS. Android is fine.

## Next

Continue to First Run for an end-to-end capture.
