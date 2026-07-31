// Package decode provides small, dependency-free decoders for inspecting
// encoded strings pulled out of files, databases, or secret findings: Base64
// (standard and URL-safe, padded or raw), ASCII hex, and URL percent-encoding.
// Each decoder reports whether it applied, the decoded value as text, and a
// hex view of the raw bytes so binary results are still inspectable.
package decode

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

// Result is a single decoder's attempt over an input string.
type Result struct {
	Name   string `json:"name"`            // human label: "Base64", "Hex", "URL"
	OK     bool   `json:"ok"`              // decoding applied and succeeded
	Value  string `json:"value"`           // decoded bytes as text (best effort)
	Hex    string `json:"hex"`             // space-grouped hex of the decoded bytes
	Binary bool   `json:"binary"`          // decoded bytes are not printable text
	Error  string `json:"error,omitempty"` // why it did not apply (when !OK)
}

// hexPreviewBytes caps the hex view so a huge decode doesn't bloat the output.
const hexPreviewBytes = 4096

// All runs every decoder over s, in display order.
func All(s string) []Result {
	return []Result{Base64(s), Hex(s), URL(s)}
}

// Base64 decodes s, trying standard and URL-safe alphabets, padded or raw.
func Base64(s string) Result {
	r := Result{Name: "Base64"}
	t := strings.TrimSpace(s)
	if t == "" {
		r.Error = "empty input"
		return r
	}
	for _, e := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	} {
		if b, err := e.DecodeString(t); err == nil {
			return finish(r, b)
		}
	}
	r.Error = "not valid Base64"
	return r
}

// Hex decodes a string of hex digits. Whitespace, "0x" prefixes, and common
// separators (":", "-", ",") are ignored, so "48 65", "0x4865" and "48:65"
// all work.
func Hex(s string) Result {
	r := Result{Name: "Hex"}
	t := cleanHex(s)
	if t == "" {
		r.Error = "no hex digits"
		return r
	}
	if len(t)%2 != 0 {
		r.Error = "odd number of hex digits"
		return r
	}
	b, err := hex.DecodeString(t)
	if err != nil {
		r.Error = "not valid hex"
		return r
	}
	return finish(r, b)
}

// URL percent-decodes s (form style: "+" becomes a space). It only applies when
// the input actually contains a percent-escape, to avoid reporting every plain
// string as a no-op "decode".
func URL(s string) Result {
	r := Result{Name: "URL"}
	t := strings.TrimSpace(s)
	if t == "" {
		r.Error = "empty input"
		return r
	}
	if !strings.Contains(t, "%") {
		r.Error = "no percent-encoding found"
		return r
	}
	out, err := url.QueryUnescape(t)
	if err != nil {
		if out, err = url.PathUnescape(t); err != nil {
			r.Error = "not valid URL encoding"
			return r
		}
	}
	return finish(r, []byte(out))
}

// cleanHex lowercases s and keeps only hex digits, dropping "0x" markers and any
// separators (spaces, colons, dashes, commas).
func cleanHex(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '0' && i+1 < len(s) && s[i+1] == 'x' { // skip an 0x marker
			i++
			continue
		}
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func finish(r Result, b []byte) Result {
	r.OK = true
	r.Value = string(b)
	r.Hex = spacedHex(b)
	r.Binary = !printable(b)
	return r
}

func spacedHex(b []byte) string {
	trunc := false
	if len(b) > hexPreviewBytes {
		b = b[:hexPreviewBytes]
		trunc = true
	}
	parts := make([]string, len(b))
	for i, c := range b {
		parts[i] = fmt.Sprintf("%02x", c)
	}
	out := strings.Join(parts, " ")
	if trunc {
		out += " ..."
	}
	return out
}

// printable reports whether b is valid UTF-8 with no control characters other
// than tab/newline/carriage-return -- i.e. it reads as text rather than binary.
func printable(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size == 1 {
			return false
		}
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
		b = b[size:]
	}
	return true
}
