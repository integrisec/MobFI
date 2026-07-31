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
| MFI-UPD-01 | Critical | Fixed | pending | ed25519 signature verification over SHA256SUMS.txt. Build-time pubKey via ldflags; empty pubKey fails closed. Release workflow must publish SHA256SUMS.sig. Regression tests cover empty/malformed/wrong-size key, wrong signature, tampered content, and missing SignatureURL. |
| MFI-PATH-01 | Critical | Fixed | pending | Added extract.Within guard + NUL / leading-separator rejection to backup.reconstruct; regression test TestReconstructRejectsPathEscape. |
| MFI-CMD-01 | Critical | Fixed | pending | Replaced `su -c 'joined'` with `su 0 <argv>`; every argv element passed to `adb shell`/`exec-out` is single-quoted for exactly one on-device sh parse. Regression test TestADBQuotesShellMetacharsInFilenames. |
| MFI-CMD-02 | High | Fixed | pending | Same wrap() rewrite as MFI-CMD-01: every non-su argv element also flows through quoteArgv, so a metachar in a device filename never surfaces as an extra sh token on device. |
| MFI-CMD-03 | High | Open | | |
| MFI-CMD-04 | High | Open | | |
| MFI-CMD-05 | High | Open | | |
| MFI-PATH-02 | High | Open | | |
| MFI-UPD-02 | High | Open | | |
| MFI-UPD-03 | High | Open | | |
| MFI-GUI-01 | High | Fixed | pending | Default-deny CSP added to index.html. Every asset is same-origin; blob: retained for PDF iframes and images. |
| MFI-GUI-02 | High | Open | | |
| MFI-GUI-03 | High | Open | | |
| MFI-PAR-01 | High | Open | | |
| MFI-PAR-02 | High | Open | | |
| MFI-PAR-03 | High | Open | | |
| MFI-PAR-04 | High | Open | | |
| MFI-SEC-01 | High | Open | | |
| MFI-XC-01 | High | Open | | |
| MFI-XC-02 | High | Open | | |
| MFI-DEP-01 | High | Fixed | 0b9025e | Bumped go directive to 1.25.12; picks up CVE-2025-61728, -61726, -61731, -68119, -68121 and later stdlib crypto/tls + net/http client fixes. |
| MFI-DEP-02 | High | Fixed | 9451a60 | Bumped x/net to v0.57.0. Fixes GO-2026-5942 / CVE-2026-46600 dnsmessage panic. |
| MFI-UPD-04 | Medium | Open | | |
| MFI-UPD-05 | Medium | Open | | |
| MFI-UPD-06 | Medium | Open | | |
| MFI-GUI-04 | Medium | Open | | |
| MFI-GUI-05 | Medium | Open | | |
| MFI-PAR-05 | Medium | Open | | |
| MFI-PAR-06 | Medium | Open | | |
| MFI-PAR-07 | Medium | Open | | |
| MFI-SEC-02 | Medium | Open | | |
| MFI-SEC-03 | Medium | Open | | |
| MFI-SEC-04 | Medium | Open | | |
| MFI-XC-03 | Medium | Open | | |
| MFI-XC-04 | Medium | Open | | |
| MFI-XC-05 | Medium | Open | | |
| MFI-XC-06 | Medium | Open | | |
| MFI-CMD-06 | Medium | Open | | |
| MFI-DEP-03 | Medium | Fixed | 9451a60 | Bumped x/text to v0.40.0. Fixes GO-2026-5970 / CVE-2026-56852 norm.Iter infinite-loop. Landed alongside MFI-DEP-02 due to shared go.mod tidy. |
| MFI-UPD-07 | Low | Open | | |
| MFI-GUI-06 | Low | Open | | |
| MFI-GUI-07 | Low | Open | | |
| MFI-GUI-08 | Low | Open | | |
| MFI-PAR-08 | Low | Open | | |
| MFI-PAR-09 | Low | Open | | |
| MFI-SEC-05 | Low | Open | | |
| MFI-XC-07 | Low | Open | | |
| MFI-XC-08 | Low | Open | | |
| MFI-PATH-03 | Low | Open | | |
| MFI-PATH-04 | Low | Open | | |
| MFI-CMD-07 | Low | Open | | |
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
