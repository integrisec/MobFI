# Handbook screenshots

GUI screenshots referenced by the handbook chapters. Keeping them in
one directory (rather than scattered next to chapters) makes it
practical to recapture the whole set after a UI change.

## Status

No screenshots are committed yet. Chapters mark the intended
positions with invisible HTML comments:

```markdown
<!-- screenshot: scan-tab-findings.png -->
```

That keeps the build green while the set is incomplete. Replace the
comment with a real image reference once the file exists:

```markdown
![The Scan tab after a completed scan](screenshots/scan-tab-findings.png)
```

## Outstanding list

| Filename | Chapter | What to show |
|---|---|---|
| `apps-tab-details.png` | Apps | Apps list with a row selected and the details panel open, showing permissions |
| `extract-tab-progress.png` | Extraction | Extract tab mid-transfer, with the live file and byte counter |
| `scan-tab-findings.png` | Scanning | Completed scan with several findings, one revealed, verification column visible |
| `diff-tab-compare.png` | Diffing | Diff results with a structural detail visible, Compare action on a row |
| `database-tab-rows.png` | Database | Database tab with table chips and a dumped table |
| `render-tab-plist.png` | Rendering | Render tab showing a decoded binary plist, file tree on the left |
| `keys-tab-items.png` | Keys | Keys tab with recovered items, values redacted, limitations visible |
| `report-tab-export.png` | Reporting | Report tab with the export controls |
| `console-tab-session.png` | Console | Console tab with an active adb shell session |

## Capture protocol

Consistency matters more than artistry: a set captured the same way
reads as documentation, a mixed set reads as clutter.

**Environment**

- Use an **emulator or simulator**, never a real client device.
  Screenshots are published; a real capture leaks real data.
- Use a **fabricated target app** with obviously fake data. Seed it
  with recognisable placeholder credentials (`AKIAEXAMPLEEXAMPLE00`)
  so a reader can tell at a glance the data is synthetic.
- Set the window to a **consistent size**. 1280x800 logical is a
  good default: readable in a PDF at page width, not so large that
  text shrinks to nothing.

**Framing**

- Capture the **whole application window**, not a cropped region.
  Context tells the reader where they are.
- Include the tab bar so the active tab is visible.
- Where a chapter discusses one panel, still capture the window and
  let the surrounding text direct attention.

**Redaction**

- Verify **no real credential, hostname, UDID, or serial** is
  visible before saving. Even synthetic-looking values from a real
  device are a leak.
- Device serials and UDIDs: use an emulator (`emulator-5554`) so the
  identifier is inherently non-identifying.

**Format**

- **PNG**, no lossy compression. Text in JPEG screenshots looks
  smeared in print.
- Standard resolution rather than HiDPI where possible: a 2x capture
  triples the file size for no benefit at print scale. If your
  display is HiDPI, downscale to logical resolution before saving.
- Keep individual files under roughly 500 KB. Run them through
  `optipng` or `pngquant` if they exceed that.

**Naming**

`<tab>-<what-it-shows>.png`, lowercase, hyphen-separated. Match the
outstanding list above so a reader of a chapter comment can find the
file.

## After capturing

```sh
$ make handbook
$ xdg-open docs/handbook.pdf     # confirm images render at a sane size
```

Check the PDF, not just the markdown: an image that looks fine in a
browser can overflow the page margins in print. The stylesheet caps
images at container width, but an unusually wide capture still
produces an unreadably small render.
