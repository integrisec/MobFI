package keystore

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite" // cgo-free SQLite driver

	"github.com/integrisec/MobFI/internal/sysproc"
)

// Android 12+ replaced the legacy per-file keystore blobs with keystore2, whose
// metadata lives in an SQLite database at /data/misc/keystore/persistent.sqlite.
// This file pulls that database (root, over adb) and inventories the key
// entries. As with the legacy store, the key material is hardware-backed and
// non-exportable -- this reports which keys exist, their owning app, alias, and
// security level (TEE / StrongBox / software), not the keys themselves.

const keystore2Path = "/data/misc/keystore/persistent.sqlite"

// dumpKeystore2 pulls and parses the keystore2 database. It returns the items
// and notes; a nil error with zero items simply means nothing was found.
func dumpKeystore2(ctx context.Context, serial string, uidToPkg map[string]string) ([]Item, []string, error) {
	data, err := adbCatRoot(ctx, serial, keystore2Path)
	if err != nil {
		return nil, nil, err
	}
	if !strings.HasPrefix(string(data[:min(16, len(data))]), "SQLite format 3") {
		return nil, []string{"Could not read persistent.sqlite as a database (root cat returned non-SQLite data)."}, nil
	}
	dir, err := os.MkdirTemp("", "mfi-ks2-")
	if err != nil {
		return nil, nil, err
	}
	defer os.RemoveAll(dir)
	dbPath := filepath.Join(dir, "persistent.sqlite")
	if err := os.WriteFile(dbPath, data, 0o600); err != nil {
		return nil, nil, err
	}
	// Also pull any hot WAL so recently-created keys (not yet checkpointed into
	// the main file) are visible. Best-effort: absent sidecars are fine.
	dsn := "file:" + dbPath + "?mode=ro&immutable=1"
	if wal, err := adbCatRoot(ctx, serial, keystore2Path+"-wal"); err == nil && len(wal) > 0 {
		if os.WriteFile(dbPath+"-wal", wal, 0o600) == nil {
			if shm, err := adbCatRoot(ctx, serial, keystore2Path+"-shm"); err == nil {
				_ = os.WriteFile(dbPath+"-shm", shm, 0o600)
			}
			dsn = "file:" + dbPath + "?mode=ro" // let SQLite apply the WAL
		}
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, nil, err
	}
	defer db.Close()

	items, err := queryKeystore2(ctx, db, uidToPkg)
	if err != nil {
		return nil, []string{"keystore2 database present but its schema could not be read: " + err.Error()}, nil
	}
	return items, []string{fmt.Sprintf("Parsed %d key(s) from the keystore2 database (persistent.sqlite).", len(items))}, nil
}

// queryKeystore2 reads key entries from an open keystore2 database. It tries a
// rich query (with security level) and falls back to a minimal one, so a schema
// drift across Android versions degrades rather than fails.
func queryKeystore2(ctx context.Context, db *sql.DB, uidToPkg map[string]string) ([]Item, error) {
	// security_level lives on keyparameter rows: 0=software, 1=TEE, 2=StrongBox.
	// No key_type filter: its enum meaning has shifted across Android versions,
	// and over-filtering risks hiding real keys (the "0 items" failure mode). We
	// list every aliased entry and let the owner/alias speak for itself.
	const rich = `
		SELECT k.namespace, k.alias, COALESCE(sl.lvl, -1)
		FROM keyentry k
		LEFT JOIN (SELECT keyentryid, MAX(security_level) AS lvl FROM keyparameter GROUP BY keyentryid) sl
		  ON sl.keyentryid = k.id
		WHERE k.alias IS NOT NULL
		ORDER BY k.namespace, k.alias`
	rows, err := db.QueryContext(ctx, rich)
	if err != nil {
		// Minimal fallback: just the owning namespace and alias.
		rows, err = db.QueryContext(ctx,
			`SELECT namespace, alias, -1 FROM keyentry WHERE alias IS NOT NULL ORDER BY namespace, alias`)
		if err != nil {
			return nil, err
		}
	}
	defer rows.Close()

	var items []Item
	for rows.Next() {
		var ns sql.NullInt64
		var alias sql.NullString
		var lvl sql.NullInt64
		if err := rows.Scan(&ns, &alias, &lvl); err != nil {
			return nil, err
		}
		uid := ""
		if ns.Valid {
			uid = fmt.Sprintf("%d", ns.Int64)
		}
		service := uidLabel(uid, uidToPkg)
		items = append(items, Item{
			Source:     "keystore",
			Class:      "Keystore key (keystore2)",
			Service:    service,
			Account:    alias.String,
			Accessible: securityLevelName(lvl) + " (non-exportable)",
			Value:      "<key material not exportable>",
		})
	}
	return items, rows.Err()
}

func securityLevelName(lvl sql.NullInt64) string {
	if !lvl.Valid {
		return "hardware-backed"
	}
	switch lvl.Int64 {
	case 0:
		return "software"
	case 1:
		return "TEE (TrustedEnvironment)"
	case 2:
		return "StrongBox"
	default:
		return "hardware-backed"
	}
}

// uidLabel maps an app uid to "package (uid)" when known, else "uid <n>".
func uidLabel(uid string, uidToPkg map[string]string) string {
	if uid == "" {
		return ""
	}
	if pkg := uidToPkg[uid]; pkg != "" {
		return fmt.Sprintf("%s (uid %s)", pkg, uid)
	}
	return "uid " + uid
}

// adbUIDMap builds a uid -> package name map via `cmd package list packages -U`
// (no root needed). Best-effort: on failure it returns an empty map.
func adbUIDMap(ctx context.Context, serial string) map[string]string {
	m := map[string]string{}
	out, err := sysproc.CommandContext(ctx, "adb", adbArgs(serial, "shell", "cmd package list packages -U")...).Output()
	if err != nil {
		return m
	}
	for _, line := range strings.Split(string(out), "\n") {
		// "package:com.example uid:10123"
		line = strings.TrimSpace(line)
		pkg := ""
		uid := ""
		for _, tok := range strings.Fields(line) {
			if v, ok := strings.CutPrefix(tok, "package:"); ok {
				pkg = v
			} else if v, ok := strings.CutPrefix(tok, "uid:"); ok {
				uid = v
			}
		}
		if pkg != "" && uid != "" {
			m[uid] = pkg
		}
	}
	return m
}

// adbCatRoot streams a root-owned file off the device as raw bytes. exec-out
// avoids the PTY line-ending translation that would corrupt binary data.
func adbCatRoot(ctx context.Context, serial, path string) ([]byte, error) {
	for _, suForm := range []string{"su -c 'cat " + path + "'", "su 0 cat " + path} {
		out, err := sysproc.CommandContext(ctx, "adb", adbArgs(serial, "exec-out", suForm)...).Output()
		if err == nil && len(out) > 0 {
			return out, nil
		}
	}
	return nil, fmt.Errorf("adb exec-out su cat %s failed (device rooted and authorized?)", path)
}

func adbArgs(serial string, rest ...string) []string {
	var args []string
	if serial != "" {
		args = append(args, "-s", serial)
	}
	return append(args, rest...)
}
