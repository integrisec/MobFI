// Package render turns native Android/iOS file types (XML, plist, web
// caches, config, proprietary formats) into a human-readable view.
// Renderers are pluggable and consulted in priority order; a hex-dump
// renderer is the final catch-all.
package render

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/integrisec/MobFI/internal/dbview"
	"github.com/integrisec/MobFI/internal/plist"
)

const (
	maxRenderBytes = 1 << 20 // cap for text/structured rendering
	maxHexBytes    = 8 << 10 // cap for hex dumps
	sniffLen       = 512     // bytes read to classify a file
)

var (
	sqliteMagic = []byte("SQLite format 3\x00")
	bplistMagic = []byte("bplist00")
)

// View is a rendered, human-readable representation of a file.
type View struct {
	MIME string `json:"mime"` // best-guess content type
	Text string `json:"text"` // rendered text form
}

// Renderer produces a View for files it recognises.
type Renderer interface {
	// Handles reports whether this renderer recognises the file at path.
	Handles(path string) bool
	// Render produces a human-readable view of the file.
	Render(ctx context.Context, path string) (*View, error)
}

// Registry selects a renderer for a file.
type Registry struct {
	renderers []Renderer
}

// DefaultRegistry returns the registry with all built-in renderers, in
// priority order (specific formats first, hex dump last).
func DefaultRegistry() *Registry {
	r := &Registry{}
	r.Add(sqliteRenderer{})
	r.Add(jsonRenderer{})
	r.Add(plistRenderer{})
	r.Add(xmlRenderer{})
	r.Add(textRenderer{})
	r.Add(hexRenderer{}) // catch-all
	return r
}

// Add registers a renderer.
func (r *Registry) Add(rr Renderer) { r.renderers = append(r.renderers, rr) }

// Render finds the first renderer that handles path and renders it.
func (r *Registry) Render(ctx context.Context, path string) (*View, error) {
	for _, rr := range r.renderers {
		if rr.Handles(path) {
			return rr.Render(ctx, path)
		}
	}
	return nil, ErrNoRenderer
}

// ErrNoRenderer means no registered renderer recognised the file.
var ErrNoRenderer = errors.New("no renderer for file")

// --- JSON ---

type jsonRenderer struct{}

func (jsonRenderer) Handles(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".json")
}

func (jsonRenderer) Render(_ context.Context, path string) (*View, error) {
	b, _, err := readCapped(path, maxRenderBytes)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, b, "", "  "); err != nil {
		// Not valid JSON after all; show it verbatim.
		return &View{MIME: "application/json", Text: string(b)}, nil
	}
	return &View{MIME: "application/json", Text: out.String()}, nil
}

// --- plist (binary + XML) ---

type plistRenderer struct{}

func (plistRenderer) Handles(path string) bool {
	if strings.EqualFold(filepath.Ext(path), ".plist") {
		return true
	}
	return bytes.HasPrefix(readSniff(path, len(bplistMagic)), bplistMagic)
}

func (plistRenderer) Render(_ context.Context, path string) (*View, error) {
	b, _, err := readCapped(path, maxRenderBytes)
	if err != nil {
		return nil, err
	}
	if plist.IsBinary(b) {
		v, err := plist.Decode(b)
		if err != nil {
			return &View{
				MIME: "application/x-plist",
				Text: "<binary plist; decode failed: " + err.Error() + ">\n\n" + hexDump(cap2(b, maxHexBytes)),
			}, nil
		}
		out, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return nil, err
		}
		return &View{MIME: "application/x-plist", Text: string(out)}, nil
	}
	// XML plist: reindent like any XML.
	pretty, err := reindentXML(b)
	if err != nil {
		return &View{MIME: "application/x-plist", Text: string(b)}, nil
	}
	return &View{MIME: "application/x-plist", Text: pretty}, nil
}

// --- XML ---

type xmlRenderer struct{}

func (xmlRenderer) Handles(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".xml")
}

func (xmlRenderer) Render(_ context.Context, path string) (*View, error) {
	b, _, err := readCapped(path, maxRenderBytes)
	if err != nil {
		return nil, err
	}
	pretty, err := reindentXML(b)
	if err != nil {
		return &View{MIME: "application/xml", Text: string(b)}, nil
	}
	return &View{MIME: "application/xml", Text: pretty}, nil
}

func reindentXML(b []byte) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(b))
	var out bytes.Buffer
	enc := xml.NewEncoder(&out)
	enc.Indent("", "  ")
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		if err := enc.EncodeToken(tok); err != nil {
			return "", err
		}
	}
	if err := enc.Flush(); err != nil {
		return "", err
	}
	return out.String(), nil
}

// --- SQLite summary ---

type sqliteRenderer struct{}

func (sqliteRenderer) Handles(path string) bool {
	return bytes.Equal(readSniff(path, len(sqliteMagic)), sqliteMagic)
}

func (sqliteRenderer) Render(ctx context.Context, path string) (*View, error) {
	db, err := dbview.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	tables, err := db.Tables(ctx)
	if err != nil {
		return nil, err
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "SQLite database — %d table(s):\n", len(tables))
	for _, t := range tables {
		fmt.Fprintf(&sb, "  %s\n", t)
	}
	sb.WriteString("\n(use `mfi db -file <path> -table <name>` to read rows)\n")
	return &View{MIME: "application/vnd.sqlite3", Text: sb.String()}, nil
}

// --- plain text ---

type textRenderer struct{}

func (textRenderer) Handles(path string) bool {
	return looksText(readSniff(path, sniffLen))
}

func (textRenderer) Render(_ context.Context, path string) (*View, error) {
	b, truncated, err := readCapped(path, maxRenderBytes)
	if err != nil {
		return nil, err
	}
	text := string(b)
	if truncated {
		text += fmt.Sprintf("\n... (truncated at %d bytes)\n", maxRenderBytes)
	}
	return &View{MIME: "text/plain", Text: text}, nil
}

// --- hex dump (catch-all) ---

type hexRenderer struct{}

func (hexRenderer) Handles(string) bool { return true }

func (hexRenderer) Render(_ context.Context, path string) (*View, error) {
	b, truncated, err := readCapped(path, maxHexBytes)
	if err != nil {
		return nil, err
	}
	text := hexDump(b)
	if truncated {
		text += fmt.Sprintf("... (truncated at %d bytes)\n", maxHexBytes)
	}
	return &View{MIME: "application/octet-stream", Text: text}, nil
}

// --- helpers ---

func readCapped(path string, max int64) ([]byte, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, max))
	if err != nil {
		return nil, false, err
	}
	return b, int64(len(b)) == max, nil
}

func readSniff(path string, n int) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	b := make([]byte, n)
	m, _ := io.ReadFull(f, b)
	return b[:m]
}

func looksText(b []byte) bool {
	if len(b) == 0 || !utf8.Valid(b) {
		return len(b) == 0
	}
	return bytes.IndexByte(b, 0) < 0
}

func cap2(b []byte, n int) []byte {
	if len(b) > n {
		return b[:n]
	}
	return b
}

func hexDump(b []byte) string {
	var sb strings.Builder
	for i := 0; i < len(b); i += 16 {
		end := i + 16
		if end > len(b) {
			end = len(b)
		}
		fmt.Fprintf(&sb, "%08x  ", i)
		for j := i; j < i+16; j++ {
			if j < end {
				fmt.Fprintf(&sb, "%02x ", b[j])
			} else {
				sb.WriteString("   ")
			}
			if j-i == 7 {
				sb.WriteByte(' ')
			}
		}
		sb.WriteString(" |")
		for j := i; j < end; j++ {
			c := b[j]
			if c < 0x20 || c > 0x7e {
				c = '.'
			}
			sb.WriteByte(c)
		}
		sb.WriteString("|\n")
	}
	return sb.String()
}
