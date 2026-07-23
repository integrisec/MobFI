// Package extract copies an installed application's on-device file tree to
// a local destination via a transport.Conn.
package extract

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/integrisec/MobFI/internal/transport"
)

// Progress reports cumulative extraction progress after each file.
type Progress struct {
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
	Path  string `json:"path"` // the file just copied
}

// Request describes an extraction job.
type Request struct {
	BundleID   string // target app package/bundle id (metadata for the report)
	SourceRoot string // on-device directory to copy (e.g. /data/data/<pkg>)
	Dest       string // local destination directory
	// Progress, if set, is called after each file is copied. It runs on the
	// extraction goroutine, so it must not block for long.
	Progress func(Progress)
}

// SkippedFile records a path that could not be copied (typically a
// permission denial) so the report can note the gap rather than hide it.
type SkippedFile struct {
	Path   string `json:"path"` // on-device path
	Reason string `json:"reason"`
}

// Result summarises what was copied.
type Result struct {
	Root      string        `json:"root"` // local root the tree was written under
	FileCount int           `json:"file_count"`
	ByteCount int64         `json:"byte_count"`
	Skipped   []SkippedFile `json:"skipped,omitempty"`
}

// Run mirrors req.SourceRoot from the device (reached via conn) into
// req.Dest, preserving directory structure. Files that cannot be read off
// the device are recorded in Result.Skipped and the walk continues; a
// failure to write locally aborts the extraction.
func Run(ctx context.Context, conn transport.Conn, req Request) (*Result, error) {
	if req.SourceRoot == "" {
		return nil, errors.New("extract: SourceRoot is required")
	}
	if req.Dest == "" {
		return nil, errors.New("extract: Dest is required")
	}
	if err := os.MkdirAll(req.Dest, 0o755); err != nil {
		return nil, err
	}

	res := &Result{Root: req.Dest}
	walkErr := conn.Walk(ctx, req.SourceRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// A failure to enumerate part of the tree is not fatal to the
			// rest of it.
			res.Skipped = append(res.Skipped, SkippedFile{Path: p, Reason: err.Error()})
			return nil
		}

		local := filepath.Join(req.Dest, filepath.FromSlash(relDevicePath(req.SourceRoot, p)))
		// Defend against a device path that would escape Dest (untrusted
		// input — extracted device data must not write outside Dest).
		if !within(req.Dest, local) {
			res.Skipped = append(res.Skipped, SkippedFile{Path: p, Reason: "path escapes destination"})
			return nil
		}

		if d.IsDir() {
			return os.MkdirAll(local, 0o755)
		}

		rc, err := conn.Open(ctx, p)
		if err != nil {
			res.Skipped = append(res.Skipped, SkippedFile{Path: p, Reason: err.Error()})
			return nil
		}
		n, werr := writeLocal(local, rc)
		_ = rc.Close()
		if werr != nil {
			return fmt.Errorf("write %s: %w", local, werr)
		}
		res.FileCount++
		res.ByteCount += n
		if req.Progress != nil {
			req.Progress(Progress{Files: res.FileCount, Bytes: res.ByteCount, Path: p})
		}
		return nil
	})
	if walkErr != nil {
		return res, walkErr
	}
	return res, nil
}

// RunTar extracts a tar stream (as produced by transport.TarStreamer) into
// req.Dest, applying the same destination guard, skip tracking and progress
// reporting as Run. Directory entries are recreated; regular files are
// copied; other entry types (symlinks, sockets, devices) are recorded as
// skipped rather than reconstructed.
func RunTar(ctx context.Context, r io.Reader, req Request) (*Result, error) {
	if req.Dest == "" {
		return nil, errors.New("extract: Dest is required")
	}
	if err := os.MkdirAll(req.Dest, 0o755); err != nil {
		return nil, err
	}

	res := &Result{Root: req.Dest}
	tr := tar.NewReader(r)
	for {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return res, err
		}

		name := path.Clean(strings.TrimPrefix(hdr.Name, "./"))
		if name == "." || name == ".." || name == "" {
			continue
		}
		local := filepath.Join(req.Dest, filepath.FromSlash(name))
		if !within(req.Dest, local) {
			res.Skipped = append(res.Skipped, SkippedFile{Path: hdr.Name, Reason: "path escapes destination"})
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(local, 0o755); err != nil {
				return res, err
			}
		case tar.TypeReg:
			n, err := writeLocal(local, tr)
			if err != nil {
				return res, fmt.Errorf("write %s: %w", local, err)
			}
			res.FileCount++
			res.ByteCount += n
			if req.Progress != nil {
				req.Progress(Progress{Files: res.FileCount, Bytes: res.ByteCount, Path: name})
			}
		default:
			res.Skipped = append(res.Skipped, SkippedFile{Path: hdr.Name, Reason: fmt.Sprintf("unsupported tar entry type %q", hdr.Typeflag)})
		}
	}
	return res, nil
}

// writeLocal creates local (and any parent directories) and copies r into
// it, returning the number of bytes written.
func writeLocal(local string, r io.Reader) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return 0, err
	}
	f, err := os.Create(local)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(f, r)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return n, err
}

// relDevicePath returns p relative to root using '/' (device) semantics.
// The root itself maps to ".".
func relDevicePath(root, p string) string {
	root = strings.TrimRight(root, "/")
	switch {
	case p == root:
		return "."
	case strings.HasPrefix(p, root+"/"):
		return p[len(root)+1:]
	default:
		return strings.TrimLeft(p, "/")
	}
}

// within reports whether target stays inside base.
func within(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
