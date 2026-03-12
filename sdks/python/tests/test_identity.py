"""Tests for micelio.identity module."""

import os
import tempfile

import pytest

from micelio.identity import Identity, _base58_encode, _base58_decode


class TestBase58:
    """Test base58btc encoding/decoding (Bitcoin alphabet)."""

    def test_empty(self):
        assert _base58_encode(b"") == ""
        assert _base58_decode("") == b""

    def test_single_byte(self):
        encoded = _base58_encode(b"\x00")
        assert encoded == "1"
        assert _base58_decode(encoded) == b"\x00"

    def test_leading_zeros(self):
        encoded = _base58_encode(b"\x00\x00\x01")
        assert encoded.startswith("11")
        decoded = _base58_decode(encoded)
        assert decoded == b"\x00\x00\x01"

    def test_known_vector(self):
        # "Hello World!" in base58btc
        data = b"Hello World!"
        encoded = _base58_encode(data)
        decoded = _base58_decode(encoded)
        assert decoded == data

    def test_roundtrip_random(self):
        data = os.urandom(32)
        encoded = _base58_encode(data)
        decoded = _base58_decode(encoded)
        assert decoded == data

    def test_invalid_character(self):
        with pytest.raises(ValueError, match="Invalid base58 character"):
            _base58_decode("0OIl")  # These chars are not in base58btc


class TestIdentity:
    """Test Ed25519 identity generation, signing, and DID derivation."""

    def test_generate(self):
        id1 = Identity.generate()
        id2 = Identity.generate()
        assert id1.did != id2.did
        assert id1.did.startswith("did:key:z")
        assert id2.did.startswith("did:key:z")

    def test_from_seed_deterministic(self):
        seed = os.urandom(32)
        id1 = Identity.from_seed(seed)
        id2 = Identity.from_seed(seed)
        assert id1.did == id2.did

    def test_from_seed_invalid_length(self):
        with pytest.raises(ValueError, match="Invalid seed size"):
            Identity.from_seed(b"too short")

    def test_sign_verify(self):
        ident = Identity.generate()
        data = b"test message"
        sig = ident.sign(data)
        assert ident.verify(data, sig)

    def test_verify_wrong_data(self):
        ident = Identity.generate()
        sig = ident.sign(b"correct data")
        assert not ident.verify(b"wrong data", sig)

    def test_verify_wrong_key(self):
        id1 = Identity.generate()
        id2 = Identity.generate()
        sig = id1.sign(b"test")
        assert not id2.verify(b"test", sig)

    def test_verify_from_did(self):
        ident = Identity.generate()
        data = b"verify from DID"
        sig = ident.sign(data)
        assert Identity.verify_from(ident.did, data, sig)

    def test_verify_from_did_wrong_sig(self):
        ident = Identity.generate()
        assert not Identity.verify_from(ident.did, b"data", b"bad_sig")

    def test_did_format(self):
        ident = Identity.generate()
        assert ident.did.startswith("did:key:z")
        # Multicodec Ed25519 prefix is 0xed01, base58btc encoded
        decoded = _base58_decode(ident.did[9:])
        assert decoded[0] == 0xED
        assert decoded[1] == 0x01
        assert len(decoded) == 34  # 2 prefix + 32 pubkey

    def test_save_load(self):
        ident = Identity.generate()
        with tempfile.NamedTemporaryFile(
            mode="w", suffix=".json", delete=False
        ) as f:
            path = f.name

        try:
            ident.save(path)
            loaded = Identity.load(path)
            assert loaded.did == ident.did

            # Verify signing still works after reload
            data = b"after reload"
            sig = loaded.sign(data)
            assert ident.verify(data, sig)
        finally:
            os.unlink(path)

    def test_save_load_cross_verify(self):
        """Verify that saved/loaded identity produces same signatures."""
        ident = Identity.generate()
        data = b"cross verify test"
        sig_before = ident.sign(data)

        with tempfile.NamedTemporaryFile(
            mode="w", suffix=".json", delete=False
        ) as f:
            path = f.name

        try:
            ident.save(path)
            loaded = Identity.load(path)
            # Original sig should verify with loaded key
            assert loaded.verify(data, sig_before)
        finally:
            os.unlink(path)

    def test_did_to_pubkey_invalid_format(self):
        with pytest.raises(ValueError, match="Invalid did:key format"):
            Identity._did_to_pubkey("not-a-did")

    def test_did_to_pubkey_invalid_prefix(self):
        # Valid base58 but wrong multicodec prefix
        data = bytes([0x00, 0x00]) + os.urandom(32)
        fake_did = f"did:key:z{_base58_encode(data)}"
        with pytest.raises(ValueError, match="Invalid multicodec prefix"):
            Identity._did_to_pubkey(fake_did)

    def test_repr(self):
        ident = Identity.generate()
        r = repr(ident)
        assert "Identity" in r
        assert ident.did in r
