package plist

import (
	"bytes"
	"encoding/base64"
	"reflect"
	"testing"
)

// xmlFixture is the XML form of the same property list as the binary
// `fixture` in plist_test.go.
const xmlFixture = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>name</key><string>dana</string>
	<key>age</key><integer>42</integer>
	<key>admin</key><true/>
	<key>score</key><real>3.5</real>
	<key>tags</key>
	<array><string>a</string><string>b</string></array>
	<key>blob</key><data>AQID</data>
</dict>
</plist>`

func TestDecodeXML(t *testing.T) {
	v, err := DecodeXML([]byte(xmlFixture))
	if err != nil {
		t.Fatalf("DecodeXML: %v", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("root %T, want map", v)
	}
	if m["name"] != "dana" || m["age"] != int64(42) || m["admin"] != true || m["score"] != 3.5 {
		t.Errorf("scalars wrong: %+v", m)
	}
	tags, _ := m["tags"].([]any)
	if len(tags) != 2 || tags[0] != "a" || tags[1] != "b" {
		t.Errorf("tags = %v", m["tags"])
	}
	if blob, _ := m["blob"].([]byte); !bytes.Equal(blob, []byte{1, 2, 3}) {
		t.Errorf("blob = %v", m["blob"])
	}
}

// TestXMLMatchesBinary confirms both encoders decode to identical Go values,
// which is what lets one FileDiffer compare across formats.
func TestXMLMatchesBinary(t *testing.T) {
	raw, _ := base64.StdEncoding.DecodeString(fixture)
	fromBinary, err := DecodeAny(raw)
	if err != nil {
		t.Fatal(err)
	}
	fromXML, err := DecodeAny([]byte(xmlFixture))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fromBinary, fromXML) {
		t.Errorf("binary and XML decodes differ:\n binary=%#v\n xml=%#v", fromBinary, fromXML)
	}
}

func TestDecodeXMLRejectsNonPlist(t *testing.T) {
	if _, err := DecodeXML([]byte("<html><body>nope</body></html>")); err == nil {
		t.Error("expected error for non-plist XML")
	}
}
