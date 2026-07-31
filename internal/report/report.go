// Package report aggregates scan and diff results into an actionable
// summary that any frontend can render as text or export as JSON.
package report

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/integrisec/MobFI/internal/diff"
	"github.com/integrisec/MobFI/internal/secrets"
)

// Report is the actionable summary of an inspection session. By default
// findings carry only redacted secrets, so a report is safe to export; when
// Unredacted is set the raw secrets are included (see BuildWith).
type Report struct {
	GeneratedAt time.Time         `json:"generated_at"`
	Findings    []secrets.Finding `json:"findings,omitempty"`
	Diff        *diff.Result      `json:"diff,omitempty"`
	// Unredacted reports whether raw secrets are retained in this report.
	Unredacted bool `json:"unredacted,omitempty"`
}

// Build assembles a report from the collected findings and diff with raw
// secrets stripped, so the report (text, JSON or HTML) is safe to share.
func Build(findings []secrets.Finding, d *diff.Result) *Report {
	return BuildOpts(findings, d, Options{})
}

// BuildWith assembles a report, optionally retaining the raw secret values.
// When unredacted is false (the safe default) each finding's raw Secret is
// stripped and only the redacted fingerprint (Match) is kept. When true the
// raw secrets are preserved and shown in every output format -- intended for
// authorized local analysis, never for sharing.
func BuildWith(findings []secrets.Finding, d *diff.Result, unredacted bool) *Report {
	return BuildOpts(findings, d, Options{Unredacted: unredacted})
}

// Options configures BuildOpts. AnonymiseRoots rewrites every path that
// starts with one of the given prefixes to a `<root-N>/`-prefixed relative
// path in the report output, so a report shipped to a client does not leak
// the operator's local case directory layout (username, engagement
// codename, macOS ~/Library location, etc.). See MFI-XC-04.
type Options struct {
	Unredacted     bool
	AnonymiseRoots []string
}

// BuildOpts is the canonical constructor. Build and BuildWith wrap it for
// backward compatibility.
func BuildOpts(findings []secrets.Finding, d *diff.Result, opts Options) *Report {
	out := make([]secrets.Finding, len(findings))
	for i, f := range findings {
		if !opts.Unredacted {
			f.Secret = ""
		}
		f.Path = anonymisePath(f.Path, opts.AnonymiseRoots)
		out[i] = f
	}
	if d != nil {
		dCopy := *d
		dCopy.RootA = anonymisePath(dCopy.RootA, opts.AnonymiseRoots)
		dCopy.RootB = anonymisePath(dCopy.RootB, opts.AnonymiseRoots)
		dCopy.Changes = make([]diff.Change, len(d.Changes))
		for i, c := range d.Changes {
			c.Path = anonymisePath(c.Path, opts.AnonymiseRoots)
			dCopy.Changes[i] = c
		}
		d = &dCopy
	}
	return &Report{GeneratedAt: time.Now().UTC(), Findings: out, Diff: d, Unredacted: opts.Unredacted}
}

// anonymisePath returns p rewritten as `<root-N>/rel/...` when p starts with
// one of the roots. Roots not matched leave the path unchanged so a bug in
// the caller's root list surfaces as visible absolute paths rather than a
// broken report.
func anonymisePath(p string, roots []string) string {
	if p == "" || len(roots) == 0 {
		return p
	}
	for i, root := range roots {
		if root == "" {
			continue
		}
		rp := root
		if !strings.HasSuffix(rp, "/") && !strings.HasSuffix(rp, `\`) {
			rp += "/"
		}
		if strings.HasPrefix(p, rp) {
			return fmt.Sprintf("<root-%d>/%s", i, p[len(rp):])
		}
		if p == root {
			return fmt.Sprintf("<root-%d>/", i)
		}
	}
	return p
}

// value returns what a finding should display: the raw secret when the report
// is unredacted (and one is present), otherwise the redacted fingerprint.
func (r *Report) value(f secrets.Finding) string {
	if r.Unredacted && f.Secret != "" {
		return f.Secret
	}
	return f.Match
}

// Summary is the headline count of what an inspection turned up.
type Summary struct {
	TotalFindings  int
	FindingsByRule map[string]int
	DiffCounts     map[diff.ChangeKind]int
}

// Summary computes the headline counts from the report.
func (r *Report) Summary() Summary {
	s := Summary{
		TotalFindings:  len(r.Findings),
		FindingsByRule: map[string]int{},
		DiffCounts:     map[diff.ChangeKind]int{},
	}
	for _, f := range r.Findings {
		s.FindingsByRule[f.RuleID]++
	}
	if r.Diff != nil {
		for _, c := range r.Diff.Changes {
			s.DiffCounts[c.Kind]++
		}
	}
	return s
}

// WriteText renders a human-readable report.
func (r *Report) WriteText(w io.Writer) error {
	s := r.Summary()
	if _, err := fmt.Fprintf(w, "MobFI report — %s\n", r.GeneratedAt.Format(time.RFC3339)); err != nil {
		return err
	}

	fmt.Fprintf(w, "\nSecrets: %d finding(s)", s.TotalFindings)
	if r.Unredacted {
		fmt.Fprintf(w, "  [UNREDACTED - contains raw secrets]")
	}
	fmt.Fprintln(w)
	for _, rule := range sortedKeys(s.FindingsByRule) {
		fmt.Fprintf(w, "  %-28s %d\n", rule, s.FindingsByRule[rule])
	}
	for _, f := range r.Findings {
		status := ""
		if f.Verified != "" && f.Verified != secrets.VerifyUnsupported {
			status = "  [" + string(f.Verified) + "]"
		}
		fmt.Fprintf(w, "  - [%s] %s:%d  %s%s\n", f.RuleID, safePath(f.Path), f.Line, r.value(f), status)
	}

	if r.Diff != nil {
		fmt.Fprintf(w, "\nDiff: %d change(s) (added %d, removed %d, modified %d)\n",
			len(r.Diff.Changes), s.DiffCounts[diff.Added], s.DiffCounts[diff.Removed], s.DiffCounts[diff.Modified])
		for _, c := range r.Diff.Changes {
			if c.Detail != "" {
				fmt.Fprintf(w, "  %-8s %s (%s)\n", c.Kind, safePath(c.Path), c.Detail)
			} else {
				fmt.Fprintf(w, "  %-8s %s\n", c.Kind, safePath(c.Path))
			}
		}
	}
	return nil
}

// safePath returns p with control bytes (CR, LF, ANSI escapes, NUL,
// backspace, ...) replaced by their `\xNN` hex escape so a device-supplied
// filename cannot inject terminal escape sequences or forge log lines when
// printed. See MFI-PATH-04. The JSON output preserves the raw bytes for
// forensic use; this sanitiser is only applied to human-facing text
// (terminal, log, plain-text report).
func safePath(p string) string {
	needs := false
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			needs = true
			break
		}
	}
	if !needs {
		return p
	}
	var b strings.Builder
	b.Grow(len(p))
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			fmt.Fprintf(&b, `\x%02X`, r)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// WriteJSON writes the report as indented JSON.
func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// htmlModel is the flattened view the HTML template renders.
type htmlModel struct {
	GeneratedAt   string
	TotalFindings int
	Unredacted    bool
	Verified      bool // any finding carries a live-verification status
	RuleRows      []ruleCount
	Findings      []findingRow
	HasDiff       bool
	RootA, RootB  string
	Added         int
	Removed       int
	Modified      int
	Changes       []diff.Change
}

type ruleCount struct {
	Rule  string
	Count int
}

// findingRow is one secret finding as shown in the report, with Value already
// resolved to the raw or redacted form per the report's redaction state.
type findingRow struct {
	RuleID   string
	Path     string
	Line     int
	Value    string
	Verified string // live-verification status ("active"/"inactive"/... or "")
}

// WriteHTML renders the report as a self-contained HTML document (inline CSS,
// no external assets). html/template escapes all values, so the untrusted
// paths and matches from extracted data are safe to embed.
func (r *Report) WriteHTML(w io.Writer) error {
	s := r.Summary()
	m := htmlModel{
		GeneratedAt:   r.GeneratedAt.Format(time.RFC1123),
		TotalFindings: s.TotalFindings,
		Unredacted:    r.Unredacted,
	}
	for _, f := range r.Findings {
		vs := ""
		if f.Verified != secrets.VerifyUnsupported {
			vs = string(f.Verified)
		}
		if vs != "" {
			m.Verified = true
		}
		m.Findings = append(m.Findings, findingRow{RuleID: f.RuleID, Path: f.Path, Line: f.Line, Value: r.value(f), Verified: vs})
	}
	for _, rule := range sortedKeys(s.FindingsByRule) {
		m.RuleRows = append(m.RuleRows, ruleCount{Rule: rule, Count: s.FindingsByRule[rule]})
	}
	if r.Diff != nil {
		m.HasDiff = true
		m.RootA, m.RootB = r.Diff.RootA, r.Diff.RootB
		m.Added = s.DiffCounts[diff.Added]
		m.Removed = s.DiffCounts[diff.Removed]
		m.Modified = s.DiffCounts[diff.Modified]
		m.Changes = r.Diff.Changes
	}
	return htmlTmpl.Execute(w, m)
}

var htmlTmpl = template.Must(template.New("report").Parse(htmlTemplate))

const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>MobFI report</title>
<style>
  :root { --bg:#0f1419; --panel:#171d26; --line:#2a3441; --text:#d7dee7; --muted:#8695a8; --ok:#46c98b; --danger:#ff6b6b; --warn:#e2b93b; }
  * { box-sizing:border-box; }
  body { margin:0; padding:32px; background:var(--bg); color:var(--text); font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif; }
  h1 { font-size:20px; margin:0 0 4px; }
  h2 { font-size:13px; color:var(--muted); text-transform:uppercase; letter-spacing:.5px; margin:28px 0 10px; border-bottom:1px solid var(--line); padding-bottom:6px; }
  .meta { color:var(--muted); font-size:12px; margin-bottom:8px; }
  .cards { display:flex; gap:12px; flex-wrap:wrap; margin:12px 0; }
  .card { background:var(--panel); border:1px solid var(--line); border-radius:10px; padding:10px 16px; min-width:110px; }
  .card .n { font-size:22px; font-weight:700; }
  .card .l { color:var(--muted); font-size:12px; }
  table { width:100%; border-collapse:collapse; margin-top:8px; font-size:13px; }
  th,td { text-align:left; padding:7px 10px; border-bottom:1px solid var(--line); vertical-align:top; }
  th { color:var(--muted); font-weight:600; }
  td.path, td.match, code { font-family:ui-monospace,"SF Mono",Menlo,monospace; font-size:12px; word-break:break-all; }
  .badge { display:inline-block; padding:1px 8px; border-radius:999px; font-size:11px; font-weight:600; border:1px solid var(--line); }
  .k-added { color:var(--ok); border-color:var(--ok); }
  .k-removed { color:var(--danger); border-color:var(--danger); }
  .k-modified { color:var(--warn); border-color:var(--warn); }
  .empty { color:var(--muted); font-style:italic; }
  .v-active { color:var(--danger); font-weight:700; }
  .v-inactive { color:var(--muted); }
  .v-unknown { color:var(--warn); }
  .warnbar { margin:12px 0; padding:10px 14px; border-radius:8px; background:rgba(255,107,107,.12); border:1px solid var(--danger); color:var(--danger); font-size:13px; }
  footer { margin-top:32px; color:var(--muted); font-size:11px; }
</style>
</head>
<body>
  <h1>MobFI report</h1>
  <div class="meta">Generated {{.GeneratedAt}}</div>
  {{if .Unredacted}}<div class="warnbar">This report contains <strong>UNREDACTED</strong> raw secrets. Handle and store it securely; do not share.</div>{{end}}

  <h2>Secrets</h2>
  <div class="cards">
    <div class="card"><div class="n">{{.TotalFindings}}</div><div class="l">finding(s)</div></div>
    {{range .RuleRows}}<div class="card"><div class="n">{{.Count}}</div><div class="l">{{.Rule}}</div></div>{{end}}
  </div>
  {{if .Findings}}
  <table>
    <thead><tr><th>Rule</th><th>Path</th><th>Line</th><th>Match</th>{{if .Verified}}<th>Status</th>{{end}}</tr></thead>
    <tbody>
    {{range .Findings}}<tr><td><code>{{.RuleID}}</code></td><td class="path">{{.Path}}</td><td>{{.Line}}</td><td class="match">{{.Value}}</td>{{if $.Verified}}<td>{{if .Verified}}<span class="v-{{.Verified}}">{{.Verified}}</span>{{end}}</td>{{end}}</tr>{{end}}
    </tbody>
  </table>
  {{else}}<p class="empty">No secrets found.</p>{{end}}

  {{if .HasDiff}}
  <h2>Diff</h2>
  <div class="meta">A: <code>{{.RootA}}</code> &nbsp; B: <code>{{.RootB}}</code></div>
  <div class="cards">
    <div class="card"><div class="n">{{.Added}}</div><div class="l">added</div></div>
    <div class="card"><div class="n">{{.Removed}}</div><div class="l">removed</div></div>
    <div class="card"><div class="n">{{.Modified}}</div><div class="l">modified</div></div>
  </div>
  {{if .Changes}}
  <table>
    <thead><tr><th>Kind</th><th>Path</th><th>Detail</th></tr></thead>
    <tbody>
    {{range .Changes}}<tr><td><span class="badge k-{{.Kind}}">{{.Kind}}</span></td><td class="path">{{.Path}}</td><td>{{.Detail}}</td></tr>{{end}}
    </tbody>
  </table>
  {{else}}<p class="empty">No differences.</p>{{end}}
  {{end}}

  <footer>Generated by MobFI · {{if .Unredacted}}secrets shown UNREDACTED{{else}}discovered secrets are redacted{{end}}</footer>
</body>
</html>
`

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
