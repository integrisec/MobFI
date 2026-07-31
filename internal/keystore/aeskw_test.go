package keystore

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// Official RFC 3394 test vectors: unwrapping the ciphertext with the KEK must
// reproduce the key data.
func TestAESUnwrapRFC3394(t *testing.T) {
	cases := []struct {
		name, kek, wrapped, key string
	}{
		{
			"128-KEK/128-key",
			"000102030405060708090A0B0C0D0E0F",
			"1FA68B0A8112B447AEF34BD8FB5A7B829D3E862371D2CFE5",
			"00112233445566778899AABBCCDDEEFF",
		},
		{
			"256-KEK/128-key",
			"000102030405060708090A0B0C0D0E0F101112131415161718191A1B1C1D1E1F",
			"64E8C3F9CE0F5BA263E9777905818A2A93C8191E7D6E8AE7",
			"00112233445566778899AABBCCDDEEFF",
		},
		{
			"256-KEK/256-key",
			"000102030405060708090A0B0C0D0E0F101112131415161718191A1B1C1D1E1F",
			"28C9F404C4B810F4CBCCB35CFB87F8263F5786E2D80ED326CBC7F0E71A99F43BFB988B9B7A02DD21",
			"00112233445566778899AABBCCDDEEFF000102030405060708090A0B0C0D0E0F",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kek, _ := hex.DecodeString(c.kek)
			wrapped, _ := hex.DecodeString(c.wrapped)
			want, _ := hex.DecodeString(c.key)
			got, err := aesUnwrap(kek, wrapped)
			if err != nil {
				t.Fatalf("aesUnwrap: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("aesUnwrap = %x, want %x", got, want)
			}
		})
	}
}

func TestAESUnwrapWrongKey(t *testing.T) {
	kek, _ := hex.DecodeString("00112233445566778899AABBCCDDEEFF00112233445566778899AABBCCDDEEFF")
	wrapped, _ := hex.DecodeString("28C9F404C4B810F4CBCCB35CFB87F8263F5786E2D80ED326CBC7F0E71A99F43BFB988B9B7A02DD21")
	if _, err := aesUnwrap(kek, wrapped); err == nil {
		t.Fatal("expected integrity failure with a wrong KEK")
	}
}
