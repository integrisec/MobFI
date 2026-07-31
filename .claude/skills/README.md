# Project skills

Skills for working **on MobFI**: the conventions, interfaces, and
safety rules that are not obvious from reading one file, written so
an assistant (or a new contributor) applies them without being told
twice.

These describe the codebase, not the product. Operator-facing
documentation is the handbook under `docs/handbook/`.

## Available skills

| Skill | Use when |
|---|---|
| `mobfi-architecture` | You do not know where a change belongs. Start here |
| `mobfi-secret-rules` | Adding or fixing a detection rule or its live verifier |
| `mobfi-file-formats` | Adding a renderer, structural differ, or decoder |
| `mobfi-device-support` | Adding a detector, transport, app lister, or extraction path |
| `mobfi-gui-binding` | Adding or changing a desktop-GUI feature |
| `mobfi-untrusted-input` | Touching anything that reads device data. Read before writing |
| `mobfi-release` | Cutting a release |

`mobfi-architecture` is the hub: it holds the package map and a
"where does this go" decision tree, and routes to the others.

## How they are used

Claude Code discovers skills in `.claude/skills/` automatically when
the working directory is inside this repository. A skill fires when
the request matches its description, or you can invoke one by name.

Nothing here is required to build or run MobFI. A contributor who
never reads them is not blocked; they will just rediscover the
conventions the slow way.

## Grounding

Every claim is taken from this repository:

| Claim area | Source |
|---|---|
| Core/frontend split, orchestration | `internal/app/app.go` |
| Registry interfaces and ordering | `internal/{render,diff,device,transport}` |
| Scanner catalog, limits, redaction | `internal/secrets/` |
| Path guards and skip tracking | `internal/extract/` |
| Keystore routes and limitations | `internal/keystore/` |
| Version and update mechanics | `internal/{version,selfupdate}/` |
| Binding and event patterns | `cmd/mfi-gui/gui.go`, `frontend/dist/app.js` |
| Build and CI gates | `Makefile`, `.github/workflows/ci.yml` |

Two external facts are cited: the Wails binding contract (a bound
struct's exported methods become the frontend API) and Go's RE2
constraint (no backtracking, lookaround, or backreferences), both
verified rather than assumed.

## Maintaining them

A skill that describes stale conventions is worse than no skill,
because it is confidently wrong. When you change something a skill
documents, update it in the same pull request. The likely triggers:

| Change | Update |
|---|---|
| New registry, or reordering an existing one | `mobfi-architecture`, plus the specific skill |
| Scanner limits, redaction, verifier rules | `mobfi-secret-rules` |
| Renderer priority, differ detail format | `mobfi-file-formats` |
| Access-mechanism probe order, a new iOS scope | `mobfi-device-support` |
| Binding, event, or cancellation conventions | `mobfi-gui-binding` |
| A new path guard or parser limit | `mobfi-untrusted-input` |
| Asset naming, version locations, update flow | `mobfi-release` |

Keep the frontmatter `description` specific: it is what decides
whether a skill fires at the right moment. Vague descriptions make
skills fire on everything or nothing.

Style matches the handbook: plain ASCII punctuation (no em-dashes),
second-person imperative, concrete file paths and real code over
abstraction.
