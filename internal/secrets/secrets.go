// Package secrets scans extracted files for secret patterns. Rules are
// pluggable: built-in Trufflehog-style detectors plus user-supplied
// known-secret lists.
package secrets

import (
	"context"
	"errors"
	"regexp"
)

// Rule matches a class of secret.
type Rule struct {
	ID      string
	Pattern *regexp.Regexp
}

// Finding is one secret match located during a scan.
type Finding struct {
	RuleID string
	Path   string // file the match was found in
	Line   int
	Match  string // redacted/truncated snippet
}

// Scanner applies a set of rules across a file tree.
type Scanner struct {
	rules []Rule
}

// NewScanner builds a scanner from the given rules.
func NewScanner(rules []Rule) *Scanner { return &Scanner{rules: rules} }

// DefaultRules returns the built-in detector set.
// TODO: port/import Trufflehog detectors here.
func DefaultRules() []Rule { return nil }

// LoadKnownSecrets turns a user-supplied list of literal secrets (one per
// line) into exact-match rules.
func LoadKnownSecrets(path string) ([]Rule, error) {
	// TODO: read path and compile literal-match rules.
	_ = path
	return nil, ErrNotImplemented
}

// ScanTree walks root and returns every match.
func (s *Scanner) ScanTree(ctx context.Context, root string) ([]Finding, error) {
	// TODO: walk root and stream file contents through s.rules.
	_ = ctx
	_ = root
	return nil, ErrNotImplemented
}

// ErrNotImplemented marks scaffolded behaviour that is not built yet.
var ErrNotImplemented = errors.New("not implemented")
