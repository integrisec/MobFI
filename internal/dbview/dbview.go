// Package dbview opens on-disk database files (SQLite and others) for
// local, read-only inspection without mutating the source.
package dbview

import (
	"context"
	"errors"
)

// Table is a listing of columns and rows read from a database.
type Table struct {
	Name    string
	Columns []string
	Rows    [][]string
}

// DB is a read-only view over an opened database file.
type DB interface {
	Tables(ctx context.Context) ([]string, error)
	Read(ctx context.Context, table string, limit int) (*Table, error)
	Close() error
}

// Open opens the database at path read-only. The concrete engine is
// chosen from the file's format.
func Open(ctx context.Context, path string) (DB, error) {
	// TODO: detect format (SQLite header, etc.) and open read-only.
	_ = ctx
	_ = path
	return nil, ErrNotImplemented
}

// ErrNotImplemented marks scaffolded behaviour that is not built yet.
var ErrNotImplemented = errors.New("not implemented")
