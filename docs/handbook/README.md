# Handbook sources

`docs/handbook.md` and `docs/handbook.pdf` are **generated**. Do not
edit them: the next regeneration overwrites your changes. Edit the
per-chapter sources in this directory instead.

## Layout

```
docs/
  handbook/
    README.md            this file
    NN-slug.md           chapter sources, ordered by the NN prefix
    screenshots/         GUI screenshots referenced by chapters
  handbook.md            GENERATED single-file handbook
  handbook.pdf           GENERATED print artifact
scripts/
  build-handbook.sh      the generator
  lib/handbook.css       print stylesheet (weasyprint path only)
```

Chapters are discovered by glob (`[0-9][0-9]-*.md`) and concatenated
in lexical order. There is no chapter list to maintain: adding
`18-something.md` puts a new chapter at the end automatically.

## Editing

```sh
$ $EDITOR docs/handbook/07-scanning.md
$ make handbook          # regenerate docs/handbook.{md,pdf}
```

Then review the rendered output before committing:

```sh
$ less docs/handbook.md
$ xdg-open docs/handbook.pdf     # or `open` on macOS
```

## Automatic regeneration

The repository ships a pre-commit hook that regenerates and stages
the artifacts whenever a chapter source (or the generator, or its
stylesheet) is part of the commit. Enable it once per clone:

```sh
$ make hooks             # git config core.hooksPath .githooks
```

With the hook enabled, the normal workflow is just:

```sh
$ $EDITOR docs/handbook/07-scanning.md
$ git add docs/handbook/07-scanning.md
$ git commit -m "handbook: clarify verification OPSEC"
# -> handbook: chapter sources changed, regenerating docs/handbook.{md,pdf}...
# -> handbook: regenerated and staged.
```

The hook never blocks a commit. If regeneration fails (missing
pandoc, a broken chapter), it warns and lets the commit through with
the previous artifacts, so a tooling problem on one machine cannot
stop work. Fix it and run `make handbook` before pushing.

Bypass it for one commit with `git commit --no-verify`.

## CI enforcement

CI runs `make handbook-check`, which regenerates the markdown into a
temp file and diffs it against the committed `docs/handbook.md`. A
chapter edit committed without regenerating fails the build with the
diff attached.

The check deliberately ignores the `date:` and `author:` frontmatter
lines. Both are derived from git state, which legitimately differs
between the moment the pre-commit hook runs (the commit does not
exist yet, so git reports the previous commit) and the moment CI
checks the tree out. Chapter content is what the check protects.

The check covers the markdown only, not the PDF. PDF output is not
reliably byte-reproducible across engine and font versions, so
gating on it would produce false failures on contributors' machines.

## Toolchain

| Tool | Required for | Install |
|---|---|---|
| bash, git, awk, sed | Markdown generation | Preinstalled |
| pandoc | PDF generation | `apt install pandoc` / `brew install pandoc` |
| xelatex | PDF, preferred engine | `apt install texlive-xetex` |
| weasyprint | PDF, fallback engine | `pipx install weasyprint` |

The generator degrades cleanly:

- **No pandoc**: markdown is still generated, PDF is skipped.
- **No PDF engine**: markdown is still generated, PDF is skipped,
  install hints are printed.
- **xelatex present**: used in preference (better typography, proper
  page breaks).
- **weasyprint only**: used as a fallback, with `scripts/lib/handbook.css`
  as the print stylesheet.

Because markdown always generates, a contributor without a document
toolchain can still edit a chapter and commit a correct
`handbook.md`. The PDF gets refreshed by the next person who has a
PDF engine, or by whoever cuts the release.

## Writing style

Follow the conventions already in the chapters:

- **Plain ASCII punctuation.** No em-dashes; use `-`, `--`, or `:`.
- **Second person, imperative.** "Open the Devices tab", not "one
  should open" or "we open".
- **Ground every claim in what the code actually does.** If a
  behaviour is not in `internal/` or a frontend, it does not belong
  in the handbook. When you change behaviour, change the chapter in
  the same PR.
- **Concrete over abstract.** Real commands, real paths, real
  output. Show the output when the output is the point.
- **OPSEC and Evidence callouts.** Use a bolded `**OPSEC**:` prefix
  for behaviour observable on the device, the network, or by a third
  party. Use `**Evidence**:` for anything affecting chain of
  custody.
- **Tables for decisions.** When a reader has to choose between
  options, give them a table with the deciding factor as a column.

## Screenshots

Chapters carry HTML comments marking where a screenshot belongs:

```markdown
<!-- screenshot: scan-tab-findings.png -->
```

These are invisible in the rendered output, so the build stays green
while the images are missing. To add one, capture it, save it to
`docs/handbook/screenshots/`, and replace the comment with an image
reference:

```markdown
![The Scan tab after a completed scan](screenshots/scan-tab-findings.png)
```

See `screenshots/README.md` for the capture protocol, naming
convention, and the outstanding list.

## Adding a chapter

1. Create `docs/handbook/NN-slug.md` with the next free number.
2. Start it with a single `# Title` H1. The generator tags that H1
   with a `{#chapter-slug}` anchor so cross-chapter links resolve.
3. Link to it from a neighbouring chapter's "Next" section.
4. `make handbook`.

To reorder chapters, renumber the files. The glob picks up the new
order with no further changes.

## Cross-references

Link to another chapter by its generated anchor:

```markdown
See the [Extraction chapter](#chapter-extraction) for scope selection.
```

The anchor is the filename slug with the numeric prefix removed:
`06-extraction.md` becomes `#chapter-extraction`.
