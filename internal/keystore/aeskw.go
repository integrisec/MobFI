package keystore

import (
	"crypto/aes"
	"crypto/subtle"
	"encoding/binary"
	"errors"
)

// aesUnwrap implements the AES Key Unwrap algorithm defined in RFC 3394
// (https://datatracker.ietf.org/doc/html/rfc3394). It is used to recover
// the iOS keybag class keys and per-file/-item keys, which are all wrapped
// with a key-encryption key (KEK) using this construction.
//
// wrapped is n+1 64-bit blocks; the result is n 64-bit blocks. It returns an
// error if the integrity check (the default IV 0xA6A6A6A6A6A6A6A6 defined
// in RFC 3394 section 2.2.3) fails -- which is how a wrong KEK (e.g. wrong
// backup password) is detected. Variable names (a, r, t, buf) mirror the
// RFC's pseudocode notation.
func aesUnwrap(kek, wrapped []byte) ([]byte, error) {
	if len(wrapped) < 24 || len(wrapped)%8 != 0 {
		return nil, errors.New("aesUnwrap: ciphertext must be a multiple of 8 bytes and at least 24")
	}
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, err
	}
	n := len(wrapped)/8 - 1

	a := make([]byte, 8)
	copy(a, wrapped[:8])
	r := make([]byte, n*8)
	copy(r, wrapped[8:])

	buf := make([]byte, 16)
	for j := 5; j >= 0; j-- {
		for i := n; i >= 1; i-- {
			// B = AES-decrypt(KEK, (A ^ t) | R[i]), t = n*j + i
			t := uint64(n*j + i)
			copy(buf[:8], a)
			tb := make([]byte, 8)
			binary.BigEndian.PutUint64(tb, t)
			for k := 0; k < 8; k++ {
				buf[k] ^= tb[k]
			}
			copy(buf[8:], r[(i-1)*8:i*8])
			block.Decrypt(buf, buf)
			copy(a, buf[:8])
			copy(r[(i-1)*8:i*8], buf[8:])
		}
	}

	var iv = []byte{0xa6, 0xa6, 0xa6, 0xa6, 0xa6, 0xa6, 0xa6, 0xa6}
	if subtle.ConstantTimeCompare(a, iv) != 1 {
		return nil, errIntegrity
	}
	return r, nil
}

// errIntegrity means an AES-unwrap integrity check failed -- almost always a
// wrong key (e.g. an incorrect backup password).
var errIntegrity = errors.New("aesUnwrap: integrity check failed (wrong key/password?)")
