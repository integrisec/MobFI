package selfupdate

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

// TestLoadPubKeyFailsClosedOnEmptyKey pins the MFI-UPD-01 default: a build
// with no ldflags-injected pubKey refuses to install anything.
func TestLoadPubKeyFailsClosedOnEmptyKey(t *testing.T) {
	saved := pubKeyBase64
	defer func() { pubKeyBase64 = saved }()
	pubKeyBase64 = ""
	if _, err := loadPubKey(); err == nil {
		t.Fatal("loadPubKey with empty pubKeyBase64 must fail; got nil error")
	}
}

func TestLoadPubKeyRejectsMalformedBase64(t *testing.T) {
	saved := pubKeyBase64
	defer func() { pubKeyBase64 = saved }()
	pubKeyBase64 = "!!! not base64 !!!"
	if _, err := loadPubKey(); err == nil {
		t.Fatal("loadPubKey with garbage pubKeyBase64 must fail")
	}
}

func TestLoadPubKeyRejectsWrongSize(t *testing.T) {
	saved := pubKeyBase64
	defer func() { pubKeyBase64 = saved }()
	pubKeyBase64 = base64.StdEncoding.EncodeToString([]byte("short"))
	if _, err := loadPubKey(); err == nil {
		t.Fatal("loadPubKey with wrong-size pubKey must fail")
	}
}

func TestVerifyChecksumsAcceptsValidSignature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sums := []byte("aabbcc  mfi_v1.0.0_linux_amd64\nddeeff  SHA256SUMS.txt\n")
	sig := ed25519.Sign(priv, sums)

	if err := verifyChecksums(sums, sig, pub); err != nil {
		t.Fatalf("verifyChecksums rejected a valid signature: %v", err)
	}
}

func TestVerifyChecksumsRejectsTamperedContent(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sums := []byte("aabbcc  mfi_v1.0.0_linux_amd64\n")
	sig := ed25519.Sign(priv, sums)

	tampered := []byte("EVIL01  mfi_v1.0.0_linux_amd64\n")
	if err := verifyChecksums(tampered, sig, pub); err == nil {
		t.Fatal("verifyChecksums accepted a tampered checksums file")
	}
}

func TestVerifyChecksumsRejectsWrongKey(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sums := []byte("aabbcc  mfi_v1.0.0_linux_amd64\n")
	sig := ed25519.Sign(priv, sums)

	if err := verifyChecksums(sums, sig, other); err == nil {
		t.Fatal("verifyChecksums accepted a signature from the wrong key")
	}
}

func TestParseSignatureAcceptsRawAndBase64(t *testing.T) {
	// Raw 64 bytes.
	raw := make([]byte, ed25519.SignatureSize)
	for i := range raw {
		raw[i] = byte(i)
	}
	got, err := parseSignature(raw)
	if err != nil {
		t.Fatalf("parseSignature(raw) error: %v", err)
	}
	if len(got) != ed25519.SignatureSize {
		t.Fatalf("parseSignature(raw) len = %d, want %d", len(got), ed25519.SignatureSize)
	}
	// Base64 (with trailing newline, as a real signing tool would emit).
	b64 := []byte(base64.StdEncoding.EncodeToString(raw) + "\n")
	got, err = parseSignature(b64)
	if err != nil {
		t.Fatalf("parseSignature(base64) error: %v", err)
	}
	if len(got) != ed25519.SignatureSize {
		t.Fatalf("parseSignature(base64) len = %d, want %d", len(got), ed25519.SignatureSize)
	}
}

func TestParseSignatureRejectsGarbage(t *testing.T) {
	if _, err := parseSignature([]byte("not-a-signature")); err == nil {
		t.Fatal("parseSignature accepted garbage")
	}
}

func TestChecksumForFindsAsset(t *testing.T) {
	body := []byte("aabbcc  mfi_v1.0.0_linux_amd64\nddeeff  mfi_v1.0.0_darwin_arm64\n")
	got, err := checksumFor(body, "mfi_v1.0.0_darwin_arm64")
	if err != nil || got != "ddeeff" {
		t.Errorf("checksumFor = %q, %v; want ddeeff, nil", got, err)
	}
	if _, err := checksumFor(body, "unknown"); err == nil {
		t.Error("checksumFor should error on unknown asset")
	}
}

// TestApplyBinaryRefusesUnsignedRelease pins the top-level contract that
// applyBinary aborts when the release provides no signature URL.
func TestApplyBinaryRefusesUnsignedRelease(t *testing.T) {
	saved := pubKeyBase64
	defer func() { pubKeyBase64 = saved }()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubKeyBase64 = base64.StdEncoding.EncodeToString(pub)

	info := &Info{
		Latest:       "1.0.0",
		AssetName:    "mfi_v1.0.0_linux_amd64",
		AssetURL:     "https://example.invalid/asset",
		ChecksumsURL: "https://example.invalid/SHA256SUMS.txt",
		SignatureURL: "", // no signature published
	}
	_, err = applyBinary(t.Context(), info, nil)
	if err == nil {
		t.Fatal("applyBinary must refuse a release with no SignatureURL")
	}
	if !strings.Contains(err.Error(), "signature") && !strings.Contains(err.Error(), "SHA256SUMS.sig") {
		t.Errorf("expected signature-related error, got: %v", err)
	}
}
