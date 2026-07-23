// Package diff compares two extracted file roots at a native/semantic
// level: raw bytes for opaque files, but structural diffs for SQLite,
// XML/plist and config formats.
package diff

import (
	"context"
	"errors"
)

// ChangeKind classifies a single difference.
type ChangeKind string

const (
	Added    ChangeKind = "added"
	Removed  ChangeKind = "removed"
	Modified ChangeKind = "modified"
)

// Change is one difference between the two roots.
type Change struct {
	Path string
	Kind ChangeKind
	// Detail carries a format-specific description (changed rows, keys,
	// elements) for files diffed structurally.
	Detail string
}

// Result is the full comparison of two roots.
type Result struct {
	RootA, RootB string
	Changes      []Change
}

// Trees diffs the file trees rooted at a and b.
func Trees(ctx context.Context, a, b string) (*Result, error) {
	// TODO: pair files across roots, dispatch each pair to a format-aware
	// differ, and fall back to byte comparison.
	_ = ctx
	_ = a
	_ = b
	return nil, ErrNotImplemented
}

// ErrNotImplemented marks scaffolded behaviour that is not built yet.
var ErrNotImplemented = errors.New("not implemented")
