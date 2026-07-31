package plist

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// DecodeAny decodes a property list in either binary or XML form into the
// same Go value shape.
func DecodeAny(data []byte) (any, error) {
	if IsBinary(data) {
		return Decode(data)
	}
	return DecodeXML(data)
}

// LooksXML reports whether data is (plausibly) an XML property list.
func LooksXML(data []byte) bool {
	head := data
	if len(head) > 1024 {
		head = head[:1024]
	}
	return bytes.Contains(head, []byte("<plist")) ||
		(bytes.Contains(head, []byte("<?xml")) && bytes.Contains(head, []byte("PropertyList")))
}

// DecodeXML parses an XML property list into plain Go values, mirroring the
// types produced by Decode (binary). It bounds recursion depth at
// maxPlistDepth so a crafted `<array><array>...</array></array>` chain
// cannot exhaust the goroutine stack (MFI-PAR-02).
func DecodeXML(data []byte) (v any, err error) {
	// Match Decode()'s recover -- encoding/xml can panic on some pathological
	// inputs, and no matter how well-bounded the walk is a panic that
	// escapes into the Wails runtime is worse than a returned error.
	defer func() {
		if r := recover(); r != nil {
			v, err = nil, fmt.Errorf("plist: malformed xml input: %v", r)
		}
	}()

	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, terr := dec.Token()
		if terr == io.EOF {
			return nil, errors.New("plist: missing <plist> element")
		}
		if terr != nil {
			return nil, terr
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "plist" {
			return parseXMLValue(dec, 0)
		}
	}
}

// parseXMLValue returns the first value element within the current parent.
func parseXMLValue(dec *xml.Decoder, depth int) (any, error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			return parseXMLElement(dec, t, depth)
		case xml.EndElement:
			return nil, nil // e.g. an empty <plist></plist>
		}
	}
}

func parseXMLElement(dec *xml.Decoder, start xml.StartElement, depth int) (any, error) {
	if depth > maxPlistDepth {
		return nil, fmt.Errorf("plist: xml nested too deep (> %d)", maxPlistDepth)
	}
	switch start.Name.Local {
	case "true":
		_, err := text(dec)
		return true, err
	case "false":
		_, err := text(dec)
		return false, err
	case "string", "key":
		return text(dec)
	case "integer":
		s, err := text(dec)
		if err != nil {
			return nil, err
		}
		return strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	case "real":
		s, err := text(dec)
		if err != nil {
			return nil, err
		}
		return strconv.ParseFloat(strings.TrimSpace(s), 64)
	case "date":
		s, err := text(dec)
		if err != nil {
			return nil, err
		}
		return time.Parse(time.RFC3339, strings.TrimSpace(s))
	case "data":
		s, err := text(dec)
		if err != nil {
			return nil, err
		}
		return base64.StdEncoding.DecodeString(stripSpace(s))
	case "array":
		return parseXMLArray(dec, depth+1)
	case "dict":
		return parseXMLDict(dec, depth+1)
	default:
		return nil, dec.Skip() // unknown element: consume it
	}
}

func parseXMLArray(dec *xml.Decoder, depth int) (any, error) {
	if depth > maxPlistDepth {
		return nil, fmt.Errorf("plist: xml array nested too deep (> %d)", maxPlistDepth)
	}
	var out []any
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			v, err := parseXMLElement(dec, t, depth)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		case xml.EndElement:
			return out, nil
		}
	}
}

func parseXMLDict(dec *xml.Decoder, depth int) (any, error) {
	if depth > maxPlistDepth {
		return nil, fmt.Errorf("plist: xml dict nested too deep (> %d)", maxPlistDepth)
	}
	out := make(map[string]any)
	haveKey := false
	var key string
	for {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Local == "key" {
				k, err := text(dec)
				if err != nil {
					return nil, err
				}
				key, haveKey = k, true
				continue
			}
			if !haveKey {
				return nil, errors.New("plist: dict value without a preceding <key>")
			}
			v, err := parseXMLElement(dec, t, depth)
			if err != nil {
				return nil, err
			}
			out[key] = v
			haveKey = false
		case xml.EndElement:
			return out, nil
		}
	}
}

// text accumulates character data up to the matching end element, so it also
// consumes the end tag of empty elements like <true/>.
func text(dec *xml.Decoder) (string, error) {
	var sb strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.CharData:
			sb.Write(t)
		case xml.StartElement:
			if err := dec.Skip(); err != nil { // unexpected nested element
				return "", err
			}
		case xml.EndElement:
			return sb.String(), nil
		}
	}
}

func stripSpace(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\t' || r == '\r' {
			return -1
		}
		return r
	}, s)
}
