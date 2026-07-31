//go:build windows

package extract

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

// OpenLocalForWrite creates local for writing. Windows lacks a portable
// O_NOFOLLOW, so a pre-existing reparse point / junction cannot be blocked
// at the open syscall itself. As a partial defence, Lstat first and refuse
// to overwrite when the destination is already a symlink / reparse point;
// there is still a small TOCTOU window between the check and the open.
// Mode is 0o600 (as portable as Windows perms get) -- see MFI-PATH-02.
// Exported so sibling extractors reuse the same guard.
func OpenLocalForWrite(local string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return nil, err
	}
	if fi, err := os.Lstat(local); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("refusing to write through pre-existing symlink at destination")
	}
	return os.OpenFile(local, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
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
