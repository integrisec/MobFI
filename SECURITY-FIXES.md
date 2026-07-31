# MobFI Security-Fix Changelog

Fix-tracking log for the findings enumerated in
`SECURITY-AUDIT.md` and `LICENSE-AUDIT.md`.

Each row corresponds to one finding. Statuses:

- `Open` -- not yet touched.
- `In progress` -- work started, not merged.
- `Fixed` -- remediated in this branch; commit hash in
  the Fix column.
- `Deferred` -- acknowledged, deliberately postponed;
  reason in Notes.
- `Wont fix` -- rejected as intended behaviour or the
  wrong tradeoff; reason in Notes.
- `N/A` -- superseded by another fix; Notes says which.

Branch: `security-audit-2026-07-31`.
Baseline commit (main HEAD at audit time): `b9c42e3`.
Tracker last synced with commit SHAs: 2026-07-31.

## Security findings

| ID | Severity | Status | Fix | Notes |
|---|---|---|---|---|
| MFI-UPD-01 | Critical | Fixed | `c815422` | ed25519 signature verification over SHA256SUMS.txt. Build-time pubKey via ldflags; empty pubKey fails closed. Release workflow must publish SHA256SUMS.sig. **Requires the operator to set up release signing per SIGNING.md before the next release; without it, self-update via the binary path is broken.** Regression tests cover empty/malformed/wrong-size key, wrong signature, tampered content, and missing SignatureURL. |
| MFI-PATH-01 | Critical | Fixed | `a23c15e` | Added extract.Within guard + NUL / leading-separator rejection to backup.reconstruct; regression test TestReconstructRejectsPathEscape. |
| MFI-CMD-01 | Critical | Fixed | `800afcd` | Replaced `su -c 'joined'` with `su 0 <argv>`; every argv element passed to `adb shell`/`exec-out` is single-quoted for exactly one on-device sh parse. Regression test TestADBQuotesShellMetacharsInFilenames. |
| MFI-CMD-02 | High | Fixed | `800afcd` | Same wrap() rewrite as MFI-CMD-01: every non-su argv element also flows through quoteArgv, so a metachar in a device filename never surfaces as an extra sh token on device. |
| MFI-CMD-03 | High | Fixed | `d8e2b7f` | ConsoleStart validates user / host / port; uses `-l user -- host` form so no argv element admits a leading `-` as an ssh option (blocks -oProxyCommand class). |
| MFI-CMD-04 | High | Fixed | `ca08b82` | deviceUnzipEntry whitelists APK entry names to `[A-Za-z0-9._/-]+`; a hostile APK icon-resource name can no longer smuggle a shell payload to `adb exec-out unzip`. |
| MFI-CMD-05 | High | Fixed | `8820dc3` | OpenExternally uses `open -- <path>` on macOS and `./`-prefixes leading-`-` paths on Linux xdg-open, so no path is parsed as a launcher flag. |
| MFI-PATH-02 | High | Fixed | `ed82da6` | extract.OpenLocalForWrite opens destination with O_NOFOLLOW on Unix (Lstat-based reject on Windows) and mode 0o600. writeLocal + backup.copyFile both route through it. |
| MFI-UPD-02 | High | Fixed | `886266c` | copyToTemp uses os.MkdirTemp (0o700) + a fresh file inside; O_EXCL on the create. No more predictable `/tmp/mobfi-update-worker` symlink-race primitive. |
| MFI-UPD-03 | High | Fixed | `f80f167` | applyGit now runs `git verify-commit HEAD` after pull; refuses to invoke install.sh / install.ps1 unless HEAD is signed by a key in the operator's gpg / ssh trust set. **Requires maintainer commit-signing + operator trust setup per SIGNING.md; without it, self-update via the git path is broken.** |
| MFI-GUI-01 | High | Fixed | `6a534ab` | Default-deny CSP added to index.html. Every asset is same-origin; blob: retained for PDF iframes and images. |
| MFI-GUI-02 | High | Fixed | `d8e2b7f` | Network SSH now uses ~/.config/MobFI/known_hosts (0700) with StrictHostKeyChecking=accept-new; first connect TOFUs, later connects hard-fail on drift. Loopback (USB iproxy) still uses /dev/null (localhost trust). |
| MFI-GUI-03 | High | Fixed | `4da29a6` | Update banner rebuilt from DocumentFragment + textContent; info.latest / info.gitBranch cannot inject markup. |
| MFI-PAR-01 | High | Fixed | `e7203b5` | Added maxPlistDepth=128 counter threaded through object/collection/dict; recursion depth beyond that returns an error instead of stack-overflowing. |
| MFI-PAR-02 | High | Fixed | `4da02aa` | Added maxPlistDepth counter to DecodeXML / parseXMLArray / parseXMLDict / parseXMLElement; also added an outer recover to keep xml decoder panics from escaping into the Wails runtime. |
| MFI-PAR-03 | High | Fixed | `00a7d6b` | Cap PBKDF2 `ITER` and `DPIC` at 10^7; reject anything larger with a clear error. |
| MFI-PAR-04 | High | Fixed | `e7203b5` | numObjects * offsetIntSize now uses math/bits.Mul64 to reject overflow before allocation; also cap numObjects at len(data). |
| MFI-SEC-01 | High | Fixed | `12b579f` | GUI.ScanSecrets / VerifyFindings return findings with Secret stripped. New GUI.RevealSecrets binding gates on native Confirm before returning raw secrets. Frontend lazy-fetches via revealedFindings cache; XSS cannot skip the Go-side Confirm. |
| MFI-XC-01 | High | Fixed | `d99e4a8` | dbview.Open no longer falls back from immutable=1 to mode=ro on the evidence file. On immutable failure it copies the DB + WAL/SHM/journal sidecars to a scratch tempdir and opens the copy; scratch dir is torn down on Close. |
| MFI-XC-02 | High | Fixed | `abdbbfd` | Verifier client uses an explicit http.Transport with Proxy: nil; HTTPS_PROXY / HTTP_PROXY are no longer honored so discovered secrets never flow through a corporate MITM. |
| MFI-DEP-01 | High | Fixed | `0b9025e` | Bumped go directive to 1.25.12; picks up CVE-2025-61728, -61726, -61731, -68119, -68121 and later stdlib crypto/tls + net/http client fixes. |
| MFI-DEP-02 | High | Fixed | `9451a60` | Bumped x/net to v0.57.0. Fixes GO-2026-5942 / CVE-2026-46600 dnsmessage panic. |
| MFI-UPD-04 | Medium | Fixed | `691b25c` | StartUpdate now fires a native MessageDialog (Yes/No) on every OS before starting the update. An XSS invoking the Bind cannot skip it since the dialog runs in the Go layer. |
| MFI-UPD-05 | Medium | Fixed | `f33906c` | Persisted version floor at $UserConfigDir/MobFI/version-floor.txt; refuse to install version <= floor. Written after each successful install. |
| MFI-UPD-06 | Medium | Fixed | `f33906c` | Dedicated updateClient with TLS 1.2 floor, explicit Transport (Proxy nil), and CheckRedirect that rejects redirects to hosts outside the GitHub release-serving allowlist. |
| MFI-GUI-04 | Medium | Deferred | -- | Full session-picked-root gating requires substantial UX design (every Bind method that takes a path needs to descend from a PickDirectory / PickFile result). MFI-GUI-01 CSP dramatically reduces the XSS-to-Bind pivot; addressing the residual "compromised JS reads arbitrary files" needs a design pass, not an ad-hoc patch. Tracked as follow-up work in TODO.md. |
| MFI-GUI-05 | Medium | Fixed | `d8e2b7f` | Same ConsoleStart rewrite as MFI-CMD-03: `-l user -- host` splits the concatenated argv element into separate fields, both validated to reject leading `-`. |
| MFI-PAR-05 | Medium | Fixed | `e7203b5` | count in collection/dict/utf-16 string now capped at len(data) before make([]any/uint16, count); attacker cannot request an 8x memory amplification. |
| MFI-PAR-06 | Medium | Fixed | `57eff5a` | dbview renderCell truncates cell display at 64 KiB with a `<truncated, total N bytes>` marker; multi-GB text-shaped blobs no longer OOM. |
| MFI-PAR-07 | Medium | Fixed | `00a7d6b` | parseKeybag caps each TLV payload at 1 MiB; rejects negative `int(uint32)` casts on 32-bit builds. |
| MFI-SEC-02 | Medium | Fixed | `bc8cb71` | Verifier client sets CheckRedirect to ErrUseLastResponse; redirect targets never receive vendor auth headers. |
| MFI-SEC-03 | Medium | Fixed | `1c1d45b` | fingerprintFor routes known-secret matches through `sha256(m)[:6]` hex tag instead of the 4-char-prefix + exact-length redact; downstream readers cannot dictionary-narrow the operator's literal secrets. Trufflehog rules unchanged (their 4-char prefixes are the public vendor prefix). |
| MFI-SEC-04 | Medium | Fixed | `9c81a25` | Verify now paces requests per vendor host at 250ms and caps total per host at 50; excess -> VerifyUnknown. |
| MFI-XC-03 | Medium | Fixed | `195d359` | CLI main wraps top-level context with signal.NotifyContext(SIGINT, SIGTERM); Ctrl-C now cancels in-flight adb / ideviceinstaller / idevicebackup2 subprocesses and lets scoped `defer os.RemoveAll` calls run. |
| MFI-XC-04 | Medium | Fixed | `a32e032` | report.BuildOpts accepts AnonymiseRoots list; paths under a listed root are rewritten to `<root-N>/rel/...`; Build/BuildWith wrap for compat. Callers (CLI report / GUI export) can now pass the scan root to keep operator username / case codename out of the shipped file. |
| MFI-XC-05 | Medium | Fixed | `7c1816c` | sysproc.CuratedEnv filters spawned-process env to an allowlist (PATH, HOME, LANG, TERM, ADB_*, ...) and blocks a deny-list (AWS_*, GITHUB_TOKEN, ANTHROPIC_*, HTTPS_PROXY, ...). Console PTY now uses CuratedEnv. |
| MFI-XC-06 | Medium | Fixed | `195d359` | adbConn.Walk surfaces transport-partial-listing errors through the walk callback rather than silently accepting a truncated listing as authoritative. |
| MFI-CMD-06 | Medium | Fixed | `799d140` | ConnectTCP / PairTCP validate addr against host:port regex and code against digit regex; leading-`-` addr can no longer be parsed as an adb option. |
| MFI-DEP-03 | Medium | Fixed | `9451a60` | Bumped x/text to v0.40.0. Fixes GO-2026-5970 / CVE-2026-56852 norm.Iter infinite-loop. Landed alongside MFI-DEP-02 due to shared go.mod tidy. |
| MFI-UPD-07 | Low | Fixed | `0c183d1` | download() checks Content-Length and wraps resp.Body in io.LimitReader capped at 512 MB. Compromised release host cannot fill the install volume. |
| MFI-GUI-06 | Low | N/A | -- | Superseded by MFI-GUI-01. The CSP `default-src 'none'; script-src 'self'` blocks any inline script that a Chroma-output edge case might inject. Follow-up sandbox for defence in depth deferred to TODO.md. |
| MFI-GUI-07 | Low | Fixed | `8820dc3` | OpenExternally rejects file extensions whose OS handler would execute (`.exe`, `.bat`, `.lnk`, `.command`, `.sh`, ~30 more) so an extracted `receipt.pdf.exe` cannot be launched by clicking "Open externally". |
| MFI-GUI-08 | Low | Fixed | `15ddd76` | PDF iframe now carries `sandbox=""` (most restrictive); prior blob URL is revoked before the next render assigns a new one. |
| MFI-PAR-08 | Low | Fixed | `57eff5a` | reindentXML tracks nesting depth and errors past 128; xml.Encoder namespace stack cannot be pumped past that. |
| MFI-PAR-09 | Low | Fixed | `57eff5a` | fileKeyFromBlob rejects Manifest.db `Files.file` blobs > 1 MiB before handing to plist.DecodeAny. |
| MFI-SEC-05 | Low | Fixed | `9c81a25` | slackVerify checks Content-Type and reads at most 1 MiB via io.LimitReader; noise / redirect targets can no longer stream unlimited data into the JSON decoder. |
| MFI-XC-07 | Low | Fixed | `60d4352` | Added package-scope sync.Mutex around load/save of window.json; concurrent debounced-resize + shutdown writes no longer interleave. |
| MFI-XC-08 | Low | Fixed | `62197f0` | Console session IDs are now 16-byte crypto/rand hex; a future XSS pivot cannot enumerate active sessions by wall-clock. |
| MFI-PATH-03 | Low | Fixed | `9d333a7` | All SQLite `file:` URIs now built via net/url + filepath.ToSlash; paths with `?` / `#` / `%` / Windows separators no longer confuse the driver. |
| MFI-PATH-04 | Low | Fixed | `5a406a4` | safePath() escapes control bytes to `\xNN` in the plain-text report output; ANSI escape sequences in a device-supplied path can no longer forge terminal lines. JSON output preserves raw bytes for forensics. |
| MFI-CMD-07 | Low | Fixed | `d319665` | afcConn.afc normalises leading-`-` device paths to `./-` before passing to afcclient; no path element can be reparsed as an afcclient option. |
| MFI-UPD-08 | Info | Fixed | `d3f03e4` | validateRelaunchTarget rejects any relaunch target outside the currently-running worker's install prefix; stops a stolen approval token from steering re-exec to an attacker binary. |
| MFI-UPD-09 | Info | Fixed | `d3f03e4` | replaceExecutable retries the Windows `.old` swap with 0/0.5/1/2/4s backoff and errors clearly on final failure; no more silent "rename failed but reported success". |
| MFI-CMD-08 | Info | Fixed | `097d05f` | dirSizeBytes validates dumpsys-derived dataDir/codePath via safeDumpsysPath before splicing into an adb-shell argv; rejects space / control / shell metacharacters. |
| MFI-CMD-09 | Info | Fixed | `097d05f` | bundleIDRe (`^[A-Za-z][A-Za-z0-9._$-]*$`) validates bundle IDs before dumpsys / run-as / (future) simctl calls. |
| MFI-CMD-10 | Info | Fixed | `34690bd` | adbCatRoot now single-quotes path for outer device shell and uses only `su 0 <argv>`; drops the vulnerable `su -c 'cat <path>'` fallback. |

## License findings

| ID | Severity | Status | Fix | Notes |
|---|---|---|---|---|
| LIC-01 | Medium | Fixed | `0bfa14d` | Added THIRD-PARTY-NOTICES.md enumerating every direct + indirect dep (license, SPDX, upstream URL) plus vendored xterm.js assets; discharges MIT / BSD / Apache / ISC notice-preservation for the redistributed binary. |
| LIC-02 | Medium | Fixed | `23e97d9` | secrets.go builtinRules comment reworded from "Ported from Trufflehog" to "Inspired by Trufflehog; regexes re-derived from public token formats". Removes the AGPL derivative-work reading. |
| LIC-03 | Medium | Fixed | `0ad05b6` | Added xterm.LICENSE and addon-fit.LICENSE alongside the vendored minified builds; MIT permission-notice requirement is now satisfied for the redistributed .js files. |
| LIC-04 | Low | Fixed | `a770126` | aeskw.go doc comment now includes an explicit RFC 3394 URL and pins the IV / variable names to the RFC pseudocode. |
| LIC-05 | Low | Fixed | `2860537` | backup_keychain.go doc comment back-references keybag.go's attribution block (Apple whitepaper / iphone-dataprotection / MVT). |

## Notes

Rows update in the same commit that lands the fix, so the
tracker and the code stay in sync. `Fix` column holds the
short SHA of the commit that closes the finding. A stragglers
sync commit (this one) fills in any `pending` placeholders that
predated the tracker-update convention.

`Deferred` and `N/A` rows are recorded in `TODO.md` for follow-up
work.
