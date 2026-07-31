# Changelog

All notable changes to MobFI are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Add new changes under `## [Unreleased]` as you go; on release, rename that
heading to the version and date and start a fresh `Unreleased` section.

## [Unreleased]

### Added
- Table rows now use a **primary button + "…" overflow menu** (and a **right-click**
  menu on the row) for their actions, instead of a growing row of buttons -- so
  the action column stays compact no matter how many device-scoped features are
  added (Devices/Apps/Scan/Diff/Keys). The webview's default context menu is
  suppressed except in text fields, the terminal, and when text is selected (so
  right-click "Copy" still works on rendered/output text).
- **Keychain / Keystore dump** (new **Keys** tab and `mfi keys` command): recover
  as much as the device state allows from the platform credential store, degrading
  gracefully. iOS methods: decrypt the keychain from an **encrypted iTunes/Finder
  backup** on a stock, non-jailbroken device (parses the backup keybag, derives the
  passcode key, and unwraps the class/file/item keys — the AES key-unwrap and
  keybag handling are unit-tested against RFC 3394 vectors), or run
  `keychain_dumper` over **SSH-over-USB** on a jailbroken device. Android (rooted):
  inventory the legacy `/data/misc/keystore` blobs and parse the modern
  **keystore2** database (`persistent.sqlite`, Android 12+) -- listing each key's
  owning app, alias, and security level (TEE / StrongBox / software), applying a
  hot WAL if present. Secret values are redacted
  unless **Reveal** is set (behind a confirmation). Honest about hard limits —
  iOS Secure Enclave and Android hardware-backed private keys are non-exportable by
  design and are inventoried, never dumped — and every path reports what it could
  and couldn't recover, and why.
- **String decoders** for inspecting encoded values found in files, database
  cells, or secret findings: **Base64** (standard and URL-safe, padded or raw),
  **ASCII hex** (tolerates spaces, `0x`, and `:`/`-` separators), and **URL**
  percent-encoding. Available as a new GUI **Decode** tab (decodes as you type;
  binary results also show a hex view) and the `mfi decode` CLI command (reads
  from an argument, `-input`, or stdin). Scan findings gained a **Decode** action
  that sends the value straight to the Decode tab. Decoded output is
  **syntax-detected and highlighted** (e.g. a decoded JWT/JSON payload), with a
  **Prettify** toggle to reformat JSON/XML and a larger input box. Prettify now
  defaults on across the Decode and Render tabs.
- **Find in a rendered file**: the Render tab has a search box that highlights
  every (case-insensitive) match, shows a live "current / total" count, and lets
  you step through matches (Enter / Shift+Enter or the up/down buttons, Esc to
  clear; Cmd/Ctrl+F focuses the box). Works on the text, hex, and
  syntax-highlighted code views; the active match is emphasised and scrolled
  into view.
- The installers put the CLI on `PATH` so you can just type **`mfi`** from
  anywhere: `install.sh` symlinks the built binary into `~/.local/bin` (falling
  back to a user-writable `/usr/local/bin` when it is already on `PATH`) and
  `install.ps1` adds the repo's `bin` to the user `PATH`. Both avoid elevation
  entirely -- the install no longer prompts for a password to link the CLI. Both
  point at the built binary, so a later `git pull` + rebuild or `mfi update` is
  reflected automatically.
- Scan findings gained a **Render** action that opens the file in the Render
  tab with the matched secret **highlighted** and scrolled into view (works for
  token-style matches and plain-text files; a multi-token generic match in a
  syntax-highlighted file still opens the file at that location).
- The Scan tab's **Verify live** and the Keys tab's **Reveal secrets** checkboxes
  now persist across sessions.
- Opt-in **live verification** of secret findings: for supported services
  (GitHub, GitLab, npm, OpenAI, Anthropic, Hugging Face, SendGrid, DigitalOcean,
  Stripe, Slack, Postman, Notion, Airtable) MobFI calls a read-only "whoami"
  endpoint with the matched secret to confirm whether it is still **active**,
  **inactive**, or **unknown**. Enabled with `mfi scan -verify` /
  `mfi report -verify` on the CLI and a **Verify live** checkbox (behind a
  confirmation) on the GUI Scan tab; the status shows in the table and reports.
  Off by default -- it makes authenticated network calls that send the secret to
  its service. Identical secrets are checked once, concurrently and time-boxed.
- Expanded the built-in secret detectors from 9 to ~45 Trufflehog-style rules,
  each anchored on a service's distinctive prefix/format for precision: cloud/CI
  (AWS, GCP incl. OAuth client secret + service-account key, DigitalOcean,
  Databricks, Terraform, Doppler), version control / packages (GitHub, GitLab,
  npm, PyPI, Postman), AI providers (OpenAI, Anthropic, Hugging Face), payments
  (Stripe, Square, Braintree), communication/email (Slack incl. app tokens &
  webhooks, Discord bot tokens & webhooks, Telegram, Twilio, SendGrid, Mailgun,
  Mailchimp), SaaS APIs (Shopify, Notion, Linear, Airtable, New Relic, Grafana),
  JWTs, PEM private keys, and connection strings / URLs with embedded
  credentials (MongoDB, SQL, Redis, HTTP basic-auth), plus broader keyword and
  bearer-token generics. Each rule is covered by a positive-sample test.

### Changed
- GUI polish: the Decode tab's empty "no result" cards no longer stretch to the
  height of a decoded value; the Scan/Diff toolbars group each input with its
  Browse button and right-align the report/export controls so they wrap cleanly;
  and the data tables fill down to the window's bottom as a framed pane instead
  of a short strip with empty space below.
- The GUI now remembers its **fullscreen state** across launches, in addition to
  the window size and position (all three on macOS, Windows, and Linux). While
  fullscreen it preserves the last windowed size/position, so leaving fullscreen
  returns to the right geometry.
- The Keys table is now sortable by any column (Class/Service/Account/Accessible/
  Value), with the sort preference persisted -- matching the Scan and Diff tables.
- The desktop GUI now checks for updates **in the background while running** --
  every few hours and when the window regains focus -- not only at launch, so a
  release published mid-session surfaces the update banner without a restart.
  Dismissing the banner stays quiet until a newer version appears.

### Fixed
- The GUI logo no longer intermittently fails to appear at launch. The bundled
  logo PNGs were oversized (~860 KB, 1448x1086) for their on-screen size, so they
  sometimes hadn't decoded by first paint; they're now downscaled (logo to
  800px/~130 KB, favicon to 64px/~4 KB) and preloaded, so the header/splash/hero
  logo shows reliably.
- Windows in-place update now **shows its progress**. Because a running `.exe`
  can't be overwritten, MobFI closes and a detached worker performs the update;
  previously that worker was silent (the update modal flashed for a moment, the
  app vanished, and it reopened ~45s later with no output). The worker now opens
  its own **"MobFI updater" console window** that streams the git pull + rebuild
  output, and the app shows a clear "closing to update -- a progress window will
  open, MobFI reopens automatically" message (held ~3s) before it closes.
- SQLite databases now always open as databases in the Render tab -- detected by
  their file header regardless of extension (`.sqlite`, `.db`, or none) -- rather
  than falling back to a hex dump when the table summary can't be produced (e.g.
  a locked or unusual database). Such a file is shown with the "Open in Database"
  action so it can still be browsed. `dbview` also now falls back from an
  `immutable` open to plain read-only, so databases carrying a hot WAL open too.
- GUI scrollable panes (the Devices/Scan/Diff/Apps/Database tables and the
  Render/output panes) now show always-visible scrollbars on both axes. macOS
  hides overlay scrollbars until you actively scroll, so they had looked absent;
  the tables also gained a bounded height so they scroll vertically within their
  own box (matching the Apps/Database tables) instead of the whole page.
- The GUI update overlay now renders the rebuild output's ANSI colour codes as
  actual colours (git/`wails`/pterm output), instead of showing raw escape
  sequences. Non-colour terminal control codes (cursor moves, spinner redraws)
  are stripped so they no longer appear as gibberish.
- GUI tables (Scan/Diff/Apps/Devices) no longer overflow the window: each grid
  now lives in a horizontally-scrollable frame, so a wide row scrolls within the
  table instead of shifting the whole page (which had clipped the tab title and
  pushed the row's **Copy** button against the window edge). The action column
  (Render/Copy) is pinned to the right so its buttons stay reachable while the
  row scrolls, and with **Wrap** on, long paths reflow to fit.

## [1.0.0] - 2026-07-25

First official release. MobFI is a cross-platform (macOS, Windows, Linux)
mobile-forensics and secrets-inspection tool for exploring the file structures
of installed Android and iOS apps, available as both a desktop GUI and a CLI
built on a single shared Go core.

### Device discovery & access
- Detect active Android devices (adb) and iOS devices (libimobiledevice) across
  USB and TCP, plus booted iOS Simulators (simctl).
- Manual TCP connect and Android 11+ wireless `adb pair` from the GUI.
- Console tab: interactive `adb shell` (Android) and SSH-over-USB (iOS, auto
  `iproxy` forwarding) in a real PTY, with history, copy/paste, logging, and
  font-size controls.

### App targeting & extraction
- Enumerate installed apps (`adb pm list packages` / `ideviceinstaller`), with
  real app icon/name/version in the GUI.
- Android extraction reads `/data/data/<pkg>`: `run-as` for debuggable apps,
  falling back to `su` on rooted devices, then a plain shell; prefers a single
  tar stream and falls back to per-file walking.
- iOS extraction via AFC house arrest with a selectable scope: `container`,
  `documents`, or `backup` (full-device backup + `Manifest.db` reconstruction
  to reach a production App Store app's private data on a non-jailbroken
  device). The `backup` scope runs a space pre-flight -- it estimates the
  backup size from the device's used data (`ideviceinfo com.apple.disk_usage`)
  and checks the destination's free space, failing fast with a clear message
  instead of dying partway through a multi-GB backup. It reports **overall**
  backup progress as the actual bytes written to disk. `idevicebackup2` reports
  only per-file progress (no reliable whole-backup percentage), and the device's
  used-data figure understates the true backup size, so MobFI shows "X.X GB
  backed up so far" rather than a misleading total/percentage (it will still use
  a real overall percentage on the rare versions that print one). The file/byte
  header is hidden until reconstruction produces files. Reconstruction lists the
  backup domains it extracts (app container plus app groups / extensions), and
  reports skipped entries clearly (e.g. "symlink (not extracted)"). Cancel is
  responsive: the click shows "Cancelling…" immediately, the progress poller
  stops the moment the context is cancelled, and the kill wait is bounded so it
  doesn't appear frozen before the clean-up prompt. The Extract Device ID field
  is wide enough to show a full iOS UDID without visually cropping it.
- Live progress with cancellation; destination path-traversal guards and
  Windows-safe filename handling.

### Inspection & analysis
- Secrets scanning: Trufflehog-style built-in rules plus a user-supplied
  known-secret list, with redacted findings.
- Native semantic diffing of two or more captures (e.g. logged-in vs.
  logged-out): tree + content diff with structural differs for SQLite
  (row-level), JSON and plist (field-level, across binary/XML plist forms).
- Read-only, immutable SQLite database viewing.
- Native rendering per file type: JSON, XML, binary + XML plists, SQLite
  summaries, text, hex, images, and PDFs, with syntax highlighting.
- Reporting: aggregate scan + diff results into text, JSON, and HTML exports.
  Secrets are redacted by default; an opt-in (`mfi report -show-secrets` / the
  GUI's **Unredacted** checkbox, behind a confirm prompt) includes raw values
  for authorized local analysis, with a plaintext warning in the output.

### Frontends
- Guided wizard by default with an advanced path, in both the GUI and the CLI.
- Wails desktop GUI (vanilla HTML/JS/CSS frontend over the Go core) with
  persisted window geometry and toggles, sortable tables, side-by-side file
  diff, and a global Dependencies panel. Device detection is resilient -- one
  failing detector never blanks the list.
- `mfi doctor` reports the presence of the runtime device tools.
- Android wireless-debugging pairing reports failures correctly (a non-zero
  exit or `error:`/`fault` output is now surfaced as an error, not a green
  success toast) with a hint that the pairing host:port/code expire quickly and
  that cloud devices (e.g. Corellium) connect via adb-over-TCP rather than pair.
- Launch update check with in-place update: detects a newer GitHub release
  (works for prebuilt binaries) and, in a git checkout, how many commits behind
  upstream. The GUI shows a dismissable banner and the CLI prints a notice /
  `mfi update`. Updating in place is a click (**Update now**) or
  `mfi update -apply`: a source checkout runs `git pull` + rebuild, a prebuilt
  binary downloads the release asset, verifies its SHA-256, and swaps itself.
  The GUI **Update now** shows a live progress overlay: on macOS/Linux the
  update runs in-process (streaming the git pull + rebuild output) with the
  window open, then relaunches; on Windows -- where a running `.exe` can't be
  overwritten -- it delegates to a detached worker after a "closing to update"
  message. The installer records the source-checkout path so this works even
  when the app runs from `/Applications` or a shortcut.
  On the CLI, the guided **wizard** now offers the update interactively when
  stdin is a terminal: it prompts "Update now? [y/N]", applies it with live
  progress, and re-execs the freshly-built binary so the wizard continues on the
  new version. One-shot subcommands stay non-interactive -- they just print a
  one-line "update available" notice to stderr afterward.
  `MFI_NO_UPDATE_CHECK=1` silences the launch check. GUI confirmations (Update
  now, unredacted export) use the native dialog, since the Wails webview
  (WKWebView) does not implement `window.confirm()` -- previously "Update now"
  appeared to do nothing. The GUI update worker logs every step to
  `<config>/MobFI/update.log`, streams the rebuild output, always relaunches
  the app (even on a crash), and on macOS falls back to exec'ing the bundle's
  binary if `open` cannot relaunch it from the detached process. The relaunched
  app runs with the worker control vars stripped from its environment on every
  relaunch path (including the macOS `open` command, which can pass its env to
  the launched app), so it never restarts as another worker -- previously a
  relaunch loop spawned many app instances. A hard re-entry guard also aborts a
  worker that starts within seconds of a prior one, capping any future loop.
  Most importantly, the update **never runs without explicit user approval**:
  the worker requires a one-time approval token that the GUI writes only when
  the user clicks Update now and confirms, so a leaked env var or stray process
  can never trigger an update on its own. The worker pulls over the public HTTPS URL (not
  the configured SSH remote) and runs git non-interactively
  (`GIT_TERMINAL_PROMPT=0`, `ssh -o BatchMode=yes`), so it never hangs on an
  SSH key/host-key prompt it cannot answer -- the previous symptom where the app
  closed and never reopened.

### Packaging & tooling
- Cross-platform install scripts: `scripts/install.sh` (macOS/Linux) and
  `scripts/install.ps1` (Windows) that resolve Go, the Wails toolchain, adb,
  and libimobiledevice, then build both binaries.
- Windows-on-ARM support: emulated amd64 GUI build, scoop bootstrap, prebuilt
  libimobiledevice, and Apple Mobile Device Support for iOS-over-USB. The update
  check runs its git commands through the no-console-window wrapper so the GUI
  no longer flashes console windows at launch, and the installer reuses an
  existing scoop install instead of re-bootstrapping it.
- macOS: the GUI finds command-line tools (adb, libimobiledevice, ...)
  regardless of how it is launched. At runtime it merges the user's login-shell
  PATH and the standard Homebrew/MacPorts/toolchain locations (LaunchServices
  does not reliably honor the bundle's `LSEnvironment`), so a Finder-launched
  GUI resolves the same `adb` as the terminal -- including adb-over-TCP devices
  (e.g. Corellium). The installer also copies the app to `/Applications`, bakes
  an `LSEnvironment` PATH, and re-signs the bundle (ad-hoc) after the plist edit
  (editing `Info.plist` invalidates Wails' signature and Apple Silicon then
  refuses to launch it).
- Linux: GUI builds against webkit2gtk-4.1 (the `webkit2_41` tag) when 4.0 is
  absent (Debian bookworm / Raspberry Pi OS); the window/taskbar icon is set at
  runtime and the installer registers a `.desktop` launcher + icon with a baked
  PATH (so a menu-launched GUI finds the device tools and the update toolchain).
- Prebuilt, version-stamped `mfi` CLI binaries for macOS, Linux and Windows are
  attached to the release; version reporting is shared by both binaries via
  `internal/version`.

[Unreleased]: https://github.com/integrisec/MobFI/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/integrisec/MobFI/releases/tag/v1.0.0
