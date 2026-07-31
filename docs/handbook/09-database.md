# Database viewing

Mobile apps keep their interesting state in SQLite. Session tokens,
cached API responses, message history, analytics queues, and
credential caches all tend to land in a `.db` file under the app's
data directory. MobFI opens them read-only so you can list tables
and dump rows without touching the evidence.

```sh
# List tables
$ mfi db -file <path-to.db>

# Dump a table
$ mfi db -file <path-to.db> -table <table> [-limit N]
```

| Flag | Default | Meaning |
|---|---|---|
| `-file` | (required) | SQLite database file |
| `-table` | (none) | Table to dump. Omit to list tables |
| `-limit` | 100 | Maximum rows to read |

## Listing and dumping

```sh
$ mfi db -file ./cap/databases/app.db
4 table(s):
  sessions
  messages
  cached_responses
  android_metadata

$ mfi db -file ./cap/databases/app.db -table sessions -limit 5
id  user_id  token                                     created_at
1   4471     eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...   1722384000
(1 row(s))
```

User tables are listed; SQLite's internal `sqlite_*` tables are
omitted.

The table name you pass is validated against the database's actual
table list before use, and quoted defensively, so a table name
cannot be used to inject SQL.

## Finding the databases in a capture

```sh
$ find ./cap -name '*.db' -o -name '*.sqlite' -o -name '*.sqlite3'
```

Android apps conventionally use `databases/`; iOS apps scatter them
more widely, often under `Library/` or `Documents/`. File extension
is a weak signal on iOS: MobFI's renderer detects SQLite by the file
header, so if you are unsure whether a file is a database, run
`mfi render -file <path>` and see what it reports.

## Read-only and evidence integrity

**Evidence**: MobFI opens databases read-only. The main database
file is never written.

Be aware of one caveat on this version: a database carrying a hot
write-ahead log (a `-wal` sibling) cannot be opened in SQLite's
strictest immutable mode, because the WAL must be read to see
committed rows. In that case MobFI falls back to a plain read-only
open, and SQLite may create or update `-wal` and `-shm` sidecar
files next to the database.

The practical consequence for chain of custody: **hash the capture
directory before browsing databases**, not after. If sidecar
mutation is unacceptable for your evidence handling, copy the
database (and its `-wal` / `-shm` siblings) to a scratch directory
and point MobFI at the copy.

```sh
$ mkdir -p /tmp/scratch
$ cp ./cap/databases/app.db* /tmp/scratch/
$ mfi db -file /tmp/scratch/app.db -table sessions
```

## Cell rendering

Values are rendered for reading, not for byte-exact export:

- `NULL` prints as `NULL`.
- Text blobs print as text.
- Binary blobs print as `<blob N bytes>` rather than dumping raw
  bytes into your terminal.

When a blob matters, extract it and inspect it separately: the
Decode tab handles base64 and hex, and `mfi render` will hex-dump
anything it does not recognise.

## What to look for

A checklist that covers most mobile findings:

| Table or column pattern | Why it matters |
|---|---|
| `session`, `token`, `auth`, `credential` | Session material persisted to disk |
| `user`, `account`, `profile` | PII, and the identity bound to the session |
| `cache`, `response`, `http` | Cached API responses, often containing the above |
| `message`, `chat`, `conversation` | Message content, often unencrypted at rest |
| `analytics`, `event`, `queue` | What the app reports about the user |
| `key`, `secret`, `cert` | Key material the app manages itself |
| Any column holding `eyJ...` | A JWT. Decode it |

Cross-reference with the scan: a finding at `databases/app.db:1`
tells you the secret is in a database, and this is how you see its
row context.

## Decoding what you find

Values in databases are frequently encoded. Pull the value and hand
it to the decoder:

```sh
$ mfi decode 'eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiI0NDcxIn0.abc'
Base64:  {"alg":"HS256"}
Hex:     no hex digits
URL:     no percent-encoding found
```

For JWTs, decoding the first segment gives you the algorithm, and
the second gives you the claims (including expiry, which tells you
whether the token you found is still valid).

## In the GUI

The Database tab lists tables as clickable chips, dumps rows into a
sortable and resizable table, and keeps the header row frozen while
the body scrolls. The row limit is adjustable per query.

The Render tab recognises SQLite files and offers an **Open in
Database** button, so a database you spot while browsing the tree is
one click from being queried.

<!-- screenshot: database-tab-rows.png -->

## Next

- Rendering and decoding: read the non-database files.
- Keys: recover credential-store material.
