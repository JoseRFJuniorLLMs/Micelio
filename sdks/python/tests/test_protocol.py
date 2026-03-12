"""Tests for micelio.protocol module."""

import json
import time
from datetime import datetime, timezone, timedelta

import pytest

from micelio.protocol import (
    Message,
    MessageType,
    new_message,
    new_conversation_id,
    decode_message,
    IntentPayload,
    ProposePayload,
    DeliverPayload,
    ReceiptPayload,
    CounterPayload,
    RejectPayload,
    CancelPayload,
    ErrorPayload,
    PingPayload,
    Budget,
    VERSION,
    MAX_CLOCK_SKEW,
)


class TestMessageType:
    """Test MessageType enum values match Go constants."""

    def test_core_types(self):
        assert MessageType.INTENT.value == "INTENT"
        assert MessageType.PROPOSE.value == "PROPOSE"
        assert MessageType.COUNTER.value == "COUNTER"
        assert MessageType.ACCEPT.value == "ACCEPT"
        assert MessageType.REJECT.value == "REJECT"
        assert MessageType.DELIVER.value == "DELIVER"
        assert MessageType.RECEIPT.value == "RECEIPT"
        assert MessageType.CANCEL.value == "CANCEL"

    def test_discovery_types(self):
        assert MessageType.DISCOVER.value == "DISCOVER"
        assert MessageType.CAPABILITY_ADVERTISE.value == "CAPABILITY_ADVERTISE"
        assert MessageType.CAPABILITY_QUERY.value == "CAPABILITY_QUERY"

    def test_system_types(self):
        assert MessageType.PING.value == "PING"
        assert MessageType.PONG.value == "PONG"
        assert MessageType.ERROR.value == "ERROR"


class TestNewMessage:
    """Test message creation."""

    def test_basic_creation(self):
        msg = new_message(
            MessageType.INTENT,
            from_did="did:key:zAlice",
            to_did="did:key:zBob",
            conversation_id="conv-1",
        )
        assert msg.version == VERSION
        assert msg.type == MessageType.INTENT
        assert msg.from_did == "did:key:zAlice"
        assert msg.to_did == "did:key:zBob"
        assert msg.conversation_id == "conv-1"
        assert msg.ttl == 60
        assert msg.msg_id  # ULID generated
        assert msg.timestamp.tzinfo is not None

    def test_custom_ttl(self):
        msg = new_message(
            MessageType.PING, "a", "b", "c", ttl=120
        )
        assert msg.ttl == 120

    def test_with_payload_dataclass(self):
        payload = IntentPayload(
            capability="translate",
            description="Translate text",
            params={"lang": "pt"},
            budget=Budget(amount=1.0, currency="USD"),
        )
        msg = new_message(
            MessageType.INTENT, "a", "b", "c", payload=payload
        )
        assert msg.payload["capability"] == "translate"
        assert msg.payload["params"]["lang"] == "pt"
        assert msg.payload["budget"]["amount"] == 1.0

    def test_with_dict_payload(self):
        msg = new_message(
            MessageType.DELIVER, "a", "b", "c",
            payload={"result": {"data": 42}},
        )
        assert msg.payload["result"]["data"] == 42

    def test_unique_ids(self):
        ids = {new_message(MessageType.PING, "a", "b", "c").msg_id for _ in range(100)}
        assert len(ids) == 100

    def test_conversation_id_unique(self):
        ids = {new_conversation_id() for _ in range(100)}
        assert len(ids) == 100


class TestEncodeDecode:
    """Test message serialization round-trips."""

    def test_basic_roundtrip(self):
        msg = new_message(MessageType.INTENT, "did:a", "did:b", "conv-1")
        encoded = msg.encode()
        decoded = decode_message(encoded)
        assert decoded.msg_id == msg.msg_id
        assert decoded.type == MessageType.INTENT
        assert decoded.from_did == "did:a"
        assert decoded.to_did == "did:b"
        assert decoded.version == VERSION

    def test_payload_roundtrip(self):
        payload = IntentPayload(
            capability="test",
            description="Test capability",
            params={"key": "value"},
        )
        msg = new_message(
            MessageType.INTENT, "a", "b", "c", payload=payload
        )
        decoded = decode_message(msg.encode())
        assert decoded.payload["capability"] == "test"
        assert decoded.payload["params"]["key"] == "value"

    def test_signature_roundtrip(self):
        msg = new_message(MessageType.INTENT, "a", "b", "c")
        msg.signature = "deadbeef"
        decoded = decode_message(msg.encode())
        assert decoded.signature == "deadbeef"

    def test_no_signature_omitted(self):
        msg = new_message(MessageType.INTENT, "a", "b", "c")
        encoded = msg.encode()
        d = json.loads(encoded)
        assert "signature" not in d

    def test_wire_format_field_names(self):
        """Verify JSON field names match Go struct tags."""
        msg = new_message(MessageType.INTENT, "did:a", "did:b", "conv-1")
        d = json.loads(msg.encode())
        assert "version" in d
        assert "msg_id" in d
        assert "type" in d
        assert "from" in d  # Go uses `json:"from"`
        assert "to" in d
        assert "conversation_id" in d
        assert "timestamp" in d
        assert "ttl" in d

    def test_decode_go_format(self):
        """Decode a message in the exact format Go would produce."""
        go_msg = {
            "version": "aip/0.1",
            "msg_id": "01HQXYZ1234567890ABCDE",
            "type": "INTENT",
            "from": "did:key:zAlice",
            "to": "did:key:zBob",
            "conversation_id": "01HQXYZ0000000000CONV1",
            "timestamp": "2024-01-15T10:30:00.000Z",
            "ttl": 60,
            "payload": {"capability": "test", "description": "Test"},
        }
        data = json.dumps(go_msg).encode()
        msg = decode_message(data)
        assert msg.type == MessageType.INTENT
        assert msg.from_did == "did:key:zAlice"
        assert msg.to_did == "did:key:zBob"
        assert msg.payload["capability"] == "test"


class TestPayloads:
    """Test payload dataclass serialization."""

    def test_intent_payload(self):
        p = IntentPayload(
            capability="test",
            description="desc",
            params={"a": 1},
            budget=Budget(amount=5.0, currency="EUR"),
        )
        d = p.to_dict()
        assert d["capability"] == "test"
        assert d["params"]["a"] == 1
        assert d["budget"]["amount"] == 5.0

    def test_intent_payload_minimal(self):
        p = IntentPayload(capability="test", description="desc")
        d = p.to_dict()
        assert "params" not in d
        assert "budget" not in d
        assert "deadline" not in d

    def test_propose_payload(self):
        p = ProposePayload(
            capability="test",
            approach="method A",
            cost=Budget(amount=1.0, currency="USD"),
            eta=30.0,
            conditions=["condition1"],
        )
        d = p.to_dict()
        assert d["capability"] == "test"
        assert d["approach"] == "method A"
        # ETA is encoded as nanoseconds (Go time.Duration)
        assert d["eta"] == 30_000_000_000
        assert d["conditions"] == ["condition1"]

    def test_counter_payload(self):
        p = CounterPayload(
            capability="test",
            approach="method B",
            reason="too expensive",
        )
        d = p.to_dict()
        assert d["reason"] == "too expensive"

    def test_deliver_payload(self):
        p = DeliverPayload(result={"text": "hello"}, metadata={"tokens": 5})
        d = p.to_dict()
        assert d["result"]["text"] == "hello"
        assert d["metadata"]["tokens"] == 5

    def test_receipt_payload(self):
        p = ReceiptPayload(accepted=True, rating=5, feedback="great")
        d = p.to_dict()
        assert d["accepted"] is True
        assert d["rating"] == 5

    def test_reject_payload(self):
        p = RejectPayload(reason="no capacity", code="capacity_exceeded")
        d = p.to_dict()
        assert d["reason"] == "no capacity"
        assert d["code"] == "capacity_exceeded"

    def test_cancel_payload(self):
        p = CancelPayload(reason="user cancelled")
        d = p.to_dict()
        assert d["reason"] == "user cancelled"

    def test_error_payload(self):
        p = ErrorPayload(code="500", message="internal error", details={"trace": "..."})
        d = p.to_dict()
        assert d["code"] == "500"
        assert d["details"]["trace"] == "..."

    def test_ping_payload(self):
        now = datetime.now(timezone.utc)
        p = PingPayload(nonce="abc123", timestamp=now)
        d = p.to_dict()
        assert d["nonce"] == "abc123"


class TestTTL:
    """Test TTL validation."""

    def test_fresh_message_valid(self):
        msg = new_message(MessageType.PING, "a", "b", "c", ttl=60)
        assert msg.validate_ttl() is None
        assert not msg.is_expired()

    def test_zero_ttl_never_expires(self):
        msg = new_message(MessageType.PING, "a", "b", "c", ttl=0)
        assert not msg.is_expired()

    def test_expired_message(self):
        msg = new_message(MessageType.PING, "a", "b", "c", ttl=1)
        msg.timestamp = datetime.now(timezone.utc) - timedelta(seconds=10)
        error = msg.validate_ttl()
        assert error is not None
        assert "expired" in error
        assert msg.is_expired()

    def test_future_message_within_skew(self):
        msg = new_message(MessageType.PING, "a", "b", "c", ttl=60)
        msg.timestamp = datetime.now(timezone.utc) + timedelta(seconds=5)
        assert msg.validate_ttl() is None

    def test_future_message_beyond_skew(self):
        msg = new_message(MessageType.PING, "a", "b", "c", ttl=60)
        msg.timestamp = datetime.now(timezone.utc) + timedelta(seconds=60)
        error = msg.validate_ttl()
        assert error is not None
        assert "future" in error


class TestSignableBytes:
    """Test signable bytes generation."""

    def test_excludes_signature(self):
        msg = new_message(MessageType.INTENT, "a", "b", "c")
        msg.signature = "should_not_appear"
        signable = msg.signable_bytes()
        d = json.loads(signable)
        assert "signature" not in d

    def test_deterministic(self):
        msg = new_message(MessageType.INTENT, "a", "b", "c")
        assert msg.signable_bytes() == msg.signable_bytes()
