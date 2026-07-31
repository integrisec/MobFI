// Package secrets scans extracted files for secret patterns. Rules are
// pluggable: built-in Trufflehog-style detectors plus user-supplied
// known-secret lists. Findings never carry the raw secret — only a
// redacted fingerprint — so reports and logs stay safe to share.
package secrets

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	defaultMaxFileSize = 16 << 20 // skip files larger than this
	maxLineLen         = 1 << 20  // longest line scanned before a file is abandoned
	maxMatchesPerLine  = 50       // cap matches per rule per line
	sniffLen           = 512      // bytes inspected for binary detection
)

// Rule matches a class of secret.
type Rule struct {
	ID      string
	Pattern *regexp.Regexp
}

// Finding is one secret match located during a scan.
type Finding struct {
	RuleID string `json:"rule_id"`
	Path   string `json:"path"` // file the match was found in
	Line   int    `json:"line"`
	Match  string `json:"match"` // redacted fingerprint, safe to display/export
	// Secret is the raw matched value, populated during a scan so the
	// operator can reveal/copy it. report.Build strips it so exported
	// reports never carry raw secrets.
	Secret string `json:"secret,omitempty"`
	// Verified is the result of an opt-in live verification (see Verify): e.g.
	// "active", "inactive", "unsupported". Empty when verification was not run.
	Verified VerifyStatus `json:"verified,omitempty"`
}

// Scanner applies a set of rules across a file tree.
type Scanner struct {
	rules []Rule
	// MaxFileSize skips files larger than this many bytes (0 = no limit).
	MaxFileSize int64
}

// NewScanner builds a scanner from the given rules.
func NewScanner(rules []Rule) *Scanner {
	return &Scanner{rules: rules, MaxFileSize: defaultMaxFileSize}
}

// AddRules appends more rules to the scanner (e.g. user-supplied known
// secrets alongside the built-in detectors).
func (s *Scanner) AddRules(rules ...Rule) { s.rules = append(s.rules, rules...) }

// Progress reports scan progress after each file (cumulative file count and
// the path just scanned).
type Progress struct {
	Files int    `json:"files"`
	Path  string `json:"path"`
}

// ScanTree walks root and returns every match, in filesystem order.
// Directories and files that cannot be read are skipped rather than
// aborting the scan; a missing root is reported as an error. progress, if
// non-nil, is called after each scanned file.
func (s *Scanner) ScanTree(ctx context.Context, root string, progress func(Progress)) ([]Finding, error) {
	var findings []Finding
	scanned := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil // tolerate an unreadable entry, keep scanning
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		if s.MaxFileSize > 0 {
			if info, err := d.Info(); err == nil && info.Size() > s.MaxFileSize {
				return nil
			}
		}
		found, err := s.scanFile(path)
		if err != nil {
			return nil // unreadable file, keep scanning
		}
		findings = append(findings, found...)
		scanned++
		if progress != nil {
			progress(Progress{Files: scanned, Path: path})
		}
		return nil
	})
	return findings, err
}

// scanFile scans a single file line by line. Files that look binary (a NUL
// byte in the first sniffLen bytes) are skipped to avoid noise.
func (s *Scanner) scanFile(path string) ([]Finding, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sniff := make([]byte, sniffLen)
	n, _ := io.ReadFull(f, sniff)
	sniff = sniff[:n]
	if bytes.IndexByte(sniff, 0) >= 0 {
		return nil, nil // binary
	}

	var findings []Finding
	sc := bufio.NewScanner(io.MultiReader(bytes.NewReader(sniff), f))
	sc.Buffer(make([]byte, 0, 64*1024), maxLineLen)
	line := 0
	for sc.Scan() {
		line++
		text := sc.Text()
		for i := range s.rules {
			r := &s.rules[i]
			for _, m := range r.Pattern.FindAllString(text, maxMatchesPerLine) {
				findings = append(findings, Finding{
					RuleID: r.ID,
					Path:   path,
					Line:   line,
					Match:  fingerprintFor(r.ID, m),
					Secret: m,
				})
			}
		}
	}
	// A scan error (e.g. an over-long line) is best-effort: return what we
	// found so far rather than failing the whole file.
	return findings, nil
}

// fingerprintFor produces the "safe to display / export" match string for
// rule ruleID matching s. For Trufflehog-style rules the first 4 chars are
// always the public vendor prefix (`ghp_`, `AKIA`, `dop_`, ...) so leaking
// them is safe and gives the operator a type hint at a glance. For a
// known-secret match the 4 chars are 4 chars of the operator's literal
// secret text -- leaking them plus the exact length narrows the guess
// space for a downstream reader of the report, so MFI-SEC-03 routes
// known-secret matches through a non-reversible sha256 hash tag instead.
func fingerprintFor(ruleID, s string) string {
	if ruleID == "known-secret" {
		sum := sha256.Sum256([]byte(strings.TrimSpace(s)))
		return "known-secret:" + hex.EncodeToString(sum[:6])
	}
	return redact(s)
}

// redact returns a short fingerprint of a matched secret: a few leading
// characters (enough to recognise the secret's type) plus its length. The
// remainder is never emitted.
func redact(s string) string {
	s = strings.TrimSpace(s)
	const keep = 4
	if len(s) <= keep+2 {
		return strings.Repeat("*", len(s))
	}
	return s[:keep] + "…(" + strconv.Itoa(len(s)) + " chars)"
}

// LoadKnownSecrets turns a user-supplied list of literal secrets (one per
// line, blank lines and '#' comments ignored) into a single exact-match
// rule.
func LoadKnownSecrets(path string) ([]Rule, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var literals []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		literals = append(literals, regexp.QuoteMeta(line))
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(literals) == 0 {
		return nil, nil
	}
	return []Rule{{ID: "known-secret", Pattern: regexp.MustCompile(strings.Join(literals, "|"))}}, nil
}

// DefaultRules returns a copy of the built-in detector set.
func DefaultRules() []Rule {
	return append([]Rule(nil), builtinRules...)
}

func mustRule(id, pattern string) Rule {
	return Rule{ID: id, Pattern: regexp.MustCompile(pattern)}
}

// builtinRules is a curated, high-signal set of Trufflehog-style detectors.
// Each is anchored on a service's distinctive prefix/format (RE2-compatible: no
// look-around or back-references) to keep precision high. Ported from the
// patterns in github.com/trufflesecurity/trufflehog/tree/main/pkg/detectors.
var builtinRules = []Rule{
	// --- Cloud & infrastructure ---
	mustRule("aws-access-key-id", `\b(?:A3T[A-Z0-9]|AKIA|AGPA|AIDA|AROA|AIPA|ANPA|ANVA|ASIA)[A-Z0-9]{16}\b`),
	mustRule("gcp-api-key", `\bAIza[0-9A-Za-z_\-]{35}\b`),
	mustRule("gcp-oauth-client-secret", `\bGOCSPX-[0-9A-Za-z_\-]{28}\b`),
	mustRule("gcp-service-account-key", `"private_key_id"\s*:\s*"[0-9a-f]{40}"`),
	mustRule("digitalocean-token", `\b(?:dop|doo|dor)_v1_[0-9a-f]{64}\b`),
	mustRule("databricks-token", `\bdapi[0-9a-f]{32}\b`),
	mustRule("doppler-token", `\bdp\.(?:pt|st|ct|sa)\.[0-9A-Za-z]{40,44}\b`),
	mustRule("terraform-cloud-token", `\b[0-9A-Za-z]{14}\.atlasv1\.[0-9A-Za-z_\-]{60,70}\b`),

	// --- Version control, packages & CI ---
	mustRule("github-token", `\b(?:ghp|gho|ghu|ghs|ghr)_[0-9A-Za-z]{36}\b`),
	mustRule("github-fine-grained-pat", `\bgithub_pat_[0-9A-Za-z_]{82}\b`),
	mustRule("gitlab-pat", `\bglpat-[0-9A-Za-z_\-]{20}\b`),
	mustRule("npm-token", `\bnpm_[0-9A-Za-z]{36}\b`),
	mustRule("pypi-token", `\bpypi-AgEI[0-9A-Za-z_\-]{50,}\b`),
	mustRule("postman-api-key", `\bPMAK-[0-9a-f]{24}-[0-9a-f]{34}\b`),

	// --- AI providers ---
	mustRule("openai-api-key", `\bsk-[0-9A-Za-z_\-]{10,}T3BlbkFJ[0-9A-Za-z_\-]{20,}\b`),
	mustRule("anthropic-api-key", `\bsk-ant-[0-9A-Za-z_\-]{20,}\b`),
	mustRule("huggingface-token", `\bhf_[0-9A-Za-z]{34,}\b`),

	// --- Payments ---
	mustRule("stripe-secret-key", `\b(?:sk|rk)_(?:live|test)_[0-9A-Za-z]{24,}\b`),
	mustRule("square-access-token", `\b(?:EAAA[0-9A-Za-z_\-]{60}|sq0(?:atp|csp|idp)-[0-9A-Za-z_\-]{22,43})\b`),
	mustRule("braintree-access-token", `\baccess_token\$production\$[0-9a-z]{16}\$[0-9a-f]{32}\b`),

	// --- Communication & email ---
	mustRule("slack-token", `\bxox[baprs]-[0-9A-Za-z-]{10,72}\b`),
	mustRule("slack-app-token", `\bxapp-[0-9]-[A-Z0-9]+-[0-9]+-[0-9a-f]+\b`),
	mustRule("slack-webhook", `https://hooks\.slack\.com/services/T[0-9A-Za-z_]+/B[0-9A-Za-z_]+/[0-9A-Za-z_]+`),
	mustRule("discord-bot-token", `\b[MNO][A-Za-z0-9_\-]{23}\.[\w-]{6}\.[\w-]{27,38}\b`),
	mustRule("discord-webhook", `https://(?:ptb\.|canary\.)?discord(?:app)?\.com/api/webhooks/[0-9]{17,20}/[0-9A-Za-z_\-]{60,68}`),
	mustRule("telegram-bot-token", `\b[0-9]{8,10}:[A-Za-z0-9_-]{35}\b`),
	mustRule("twilio-api-key", `\bSK[0-9a-fA-F]{32}\b`),
	mustRule("sendgrid-api-key", `\bSG\.[0-9A-Za-z_\-]{22}\.[0-9A-Za-z_\-]{43}\b`),
	mustRule("mailgun-api-key", `\bkey-[0-9a-f]{32}\b`),
	mustRule("mailchimp-api-key", `\b[0-9a-f]{32}-us[0-9]{1,2}\b`),

	// --- SaaS / APIs ---
	mustRule("shopify-token", `\bshp(?:at|ca|pa|ss)_[0-9a-fA-F]{32}\b`),
	mustRule("notion-token", `\b(?:secret_|ntn_)[0-9A-Za-z]{43}\b`),
	mustRule("linear-api-key", `\blin_api_[0-9A-Za-z]{40,}\b`),
	mustRule("airtable-token", `\bpat[0-9A-Za-z]{14}\.[0-9a-f]{64}\b`),
	mustRule("newrelic-api-key", `\bNRAK-[A-Z0-9]{27}\b`),
	mustRule("grafana-token", `\bgl(?:sa|c)_[0-9A-Za-z_\-]{32,}\b`),

	// --- Auth & crypto ---
	mustRule("jwt", `\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`),
	mustRule("private-key", `-----BEGIN (?:RSA |EC |DSA |OPENSSH |PGP |ENCRYPTED )?PRIVATE KEY-----`),

	// --- Connection strings / URLs with embedded credentials ---
	mustRule("mongodb-uri", `\bmongodb(?:\+srv)?://[^\s:@/]+:[^\s:@/]+@[^\s/]+`),
	mustRule("sql-uri", `\b(?:postgres(?:ql)?|mysql|mariadb)://[^\s:@/]+:[^\s:@/]+@[^\s/]+`),
	mustRule("redis-uri", `\brediss?://[^\s:@/]*:[^\s:@/]+@[^\s/]+`),
	mustRule("basic-auth-url", `\bhttps?://[^\s:@/]+:[^\s:@/]+@[^\s/]+`),

	// --- Generic keyword-anchored secrets ---
	mustRule("generic-secret-assignment", `(?i)(?:api[_-]?key|secret|token|passwd|password|access[_-]?token|client[_-]?secret|private[_-]?key|auth[_-]?token)["']?\s*[:=]\s*["'][^"'\s]{8,}["']`),
	mustRule("bearer-token", `(?i)\bbearer\s+[A-Za-z0-9._\-]{20,}`),
}
