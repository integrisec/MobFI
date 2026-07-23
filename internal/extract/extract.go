// Package extract copies an installed application's on-device file tree to
// a local destination via a transport.Conn.
package extract

import (
	"context"
	"errors"

	"github.com/integrisec/MobFI/internal/transport"
)

// Request describes an extraction job.
type Request struct {
	BundleID string // target app package/bundle id
	Dest     string // local destination directory
}

// Result summarises what was copied.
type Result struct {
	FileCount int
	ByteCount int64
	Root      string // local root the tree was written under
}

// Run copies the target app's file tree over conn into req.Dest.
func Run(ctx context.Context, conn transport.Conn, req Request) (*Result, error) {
	// TODO: resolve the app data root on-device, walk it via conn and
	// mirror it under req.Dest, preserving structure and metadata.
	_ = ctx
	_ = conn
	_ = req
	return nil, ErrNotImplemented
}

// ErrNotImplemented marks scaffolded behaviour that is not built yet.
var ErrNotImplemented = errors.New("not implemented")
