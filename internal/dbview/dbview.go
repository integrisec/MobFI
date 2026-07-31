// Package dbview opens on-disk database files (SQLite today) for local,
// read-only inspection. Files are opened immutable so inspecting evidence
// never mutates it.
package dbview

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	_ "modernc.org/sqlite" // cgo-free SQLite driver, registered as "sqlite"
)

// sqliteMagic is the 16-byte header every SQLite 3 database begins with.
var sqliteMagic = []byte("SQLite format 3\x00")

// Table is a listing of columns and rows read from a database.
type Table struct {
	Name    string     `json:"name"`
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

// DB is a read-only view over an opened database file.
type DB interface {
	Tables(ctx context.Context) ([]string, error)
	Read(ctx context.Context, table string, limit int) (*Table, error)
	Close() error
}

// Open opens the database at path read-only. The format is detected from
// the file header; only SQLite is supported so far.
//
// The evidence directory is never touched. mode=ro + immutable=1 is the
// preferred DSN because it never opens for write and never touches WAL
// sidecars. When the DB has a hot WAL that requires reading -journal / -wal /
// -shm to see committed rows, the immutable open fails; the fallback is to
// copy the DB and its sidecars into an OS temp dir and open the copy, so any
// sidecar mutation lands in the scratch dir, not next to evidence. See
// MFI-XC-01 in SECURITY-AUDIT.md.
func Open(ctx context.Context, path string) (DB, error) {
	if err := checkSQLite(path); err != nil {
		return nil, err
	}

	// First try: immutable open on the original file. This never writes.
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&immutable=1")
	if err == nil {
		if pingErr := db.PingContext(ctx); pingErr == nil {
			return &sqliteDB{db: db}, nil
		}
		db.Close()
	}

	// Fallback: copy into a scratch dir (with sidecars) and open there. Any
	// -journal / -wal / -shm materialisation happens under scratch, so the
	// evidence directory is left byte-for-byte untouched.
	scratch, cleanup, copyErr := copyWithSidecars(path)
	if copyErr != nil {
		return nil, fmt.Errorf("dbview: could not open %s immutable and copy-fallback failed: %w", path, copyErr)
	}
	db, err = sql.Open("sqlite", "file:"+scratch+"?mode=ro")
	if err != nil {
		cleanup()
		return nil, err
	}
	if pingErr := db.PingContext(ctx); pingErr != nil {
		db.Close()
		cleanup()
		return nil, pingErr
	}
	return &sqliteDB{db: db, cleanup: cleanup}, nil
}

// copyWithSidecars copies src plus any -journal / -wal / -shm siblings into
// a fresh temp dir with 0700 perms. Returns the copied main-DB path, a
// cleanup func that removes the temp dir, and any copy error.
func copyWithSidecars(src string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "mfi-dbview-*")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	base := filepath.Base(src)
	dst := filepath.Join(dir, base)
	if err := copyFile(src, dst); err != nil {
		cleanup()
		return "", func() {}, err
	}
	for _, suffix := range []string{"-journal", "-wal", "-shm"} {
		s := src + suffix
		if _, err := os.Stat(s); err != nil {
			continue
		}
		if err := copyFile(s, dst+suffix); err != nil {
			cleanup()
			return "", func() {}, err
		}
	}
	return dst, cleanup, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func checkSQLite(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	header := make([]byte, len(sqliteMagic))
	if _, err := f.Read(header); err != nil {
		return fmt.Errorf("dbview: reading header: %w", err)
	}
	if string(header) != string(sqliteMagic) {
		return errors.New("dbview: not a SQLite database")
	}
	return nil
}

type sqliteDB struct {
	db      *sql.DB
	cleanup func() // removes the scratch-copy dir if Open used the copy fallback
}

func (s *sqliteDB) Close() error {
	err := s.db.Close()
	if s.cleanup != nil {
		s.cleanup()
	}
	return err
}

// Tables lists user tables (sqlite_* internal tables are omitted).
func (s *sqliteDB) Tables(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		tables = append(tables, name)
	}
	return tables, rows.Err()
}

// Read returns up to limit rows from table. The table name is validated
// against the database's actual tables before use, since identifiers
// cannot be passed as query parameters.
func (s *sqliteDB) Read(ctx context.Context, table string, limit int) (*Table, error) {
	known, err := s.Tables(ctx)
	if err != nil {
		return nil, err
	}
	if !contains(known, table) {
		return nil, fmt.Errorf("dbview: unknown table %q", table)
	}
	if limit <= 0 {
		limit = 100
	}

	// table is validated above; quote it defensively all the same.
	query := fmt.Sprintf("SELECT * FROM %s LIMIT ?", quoteIdent(table))
	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := &Table{Name: table, Columns: cols}
	for rows.Next() {
		cells := make([]any, len(cols))
		for i := range cells {
			cells[i] = new(any)
		}
		if err := rows.Scan(cells...); err != nil {
			return nil, err
		}
		record := make([]string, len(cols))
		for i, c := range cells {
			record[i] = renderCell(*(c.(*any)))
		}
		out.Rows = append(out.Rows, record)
	}
	return out, rows.Err()
}

// maxCellDisplayBytes caps how much of a single cell is materialised into
// the display string. modernc.org/sqlite returns multi-GB blobs without
// complaint; a text-shaped blob column that carries hostile input would
// otherwise OOM the process (MFI-PAR-06).
const maxCellDisplayBytes = 64 << 10

// renderCell turns a scanned SQLite value into a display string. Textual
// blobs are shown as-is; binary blobs are summarised rather than dumped.
// Both branches truncate at maxCellDisplayBytes with an explicit marker so
// large-cell DoS is bounded but the caller still sees SOMETHING.
func renderCell(v any) string {
	switch val := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		if utf8.Valid(val) && !hasControlBytes(val) {
			if len(val) > maxCellDisplayBytes {
				return string(val[:maxCellDisplayBytes]) + fmt.Sprintf("... <truncated, total %d bytes>", len(val))
			}
			return string(val)
		}
		return fmt.Sprintf("<blob %d bytes>", len(val))
	case string:
		if len(val) > maxCellDisplayBytes {
			return val[:maxCellDisplayBytes] + fmt.Sprintf("... <truncated, total %d bytes>", len(val))
		}
		return val
	default:
		return fmt.Sprint(val)
	}
}

func hasControlBytes(b []byte) bool {
	for _, c := range b {
		if c < 0x09 || (c > 0x0d && c < 0x20) {
			return true
		}
	}
	return false
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
