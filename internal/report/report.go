// Package report aggregates scan and diff results into an actionable
// summary that any frontend can render or export.
package report

import (
	"github.com/integrisec/MobFI/internal/diff"
	"github.com/integrisec/MobFI/internal/secrets"
)

// Report is the actionable summary of an inspection session.
type Report struct {
	Findings []secrets.Finding
	Diff     *diff.Result
}

// Build assembles a report from the collected findings and diff.
func Build(findings []secrets.Finding, d *diff.Result) *Report {
	return &Report{Findings: findings, Diff: d}
}
