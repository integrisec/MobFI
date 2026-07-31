# Rendering and decoding

An extracted tree is full of formats that are unreadable in a plain
text editor: binary property lists, SQLite files, minified JSON,
opaque blobs. Rendering makes them legible; decoding turns encoded
strings back into their contents.

## Rendering a file

```sh
$ mfi render -file <path>
```

```
# application/x-plist

{
  "SessionToken": "eyJhbGciOiJIUzI1NiJ9...",
  "UserID": 4471,
  "LastSync": 2026-07-31T09:14:22Z
}
```

The output begins with the detected MIME type, then the rendered
content.

### Renderer selection

MobFI picks a renderer by **content**, not by file extension, trying
each in priority order and using the first that recognises the file:

| Order | Renderer | Detects | Output |
|---|---|---|---|
| 1 | SQLite | `SQLite format 3` header | Table summary |
| 2 | JSON | Parses as JSON | Pretty-printed, indented |
| 3 | Property list | Binary `bplist00` magic or XML plist | Decoded structure |
| 4 | XML | Parses as XML | Reindented |
| 5 | Text | Valid UTF-8, no control bytes | As-is |
| 6 | Hex dump | (catch-all) | Classic offset/hex/ASCII dump |

Content detection matters on iOS especially, where a file named
`.plist` may be XML or binary, and a file with no extension at all
may be either. You do not need to know which; render it and see.

Rendering caps input at **1 MB**. Files larger than that are
truncated with a marker at the cut point. To read past the cap, use
your own tooling on the extracted file: the capture on disk is the
complete file, only the rendered view is bounded.

### Practical uses

**Read a binary plist** without converting it first:

```sh
$ mfi render -file ./cap/Library/Preferences/com.example.target.plist
```

**Summarise a database** you have not decided to query yet:

```sh
$ mfi render -file ./cap/databases/app.db
```

**See what an extensionless blob actually is**:

```sh
$ mfi render -file ./cap/files/blob_0041
```

If it hex-dumps, look at the first bytes: `PK` is a zip, `\x89PNG`
is an image, `bplist00` a binary plist that failed to parse
(possibly truncated or corrupt).

## Decoding strings

```sh
$ mfi decode <string>
$ mfi decode -input <string>
$ echo <string> | mfi decode
```

All three forms work; use whichever fits your pipeline. The decoder
runs every decoder over the input and reports each result:

```sh
$ mfi decode 'SGVsbG8sIG9wZXJhdG9y'
Base64:  Hello, operator
Hex:     odd number of hex digits
URL:     no percent-encoding found
```

### The decoders

**Base64** tries standard and URL-safe alphabets, padded and raw. An
input that decodes under any of the four is reported.

**Hex** ignores whitespace, `0x` prefixes, and `:`/`-`/`,`
separators, so `48 65`, `0x4865`, and `48:65` all decode.

**URL** percent-decodes, form-style (so `+` becomes a space). It
only applies when the input actually contains a `%`, to avoid
reporting every plain string as a successful no-op decode.

### Binary results

When a decode produces bytes that are not printable text, MobFI
reports it as binary and shows a hex view instead of dumping control
characters into your terminal:

```sh
$ mfi decode 'H4sIAAAAAAAAA/NIzcnJVyjPL8pJAQBWsRdKCwAAAA=='
Base64:  (binary) 1f 8b 08 00 00 00 00 00 00 03 f3 48 cd c9 c9 ...
```

`1f 8b` is a gzip header: that value is a compressed payload, not a
credential.

The hex view is capped at 4096 bytes so a large decode does not
flood the output.

### Chaining decodes

Layered encoding is common in mobile apps. Decode iteratively:

```sh
$ mfi decode 'JTdCJTIydG9rZW4lMjIlM0ElMjJhYmMlMjIlN0Q='
Base64:  %7B%22token%22%3A%22abc%22%7D

$ mfi decode '%7B%22token%22%3A%22abc%22%7D'
URL:     {"token":"abc"}
```

Base64 wrapping URL-encoding wrapping JSON. Two passes and you have
the plaintext.

## In the GUI

The **Render** tab is a file browser plus a viewer:

- Navigate the extracted tree, click a file to render it.
- Syntax highlighting for code and structured formats.
- Images render inline; PDFs render in an embedded viewer.
- A **hex** toggle forces a hex dump of any file.
- A **wrap** toggle for long lines.
- **Open externally** hands the file to your OS default handler.

**Do not use Open externally on files from an untrusted device.**
It invokes your operating system's handler, which will execute an
executable, open a shortcut's target, or run a script. The extracted
tree is attacker-controlled data. If you must open something
externally, copy it out to a scratch directory first and inspect the
name and type deliberately.

The **Decode** tab takes a pasted string and shows every decoder's
result at once, with a copy button per result. The Scan tab's
per-row **Decode** action sends a finding straight there, which is
the fastest route from "the scanner found a JWT" to "here are its
claims".

<!-- screenshot: render-tab-plist.png -->

## Next

- Keys: recover keychain and keystore material.
- Reporting: aggregate what you found.
