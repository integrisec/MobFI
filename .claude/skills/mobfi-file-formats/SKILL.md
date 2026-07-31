---
name: mobfi-file-formats
description: >
  Teach MobFI to handle a new file format end to end: a renderer so the format
  is readable, a structural differ so two captures compare semantically rather
  than byte-wise, and a decoder for encoded string values. Triggers on "add a
  renderer", "MobFI shows this file as a hex dump", "add support for <format>",
  "diff shows only bytes changed", "add a structural differ", "add a decoder",
  or edits under internal/render, internal/diff, internal/plist, or
  internal/decode. Covers the Renderer and FileDiffer interfaces, content
  sniffing versus extension matching, registry ordering, the size caps, and
  the detail-string conventions a differ must follow.
---

# File formats

## Purpose

Add or fix format handling across the three places MobFI reasons
about file contents: rendering (read one file), structural diffing
(compare two files), and decoding (unwrap an encoded string).

## Which one do you need

| Symptom | Add | Package |
|---|---|---|
| File shows as a hex dump, should be readable | Renderer | `internal/render` |
| Diff says only "size and hash changed" for a format with structure | Structural differ | `internal/diff` |
| A string value needs unwrapping (encoded, not a file) | Decoder | `internal/decode` |
| A container format needs parsing before any of the above | Parser package | e.g. `internal/plist` |

A rich format usually wants both a renderer and a differ, and they
are independent: adding one does not oblige you to add the other.

## Renderers

### Interface

```go
type Renderer interface {
    Handles(path string) bool
    Render(ctx context.Context, path string) (*View, error)
}

type View struct {
    MIME string `json:"mime"` // best-guess content type
    Text string `json:"text"` // rendered text form
}
```

### Registry order

`render.DefaultRegistry()` consults renderers in order and uses the
first whose `Handles` returns true:

```go
r.Add(sqliteRenderer{})
r.Add(jsonRenderer{})
r.Add(plistRenderer{})
r.Add(xmlRenderer{})
r.Add(textRenderer{})
r.Add(hexRenderer{}) // catch-all
```

Specific to general, hex dump last. **Insert a new renderer above
every renderer that would also claim the file, and below every
renderer more specific than it.** A renderer for a JSON-based format
goes above `jsonRenderer`, or `jsonRenderer` will claim it first.

The catch-all means `Render` never fails to produce something for a
readable file, which is why the GUI can point at any file in a
capture without a type check.

### Sniff content, not extension

`Handles` should look at bytes, not the filename. Extensions on
mobile devices are unreliable: iOS `.plist` files are binary or XML,
files with no extension are common, and an attacker chooses the name
anyway.

```go
func (r myRenderer) Handles(path string) bool {
    return bytes.HasPrefix(readSniff(path, len(myMagic)), myMagic)
}
```

`readSniff(path, n)` reads the first `n` bytes; `sqliteRenderer`
uses it against the `SQLite format 3\x00` header. Match on magic
bytes where the format has them. Where it does not (JSON, XML), a
trial parse of a capped prefix is the honest test, which is what
`jsonRenderer` and `xmlRenderer` do.

### Size cap

Rendering is bounded by `maxRenderBytes` (1 MB). Use the shared
`readCapped(path, maxRenderBytes)` helper, which returns the bytes
and a `truncated` flag, and append the truncation marker when it is
set, as `textRenderer` does. Do not read a whole file into memory to
render it: the input is attacker-controlled and may be enormous.

### Checklist

- [ ] `Handles` sniffs content, does not trust the extension
- [ ] Registered in `DefaultRegistry()` at the correct priority
- [ ] Uses `readCapped` and honours truncation
- [ ] Sets a sensible `MIME` (it is displayed to the user)
- [ ] Test in `internal/render/render_test.go` covering: recognised,
      rendered correctly, and not claimed by a neighbouring renderer
- [ ] Malformed input returns an error, never a panic

## Structural differs

### Interface

```go
type FileDiffer interface {
    Handles(path string) bool
    Diff(ctx context.Context, a, b string) (string, error)
}

var defaultFileDiffers = []FileDiffer{sqliteDiffer{}, jsonDiffer{}, plistDiffer{}}
```

The returned string is the human-readable `Detail` on a `Change`.
When no differ recognises the file, or a differ errors, `compare`
falls back to `byteDetail` (a size and hash summary), with the
failure reason appended.

### Detail-string conventions

The detail is read by an operator triaging a diff, so it follows a
shape:

```
sqlite: 2 table(s) changed, 14 row(s) differ
sqlite: no row differences (metadata only)
json: 3 field(s) changed
json: no field differences
```

Three rules:

1. **Prefix with the format** (`sqlite:`, `json:`). A diff mixes
   formats; the reader needs to know what kind of comparison
   produced the claim.
2. **Say "no differences" explicitly when the bytes changed but the
   structure did not.** This is the single most useful output a
   differ produces: a SQLite file whose header timestamp moved but
   whose rows are identical is noise, and saying so saves the
   operator opening it.
3. **Count, do not enumerate.** A diff of a large database must stay
   one line. Detail goes in the GUI's Compare view, not the summary.

### Checklist

- [ ] `Handles` sniffs content like a renderer does
- [ ] Added to `defaultFileDiffers` at the correct priority
- [ ] Detail is prefixed, counted, and single-line
- [ ] The "changed bytes, unchanged structure" case is reported
      explicitly
- [ ] Errors return an error, so `compare` can fall back cleanly
- [ ] Test in `internal/diff/structural_test.go`

## Decoders

`internal/decode` handles encoded **strings**, not files. `All(s)`
runs every decoder and returns one `Result` each, so the caller can
show what applied and what did not.

```go
type Result struct {
    Name   string // "Base64", "Hex", "URL"
    OK     bool
    Value  string // decoded bytes as text
    Hex    string // space-grouped hex
    Binary bool   // decoded bytes are not printable text
    Error  string // why it did not apply, when !OK
}
```

Conventions worth preserving:

- **Report why a decoder did not apply** (`"not valid Base64"`,
  `"no percent-encoding found"`) rather than silently omitting it.
  The user is trying to work out what an opaque value is; a negative
  result is information.
- **Only apply when the input plausibly is that encoding.** `URL`
  requires a literal `%`, so a plain string does not get reported as
  a successful no-op decode.
- **Detect binary output** and populate `Hex` instead of dumping
  control bytes into a terminal. `finish()` does this via
  `printable()`.
- **Cap the hex preview** (`hexPreviewBytes`, 4096).

## Parser packages

A format complex enough to need real parsing gets its own package
(`internal/plist` is the model), and the renderer or differ calls
into it. Keep parsing separate from presentation: the plist decoder
returns Go values, and the renderer decides how to show them.

Parsers consume attacker-controlled bytes directly, so they carry
the heaviest safety burden in the codebase. Read
`mobfi-untrusted-input` before writing one. The short version:
bound recursion depth, validate every length prefix against the
remaining buffer, reject allocation sizes derived from input, and
return errors rather than panicking.

## Documentation

`docs/handbook/10-render-decode.md` documents the renderer priority
order and the 1 MB cap; `docs/handbook/08-diffing.md` documents
which formats diff structurally. Adding a format means updating the
relevant table in the same pull request.

## Cross-references

- `mobfi-untrusted-input`: mandatory reading before writing a parser
- `mobfi-architecture`: package boundaries
- `mobfi-gui-binding`: how rendered output reaches the webview
