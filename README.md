<p align="center">
  <img src="media/micelio.gif" width="100%" alt="Micelio Demo" />
</p>

<h1 align="center">Micelio</h1>

<p align="center">
  <strong>The Mycelium Network for Autonomous Agents</strong><br/>
  <em>AIP (Agent Internet Protocol) — an open P2P protocol for agent communication, discovery, and negotiation</em>
</p>

<p align="center">
  <a href="#quick-start">Quick Start</a> &bull;
  <a href="#architecture">Architecture</a> &bull;
  <a href="#the-protocol">The Protocol</a> &bull;
  <a href="#security">Security</a> &bull;
  <a href="#api-reference">API Reference</a> &bull;
  <a href="#demo">Demo</a> &bull;
  <a href="#roadmap">Roadmap</a>
</p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white" />
  <img alt="License" src="https://img.shields.io/badge/License-MIT-green" />
  <img alt="CI" src="https://img.shields.io/github/actions/workflow/status/JoseRFJuniorLLMs/Micelio/ci.yml?label=CI" />
</p>

---

## The Problem

When two AI agents need to communicate today, the options are:

| Current Approach | Problem |
|---|---|
| REST APIs | Proprietary, incompatible formats, no negotiation semantics |
| SDKs (LangChain, CrewAI) | Language lock-in, framework-dependent |
| Message Brokers (Kafka, RabbitMQ) | Single point of failure, centralized |
| MCP (Anthropic) | Client-Server only, no P2P, no discovery, no identity |

**None of them provide**: peer discovery, autonomous negotiation, cryptographic identity, or decentralized operation.

## The Solution

**AIP is to agents what HTTP was to documents.**

A minimal, universal protocol where agents:
1. **Discover** each other (mDNS / Kademlia DHT)
2. **Identify** themselves (DID:key from Ed25519 — no CA, no blockchain)
3. **Negotiate** tasks (INTENT -> PROPOSE -> ACCEPT -> DELIVER -> RECEIPT)
4. **Verify** every message (Ed25519 signatures with replay protection)

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

### Build

```bash
make build        # Build binary
make test         # Run tests with race detector
make test-cover   # Generate coverage report
make all          # Format + vet + test + build
```

### Docker

```bash
docker build -t micelio .
docker run -p 9000:9000 micelio agent --port 9000
```

### Run the Demo

Two agents negotiate a translation task — entirely peer-to-peer:

```bash
go run ./examples/two_agents/
```

**Output:**
```
=== Micelio AIP Demo — Two Agents, No Server ===

Alice DID: did:key:z6MkgCABuHptPFte3HaAnLQYj2Eo86mt3fPtmueXqkr8M64W
Bob   DID: did:key:z6Mkjf49WGdEQuLDdVocD36k1f9zU1WsTq65YkWeoDEJKXND

--- Negotiation Start ---

[Alice] sending INTENT: "Translate 'Hello, how are you?' from English to Portuguese"
[Bob]   received INTENT -> sending PROPOSE: "Neural translation using local model"
[Alice] received PROPOSE -> sending ACCEPT
[Bob]   received ACCEPT -> doing the work...
[Bob]   sending DELIVER: translation complete
[Alice] received DELIVER:
  Original:   Hello, how are you?
  Translated: Ola, como voce esta?
[Alice] sending RECEIPT (rating: 5/5)
[Bob]   received RECEIPT: accepted=true, rating=5/5

--- Negotiation Complete ---
Full flow: INTENT -> PROPOSE -> ACCEPT -> DELIVER -> RECEIPT
No server. No API key. No central authority.
```

### Generate an Agent Identity

```bash
# Plain (unencrypted)
go run ./cmd/micelio/ keygen --output my-agent.json

# Encrypted with passphrase (AES-256-GCM + PBKDF2)
go run ./cmd/micelio/ keygen --output my-agent.enc.json --encrypt
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
+------+----------------------------------------------------------+
|  L7  |  Applications                                            |
|      |  Your agents, workflows, pipelines                       |
+------+----------------------------------------------------------+
|  L6  |  Cognition Layer                                         |
|      |  NietzscheDB (hyperbolic memory, trust, desires)         |
+------+----------------------------------------------------------+
|  L5  |  AIP Protocol  <-- THIS PROJECT                          |
|      |  - Intent Protocol (negotiation state machine)           |
|      |  - Capability Discovery & Advertisement                  |
|      |  - Reputation Signaling (local-first trust scores)       |
+------+----------------------------------------------------------+
|  L3  |  AIP Message Format                                      |
|      |  ULID IDs, Ed25519 signatures, TTL, replay protection,   |
|      |  message deduplication, JSON Schema validation            |
+------+----------------------------------------------------------+
|  L2  |  Discovery Layer                                         |
|      |  mDNS (local), Kademlia DHT (global), GossipSub          |
+------+----------------------------------------------------------+
|  L1  |  Identity Layer                                          |
|      |  DID:key from Ed25519, AES-256-GCM key encryption        |
+------+----------------------------------------------------------+
|  L0  |  Transport Layer                                         |
|      |  libp2p - TCP, QUIC, TLS + Noise encryption,             |
|      |  connection manager, rate limiting                        |
+------+----------------------------------------------------------+
```

### Internet Analogy

| Human Internet | Agent Internet (AIP) |
|---|---|
| TCP/IP | libp2p (TCP, QUIC, TLS, Noise) |
| DNS | Kademlia DHT + mDNS |
| HTTP | **AIP** |
| SSL Certificates | DID:key (Ed25519) |
| Web Apps | Autonomous Agents |

### Why AIP Only Lives at L5

AIP is **just a protocol** — a specification of how agents communicate. What does NOT belong in AIP:

- Payments / tokens / blockchain
- Code execution or verification
- Knowledge storage (that's L6 — NietzscheDB)
- Business logic (that's L7)
- Distributed consensus

This separation is what killed competing projects and what makes AIP viable.

---

## The Protocol

### Negotiation State Machine (FSM)

```
                    +----------+
                    | CREATED  |
                    +----+-----+
                         | INTENT
                         v
               +---------------------+
          +----+     PROPOSED        +----+
          |    +----------+----------+    |
       REJECT             |            COUNTER
          |            ACCEPT             |
          v               |               v
   +----------+           |         +-----------+
   | REJECTED |           |         | COUNTERED +---- ACCEPT/REJECT/COUNTER
   +----------+           v         +-----------+
                   +----------+
                   | ACCEPTED |
                   +----+-----+
                        | DELIVER
                        v
                 +----------+
                 | DELIVERED|
                 +----+-----+
                      | RECEIPT
                      v
                 +-----------+
                 | COMPLETED |
                 +-----------+

  (CANCEL may transition from PROPOSED, COUNTERED, or ACCEPTED)
  (TIMED_OUT auto-transitions from any non-terminal state after timeout)
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
| `timed_out` | — (terminal) |

### Message Types

| Type | Direction | Purpose |
|---|---|---|
| `INTENT` | Initiator -> Responder | "I need X done" |
| `PROPOSE` | Responder -> Initiator | "I can do X, here's how" |
| `COUNTER` | Either -> Either | "How about this instead?" |
| `ACCEPT` | Either -> Either | "Deal" |
| `REJECT` | Either -> Either | "No deal" (with reason + code) |
| `DELIVER` | Responder -> Initiator | "Here's the result" |
| `RECEIPT` | Initiator -> Responder | "Got it, here's my rating" |
| `CANCEL` | Either -> Either | "Abort this negotiation" |
| `DISCOVER` | Broadcast | "Who can do X?" |
| `CAPABILITY_ADVERTISE` | Broadcast | "I can do X, Y, Z" |
| `CAPABILITY_QUERY` | Direct/Broadcast | "What can you do?" |
| `PING` / `PONG` | Direct | Liveness check (with nonce) |
| `ERROR` | Direct | Protocol error (with code + details) |

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
  "payload": { ... },
  "signature": "base64-encoded-ed25519-signature-over-canonical-message"
}
```

**Key design choices:**
- **ULID** for `msg_id` — lexicographically sortable by time, 128-bit entropy
- **DID:key** for `from`/`to` — self-sovereign identity, no registry
- **Ed25519 signature** — covers the entire message (without signature field)
- **TTL** — messages expire, preventing replay attacks
- **conversation_id** — groups all messages in a single negotiation
- **Message deduplication** — seen msg_id cache prevents replays

---

## Security

### Cryptography

| Component | Algorithm | Purpose |
|---|---|---|
| Identity | Ed25519 | Keypair generation, DID derivation |
| Signing | Ed25519 | Message authentication and integrity |
| Verification | Ed25519 | Verify sender identity without network access |
| Key Encryption | AES-256-GCM + PBKDF2-SHA256 (100K iterations) | At-rest key protection |
| Transport | TLS 1.3 + Noise Protocol (via libp2p) | Encrypted peer connections |

### Threat Mitigation

| Threat | Mitigation | Status |
|---|---|---|
| Replay attacks | TTL expiration + message dedup (msg_id cache, 5min window) | Implemented |
| Message tampering | Ed25519 signature over canonical message | Implemented |
| Signature bypass | Mandatory signature verification on receive | Implemented |
| Identity spoofing | DID:key derived from public key — unforgeable | Implemented |
| Man-in-the-middle | TLS + Noise protocol at transport layer | Implemented |
| Key theft | AES-256-GCM encrypted key files with passphrase | Implemented |
| Spam / DoS | Per-peer rate limiter (100 msg/s default) | Implemented |
| Peer flooding | Connection manager (50-200 peers) + max 200 discovered | Implemented |
| Reconnect storms | Exponential backoff on failed connections (2^n sec, max 5min) | Implemented |
| Handler crashes | Panic recovery in message handlers | Implemented |
| Counter loop | Max counter rounds per conversation (default: 10) | Implemented |
| Stale conversations | Configurable timeout with auto-expiry | Implemented |
| Sybil attacks | Future: local-first trust graph per agent | Planned |

### Identity Key Encryption

Agent keys can be encrypted at rest using AES-256-GCM with PBKDF2 key derivation:

```go
// Save encrypted
id.SaveEncrypted("agent.enc.json", "my-passphrase")

// Load encrypted
id, _ := identity.LoadEncrypted("agent.enc.json", "my-passphrase")
```

The encrypted file contains the AES-256-GCM ciphertext, PBKDF2 salt, and GCM nonce — the passphrase never touches disk.

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

// Persist and load (plaintext)
id.Save("agent.json")
id2, _ := identity.Load("agent.json")

// Persist and load (encrypted)
id.SaveEncrypted("agent.enc.json", "passphrase")
id3, _ := identity.LoadEncrypted("agent.enc.json", "passphrase")

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

// Handle incoming messages (with automatic signature verification + dedup)
agent.OnMessage(protocol.TypeIntent, func(from peer.ID, msg *protocol.Message) *protocol.Message {
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

// TTL validation and expiry
err := msg.ValidateTTL()   // checks age + clock skew
expired := msg.IsExpired() // simple bool check

// Serialize / deserialize
bytes, _ := msg.Encode()
msg2, _ := protocol.DecodeMessage(bytes)

// Conversation tracking with FSM + timeouts
store := protocol.NewConversationStore()
conv := store.Create(convID, initiatorDID)
conv.Transition(intentMsg)  // created -> created (INTENT sent)
conv.Transition(proposeMsg) // created -> proposed
conv.Transition(acceptMsg)  // proposed -> accepted

// Message deduplication
store.HasSeen(msgID)   // check if already processed
store.MarkSeen(msgID)  // mark as seen
store.CleanupSeen()    // remove entries older than 5 min

// Conversation timeout management
timedOut := store.TimeoutConversations() // auto-expire stale conversations
```

### Network (`pkg/network`)

```go
// Create a libp2p host with TLS + Noise, connection manager, rate limiter
host, _ := network.New(ctx, network.Config{
    ListenPort: 9000,
    PrivKey:    identity.PrivKey,
})

// Enable mDNS peer discovery (bounded, with exponential backoff)
host.StartDiscovery()

// Register AIP protocol handler (rate-limited, with read deadlines)
host.SetStreamHandler(func(peerID peer.ID, msg *protocol.Message) {
    // Handle incoming AIP messages
})

// Send directly to a peer (length-prefixed binary framing)
host.SendDirect(ctx, targetPeerID, msg)

// Join GossipSub topics for broadcast
topic, _ := host.JoinTopic("aip-capabilities")
```

### Cognition (`pkg/cognition`) — L6 NietzscheDB Integration

```go
// Create a cognitive store backed by NietzscheDB
store, _ := cognition.NewStore(ctx, "localhost:50051", "agent-brain")

// Trust management (Poincare ball — deeper = more trusted)
trust, _ := cognition.NewTrustManager(store)
trust.RecordInteraction(peerDID, outcome, rating)
score := trust.GetTrustScore(peerDID)

// Capability cache
capCache, _ := cognition.NewCapabilityCache(store)
capCache.Store(peerDID, capabilities)
peers := capCache.FindProviders("nlp.translate")

// Episodic memory
memory, _ := cognition.NewMemory(store)
memory.Remember(conversationID, summary, outcome)
relevant := memory.Recall("translation tasks", 5) // top-5 KNN
```

---

## Project Structure

```
Micelio/
+-- cmd/
|   +-- micelio/
|       +-- main.go                 # CLI: keygen, agent, info
+-- pkg/
|   +-- identity/
|   |   +-- did.go                  # L1: DID:key, Ed25519, sign/verify, AES-256-GCM encryption
|   |   +-- did_test.go             # Identity tests (keygen, sign, verify, encrypt/decrypt)
|   +-- protocol/
|   |   +-- types.go                # Message types, states, constants
|   |   +-- message.go              # AIP envelope, payloads, ULID, TTL validation, dedup
|   |   +-- negotiation.go          # FSM, conversation tracking, timeouts, dedup store
|   |   +-- protocol_test.go        # Protocol tests (FSM, transitions, TTL, payloads)
|   +-- network/
|   |   +-- host.go                 # libp2p host, GossipSub, TLS+Noise, connection manager
|   |   +-- discovery.go            # mDNS discovery, bounded peers, exponential backoff
|   |   +-- handler.go              # Stream handler, rate limiting, read deadlines
|   |   +-- ratelimit.go            # Per-peer rate limiter (sliding window)
|   +-- agent/
|   |   +-- agent.go                # Agent abstraction, signature verification, panic recovery
|   |   +-- agent_test.go           # Agent tests (capabilities, handlers, lifecycle)
|   +-- cognition/
|   |   +-- store.go                # NietzscheDB connection (L6)
|   |   +-- trust.go                # Trust scoring (Poincare ball depth)
|   |   +-- memory.go               # Episodic memory (KNN recall)
|   |   +-- capability.go           # Capability cache
|   |   +-- desire.go               # Desire generation
|   +-- logging/
|       +-- logger.go               # Structured logger (DEBUG/INFO/WARN/ERROR)
+-- schemas/
|   +-- message.schema.json         # AIP envelope JSON Schema
|   +-- intent.schema.json          # INTENT payload schema
|   +-- propose.schema.json         # PROPOSE payload schema
|   +-- capability.schema.json      # CAPABILITY_ADVERTISE schema
+-- examples/
|   +-- two_agents/
|       +-- main.go                 # Two-agent P2P demo
+-- .github/
|   +-- workflows/
|       +-- ci.yml                  # GitHub Actions CI (test + build + lint)
+-- media/
|   +-- micelio.mp4                 # Demo video
+-- AIP_Especificacao_Tecnica_v0.2-Micelio.docx
+-- Agent_Internet_Stack_v1.0-Micelio.docx
+-- Makefile                        # build, test, lint, fmt, vet, clean
+-- Dockerfile                      # Multi-stage Alpine build
+-- LICENSE                         # MIT
+-- CONTRIBUTING.md                 # Contribution guidelines
+-- SECURITY.md                     # Security policy & vulnerability reporting
+-- go.mod
+-- go.sum
+-- README.md
```

**Total: ~4,000 lines of Go** — minimal, no bloat, every line has a purpose.

---

## Key Design Decisions

### Why libp2p?

- Battle-tested (IPFS, Filecoin, Ethereum 2.0)
- Transport-agnostic (TCP, QUIC, WebTransport)
- Built-in encryption (TLS 1.3 + Noise protocol)
- mDNS for local discovery, Kademlia DHT for global
- Connection manager for bounded resource usage
- GossipSub for pub/sub broadcast

### Why DID:key (not API keys, not OAuth, not blockchain)?

```
Ed25519 Keypair -> Multicodec prefix (0xed01) -> Base58btc -> did:key:z...
```

- **Self-sovereign**: no registration, no central authority
- **Verifiable offline**: public key is embedded in the DID itself
- **No blockchain**: DID:key method is purely cryptographic
- **Interoperable**: W3C DID standard
- **Encryptable**: AES-256-GCM at-rest protection with PBKDF2

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

| Feature | AIP (Micelio) | MCP (Anthropic) | Fetch.ai | AutoGPT | LangChain |
|---|---|---|---|---|---|
| P2P | libp2p | Client-Server | + blockchain | No | No |
| Negotiation | FSM (8 states) | No | Partial | No | No |
| Identity | DID:key | API keys | Token-based | No | No |
| Discovery | mDNS + DHT | No | Yes | No | No |
| Signatures | Ed25519 + verify | No | Yes | No | No |
| Key Encryption | AES-256-GCM | No | No | No | No |
| Rate Limiting | Per-peer | No | No | No | No |
| Replay Protection | TTL + dedup | No | Partial | No | No |
| Blockchain required | No | No | Yes | No | No |
| Token required | No | No | Yes | No | No |
| Language agnostic | JSON Schema | Partial | No | No | Python |
| Spec-first | Yes | Yes | No | No | No |

---

## Roadmap

- [x] **Phase 0** — Specification (AIP Draft 0.2)
- [x] **Phase 1** — Transport + Identity (libp2p + DID:key + Ed25519)
- [x] **Phase 2** — Messaging Layer (envelope, signatures, TTL, ULID, dedup)
- [x] **Phase 3** — Intent Protocol (negotiation FSM, conversation tracking, timeouts)
- [x] **Phase 3.5** — Security Hardening (signature verification, rate limiting, key encryption, connection management, backoff, panic recovery)
- [ ] **Phase 4** — Capability Discovery (DHT provider records, gossip broadcast)
- [ ] **Phase 5** — Reputation Layer (local-first trust scores, weighted decay)
- [ ] **Phase 6** — SDKs (JavaScript/TypeScript, Python, Rust)
- [ ] **Phase 7** — WebTransport (browser-native agents)
- [ ] **Phase 8** — NietzscheDB Integration (L6 cognition layer — trust, memory, desires)
- [ ] **Phase 9** — NAT Traversal (relay, hole punching for internet-wide agents)

---

## Development

### Prerequisites

- Go 1.22+
- (Optional) `golangci-lint` for linting

### Commands

```bash
make build        # Build binary with version info
make test         # Run all tests with race detector
make test-cover   # Generate HTML coverage report
make lint         # Run golangci-lint
make fmt          # Format all code
make vet          # Run go vet
make clean        # Remove build artifacts
make all          # fmt + vet + test + build
```

### Running Tests

```bash
go test -race -v ./...
```

Tests cover:
- **Identity**: key generation, DID encoding/decoding, signing/verification, encrypted save/load
- **Protocol**: message creation, FSM transitions, invalid transitions, TTL validation, payload types
- **Agent**: capability registration, message handler wiring, lifecycle management

---

## Dependencies

| Package | Purpose |
|---|---|
| `github.com/libp2p/go-libp2p` | P2P networking, TLS + Noise encryption |
| `github.com/libp2p/go-libp2p-pubsub` | GossipSub broadcast |
| `github.com/libp2p/go-libp2p/p2p/net/connmgr` | Connection management (50-200 peers) |
| `github.com/multiformats/go-multiaddr` | Network address abstraction |
| `github.com/oklog/ulid/v2` | Time-sortable unique IDs |
| `golang.org/x/crypto/pbkdf2` | Key derivation for encrypted identity |

Zero external dependencies beyond libp2p ecosystem + Go stdlib extensions. No frameworks. No ORMs. No magic.

---

## Related Documents

| Document | Description |
|---|---|
| `AIP_Especificacao_Tecnica_v0.2-Micelio.docx` | Full AIP specification — all layers, algorithms, schemas |
| `Agent_Internet_Stack_v1.0-Micelio.docx` | Agent Internet Stack design — 8-layer architecture rationale |
| `schemas/` | Machine-readable JSON Schemas for all message types |
| `CONTRIBUTING.md` | How to contribute to Micelio |
| `SECURITY.md` | Security policy and vulnerability reporting |

---

## License

MIT — see [LICENSE](LICENSE)

---

<p align="center">
  <em>"Like mycelium connecting trees in a forest, AIP connects autonomous agents in a decentralized network — sharing resources, negotiating tasks, and building collective intelligence without central control."</em>
</p>

<p align="center">
  <strong>Micelio</strong> — Protocol, not platform.
</p>
