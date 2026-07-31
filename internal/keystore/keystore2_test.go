package keystore

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// buildKeystore2DB creates a minimal keystore2-shaped database for testing the
// inventory query (keyentry + keyparameter, mirroring the Android 12+ schema).
func buildKeystore2DB(t *testing.T) *sql.DB {
	t.Helper()
	p := filepath.Join(t.TempDir(), "persistent.sqlite")
	db, err := sql.Open("sqlite", "file:"+p)
	if err != nil {
		t.Fatal(err)
	}
	stmts := []string{
		`CREATE TABLE keyentry (id INTEGER PRIMARY KEY, key_type INTEGER, domain INTEGER, namespace INTEGER, alias TEXT, state INTEGER, km_uuid BLOB)`,
		`CREATE TABLE keyparameter (keyentryid INTEGER, tag INTEGER, data ANY, security_level INTEGER)`,
		// A client app key (key_type 0) owned by uid 10123, StrongBox-backed.
		`INSERT INTO keyentry VALUES (1, 0, 0, 10123, 'login_key', 1, x'00')`,
		`INSERT INTO keyparameter VALUES (1, 268435458, 3, 2)`, // ALGORITHM=EC, StrongBox
		// A TEE key owned by uid 1000.
		`INSERT INTO keyentry VALUES (2, 0, 0, 1000, 'platform_key', 1, x'00')`,
		`INSERT INTO keyparameter VALUES (2, 268435458, 1, 1)`, // RSA, TEE
		// An internal super key (uid 0, no key parameters).
		`INSERT INTO keyentry VALUES (3, 2, 1, 0, 'super_key', 1, x'00')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("setup: %v (%s)", err, s)
		}
	}
	return db
}

func TestQueryKeystore2(t *testing.T) {
	db := buildKeystore2DB(t)
	defer db.Close()
	uidMap := map[string]string{"10123": "com.example.app"}

	items, diag, err := queryKeystore2(context.Background(), db, uidMap)
	if err != nil {
		t.Fatalf("queryKeystore2: %v", err)
	}
	if diag != "" {
		t.Fatalf("unexpected fallback diag on a valid schema: %q", diag)
	}
	if len(items) != 3 {
		t.Fatalf("got %d keys, want 3", len(items))
	}
	// Sorted by namespace: uid 0 (super key), 1000, then 10123.
	if items[0].Account != "super_key" || items[0].Service != "uid 0" {
		t.Errorf("item0 = %+v", items[0])
	}
	if items[0].Accessible != "unknown (non-exportable)" { // no key params -> unknown level
		t.Errorf("item0 accessible = %q, want unknown", items[0].Accessible)
	}
	if items[1].Account != "platform_key" || items[1].Service != "uid 1000" {
		t.Errorf("item1 = %+v", items[1])
	}
	if items[1].Accessible != "TEE (TrustedEnvironment) (non-exportable)" {
		t.Errorf("item1 accessible = %q", items[1].Accessible)
	}
	if items[2].Service != "com.example.app (uid 10123)" || items[2].Account != "login_key" {
		t.Errorf("item2 = %+v", items[2])
	}
	if items[2].Accessible != "StrongBox (non-exportable)" {
		t.Errorf("item2 accessible = %q", items[2].Accessible)
	}
	for _, it := range items {
		if it.Value != "<key material not exportable>" {
			t.Errorf("value should note non-exportability: %q", it.Value)
		}
	}
}
