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
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

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
		msg := fmt.Sprintf("device reports ~%.1f GB used (a full backup can be larger)", gb(est))
		if ferr == nil {
			msg = fmt.Sprintf("device reports ~%.1f GB used (a full backup can be larger); destination free ~%.1f GB", gb(est), gb(int64(free)))
		}
		o.Progress(extract.Progress{Path: msg})
	}
	if est > 0 && ferr == nil {
		// The estimate (device used data) understates the real backup size, so
		// only hard-fail when the destination has less than even the estimate --
		// a clearly-doomed run; larger shortfalls surface as MBErrorDomain/105.
		if int64(free) < est {
			return nil, fmt.Errorf("not enough free space at %s: ~%.1f GB free, but the device already reports ~%.1f GB used and a full backup is usually larger. "+
				"Free space or choose a destination with room -- iOS backs up the whole device even to extract one app", o.Dest, gb(int64(free)), gb(est))
		}
	}

	// The backup itself is a black box with no per-file progress; tell the
	// user so a multi-minute, multi-GB backup does not look like a freeze.
	if o.Progress != nil {
		o.Progress(extract.Progress{Path: "backing up the device (a full device backup -- needs free space for the WHOLE device, and can take several minutes)..."})
	}

	// idevicebackup2 -u <UDID> backup <parent>  ->  <parent>/<UDID>/
	// idevicebackup2 only prints per-file progress, so report OVERALL progress
	// by polling how much the staging dir has grown against the estimate. Its
	// own output is captured (for error context) but not shown, to avoid the
	// per-file bar competing with the overall figure.
	cmd := sysproc.CommandContext(ctx, o.bin(), "-u", o.UDID, "backup", rawParent)
	// On cancel, idevicebackup2 is killed; WaitDelay bounds how long Run then
	// waits for its I/O to drain so Cancel returns promptly instead of hanging.
	cmd.WaitDelay = 5 * time.Second
	pw := &progressLines{pct: -1} // capture-only: error tail + overall percent
	cmd.Stdout = pw
	cmd.Stderr = pw

	stop := make(chan struct{})
	var wg sync.WaitGroup
	if o.Progress != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t := time.NewTicker(3 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-stop:
					return
				case <-ctx.Done():
					// Cancelled: stop reporting "backing up..." immediately so
					// the UI reflects the cancel without waiting for the kill.
					o.Progress(extract.Progress{Path: "cancelling backup and cleaning up..."})
					return
				case <-t.C:
					sz := dirSize(rawParent)
					if p := pw.percent(); p >= 0 {
						// Some idevicebackup2 versions do report a real overall
						// percentage; use it when present.
						o.Progress(extract.Progress{Path: fmt.Sprintf("backing up the device: %d%% (%.1f GB backed up)", p, gb(sz))})
					} else {
						// Most versions report only per-file progress, and the
						// device's used-data figure understates the true backup
						// size -- so show the real bytes written, not a misleading
						// total or percentage.
						o.Progress(extract.Progress{Path: fmt.Sprintf("backing up the device: %.1f GB backed up so far (full device backup)", gb(sz))})
					}
				}
			}
		}()
	}

	err := cmd.Run()
	close(stop)
	wg.Wait()
	pw.flush()
	if err != nil {
		low := strings.ToLower(pw.tailStr())
		if strings.Contains(low, "mberrordomain/105") || strings.Contains(low, "insufficient free disk space") {
			return nil, fmt.Errorf("backup needs room for a full device backup (tens of GB) and the destination drive does not have enough free space. " +
				"Free space, or set the destination to a drive with room for the whole device. iOS backs up the entire device even to extract one app")
		}
		return nil, fmt.Errorf("idevicebackup2 backup failed: %w: %s", err, errLine([]byte(pw.tailStr())))
	}

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
	// MFI-PATH-03: build the sqlite URI via net/url so a backupDir containing
	// `?` / `#` / `%` (or Windows backslashes) does not confuse the driver's
	// URI parser. filepath.ToSlash normalises Windows separators for the URI.
	manifestURI := (&url.URL{
		Scheme:   "file",
		Path:     filepath.ToSlash(filepath.Join(backupDir, "Manifest.db")),
		RawQuery: "mode=ro&immutable=1",
	}).String()
	dsn := manifestURI
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("backup: open Manifest.db: %w", err)
	}
	defer db.Close()

	// Match the app's own domain plus any related domain that references the
	// bundle id (app groups, extensions/plugins, keychain-shared containers).
	like := "%" + o.BundleID + "%"

	// List the domains being extracted so the user sees coverage beyond the
	// app's main container (app groups, extensions, plugins, ...).
	if o.Progress != nil {
		if domains := distinctDomains(ctx, db, "AppDomain-"+o.BundleID, like); len(domains) > 0 {
			o.Progress(extract.Progress{Path: fmt.Sprintf("extracting %d domain(s): %s", len(domains), strings.Join(domains, ", "))})
		}
	}

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
		// The Manifest.db is on the same trust footing as the device: an
		// attacker with control of a compromised backup can populate
		// relativePath with "../../.ssh/authorized_keys" (or a NUL / leading
		// slash on Windows). SafeJoin sanitises per-component filename
		// characters but Clean still resolves ".." lexically, so a boundary
		// check is required here just as extract.Run applies one on the
		// live-device walk path.
		if strings.ContainsAny(relPath, "\x00") || strings.HasPrefix(relPath, "/") || strings.HasPrefix(relPath, `\`) {
			res.Skipped = append(res.Skipped, extract.SkippedFile{Path: domain + "/" + relPath, Reason: "invalid relative path"})
			continue
		}
		// flags: 1 = file, 2 = directory, 4 = symlink.
		local := extract.SafeJoin(o.Dest, domain+"/"+relPath)
		if !extract.Within(o.Dest, local) {
			res.Skipped = append(res.Skipped, extract.SkippedFile{Path: domain + "/" + relPath, Reason: "path escapes destination"})
			continue
		}
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
		case 4:
			// A symlink in the backup points at an on-device path that does not
			// exist locally, so it is not reconstructed (nothing is lost).
			res.Skipped = append(res.Skipped, extract.SkippedFile{Path: domain + "/" + relPath, Reason: "symlink (not extracted)"})
		default:
			res.Skipped = append(res.Skipped, extract.SkippedFile{Path: domain + "/" + relPath, Reason: fmt.Sprintf("unsupported entry (flags=%d)", flags)})
		}
	}
	return res, rows.Err()
}

// distinctDomains returns the distinct backup domains that match the app (its
// own AppDomain plus any domain referencing the bundle id), for reporting which
// containers are being extracted.
func distinctDomains(ctx context.Context, db *sql.DB, appDomain, like string) []string {
	rows, err := db.QueryContext(ctx,
		`SELECT DISTINCT domain FROM Files WHERE domain = ? OR domain LIKE ? ORDER BY domain`,
		appDomain, like)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if rows.Scan(&d) == nil && d != "" {
			out = append(out, d)
		}
	}
	return out
}

// copyFile copies src to dst (creating parents), returning bytes written.
// Delegates the destination open to extract.OpenLocalForWrite so a pre-
// planted symlink at dst does not redirect the write to a target outside
// the reconstruction tree (MFI-PATH-02).
func copyFile(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	out, err := extract.OpenLocalForWrite(dst)
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

// dirSize sums the sizes of all regular files under root (best effort; unreadable
// entries are skipped). Used to gauge overall backup progress.
func dirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, e := d.Info(); e == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

// overallPctRe matches idevicebackup2's OVERALL progress bar -- "[====] NN%"
// with nothing after the percent. Its per-file bar appends " (current/total)"
// byte counts, so the end-anchor deliberately excludes those.
var overallPctRe = regexp.MustCompile(`]\s*(\d{1,3})%\s*$`)

// progressLines forwards a subprocess's output to a progress callback line by
// line as it arrives, so a long-running command shows movement instead of
// freezing. It splits on both '\n' and '\r' so idevicebackup2's carriage-return
// percentage bar updates come through, keeps the last lines for error context,
// and extracts the latest overall percentage. Safe for concurrent writes.
type progressLines struct {
	progress func(extract.Progress)
	mu       sync.Mutex
	buf      []byte
	tail     []string
	pct      int // latest overall percent parsed (-1 = none seen yet)
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
	if m := overallPctRe.FindStringSubmatch(line); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n >= 0 && n <= 100 {
			w.pct = n
		}
	}
	if w.progress != nil {
		w.progress(extract.Progress{Path: line})
	}
	w.tail = append(w.tail, line)
	if len(w.tail) > 40 {
		w.tail = w.tail[len(w.tail)-40:]
	}
}

// percent returns the latest overall percent parsed, or -1 if none seen.
func (w *progressLines) percent() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.pct
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
