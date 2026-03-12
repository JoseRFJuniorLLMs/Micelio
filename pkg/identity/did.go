// Package identity implements the L1 Identity Layer of the AIP protocol.
// Each agent is identified by a DID:key derived from an Ed25519 public key.
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/libp2p/go-libp2p/core/crypto"
)

// Identity represents an agent's cryptographic identity (DID:key from Ed25519).
type Identity struct {
	PrivKey crypto.PrivKey
	PubKey  crypto.PubKey
	DID     string
}

// Generate creates a new random Ed25519 identity.
func Generate() (*Identity, error) {
	priv, pub, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 key: %w", err)
	}
	did, err := pubKeyToDID(pub)
	if err != nil {
		return nil, err
	}
	return &Identity{PrivKey: priv, PubKey: pub, DID: did}, nil
}

// FromPrivateKey reconstructs an identity from a raw Ed25519 private key seed (32 bytes).
func FromPrivateKey(seed []byte) (*Identity, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("invalid seed size: got %d, want %d", len(seed), ed25519.SeedSize)
	}
	stdKey := ed25519.NewKeyFromSeed(seed)
	priv, err := crypto.UnmarshalEd25519PrivateKey(stdKey)
	if err != nil {
		return nil, fmt.Errorf("unmarshal private key: %w", err)
	}
	pub := priv.GetPublic()
	did, err := pubKeyToDID(pub)
	if err != nil {
		return nil, err
	}
	return &Identity{PrivKey: priv, PubKey: pub, DID: did}, nil
}

// Sign signs data with this identity's private key.
func (id *Identity) Sign(data []byte) ([]byte, error) {
	sig, err := id.PrivKey.Sign(data)
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}
	return sig, nil
}

// Verify checks a signature against this identity's public key.
func (id *Identity) Verify(data, sig []byte) (bool, error) {
	return id.PubKey.Verify(data, sig)
}

// VerifyFrom verifies a signature from a given DID's public key.
func VerifyFrom(did string, data, sig []byte) (bool, error) {
	pub, err := DIDToPubKey(did)
	if err != nil {
		return false, err
	}
	return pub.Verify(data, sig)
}

// pubKeyToDID derives a did:key from an Ed25519 public key.
// Format: did:key:z<base58btc-multicodec-pubkey>
func pubKeyToDID(pub crypto.PubKey) (string, error) {
	raw, err := pub.Raw()
	if err != nil {
		return "", fmt.Errorf("extract raw public key: %w", err)
	}
	// Multicodec prefix for Ed25519 public key: 0xed01
	multicodec := append([]byte{0xed, 0x01}, raw...)
	encoded := base58Encode(multicodec)
	return "did:key:z" + encoded, nil
}

// DIDToPubKey extracts the Ed25519 public key from a did:key string.
func DIDToPubKey(did string) (crypto.PubKey, error) {
	if len(did) < 9 || did[:9] != "did:key:z" {
		return nil, fmt.Errorf("invalid did:key format: %s", did)
	}
	decoded, err := base58Decode(did[9:])
	if err != nil {
		return nil, fmt.Errorf("base58 decode: %w", err)
	}
	if len(decoded) < 2 || decoded[0] != 0xed || decoded[1] != 0x01 {
		return nil, fmt.Errorf("invalid multicodec prefix for Ed25519")
	}
	raw := decoded[2:]
	return crypto.UnmarshalEd25519PublicKey(raw)
}

// persistedIdentity is the JSON format for saving/loading identities.
type persistedIdentity struct {
	PrivateKeySeed string `json:"private_key_seed"`
	DID            string `json:"did"`
}

// Save persists the identity to a JSON file.
func (id *Identity) Save(path string) error {
	raw, err := id.PrivKey.Raw()
	if err != nil {
		return fmt.Errorf("extract raw private key: %w", err)
	}
	// Ed25519 raw private key is 64 bytes (seed + public), we only need the seed (first 32)
	seed := raw[:ed25519.SeedSize]
	p := persistedIdentity{
		PrivateKeySeed: base64.StdEncoding.EncodeToString(seed),
		DID:            id.DID,
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// Load reads an identity from a JSON file.
func Load(path string) (*Identity, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p persistedIdentity
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	seed, err := base64.StdEncoding.DecodeString(p.PrivateKeySeed)
	if err != nil {
		return nil, fmt.Errorf("decode seed: %w", err)
	}
	return FromPrivateKey(seed)
}

// base58Encode encodes bytes to base58btc (Bitcoin alphabet).
func base58Encode(data []byte) string {
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	if len(data) == 0 {
		return ""
	}

	// Count leading zeros
	zeros := 0
	for _, b := range data {
		if b != 0 {
			break
		}
		zeros++
	}

	// Convert to base58
	size := len(data)*138/100 + 1
	buf := make([]byte, size)
	high := size - 1
	for _, b := range data {
		carry := int(b)
		j := size - 1
		for ; j > high || carry != 0; j-- {
			carry += 256 * int(buf[j])
			buf[j] = byte(carry % 58)
			carry /= 58
		}
		high = j
	}

	// Skip leading zeros in base58 result
	j := 0
	for j < size && buf[j] == 0 {
		j++
	}

	// Build result
	result := make([]byte, zeros+size-j)
	for i := 0; i < zeros; i++ {
		result[i] = '1'
	}
	for i := zeros; j < size; i, j = i+1, j+1 {
		result[i] = alphabet[buf[j]]
	}
	return string(result)
}

// base58Decode decodes a base58btc string to bytes.
func base58Decode(s string) ([]byte, error) {
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	if len(s) == 0 {
		return nil, nil
	}

	// Build index
	var index [128]int
	for i := range index {
		index[i] = -1
	}
	for i, c := range alphabet {
		index[c] = i
	}

	// Count leading '1's
	zeros := 0
	for _, c := range s {
		if c != '1' {
			break
		}
		zeros++
	}

	// Decode
	size := len(s)*733/1000 + 1
	buf := make([]byte, size)
	for _, c := range s {
		if c >= 128 || index[c] == -1 {
			return nil, fmt.Errorf("invalid base58 character: %c", c)
		}
		carry := index[c]
		for j := size - 1; j >= 0; j-- {
			carry += 58 * int(buf[j])
			buf[j] = byte(carry % 256)
			carry /= 256
		}
	}

	// Skip leading zeros in result
	j := 0
	for j < size && buf[j] == 0 {
		j++
	}

	result := make([]byte, zeros+size-j)
	copy(result[zeros:], buf[j:])
	return result, nil
}
