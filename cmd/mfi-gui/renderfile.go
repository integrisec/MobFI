package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

const (
	maxImageBytes = 32 << 20  // 32 MB
	maxPDFBytes   = 64 << 20  // 64 MB
	maxHexBytes   = 256 << 10 // 256 KB
	maxTextBytes  = 4 << 20   // 4 MB for prettify
)

// FSEntry is one directory entry for the render file tree.
type FSEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Dir  bool   `json:"dir"`
}

// OpenExternally opens a path in the OS default application.
func (g *GUI) OpenExternally(path string) error {
	if path == "" {
		return fmt.Errorf("no path")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	default: // linux, bsd, …
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}

// ListDir lists a directory (directories first, then files, case-insensitive).
func (g *GUI) ListDir(path string) ([]FSEntry, error) {
	ents, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]FSEntry, 0, len(ents))
	for _, e := range ents {
		out = append(out, FSEntry{Name: e.Name(), Path: filepath.Join(path, e.Name()), Dir: e.IsDir()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// RenderResult is a rendered view of a file for the GUI.
type RenderResult struct {
	Kind    string `json:"kind"` // image | pdf | code | text | hex | toolarge | error
	MIME    string `json:"mime"`
	Text    string `json:"text"`     // plain text or hex dump
	HTML    string `json:"html"`     // syntax-highlighted code
	DataURL string `json:"data_url"` // image / pdf
	Name    string `json:"name"`
	Size    int64  `json:"size"` // file size in bytes
}

// RenderPath renders a file. mode "hex" forces a hex dump; "auto" detects the
// type: images and PDFs are returned as data URLs, structured/text formats go
// through the shared renderer (XML reindent, plist decode, SQLite summary,
// …) and code-like content is syntax-highlighted. When pretty is set, JSON
// and XML are reformatted (indented) regardless of file extension.
func (g *GUI) RenderPath(path, mode string, pretty bool) (RenderResult, error) {
	r := RenderResult{Name: filepath.Base(path)}
	info, err := os.Stat(path)
	if err != nil {
		r.Kind, r.Text = "error", err.Error()
		return r, nil
	}
	if info.IsDir() {
		r.Kind, r.Text = "text", "(folder — select a file)"
		return r, nil
	}
	r.Size = info.Size()
	ext := strings.ToLower(filepath.Ext(path))

	if mode == "hex" {
		res := hexResult(path)
		res.Size = r.Size
		return res, nil
	}

	if mime := imageMIME(ext); mime != "" {
		r.MIME = mime
		if info.Size() > maxImageBytes {
			r.Kind = "toolarge"
			return r, nil
		}
		if b, err := readCapped(path, maxImageBytes); err == nil {
			r.Kind = "image"
			r.DataURL = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
			return r, nil
		}
	}
	if ext == ".pdf" || fileHasPrefix(path, "%PDF") {
		r.MIME = "application/pdf"
		if info.Size() > maxPDFBytes {
			r.Kind = "toolarge"
			return r, nil
		}
		if b, err := readCapped(path, maxPDFBytes); err == nil {
			r.Kind = "pdf"
			r.DataURL = "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(b)
			return r, nil
		}
	}

	// Prettify JSON/XML on request, even for files with no/odd extension.
	if pretty {
		if raw, err := readCapped(path, maxTextBytes); err == nil {
			if formatted, lexer, mime, ok := tryPrettify(raw); ok {
				r.Kind, r.MIME, r.HTML = "code", mime, highlight(formatted, lexer)
				return r, nil
			}
		}
	}

	// Structured/text formats via the shared renderer.
	view, err := g.app.Render(g.ctx, path)
	if err != nil || view == nil {
		return hexResult(path), nil
	}
	r.MIME = view.MIME

	if lexer := pickLexer(path, view.Text); lexer != nil {
		r.Kind, r.HTML = "code", highlight(view.Text, lexer)
		return r, nil
	}
	r.Kind, r.Text = "text", view.Text
	return r, nil
}

// tryPrettify reformats JSON or XML content. Returns the formatted text, a
// lexer, its MIME, and whether it applied.
func tryPrettify(raw []byte) (string, chroma.Lexer, string, bool) {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 {
		return "", nil, "", false
	}
	switch t[0] {
	case '{', '[':
		var buf bytes.Buffer
		if json.Indent(&buf, t, "", "  ") == nil {
			if lx := lexers.Get("json"); lx != nil {
				return buf.String(), lx, "application/json", true
			}
		}
	case '<':
		if s, err := reindentXML(t); err == nil {
			if lx := lexers.Get("xml"); lx != nil {
				return s, lx, "application/xml", true
			}
		}
	}
	return "", nil, "", false
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

func hexResult(path string) RenderResult {
	b, _ := readCapped(path, maxHexBytes)
	return RenderResult{Kind: "hex", MIME: "application/octet-stream", Text: hexDump(b), Name: filepath.Base(path)}
}

// pickLexer resolves a Chroma lexer by filename then by content, returning nil
// for plain (non-code) text so it renders without highlighting.
func pickLexer(path, content string) chroma.Lexer {
	lexer := lexers.Match(filepath.Base(path))
	if lexer == nil {
		lexer = lexers.Analyse(content)
	}
	if lexer == nil {
		return nil
	}
	if cfg := lexer.Config(); cfg != nil && strings.EqualFold(cfg.Name, "plaintext") {
		return nil
	}
	return lexer
}

func highlight(source string, lexer chroma.Lexer) string {
	style := styles.Get("github-dark")
	if style == nil {
		style = styles.Fallback
	}
	formatter := chromahtml.New(chromahtml.WithClasses(false), chromahtml.TabWidth(4))
	it, err := lexer.Tokenise(nil, source)
	if err != nil {
		return "<pre>" + htmlEscape(source) + "</pre>"
	}
	var buf bytes.Buffer
	if err := formatter.Format(&buf, style, it); err != nil {
		return "<pre>" + htmlEscape(source) + "</pre>"
	}
	return buf.String()
}

func imageMIME(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".tif", ".tiff":
		return "image/tiff"
	}
	return ""
}

func readCapped(path string, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, max)
	n, _ := f.Read(buf)
	return buf[:n], nil
}

func fileHasPrefix(path, prefix string) bool {
	b, err := readCapped(path, int64(len(prefix)))
	return err == nil && bytes.HasPrefix(b, []byte(prefix))
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
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
	if len(b) == maxHexBytes {
		sb.WriteString("... (truncated)\n")
	}
	return sb.String()
}
