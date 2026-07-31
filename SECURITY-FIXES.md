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

## Security findings

| ID | Severity | Status | Fix | Notes |
|---|---|---|---|---|
| MFI-UPD-01 | Critical | Fixed | pending | ed25519 signature verification over SHA256SUMS.txt. Build-time pubKey via ldflags; empty pubKey fails closed. Release workflow must publish SHA256SUMS.sig. **Requires the operator to set up release signing per SIGNING.md before the next release; without it, self-update via the binary path is broken.** Regression tests cover empty/malformed/wrong-size key, wrong signature, tampered content, and missing SignatureURL. |
| MFI-PATH-01 | Critical | Fixed | pending | Added extract.Within guard + NUL / leading-separator rejection to backup.reconstruct; regression test TestReconstructRejectsPathEscape. |
| MFI-CMD-01 | Critical | Fixed | pending | Replaced `su -c 'joined'` with `su 0 <argv>`; every argv element passed to `adb shell`/`exec-out` is single-quoted for exactly one on-device sh parse. Regression test TestADBQuotesShellMetacharsInFilenames. |
| MFI-CMD-02 | High | Fixed | pending | Same wrap() rewrite as MFI-CMD-01: every non-su argv element also flows through quoteArgv, so a metachar in a device filename never surfaces as an extra sh token on device. |
| MFI-CMD-03 | High | Fixed | pending | ConsoleStart validates user / host / port; uses `-l user -- host` form so no argv element admits a leading `-` as an ssh option (blocks -oProxyCommand class). |
| MFI-CMD-04 | High | Fixed | pending | deviceUnzipEntry whitelists APK entry names to `[A-Za-z0-9._/-]+`; a hostile APK icon-resource name can no longer smuggle a shell payload to `adb exec-out unzip`. |
| MFI-CMD-05 | High | Fixed | pending | OpenExternally uses `open -- <path>` on macOS and `./`-prefixes leading-`-` paths on Linux xdg-open, so no path is parsed as a launcher flag. |
| MFI-PATH-02 | High | Fixed | pending | extract.OpenLocalForWrite opens destination with O_NOFOLLOW on Unix (Lstat-based reject on Windows) and mode 0o600. writeLocal + backup.copyFile both route through it. |
| MFI-UPD-02 | High | Fixed | pending | copyToTemp uses os.MkdirTemp (0o700) + a fresh file inside; O_EXCL on the create. No more predictable `/tmp/mobfi-update-worker` symlink-race primitive. |
| MFI-UPD-03 | High | Fixed | pending | applyGit now runs `git verify-commit HEAD` after pull; refuses to invoke install.sh / install.ps1 unless HEAD is signed by a key in the operator's gpg / ssh trust set. **Requires maintainer commit-signing + operator trust setup per SIGNING.md; without it, self-update via the git path is broken.** |
| MFI-GUI-01 | High | Fixed | pending | Default-deny CSP added to index.html. Every asset is same-origin; blob: retained for PDF iframes and images. |
| MFI-GUI-02 | High | Fixed | pending | Network SSH now uses ~/.config/MobFI/known_hosts (0700) with StrictHostKeyChecking=accept-new; first connect TOFUs, later connects hard-fail on drift. Loopback (USB iproxy) still uses /dev/null (localhost trust). |
| MFI-GUI-03 | High | Fixed | pending | Update banner rebuilt from DocumentFragment + textContent; info.latest / info.gitBranch cannot inject markup. |
| MFI-PAR-01 | High | Fixed | pending | Added maxPlistDepth=128 counter threaded through object/collection/dict; recursion depth beyond that returns an error instead of stack-overflowing. |
| MFI-PAR-02 | High | Fixed | pending | Added maxPlistDepth counter to DecodeXML / parseXMLArray / parseXMLDict / parseXMLElement; also added an outer recover to keep xml decoder panics from escaping into the Wails runtime. |
| MFI-PAR-03 | High | Fixed | pending | Cap PBKDF2 `ITER` and `DPIC` at 10^7; reject anything larger with a clear error. |
| MFI-PAR-04 | High | Fixed | pending | numObjects * offsetIntSize now uses math/bits.Mul64 to reject overflow before allocation; also cap numObjects at len(data). |
| MFI-SEC-01 | High | Fixed | pending | GUI.ScanSecrets / VerifyFindings return findings with Secret stripped. New GUI.RevealSecrets binding gates on native Confirm before returning raw secrets. Frontend lazy-fetches via revealedFindings cache; XSS cannot skip the Go-side Confirm. |
| MFI-XC-01 | High | Fixed | pending | dbview.Open no longer falls back from immutable=1 to mode=ro on the evidence file. On immutable failure it copies the DB + WAL/SHM/journal sidecars to a scratch tempdir and opens the copy; scratch dir is torn down on Close. |
| MFI-XC-02 | High | Fixed | pending | Verifier client uses an explicit http.Transport with Proxy: nil; HTTPS_PROXY / HTTP_PROXY are no longer honored so discovered secrets never flow through a corporate MITM. |
| MFI-DEP-01 | High | Fixed | 0b9025e | Bumped go directive to 1.25.12; picks up CVE-2025-61728, -61726, -61731, -68119, -68121 and later stdlib crypto/tls + net/http client fixes. |
| MFI-DEP-02 | High | Fixed | 9451a60 | Bumped x/net to v0.57.0. Fixes GO-2026-5942 / CVE-2026-46600 dnsmessage panic. |
| MFI-UPD-04 | Medium | Fixed | pending | StartUpdate now fires a native MessageDialog (Yes/No) on every OS before starting the update. An XSS invoking the Bind cannot skip it since the dialog runs in the Go layer. |
| MFI-UPD-05 | Medium | Fixed | pending | Persisted version floor at $UserConfigDir/MobFI/version-floor.txt; refuse to install version <= floor. Written after each successful install. |
| MFI-UPD-06 | Medium | Fixed | pending | Dedicated updateClient with TLS 1.2 floor, explicit Transport (Proxy nil), and CheckRedirect that rejects redirects to hosts outside the GitHub release-serving allowlist. |
| MFI-GUI-04 | Medium | Open | | |
| MFI-GUI-05 | Medium | Fixed | pending | Same ConsoleStart rewrite as MFI-CMD-03: `-l user -- host` splits the concatenated argv element into separate fields, both validated to reject leading `-`. |
| MFI-PAR-05 | Medium | Fixed | pending | count in collection/dict/utf-16 string now capped at len(data) before make([]any/uint16, count); attacker cannot request an 8x memory amplification. |
| MFI-PAR-06 | Medium | Fixed | pending | dbview renderCell truncates cell display at 64 KiB with a `<truncated, total N bytes>` marker; multi-GB text-shaped blobs no longer OOM. |
| MFI-PAR-07 | Medium | Fixed | pending | parseKeybag caps each TLV payload at 1 MiB; rejects negative `int(uint32)` casts on 32-bit builds. |
| MFI-SEC-02 | Medium | Fixed | pending | Verifier client sets CheckRedirect to ErrUseLastResponse; redirect targets never receive vendor auth headers. |
| MFI-SEC-03 | Medium | Fixed | pending | fingerprintFor routes known-secret matches through `sha256(m)[:6]` hex tag instead of the 4-char-prefix + exact-length redact; downstream readers cannot dictionary-narrow the operator's literal secrets. Trufflehog rules unchanged (their 4-char prefixes are the public vendor prefix). |
| MFI-SEC-04 | Medium | Fixed | pending | Verify now paces requests per vendor host at 250ms and caps total per host at 50; excess -> VerifyUnknown. |
| MFI-XC-03 | Medium | Open | | |
| MFI-XC-04 | Medium | Open | | |
| MFI-XC-05 | Medium | Open | | |
| MFI-XC-06 | Medium | Open | | |
| MFI-CMD-06 | Medium | Fixed | pending | ConnectTCP / PairTCP validate addr against host:port regex and code against digit regex; leading-`-` addr can no longer be parsed as an adb option. |
| MFI-DEP-03 | Medium | Fixed | 9451a60 | Bumped x/text to v0.40.0. Fixes GO-2026-5970 / CVE-2026-56852 norm.Iter infinite-loop. Landed alongside MFI-DEP-02 due to shared go.mod tidy. |
| MFI-UPD-07 | Low | Fixed | pending | download() checks Content-Length and wraps resp.Body in io.LimitReader capped at 512 MB. Compromised release host cannot fill the install volume. |
| MFI-GUI-06 | Low | Open | | |
| MFI-GUI-07 | Low | Fixed | pending | OpenExternally rejects file extensions whose OS handler would execute (`.exe`, `.bat`, `.lnk`, `.command`, `.sh`, ~30 more) so an extracted `receipt.pdf.exe` cannot be launched by clicking "Open externally". |
| MFI-GUI-08 | Low | Fixed | pending | PDF iframe now carries `sandbox=""` (most restrictive); prior blob URL is revoked before the next render assigns a new one. |
| MFI-PAR-08 | Low | Fixed | pending | reindentXML tracks nesting depth and errors past 128; xml.Encoder namespace stack cannot be pumped past that. |
| MFI-PAR-09 | Low | Fixed | pending | fileKeyFromBlob rejects Manifest.db `Files.file` blobs > 1 MiB before handing to plist.DecodeAny. |
| MFI-SEC-05 | Low | Fixed | pending | slackVerify checks Content-Type and reads at most 1 MiB via io.LimitReader; noise / redirect targets can no longer stream unlimited data into the JSON decoder. |
| MFI-XC-07 | Low | Fixed | pending | Added package-scope sync.Mutex around load/save of window.json; concurrent debounced-resize + shutdown writes no longer interleave. |
| MFI-XC-08 | Low | Fixed | pending | Console session IDs are now 16-byte crypto/rand hex; a future XSS pivot cannot enumerate active sessions by wall-clock. |
| MFI-PATH-03 | Low | Fixed | pending | All SQLite `file:` URIs now built via net/url + filepath.ToSlash; paths with `?` / `#` / `%` / Windows separators no longer confuse the driver. |
| MFI-PATH-04 | Low | Fixed | pending | safePath() escapes control bytes to `\xNN` in the plain-text report output; ANSI escape sequences in a device-supplied path can no longer forge terminal lines. JSON output preserves raw bytes for forensics. |
| MFI-CMD-07 | Low | Fixed | pending | afcConn.afc normalises leading-`-` device paths to `./-` before passing to afcclient; no path element can be reparsed as an afcclient option. |
| MFI-UPD-08 | Info | Open | | |
| MFI-UPD-09 | Info | Open | | |
| MFI-CMD-08 | Info | Open | | |
| MFI-CMD-09 | Info | Open | | |
| MFI-CMD-10 | Info | Fixed | pending | adbCatRoot now single-quotes path for outer device shell and uses only `su 0 <argv>`; drops the vulnerable `su -c 'cat <path>'` fallback. |

## License findings

| ID | Severity | Status | Fix | Notes |
|---|---|---|---|---|
| LIC-01 | Medium | Open | | |
| LIC-02 | Medium | Open | | |
| LIC-03 | Medium | Open | | |
| LIC-04 | Low | Open | | |
| LIC-05 | Low | Open | | |

## Notes

Rows update in the same commit that lands the fix, so the
tracker and the code stay in sync. `Fix` column holds the
short SHA of the commit that closes the finding.
