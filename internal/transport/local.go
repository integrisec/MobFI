package transport

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// localConn is a Conn over the host filesystem. It is used for targets whose
// data already lives on the local machine — notably iOS Simulators, whose
// containers sit under ~/Library/Developer/CoreSimulator — so extraction is a
// plain recursive copy with no device transport involved.
type localConn struct{}

// NewLocalConn returns a Conn that reads the local filesystem.
func NewLocalConn() Conn { return &localConn{} }

// Exec is unsupported: there is no shell to run against a local path.
func (c *localConn) Exec(ctx context.Context, cmd string, args ...string) ([]byte, error) {
	return nil, errors.New("transport: exec unsupported on local filesystem")
}

// Open opens a local file for reading.
func (c *localConn) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	return os.Open(path)
}

// Walk traverses root on the local filesystem, honouring context cancellation.
func (c *localConn) Walk(ctx context.Context, root string, fn fs.WalkDirFunc) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		return fn(p, d, err)
	})
}

// Close is a no-op.
func (c *localConn) Close() error { return nil }
