package render

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func renderFile(t *testing.T, name, content string) *View {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	v, err := DefaultRegistry().Render(context.Background(), path)
	if err != nil {
		t.Fatalf("render %s: %v", name, err)
	}
	return v
}

func TestJSONRenderer(t *testing.T) {
	v := renderFile(t, "data.json", `{"b":1,"a":2}`)
	if v.MIME != "application/json" {
		t.Errorf("MIME = %s", v.MIME)
	}
	if !strings.Contains(v.Text, "\n  \"b\": 1") {
		t.Errorf("JSON not indented:\n%s", v.Text)
	}
}

func TestXMLRenderer(t *testing.T) {
	v := renderFile(t, "conf.xml", `<a><b>x</b></a>`)
	if v.MIME != "application/xml" {
		t.Errorf("MIME = %s", v.MIME)
	}
	if !strings.Contains(v.Text, "\n  <b>x</b>") {
		t.Errorf("XML not reindented:\n%s", v.Text)
	}
}

func TestTextRenderer(t *testing.T) {
	v := renderFile(t, "notes.log", "just some log text\n")
	if v.MIME != "text/plain" {
		t.Errorf("MIME = %s", v.MIME)
	}
	if !strings.Contains(v.Text, "just some log text") {
		t.Errorf("text = %q", v.Text)
	}
}

func TestHexFallback(t *testing.T) {
	// A file with a NUL byte and no known extension falls through to hex.
	v := renderFile(t, "blob.dat", "\x00\x01\x02ABC")
	if v.MIME != "application/octet-stream" {
		t.Errorf("MIME = %s", v.MIME)
	}
	if !strings.Contains(v.Text, "00000000") || !strings.Contains(v.Text, "ABC") {
		t.Errorf("hex dump unexpected:\n%s", v.Text)
	}
}

func TestSQLiteRenderer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE creds (id INTEGER, secret TEXT)`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	v, err := DefaultRegistry().Render(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if v.MIME != "application/vnd.sqlite3" {
		t.Errorf("MIME = %s", v.MIME)
	}
	if !strings.Contains(v.Text, "SQLite database") || !strings.Contains(v.Text, "creds") {
		t.Errorf("sqlite summary unexpected:\n%s", v.Text)
	}
}
