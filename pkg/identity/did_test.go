package identity

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerate(t *testing.T) {
	id, err := Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	// DID must start with did:key:z
	if !strings.HasPrefix(id.DID, "did:key:z") {
		t.Errorf("DID format invalid: got %q, want prefix 'did:key:z'", id.DID)
	}

	// Keys must not be nil
	if id.PrivKey == nil {
		t.Error("PrivKey is nil")
	}
	if id.PubKey == nil {
		t.Error("PubKey is nil")
	}

	// Raw public key must be 32 bytes (Ed25519)
	raw, err := id.PubKey.Raw()
	if err != nil {
		t.Fatalf("PubKey.Raw() error: %v", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		t.Errorf("public key size: got %d, want %d", len(raw), ed25519.PublicKeySize)
	}

	// Two generated identities must differ
	id2, err := Generate()
	if err != nil {
		t.Fatalf("Generate() second call error: %v", err)
	}
	if id.DID == id2.DID {
		t.Error("two generated identities have the same DID")
	}
}

func TestFromPrivateKey(t *testing.T) {
	// Generate an identity, extract seed, reconstruct, compare DID
	id1, err := Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	raw, err := id1.PrivKey.Raw()
	if err != nil {
		t.Fatalf("PrivKey.Raw() error: %v", err)
	}
	seed := raw[:ed25519.SeedSize]

	id2, err := FromPrivateKey(seed)
	if err != nil {
		t.Fatalf("FromPrivateKey() error: %v", err)
	}

	if id1.DID != id2.DID {
		t.Errorf("round-trip DID mismatch: %q != %q", id1.DID, id2.DID)
	}

	// Public keys must match
	raw1, _ := id1.PubKey.Raw()
	raw2, _ := id2.PubKey.Raw()
	if string(raw1) != string(raw2) {
		t.Error("round-trip public keys differ")
	}
}

func TestSignVerify(t *testing.T) {
	id, err := Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	data := []byte("hello micelio protocol")
	sig, err := id.Sign(data)
	if err != nil {
		t.Fatalf("Sign() error: %v", err)
	}

	if len(sig) == 0 {
		t.Fatal("signature is empty")
	}

	ok, err := id.Verify(data, sig)
	if err != nil {
		t.Fatalf("Verify() error: %v", err)
	}
	if !ok {
		t.Error("Verify() returned false for valid signature")
	}

	// Tampered data must not verify
	tampered := []byte("hello micelio tampered")
	ok, err = id.Verify(tampered, sig)
	if err != nil {
		t.Fatalf("Verify() with tampered data error: %v", err)
	}
	if ok {
		t.Error("Verify() returned true for tampered data")
	}
}

func TestVerifyFrom(t *testing.T) {
	id, err := Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	data := []byte("verify from DID string")
	sig, err := id.Sign(data)
	if err != nil {
		t.Fatalf("Sign() error: %v", err)
	}

	ok, err := VerifyFrom(id.DID, data, sig)
	if err != nil {
		t.Fatalf("VerifyFrom() error: %v", err)
	}
	if !ok {
		t.Error("VerifyFrom() returned false for valid signature")
	}

	// Different identity must not verify
	id2, _ := Generate()
	ok, err = VerifyFrom(id2.DID, data, sig)
	if err != nil {
		t.Fatalf("VerifyFrom() with wrong DID error: %v", err)
	}
	if ok {
		t.Error("VerifyFrom() returned true for wrong DID")
	}
}

func TestDIDToPubKey(t *testing.T) {
	// Valid DID
	id, err := Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	pub, err := DIDToPubKey(id.DID)
	if err != nil {
		t.Fatalf("DIDToPubKey() error: %v", err)
	}

	raw1, _ := id.PubKey.Raw()
	raw2, _ := pub.Raw()
	if string(raw1) != string(raw2) {
		t.Error("DIDToPubKey returned different public key")
	}

	// Invalid DIDs
	invalidDIDs := []string{
		"",
		"did:key:",
		"did:key:a",       // missing 'z' prefix
		"not-a-did",
		"did:web:example", // wrong method
	}

	for _, d := range invalidDIDs {
		_, err := DIDToPubKey(d)
		if err == nil {
			t.Errorf("DIDToPubKey(%q) expected error, got nil", d)
		}
	}
}

func TestSaveLoad(t *testing.T) {
	id1, err := Generate()
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "identity.json")

	if err := id1.Save(path); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// File must exist
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("saved file does not exist: %v", err)
	}

	id2, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if id1.DID != id2.DID {
		t.Errorf("Save/Load DID mismatch: %q != %q", id1.DID, id2.DID)
	}

	// Sign with original, verify with loaded
	data := []byte("persistence test")
	sig, _ := id1.Sign(data)
	ok, err := id2.Verify(data, sig)
	if err != nil {
		t.Fatalf("cross-verify error: %v", err)
	}
	if !ok {
		t.Error("cross-verify failed after Save/Load")
	}
}

func TestBase58RoundTrip(t *testing.T) {
	testCases := [][]byte{
		{},
		{0},
		{0, 0, 0},
		{1, 2, 3, 4, 5},
		{0xed, 0x01, 0xff, 0xaa, 0x55},
		make([]byte, 32), // all zeros
	}

	for i, input := range testCases {
		encoded := base58Encode(input)
		decoded, err := base58Decode(encoded)
		if err != nil {
			t.Errorf("case %d: base58Decode error: %v", i, err)
			continue
		}

		// Handle nil vs empty slice for empty input
		if len(input) == 0 && len(decoded) == 0 {
			continue
		}

		if string(decoded) != string(input) {
			t.Errorf("case %d: round-trip mismatch: got %v, want %v", i, decoded, input)
		}
	}
}

func TestInvalidSeedSize(t *testing.T) {
	badSeeds := [][]byte{
		nil,
		{},
		make([]byte, 16),
		make([]byte, 31),
		make([]byte, 33),
		make([]byte, 64),
	}

	for i, seed := range badSeeds {
		_, err := FromPrivateKey(seed)
		if err == nil {
			t.Errorf("case %d: FromPrivateKey(len=%d) expected error, got nil", i, len(seed))
		}
	}
}
