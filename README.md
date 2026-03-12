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
  <a href="#python-sdk">Python SDK</a> &bull;
  <a href="#roadmap">Roadmap</a>
</p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white" />
  <img alt="Python" src="https://img.shields.io/badge/Python-3.9+-3776AB?logo=python&logoColor=white" />
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
1. **Discover** each other (mDNS local + Kademlia DHT global + GossipSub broadcast)
2. **Identify** themselves (DID:key from Ed25519 — no CA, no blockchain)
3. **Negotiate** tasks (INTENT -> PROPOSE -> ACCEPT -> DELIVER -> RECEIPT)
4. **Verify** every message (Ed25519 signatures with replay protection)
5. **Trust** each other (local-first reputation with weighted temporal decay)
6. **Remember** interactions (NietzscheDB hyperbolic episodic memory)

No servers. No API keys. No central authority. No tokens. No blockchain.

---

## Quick Start

### Prerequisites

- **Go 1.22+** (Go SDK)
- **Python 3.9+** (Python SDK, optional)

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

The agent starts listening for peers via mDNS + DHT and is ready to receive AIP messages.

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
|      |  Cognition Engine (desire -> INTENT autonomous loop)     |
+------+----------------------------------------------------------+
|  L5  |  AIP Protocol  <-- THIS PROJECT                          |
|      |  - Intent Protocol (negotiation state machine)           |
|      |  - Capability Discovery (DHT + GossipSub)                |
|      |  - Reputation Layer (local-first trust scores)           |
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
|      |  libp2p - TCP, QUIC, WebTransport, TLS + Noise,          |
|      |  NAT traversal (UPnP, relay, hole punching),             |
|      |  connection manager, rate limiting                        |
+------+----------------------------------------------------------+
```

### Internet Analogy

| Human Internet | Agent Internet (AIP) |
|---|---|
| TCP/IP | libp2p (TCP, QUIC, WebTransport, Noise) |
| DNS | Kademlia DHT + mDNS + GossipSub |
| HTTP | **AIP** |
| SSL Certificates | DID:key (Ed25519) |
| Web of Trust | Reputation Layer (temporal decay) |
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
| Signature bypass | Mandatory verification on receive + reputation ban | Implemented |
| Identity spoofing | DID:key derived from public key — unforgeable | Implemented |
| Man-in-the-middle | TLS + Noise protocol at transport layer | Implemented |
| Key theft | AES-256-GCM encrypted key files with passphrase | Implemented |
| Spam / DoS | Per-peer rate limiter (100 msg/s default) | Implemented |
| Peer flooding | Connection manager (50-200) + max 200 discovered | Implemented |
| Reconnect storms | Exponential backoff (2^n sec, max 5min) | Implemented |
| Handler crashes | Panic recovery in message handlers | Implemented |
| Counter loop | Max counter rounds per conversation (default: 10) | Implemented |
| Stale conversations | Configurable timeout with auto-expiry | Implemented |
| Sybil attacks | Local-first reputation with temporal decay | Implemented |
| NAT barriers | UPnP port mapping + relay + hole punching | Implemented |

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

// Persist and load (plaintext or encrypted)
id.Save("agent.json")
id.SaveEncrypted("agent.enc.json", "passphrase")
id2, _ := identity.Load("agent.json")
id3, _ := identity.LoadEncrypted("agent.enc.json", "passphrase")

// Verify from DID string (no private key needed)
ok, _ = identity.VerifyFrom(id.DID, data, sig)
```

### Agent (`pkg/agent`)

```go
// Create an agent with reputation + optional NietzscheDB cognition
agent, _ := agent.New(ctx, agent.Config{
    Name:           "MyAgent",
    Port:           9000,
    NietzscheAddr:  "localhost:50051", // optional: enables L6 cognition
    ReputationFile: "rep.json",        // optional: persists trust scores
})

// Register capabilities
agent.RegisterCapability(agent.Capability{
    Name:        "code.review",
    Description: "Review Go code for bugs and style issues",
    Version:     "1.0.0",
})

// Handle incoming messages (auto signature verification + dedup + reputation)
agent.OnMessage(protocol.TypeIntent, func(from peer.ID, msg *protocol.Message) *protocol.Message {
    reply, _ := protocol.NewMessage(protocol.TypePropose, agent.DID(), msg.From, msg.ConversationID, propose)
    return reply
})

// Smart negotiation: auto-select trusted peer from NietzscheDB
conv, targetPeer, _ := agent.SendIntentSmart(ctx, protocol.IntentPayload{
    Capability:  "nlp.translate",
    Description: "Translate this text",
})
```

### Network (`pkg/network`) — DHT + NAT + WebTransport

```go
// Create host with DHT, WebTransport, and NAT traversal
host, _ := network.New(ctx, network.Config{
    ListenPort:        9000,
    PrivKey:           identity.PrivKey,
    EnableDHT:         true,  // Kademlia DHT for global discovery
    EnableWebTransport: true, // QUIC + WebTransport for browser agents
    EnableNATTraversal: true, // UPnP + relay + hole punching
})

// Start all discovery methods
host.StartDiscovery()        // mDNS (local)
host.StartDHTDiscovery()     // Kademlia DHT (global) + GossipSub

// Advertise capabilities via DHT + GossipSub
host.AdvertiseCapability(ctx, "nlp.translate")

// Find peers with a capability via DHT
peers, _ := host.FindCapabilityProviders(ctx, "nlp.translate", 10)
```

### Reputation (`pkg/reputation`) — Standalone Trust Layer

```go
// Create trust store (no database needed)
store := reputation.NewTrustStore()

// Record interactions
store.RecordSuccess("did:key:z6Mk...", 150.0) // peer delivered in 150ms
store.RecordFailure("did:key:z6Mk...")         // peer failed
store.RecordSignatureFailure("did:key:z6Mk...") // immediate ban (score=0)

// Query trust
score := store.GetScore("did:key:z6Mk...")     // 0.0-1.0, 0.5 for unknown
trusted := store.GetTrustedPeers(0.6)          // peers with score >= 0.6
banned := store.IsBlocked("did:key:z6Mk...")

// Peer selection (trust-weighted)
selector := reputation.NewPeerSelector(store)
best := selector.SelectBest(candidates, 0.3)           // sorted by trust
chosen, _ := selector.WeightedRandom(candidates, 0.3)  // probabilistic

// Persistence
store.Save("reputation.json")
loaded, _ := reputation.Load("reputation.json")
```

### Cognition (`pkg/cognition`) — L6 NietzscheDB Integration

```go
// Create cognitive store backed by NietzscheDB (Poincare ball)
store, _ := cognition.NewStore(ctx, cognition.Config{
    NietzscheAddr: "localhost:50051",
    AgentDID:      "did:key:z6Mk...",
})

// Trust management (magnitude = depth in Poincare ball)
store.RecordInteraction(ctx, peerDID, true, 150.0)
score := store.GetTrustScore(ctx, peerDID)
trusted, _ := store.GetTrustedPeers(ctx, 0.6, 10)

// Episodic memory (negotiation history)
store.RecordNegotiation(ctx, memory)
history, _ := store.GetNegotiationHistory(ctx, peerDID, 10)

// Capability cache (avoid redundant DHT lookups)
store.CacheCapability(ctx, cap)
providers, _ := store.FindPeersWithCapability(ctx, "nlp.translate", 0.3, 5)

// Desire engine (knowledge gaps -> AIP INTENTs)
engine := cognition.NewCognitionEngine(store)
engine.Start() // polls desires every 30s, purges expired caps every 60s
for desire := range engine.Desires() {
    // Convert desire to AIP INTENT and send to network
}
```

---

## Python SDK

A Python SDK is available in `sdks/python/` for building Python-based AIP agents:

```bash
cd sdks/python
pip install -e .
```

```python
from micelio import Identity, Message, MessageType, Conversation, MicelioClient

# Generate identity (wire-compatible with Go agents)
identity = Identity.generate()
print(identity.did)  # did:key:z6Mk...

# Sign and verify
sig = identity.sign(b"hello")
assert identity.verify(b"hello", sig)

# Create AIP messages
msg = Message.new(
    msg_type=MessageType.INTENT,
    from_did=identity.did,
    to_did="did:key:z6Mk...",
    conversation_id=Conversation.new_id(),
    payload={"capability": "nlp.translate", "description": "Translate text"},
)

# Connect to a Go agent
client = MicelioClient()
client.connect("localhost", 9000)
client.send_message(msg)
response = client.receive_message()
```

The Python SDK implements the full AIP protocol: identity (DID:key), message envelope, negotiation FSM, TCP client, and mDNS discovery.

---

## Project Structure

```
Micelio/
+-- cmd/micelio/main.go                  # CLI: keygen, agent, info
+-- pkg/
|   +-- identity/
|   |   +-- did.go                       # L1: DID:key, Ed25519, AES-256-GCM encryption
|   |   +-- did_test.go                  # Identity tests
|   +-- protocol/
|   |   +-- types.go                     # Message types, states, constants
|   |   +-- message.go                   # AIP envelope, payloads, ULID, TTL
|   |   +-- negotiation.go              # FSM, conversation tracking, timeouts
|   |   +-- protocol_test.go            # Protocol tests
|   +-- network/
|   |   +-- host.go                      # libp2p host, TLS+Noise, connmgr, NAT, WebTransport
|   |   +-- dht.go                       # Kademlia DHT discovery + capability provider records
|   |   +-- gossip.go                    # GossipSub capability broadcast
|   |   +-- discovery.go                 # mDNS discovery, bounded peers, backoff
|   |   +-- handler.go                   # Stream handler, rate limiting, read deadlines
|   |   +-- ratelimit.go                 # Per-peer rate limiter
|   +-- agent/
|   |   +-- agent.go                     # Agent abstraction, sig verification, reputation
|   |   +-- agent_test.go               # Agent tests
|   +-- reputation/
|   |   +-- trust.go                     # Standalone trust scoring (no DB needed)
|   |   +-- persistence.go              # JSON file persistence
|   |   +-- selector.go                 # Trust-weighted peer selection
|   |   +-- reputation_test.go          # Reputation tests
|   +-- cognition/
|   |   +-- store.go                     # NietzscheDB connection (L6)
|   |   +-- trust.go                     # Trust scoring (Poincare ball depth)
|   |   +-- memory.go                    # Episodic memory (negotiation history)
|   |   +-- capability.go               # Capability cache with TTL
|   |   +-- desire.go                    # Desire generation (knowledge gaps)
|   |   +-- engine.go                    # Cognition engine (desire -> INTENT loop)
|   +-- logging/
|       +-- logger.go                    # Structured logger (DEBUG/INFO/WARN/ERROR)
+-- sdks/python/                         # Python SDK
|   +-- micelio/                         # identity, protocol, negotiation, client, discovery
|   +-- tests/                           # Python test suite
|   +-- examples/basic_agent.py          # Python agent example
+-- schemas/                             # JSON Schemas for all message types
+-- examples/two_agents/main.go          # Two-agent P2P demo
+-- .github/workflows/ci.yml             # GitHub Actions CI
+-- Makefile                             # build, test, lint, fmt, vet, clean
+-- Dockerfile                           # Multi-stage Alpine build
+-- LICENSE | CONTRIBUTING.md | SECURITY.md
```

**Total: ~5,500 lines of Go + ~2,000 lines of Python**

---

## Key Design Decisions

### Why libp2p?

- Battle-tested (IPFS, Filecoin, Ethereum 2.0)
- Transport-agnostic (TCP, QUIC, WebTransport)
- Built-in encryption (TLS 1.3 + Noise protocol)
- mDNS for local discovery, Kademlia DHT for global
- NAT traversal (UPnP, relay, hole punching)
- Connection manager for bounded resource usage
- GossipSub for pub/sub broadcast

### Why DID:key?

```
Ed25519 Keypair -> Multicodec prefix (0xed01) -> Base58btc -> did:key:z...
```

- **Self-sovereign**: no registration, no central authority
- **Verifiable offline**: public key is embedded in the DID itself
- **No blockchain**: DID:key method is purely cryptographic
- **Interoperable**: W3C DID standard
- **Encryptable**: AES-256-GCM at-rest protection with PBKDF2

### Wire Format

Messages are transmitted as **length-prefixed binary frames**:

```
[4 bytes: payload length (big-endian uint32)] [N bytes: JSON payload]
```

Max message size: 1 MB. Same framing in Go and Python SDKs.

---

## Comparison with Existing Systems

| Feature | AIP (Micelio) | MCP (Anthropic) | Fetch.ai | AutoGPT | LangChain |
|---|---|---|---|---|---|
| P2P | libp2p | Client-Server | + blockchain | No | No |
| Negotiation | FSM (9 states) | No | Partial | No | No |
| Identity | DID:key | API keys | Token-based | No | No |
| Global Discovery | Kademlia DHT | No | Yes | No | No |
| Local Discovery | mDNS | No | No | No | No |
| Capability Broadcast | GossipSub | No | Partial | No | No |
| Signatures | Ed25519 + verify | No | Yes | No | No |
| Key Encryption | AES-256-GCM | No | No | No | No |
| Reputation | Trust + decay | No | Partial | No | No |
| Rate Limiting | Per-peer | No | No | No | No |
| Replay Protection | TTL + dedup | No | Partial | No | No |
| NAT Traversal | UPnP + relay + DCUtR | No | Yes | No | No |
| WebTransport | QUIC + WT | No | No | No | No |
| Graph Memory | NietzscheDB | No | No | No | No |
| Python SDK | Yes | Yes | Yes | No | Yes |
| Blockchain required | No | No | Yes | No | No |
| Token required | No | No | Yes | No | No |
| Spec-first | Yes | Yes | No | No | No |

---

## Roadmap

- [x] **Phase 0** — Specification (AIP Draft 0.2)
- [x] **Phase 1** — Transport + Identity (libp2p + DID:key + Ed25519)
- [x] **Phase 2** — Messaging Layer (envelope, signatures, TTL, ULID, dedup)
- [x] **Phase 3** — Intent Protocol (negotiation FSM, conversation tracking, timeouts)
- [x] **Phase 3.5** — Security Hardening (signature verification, rate limiting, key encryption, connection management, backoff, panic recovery)
- [x] **Phase 4** — Capability Discovery (Kademlia DHT provider records + GossipSub broadcast)
- [x] **Phase 5** — Reputation Layer (standalone trust scores, weighted temporal decay, peer selection)
- [x] **Phase 6** — Python SDK (identity, protocol, FSM, TCP client, mDNS discovery)
- [x] **Phase 7** — WebTransport + QUIC (browser-native agents via libp2p)
- [x] **Phase 8** — NietzscheDB Cognition (trust, episodic memory, capability cache, desire engine)
- [x] **Phase 9** — NAT Traversal (UPnP port mapping, circuit relay, hole punching via DCUtR)
- [ ] **Phase 10** — JavaScript/TypeScript SDK
- [ ] **Phase 11** — Rust SDK
- [ ] **Phase 12** — Formal RFC specification

---

## Development

### Prerequisites

- Go 1.22+
- Python 3.9+ (for Python SDK)
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
# Go
go test -race -v ./...

# Python
cd sdks/python && pip install -e ".[dev]" && pytest tests/ -v
```

Tests cover:
- **Identity**: key generation, DID encoding/decoding, signing/verification, encrypted save/load
- **Protocol**: message creation, FSM transitions, invalid transitions, TTL validation, payload types
- **Agent**: capability registration, message handler wiring, lifecycle management
- **Reputation**: trust scoring, temporal decay, signature ban, persistence, peer selection

---

## Dependencies

### Go

| Package | Purpose |
|---|---|
| `github.com/libp2p/go-libp2p` | P2P networking, TLS + Noise, QUIC, WebTransport |
| `github.com/libp2p/go-libp2p-kad-dht` | Kademlia DHT for global discovery |
| `github.com/libp2p/go-libp2p-pubsub` | GossipSub broadcast |
| `github.com/multiformats/go-multiaddr` | Network address abstraction |
| `github.com/oklog/ulid/v2` | Time-sortable unique IDs |
| `golang.org/x/crypto/pbkdf2` | Key derivation for encrypted identity |

### Python

| Package | Purpose |
|---|---|
| `cryptography` | Ed25519 signing, key management |
| `python-ulid` | ULID generation |
| `zeroconf` (optional) | mDNS discovery |

Zero external dependencies beyond libp2p ecosystem. No frameworks. No ORMs. No magic.

---

## Related Documents

| Document | Description |
|---|---|
| `AIP_Especificacao_Tecnica_v0.2-Micelio.docx` | Full AIP specification |
| `Agent_Internet_Stack_v1.0-Micelio.docx` | 8-layer architecture rationale |
| `schemas/` | Machine-readable JSON Schemas for all message types |
| `sdks/python/README.md` | Python SDK documentation |
| `CONTRIBUTING.md` | How to contribute |
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
