"""Tests for micelio.negotiation module."""

from datetime import timedelta

import pytest

from micelio.negotiation import (
    NegotiationState,
    Conversation,
    ConversationStore,
    DEFAULT_CONVERSATION_TIMEOUT,
    DEFAULT_MAX_COUNTER_ROUNDS,
)
from micelio.protocol import MessageType, new_message, new_conversation_id


def _msg(msg_type, from_did="did:a", to_did="did:b", conv_id="conv-1"):
    return new_message(msg_type, from_did, to_did, conv_id)


class TestNegotiationState:
    """Test NegotiationState enum values match Go."""

    def test_values(self):
        assert NegotiationState.CREATED.value == "created"
        assert NegotiationState.PROPOSED.value == "proposed"
        assert NegotiationState.COUNTERED.value == "countered"
        assert NegotiationState.ACCEPTED.value == "accepted"
        assert NegotiationState.REJECTED.value == "rejected"
        assert NegotiationState.DELIVERED.value == "delivered"
        assert NegotiationState.COMPLETED.value == "completed"
        assert NegotiationState.CANCELLED.value == "cancelled"
        assert NegotiationState.TIMED_OUT.value == "timed_out"


class TestConversation:
    """Test conversation FSM transitions."""

    def test_initial_state(self):
        conv = Conversation("c1", "did:alice")
        assert conv.state == NegotiationState.CREATED
        assert conv.initiator == "did:alice"
        assert conv.responder is None

    def test_happy_path(self):
        """Full negotiation: INTENT -> PROPOSE -> ACCEPT -> DELIVER -> RECEIPT."""
        conv = Conversation("c1", "did:a")

        conv.transition(_msg(MessageType.INTENT, "did:a", "did:b"))
        assert conv.state == NegotiationState.CREATED  # INTENT stays created

        conv.transition(_msg(MessageType.PROPOSE, "did:b", "did:a"))
        assert conv.state == NegotiationState.PROPOSED
        assert conv.responder == "did:b"

        conv.transition(_msg(MessageType.ACCEPT, "did:a", "did:b"))
        assert conv.state == NegotiationState.ACCEPTED

        conv.transition(_msg(MessageType.DELIVER, "did:b", "did:a"))
        assert conv.state == NegotiationState.DELIVERED

        conv.transition(_msg(MessageType.RECEIPT, "did:a", "did:b"))
        assert conv.state == NegotiationState.COMPLETED

    def test_reject_from_created(self):
        conv = Conversation("c1", "did:a")
        conv.transition(_msg(MessageType.REJECT, "did:b", "did:a"))
        assert conv.state == NegotiationState.REJECTED

    def test_reject_from_proposed(self):
        conv = Conversation("c1", "did:a")
        conv.transition(_msg(MessageType.PROPOSE, "did:b", "did:a"))
        conv.transition(_msg(MessageType.REJECT, "did:a", "did:b"))
        assert conv.state == NegotiationState.REJECTED

    def test_counter_flow(self):
        conv = Conversation("c1", "did:a")
        conv.transition(_msg(MessageType.PROPOSE, "did:b", "did:a"))
        conv.transition(_msg(MessageType.COUNTER, "did:a", "did:b"))
        assert conv.state == NegotiationState.COUNTERED

        # Counter again
        conv.transition(_msg(MessageType.COUNTER, "did:b", "did:a"))
        assert conv.state == NegotiationState.COUNTERED

        # Accept the counter
        conv.transition(_msg(MessageType.ACCEPT, "did:a", "did:b"))
        assert conv.state == NegotiationState.ACCEPTED

    def test_cancel_from_proposed(self):
        conv = Conversation("c1", "did:a")
        conv.transition(_msg(MessageType.PROPOSE, "did:b", "did:a"))
        conv.transition(_msg(MessageType.CANCEL, "did:a", "did:b"))
        assert conv.state == NegotiationState.CANCELLED

    def test_cancel_from_accepted(self):
        conv = Conversation("c1", "did:a")
        conv.transition(_msg(MessageType.PROPOSE, "did:b", "did:a"))
        conv.transition(_msg(MessageType.ACCEPT, "did:a", "did:b"))
        conv.transition(_msg(MessageType.CANCEL, "did:b", "did:a"))
        assert conv.state == NegotiationState.CANCELLED

    def test_invalid_transition_from_created(self):
        conv = Conversation("c1", "did:a")
        with pytest.raises(ValueError, match="invalid transition"):
            conv.transition(_msg(MessageType.DELIVER))

    def test_invalid_transition_from_completed(self):
        conv = Conversation("c1", "did:a")
        conv.transition(_msg(MessageType.PROPOSE, "did:b", "did:a"))
        conv.transition(_msg(MessageType.ACCEPT, "did:a", "did:b"))
        conv.transition(_msg(MessageType.DELIVER, "did:b", "did:a"))
        conv.transition(_msg(MessageType.RECEIPT, "did:a", "did:b"))
        assert conv.state == NegotiationState.COMPLETED

        with pytest.raises(ValueError, match="terminal state"):
            conv.transition(_msg(MessageType.INTENT))

    def test_invalid_transition_from_rejected(self):
        conv = Conversation("c1", "did:a")
        conv.transition(_msg(MessageType.REJECT, "did:b", "did:a"))
        with pytest.raises(ValueError, match="terminal state"):
            conv.transition(_msg(MessageType.PROPOSE))

    def test_invalid_transition_from_cancelled(self):
        conv = Conversation("c1", "did:a")
        conv.transition(_msg(MessageType.PROPOSE, "did:b", "did:a"))
        conv.transition(_msg(MessageType.CANCEL, "did:a", "did:b"))
        with pytest.raises(ValueError, match="terminal state"):
            conv.transition(_msg(MessageType.ACCEPT))

    def test_max_counter_rounds(self):
        conv = Conversation("c1", "did:a", max_counter_rounds=2)
        conv.transition(_msg(MessageType.PROPOSE, "did:b", "did:a"))
        conv.transition(_msg(MessageType.COUNTER, "did:a", "did:b"))
        conv.transition(_msg(MessageType.COUNTER, "did:b", "did:a"))

        with pytest.raises(ValueError, match="exceeded max counter rounds"):
            conv.transition(_msg(MessageType.COUNTER, "did:a", "did:b"))

    def test_messages_tracked(self):
        conv = Conversation("c1", "did:a")
        conv.transition(_msg(MessageType.INTENT))
        conv.transition(_msg(MessageType.PROPOSE, "did:b", "did:a"))
        assert len(conv.messages) == 2

    def test_deliver_only_from_accepted(self):
        conv = Conversation("c1", "did:a")
        conv.transition(_msg(MessageType.PROPOSE, "did:b", "did:a"))
        with pytest.raises(ValueError, match="invalid transition"):
            conv.transition(_msg(MessageType.DELIVER))

    def test_receipt_only_from_delivered(self):
        conv = Conversation("c1", "did:a")
        conv.transition(_msg(MessageType.PROPOSE, "did:b", "did:a"))
        conv.transition(_msg(MessageType.ACCEPT, "did:a", "did:b"))
        with pytest.raises(ValueError, match="invalid transition"):
            conv.transition(_msg(MessageType.RECEIPT))


class TestConversationStore:
    """Test conversation store operations."""

    def test_create_and_get(self):
        store = ConversationStore()
        conv = store.create("c1", "did:a")
        assert conv.id == "c1"
        assert store.get("c1") is conv
        assert store.get("nonexistent") is None

    def test_list(self):
        store = ConversationStore()
        store.create("c1", "did:a")
        store.create("c2", "did:b")
        convs = store.list()
        assert len(convs) == 2

    def test_remove(self):
        store = ConversationStore()
        store.create("c1", "did:a")
        store.remove("c1")
        assert store.get("c1") is None

    def test_remove_nonexistent(self):
        store = ConversationStore()
        store.remove("nonexistent")  # Should not raise

    def test_has_seen_mark_seen(self):
        store = ConversationStore()
        assert not store.has_seen("msg-1")
        store.mark_seen("msg-1")
        assert store.has_seen("msg-1")

    def test_cleanup_seen(self):
        store = ConversationStore()
        store.mark_seen("msg-1")
        # Force the seen timestamp to the past
        from datetime import datetime, timezone
        store._seen_msg_ids["msg-1"] = datetime(2020, 1, 1, tzinfo=timezone.utc)
        store.cleanup_seen(max_age=timedelta(seconds=60))
        assert not store.has_seen("msg-1")

    def test_timeout_conversations(self):
        store = ConversationStore()
        conv = store.create(
            "c1", "did:a", timeout=timedelta(seconds=1)
        )
        # Force update time to past so it's clearly timed out
        from datetime import datetime, timezone
        conv.updated_at = datetime(2020, 1, 1, tzinfo=timezone.utc)

        timed_out = store.timeout_conversations()
        assert "c1" in timed_out
        assert conv.state == NegotiationState.TIMED_OUT

    def test_timeout_skips_terminal(self):
        store = ConversationStore()
        conv = store.create(
            "c1", "did:a", timeout=timedelta(seconds=0)
        )
        conv.transition(_msg(MessageType.REJECT, "did:b", "did:a"))
        from datetime import datetime, timezone
        conv.updated_at = datetime(2020, 1, 1, tzinfo=timezone.utc)

        timed_out = store.timeout_conversations()
        assert len(timed_out) == 0
        assert conv.state == NegotiationState.REJECTED
