package keystore

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/integrisec/MobFI/internal/sysproc"
)

// DumpIOSJailbroken runs keychain_dumper on a jailbroken device over SSH (USB,
// tunneled with iproxy) and parses its output. It requires the device to have
// sshd running (key-based auth) and keychain_dumper installed.
func DumpIOSJailbroken(ctx context.Context, udid string, reveal bool) (*Result, error) {
	local, err := freePort()
	if err != nil {
		return nil, err
	}
	// Forward a local port to the device's SSH port over USB.
	iproxy := sysproc.CommandContext(ctx, "iproxy", "-u", udid, fmt.Sprintf("%d:22", local))
	if err := iproxy.Start(); err != nil {
		return nil, fmt.Errorf("iproxy (is libimobiledevice installed?): %w", err)
	}
	defer func() {
		if iproxy.Process != nil {
			_ = iproxy.Process.Kill()
		}
	}()
	if err := waitPort(local, 4*time.Second); err != nil {
		return nil, fmt.Errorf("iproxy forward not ready (is the device jailbroken with sshd?): %w", err)
	}

	out, err := sysproc.CommandContext(ctx, "ssh",
		"-p", strconv.Itoa(local),
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "BatchMode=yes",
		"root@127.0.0.1", "keychain_dumper",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("ssh keychain_dumper (need sshd key auth + keychain_dumper on the device): %w", err)
	}

	res := &Result{Platform: "ios", Method: "keychain_dumper (SSH over USB)"}
	res.Items = parseKeychainDumper(string(out), reveal)
	res.Notes = append(res.Notes, fmt.Sprintf("Recovered %d keychain item(s) from the device.", len(res.Items)))
	res.Limitations = append(res.Limitations,
		"keychain_dumper returns items accessible with its entitlements; Secure Enclave keys remain non-exportable.")
	return res, nil
}

// parseKeychainDumper parses keychain_dumper's textual output into items. The
// tool prints blocks of "Field: value" lines separated by blank lines, grouped
// under section headers (Generic Password, Internet Password, Certificate, Key).
func parseKeychainDumper(out string, reveal bool) []Item {
	var items []Item
	section := "Generic Password"
	var cur map[string]string
	flush := func() {
		if cur == nil {
			return
		}
		items = append(items, keychainDumperItem(section, cur, reveal))
		cur = nil
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			flush()
		case isDumperSection(trimmed):
			flush()
			section = normalizeSection(trimmed)
		default:
			k, v, ok := splitField(trimmed)
			if !ok {
				continue
			}
			if cur == nil {
				cur = map[string]string{}
			}
			cur[strings.ToLower(k)] = v
		}
	}
	flush()
	return items
}

func keychainDumperItem(section string, f map[string]string, reveal bool) Item {
	it := Item{
		Source:     "keychain",
		Class:      section,
		Service:    firstOf(f, "service", "server"),
		Account:    f["account"],
		Group:      firstOf(f, "entitlement group", "access group", "keychain group"),
		Label:      f["label"],
		Accessible: f["accessible attribute"],
		Extra:      map[string]string{},
	}
	for _, k := range []string{"protocol", "port", "path", "generic field", "description"} {
		if v := f[k]; v != "" {
			it.Extra[k] = v
		}
	}
	data := firstOf(f, "keychain data", "data")
	it.Value, it.Binary = renderValue([]byte(data), reveal)
	return it
}

func isDumperSection(s string) bool {
	switch strings.ToLower(strings.TrimRight(s, "s :")) {
	case "generic password", "internet password", "certificate", "key", "identitie", "identity":
		return true
	}
	return false
}

func normalizeSection(s string) string {
	l := strings.ToLower(strings.TrimRight(s, "s :"))
	switch l {
	case "generic password":
		return "Generic Password"
	case "internet password":
		return "Internet Password"
	case "certificate":
		return "Certificate"
	case "key":
		return "Key"
	default:
		return "Identity"
	}
}

func splitField(line string) (key, val string, ok bool) {
	i := strings.Index(line, ":")
	if i <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
}

func firstOf(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := m[k]; v != "" {
			return v
		}
	}
	return ""
}

// --- Android ----------------------------------------------------------------

// androidKeystoreProbe lists the keystore blob directory and reports whether the
// keystore2 database is present. Run via `adb shell su -c`.
const androidKeystoreProbe = `su -c 'ls -1 /data/misc/keystore/user_0/ 2>/dev/null; ` +
	`ls -1 /data/misc/keystore/ 2>/dev/null | grep -i sqlite; ` +
	`echo ---KS2---; ls -l /data/misc/keystore/persistent.sqlite 2>/dev/null'`

// DumpAndroidKeystore inventories the on-device keystore over adb (root
// required). Key material is TEE-wrapped and non-exportable, so this reports
// which keys exist (per app uid + alias + type), not the keys themselves.
func DumpAndroidKeystore(ctx context.Context, serial string, reveal bool) (*Result, error) {
	args := []string{}
	if serial != "" {
		args = append(args, "-s", serial)
	}
	args = append(args, "shell", androidKeystoreProbe)
	out, err := sysproc.CommandContext(ctx, "adb", args...).Output()
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("adb keystore probe (is the device rooted and authorized?): %w", err)
	}
	res := &Result{Platform: "android", Method: "keystore inventory (root)"}
	res.Items = parseAndroidKeystore(string(out))
	res.Notes = append(res.Notes, fmt.Sprintf("Inventoried %d keystore entr(ies).", len(res.Items)))
	if strings.Contains(string(out), "persistent.sqlite") {
		res.Notes = append(res.Notes, "A keystore2 database (persistent.sqlite) is present; newer keys are tracked there and are also non-exportable.")
	}
	res.Limitations = append(res.Limitations,
		"Key material is hardware-backed and non-exportable: this is an inventory of which keys exist, not the private/secret keys themselves.")
	return res, nil
}

// parseAndroidKeystore turns keystore blob filenames into inventory items.
// Legacy blob names look like "<uid>_USRPKEY_<alias>" (private key),
// "USRSKEY" (secret key), "USRCERT"/"CACERT" (certificate).
func parseAndroidKeystore(out string) []Item {
	var items []Item
	for _, raw := range strings.Split(out, "\n") {
		name := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if name == "" || name == "---KS2---" || strings.HasPrefix(name, "/") || strings.Contains(name, "persistent.sqlite") {
			continue
		}
		uid, typ, alias, ok := parseBlobName(name)
		if !ok {
			continue
		}
		items = append(items, Item{
			Source:     "keystore",
			Class:      keyTypeName(typ),
			Service:    uid,
			Account:    alias,
			Accessible: "hardware-backed (non-exportable)",
			Value:      "<key material not exportable>",
		})
	}
	return items
}

func parseBlobName(name string) (uid, typ, alias string, ok bool) {
	// e.g. "1000_USRPKEY_my_alias" -> uid=1000, typ=USRPKEY, alias=my_alias
	first := strings.IndexByte(name, '_')
	if first <= 0 {
		return "", "", "", false
	}
	uid = name[:first]
	if _, err := strconv.Atoi(uid); err != nil {
		return "", "", "", false // not a uid-prefixed blob (e.g. ".masterkey")
	}
	rest := name[first+1:]
	second := strings.IndexByte(rest, '_')
	if second < 0 {
		return uid, rest, "", true
	}
	return uid, rest[:second], rest[second+1:], true
}

func keyTypeName(typ string) string {
	switch strings.ToUpper(typ) {
	case "USRPKEY":
		return "Keystore private key"
	case "USRSKEY":
		return "Keystore secret key"
	case "USRCERT":
		return "Keystore certificate"
	case "CACERT":
		return "Keystore CA certificate"
	default:
		return "Keystore entry (" + typ + ")"
	}
}

// --- small net helpers (mirrors the Console iproxy setup) -------------------

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func waitPort(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
		if err == nil {
			c.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("port %d not ready", port)
}
