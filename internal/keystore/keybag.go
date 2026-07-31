package keystore

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/crypto/pbkdf2"
)

// The iOS keybag stores the class keys used to protect backup files and
// keychain items. In an encrypted backup the class keys are themselves wrapped
// with a key derived from the backup password. This file parses the keybag
// (a TLV blob) and, given the password, unwraps the class keys so file and
// item keys can be recovered.
//
// References: Apple iOS Security whitepaper; the iphone-dataprotection and
// Mobile Verification Toolkit (mvt) projects.

const (
	wrapDevice   = 1 // class key wrapped with the (device-only) hardware key
	wrapPasscode = 2 // class key wrapped with the passcode/backup-password key
)

// classKey is one protection-class key from the keybag.
type classKey struct {
	class   int
	wrap    uint32
	wrapped []byte // WPKY: the wrapped key
	key     []byte // unwrapped key (populated after unlock)
}

// keybag holds the parsed keybag attributes and class keys.
type keybag struct {
	attrs     map[string][]byte
	classKeys map[int]*classKey
}

// keybagHeaderTags are the top-level (non-class-key) TLV tags.
var keybagHeaderTags = map[string]bool{
	"VERS": true, "TYPE": true, "HMCK": true, "SALT": true,
	"ITER": true, "DPWT": true, "DPIC": true, "DPSL": true, "GRCE": true,
}

// parseKeybag decodes a binary keybag blob into its attributes and class keys.
func parseKeybag(blob []byte) (*keybag, error) {
	kb := &keybag{attrs: map[string][]byte{}, classKeys: map[int]*classKey{}}
	var cur *classKey
	sawUUID := false

	for off := 0; off+8 <= len(blob); {
		tag := string(blob[off : off+4])
		n := int(binary.BigEndian.Uint32(blob[off+4 : off+8]))
		off += 8
		if off+n > len(blob) {
			return nil, errors.New("keystore: truncated keybag")
		}
		val := blob[off : off+n]
		off += n

		switch tag {
		case "UUID":
			if !sawUUID {
				sawUUID = true
				kb.attrs["UUID"] = append([]byte(nil), val...)
			} else {
				if cur != nil {
					kb.classKeys[cur.class] = cur
				}
				cur = &classKey{}
			}
		case "CLAS":
			if cur != nil {
				cur.class = int(be32(val)) & 0xF
			}
		case "WRAP":
			if cur != nil {
				cur.wrap = be32(val)
			} else {
				kb.attrs["WRAP"] = append([]byte(nil), val...)
			}
		case "WPKY":
			if cur != nil {
				cur.wrapped = append([]byte(nil), val...)
			}
		case "KTYP", "PBKY":
			// key type / public key: not needed for unwrapping
		default:
			if keybagHeaderTags[tag] {
				kb.attrs[tag] = append([]byte(nil), val...)
			}
		}
	}
	if cur != nil {
		kb.classKeys[cur.class] = cur
	}
	if len(kb.classKeys) == 0 {
		return nil, errors.New("keystore: no class keys in keybag")
	}
	return kb, nil
}

// unlock derives the passcode key from the backup password and unwraps every
// passcode-wrapped class key. Device-wrapped keys (whose KEK lives only on the
// device) cannot be recovered from a backup and are left locked.
func (kb *keybag) unlock(password string) error {
	salt := kb.attrs["SALT"]
	iter := int(be32(kb.attrs["ITER"]))
	dpsl := kb.attrs["DPSL"]
	dpic := int(be32(kb.attrs["DPIC"]))
	if len(salt) == 0 || iter == 0 {
		return errors.New("keystore: keybag missing SALT/ITER (not an encrypted-backup keybag?)")
	}

	// Backups use a double PBKDF2: first SHA-256 with the DPSL salt / DPIC
	// iterations, then SHA-1 with the SALT / ITER.
	pass := []byte(password)
	if len(dpsl) > 0 && dpic > 0 {
		pass = pbkdf2.Key(pass, dpsl, dpic, 32, sha256.New)
	}
	passcodeKey := pbkdf2.Key(pass, salt, iter, 32, sha1.New)

	unlocked := 0
	for _, ck := range kb.classKeys {
		if ck.wrap&wrapPasscode == 0 || len(ck.wrapped) == 0 {
			continue // device-only key: not recoverable from a backup
		}
		key, err := aesUnwrap(passcodeKey, ck.wrapped)
		if err != nil {
			if errors.Is(err, errIntegrity) {
				return fmt.Errorf("keystore: could not unlock keybag (wrong backup password?)")
			}
			continue
		}
		ck.key = key
		unlocked++
	}
	if unlocked == 0 {
		return errors.New("keystore: no class keys could be unlocked with the given password")
	}
	return nil
}

// unwrapForClass recovers a per-file or per-item key that was wrapped with the
// given protection class's key.
func (kb *keybag) unwrapForClass(class int, wrapped []byte) ([]byte, error) {
	ck := kb.classKeys[class&0xF]
	if ck == nil || len(ck.key) == 0 {
		return nil, fmt.Errorf("keystore: class %d key not available (device-protected or locked)", class)
	}
	return aesUnwrap(ck.key, wrapped)
}

func be32(b []byte) uint32 {
	if len(b) < 4 {
		return 0
	}
	return binary.BigEndian.Uint32(b[:4])
}
