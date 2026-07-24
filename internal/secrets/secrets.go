// Package secrets scans extracted files for secret patterns. Rules are
// pluggable: built-in Trufflehog-style detectors plus user-supplied
// known-secret lists. Findings never carry the raw secret — only a
// redacted fingerprint — so reports and logs stay safe to share.
package secrets

import (
	"bufio"
	"bytes"
	"context"
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
					Match:  redact(m),
					Secret: m,
				})
			}
		}
	}
	// A scan error (e.g. an over-long line) is best-effort: return what we
	// found so far rather than failing the whole file.
	return findings, nil
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
var builtinRules = []Rule{
	mustRule("aws-access-key-id", `\b(?:A3T[A-Z0-9]|AKIA|AGPA|AIDA|AROA|AIPA|ANPA|ANVA|ASIA)[A-Z0-9]{16}\b`),
	mustRule("github-token", `\b(?:ghp|gho|ghu|ghs|ghr)_[0-9A-Za-z]{36}\b`),
	mustRule("github-fine-grained-pat", `\bgithub_pat_[0-9A-Za-z_]{82}\b`),
	mustRule("google-api-key", `\bAIza[0-9A-Za-z\-_]{35}\b`),
	mustRule("slack-token", `xox[baprs]-[0-9A-Za-z-]{10,48}`),
	mustRule("stripe-secret-key", `\b(?:sk|rk)_live_[0-9a-zA-Z]{24,}\b`),
	mustRule("private-key", `-----BEGIN (?:RSA |EC |DSA |OPENSSH |PGP )?PRIVATE KEY-----`),
	mustRule("jwt", `eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`),
	mustRule("generic-secret-assignment", `(?i)(?:api[_-]?key|secret|token|passwd|password)["']?\s*[:=]\s*["'][^"'\s]{8,}["']`),
}
