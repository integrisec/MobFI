package keystore

import (
	"bytes"
	"crypto/aes"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"testing"

	"golang.org/x/crypto/pbkdf2"
)

// aesWrap is the forward AES Key Wrap (RFC 3394), used only by tests to build a
// synthetic keybag that aesUnwrap must then reverse.
func aesWrap(t *testing.T, kek, plain []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(kek)
	if err != nil {
		t.Fatal(err)
	}
	n := len(plain) / 8
	a := []byte{0xa6, 0xa6, 0xa6, 0xa6, 0xa6, 0xa6, 0xa6, 0xa6}
	r := append([]byte(nil), plain...)
	buf := make([]byte, 16)
	for j := 0; j <= 5; j++ {
		for i := 1; i <= n; i++ {
			copy(buf[:8], a)
			copy(buf[8:], r[(i-1)*8:i*8])
			block.Encrypt(buf, buf)
			copy(a, buf[:8])
			t2 := uint64(n*j + i)
			tb := make([]byte, 8)
			binary.BigEndian.PutUint64(tb, t2)
			for k := 0; k < 8; k++ {
				a[k] ^= tb[k]
			}
			copy(r[(i-1)*8:i*8], buf[8:])
		}
	}
	return append(append([]byte(nil), a...), r...)
}

func tlv(tag string, val []byte) []byte {
	b := make([]byte, 8+len(val))
	copy(b[:4], tag)
	binary.BigEndian.PutUint32(b[4:8], uint32(len(val)))
	copy(b[8:], val)
	return b
}

func u32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

func TestKeybagUnlockRoundTrip(t *testing.T) {
	const password = "correct horse battery staple"
	salt := []byte("0123456789abcdef")
	dpsl := []byte("fedcba9876543210")
	iter, dpic := 1000, 1000

	// Derive the passcode key exactly as unlock() does.
	pass := pbkdf2.Key([]byte(password), dpsl, dpic, 32, sha256.New)
	passcodeKey := pbkdf2.Key(pass, salt, iter, 32, sha1.New)

	// A class-3 key, wrapped with the passcode key.
	class3 := make([]byte, 32)
	rand.Read(class3)
	wrapped := aesWrap(t, passcodeKey, class3)

	// Assemble a keybag: header attrs, then one class key.
	var blob bytes.Buffer
	blob.Write(tlv("VERS", u32(4)))
	blob.Write(tlv("TYPE", u32(1)))
	blob.Write(tlv("UUID", make([]byte, 16))) // keybag UUID (header)
	blob.Write(tlv("HMCK", make([]byte, 40)))
	blob.Write(tlv("WRAP", u32(1)))
	blob.Write(tlv("SALT", salt))
	blob.Write(tlv("ITER", u32(uint32(iter))))
	blob.Write(tlv("DPWT", u32(1)))
	blob.Write(tlv("DPIC", u32(uint32(dpic))))
	blob.Write(tlv("DPSL", dpsl))
	// class key
	blob.Write(tlv("UUID", make([]byte, 16)))
	blob.Write(tlv("CLAS", u32(3)))
	blob.Write(tlv("WRAP", u32(wrapPasscode)))
	blob.Write(tlv("KTYP", u32(0)))
	blob.Write(tlv("WPKY", wrapped))

	kb, err := parseKeybag(blob.Bytes())
	if err != nil {
		t.Fatalf("parseKeybag: %v", err)
	}
	if len(kb.classKeys) != 1 || kb.classKeys[3] == nil {
		t.Fatalf("expected one class-3 key, got %v", kb.classKeys)
	}
	if err := kb.unlock(password); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if !bytes.Equal(kb.classKeys[3].key, class3) {
		t.Fatalf("recovered class key mismatch")
	}

	// A per-item key wrapped with the class-3 key must round-trip too.
	itemKey := make([]byte, 32)
	rand.Read(itemKey)
	itemWrapped := aesWrap(t, class3, itemKey)
	got, err := kb.unwrapForClass(3, itemWrapped)
	if err != nil {
		t.Fatalf("unwrapForClass: %v", err)
	}
	if !bytes.Equal(got, itemKey) {
		t.Fatalf("unwrapForClass mismatch")
	}

	// A wrong password must be rejected by the integrity check.
	kb2, _ := parseKeybag(blob.Bytes())
	if err := kb2.unlock("wrong password"); err == nil {
		t.Fatal("expected unlock to fail with a wrong password")
	}
}
