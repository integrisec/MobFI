package keystore

import "testing"

func TestParseKeychainDumper(t *testing.T) {
	const sample = `Generic Password
-----------------
Service: com.apple.account
Account: alice@example.com
Entitlement Group: ABCDE12345.com.example
Label: Account token
Keychain Data: s3cr3t-token

Internet Password
-----------------
Server: example.com
Account: bob
Protocol: htps
Keychain Data: hunter2
`
	items := parseKeychainDumper(sample, true)
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	g := items[0]
	if g.Class != "Generic Password" || g.Service != "com.apple.account" || g.Account != "alice@example.com" {
		t.Errorf("generic item parsed wrong: %+v", g)
	}
	if g.Value != "s3cr3t-token" {
		t.Errorf("generic value = %q, want revealed secret", g.Value)
	}
	i := items[1]
	if i.Class != "Internet Password" || i.Service != "example.com" || i.Value != "hunter2" {
		t.Errorf("internet item parsed wrong: %+v", i)
	}
	if i.Extra["protocol"] != "htps" {
		t.Errorf("internet protocol extra = %q", i.Extra["protocol"])
	}
}

func TestParseKeychainDumperRedacted(t *testing.T) {
	const sample = "Generic Password\nService: x\nKeychain Data: topsecret\n"
	items := parseKeychainDumper(sample, false)
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Value == "topsecret" {
		t.Errorf("value should be redacted by default, got %q", items[0].Value)
	}
}

func TestParseAndroidKeystore(t *testing.T) {
	const sample = `1000_USRPKEY_platform_key
1000_USRCERT_platform_key
10142_USRSKEY_com.example.app.masterkey
.masterkey
---KS2---
`
	items := parseAndroidKeystore(sample)
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3 (.masterkey and marker skipped)", len(items))
	}
	if items[0].Service != "1000" || items[0].Account != "platform_key" || items[0].Class != "Keystore private key" {
		t.Errorf("blob 0 parsed wrong: %+v", items[0])
	}
	if items[2].Service != "10142" || items[2].Class != "Keystore secret key" {
		t.Errorf("blob 2 parsed wrong: %+v", items[2])
	}
	for _, it := range items {
		if it.Value != "<key material not exportable>" {
			t.Errorf("keystore value should note non-exportability, got %q", it.Value)
		}
	}
}
