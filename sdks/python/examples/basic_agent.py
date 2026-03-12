#!/usr/bin/env python3
"""Basic agent example demonstrating the AIP negotiation flow.

This example shows how to:
1. Create an Ed25519 identity with a DID:key
2. Build and serialize AIP protocol messages
3. Track a negotiation conversation through the FSM
4. (Optionally) connect to a Micelio node via TCP

Usage:
    python basic_agent.py
"""

import json
import sys
import os

# Allow running from the examples directory
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from micelio import (
    Identity,
    Message,
    MessageType,
    new_message,
    new_conversation_id,
    decode_message,
    IntentPayload,
    ProposePayload,
    ReceiptPayload,
    Budget,
    Conversation,
    ConversationStore,
    MicelioClient,
)


def main():
    # --- Step 1: Create identities ---
    print("=== Creating Identities ===")
    alice = Identity.generate()
    bob = Identity.generate()
    print(f"Alice: {alice.did}")
    print(f"Bob:   {bob.did}")
    print()

    # --- Step 2: Save and reload identity ---
    alice.save("alice_identity.json")
    alice_reloaded = Identity.load("alice_identity.json")
    assert alice_reloaded.did == alice.did
    print(f"Alice identity saved and reloaded successfully.")
    os.remove("alice_identity.json")
    print()

    # --- Step 3: Sign and verify ---
    print("=== Sign and Verify ===")
    data = b"Hello AIP!"
    sig = alice.sign(data)
    print(f"Signature: {sig.hex()[:32]}...")
    print(f"Self-verify: {alice.verify(data, sig)}")
    print(f"Cross-verify from DID: {Identity.verify_from(alice.did, data, sig)}")
    print(f"Wrong data verify: {alice.verify(b'wrong', sig)}")
    print()

    # --- Step 4: Build messages ---
    print("=== AIP Messages ===")
    conv_id = new_conversation_id()
    print(f"Conversation ID: {conv_id}")

    # Alice sends INTENT to Bob
    intent_msg = new_message(
        MessageType.INTENT,
        from_did=alice.did,
        to_did=bob.did,
        conversation_id=conv_id,
        payload=IntentPayload(
            capability="text-translation",
            description="Translate English to Portuguese",
            params={"source_lang": "en", "target_lang": "pt"},
            budget=Budget(amount=0.01, currency="USD"),
        ),
    )

    # Sign the message
    signable = intent_msg.signable_bytes()
    intent_msg.signature = alice.sign(signable).hex()
    print(f"\nINTENT message:")
    print(json.loads(intent_msg.encode_pretty()))
    print()

    # --- Step 5: Encode/decode round-trip ---
    encoded = intent_msg.encode()
    decoded = decode_message(encoded)
    assert decoded.msg_id == intent_msg.msg_id
    assert decoded.type == MessageType.INTENT
    assert decoded.from_did == alice.did
    assert decoded.to_did == bob.did
    print(f"Encode/decode round-trip: OK (msg_id={decoded.msg_id})")
    print()

    # --- Step 6: Negotiation FSM ---
    print("=== Negotiation FSM ===")
    store = ConversationStore()
    conv = store.create(conv_id, alice.did)
    print(f"State: {conv.state.value}")

    # Alice sends INTENT
    conv.transition(intent_msg)
    print(f"After INTENT: {conv.state.value}")

    # Bob sends PROPOSE
    propose_msg = new_message(
        MessageType.PROPOSE,
        from_did=bob.did,
        to_did=alice.did,
        conversation_id=conv_id,
        payload=ProposePayload(
            capability="text-translation",
            approach="Neural MT with back-translation verification",
            cost=Budget(amount=0.005, currency="USD"),
            eta=30.0,  # 30 seconds
        ),
    )
    conv.transition(propose_msg)
    print(f"After PROPOSE: {conv.state.value}")

    # Alice sends ACCEPT
    accept_msg = new_message(
        MessageType.ACCEPT,
        from_did=alice.did,
        to_did=bob.did,
        conversation_id=conv_id,
    )
    conv.transition(accept_msg)
    print(f"After ACCEPT: {conv.state.value}")

    # Bob sends DELIVER
    deliver_msg = new_message(
        MessageType.DELIVER,
        from_did=bob.did,
        to_did=alice.did,
        conversation_id=conv_id,
        payload={"result": {"translated_text": "Ola Mundo!"}},
    )
    conv.transition(deliver_msg)
    print(f"After DELIVER: {conv.state.value}")

    # Alice sends RECEIPT
    receipt_msg = new_message(
        MessageType.RECEIPT,
        from_did=alice.did,
        to_did=bob.did,
        conversation_id=conv_id,
        payload=ReceiptPayload(accepted=True, rating=5, feedback="Excellent!"),
    )
    conv.transition(receipt_msg)
    print(f"After RECEIPT: {conv.state.value}")
    print()

    # --- Step 7: Invalid transition ---
    print("=== Invalid Transition Test ===")
    try:
        conv.transition(intent_msg)
        print("ERROR: Should have raised ValueError")
    except ValueError as e:
        print(f"Caught expected error: {e}")
    print()

    # --- Step 8: Message dedup ---
    print("=== Message Dedup ===")
    print(f"Seen intent? {store.has_seen(intent_msg.msg_id)}")
    store.mark_seen(intent_msg.msg_id)
    print(f"After mark: {store.has_seen(intent_msg.msg_id)}")
    print()

    # --- Step 9: TTL validation ---
    print("=== TTL Validation ===")
    fresh_msg = new_message(
        MessageType.PING, alice.did, bob.did, conv_id, ttl=60
    )
    print(f"Fresh message expired? {fresh_msg.is_expired()}")
    print(f"TTL validation: {fresh_msg.validate_ttl()}")
    print()

    print("=== Complete ===")
    print(f"Total messages in conversation: {len(conv.messages)}")
    print(f"Responder: {conv.responder}")


if __name__ == "__main__":
    main()
