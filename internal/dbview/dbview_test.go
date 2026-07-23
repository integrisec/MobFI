package dbview

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// makeDB creates a SQLite file with a users table and a couple of rows.
func makeDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stmts := []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, token TEXT)`,
		`INSERT INTO users (name, token) VALUES ('dana', 'abc'), ('sam', NULL)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("exec %q: %v", s, err)
		}
	}
	return path
}

func TestTablesAndRead(t *testing.T) {
	path := makeDB(t)
	ctx := context.Background()

	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tables, err := db.Tables(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tables) != 1 || tables[0] != "users" {
		t.Fatalf("tables = %v, want [users]", tables)
	}

	got, err := db.Read(ctx, "users", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Columns) != 3 {
		t.Errorf("columns = %v", got.Columns)
	}
	if len(got.Rows) != 2 {
		t.Fatalf("rows = %v", got.Rows)
	}
	if got.Rows[0][1] != "dana" {
		t.Errorf("row0 name = %q, want dana", got.Rows[0][1])
	}
	if got.Rows[1][2] != "NULL" {
		t.Errorf("row1 token = %q, want NULL", got.Rows[1][2])
	}
}

func TestReadUnknownTableRejected(t *testing.T) {
	db, err := Open(context.Background(), makeDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Read(context.Background(), "users; DROP TABLE users", 10); err == nil {
		t.Error("expected unknown-table error for an injection attempt")
	}
}

func TestOpenRejectsNonSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not.db")
	if err := os.WriteFile(path, []byte("just some text, not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), path); err == nil {
		t.Error("expected error opening a non-SQLite file")
	}
}
