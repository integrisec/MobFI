package diff

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// usersDB writes a SQLite file at path with a users(name, val) table.
func usersDB(t *testing.T, path string, rows [][2]string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE users (name TEXT, val TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if _, err := db.Exec(`INSERT INTO users (name, val) VALUES (?, ?)`, r[0], r[1]); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSQLiteDiffer(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.db")
	b := filepath.Join(dir, "b.db")
	usersDB(t, a, [][2]string{{"dana", "x"}, {"sam", "y"}})
	usersDB(t, b, [][2]string{{"dana", "x"}, {"max", "z"}}) // sam removed, max added

	if !(sqliteDiffer{}).Handles(a) {
		t.Fatal("Handles should recognise a SQLite file")
	}
	detail, err := sqliteDiffer{}.Diff(context.Background(), a, b)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, "users: +1 -1 rows") {
		t.Errorf("detail = %q, want it to mention users: +1 -1 rows", detail)
	}
}

func TestJSONDiffer(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.json")
	b := filepath.Join(dir, "b.json")
	os.WriteFile(a, []byte(`{"x":1,"y":2,"z":{"a":1}}`), 0o600)
	os.WriteFile(b, []byte(`{"x":1,"y":3,"z":{"a":1},"w":9}`), 0o600) // y changed, w added

	detail, err := jsonDiffer{}.Diff(context.Background(), a, b)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, "1 changed") || !strings.Contains(detail, "1 added") {
		t.Errorf("detail = %q, want 1 changed / 1 added", detail)
	}
}

// TestTreesUsesStructuralDiff proves the differ is wired into Trees.
func TestTreesUsesStructuralDiff(t *testing.T) {
	rootA, rootB := t.TempDir(), t.TempDir()
	usersDB(t, filepath.Join(rootA, "app.db"), [][2]string{{"dana", "x"}})
	usersDB(t, filepath.Join(rootB, "app.db"), [][2]string{{"dana", "x"}, {"sam", "y"}})

	res, err := Trees(context.Background(), rootA, rootB)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) != 1 {
		t.Fatalf("changes = %+v", res.Changes)
	}
	ch := res.Changes[0]
	if ch.Kind != Modified || !strings.Contains(ch.Detail, "sqlite:") || !strings.Contains(ch.Detail, "users: +1 -0 rows") {
		t.Errorf("change = %+v, want structural sqlite detail", ch)
	}
}
