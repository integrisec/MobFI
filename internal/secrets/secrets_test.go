package secrets

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const awsKey = "AKIAIOSFODNN7EXAMPLE" // 20 chars

// ghp_ + exactly 36 base62 chars.
var ghToken = "ghp_" + strings.Repeat("a", 36)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestScanTreeFindsAndRedacts(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.txt", "aws_key = "+awsKey+"\n")
	writeFile(t, dir, "token.env", "GH_TOKEN="+ghToken+"\n")
	writeFile(t, dir, "safe.txt", "nothing secret here\n")

	findings, err := NewScanner(DefaultRules()).ScanTree(context.Background(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}

	byRule := map[string]Finding{}
	for _, f := range findings {
		byRule[f.RuleID] = f
	}
	if _, ok := byRule["aws-access-key-id"]; !ok {
		t.Errorf("expected an AWS key finding, got %+v", findings)
	}
	if _, ok := byRule["github-token"]; !ok {
		t.Errorf("expected a GitHub token finding, got %+v", findings)
	}

	// The raw secret must never appear in the redacted Match...
	for _, f := range findings {
		if strings.Contains(f.Match, "IOSFODNN7EXAMPLE") || strings.Contains(f.Match, ghToken) {
			t.Errorf("finding leaked the raw secret in Match: %q", f.Match)
		}
	}
	// ...but Secret carries the raw value for reveal/copy.
	if got := byRule["aws-access-key-id"]; got.Secret != awsKey {
		t.Errorf("Secret = %q, want the raw %q", got.Secret, awsKey)
	}
}

func TestScanSkipsBinary(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "blob.bin", "\x00\x01"+ghToken)

	findings, err := NewScanner(DefaultRules()).ScanTree(context.Background(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Errorf("binary file should be skipped, got %+v", findings)
	}
}

func TestLoadKnownSecrets(t *testing.T) {
	dir := t.TempDir()
	knownPath := filepath.Join(dir, "known.txt")
	// Includes regex metacharacters to confirm they are matched literally.
	writeFile(t, dir, "known.txt", "# my secrets\ns3cr3t.value*\n\n")

	rules, err := LoadKnownSecrets(knownPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].ID != "known-secret" {
		t.Fatalf("rules = %+v", rules)
	}

	scanDir := t.TempDir()
	writeFile(t, scanDir, "app.conf", "password: s3cr3t.value*\n")
	writeFile(t, scanDir, "other.conf", "password: s3cr3tXvalueY\n") // must NOT match

	findings, err := NewScanner(rules).ScanTree(context.Background(), scanDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("want 1 known-secret finding, got %+v", findings)
	}
	if findings[0].RuleID != "known-secret" || !strings.HasSuffix(findings[0].Path, "app.conf") {
		t.Errorf("unexpected finding: %+v", findings[0])
	}
}
