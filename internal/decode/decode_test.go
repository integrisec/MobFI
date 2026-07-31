package decode

import "testing"

func TestBase64(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"SGVsbG8sIHdvcmxk", "Hello, world", true}, // raw (no padding)
		{"SGVsbG8sIHdvcmxk=", "", false},           // bad padding
		{"aGVsbG8=", "hello", true},                // padded std
		{"cART_-9w", "", true},                     // URL-safe alphabet, decodes to binary
		{"not base64!!", "", false},
		{"", "", false},
	} {
		r := Base64(tc.in)
		if r.OK != tc.ok {
			t.Errorf("Base64(%q).OK = %v, want %v (err=%q)", tc.in, r.OK, tc.ok, r.Error)
			continue
		}
		if tc.ok && !r.Binary && r.Value != tc.want {
			t.Errorf("Base64(%q).Value = %q, want %q", tc.in, r.Value, tc.want)
		}
	}
}

func TestHex(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"48656c6c6f", "Hello", true},
		{"48 65 6c 6c 6f", "Hello", true},
		{"0x4865", "He", true},
		{"48:65:6c", "Hel", true},
		{"abc", "", false}, // odd digits
		{"xyz", "", false}, // no hex digits
	} {
		r := Hex(tc.in)
		if r.OK != tc.ok {
			t.Errorf("Hex(%q).OK = %v, want %v (err=%q)", tc.in, r.OK, tc.ok, r.Error)
			continue
		}
		if tc.ok && r.Value != tc.want {
			t.Errorf("Hex(%q).Value = %q, want %q", tc.in, r.Value, tc.want)
		}
	}
}

func TestURL(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"a%20b%2Fc", "a b/c", true},
		{"key%3Dval%26x%3D1", "key=val&x=1", true},
		{"plain-string", "", false}, // no percent-encoding
		{"%zz", "", false},          // invalid escape
	} {
		r := URL(tc.in)
		if r.OK != tc.ok {
			t.Errorf("URL(%q).OK = %v, want %v (err=%q)", tc.in, r.OK, tc.ok, r.Error)
			continue
		}
		if tc.ok && r.Value != tc.want {
			t.Errorf("URL(%q).Value = %q, want %q", tc.in, r.Value, tc.want)
		}
	}
}

func TestBinaryFlagAndHex(t *testing.T) {
	r := Hex("00ff10")
	if !r.OK || !r.Binary {
		t.Fatalf("expected OK binary result, got OK=%v Binary=%v", r.OK, r.Binary)
	}
	if r.Hex != "00 ff 10" {
		t.Errorf("Hex view = %q, want %q", r.Hex, "00 ff 10")
	}
}

func TestAll(t *testing.T) {
	got := All("SGVsbG8=")
	if len(got) != 3 {
		t.Fatalf("All returned %d results, want 3", len(got))
	}
	names := []string{"Base64", "Hex", "URL"}
	for i, n := range names {
		if got[i].Name != n {
			t.Errorf("result %d name = %q, want %q", i, got[i].Name, n)
		}
	}
}
