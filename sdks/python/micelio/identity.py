"""DID:key identity from Ed25519 keypairs.

Wire-compatible with the Go implementation: same base58btc encoding,
same multicodec prefix (0xed01), same JSON persistence format.
"""

import base64
import json
import os
from typing import Optional

from cryptography.hazmat.primitives.asymmetric.ed25519 import (
    Ed25519PrivateKey,
    Ed25519PublicKey,
)


# Bitcoin base58 alphabet (same as Go implementation)
_BASE58_ALPHABET = b"123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
_BASE58_INDEX = {c: i for i, c in enumerate(_BASE58_ALPHABET)}


def _base58_encode(data: bytes) -> str:
    """Encode bytes to base58btc (Bitcoin alphabet).

    Matches the Go base58Encode implementation exactly.
    """
    if not data:
        return ""

    # Count leading zeros
    zeros = 0
    for b in data:
        if b != 0:
            break
        zeros += 1

    # Convert to base58
    size = len(data) * 138 // 100 + 1
    buf = [0] * size
    high = size - 1

    for b in data:
        carry = b
        j = size - 1
        while j > high or carry != 0:
            carry += 256 * buf[j]
            buf[j] = carry % 58
            carry //= 58
            j -= 1
        high = j

    # Skip leading zeros in base58 result
    j = 0
    while j < size and buf[j] == 0:
        j += 1

    # Build result
    result = bytearray()
    for _ in range(zeros):
        result.append(ord("1"))
    while j < size:
        result.append(_BASE58_ALPHABET[buf[j]])
        j += 1

    return result.decode("ascii")


def _base58_decode(s: str) -> bytes:
    """Decode a base58btc string to bytes.

    Matches the Go base58Decode implementation exactly.
    """
    if not s:
        return b""

    # Count leading '1's
    zeros = 0
    for c in s:
        if c != "1":
            break
        zeros += 1

    # Decode
    size = len(s) * 733 // 1000 + 1
    buf = [0] * size

    for c in s:
        b = c.encode("ascii")[0]
        if b not in _BASE58_INDEX:
            raise ValueError(f"Invalid base58 character: {c}")
        carry = _BASE58_INDEX[b]
        for j in range(size - 1, -1, -1):
            carry += 58 * buf[j]
            buf[j] = carry % 256
            carry //= 256

    # Skip leading zeros in result
    j = 0
    while j < size and buf[j] == 0:
        j += 1

    result = bytearray(zeros)
    result.extend(buf[j:])
    return bytes(result)


class Identity:
    """Cryptographic identity based on Ed25519, identified by a DID:key.

    Wire-compatible with the Go Identity type.
    """

    def __init__(self, private_key: Ed25519PrivateKey):
        self._private_key = private_key
        self._public_key = private_key.public_key()
        self._did = self._derive_did()

    @property
    def private_key(self) -> Ed25519PrivateKey:
        return self._private_key

    @property
    def public_key(self) -> Ed25519PublicKey:
        return self._public_key

    @property
    def did(self) -> str:
        return self._did

    @classmethod
    def generate(cls) -> "Identity":
        """Generate a new random Ed25519 identity."""
        return cls(Ed25519PrivateKey.generate())

    @classmethod
    def from_seed(cls, seed: bytes) -> "Identity":
        """Reconstruct from 32-byte seed."""
        if len(seed) != 32:
            raise ValueError(f"Invalid seed size: got {len(seed)}, want 32")
        return cls(Ed25519PrivateKey.from_private_bytes(seed))

    def sign(self, data: bytes) -> bytes:
        """Sign data with Ed25519."""
        return self._private_key.sign(data)

    def verify(self, data: bytes, signature: bytes) -> bool:
        """Verify a signature against this identity's public key."""
        try:
            self._public_key.verify(signature, data)
            return True
        except Exception:
            return False

    @staticmethod
    def verify_from(did: str, data: bytes, signature: bytes) -> bool:
        """Verify a signature from a DID string's public key."""
        pub_key = Identity._did_to_pubkey(did)
        try:
            pub_key.verify(signature, data)
            return True
        except Exception:
            return False

    def _derive_did(self) -> str:
        """Derive did:key from Ed25519 public key.

        Format: did:key:z<base58btc(multicodec_prefix + raw_pubkey)>
        Multicodec prefix for Ed25519: 0xed 0x01
        """
        raw = self._public_key.public_bytes_raw()
        multicodec = bytes([0xED, 0x01]) + raw
        encoded = _base58_encode(multicodec)
        return f"did:key:z{encoded}"

    @staticmethod
    def _did_to_pubkey(did: str) -> Ed25519PublicKey:
        """Extract Ed25519 public key from did:key string."""
        if not did.startswith("did:key:z"):
            raise ValueError(f"Invalid did:key format: {did}")
        decoded = _base58_decode(did[9:])
        if len(decoded) < 2 or decoded[0] != 0xED or decoded[1] != 0x01:
            raise ValueError("Invalid multicodec prefix for Ed25519")
        return Ed25519PublicKey.from_public_bytes(decoded[2:])

    def save(self, path: str) -> None:
        """Save identity to JSON file (compatible with Go format)."""
        raw = self._private_key.private_bytes_raw()
        data = {
            "private_key_seed": base64.b64encode(raw).decode("ascii"),
            "did": self._did,
        }
        with open(path, "w") as f:
            json.dump(data, f, indent=2)

    @classmethod
    def load(cls, path: str) -> "Identity":
        """Load identity from JSON file (compatible with Go format)."""
        with open(path) as f:
            data = json.load(f)
        seed = base64.b64decode(data["private_key_seed"])
        return cls.from_seed(seed)

    def __repr__(self) -> str:
        return f"Identity(did={self._did!r})"
