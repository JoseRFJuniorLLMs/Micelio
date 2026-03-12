<p align="center">
  <img src="media/micelio.gif" width="100%" alt="Micélio Demo" />
</p>

<h1 align="center">🍄 Micélio</h1>

<p align="center">
  <strong>The Mycelium Network for Autonomous Agents</strong><br/>
  <em>AIP (Agent Internet Protocol) — an open P2P protocol for agent communication, discovery, and negotiation</em>
</p>

<p align="center">
  <a href="#quick-start">Quick Start</a> •
  <a href="#architecture">Architecture</a> •
  <a href="#the-protocol">The Protocol</a> •
  <a href="#api-reference">API Reference</a> •
  <a href="#demo">Demo</a> •
  <a href="#roadmap">Roadmap</a>
</p>

---

## The Problem

When two AI agents need to communicate today, the options are:

| Current Approach | Problem |
|---|---|
| REST APIs | Proprietary, incompatible formats, no negotiation semantics |
| SDKs (LangChain, CrewAI) | Language lock-in, framework-dependent |
| Message Brokers (Kafka, RabbitMQ) | Single point of failure, centralized |
| MCP (Anthropic) | Client→Server only, no P2P, no discovery, no identity |

**None of them provide**: peer discovery, autonomous negotiation, cryptographic identity, or decentralized operation.

## The Solution

**AIP is to agents what HTTP was to documents.**

A minimal, universal protocol where agents:
1. **Discover** each other (mDNS / Kademlia DHT)
2. **Identify** themselves (DID:key from Ed25519 — no CA, no blockchain)
3. **Negotiate** tasks (INTENT → PROPOSE → ACCEPT → DELIVER → RECEIPT)
4. **Verify** every message (Ed25519 signatures)

No servers. No API keys. No central authority. No tokens. No blockchain.

---

## Quick Start

### Prerequisites

- **Go 1.22+**

### Installation

```bash
git clone https://github.com/JoseRFJuniorLLMs/Micelio.git
cd Micelio
go mod tidy
```

### Run the Demo

Two agents negotiate a translation task — entirely peer-to-peer:

```bash
go run ./examples/two_agents/
```

**Output:**
```
=== Micélio AIP Demo — Two Agents, No Server ===

Alice DID: did:key:z6MkgCABuHptPFte3HaAnLQYj2Eo86mt3fPtmueXqkr8M64W
Bob   DID: did:key:z6Mkjf49WGdEQuLDdVocD36k1f9zU1WsTq65YkWeoDEJKXND

--- Negotiation Start ---

[Alice] sending INTENT: "Translate 'Hello, how are you?' from English to Portuguese"
[Bob]   received INTENT → sending PROPOSE: "Neural translation using local model"
[Alice] received PROPOSE → sending ACCEPT
[Bob]   received ACCEPT → doing the work...
[Bob]   sending DELIVER: translation complete
[Alice] received DELIVER:
  Original:   Hello, how are you?
  Translated: Olá, como você está?
[Alice] sending RECEIPT (rating: 5/5)
[Bob]   received RECEIPT: accepted=true, rating=5/5

--- Negotiation Complete ---
Full flow: INTENT → PROPOSE → ACCEPT → DELIVER → RECEIPT
No server. No API key. No central authority.
```

### Generate an Agent Identity

```bash
go run ./cmd/micelio/ keygen --output my-agent.json
```

Creates a persistent Ed25519 keypair with DID:key identifier:

```json
{
  "private_key_seed": "base64-encoded-32-byte-seed",
  "did": "did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK"
}
```

### Start an Agent Node

```bash
go run ./cmd/micelio/ agent --port 9000 --identity my-agent.json
```

The agent starts listening for peers via mDNS and is ready to receive AIP messages.

### Show Identity Info

```bash
go run ./cmd/micelio/ info --identity my-agent.json
```

---

## Architecture

### Agent Internet Stack — 8 Layers

```
┌──────────────────────────────────────────────────────────────┐
│  L7  │  Applications                                         │
│      │  Your agents, workflows, pipelines                    │
├──────┼───────────────────────────────────────────────────────┤
│  L6  │  Cognition Layer                                      │
│      │  NietzscheDB, LLMs, LangGraph, symbolic AI            │
├──────┼───────────────────────────────────────────────────────┤
│  L5  │  AIP Protocol  ◄── THIS PROJECT                      │
│      │  • Intent Protocol (negotiation state machine)        │
│      │  • Capability Discovery & Advertisement               │
│      │  • Reputation Signaling (local-first trust scores)    │
├──────┼───────────────────────────────────────────────────────┤
│  L3  │  AIP Message Format                                   │
│      │  ULID message IDs, Ed25519 signatures, TTL, replay    │
│      │  protection, JSON Schema validation                   │
├──────┼───────────────────────────────────────────────────────┤
│  L2  │  Discovery Layer                                      │
│      │  mDNS (local), Kademlia DHT (global), Gossipsub       │
├──────┼───────────────────────────────────────────────────────┤
│  L1  │  Identity Layer                                       │
│      │  DID:key from Ed25519, self-sovereign, no registry    │
├──────┼───────────────────────────────────────────────────────┤
│  L0  │  Transport Layer                                      │
│      │  libp2p — TCP, QUIC, WebTransport, Noise encryption   │
└──────┴───────────────────────────────────────────────────────┘
```

### Analogia com a Internet Clássica

| Internet Humana | Internet de Agentes (AIP) |
|---|---|
| TCP/IP | libp2p (TCP, QUIC, Noise) |
| DNS | Kademlia DHT + mDNS |
| HTTP | **AIP** |
| Certificados SSL | DID:key (Ed25519) |
| Web Apps | Agentes Autónomos |

### Why AIP Only Lives at L5

> *The fatal mistake of Fetch.ai and SingularityNET was mixing protocol with economics, blockchain, and marketplace in a single system.*

AIP is **just a protocol** — a specification of how agents communicate. What does NOT belong in AIP:

- ❌ Payments / tokens / blockchain
- ❌ Code execution or verification
- ❌ Knowledge storage (that's L6)
- ❌ Business logic (that's L7)
- ❌ Distributed consensus

This separation is what killed competing projects and what makes AIP viable.

---

## The Protocol

### Negotiation State Machine (FSM)

```
                    ┌──────────┐
                    │ CREATED  │
                    └────┬─────┘
                         │ INTENT
                         ▼
               ┌─────────────────────┐
          ┌────│     PROPOSED        │────┐
          │    └─────────┬───────────┘    │
       REJECT            │             COUNTER
          │           ACCEPT              │
          ▼              │                ▼
   ┌──────────┐          │         ┌───────────┐
   │ REJECTED │          │         │ COUNTERED │──── ACCEPT/REJECT/COUNTER
   └──────────┘          ▼         └───────────┘
                  ┌──────────┐
                  │ ACCEPTED │
                  └────┬─────┘
                       │ DELIVER
                       ▼
                ┌────────────┐
                │ DELIVERED  │
                └────┬───────┘
                     │ RECEIPT
                     ▼
                ┌───────────┐
                │ COMPLETED │
                └───────────┘
```

**Valid transitions:**

| From State | Allowed Messages |
|---|---|
| `created` | INTENT, PROPOSE, REJECT |
| `proposed` | ACCEPT, REJECT, COUNTER, CANCEL |
| `countered` | ACCEPT, REJECT, COUNTER, CANCEL |
| `accepted` | DELIVER, CANCEL |
| `delivered` | RECEIPT |
| `completed` | — (terminal) |
| `rejected` | — (terminal) |
| `cancelled` | — (terminal) |

### Message Types

| Type | Direction | Purpose |
|---|---|---|
| `INTENT` | Initiator → Responder | "I need X done" |
| `PROPOSE` | Responder → Initiator | "I can do X, here's how" |
| `COUNTER` | Either → Either | "How about this instead?" |
| `ACCEPT` | Either → Either | "Deal" |
| `REJECT` | Either → Either | "No deal" |
| `DELIVER` | Responder → Initiator | "Here's the result" |
| `RECEIPT` | Initiator → Responder | "Got it, here's my rating" |
| `CANCEL` | Either → Either | "Abort this negotiation" |
| `DISCOVER` | Broadcast | "Who can do X?" |
| `CAPABILITY_ADVERTISE` | Broadcast | "I can do X, Y, Z" |
| `CAPABILITY_QUERY` | Direct/Broadcast | "What can you do?" |
| `PING` / `PONG` | Direct | Liveness check |
| `ERROR` | Direct | Protocol error |

### AIP Message Envelope

Every message follows the same envelope (JSON, MessagePack, or CBOR):

```json
{
  "version": "aip/0.1",
  "msg_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV",
  "type": "INTENT",
  "from": "did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK",
  "to": "did:key:z6Mkk7yqnGF3YwTrLpqrW6PGsKci7dNqh1CjnvMbzrMerSKq",
  "conversation_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV",
  "timestamp": "2026-03-12T10:00:00Z",
  "ttl": 60,
  "payload": {
    "capability": "nlp.translate",
    "description": "Translate text from English to Portuguese",
    "params": {
      "text": "Hello, how are you?",
      "source_lang": "en",
      "target_lang": "pt"
    }
  },
  "signature": "base64-encoded-ed25519-signature-over-canonical-message"
}
```

**Key design choices:**
- **ULID** for `msg_id` — lexicographically sortable by time, 128-bit entropy
- **DID:key** for `from`/`to` — self-sovereign identity, no registry
- **Ed25519 signature** — covers the entire message (without signature field)
- **TTL** — messages expire, preventing replay attacks
- **conversation_id** — groups all messages in a single negotiation

### Payload Schemas

#### INTENT — "I need something done"
```json
{
  "capability": "nlp.translate",
  "description": "Translate text from English to Portuguese",
  "params": { "text": "Hello", "source_lang": "en", "target_lang": "pt" },
  "budget": { "amount": 0.01, "currency": "USD" },
  "deadline": "2026-03-12T11:00:00Z"
}
```

#### PROPOSE — "I can do it"
```json
{
  "capability": "nlp.translate",
  "approach": "Neural translation using local model, no external API",
  "cost": { "amount": 0.005, "currency": "USD" },
  "eta": "PT5S",
  "conditions": ["text must be under 1000 chars"]
}
```

#### DELIVER — "Here's the result"
```json
{
  "result": { "translated": "Olá, como você está?", "confidence": 0.97 },
  "metadata": { "model": "local-nmt-v3", "duration": "0.5s" }
}
```

#### RECEIPT — "Confirmed, here's my rating"
```json
{
  "accepted": true,
  "rating": 5,
  "feedback": "Perfect translation!"
}
```

#### CAPABILITY_ADVERTISE — "Here's what I can do"
```json
{
  "name": "nlp.translate",
  "version": "1.0.0",
  "description": "Neural machine translation between 100+ languages",
  "input_schema": { "type": "object", "required": ["text", "target_lang"] },
  "output_schema": { "type": "object", "required": ["translated"] },
  "tags": ["nlp", "translation", "multilingual"]
}
```

---

## API Reference

### Identity (`pkg/identity`)

```go
// Generate a new random identity
id, _ := identity.Generate()
fmt.Println(id.DID) // did:key:z6Mk...

// Sign and verify
sig, _ := id.Sign([]byte("hello"))
ok, _ := id.Verify([]byte("hello"), sig) // true

// Persist and load
id.Save("agent.json")
id2, _ := identity.Load("agent.json")

// Verify from DID string (no private key needed)
ok, _ = identity.VerifyFrom(id.DID, data, sig)
```

### Agent (`pkg/agent`)

```go
// Create an agent
agent, _ := agent.New(ctx, agent.Config{
    Name: "MyAgent",
    Port: 9000,
})

// Register capabilities
agent.RegisterCapability(agent.Capability{
    Name:        "code.review",
    Description: "Review Go code for bugs and style issues",
    Version:     "1.0.0",
})

// Handle incoming messages
agent.OnMessage(protocol.TypeIntent, func(from peer.ID, msg *protocol.Message) *protocol.Message {
    // Process the intent, return a PROPOSE or REJECT
    reply, _ := protocol.NewMessage(protocol.TypePropose, agent.DID(), msg.From, msg.ConversationID, propose)
    return reply
})

// Initiate a negotiation
conv, _ := agent.SendIntent(ctx, targetPeerID, protocol.IntentPayload{
    Capability:  "nlp.translate",
    Description: "Translate this text",
})

// Send follow-up messages
agent.SendPropose(ctx, target, convID, proposePayload)
agent.SendAccept(ctx, target, convID)
agent.SendDeliver(ctx, target, convID, deliverPayload)
agent.SendReceipt(ctx, target, convID, receiptPayload)
```

### Protocol (`pkg/protocol`)

```go
// Create messages with auto-generated ULID and timestamp
msg, _ := protocol.NewMessage(
    protocol.TypeIntent,
    "did:key:z6Mk...", // from
    "did:key:z6Mk...", // to
    convID,
    intentPayload,
)

// Serialize / deserialize
bytes, _ := msg.Encode()
msg2, _ := protocol.DecodeMessage(bytes)

// Conversation tracking with FSM
store := protocol.NewConversationStore()
conv := store.Create(convID, initiatorDID)
conv.Transition(intentMsg)  // created → created (INTENT sent)
conv.Transition(proposeMsg) // created → proposed
conv.Transition(acceptMsg)  // proposed → accepted
conv.Transition(deliverMsg) // accepted → delivered
conv.Transition(receiptMsg) // delivered → completed
```

### Network (`pkg/network`)

```go
// Create a libp2p host with GossipSub
host, _ := network.New(ctx, network.Config{
    ListenPort: 9000,
    PrivKey:    identity.PrivKey,
})

// Enable mDNS peer discovery
host.StartDiscovery()

// Register AIP protocol handler
host.SetStreamHandler(func(peerID peer.ID, msg *protocol.Message) {
    // Handle incoming AIP messages
})

// Send directly to a peer (length-prefixed binary framing)
host.SendDirect(ctx, targetPeerID, msg)

// Join GossipSub topics for broadcast
topic, _ := host.JoinTopic("aip-capabilities")
```

---

## Project Structure

```
Micelio/
├── cmd/
│   └── micelio/
│       └── main.go              # CLI: keygen, agent, info (152 lines)
├── pkg/
│   ├── identity/
│   │   └── did.go               # L1: DID:key, Ed25519, sign/verify (246 lines)
│   ├── protocol/
│   │   ├── types.go             # Message types, states, constants (49 lines)
│   │   ├── message.go           # AIP envelope, payloads, ULID (127 lines)
│   │   └── negotiation.go       # FSM, conversation tracking (148 lines)
│   ├── network/
│   │   ├── host.go              # libp2p host, GossipSub, mDNS (153 lines)
│   │   ├── discovery.go         # mDNS peer discovery handler (38 lines)
│   │   └── handler.go           # Stream handler, length-prefix framing (49 lines)
│   └── agent/
│       └── agent.go             # Agent abstraction, message handlers (248 lines)
├── schemas/
│   ├── message.schema.json      # AIP envelope JSON Schema
│   ├── intent.schema.json       # INTENT payload schema
│   ├── propose.schema.json      # PROPOSE payload schema
│   └── capability.schema.json   # CAPABILITY_ADVERTISE schema
├── examples/
│   └── two_agents/
│       └── main.go              # Two-agent P2P demo (243 lines)
├── media/
│   └── micelio.mp4              # Demo video
├── AIP_Especificacao_Tecnica_v0.2-Micelio.docx
├── Agent_Internet_Stack_v1.0-Micelio.docx
├── go.mod
├── go.sum
└── README.md
```

**Total: ~1,450 lines of Go** — minimal, no bloat, every line has a purpose.

---

## Key Design Decisions

### Why libp2p?

- Battle-tested (IPFS, Filecoin, Ethereum 2.0)
- Transport-agnostic (TCP, QUIC, WebTransport)
- Built-in encryption (Noise protocol)
- mDNS for local discovery, Kademlia DHT for global
- GossipSub for pub/sub broadcast

### Why DID:key (not API keys, not OAuth, not blockchain)?

```
Ed25519 Keypair → Multicodec prefix (0xed01) → Base58btc → did:key:z...
```

- **Self-sovereign**: no registration, no central authority
- **Verifiable offline**: public key is embedded in the DID itself
- **No blockchain**: DID:key method is purely cryptographic
- **Interoperable**: W3C DID standard

### Why ULID (not UUID)?

```
01ARZ3NDEKTSV4RRFFQ69G5FAV
|----------|------------|
 timestamp    random
 (48-bit)   (80-bit)
```

- Lexicographically sortable by creation time
- Efficient range queries in databases (NietzscheDB, etc.)
- Same collision resistance as UUIDv4 (128 bits)
- 26 characters, Crockford Base32

### Why JSON Schema for Capabilities?

Capabilities are advertised with machine-readable schemas. Before negotiation starts, agents can validate:
- "Can I provide the inputs this capability needs?"
- "Will the output format work for my use case?"

This creates **verifiable contracts** before any work begins.

### Wire Format

Messages are transmitted as **length-prefixed binary frames**:

```
[4 bytes: payload length (big-endian uint32)] [N bytes: JSON payload]
```

Max message size: 1 MB. This is intentionally simple — no HTTP overhead, no content negotiation, just raw protocol.

---

## Comparison with Existing Systems

| Feature | AIP (Micélio) | MCP (Anthropic) | Fetch.ai | AutoGPT | LangChain |
|---|---|---|---|---|---|
| P2P | ✅ libp2p | ❌ Client-Server | ✅ (+ blockchain) | ❌ | ❌ |
| Negotiation | ✅ State machine | ❌ | Partial | ❌ | ❌ |
| Identity | ✅ DID:key | ❌ API keys | Token-based | ❌ | ❌ |
| Discovery | ✅ mDNS + DHT | ❌ | ✅ | ❌ | ❌ |
| Blockchain required | ❌ | ❌ | ✅ | ❌ | ❌ |
| Token required | ❌ | ❌ | ✅ | ❌ | ❌ |
| Language agnostic | ✅ JSON Schema | Partial | ❌ | ❌ | ❌ Python |
| Signatures | ✅ Ed25519 | ❌ | ✅ | ❌ | ❌ |
| Spec-first | ✅ | ✅ | ❌ | ❌ | ❌ |

---

## Security Model

### Cryptography

| Component | Algorithm | Purpose |
|---|---|---|
| Identity | Ed25519 | Keypair generation, DID derivation |
| Signing | Ed25519 | Message authentication and integrity |
| Verification | Ed25519 | Verify sender identity without network access |
| Transport | Noise Protocol (via libp2p) | Encrypted peer connections |

### Threat Mitigation

| Threat | Mitigation |
|---|---|
| Replay attacks | TTL expiration + nonce deduplication (60s window) |
| Message tampering | Ed25519 signature over canonical message |
| Identity spoofing | DID:key derived from public key — unforgeable |
| Man-in-the-middle | Noise protocol encryption at transport layer |
| Spam / DoS | Future: reputation layer with trust scores |
| Sybil attacks | Future: local-first trust graph per agent |

---

## Roadmap

- [x] **Phase 0** — Specification (AIP Draft 0.2)
- [x] **Phase 1** — Transport + Identity (libp2p + DID:key + Ed25519)
- [x] **Phase 2** — Messaging Layer (envelope, signatures, TTL, ULID)
- [x] **Phase 3** — Intent Protocol (negotiation FSM, conversation tracking)
- [ ] **Phase 4** — Capability Discovery (DHT provider records, gossip broadcast)
- [ ] **Phase 5** — Reputation Layer (local-first trust scores, weighted decay)
- [ ] **Phase 6** — SDKs (JavaScript/TypeScript, Python, Rust)
- [ ] **Phase 7** — WebTransport (browser-native agents)
- [ ] **Phase 8** — NietzscheDB Integration (L6 cognition layer)

---

## Dependencies

| Package | Version | Purpose |
|---|---|---|
| `github.com/libp2p/go-libp2p` | v0.38.2 | P2P networking, Noise encryption |
| `github.com/libp2p/go-libp2p-pubsub` | v0.12.0 | GossipSub broadcast |
| `github.com/multiformats/go-multiaddr` | v0.14.0 | Network address abstraction |
| `github.com/oklog/ulid/v2` | v2.1.0 | Time-sortable unique IDs |

Zero external dependencies beyond libp2p ecosystem. No frameworks. No ORMs. No magic.

---

## Related Documents

| Document | Description |
|---|---|
| `AIP_Especificacao_Tecnica_v0.2-Micelio.docx` | Full AIP specification — all layers, algorithms, schemas |
| `Agent_Internet_Stack_v1.0-Micelio.docx` | Agent Internet Stack design — 8-layer architecture rationale |
| `schemas/` | Machine-readable JSON Schemas for all message types |

---

## License

MIT

---

<p align="center">
  <em>"Like mycelium connecting trees in a forest, AIP connects autonomous agents in a decentralized network — sharing resources, negotiating tasks, and building collective intelligence without central control."</em>
</p>

<p align="center">
  🍄 <strong>Micélio</strong> — Protocol, not platform.
</p>
