// Package keystore recovers secrets from the platform credential stores -- the
// iOS Keychain and the Android Keystore -- dumping as much as the device's
// state allows and degrading gracefully when it can't.
//
// What's achievable is bounded by hard platform limits: hardware-backed Android
// Keystore private keys and iOS Secure Enclave keys are non-exportable by
// design, so those are only ever inventoried, never dumped. The methods, best
// first:
//
//   - iOS, encrypted backup (any device, incl. non-jailbroken): decrypt the
//     keychain from an encrypted iTunes/Finder backup with the backup password.
//   - iOS, jailbroken (SSH over USB): run keychain_dumper on the device.
//   - Android, rooted (adb): inventory /data/misc/keystore key blobs.
//
// Discovered secret values are redacted unless the caller explicitly opts in.
package keystore

import (
	"context"
	"fmt"
	"unicode/utf8"
)

// Item is one recovered (or inventoried) credential-store entry.
type Item struct {
	Source     string            `json:"source"`               // "keychain" or "keystore"
	Class      string            `json:"class"`                // e.g. "Generic Password", "Key"
	Service    string            `json:"service,omitempty"`    // svce / server / uid
	Account    string            `json:"account,omitempty"`    // acct / alias
	Group      string            `json:"group,omitempty"`      // access group
	Label      string            `json:"label,omitempty"`      // labl
	Accessible string            `json:"accessible,omitempty"` // protection class
	Value      string            `json:"value,omitempty"`      // secret (redacted unless revealed)
	Binary     bool              `json:"binary,omitempty"`     // value is binary (shown as hex)
	Extra      map[string]string `json:"extra,omitempty"`      // server/protocol/port/etc.
}

// Result is the outcome of a dump: the items plus a plain-language account of
// what method ran, what it could not obtain, and why.
type Result struct {
	Platform    string   `json:"platform"`
	Method      string   `json:"method"`
	Degraded    bool     `json:"degraded"`
	Items       []Item   `json:"items"`
	Notes       []string `json:"notes,omitempty"`
	Limitations []string `json:"limitations,omitempty"`
}

// Options controls a dump.
type Options struct {
	Platform  string // "ios" or "android"
	DeviceID  string // UDID (iOS) or serial (Android)
	Transport string // "usb", "tcp", "simulator", ...
	State     string // device state: "jailbroken"/"rooted"/"not detected"/...
	BackupDir string // iOS: path to an encrypted backup (enables the backup method)
	Password  string // iOS: the backup password
	Reveal    bool   // include raw secret values (default: redacted)
}

// Dump selects the best available method for the target and runs it, degrading
// gracefully. When no method can run, it returns a Result explaining why rather
// than an error, so the UI can always show actionable guidance.
func Dump(ctx context.Context, opts Options) (*Result, error) {
	switch opts.Platform {
	case "ios":
		return dumpIOS(ctx, opts)
	case "android":
		return dumpAndroid(ctx, opts)
	default:
		return nil, fmt.Errorf("keystore: unsupported platform %q", opts.Platform)
	}
}

func dumpIOS(ctx context.Context, opts Options) (*Result, error) {
	// Prefer the encrypted-backup route when a backup was provided: it works on
	// stock, non-jailbroken devices.
	if opts.BackupDir != "" {
		res, err := DecryptBackupKeychain(ctx, opts.BackupDir, opts.Password, opts.Reveal)
		if err != nil {
			// Fall through to a jailbreak attempt if possible, else report.
			if opts.State == "jailbroken" {
				if r2, err2 := DumpIOSJailbroken(ctx, opts.DeviceID, opts.Reveal); err2 == nil {
					return r2, nil
				}
			}
			return degradedIOS(opts, fmt.Sprintf("encrypted-backup keychain decryption failed: %v", err)), nil
		}
		return res, nil
	}
	if opts.State == "jailbroken" {
		res, err := DumpIOSJailbroken(ctx, opts.DeviceID, opts.Reveal)
		if err != nil {
			return degradedIOS(opts, fmt.Sprintf("on-device keychain_dumper failed: %v", err)), nil
		}
		return res, nil
	}
	return degradedIOS(opts, ""), nil
}

func degradedIOS(opts Options, why string) *Result {
	r := &Result{Platform: "ios", Method: "unavailable", Degraded: true}
	if why != "" {
		r.Notes = append(r.Notes, why)
	}
	r.Limitations = append(r.Limitations,
		"On a non-jailbroken device, the keychain can only be recovered from an ENCRYPTED backup: make one (MobFI's Extract 'backup' scope with backup encryption enabled), then point this tab at it and supply the backup password.",
		"With a jailbroken device (sshd + keychain_dumper installed), MobFI can dump it directly over USB.",
		"iOS Secure Enclave keys are non-exportable by design and can never be dumped.")
	return r
}

func dumpAndroid(ctx context.Context, opts Options) (*Result, error) {
	if opts.State == "rooted" {
		res, err := DumpAndroidKeystore(ctx, opts.DeviceID, opts.Reveal)
		if err != nil {
			return degradedAndroid(opts, err.Error()), nil
		}
		return res, nil
	}
	return degradedAndroid(opts, ""), nil
}

func degradedAndroid(opts Options, why string) *Result {
	r := &Result{Platform: "android", Method: "unavailable", Degraded: true}
	if why != "" {
		r.Notes = append(r.Notes, why)
	}
	r.Limitations = append(r.Limitations,
		"The Android Keystore is readable only on a ROOTED device (its blobs live under /data/misc/keystore, owned by root).",
		"Hardware-backed (TEE/StrongBox) private keys are non-exportable by design: even with root, only an inventory of which keys exist is possible, not the key material.",
		"To capture the plaintext keys/secrets an app uses, hook it at runtime with Frida/objection instead of dumping storage.")
	return r
}

// --- value rendering --------------------------------------------------------

// renderValue turns a raw secret value into a display string, honoring
// redaction. It reports whether the value is binary (shown as hex when
// revealed).
func renderValue(data []byte, reveal bool) (string, bool) {
	if len(data) == 0 {
		return "", false
	}
	binary := !isText(data)
	if !reveal {
		kind := "text"
		if binary {
			kind = "binary"
		}
		return fmt.Sprintf("[hidden %s, %d bytes]", kind, len(data)), binary
	}
	if binary {
		return hexPreview(data), true
	}
	return string(data), false
}

func isText(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	for len(b) > 0 {
		r, n := utf8.DecodeRune(b)
		if r == utf8.RuneError && n == 1 {
			return false
		}
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
		b = b[n:]
	}
	return true
}

func hexPreview(b []byte) string {
	const max = 512
	trunc := false
	if len(b) > max {
		b = b[:max]
		trunc = true
	}
	const hexdig = "0123456789abcdef"
	out := make([]byte, 0, len(b)*3)
	for i, c := range b {
		if i > 0 {
			out = append(out, ' ')
		}
		out = append(out, hexdig[c>>4], hexdig[c&0xf])
	}
	s := string(out)
	if trunc {
		s += " ..."
	}
	return s
}
