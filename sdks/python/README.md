# Micelio Python SDK

Python SDK for the Agent Internet Protocol (AIP). Enables Python agents to communicate with Go agents via the AIP protocol with full wire compatibility.

## Installation

```bash
# Basic install
pip install -e .

# With mDNS discovery support
pip install -e ".[discovery]"

# With development dependencies
pip install -e ".[dev]"
```

## Quick Start

```python
from micelio import (
    Identity, MessageType, new_message, new_conversation_id,
    IntentPayload, Budget, Conversation, MicelioClient,
)

# Create an identity (Ed25519 + DID:key)
agent = Identity.generate()
print(f"My DID: {agent.did}")

# Build an INTENT message
conv_id = new_conversation_id()
msg = new_message(
    MessageType.INTENT,
    from_did=agent.did,
    to_did="did:key:zRecipient",
    conversation_id=conv_id,
    payload=IntentPayload(
        capability="text-translation",
        description="Translate English to Portuguese",
        budget=Budget(amount=0.01, currency="USD"),
    ),
)

# Sign the message
msg.signature = agent.sign(msg.signable_bytes()).hex()

# Connect to a Micelio node
with MicelioClient() as client:
    client.connect("localhost", 9000)
    client.send_message(msg)
    response = client.receive_message()
```

## API Reference

### Identity (`micelio.identity`)
- `Identity.generate()` - Create new Ed25519 identity
- `Identity.from_seed(bytes)` - Reconstruct from 32-byte seed
- `Identity.load(path)` / `identity.save(path)` - Persist to JSON
- `identity.sign(data)` / `identity.verify(data, sig)` - Ed25519 signatures
- `Identity.verify_from(did, data, sig)` - Verify from DID string
- `identity.did` - The `did:key:z...` identifier

### Protocol (`micelio.protocol`)
- `new_message(type, from, to, conv_id, payload)` - Create AIP message
- `decode_message(bytes)` - Deserialize from JSON
- `message.encode()` / `message.signable_bytes()` - Serialize
- `message.validate_ttl()` / `message.is_expired()` - TTL checks
- Payload types: `IntentPayload`, `ProposePayload`, `CounterPayload`, `DeliverPayload`, `ReceiptPayload`, `RejectPayload`, `CancelPayload`, `ErrorPayload`, `PingPayload`

### Negotiation (`micelio.negotiation`)
- `Conversation(id, initiator)` - FSM tracking a negotiation
- `conversation.transition(msg)` - Advance state machine
- `ConversationStore()` - Manage multiple conversations with dedup

### Client (`micelio.client`)
- `MicelioClient()` - TCP client with length-prefixed framing
- `client.connect(host, port)` / `client.close()`
- `client.send_message(msg)` / `client.receive_message()`

### Discovery (`micelio.discovery`)
- `discover_peers(timeout=5)` - Find peers via mDNS (requires `zeroconf`)

## Wire Compatibility

This SDK produces messages that are fully wire-compatible with the Go implementation:
- Same base58btc encoding (Bitcoin alphabet)
- Same DID:key multicodec prefix (`0xed01`)
- Same JSON field names and format
- Same length-prefixed TCP framing (4 bytes big-endian + JSON)
- Same negotiation FSM states and transitions
