# MobFI TODO

Follow-up work from the `security-audit-2026-07-31` branch:
tasks that are out of scope for the security-fix pass but worth
scheduling next. Grouped by theme, each entry lists what needs to
happen, who benefits, and what a good outcome looks like.

## Docs

### DOC-01 -- Operator handbook

**Priority:** high. **Effort:** substantial (~1-2 days of focused
work).

Produce a single, comprehensive operator handbook that covers
every MobFI workflow, from first-run setup through publishing a
report. Model the structure and delivery pipeline on Dinah's
`docs/full-handbook.md` (a single pandoc-friendly markdown source
that renders to PDF via the existing build tooling).

Include, at minimum:

- **Prerequisites** -- adb + libimobiledevice install per OS
  (macOS via Homebrew, Linux via apt, Windows via chocolatey /
  scoop); iTunes / iCloud backup configuration for the iOS
  backup path; when a jailbroken device is / is not required.
- **First run** -- launch the GUI, `mfi doctor`, connect a
  device over USB, over ADB-TCP, over Wireless Debugging. Show
  what the Devices tab looks like when a device is `ready` vs
  `unauthorized` vs `unpaired`.
- **Extract workflows** -- Android `run-as` vs rooted (`su`)
  vs iOS AFC (container / documents) vs iOS backup
  reconstruction. Include the exact CLI invocation and the
  equivalent GUI steps side by side.
- **Scan for secrets** -- default catalog, adding a known-secret
  file, live-verify opt-in (its cost / risk / OPSEC
  considerations), reveal flow (per MFI-SEC-01).
- **Diff two captures** -- typical logged-in vs logged-out
  scenario; structural diffs for SQLite / JSON / plist / bplist;
  interpreting the output.
- **Database viewing** -- opening a hot-WAL SQLite (scratch-
  copy behaviour per MFI-XC-01), read-only guarantees, evidence
  integrity notes.
- **Render / decode** -- what MIME types render inline (image,
  PDF, code with Chroma highlighting, plist, JSON), what the
  hex-dump fallback looks like, security posture for opening
  extracted files externally (per MFI-CMD-05 / MFI-GUI-07).
- **Keys dump** -- Android keystore2 workflow, iOS keychain
  workflow (jailbroken vs encrypted-backup paths, backup-
  password prompt).
- **Report export** -- JSON / HTML / text; redaction on by
  default; anonymise-roots option (per MFI-XC-04); what to hand
  to the client vs what to keep internal.
- **Console** -- adb shell on Android, ssh on iOS (USB via
  iproxy vs network); host-key TOFU flow (per MFI-GUI-02);
  session transcript logging.
- **Self-update** -- signature verification (per SIGNING.md),
  what the "Update now" flow does on each OS, how to roll back
  a bad update via `.old`.
- **Doctor** -- what each check probes, how to fix a missing
  dependency.
- **Security posture** -- pointer to `SECURITY-AUDIT.md`,
  `SECURITY-FIXES.md`, `SIGNING.md`, `THIRD-PARTY-NOTICES.md`.
  Note the trust boundary (a paired device is untrusted input).

Screenshots: capture actual GUI panels on macOS (light and dark
themes if the app supports both). Use the same shot-set for the
matching CLI section as a "and this is what the terminal-only
version looks like." A `docs/handbook/screenshots/` directory
mirroring the section layout is easier to update than inlining
images throughout.

Delivery:
- `docs/handbook.md` (source, pandoc frontmatter with title /
  toc / fonts matching what Dinah's `docs/full-handbook.md`
  uses; MobFI can pick its own colour scheme).
- `docs/handbook.pdf` (built by the Makefile / release
  workflow; include in every GitHub release as an asset).
- `docs/handbook/screenshots/*.png` (or `.webp` for smaller
  files).
- Update `README.md` to link the handbook prominently.

Style notes (from Dinah conventions the operator has been
applying):
- Plain ASCII punctuation only. No em-dashes anywhere; use `-`
  or `--` or `:` instead.
- Second-person imperative voice ("open the Devices tab", not
  "one should" or "we open"). No first-person plural.
- Ground every claim in what the code actually does today. Do
  not describe hypothetical behaviour.
- Prefer concrete commands, exact paths, and file names over
  hand-waving.
- Where a workflow has a security implication, cross-reference
  the SECURITY-AUDIT.md finding by ID.

## Security follow-ups (from Deferred / N/A rows)

### SEC-FOLLOWUP-01 -- MFI-GUI-04 Bind path restriction

**Priority:** medium. **Effort:** substantial (design + code +
frontend + Docs).

`SECURITY-FIXES.md` deferred MFI-GUI-04 because a proper fix is
an architecture change, not a patch:

- Every bound method that takes a `path string` currently accepts
  any local path (`Render`, `RenderPath`, `ListDir`,
  `FileStat`, `DBRead`, `DBTables`, `AddKnownSecrets`,
  `RemoveDir`, `OpenExternally`).
- MFI-GUI-01 CSP dramatically reduces the XSS-to-Bind pivot,
  but a compromised JS runtime still reads arbitrary local
  files.

Design task: introduce a session-scoped "picked roots" set,
populated only by native PickDirectory / PickFile calls. Every
path-taking method rejects paths that do not descend from a
picked root (with a clear error to the JS side). PickFile is
the entry point that expands the trusted set. Also cover: how
the operator adds a NEW root mid-session (should require a
new PickDirectory call, not a JS-controlled add); how tests
mock the trust boundary; whether the trust-set persists across
window reopens.

### SEC-FOLLOWUP-02 -- MFI-GUI-06 Chroma output sandbox (defence in depth)

**Priority:** low. **Effort:** small (~half a day).

MFI-GUI-01 CSP already blocks the exploit vector, but wrapping
Chroma highlighter output in an `<iframe sandbox="">` (or
running through DOMPurify before `innerHTML`) is a cheap
defence-in-depth improvement. Only ship if it does not visibly
change the highlighter's behaviour.

### SEC-FOLLOWUP-03 -- Curated env everywhere

**Priority:** medium. **Effort:** small (~1 hour).

MFI-XC-05 landed `sysproc.CuratedEnv` and switched the console
PTY over. The update worker
(`cmd/mfi-gui/update.go:startUpdateWorker`) and the CLI
self-re-exec (`cmd/mfi/exec_unix.go` / `exec_windows.go`) still
use `os.Environ()`. Migrate them after a per-tool env compat
check confirms nothing under the deny-list is required by any
downstream install-script step.

### SEC-FOLLOWUP-04 -- Anonymise-roots wiring

**Priority:** medium. **Effort:** small (~1 hour).

MFI-XC-04 added `report.BuildOpts.AnonymiseRoots` but the
existing callers (CLI `mfi report`, GUI Export button) still
call `Build` / `BuildWith`. Thread the scan root through so
exported reports do not leak the operator's local case
directory layout by default. A CLI flag `--anonymize-root` (or
just auto-anonymise the extract dest by default) works either
way; pick whichever matches the operator's workflow.

## CI / release infrastructure

### CI-01 -- Release-signing pipeline (blocker for MFI-UPD-01)

`SIGNING.md` documents the operator-side setup. Wire it into
the release workflow so every published release ships
`SHA256SUMS.sig` and the maintainer's signed commits are the
only ones ever tagged. Also codify:

- The maintainer keyring lives in `.github/maintainer-keys/`
  (public keys only).
- The release-signing private key lives ONLY on the offline
  signing machine or in a hardware token (YubiKey, etc.).
- Rotation procedure: bump `internal/selfupdate.pubKeyBase64`
  via ldflags in one release, publish both old and new sig for
  one transition release, then drop the old key.

### CI-02 -- govulncheck in CI

Add `govulncheck` to the CI workflow so new advisories against
pinned deps are caught before the next release cut. Fail the
build on any advisory that applies to a linked function.

### CI-03 -- License-audit in CI

Add a `go-licenses check ./...` step (or a comparable license
scanner) so a future indirect dep bump that pulls in a
copyleft module fails the build rather than shipping quietly.
The `THIRD-PARTY-NOTICES.md` refresh procedure should be
enforced too.

## Testing gaps

### TEST-01 -- Regression tests for the parser hardening

The plist / keybag caps added by MFI-PAR-01 through MFI-PAR-09
have no fuzz tests. Add a small go-fuzz corpus with malformed
plist headers, overflowing offsets, deeply-nested arrays, and
crafted TLV lengths so future refactors don't regress the
caps. Same treatment for the tar extraction guards
(MFI-PATH-02).

### TEST-02 -- End-to-end updater test

The MFI-UPD-01 signature-verify path is unit-tested (`apply_test.go`)
but the full "download + verify sig + verify sha + rename" end
to end is not. Add an integration test using a local httptest
server that publishes a signed asset set; run against a
temporary MobFI binary; assert the swap succeeds.

### TEST-03 -- Fuzz the extract tar path

`RunTar` handles attacker-controlled tar entries. Fuzz with
malformed headers, tar-slip variants, and mixed absolute/relative
names to lock in the current guard behaviour.

## Housekeeping

### HK-01 -- Bump go 1.25.12 to a supported LTS as Go moves

`go 1.25.12` is the floor set by MFI-DEP-01; when Go 1.26 or a
later minor becomes the supported branch, bump so the toolchain
directive tracks the latest stdlib fixes.

### HK-02 -- Replace `pkg/errors` and pre-generic slice types

`pkg/errors` is deprecated upstream. Migrate to `errors.Join`
and `fmt.Errorf("...: %w", err)` on the next refactor pass.
