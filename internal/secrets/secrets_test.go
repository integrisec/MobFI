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

// TestBuiltinDetectors checks every built-in rule matches a format-valid sample
// and that none fire on benign text (a basic precision/regression guard).
func TestBuiltinDetectors(t *testing.T) {
	r := strings.Repeat
	samples := map[string]string{
		"aws-access-key-id":         "AKIA" + r("A", 16),
		"gcp-api-key":               "AIza" + r("a", 35),
		"gcp-oauth-client-secret":   "GOCSPX-" + r("a", 28),
		"gcp-service-account-key":   `"private_key_id": "` + r("a", 40) + `"`,
		"digitalocean-token":        "dop_v1_" + r("a", 64),
		"databricks-token":          "dapi" + r("a", 32),
		"doppler-token":             "dp.pt." + r("a", 42),
		"terraform-cloud-token":     r("A", 14) + ".atlasv1." + r("a", 65),
		"github-token":              "ghp_" + r("a", 36),
		"github-fine-grained-pat":   "github_pat_" + r("a", 82),
		"gitlab-pat":                "glpat-" + r("a", 20),
		"npm-token":                 "npm_" + r("a", 36),
		"pypi-token":                "pypi-AgEI" + r("a", 55),
		"postman-api-key":           "PMAK-" + r("a", 24) + "-" + r("a", 34),
		"openai-api-key":            "sk-" + r("a", 20) + "T3BlbkFJ" + r("a", 20),
		"anthropic-api-key":         "sk-ant-api03-" + r("a", 90),
		"huggingface-token":         "hf_" + r("a", 34),
		"stripe-secret-key":         "sk_live_" + r("a", 24),
		"square-access-token":       "EAAA" + r("a", 60),
		"braintree-access-token":    "access_token$production$" + r("a", 16) + "$" + r("a", 32),
		"slack-token":               "xoxb-" + r("1", 30),
		"slack-app-token":           "xapp-1-ABC123-1234567890-" + r("a", 12),
		"slack-webhook":             "https://hooks.slack.com/services/T00000000/B00000000/" + r("X", 24),
		"discord-bot-token":         "M" + r("a", 23) + "." + r("a", 6) + "." + r("a", 30),
		"discord-webhook":           "https://discord.com/api/webhooks/12345678901234567/" + r("a", 64),
		"telegram-bot-token":        "1234567890:" + r("a", 35),
		"twilio-api-key":            "SK" + r("a", 32),
		"sendgrid-api-key":          "SG." + r("a", 22) + "." + r("a", 43),
		"mailgun-api-key":           "key-" + r("a", 32),
		"mailchimp-api-key":         r("a", 32) + "-us1",
		"shopify-token":             "shpat_" + r("a", 32),
		"notion-token":              "secret_" + r("a", 43),
		"linear-api-key":            "lin_api_" + r("a", 40),
		"airtable-token":            "pat" + r("a", 14) + "." + r("a", 64),
		"newrelic-api-key":          "NRAK-" + r("A", 27),
		"grafana-token":             "glsa_" + r("a", 40),
		"jwt":                       "eyJ" + r("a", 12) + "." + r("a", 12) + "." + r("a", 12),
		"private-key":               "-----BEGIN RSA PRIVATE KEY-----",
		"mongodb-uri":               "mongodb+srv://user:pass@cluster.example.net",
		"sql-uri":                   "postgres://user:pass@host:5432/db",
		"redis-uri":                 "redis://:pass@host:6379",
		"basic-auth-url":            "https://user:pass@example.com/x",
		"generic-secret-assignment": `api_key = "abcdef12345678"`,
		"bearer-token":              "Authorization: Bearer abcdefghij1234567890XYZ",
	}

	for _, rule := range DefaultRules() {
		s, ok := samples[rule.ID]
		if !ok {
			t.Errorf("no positive sample for rule %q (add one when adding a rule)", rule.ID)
			continue
		}
		if !rule.Pattern.MatchString(s) {
			t.Errorf("rule %q did not match its sample %q", rule.ID, s)
		}
	}

	benign := "the quick brown fox jumps over the lazy dog 12345\n" +
		"https://example.com/docs?page=2\n" +
		"let total = compute(a, b, c); version = 1.2.3\n"
	for _, rule := range DefaultRules() {
		if loc := rule.Pattern.FindString(benign); loc != "" {
			t.Errorf("rule %q false-positived on benign text: %q", rule.ID, loc)
		}
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
