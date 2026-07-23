package plist

import (
	"bytes"
	"encoding/base64"
	"testing"
)

// fixture is a binary plist produced by Python's plistlib for:
//
//	{"name":"dana","age":42,"admin":true,"score":3.5,
//	 "tags":["a","b"],"blob":b"\x01\x02\x03"}
const fixture = "YnBsaXN0MDDWAQIDBAUGBwgJCgsMVWFkbWluU2FnZVRibG9iVG5hbWVVc2NvcmVUdGFncwkQKkMBAgNUZGFuYSNADAAAAAAAAKINDlFhUWIIFRsfJCkvNDU3O0BJTE4AAAAAAAABAQAAAAAAAAAPAAAAAAAAAAAAAAAAAAAAUA=="

func decodeFixture(t *testing.T) map[string]any {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(fixture)
	if err != nil {
		t.Fatal(err)
	}
	v, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("root is %T, want map", v)
	}
	return m
}

func TestDecodeScalars(t *testing.T) {
	m := decodeFixture(t)

	if m["name"] != "dana" {
		t.Errorf("name = %v", m["name"])
	}
	if m["age"] != int64(42) {
		t.Errorf("age = %v (%T), want int64 42", m["age"], m["age"])
	}
	if m["admin"] != true {
		t.Errorf("admin = %v", m["admin"])
	}
	if m["score"] != 3.5 {
		t.Errorf("score = %v (%T), want float64 3.5", m["score"], m["score"])
	}
}

func TestDecodeArrayAndData(t *testing.T) {
	m := decodeFixture(t)

	tags, ok := m["tags"].([]any)
	if !ok || len(tags) != 2 || tags[0] != "a" || tags[1] != "b" {
		t.Errorf("tags = %v", m["tags"])
	}
	blob, ok := m["blob"].([]byte)
	if !ok || !bytes.Equal(blob, []byte{1, 2, 3}) {
		t.Errorf("blob = %v", m["blob"])
	}
}

func TestDecodeRejectsNonPlist(t *testing.T) {
	if _, err := Decode([]byte("not a plist at all")); err == nil {
		t.Error("expected error for non-plist input")
	}
}

func TestDecodeTruncatedDoesNotPanic(t *testing.T) {
	raw, _ := base64.StdEncoding.DecodeString(fixture)
	// Truncate into the object region; must error, not panic.
	if _, err := Decode(raw[:len(raw)-40]); err == nil {
		t.Error("expected error for truncated plist")
	}
}
