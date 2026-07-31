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
	"github.com/integrisec/MobFI/internal/sysproc"
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

// DirState reports whether a path exists and (for a directory) is empty.
type DirState struct {
	Exists bool `json:"exists"`
	Empty  bool `json:"empty"`
}

// DirStatus checks an extraction destination before writing to it.
func (g *GUI) DirStatus(path string) DirState {
	fi, err := os.Stat(path)
	if err != nil {
		return DirState{Exists: false, Empty: true}
	}
	if !fi.IsDir() {
		return DirState{Exists: true, Empty: false}
	}
	entries, err := os.ReadDir(path)
	return DirState{Exists: true, Empty: err == nil && len(entries) == 0}
}

// RemoveDir deletes a directory tree, with guards against removing obviously
// dangerous paths (root, home, top-level).
func (g *GUI) RemoveDir(path string) error {
	p := filepath.Clean(path)
	if p == "" || p == "/" || p == "." || p == ".." {
		return fmt.Errorf("refusing to remove %q", p)
	}
	if home, err := os.UserHomeDir(); err == nil && p == filepath.Clean(home) {
		return fmt.Errorf("refusing to remove the home directory")
	}
	if len(strings.Split(strings.Trim(filepath.ToSlash(p), "/"), "/")) < 2 {
		return fmt.Errorf("refusing to remove a top-level path %q", p)
	}
	return os.RemoveAll(p)
}

// FileMeta is filesystem metadata for a file, shown in the Render details
// panel. Times are pre-formatted in the host's local zone; Created is empty
// on platforms that do not expose a birth time.
type FileMeta struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	IsDir    bool   `json:"is_dir"`
	Mode     string `json:"mode"`     // e.g. -rw-r--r--
	Ext      string `json:"ext"`      // lowercased extension including the dot
	Modified string `json:"modified"` // local time, "2006-01-02 15:04:05"
	Created  string `json:"created"`  // birth time if the OS provides it
}

// FileStat returns filesystem metadata for path (used by the Render details
// panel alongside the rendered content type).
func (g *GUI) FileStat(path string) (FileMeta, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return FileMeta{}, err
	}
	const layout = "2006-01-02 15:04:05"
	m := FileMeta{
		Name:     fi.Name(),
		Path:     path,
		Size:     fi.Size(),
		IsDir:    fi.IsDir(),
		Mode:     fi.Mode().String(),
		Ext:      strings.ToLower(filepath.Ext(path)),
		Modified: fi.ModTime().Local().Format(layout),
	}
	if bt, ok := birthTime(fi); ok {
		m.Created = bt.Local().Format(layout)
	}
	return m, nil
}

// dangerousOpenExtensions are file types the OS shell handler would execute
// (or that would launch a script host) rather than open in a viewer. A
// hostile app on the device can drop a `receipt.pdf.exe` or `.command` into
// its data tree; if the operator clicks "Open externally" on it, the OS runs
// the payload. Extraction is not a trust boundary for these types.
var dangerousOpenExtensions = map[string]bool{
	".exe": true, ".bat": true, ".cmd": true, ".com": true, ".ps1": true,
	".psm1": true, ".vbs": true, ".vbe": true, ".js": true, ".jse": true,
	".wsf": true, ".wsh": true, ".hta": true, ".msi": true, ".msp": true,
	".scr": true, ".pif": true, ".cpl": true, ".lnk": true, ".url": true,
	".jar": true, ".app": true, ".command": true, ".workflow": true,
	".sh": true, ".zsh": true, ".bash": true, ".fish": true, ".py": true,
	".pyw": true, ".rb": true, ".pl": true, ".php": true,
}

// OpenExternally opens a path in the OS default application.
//
// MFI-CMD-05: never let path be parsed as a flag by the OS launcher.
// macOS `open <path>` and Linux `xdg-open <path>` both treat a leading `-`
// as an option (`-a AppName`, `-b bundleid`, `-e`, etc.); a file with such
// a name on the device would open the wrong thing. On macOS `open` accepts
// `--` to end option parsing; on Linux we normalise a leading `-` to `./-`
// which every path-taker accepts as a literal filename. On Windows the
// `cmd /c start "" <path>` form is already unambiguous, but the path is
// still `cd`-quoted to defend against embedded quotes.
//
// MFI-GUI-07: reject file extensions whose OS handler would execute rather
// than display. A hostile app plants `statement.pdf.lnk`; without this
// guard, "Open externally" launches the shortcut target.
func (g *GUI) OpenExternally(path string) error {
	if path == "" {
		return fmt.Errorf("no path")
	}
	// Strip a trailing dot Windows would ignore, then check the extension.
	ext := strings.ToLower(filepath.Ext(strings.TrimRight(path, ".")))
	if dangerousOpenExtensions[ext] {
		return fmt.Errorf("refusing to open externally: %q is a %s file whose OS handler would execute it rather than display", filepath.Base(path), ext)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = sysproc.Command("open", "--", path)
	case "windows":
		cmd = sysproc.Command("cmd", "/c", "start", "", path)
	default: // linux, bsd, ...
		normal := path
		if strings.HasPrefix(normal, "-") {
			normal = "./" + normal
		}
		cmd = sysproc.Command("xdg-open", normal)
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

	// A SQLite database (by header, regardless of extension: .sqlite, .db, or
	// none) should always open as a database, not a hex dump. The shared
	// renderer's table summary can fail on a locked/unusual DB, but it is still
	// browsable in the Database tab, so surface it as such either way.
	if fileHasPrefix(path, "SQLite format 3\x00") {
		r.MIME = "application/vnd.sqlite3"
		if view, err := g.app.Render(g.ctx, path); err == nil && view != nil {
			r.Kind, r.Text = "text", view.Text
		} else {
			r.Kind, r.Text = "text", "SQLite database — open it in the Database tab to browse its tables."
		}
		return r, nil
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

// DecodeView is a syntax-highlighted rendering of a decoded value for the
// Decode tab.
type DecodeView struct {
	HTML string `json:"html"` // highlighted HTML (empty when not recognisably code)
	Lang string `json:"lang"` // detected language name, e.g. "JSON"
}

// HighlightValue detects the syntax of a decoded string and returns
// syntax-highlighted HTML (optionally pretty-printing JSON/XML) for the Decode
// tab, reusing the same Chroma pipeline as the Render tab. When the content
// isn't recognisably code, HTML is empty and the caller renders plain text.
func (g *GUI) HighlightValue(text string, pretty bool) DecodeView {
	if strings.TrimSpace(text) == "" {
		return DecodeView{}
	}
	// Detect JSON/XML by content so it highlights even when not reformatting;
	// reindent only when pretty is set.
	if formatted, lexer, _, ok := tryPrettify([]byte(text)); ok {
		src := text
		if pretty {
			src = formatted
		}
		return DecodeView{HTML: highlight(src, lexer), Lang: lexerName(lexer)}
	}
	if lexer := pickLexer("", text); lexer != nil {
		return DecodeView{HTML: highlight(text, lexer), Lang: lexerName(lexer)}
	}
	return DecodeView{}
}

func lexerName(l chroma.Lexer) string {
	if l == nil {
		return ""
	}
	if c := l.Config(); c != nil {
		return c.Name
	}
	return ""
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
