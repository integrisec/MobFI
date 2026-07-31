package keystore

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // cgo-free SQLite driver

	"github.com/integrisec/MobFI/internal/plist"
)

// DecryptBackupKeychain recovers keychain items from an ENCRYPTED iTunes/Finder
// backup directory, given the backup password. It works on non-jailbroken
// devices: the backup (as produced by MobFI's `backup` extraction scope, i.e.
// idevicebackup2) must have been made with backup encryption enabled.
//
// The pipeline: parse Manifest.plist -> unlock the backup keybag with the
// password -> decrypt Manifest.db -> locate & decrypt the keychain file ->
// parse its items. Every stage degrades gracefully, reporting what it could and
// couldn't recover rather than failing hard.
//
// Note: keychain items whose protection class is "ThisDeviceOnly" (and Secure
// Enclave keys) are excluded from backups by iOS and cannot appear here.
func DecryptBackupKeychain(ctx context.Context, backupDir, password string, reveal bool) (*Result, error) {
	res := &Result{Platform: "ios", Method: "encrypted backup"}

	manifest, err := readPlistFile(filepath.Join(backupDir, "Manifest.plist"))
	if err != nil {
		return nil, fmt.Errorf("read Manifest.plist: %w (is %q an iOS backup directory?)", err, backupDir)
	}
	mm, _ := manifest.(map[string]any)
	if mm == nil {
		return nil, errors.New("Manifest.plist is not a dictionary")
	}
	if enc, _ := mm["IsEncrypted"].(bool); !enc {
		return nil, errors.New("this backup is not encrypted; the keychain is only present in encrypted backups (enable backup encryption and re-run)")
	}
	kbBlob, _ := mm["BackupKeyBag"].([]byte)
	if len(kbBlob) == 0 {
		return nil, errors.New("Manifest.plist has no BackupKeyBag")
	}
	kb, err := parseKeybag(kbBlob)
	if err != nil {
		return nil, err
	}
	if err := kb.unlock(password); err != nil {
		return nil, err
	}
	res.Notes = append(res.Notes, "Unlocked the backup keybag.")

	// Decrypt Manifest.db with the wrapped ManifestKey.
	manKey, _ := mm["ManifestKey"].([]byte)
	if len(manKey) < 4 {
		return nil, errors.New("Manifest.plist has no ManifestKey")
	}
	manClass := int(binary.LittleEndian.Uint32(manKey[:4]))
	manFileKey, err := kb.unwrapForClass(manClass, manKey[4:])
	if err != nil {
		return nil, fmt.Errorf("unwrap ManifestKey: %w", err)
	}
	encDB, err := os.ReadFile(filepath.Join(backupDir, "Manifest.db"))
	if err != nil {
		return nil, fmt.Errorf("read Manifest.db: %w", err)
	}
	plainDB, err := aesCBCDecrypt(manFileKey, encDB)
	if err != nil {
		return nil, fmt.Errorf("decrypt Manifest.db: %w", err)
	}

	fileID, fileBlob, err := findKeychainEntry(ctx, plainDB)
	if err != nil {
		return nil, err
	}

	// The keychain file itself is encrypted with a per-file key wrapped in the
	// keybag; its metadata (class + wrapped key + size) is in the file blob.
	fileKey, size, err := fileKeyFromBlob(kb, fileBlob)
	if err != nil {
		return nil, fmt.Errorf("keychain file key: %w", err)
	}
	encKC, err := readBackupFile(backupDir, fileID)
	if err != nil {
		return nil, fmt.Errorf("read keychain file: %w", err)
	}
	plainKC, err := aesCBCDecrypt(fileKey, encKC)
	if err != nil {
		return nil, fmt.Errorf("decrypt keychain file: %w", err)
	}
	if size > 0 && size <= len(plainKC) {
		plainKC = plainKC[:size]
	}

	items, notes := parseKeychainPlist(plainKC, reveal)
	res.Items = items
	res.Notes = append(res.Notes, notes...)
	res.Notes = append(res.Notes, fmt.Sprintf("Recovered %d keychain item(s).", len(items)))
	res.Limitations = append(res.Limitations,
		"Items protected 'ThisDeviceOnly' and Secure Enclave keys are excluded from backups by iOS and cannot be recovered here.")
	return res, nil
}

// findKeychainEntry decrypts nothing further; it opens the (already-decrypted)
// Manifest.db and returns the keychain file's ID and its file blob.
func findKeychainEntry(ctx context.Context, plainDB []byte) (fileID string, fileBlob []byte, err error) {
	tmp, err := os.CreateTemp("", "mfi-manifest-*.db")
	if err != nil {
		return "", nil, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(plainDB); err != nil {
		tmp.Close()
		return "", nil, err
	}
	tmp.Close()

	db, err := sql.Open("sqlite", "file:"+tmp.Name()+"?mode=ro&immutable=1")
	if err != nil {
		return "", nil, fmt.Errorf("open Manifest.db: %w", err)
	}
	defer db.Close()
	row := db.QueryRowContext(ctx,
		`SELECT fileID, file FROM Files WHERE domain='KeychainDomain' AND relativePath='keychain-backup.plist' LIMIT 1`)
	if err := row.Scan(&fileID, &fileBlob); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil, errors.New("no keychain entry in Manifest.db (KeychainDomain/keychain-backup.plist)")
		}
		return "", nil, fmt.Errorf("query Manifest.db: %w", err)
	}
	return fileID, fileBlob, nil
}

// fileKeyFromBlob walks the NSKeyedArchiver file blob to find the wrapped
// EncryptionKey and Size, then unwraps the file key with the keybag.
func fileKeyFromBlob(kb *keybag, blob []byte) (key []byte, size int, err error) {
	v, err := plist.DecodeAny(blob)
	if err != nil {
		return nil, 0, fmt.Errorf("decode file blob: %w", err)
	}
	top, _ := v.(map[string]any)
	if top == nil {
		return nil, 0, errors.New("file blob is not a dictionary")
	}
	objects, _ := top["$objects"].([]any)

	// Resolve the root MBFile object (keyed archiver) or use the dict directly.
	mbfile := top
	if objects != nil {
		if t, ok := top["$top"].(map[string]any); ok {
			if rootUID, ok := t["root"].(plist.UID); ok {
				if d, ok := objAt(objects, rootUID).(map[string]any); ok {
					mbfile = d
				}
			}
		}
	}

	size = toInt(mbfile["Size"])
	rawKey := resolveData(objects, mbfile["EncryptionKey"])
	if len(rawKey) < 4 {
		return nil, size, errors.New("no EncryptionKey in file blob (file may be unencrypted)")
	}
	class := int(binary.LittleEndian.Uint32(rawKey[:4]))
	fk, err := kb.unwrapForClass(class, rawKey[4:])
	if err != nil {
		return nil, size, err
	}
	return fk, size, nil
}

// parseKeychainPlist extracts items from the decrypted keychain plist. Values
// are redacted unless reveal is set.
func parseKeychainPlist(data []byte, reveal bool) (items []Item, notes []string) {
	v, err := plist.DecodeAny(data)
	if err != nil {
		return nil, []string{"could not parse the decrypted keychain (unexpected format)"}
	}
	kc, _ := v.(map[string]any)
	if kc == nil {
		return nil, []string{"decrypted keychain was not a dictionary"}
	}
	sections := []struct{ key, class string }{
		{"genp", "Generic Password"},
		{"inet", "Internet Password"},
		{"cert", "Certificate"},
		{"keys", "Key"},
	}
	for _, sec := range sections {
		arr, _ := kc[sec.key].([]any)
		for _, raw := range arr {
			m, _ := raw.(map[string]any)
			if m == nil {
				continue
			}
			items = append(items, keychainItem(sec.class, m, reveal))
		}
	}
	return items, notes
}

func keychainItem(class string, m map[string]any, reveal bool) Item {
	it := Item{
		Source:     "keychain",
		Class:      class,
		Service:    str(m["svce"]),
		Account:    str(m["acct"]),
		Group:      str(m["agrp"]),
		Label:      str(m["labl"]),
		Accessible: accessibleName(str(m["pdmn"])),
		Extra:      map[string]string{},
	}
	if it.Service == "" { // internet passwords use srvr/ptcl instead of svce
		it.Service = str(m["srvr"])
	}
	for k, disp := range map[string]string{"srvr": "server", "ptcl": "protocol", "port": "port", "path": "path"} {
		if s := str(m[k]); s != "" {
			it.Extra[disp] = s
		}
	}
	data, _ := m["v_Data"].([]byte)
	it.Value, it.Binary = renderValue(data, reveal)
	return it
}

// --- crypto / io helpers ----------------------------------------------------

func aesCBCDecrypt(key, ct []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext length %d is not a multiple of the block size", len(ct))
	}
	out := make([]byte, len(ct))
	iv := make([]byte, aes.BlockSize) // iOS backup files use a zero IV
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, ct)
	return out, nil
}

// readBackupFile reads a backup file by its 40-hex fileID, trying the iOS 10+
// sharded layout (<dir>/ab/abcdef...) and the older flat layout.
func readBackupFile(backupDir, fileID string) ([]byte, error) {
	candidates := []string{
		filepath.Join(backupDir, fileID[:2], fileID),
		filepath.Join(backupDir, fileID),
	}
	var firstErr error
	for _, p := range candidates {
		if b, err := os.ReadFile(p); err == nil {
			return b, nil
		} else if firstErr == nil {
			firstErr = err
		}
	}
	return nil, firstErr
}

func readPlistFile(path string) (any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return plist.DecodeAny(b)
}

// objAt indexes a keyed-archiver $objects array by a UID reference.
func objAt(objects []any, uid plist.UID) any {
	i := int(uid)
	if i < 0 || i >= len(objects) {
		return nil
	}
	return objects[i]
}

// resolveData returns the bytes for a value that may be raw []byte, or a UID
// reference into a keyed-archiver $objects array pointing at an {NS.data: ...}.
func resolveData(objects []any, v any) []byte {
	switch t := v.(type) {
	case []byte:
		return t
	case plist.UID:
		obj := objAt(objects, t)
		if d, ok := obj.(map[string]any); ok {
			if b, ok := d["NS.data"].([]byte); ok {
				return b
			}
		}
		if b, ok := obj.([]byte); ok {
			return b
		}
	}
	return nil
}

func toInt(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case uint64:
		return int(t)
	case float64:
		return int(t)
	}
	return 0
}

func str(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	}
	return ""
}

// accessibleName maps a keychain protection-class code (pdmn) to a readable
// accessibility label.
func accessibleName(pdmn string) string {
	switch pdmn {
	case "ak":
		return "AfterFirstUnlock"
	case "ck":
		return "AfterFirstUnlockThisDeviceOnly"
	case "dk":
		return "Always"
	case "aku":
		return "WhenUnlocked"
	case "cku":
		return "WhenUnlockedThisDeviceOnly"
	case "dku":
		return "AlwaysThisDeviceOnly"
	case "akpu":
		return "WhenPasscodeSetThisDeviceOnly"
	case "":
		return ""
	}
	return pdmn
}
