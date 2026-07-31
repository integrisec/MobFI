# CLI reference

Every command and every flag. Running `mfi` with no arguments starts
the guided wizard; `mfi help` lists the commands; `mfi <command> -h`
lists the flags for one command.

Flags use Go's standard flag package, so `-flag value`,
`-flag=value`, and `--flag value` are all accepted.

## Command summary

| Command | Purpose |
|---|---|
| `wizard` | Guided, step-by-step workflow (the default) |
| `detect` | List reachable Android/iOS devices |
| `apps` | List installed apps on a device |
| `extract` | Copy an app's file tree to a local directory |
| `scan` | Scan an extracted tree for secrets |
| `diff` | Compare two extracted roots |
| `report` | Scan and/or diff, then summarise |
| `db` | Inspect a SQLite database read-only |
| `render` | Render a file in a readable form |
| `decode` | Decode a string (Base64, hex, URL) |
| `keys` | Dump iOS Keychain / Android Keystore |
| `doctor` | Check for external device tools |
| `update` | Check for (and apply) a newer MobFI |
| `version` | Print the MobFI version |
| `help` | Show usage |

---

## `mfi wizard`

Guided workflow: detect, pick an app, extract, scan and/or diff,
report. No flags. Type `q` or Ctrl-D at any prompt to quit.

```sh
$ mfi
$ mfi wizard
```

Reports produced by the wizard are always redacted.

---

## `mfi detect`

Lists every reachable device across all detectors. No flags.

```sh
$ mfi detect
```

Columns: `ID`, `NAME`, `PLATFORM`, `TRANSPORT`, `STATE`. The `ID`
value is what you pass to `-device` elsewhere.

A failing detector does not suppress the others; its error is
reported after the device list.

---

## `mfi apps`

Lists installed applications.

| Flag | Default | Meaning |
|---|---|---|
| `-device` | (required) | Device ID from `mfi detect` |
| `-all` | false | Include system apps |

```sh
$ mfi apps -device emulator-5554
$ mfi apps -device emulator-5554 -all
```

Columns: `BUNDLE ID`, `NAME`, `VERSION`, `DATA PATH`,
`INSTALL PATH`.

---

## `mfi extract`

Mirrors an app's on-device tree to a local directory.

| Flag | Default | Meaning |
|---|---|---|
| `-device` | (required) | Device ID |
| `-app` | (required) | Package name or bundle id |
| `-out` | (required) | Local destination directory |
| `-scope` | `container` | iOS only: `container`, `documents`, or `backup` |

```sh
$ mfi extract -device <id> -app com.example.target -out ./cap
$ mfi extract -device <udid> -app com.example.target -out ./cap -scope documents
$ mfi extract -device <udid> -app com.example.target -out ./cap -scope backup
```

Progress goes to stderr; the summary goes to stdout. Skipped paths
are listed with their reasons.

---

## `mfi scan`

Scans an extracted tree for secrets.

| Flag | Default | Meaning |
|---|---|---|
| `-root` | (required) | Extracted tree to scan |
| `-known` | (none) | File of known secrets, one per line |
| `-verify` | false | Live-verify findings against each service's API |

```sh
$ mfi scan -root ./cap
$ mfi scan -root ./cap -known ./known-secrets.txt
$ mfi scan -root ./cap -verify
```

Output is one line per finding: rule id, path, line, redacted
fingerprint, and (with `-verify`) status. Raw secrets are never
printed by this command.

`-verify` sends the matched secret to the issuing service. See the
Scanning chapter before using it on an engagement.

---

## `mfi diff`

Compares two extracted roots.

| Flag | Default | Meaning |
|---|---|---|
| `-a` | (required) | First root |
| `-b` | (required) | Second root |

```sh
$ mfi diff -a ./cap-01-fresh -b ./cap-02-loggedin
```

Each change reports `added`, `removed`, or `modified`, plus a
structural detail for SQLite, JSON, and property-list files.

---

## `mfi report`

Aggregates a scan and/or a diff into a report.

| Flag | Default | Meaning |
|---|---|---|
| `-root` | (none) | Tree to scan |
| `-known` | (none) | Known-secrets file for the scan |
| `-a` | (none) | First root to diff |
| `-b` | (none) | Second root to diff |
| `-out` | (none) | Also write to this file, format by extension |
| `-show-secrets` | false | Include raw, unredacted secrets |
| `-verify` | false | Live-verify findings |

Requires `-root`, or both `-a` and `-b`, or all three.

```sh
$ mfi report -root ./cap
$ mfi report -a ./cap-01 -b ./cap-02
$ mfi report -root ./cap -a ./cap-01 -b ./cap-02 -out ./report.html
$ mfi report -root ./cap -show-secrets -out ./report-raw.json
```

Format follows the `-out` extension: `.html`/`.htm` for HTML, `.txt`
for text, anything else for JSON. The text summary always prints to
stdout.

---

## `mfi db`

Inspects a SQLite database read-only.

| Flag | Default | Meaning |
|---|---|---|
| `-file` | (required) | Database file |
| `-table` | (none) | Table to dump. Omit to list tables |
| `-limit` | 100 | Maximum rows |

```sh
$ mfi db -file ./cap/databases/app.db
$ mfi db -file ./cap/databases/app.db -table sessions
$ mfi db -file ./cap/databases/app.db -table sessions -limit 1000
```

---

## `mfi render`

Renders a file in a human-readable form.

| Flag | Default | Meaning |
|---|---|---|
| `-file` | (required) | File to render |

```sh
$ mfi render -file ./cap/Library/Preferences/com.example.plist
```

Renderer chosen by content: SQLite, JSON, property list, XML, text,
hex dump. Input is capped at 1 MB with a truncation marker.

---

## `mfi decode`

Decodes a string as Base64, hex, and URL percent-encoding.

| Flag | Default | Meaning |
|---|---|---|
| `-input` | (none) | String to decode |

The input may also be a positional argument or piped on stdin:

```sh
$ mfi decode 'SGVsbG8='
$ mfi decode -input 'SGVsbG8='
$ echo 'SGVsbG8=' | mfi decode
```

Every decoder reports its result or why it did not apply. Binary
results are shown as hex, capped at 4096 bytes.

---

## `mfi keys`

Dumps the platform credential store.

| Flag | Default | Meaning |
|---|---|---|
| `-platform` | (required unless `-backup`) | `ios` or `android` |
| `-device` | (none) | UDID (iOS) or serial (Android) |
| `-state` | (none) | `jailbroken` (iOS) or `rooted` (Android) |
| `-backup` | (none) | iOS: path to an encrypted backup directory |
| `-password` | (none) | iOS: backup password |
| `-reveal` | false | Include raw secret values |

`-platform` is inferred as `ios` when `-backup` is given.
`-password` falls back to `MFI_BACKUP_PASSWORD`.

```sh
$ mfi keys -platform android -device <serial> -state rooted
$ mfi keys -platform ios -device <udid> -state jailbroken
$ MFI_BACKUP_PASSWORD='pw' mfi keys -backup ./backup-dir
$ mfi keys -backup ./backup-dir -reveal
```

Prefer `MFI_BACKUP_PASSWORD` over `-password` on shared hosts:
arguments are visible in the process table.

---

## `mfi doctor`

Reports which external tools are installed.

| Flag | Default | Meaning |
|---|---|---|
| `-json` | false | Machine-readable output |

```sh
$ mfi doctor
$ mfi doctor -json
```

Status is `ok`, `MISSING` (a core tool), or `optional`.

---

## `mfi update`

Checks for a newer MobFI, and optionally applies it.

| Flag | Default | Meaning |
|---|---|---|
| `-json` | false | Machine-readable output |
| `-apply` | false | Perform the update |

```sh
$ mfi update
$ mfi update -json
$ mfi update -apply
```

Set `MFI_NO_UPDATE_CHECK` to any value to disable automatic checks
everywhere.

---

## `mfi version`

```sh
$ mfi version
$ mfi --version
$ mfi -v
```

Prints the version, git commit, and build date.

---

## `mfi help`

```sh
$ mfi help
$ mfi -h
$ mfi --help
```

---

## Environment variables

| Variable | Effect |
|---|---|
| `MFI_NO_UPDATE_CHECK` | Any value disables update checks |
| `MFI_BACKUP_PASSWORD` | iOS backup password for `mfi keys` |
| `MFI_UPDATED` | Set internally after a self-update re-exec |

## Exit codes

`0` on success, `1` on error with the message on stderr prefixed
`mfi:`. Scripts should test the exit code rather than parsing
output.

```sh
$ if ! mfi extract -device "$ID" -app "$PKG" -out "$OUT"; then
    echo "extraction failed" >&2
    exit 1
  fi
```
