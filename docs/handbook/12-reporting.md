# Reporting

The report command aggregates a secrets scan and a diff into one
artifact, in text, JSON, or HTML.

```sh
$ mfi report -root <tree>                          # scan only
$ mfi report -a <root-a> -b <root-b>               # diff only
$ mfi report -root <tree> -a <root-a> -b <root-b>  # both
```

| Flag | Meaning |
|---|---|
| `-root` | Tree to scan for secrets |
| `-known` | Known-secrets file to add to the scan |
| `-a`, `-b` | Two roots to diff |
| `-out` | Also write to this file. Format follows the extension |
| `-show-secrets` | Include raw, unredacted secrets |
| `-verify` | Live-verify findings (opt-in, network) |

At least `-root`, or both `-a` and `-b`, is required.

## Output formats

The format follows the extension of `-out`:

| Extension | Format |
|---|---|
| `.html`, `.htm` | Self-contained HTML, inline CSS, no external assets |
| `.txt` | Plain text, same as stdout |
| anything else | JSON |

```sh
$ mfi report -root ./cap -out ./report.html    # HTML
$ mfi report -root ./cap -out ./report.json    # JSON
$ mfi report -root ./cap -out ./report.txt     # text
```

The text summary always prints to stdout as well, so you see the
result immediately and get the file for later.

### Text

```
MobFI report - 2026-07-31T14:22:08Z

Secrets: 3 finding(s)
  aws-access-key-id            1
  jwt                          1
  generic-secret-assignment    1
  - [aws-access-key-id] /home/op/cap/shared_prefs/aws.xml:4  AKIA...(20 chars)
  - [jwt] /home/op/cap/databases/session.db:1  eyJh...(214 chars)

Diff: 7 change(s) (added 3, removed 1, modified 3)
  added    databases/session.db
  modified shared_prefs/auth.xml (json: 3 field(s) changed)
```

Good for terminal review and for pasting into notes.

### JSON

Machine-readable, for pipelines and for importing into a reporting
platform. Contains the findings array (rule id, path, line, redacted
match, verification status), the diff result, and generation
metadata.

```sh
$ mfi report -root ./cap -out ./report.json
$ jq '.findings[] | select(.verified=="active")' ./report.json
```

### HTML

A single self-contained file with inline CSS and no external
assets, so it renders identically offline and can be attached to a
ticket or emailed. Values are HTML-escaped, so attacker-controlled
paths and matches from the extracted data are safe to embed.

## Redaction

**By default, reports contain only redacted fingerprints** (first
four characters plus length). That is the shareable form.

`-show-secrets` retains the raw values in every output format:

```sh
$ mfi report -root ./cap -show-secrets -out ./report-raw.json
```

The report marks itself:

```
Secrets: 3 finding(s)  [UNREDACTED - contains raw secrets]
```

Use it for authorized local analysis. Do not share the output,
do not attach it to a ticket, and delete it when the analysis is
done. If you need to hand a client the *evidence* that a credential
was exposed, the redacted fingerprint plus the file path is normally
sufficient, and the raw value can be provided separately through
whatever secure channel the engagement uses.

## Paths in reports

Findings record the path as walked, so if you scanned an absolute
path, the report contains absolute paths:

```json
{"rule_id":"jwt","path":"/home/christoff/engagements/acme-2026/cap/databases/session.db","line":1}
```

That leaks your username, your directory layout, and possibly the
client's codename to anyone who reads the report.

Two mitigations:

**Scan with a relative path**, from inside the parent directory:

```sh
$ cd ~/engagements/acme-2026
$ mfi report -root ./cap -out ./report.json    # paths are ./cap/...
```

**Or post-process** before sharing:

```sh
$ jq '(.findings[].path) |= sub("^/home/[^/]+/engagements/[^/]+/"; "")' \
    report.json > report-clean.json
```

Check the report before it leaves your machine. This is the most
common accidental disclosure in tooling output.

## Verification in reports

`-verify` adds live verification (see the Scanning chapter for what
that sends and where):

```sh
$ mfi report -root ./cap -verify -out ./report.html
```

Verified findings carry `active`, `inactive`, `unknown`, or
`unsupported`, which makes the report far more actionable: a client
reading "3 credentials found, 1 confirmed live" prioritises
differently from "3 credentials found".

## A complete engagement flow

```sh
$ cd ~/engagements/acme-2026

# Capture baseline, act on the device, capture again
$ mfi extract -device <id> -app com.acme.mobile -out ./cap-01-fresh
$ mfi extract -device <id> -app com.acme.mobile -out ./cap-02-loggedin

# One report covering both the scan and what login changed
$ mfi report -root ./cap-02-loggedin \
             -a ./cap-01-fresh -b ./cap-02-loggedin \
             -known ./known-secrets.txt \
             -out ./report.html

# Keep a JSON copy for the reporting platform
$ mfi report -root ./cap-02-loggedin \
             -a ./cap-01-fresh -b ./cap-02-loggedin \
             -out ./report.json
```

**Evidence**: keep the captures. The report references file paths
and line numbers that are only meaningful alongside the tree they
came from. Archive `cap-*` and the report together.

## In the GUI

The Report tab builds the same artifact from the last scan and diff
performed in the session, with an export button per format. The
unredacted export is gated behind a confirmation dialog that states
what the file will contain.

<!-- screenshot: report-tab-export.png -->

## Next

- Console: interactive device shell for follow-up questions.
- Updating: keep MobFI current.
