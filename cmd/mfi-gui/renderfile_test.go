package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// A SQLite database must open as a database (so the GUI offers "Open in
// Database"), never as a hex dump -- regardless of its extension, and even when
// the table summary can't be produced. Only the header is trusted.
func TestRenderPathSQLiteNeverHex(t *testing.T) {
	g := NewGUI()
	g.ctx = context.Background()
	dir := t.TempDir()

	realDB := filepath.Join(dir, "app.sqlite")
	db, err := sql.Open("sqlite", "file:"+realDB)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE t (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	// Valid SQLite header but a truncated/corrupt body: the summary fails, but
	// it must still be presented as a database rather than falling back to hex.
	corrupt := filepath.Join(dir, "corrupt.sqlite")
	if err := os.WriteFile(corrupt, append([]byte("SQLite format 3\x00"), make([]byte, 128)...), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{realDB, corrupt} {
		r, err := g.RenderPath(p, "auto", false)
		if err != nil {
			t.Errorf("%s: unexpected error: %v", filepath.Base(p), err)
		}
		if r.Kind == "hex" {
			t.Errorf("%s: rendered as hex; want a database view", filepath.Base(p))
		}
		if r.MIME != "application/vnd.sqlite3" {
			t.Errorf("%s: MIME = %q; want application/vnd.sqlite3", filepath.Base(p), r.MIME)
		}
	}
}
