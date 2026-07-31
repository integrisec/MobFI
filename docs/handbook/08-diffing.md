# Diffing captures

Diffing compares two extracted trees and reports what changed. It is
the highest-signal technique in the toolkit: instead of reading an
app's entire data directory hoping something stands out, you perform
an action on the device and see exactly which files the action
touched.

```sh
$ mfi diff -a <first-root> -b <second-root>
```

```
7 change(s) between /home/op/cap-loggedout and /home/op/cap-loggedin
  added    databases/session.db
  modified shared_prefs/auth.xml (json: 3 field(s) changed)
  modified databases/app.db (sqlite: 2 table(s) changed, 14 row(s) differ)
  removed  files/onboarding.flag
```

## Change kinds

| Kind | Meaning |
|---|---|
| `added` | Present only in the second tree |
| `removed` | Present only in the first tree |
| `modified` | Present in both, contents differ |

The comparison is by path relative to each root, so the two trees
must be captures of the same app for the output to mean anything.

## Structural diffing

For file-level `modified` entries, MobFI does not stop at "these
bytes differ". Three structural differs produce a semantic
description:

**SQLite**: compares table by table and reports how many tables
changed and how many rows differ. A database whose file bytes
changed but whose rows did not (a vacuum, a journal checkpoint,
a timestamp in the header) reports `sqlite: no row differences
(metadata only)`, which saves you opening it.

**JSON**: compares documents structurally and counts changed fields
rather than changed lines. Key reordering and whitespace changes
produce `json: no field differences`.

**Property lists**: handles binary and XML plists, including
comparing a binary plist against an XML one, since the underlying
data model is the same.

When no structural differ recognises the file, or a structural diff
fails, MobFI falls back to a byte-level description (size and hash
change), with the failure reason appended when one occurred.

This is why the diff is worth running even on captures where you
already know something changed: the structural detail tells you
*what* changed, not just *that* it did.

## The canonical workflow

The reason to diff is to attribute data to an action.

```sh
# 1. Baseline: app installed, never logged in
$ mfi extract -device <id> -app com.example.target -out ./cap-01-fresh

# 2. Perform exactly one action on the device: log in

# 3. Capture again
$ mfi extract -device <id> -app com.example.target -out ./cap-02-loggedin

# 4. What did logging in write?
$ mfi diff -a ./cap-01-fresh -b ./cap-02-loggedin
```

Every file in that diff is part of the app's authenticated state.
That is a far better place to hunt for session tokens than the whole
tree.

Actions worth diffing around:

| Action | What you are looking for |
|---|---|
| Log in | Session tokens, refresh tokens, credential caches |
| Log out | Whether the above are actually cleared |
| Enable biometrics | Key material, keychain or keystore references |
| Save a payment method | Card data, tokenised references |
| Receive a push notification | Notification payload persistence |
| Go offline then online | Sync state, cached responses |
| Change a privacy setting | Whether the setting is enforced locally or only in the UI |

The logout case is the most productive finding generator in mobile
testing: apps very often write a token on login and fail to remove
it on logout.

## Sequencing captures

Number your capture directories in the order you took them. The
alternative is discovering three days later that you cannot tell
which of `cap-a` and `cap-b` came first:

```
captures/
  2026-07-31-target/
    cap-01-fresh/
    cap-02-loggedin/
    cap-03-loggedout/
    cap-04-biometrics-on/
```

Then diff whichever pair answers your question:

```sh
$ mfi diff -a cap-02-loggedin -b cap-03-loggedout   # what did logout clear?
$ mfi diff -a cap-01-fresh    -b cap-03-loggedout   # what survived the round trip?
```

The second comparison is the interesting one. Anything present in
`cap-03` that was absent in `cap-01` outlived a full login/logout
cycle.

## Noise

Every diff of a real app contains noise. Common sources:

- **Log files and caches.** Timestamps, request logs, image caches.
- **SQLite WAL and journal files.** `-wal` and `-shm` siblings
  change constantly. The structural differ helps here: the main
  database often reports "metadata only" while the WAL churns.
- **Analytics queues.** Batched events accumulate between captures.
- **Crash reporter state.** Session ids that rotate on every launch.

None of this is a bug in the diff. Filter it in your head, or with
`grep -v`:

```sh
$ mfi diff -a ./cap-01 -b ./cap-02 | grep -vE 'cache/|\.log$|-wal$|-shm$'
```

To reduce noise at the source, minimise the time and activity
between captures: take the second capture immediately after the
action, with the app backgrounded rather than actively running.

## Combining with a scan

The report command runs both in one pass and puts them in one
artifact:

```sh
$ mfi report -root ./cap-02-loggedin \
             -a ./cap-01-fresh -b ./cap-02-loggedin \
             -out ./report.html
```

Findings from the scan and changes from the diff land in the same
report, which is what you want for a writeup: "logging in wrote
`session.db`, and `session.db` contains a live JWT".

## In the GUI

The Diff tab takes two root directories and shows the change list
with the same structural details. Rows carry a **Compare** action as
the primary button, which opens both versions of the file side by
side in the render pane, so you can read the actual difference
rather than the summary of it.

<!-- screenshot: diff-tab-compare.png -->

## Next

- Database viewing: open the SQLite files a diff pointed you at.
- Reporting: aggregate scan and diff into a shareable artifact.
