// Package diff compares two extracted file roots. Today it diffs the file
// trees and file contents (size, then SHA-256). Format-aware structural
// diffing (SQLite, XML/plist, config) plugs in at the marked seam in
// compare.
package diff

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
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
	Path string     `json:"path"`
	Kind ChangeKind `json:"kind"`
	// Detail carries a format-specific description (changed rows, keys,
	// elements) for files diffed structurally.
	Detail string `json:"detail,omitempty"`
}

// Result is the full comparison of two roots.
type Result struct {
	RootA   string   `json:"root_a"`
	RootB   string   `json:"root_b"`
	Changes []Change `json:"changes"`
}

// Trees diffs the file trees rooted at a and b. Changes are returned sorted
// by path. Files present only under a are Removed, only under b are Added,
// and files present in both whose contents differ are Modified.
func Trees(ctx context.Context, a, b string) (*Result, error) {
	ia, err := indexTree(a)
	if err != nil {
		return nil, err
	}
	ib, err := indexTree(b)
	if err != nil {
		return nil, err
	}

	res := &Result{RootA: a, RootB: b}
	for _, rel := range unionKeys(ia, ib) {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		ma, inA := ia[rel]
		mb, inB := ib[rel]
		switch {
		case inA && !inB:
			res.Changes = append(res.Changes, Change{Path: rel, Kind: Removed})
		case !inA && inB:
			res.Changes = append(res.Changes, Change{Path: rel, Kind: Added})
		default:
			changed, detail, err := compare(ma, mb)
			if err != nil {
				res.Changes = append(res.Changes, Change{Path: rel, Kind: Modified, Detail: "unreadable: " + err.Error()})
				continue
			}
			if changed {
				res.Changes = append(res.Changes, Change{Path: rel, Kind: Modified, Detail: detail})
			}
		}
	}
	return res, nil
}

// fileMeta is what the diff needs to know about a file without reading it.
type fileMeta struct {
	full string // absolute/host path
	size int64
}

// indexTree maps every regular file under root to its metadata, keyed by
// slash-separated path relative to root.
func indexTree(root string) (map[string]fileMeta, error) {
	idx := map[string]fileMeta{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		idx[filepath.ToSlash(rel)] = fileMeta{full: p, size: info.Size()}
		return nil
	})
	return idx, err
}

// compare reports whether two files differ and a human-readable reason.
// Differing sizes are decisive; equal sizes fall back to a content hash.
//
// TODO: dispatch same-type pairs (SQLite, XML/plist, config) to a
// format-aware differ here before the byte-level fallback.
func compare(a, b fileMeta) (bool, string, error) {
	if a.size != b.size {
		return true, fmt.Sprintf("size %d -> %d bytes", a.size, b.size), nil
	}
	ha, err := hashFile(a.full)
	if err != nil {
		return false, "", err
	}
	hb, err := hashFile(b.full)
	if err != nil {
		return false, "", err
	}
	if ha != hb {
		return true, "content differs", nil
	}
	return false, "", nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func unionKeys(a, b map[string]fileMeta) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
