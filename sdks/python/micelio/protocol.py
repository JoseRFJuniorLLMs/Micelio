"""AIP protocol message types, envelope, and serialization.

Wire-compatible with the Go protocol package: same JSON field names,
same MessageType strings, same ULID generation, same TTL semantics.
"""

import json
from dataclasses import dataclass, field, asdict
from datetime import datetime, timezone, timedelta
from enum import Enum
from typing import Any, Dict, List, Optional

from ulid import ULID


# AIP version string (must match Go)
VERSION = "aip/0.1"

# Maximum clock skew allowed for TTL checks (same as Go: 30 seconds)
MAX_CLOCK_SKEW = timedelta(seconds=30)


class MessageType(str, Enum):
    """AIP message types per the specification."""

    # Core negotiation flow
    INTENT = "INTENT"
    PROPOSE = "PROPOSE"
    COUNTER = "COUNTER"
    ACCEPT = "ACCEPT"
    REJECT = "REJECT"
    DELIVER = "DELIVER"
    RECEIPT = "RECEIPT"
    CANCEL = "CANCEL"

    # Discovery
    DISCOVER = "DISCOVER"
    CAPABILITY_ADVERTISE = "CAPABILITY_ADVERTISE"
    CAPABILITY_QUERY = "CAPABILITY_QUERY"

    # System
    PING = "PING"
    PONG = "PONG"
    ERROR = "ERROR"


@dataclass
class Budget:
    """Cost/budget constraint."""

    amount: float
    currency: str

    def to_dict(self) -> Dict[str, Any]:
        return {"amount": self.amount, "currency": self.currency}

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "Budget":
        return cls(amount=d["amount"], currency=d["currency"])


@dataclass
class IntentPayload:
    """Payload for INTENT messages."""

    capability: str
    description: str
    params: Optional[Dict[str, Any]] = None
    budget: Optional[Budget] = None
    deadline: Optional[datetime] = None

    def to_dict(self) -> Dict[str, Any]:
        d: Dict[str, Any] = {
            "capability": self.capability,
            "description": self.description,
        }
        if self.params is not None:
            d["params"] = self.params
        if self.budget is not None:
            d["budget"] = self.budget.to_dict()
        if self.deadline is not None:
            d["deadline"] = self.deadline.isoformat()
        return d


@dataclass
class ProposePayload:
    """Payload for PROPOSE messages."""

    capability: str
    approach: str
    cost: Optional[Budget] = None
    eta: Optional[float] = None  # seconds (Go uses time.Duration which is nanoseconds in JSON)
    conditions: Optional[List[str]] = None

    def to_dict(self) -> Dict[str, Any]:
        d: Dict[str, Any] = {
            "capability": self.capability,
            "approach": self.approach,
        }
        if self.cost is not None:
            d["cost"] = self.cost.to_dict()
        if self.eta is not None:
            # Go encodes time.Duration as nanoseconds in JSON
            d["eta"] = int(self.eta * 1_000_000_000)
        if self.conditions is not None:
            d["conditions"] = self.conditions
        return d


@dataclass
class CounterPayload:
    """Payload for COUNTER messages (modified proposal)."""

    capability: str
    approach: str
    reason: str
    cost: Optional[Budget] = None
    eta: Optional[float] = None  # seconds
    conditions: Optional[List[str]] = None

    def to_dict(self) -> Dict[str, Any]:
        d: Dict[str, Any] = {
            "capability": self.capability,
            "approach": self.approach,
            "reason": self.reason,
        }
        if self.cost is not None:
            d["cost"] = self.cost.to_dict()
        if self.eta is not None:
            d["eta"] = int(self.eta * 1_000_000_000)
        if self.conditions is not None:
            d["conditions"] = self.conditions
        return d


@dataclass
class DeliverPayload:
    """Payload for DELIVER messages."""

    result: Any
    metadata: Optional[Dict[str, Any]] = None

    def to_dict(self) -> Dict[str, Any]:
        d: Dict[str, Any] = {"result": self.result}
        if self.metadata is not None:
            d["metadata"] = self.metadata
        return d


@dataclass
class ReceiptPayload:
    """Payload for RECEIPT messages."""

    accepted: bool
    rating: Optional[int] = None  # 1-5
    feedback: Optional[str] = None

    def to_dict(self) -> Dict[str, Any]:
        d: Dict[str, Any] = {"accepted": self.accepted}
        if self.rating is not None:
            d["rating"] = self.rating
        if self.feedback is not None:
            d["feedback"] = self.feedback
        return d


@dataclass
class RejectPayload:
    """Payload for REJECT messages."""

    reason: str
    code: Optional[str] = None

    def to_dict(self) -> Dict[str, Any]:
        d: Dict[str, Any] = {"reason": self.reason}
        if self.code is not None:
            d["code"] = self.code
        return d


@dataclass
class CancelPayload:
    """Payload for CANCEL messages."""

    reason: str

    def to_dict(self) -> Dict[str, Any]:
        return {"reason": self.reason}


@dataclass
class ErrorPayload:
    """Payload for ERROR messages."""

    code: str
    message: str
    details: Optional[Dict[str, Any]] = None

    def to_dict(self) -> Dict[str, Any]:
        d: Dict[str, Any] = {"code": self.code, "message": self.message}
        if self.details is not None:
            d["details"] = self.details
        return d


@dataclass
class PingPayload:
    """Payload for PING/PONG messages."""

    nonce: str
    timestamp: datetime

    def to_dict(self) -> Dict[str, Any]:
        return {
            "nonce": self.nonce,
            "timestamp": self.timestamp.isoformat(),
        }


@dataclass
class Message:
    """AIP v0.1 message envelope.

    All fields match the Go Message struct JSON tags exactly.
    """

    version: str
    msg_id: str
    type: MessageType
    from_did: str  # "from" in JSON
    to_did: str  # "to" in JSON
    conversation_id: str
    timestamp: datetime
    ttl: int
    payload: Optional[Dict[str, Any]] = None
    signature: Optional[str] = None

    def encode(self) -> bytes:
        """Serialize to JSON bytes (wire-compatible with Go)."""
        return json.dumps(self._to_wire_dict(), separators=(",", ":")).encode("utf-8")

    def encode_pretty(self) -> bytes:
        """Serialize to pretty JSON bytes."""
        return json.dumps(self._to_wire_dict(), indent=2).encode("utf-8")

    def _to_wire_dict(self) -> Dict[str, Any]:
        """Convert to dict with Go-compatible field names."""
        d: Dict[str, Any] = {
            "version": self.version,
            "msg_id": self.msg_id,
            "type": self.type.value,
            "from": self.from_did,
            "to": self.to_did,
            "conversation_id": self.conversation_id,
            "timestamp": self.timestamp.strftime("%Y-%m-%dT%H:%M:%S.%f")[:-3] + "Z"
            if self.timestamp.tzinfo is None
            else self.timestamp.isoformat(),
            "ttl": self.ttl,
        }
        if self.payload is not None:
            d["payload"] = self.payload
        if self.signature:
            d["signature"] = self.signature
        return d

    def signable_bytes(self) -> bytes:
        """Return canonical bytes to sign (message without signature field).

        Matches the Go SignableBytes method.
        """
        cp = Message(
            version=self.version,
            msg_id=self.msg_id,
            type=self.type,
            from_did=self.from_did,
            to_did=self.to_did,
            conversation_id=self.conversation_id,
            timestamp=self.timestamp,
            ttl=self.ttl,
            payload=self.payload,
            signature=None,
        )
        return cp.encode()

    def validate_ttl(self) -> Optional[str]:
        """Check whether the message has expired based on TTL and timestamp.

        Returns None if valid, or an error string if invalid.
        Allows up to MAX_CLOCK_SKEW for messages slightly in the future.
        """
        now = datetime.now(timezone.utc)
        ts = self.timestamp
        if ts.tzinfo is None:
            ts = ts.replace(tzinfo=timezone.utc)
        age = now - ts

        if age < -MAX_CLOCK_SKEW:
            return f"message timestamp is too far in the future ({-age} ahead)"

        if self.ttl > 0 and age > timedelta(seconds=self.ttl):
            return f"message expired: age {age} exceeds TTL {self.ttl}s"

        return None

    def is_expired(self) -> bool:
        """Return True if the message TTL has been exceeded."""
        if self.ttl <= 0:
            return False
        now = datetime.now(timezone.utc)
        ts = self.timestamp
        if ts.tzinfo is None:
            ts = ts.replace(tzinfo=timezone.utc)
        return (now - ts) > timedelta(seconds=self.ttl)


def new_message(
    msg_type: MessageType,
    from_did: str,
    to_did: str,
    conversation_id: str,
    payload: Any = None,
    ttl: int = 60,
) -> Message:
    """Create a new AIP message with auto-generated ULID and UTC timestamp.

    If payload has a to_dict() method it will be called, otherwise it
    is used as-is (must be JSON-serializable).
    """
    msg_id = str(ULID())

    payload_dict = None
    if payload is not None:
        if hasattr(payload, "to_dict"):
            payload_dict = payload.to_dict()
        elif isinstance(payload, dict):
            payload_dict = payload
        else:
            payload_dict = payload

    return Message(
        version=VERSION,
        msg_id=msg_id,
        type=msg_type,
        from_did=from_did,
        to_did=to_did,
        conversation_id=conversation_id,
        timestamp=datetime.now(timezone.utc),
        ttl=ttl,
        payload=payload_dict,
    )


def new_conversation_id() -> str:
    """Generate a new ULID for a conversation."""
    return str(ULID())


def decode_message(data: bytes) -> Message:
    """Deserialize a message from JSON bytes.

    Wire-compatible with Go's DecodeMessage.
    """
    d = json.loads(data)

    # Parse timestamp
    ts_str = d["timestamp"]
    # Handle Go's time.Time format (RFC3339 with optional fractional seconds)
    if ts_str.endswith("Z"):
        ts_str = ts_str[:-1] + "+00:00"
    timestamp = datetime.fromisoformat(ts_str)
    if timestamp.tzinfo is None:
        timestamp = timestamp.replace(tzinfo=timezone.utc)

    return Message(
        version=d["version"],
        msg_id=d["msg_id"],
        type=MessageType(d["type"]),
        from_did=d["from"],
        to_did=d["to"],
        conversation_id=d["conversation_id"],
        timestamp=timestamp,
        ttl=d["ttl"],
        payload=d.get("payload"),
        signature=d.get("signature"),
    )
