"""Negotiation FSM and conversation tracking.

Wire-compatible with the Go negotiation package: same states,
same transition rules, same counter-round limits.
"""

import threading
from dataclasses import dataclass, field
from datetime import datetime, timezone, timedelta
from enum import Enum
from typing import Dict, List, Optional

from micelio.protocol import Message, MessageType


# Default conversation timeout (same as Go: 5 minutes)
DEFAULT_CONVERSATION_TIMEOUT = timedelta(minutes=5)

# Default maximum counter rounds (same as Go: 10)
DEFAULT_MAX_COUNTER_ROUNDS = 10


class NegotiationState(str, Enum):
    """Negotiation state machine states (matches Go exactly)."""

    CREATED = "created"
    PROPOSED = "proposed"
    COUNTERED = "countered"
    ACCEPTED = "accepted"
    REJECTED = "rejected"
    DELIVERED = "delivered"
    COMPLETED = "completed"
    CANCELLED = "cancelled"
    TIMED_OUT = "timed_out"


# Valid state transitions (matches Go validateTransition)
_ALLOWED_TRANSITIONS: Dict[NegotiationState, List[MessageType]] = {
    NegotiationState.CREATED: [
        MessageType.INTENT,
        MessageType.PROPOSE,
        MessageType.REJECT,
    ],
    NegotiationState.PROPOSED: [
        MessageType.ACCEPT,
        MessageType.REJECT,
        MessageType.COUNTER,
        MessageType.CANCEL,
    ],
    NegotiationState.COUNTERED: [
        MessageType.ACCEPT,
        MessageType.REJECT,
        MessageType.COUNTER,
        MessageType.CANCEL,
    ],
    NegotiationState.ACCEPTED: [
        MessageType.DELIVER,
        MessageType.CANCEL,
    ],
    NegotiationState.DELIVERED: [
        MessageType.RECEIPT,
    ],
}

# Terminal states (no transitions allowed)
_TERMINAL_STATES = {
    NegotiationState.COMPLETED,
    NegotiationState.CANCELLED,
    NegotiationState.REJECTED,
    NegotiationState.TIMED_OUT,
}

# Message type to resulting state mapping
_STATE_MAP = {
    MessageType.PROPOSE: NegotiationState.PROPOSED,
    MessageType.COUNTER: NegotiationState.COUNTERED,
    MessageType.ACCEPT: NegotiationState.ACCEPTED,
    MessageType.REJECT: NegotiationState.REJECTED,
    MessageType.DELIVER: NegotiationState.DELIVERED,
    MessageType.RECEIPT: NegotiationState.COMPLETED,
    MessageType.CANCEL: NegotiationState.CANCELLED,
}


class Conversation:
    """Tracks the state of a negotiation between two agents.

    Thread-safe via internal lock. Matches Go Conversation exactly.
    """

    def __init__(
        self,
        id: str,
        initiator: str,
        timeout: timedelta = DEFAULT_CONVERSATION_TIMEOUT,
        max_counter_rounds: int = DEFAULT_MAX_COUNTER_ROUNDS,
    ):
        self.id = id
        self.state = NegotiationState.CREATED
        self.initiator = initiator
        self.responder: Optional[str] = None
        self.messages: List[Message] = []
        now = datetime.now(timezone.utc)
        self.created_at = now
        self.updated_at = now
        self.timeout = timeout
        self.max_counter_rounds = max_counter_rounds
        self._lock = threading.Lock()

    def is_timed_out(self) -> bool:
        """Return True if the conversation has exceeded its timeout."""
        with self._lock:
            if self.timeout.total_seconds() <= 0:
                return False
            return (datetime.now(timezone.utc) - self.updated_at) > self.timeout

    def _counter_count(self) -> int:
        """Count the number of COUNTER messages in the conversation."""
        return sum(1 for m in self.messages if m.type == MessageType.COUNTER)

    def transition(self, msg: Message) -> None:
        """Apply a message to the conversation, advancing the state machine.

        Raises ValueError on invalid transitions.
        """
        with self._lock:
            self._validate_transition(msg.type)

            self.messages.append(msg)
            self.updated_at = datetime.now(timezone.utc)

            if self.responder is None and msg.from_did != self.initiator:
                self.responder = msg.from_did

            # INTENT keeps state as CREATED
            if msg.type in _STATE_MAP:
                self.state = _STATE_MAP[msg.type]

    def _validate_transition(self, msg_type: MessageType) -> None:
        """Validate that a message type is allowed in the current state.

        Raises ValueError on invalid transition.
        """
        allowed = _ALLOWED_TRANSITIONS.get(self.state)
        if allowed is None:
            raise ValueError(
                f"conversation {self.id} is in terminal state {self.state.value}"
            )

        if msg_type not in allowed:
            raise ValueError(
                f"invalid transition: {self.state.value} -> {msg_type.value} "
                f"(conversation {self.id})"
            )

        # Check counter round limit
        if (
            msg_type == MessageType.COUNTER
            and self._counter_count() >= self.max_counter_rounds
        ):
            raise ValueError(
                f"conversation {self.id} exceeded max counter rounds "
                f"({self.max_counter_rounds})"
            )


class ConversationStore:
    """Manages active conversations and message deduplication.

    Thread-safe via internal lock. Matches Go ConversationStore exactly.
    """

    def __init__(self):
        self._conversations: Dict[str, Conversation] = {}
        self._seen_msg_ids: Dict[str, datetime] = {}
        self._lock = threading.RLock()

    def has_seen(self, msg_id: str) -> bool:
        """Return True if a message ID has already been processed."""
        with self._lock:
            return msg_id in self._seen_msg_ids

    def mark_seen(self, msg_id: str) -> None:
        """Record a message ID as processed."""
        with self._lock:
            self._seen_msg_ids[msg_id] = datetime.now(timezone.utc)

    def cleanup_seen(self, max_age: timedelta = timedelta(minutes=5)) -> None:
        """Remove seen message IDs older than max_age."""
        with self._lock:
            cutoff = datetime.now(timezone.utc) - max_age
            expired = [
                mid for mid, ts in self._seen_msg_ids.items() if ts <= cutoff
            ]
            for mid in expired:
                del self._seen_msg_ids[mid]

    def create(
        self,
        id: str,
        initiator: str,
        timeout: timedelta = DEFAULT_CONVERSATION_TIMEOUT,
        max_counter_rounds: int = DEFAULT_MAX_COUNTER_ROUNDS,
    ) -> Conversation:
        """Start a new conversation."""
        with self._lock:
            conv = Conversation(id, initiator, timeout, max_counter_rounds)
            self._conversations[id] = conv
            return conv

    def get(self, id: str) -> Optional[Conversation]:
        """Retrieve a conversation by ID, or None if not found."""
        with self._lock:
            return self._conversations.get(id)

    def list(self) -> List[Conversation]:
        """Return all active conversations."""
        with self._lock:
            return list(self._conversations.values())

    def remove(self, id: str) -> None:
        """Delete a conversation from the store."""
        with self._lock:
            self._conversations.pop(id, None)

    def timeout_conversations(self) -> List[str]:
        """Mark all expired conversations as TIMED_OUT.

        Returns the IDs of conversations that were timed out.
        """
        with self._lock:
            timed_out = []
            for id, conv in self._conversations.items():
                with conv._lock:
                    if conv.timeout.total_seconds() <= 0:
                        continue
                    if (datetime.now(timezone.utc) - conv.updated_at) <= conv.timeout:
                        continue
                    if conv.state in _TERMINAL_STATES:
                        continue
                    conv.state = NegotiationState.TIMED_OUT
                    timed_out.append(id)
            return timed_out
