package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/integrisec/MobFI/internal/diff"
	"github.com/integrisec/MobFI/internal/secrets"
)

func sampleReport() *Report {
	findings := []secrets.Finding{
		{RuleID: "aws-access-key-id", Path: "a.txt", Line: 1, Match: "AKIA…(20 chars)", Secret: "AKIAIOSFODNN7EXAMPLE"},
		{RuleID: "aws-access-key-id", Path: "b.txt", Line: 2, Match: "AKIA…(20 chars)", Secret: "AKIAIOSFODNN7EXAMPLE"},
		{RuleID: "github-token", Path: "c.txt", Line: 3, Match: "ghp_…(40 chars)", Secret: "ghp_rawtokenvalue"},
	}
	d := &diff.Result{RootA: "a", RootB: "b", Changes: []diff.Change{
		{Path: "x", Kind: diff.Added},
		{Path: "y", Kind: diff.Modified, Detail: "content differs"},
	}}
	return Build(findings, d)
}

func TestWriteHTML(t *testing.T) {
	var buf bytes.Buffer
	if err := sampleReport().WriteHTML(&buf); err != nil {
		t.Fatalf("WriteHTML: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"<!DOCTYPE html>", "MobFI report", "aws-access-key-id", "github-token", "content differs", "k-added", "k-modified"} {
		if !strings.Contains(out, want) {
			t.Errorf("HTML report missing %q", want)
		}
	}
	// Build() strips raw secrets; they must never reach the export.
	if strings.Contains(out, "AKIAIOSFODNN7EXAMPLE") || strings.Contains(out, "ghp_rawtokenvalue") {
		t.Error("HTML report leaked a raw secret")
	}
}

func TestWriteHTMLEscapes(t *testing.T) {
	// Untrusted path/match from extracted data must be HTML-escaped.
	r := Build([]secrets.Finding{
		{RuleID: "x", Path: "<script>alert(1)</script>", Line: 1, Match: "a&b"},
	}, nil)
	var buf bytes.Buffer
	if err := r.WriteHTML(&buf); err != nil {
		t.Fatalf("WriteHTML: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "<script>alert(1)</script>") {
		t.Error("path was not HTML-escaped (XSS risk)")
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Error("expected escaped path in output")
	}
}

func TestSummary(t *testing.T) {
	s := sampleReport().Summary()
	if s.TotalFindings != 3 {
		t.Errorf("TotalFindings = %d, want 3", s.TotalFindings)
	}
	if s.FindingsByRule["aws-access-key-id"] != 2 || s.FindingsByRule["github-token"] != 1 {
		t.Errorf("FindingsByRule = %v", s.FindingsByRule)
	}
	if s.DiffCounts[diff.Added] != 1 || s.DiffCounts[diff.Modified] != 1 {
		t.Errorf("DiffCounts = %v", s.DiffCounts)
	}
}

func TestWriteText(t *testing.T) {
	var buf bytes.Buffer
	if err := sampleReport().WriteText(&buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"Secrets: 3 finding(s)", "aws-access-key-id", "2 change(s)", "content differs"} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q:\n%s", want, out)
		}
	}
}

func TestBuildStripsRawSecrets(t *testing.T) {
	rep := sampleReport()
	for _, f := range rep.Findings {
		if f.Secret != "" {
			t.Errorf("report retained a raw secret: %+v", f)
		}
	}
	// The redacted Match is preserved.
	if rep.Findings[0].Match == "" {
		t.Error("expected the redacted Match to be preserved")
	}
}

func TestWriteJSONRoundTrips(t *testing.T) {
	var buf bytes.Buffer
	if err := sampleReport().WriteJSON(&buf); err != nil {
		t.Fatal(err)
	}
	var got Report
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got.Findings) != 3 {
		t.Errorf("round-tripped findings = %d, want 3", len(got.Findings))
	}
	if got.Diff == nil || len(got.Diff.Changes) != 2 {
		t.Errorf("round-tripped diff = %+v", got.Diff)
	}
}
