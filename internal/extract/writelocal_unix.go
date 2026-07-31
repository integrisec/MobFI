//go:build !windows

package extract

import (
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// OpenLocalForWrite creates local for writing with O_NOFOLLOW so a symlink
// pre-planted at that path by another process does not redirect the write
// to a target outside the extraction tree (MFI-PATH-02). Mode is 0o600 --
// extracted evidence has no reason to be world-readable. Exported so
// sibling extractors (e.g. iOS backup reconstruction) share the guard.
func OpenLocalForWrite(local string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(local, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o600)
}

func writeLocalCopy(local string, r io.Reader) (int64, error) {
	f, err := OpenLocalForWrite(local)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return n, err
}
