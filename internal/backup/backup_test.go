package backup

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/integrisec/MobFI/internal/extract"
)

// TestReconstruct builds a synthetic backup (Manifest.db + a hashed file) and
// checks that reconstruct pulls only the target app's files into a readable
// tree, counting bytes and ignoring unrelated apps.
func TestReconstruct(t *testing.T) {
	backupDir := t.TempDir()

	db, err := sql.Open("sqlite", filepath.Join(backupDir, "Manifest.db"))
	if err != nil {
		t.Fatal(err)
	}
	stmts := []string{
		`CREATE TABLE Files (fileID TEXT, domain TEXT, relativePath TEXT, flags INTEGER, file BLOB)`,
		`INSERT INTO Files (fileID, domain, relativePath, flags) VALUES ('aab0','AppDomain-com.x','Documents',2)`,
		`INSERT INTO Files (fileID, domain, relativePath, flags) VALUES ('aab1122','AppDomain-com.x','Documents/note.txt',1)`,
		`INSERT INTO Files (fileID, domain, relativePath, flags) VALUES ('ccdd','AppDomain-com.other','Documents/secret',1)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("setup %q: %v", s, err)
		}
	}
	db.Close()

	// Hashed content file for the target file entry: <backupDir>/aa/aab1122.
	if err := os.MkdirAll(filepath.Join(backupDir, "aa"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "aa", "aab1122"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	dest := t.TempDir()
	res, err := reconstruct(context.Background(), backupDir, Options{BundleID: "com.x", Dest: dest})
	if err != nil {
		t.Fatal(err)
	}
	if res.FileCount != 1 || res.ByteCount != 5 {
		t.Fatalf("got %d file(s), %d byte(s); want 1 file, 5 bytes", res.FileCount, res.ByteCount)
	}
	got, err := os.ReadFile(extract.SafeJoin(dest, "AppDomain-com.x/Documents/note.txt"))
	if err != nil || string(got) != "hello" {
		t.Fatalf("reconstructed file = %q, err %v; want %q", got, err, "hello")
	}
	// The unrelated app's domain must not have been extracted.
	if _, err := os.Stat(filepath.Join(dest, "AppDomain-com.other")); !os.IsNotExist(err) {
		t.Fatalf("unrelated app domain should not be extracted")
	}
}
