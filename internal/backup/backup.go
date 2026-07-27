// Package backup extracts an iOS app's data from a full device backup made
// with idevicebackup2. On a non-jailbroken device, AFC house arrest can only
// reach a dev-signed app's container (or a file-sharing app's Documents), so a
// backup is the way to obtain a production App Store app's private data. Run
// performs a backup, then reconstructs the target app's files from the
// backup's Manifest.db into a readable tree.
package backup

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	_ "modernc.org/sqlite" // sql driver "sqlite" (cgo-free)

	"github.com/integrisec/MobFI/internal/extract"
	"github.com/integrisec/MobFI/internal/plist"
	"github.com/integrisec/MobFI/internal/sysproc"
)

// Options configures a backup-based extraction.
type Options struct {
	UDID     string // target device UDID
	BundleID string // app whose data to reconstruct
	Dest     string // local destination directory for the reconstructed tree
	Bin      string // idevicebackup2 binary; empty means "idevicebackup2" from PATH
	InfoBin  string // ideviceinfo binary; empty means "ideviceinfo" from PATH
	Progress func(extract.Progress)
	// KeepRaw leaves the raw backup on disk (under Dest/_backup) instead of
	// deleting it after reconstruction. Off by default to save space.
	KeepRaw bool
}

func (o Options) bin() string {
	if o.Bin != "" {
		return o.Bin
	}
	return "idevicebackup2"
}

func (o Options) infoBin() string {
	if o.InfoBin != "" {
		return o.InfoBin
	}
	return "ideviceinfo"
}

// EstimateBackupSize returns the device's used data in bytes, which approximates
// the size of a full (unencrypted) backup -- the dominant contents (photos,
// videos, app data) are backed up, while the OS and app binaries are not, so a
// backup is usually somewhat smaller. It reads the com.apple.disk_usage domain
// via ideviceinfo. bin may be "" for "ideviceinfo" from PATH. A zero result
// with a nil error means the value could not be determined.
func EstimateBackupSize(ctx context.Context, udid, bin string) (int64, error) {
	if bin == "" {
		bin = "ideviceinfo"
	}
	args := []string{"-q", "com.apple.disk_usage"}
	if udid != "" {
		args = append([]string{"-u", udid}, args...)
	}
	out, err := sysproc.CommandContext(ctx, bin, args...).Output()
	if err != nil {
		return 0, err
	}
	var capacity, avail int64
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		n, perr := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if perr != nil {
			continue
		}
		switch strings.TrimSpace(k) {
		case "TotalDataCapacity":
			capacity = n
		case "TotalDataAvailable":
			avail = n
		}
	}
	if capacity > 0 && avail >= 0 && capacity >= avail {
		return capacity - avail, nil
	}
	return 0, nil
}

// Run backs up the device and reconstructs BundleID's files into Dest. It
// backs up the WHOLE device (mobilebackup2 cannot target one app), which can
// take a while and needs disk space; only the target app's files are written
// to Dest.
func (o Options) Run(ctx context.Context) (*extract.Result, error) {
	if o.UDID == "" || o.BundleID == "" || o.Dest == "" {
		return nil, fmt.Errorf("backup: UDID, BundleID and Dest are required")
	}
	if err := os.MkdirAll(o.Dest, 0o755); err != nil {
		return nil, err
	}

	// Stage the raw backup under the destination (a user-chosen drive) rather
	// than the system temp dir, so a large full-device backup can land where
	// there is room. Removed after reconstruction unless KeepRaw.
	rawParent := filepath.Join(o.Dest, "_backup-staging")
	if err := os.MkdirAll(rawParent, 0o755); err != nil {
		return nil, err
	}
	if !o.KeepRaw {
		defer os.RemoveAll(rawParent)
	}

	// Pre-flight: estimate the backup size (the device's used data) and make
	// sure the destination drive has room, so we fail fast with a clear message
	// instead of dying partway through a long backup. Best effort -- if either
	// value can't be read we proceed and let idevicebackup2 report a shortage.
	est, _ := EstimateBackupSize(ctx, o.UDID, o.infoBin())
	free, ferr := freeSpace(o.Dest)
	if est > 0 && o.Progress != nil {
		msg := fmt.Sprintf("estimated backup size ~%.1f GB (full device)", gb(est))
		if ferr == nil {
			msg = fmt.Sprintf("estimated backup size ~%.1f GB; destination free ~%.1f GB", gb(est), gb(int64(free)))
		}
		o.Progress(extract.Progress{Path: msg})
	}
	if est > 0 && ferr == nil {
		need := est + est/10 // require the estimate plus a ~10% margin
		if int64(free) < need {
			return nil, fmt.Errorf("not enough free space at %s: ~%.1f GB free, but a full-device backup needs ~%.1f GB (the device's used data). "+
				"Free space or choose a destination with room -- iOS backs up the whole device even to extract one app", o.Dest, gb(int64(free)), gb(need))
		}
	}

	// The backup itself is a black box with no per-file progress; tell the
	// user so a multi-minute, multi-GB backup does not look like a freeze.
	if o.Progress != nil {
		o.Progress(extract.Progress{Path: "backing up the device (a full device backup -- needs free space for the WHOLE device, and can take several minutes)..."})
	}

	// idevicebackup2 -u <UDID> backup <parent>  ->  <parent>/<UDID>/
	// Stream its output live (it prints per-file / percentage progress) rather
	// than buffering to the end, so a long multi-GB backup shows movement.
	cmd := sysproc.CommandContext(ctx, o.bin(), "-u", o.UDID, "backup", rawParent)
	pw := &progressLines{progress: o.Progress}
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Run(); err != nil {
		pw.flush()
		low := strings.ToLower(pw.tailStr())
		if strings.Contains(low, "mberrordomain/105") || strings.Contains(low, "insufficient free disk space") {
			return nil, fmt.Errorf("backup needs room for a full device backup (tens of GB) and the destination drive does not have enough free space. " +
				"Free space, or set the destination to a drive with room for the whole device. iOS backs up the entire device even to extract one app")
		}
		return nil, fmt.Errorf("idevicebackup2 backup failed: %w: %s", err, errLine([]byte(pw.tailStr())))
	}
	pw.flush()

	backupDir := filepath.Join(rawParent, o.UDID)
	if fi, err := os.Stat(backupDir); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("backup: no backup produced at %s (device may have refused; check it is unlocked and trusted)", backupDir)
	}
	if enc, _ := isEncrypted(backupDir); enc {
		return nil, fmt.Errorf("backup: the device makes ENCRYPTED backups; disable \"Encrypt Local Backup\" for this device (encrypted backups are not readable without the backup password)")
	}
	return reconstruct(ctx, backupDir, o)
}

// isEncrypted reports whether the backup's Manifest.plist marks it encrypted.
func isEncrypted(backupDir string) (bool, error) {
	raw, err := os.ReadFile(filepath.Join(backupDir, "Manifest.plist"))
	if err != nil {
		return false, err
	}
	v, err := plist.DecodeAny(raw)
	if err != nil {
		return false, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return false, nil
	}
	b, _ := m["IsEncrypted"].(bool)
	return b, nil
}

// reconstruct copies the target app's backed-up files from their hashed names
// into a readable tree under Dest, grouped by backup domain.
func reconstruct(ctx context.Context, backupDir string, o Options) (*extract.Result, error) {
	dsn := "file:" + filepath.Join(backupDir, "Manifest.db") + "?mode=ro&immutable=1"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("backup: open Manifest.db: %w", err)
	}
	defer db.Close()

	// Match the app's own domain plus any related domain that references the
	// bundle id (app groups, extensions/plugins, keychain-shared containers).
	like := "%" + o.BundleID + "%"
	rows, err := db.QueryContext(ctx,
		`SELECT fileID, domain, relativePath, flags FROM Files
		 WHERE (domain = ? OR domain LIKE ?) AND relativePath IS NOT NULL AND relativePath != ''`,
		"AppDomain-"+o.BundleID, like)
	if err != nil {
		return nil, fmt.Errorf("backup: query Manifest.db: %w", err)
	}
	defer rows.Close()

	res := &extract.Result{Root: o.Dest}
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		var fileID, domain, relPath string
		var flags int
		if err := rows.Scan(&fileID, &domain, &relPath, &flags); err != nil {
			return res, err
		}
		// flags: 1 = file, 2 = directory, 4 = symlink.
		local := extract.SafeJoin(o.Dest, domain+"/"+relPath)
		switch flags {
		case 2:
			if err := os.MkdirAll(local, 0o755); err != nil {
				return res, err
			}
		case 1:
			if len(fileID) < 2 {
				res.Skipped = append(res.Skipped, extract.SkippedFile{Path: domain + "/" + relPath, Reason: "malformed fileID"})
				continue
			}
			src := filepath.Join(backupDir, fileID[:2], fileID)
			n, err := copyFile(src, local)
			if err != nil {
				res.Skipped = append(res.Skipped, extract.SkippedFile{Path: domain + "/" + relPath, Reason: err.Error()})
				continue
			}
			res.FileCount++
			res.ByteCount += n
			if o.Progress != nil {
				o.Progress(extract.Progress{Files: res.FileCount, Bytes: res.ByteCount, Path: domain + "/" + relPath})
			}
		default:
			res.Skipped = append(res.Skipped, extract.SkippedFile{Path: domain + "/" + relPath, Reason: "unsupported backup entry"})
		}
	}
	return res, rows.Err()
}

// copyFile copies src to dst (creating parents), returning bytes written.
func copyFile(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	out, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return n, err
}

// gb converts a byte count to gigabytes (decimal, matching how iOS storage is
// reported) for display.
func gb(b int64) float64 { return float64(b) / 1e9 }

// progressLines forwards a subprocess's output to a progress callback line by
// line as it arrives, so a long-running command shows movement instead of
// freezing. It splits on both '\n' and '\r' so idevicebackup2's carriage-return
// percentage bar updates come through, and keeps the last lines for error
// context. Safe for concurrent stdout+stderr writes.
type progressLines struct {
	progress func(extract.Progress)
	mu       sync.Mutex
	buf      []byte
	tail     []string
}

func (w *progressLines) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexAny(w.buf, "\r\n")
		if i < 0 {
			break
		}
		w.emit(strings.TrimSpace(string(w.buf[:i])))
		w.buf = w.buf[i+1:]
	}
	return len(p), nil
}

func (w *progressLines) emit(line string) {
	if line == "" {
		return
	}
	if w.progress != nil {
		w.progress(extract.Progress{Path: line})
	}
	w.tail = append(w.tail, line)
	if len(w.tail) > 40 {
		w.tail = w.tail[len(w.tail)-40:]
	}
}

func (w *progressLines) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		w.emit(strings.TrimSpace(string(w.buf)))
		w.buf = nil
	}
}

func (w *progressLines) tailStr() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.Join(w.tail, "\n")
}

// errLine picks the most informative line from a tool's output: the last line
// mentioning an error, else the last non-empty line (the summary/failure is
// usually last, not first).
func errLine(b []byte) string {
	var last, errored string
	for _, ln := range strings.Split(string(b), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		last = ln
		if l := strings.ToLower(ln); strings.Contains(l, "error") || strings.Contains(l, "failed") {
			errored = ln
		}
	}
	if errored != "" {
		return errored
	}
	return last
}
