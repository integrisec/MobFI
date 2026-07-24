//go:build !windows

package extract

// safeComponent is identity off Windows: macOS/Linux filesystems allow the
// characters (colons, etc.) that appear in Android filenames, so extracted
// names are preserved verbatim.
func safeComponent(name string) string { return name }
