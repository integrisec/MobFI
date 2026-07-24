package extract

import (
	"fmt"
	"path/filepath"
	"strings"
)

// safeLocalPath joins dest with a device-relative, slash-separated path,
// sanitizing each component for the host filesystem via safeComponent. This
// lets filenames that are legal on the device but not on the host (e.g. a
// Firebase config file "frc_1:proj:android:appid_..." on Windows, where ':'
// is reserved) still be written locally. Path separators are handled by the
// split, so directory structure is preserved.
func safeLocalPath(dest, slashRel string) string {
	parts := strings.Split(slashRel, "/")
	for i, p := range parts {
		parts[i] = safeComponent(p)
	}
	return filepath.Join(append([]string{dest}, parts...)...)
}

// windowsSafeName makes one path component a legal Windows filename: it
// percent-encodes the characters Windows forbids (<>:"|?* and control codes),
// trims a trailing dot or space (also illegal), and escapes reserved device
// names (CON, NUL, COM1, ...). It is used by safeComponent on Windows and is
// exported to tests. The percent-encoding is unambiguous and keeps the name
// readable (e.g. "a:b" -> "a%3Ab").
func windowsSafeName(name string) string {
	if name == "" || name == "." || name == ".." {
		return name
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20, r == '<', r == '>', r == ':', r == '"', r == '|', r == '?', r == '*':
			fmt.Fprintf(&b, "%%%02X", r)
		default:
			b.WriteRune(r)
		}
	}
	s := strings.TrimRight(b.String(), " .")
	if s == "" {
		return "_"
	}
	stem := s
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	if winReserved[strings.ToUpper(stem)] {
		return "_" + s
	}
	return s
}

var winReserved = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}
