# MobFI License and Attribution Audit

- Target: `github.com/integrisec/MobFI` at commit `b9c42e3`
  (branch `main`).
- Date: 2026-07-31.
- Project license: MIT (`LICENSE`, `Copyright (c) 2026 integrisec`).
- Scope: dependency license compatibility (`go.mod` / `go.sum`,
  direct + indirect) and attribution hygiene for code that
  looks copy-pasted or ported without credit.

## Summary

- No copyleft (GPL / AGPL / LGPL) or weak-copyleft
  (MPL / EUPL / CC-BY-SA) modules are linked into the MobFI
  binary. Every direct and indirect dependency is
  MIT / BSD-2 / BSD-3 / Apache-2.0 / ISC / Unlicense.
- One transitive test dep (`github.com/hashicorp/golang-lru/v2`)
  is MPL-2.0 but not imported from MobFI source; not linked.
- The `internal/secrets/secrets.go` header comment currently
  says the pattern set was "ported from" Trufflehog. Trufflehog
  is AGPL-3.0 upstream. A close comparison of seven high-signal
  patterns shows every regex differs materially in prefix set,
  length quantifier, character class, capture-group shape, and
  word-boundary usage. The rule catalog was clearly informed by
  Trufflehog but the code is not a port. The word "ported" is
  the risk, not the code.
- `frontend/dist/vendor/xterm.js` and `frontend/dist/vendor/addon-fit.js`
  are minified UMD builds of upstream `xterm.js` (MIT). The
  minifier stripped the license header from both `.js` files;
  the accompanying `xterm.css` preserves its header. MIT
  redistribution requires the notice + permission text to
  accompany the redistributed copy.
- No `THIRD-PARTY-NOTICES` file exists at the repo root.
  Standard practice for a redistributed Go binary.

## Table of findings

| ID | Severity | Title |
|---|---|---|
| LIC-01 | Medium | No THIRD-PARTY-NOTICES enumerates dependency attributions |
| LIC-02 | Medium | `secrets.go` "ported from Trufflehog" wording risks AGPL confusion |
| LIC-03 | Medium | Vendored xterm.js / addon-fit.js lack their MIT license text |
| LIC-04 | Low | `aeskw.go` implements RFC 3394 without citing it |
| LIC-05 | Low | `backup_keychain.go` does not back-reference `keybag.go` attribution |

## Findings

### LIC-01 -- No THIRD-PARTY-NOTICES enumerates dependency attributions

- **Severity:** Medium (compliance)
- **Observation:** MobFI is a redistributed Go binary that
  statically links 40+ modules. Every MIT / BSD / Apache-2.0
  license in that set requires the copyright + permission
  notice (and, for Apache, the NOTICE file if one exists
  upstream) to accompany binary redistribution. No
  `THIRD-PARTY-NOTICES.md` or `NOTICES` file exists.
- **Remediation:** Add `THIRD-PARTY-NOTICES.md` at the repo
  root enumerating every dependency in `go.mod` (direct and
  indirect actually linked into the binary) with its module
  path, version, SPDX identifier, upstream URL, and the full
  license text.

### LIC-02 -- `secrets.go` "ported from Trufflehog" wording risks AGPL confusion

- **Severity:** Medium (compliance / clarity)
- **File:** `internal/secrets/secrets.go` (package header
  comment)
- **Observation:** The comment describes the regex catalog as
  "ported from the patterns in
  github.com/trufflesecurity/trufflehog/tree/main/pkg/detectors."
  Trufflehog is AGPL-3.0-or-later. A verbatim port would create
  an incompatible license mixture with MobFI's MIT license.
  Comparison against Trufflehog's live source for
  `github-token`, `github-fine-grained-pat`, `aws-access-key-id`,
  `stripe-secret-key`, `slack-webhook`, `openai-api-key`, and
  `anthropic-api-key` shows every pattern differs -- MobFI
  captures more prefixes (AWS: 9 vs 3), uses tighter length
  quantifiers, different character classes, no capture groups,
  and different word-boundary handling. The token-format facts
  the patterns encode (the `AKIA` prefix, `T3BlbkFJ` OpenAI
  marker, `ghp_` GitHub prefix) are published by the token
  issuers and are not copyrightable expression.
- **Impact:** The code is defensible as an independent
  implementation, but the source comment reads as an admission
  of derivation. A downstream consumer or a copyright auditor
  reading the comment alone could reasonably conclude MobFI
  contains AGPL-derived code.
- **Remediation:** Reword the header comment to make the
  independent-derivation position explicit:
  ```
  // Rule catalog inspired by
  // github.com/trufflesecurity/trufflehog (AGPL-3.0). Each
  // regex here is re-derived from the token format published
  // by the issuer and is not a code port; the file layout,
  // rule shape, and matcher structure are original.
  ```

### LIC-03 -- Vendored xterm.js / addon-fit.js lack their MIT license text

- **Severity:** Medium (compliance)
- **Files:** `cmd/mfi-gui/frontend/dist/vendor/xterm.js`,
  `cmd/mfi-gui/frontend/dist/vendor/addon-fit.js`
- **Observation:** Both files are minified UMD builds of
  upstream `xterm.js` (MIT). The minifier stripped the license
  header from the JS builds; `vendor/xterm.css` preserves its
  header correctly. The MIT license requires the copyright +
  permission notice to accompany all copies or substantial
  portions of the Software. Placing the code under `vendor/`
  does not discharge that requirement without an adjacent
  license file.
- **Remediation:** Add
  `cmd/mfi-gui/frontend/dist/vendor/xterm.LICENSE` and
  `cmd/mfi-gui/frontend/dist/vendor/addon-fit.LICENSE`
  containing the upstream MIT license text and copyright.
  (These can be cross-referenced from THIRD-PARTY-NOTICES.md
  in LIC-01.)

### LIC-04 -- `aeskw.go` implements RFC 3394 without citing it

- **Severity:** Low (attribution hygiene)
- **File:** `internal/keystore/aeskw.go`
- **Observation:** The file implements RFC 3394 AES Key
  Unwrap. The default IV `0xA6A6A6A6A6A6A6A6`, the `j` outer
  loop counting down from 5, and the algorithm variables
  (`a`, `r`, `t`, `buf`) all mirror the RFC's pseudocode. The
  file comment names the algorithm ("AES Key Wrap") but does
  not cite the RFC. A reader unfamiliar with the standard has
  no direct pointer to the source of the constants.
- **Remediation:** Prepend an RFC citation to the doc comment:
  ```
  // Implements RFC 3394 (AES Key Wrap). The default IV
  // 0xA6A6A6A6A6A6A6A6 and the algorithm variables mirror
  // the RFC's pseudocode.
  ```

### LIC-05 -- `backup_keychain.go` does not back-reference `keybag.go` attribution

- **Severity:** Low (attribution hygiene)
- **File:** `internal/keystore/backup_keychain.go`
- **Observation:** `keybag.go` correctly credits Apple's iOS
  Security whitepaper, `iphone-dataprotection`, and Mobile
  Verification Toolkit for the keybag format. `backup_keychain.go`
  is the higher-level pipeline for the same body of prior work
  (iOS backup keychain extraction) and does not back-reference
  those sources. A reader who opens `backup_keychain.go` alone
  sees no attribution.
- **Remediation:** Add a one-line pointer to the package doc
  comment:
  ```
  // See keybag.go for the keybag-format attribution
  // (Apple iOS Security whitepaper, iphone-dataprotection,
  // Mobile Verification Toolkit).
  ```

## Dependency inventory

Every module in `go.mod` (direct + indirect) resolved via
`pkg.go.dev/<module>?tab=licenses`. No copyleft or weak-copyleft
in the linked binary.

### Direct

| Module | Version | License |
|---|---|---|
| `github.com/UserExistsError/conpty` | v0.1.4 | MIT |
| `github.com/alecthomas/chroma/v2` | v2.27.0 | MIT (+ OFL-1.1 for one embedded font, not used) |
| `github.com/creack/pty` | v1.1.24 | MIT |
| `github.com/wailsapp/wails/v2` | v2.13.0 | MIT |
| `golang.org/x/crypto` | v0.51.0 | BSD-3-Clause |
| `golang.org/x/sys` | v0.46.0 | BSD-3-Clause |
| `modernc.org/sqlite` | v1.54.0 | BSD-3-Clause |

### Indirect

| Module | Version | License |
|---|---|---|
| `git.sr.ht/~jackmordaunt/go-toast/v2` | v2.0.3 | Unlicense OR MIT |
| `github.com/bep/debounce` | v1.2.1 | MIT |
| `github.com/dlclark/regexp2/v2` | v2.2.1 | MIT |
| `github.com/dustin/go-humanize` | v1.0.1 | MIT |
| `github.com/go-ole/go-ole` | v1.3.0 | MIT |
| `github.com/godbus/dbus/v5` | v5.1.0 | BSD-2-Clause |
| `github.com/google/uuid` | v1.6.0 | BSD-3-Clause |
| `github.com/gorilla/websocket` | v1.5.3 | BSD-2-Clause |
| `github.com/jchv/go-winloader` | v0.0.0-20210711 | ISC |
| `github.com/labstack/echo/v4` | v4.13.3 | MIT |
| `github.com/labstack/gommon` | v0.4.2 | MIT |
| `github.com/leaanthony/go-ansi-parser` | v1.6.1 | MIT |
| `github.com/leaanthony/gosod` | v1.0.4 | MIT |
| `github.com/leaanthony/slicer` | v1.6.0 | MIT |
| `github.com/leaanthony/u` | v1.1.1 | MIT |
| `github.com/mattn/go-colorable` | v0.1.13 | MIT |
| `github.com/mattn/go-isatty` | v0.0.20 | MIT |
| `github.com/ncruces/go-strftime` | v1.0.0 | MIT |
| `github.com/pkg/browser` | v0.0.0-20240102 | BSD-2-Clause |
| `github.com/pkg/errors` | v0.9.1 | BSD-2-Clause |
| `github.com/remyoudompheng/bigfft` | v0.0.0-20230129 | BSD-3-Clause |
| `github.com/rivo/uniseg` | v0.4.7 | MIT |
| `github.com/samber/lo` | v1.49.1 | MIT |
| `github.com/tkrajina/go-reflector` | v0.5.8 | Apache-2.0 (no upstream NOTICE) |
| `github.com/valyala/bytebufferpool` | v1.0.0 | MIT |
| `github.com/valyala/fasttemplate` | v1.2.2 | MIT |
| `github.com/wailsapp/go-webview2` | v1.0.22 | MIT |
| `github.com/wailsapp/mimetype` | v1.4.1 | MIT |
| `golang.org/x/net` | v0.54.0 | BSD-3-Clause |
| `golang.org/x/text` | v0.37.0 | BSD-3-Clause |
| `modernc.org/libc` | v1.74.1 | BSD-3-Clause |
| `modernc.org/mathutil` | v1.7.1 | BSD-3-Clause |
| `modernc.org/memory` | v1.11.0 | BSD-3-Clause |

### Test-only (from `go.sum`, not linked into shipped binary)

| Module | License | Notes |
|---|---|---|
| `github.com/stretchr/testify` | MIT | test framework |
| `github.com/davecgh/go-spew` | ISC | dep of testify |
| `github.com/pmezard/go-difflib` | BSD-3-Clause | dep of testify |
| `gopkg.in/yaml.v3` | MIT + Apache-2.0 | dep of testify |
| `github.com/hashicorp/golang-lru/v2` | MPL-2.0 | transitive test-only; not imported |
| `github.com/hexops/gotextdiff` | BSD-3-Clause | transitive test-only; not imported |

## Verified clean

- No dependency uses GPL, AGPL, LGPL, MPL (in shipped
  binary), EUPL, or CC-BY-SA.
- `internal/keystore/keybag.go` correctly credits Apple's iOS
  Security whitepaper, `iphone-dataprotection`, and Mobile
  Verification Toolkit for the on-wire keybag format.
- `internal/plist/plist.go` and `xml.go` are independent
  implementations, not derived from `DHowett/go-plist`
  (compared struct layout, method decomposition, cycle
  detection -- all differ).
- `internal/keystore/keystore2.go` uses AOSP schema names
  (public) and the `SecurityLevel` HAL enum (public);
  independent implementation.
- `internal/secrets/verify.go` uses stdlib `net/http` with
  vendor-documented whoami endpoints; no shared structure
  with Trufflehog's typed `Detector` / `Result` design.
- `cmd/mfi-gui/frontend/dist/app.js` (2672 lines) is hand
  written; comment style consistent with the rest of the repo,
  no third-party fingerprints.

## Remediation order

1. **LIC-02** -- reword `secrets.go` comment. Fastest way to
   remove the highest-visibility AGPL concern.
2. **LIC-03** -- add the two vendored-JS license files.
3. **LIC-01** -- add `THIRD-PARTY-NOTICES.md`.
4. **LIC-04** -- add RFC 3394 citation to `aeskw.go`.
5. **LIC-05** -- cross-reference `keybag.go` attribution
   from `backup_keychain.go`.

Fix status is tracked in `SECURITY-FIXES.md` alongside the
security findings.
