// Package report aggregates scan and diff results into an actionable
// summary that any frontend can render as text or export as JSON.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/integrisec/MobFI/internal/diff"
	"github.com/integrisec/MobFI/internal/secrets"
)

// Report is the actionable summary of an inspection session. Findings
// already carry only redacted secrets, so a report is safe to export.
type Report struct {
	GeneratedAt time.Time         `json:"generated_at"`
	Findings    []secrets.Finding `json:"findings,omitempty"`
	Diff        *diff.Result      `json:"diff,omitempty"`
}

// Build assembles a report from the collected findings and diff. Raw
// secrets are stripped so the report (text or JSON) is safe to share.
func Build(findings []secrets.Finding, d *diff.Result) *Report {
	safe := make([]secrets.Finding, len(findings))
	for i, f := range findings {
		f.Secret = ""
		safe[i] = f
	}
	return &Report{GeneratedAt: time.Now().UTC(), Findings: safe, Diff: d}
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

	fmt.Fprintf(w, "\nSecrets: %d finding(s)\n", s.TotalFindings)
	for _, rule := range sortedKeys(s.FindingsByRule) {
		fmt.Fprintf(w, "  %-28s %d\n", rule, s.FindingsByRule[rule])
	}
	for _, f := range r.Findings {
		fmt.Fprintf(w, "  - [%s] %s:%d  %s\n", f.RuleID, f.Path, f.Line, f.Match)
	}

	if r.Diff != nil {
		fmt.Fprintf(w, "\nDiff: %d change(s) (added %d, removed %d, modified %d)\n",
			len(r.Diff.Changes), s.DiffCounts[diff.Added], s.DiffCounts[diff.Removed], s.DiffCounts[diff.Modified])
		for _, c := range r.Diff.Changes {
			if c.Detail != "" {
				fmt.Fprintf(w, "  %-8s %s (%s)\n", c.Kind, c.Path, c.Detail)
			} else {
				fmt.Fprintf(w, "  %-8s %s\n", c.Kind, c.Path)
			}
		}
	}
	return nil
}

// WriteJSON writes the report as indented JSON.
func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
