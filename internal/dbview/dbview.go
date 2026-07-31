// Package dbview opens on-disk database files (SQLite today) for local,
// read-only inspection. Files are opened immutable so inspecting evidence
// never mutates it.
package dbview

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
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
func Open(ctx context.Context, path string) (DB, error) {
	if err := checkSQLite(path); err != nil {
		return nil, err
	}
	// Prefer mode=ro + immutable=1: never write, and assume no other writer, so
	// the (already-copied) evidence file is left byte-for-byte untouched. Some
	// real databases won't open that way -- e.g. one carrying a hot WAL that
	// must be read to see committed rows -- so fall back to plain read-only,
	// which still never writes the main database.
	var firstErr error
	for _, dsn := range []string{
		"file:" + path + "?mode=ro&immutable=1",
		"file:" + path + "?mode=ro",
	} {
		db, err := sql.Open("sqlite", dsn)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if err := db.PingContext(ctx); err != nil {
			db.Close()
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		return &sqliteDB{db: db}, nil
	}
	return nil, firstErr
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
	db *sql.DB
}

func (s *sqliteDB) Close() error { return s.db.Close() }

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

// renderCell turns a scanned SQLite value into a display string. Textual
// blobs are shown as-is; binary blobs are summarised rather than dumped.
func renderCell(v any) string {
	switch val := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		if utf8.Valid(val) && !hasControlBytes(val) {
			return string(val)
		}
		return fmt.Sprintf("<blob %d bytes>", len(val))
	case string:
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
