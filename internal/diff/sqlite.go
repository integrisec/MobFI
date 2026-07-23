package diff

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/integrisec/MobFI/internal/dbview"
)

// maxDiffRows caps how many rows per table are compared, to bound memory on
// large databases.
const maxDiffRows = 100000

var sqliteMagic = []byte("SQLite format 3\x00")

// sqliteDiffer compares two SQLite databases table by table, reporting how
// many rows were added and removed per table (and tables added/removed).
// Rows are compared as whole tuples, so an edited row shows as one removed
// plus one added.
type sqliteDiffer struct{}

func (sqliteDiffer) Handles(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := make([]byte, len(sqliteMagic))
	n, _ := io.ReadFull(f, h)
	return n == len(sqliteMagic) && bytes.Equal(h, sqliteMagic)
}

func (sqliteDiffer) Diff(ctx context.Context, aPath, bPath string) (string, error) {
	da, err := dbview.Open(ctx, aPath)
	if err != nil {
		return "", err
	}
	defer da.Close()
	db, err := dbview.Open(ctx, bPath)
	if err != nil {
		return "", err
	}
	defer db.Close()

	ta, err := da.Tables(ctx)
	if err != nil {
		return "", err
	}
	tb, err := db.Tables(ctx)
	if err != nil {
		return "", err
	}
	setA, setB := toSet(ta), toSet(tb)

	var notes []string
	for _, name := range unionSorted(setA, setB) {
		_, inA := setA[name]
		_, inB := setB[name]
		switch {
		case inA && !inB:
			notes = append(notes, "table "+name+" removed")
		case !inA && inB:
			notes = append(notes, "table "+name+" added")
		default:
			added, removed, err := tableRowDiff(ctx, da, db, name)
			if err != nil {
				return "", err
			}
			if added > 0 || removed > 0 {
				notes = append(notes, fmt.Sprintf("%s: +%d -%d rows", name, added, removed))
			}
		}
	}
	if len(notes) == 0 {
		return "sqlite: no row differences (metadata only)", nil
	}
	return "sqlite: " + strings.Join(notes, "; "), nil
}

// tableRowDiff counts rows present in only one of the two tables, treating
// each row as a whole tuple (a multiset comparison).
func tableRowDiff(ctx context.Context, da, db dbview.DB, table string) (added, removed int, err error) {
	ra, err := da.Read(ctx, table, maxDiffRows)
	if err != nil {
		return 0, 0, err
	}
	rb, err := db.Read(ctx, table, maxDiffRows)
	if err != nil {
		return 0, 0, err
	}
	counts := make(map[string]int, len(ra.Rows))
	for _, row := range ra.Rows {
		counts[rowKey(row)]++
	}
	for _, row := range rb.Rows {
		counts[rowKey(row)]--
	}
	for _, c := range counts {
		if c > 0 {
			removed += c
		} else if c < 0 {
			added += -c
		}
	}
	return added, removed, nil
}

func rowKey(row []string) string { return strings.Join(row, "\x1f") }

func toSet(xs []string) map[string]struct{} {
	m := make(map[string]struct{}, len(xs))
	for _, x := range xs {
		m[x] = struct{}{}
	}
	return m
}

func unionSorted(a, b map[string]struct{}) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
