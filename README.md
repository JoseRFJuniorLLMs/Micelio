# Micélio 🍄

**The Mycelium Network for Autonomous Agents**

Micélio implements the **AIP (Agent Internet Protocol)** — an open, peer-to-peer protocol for autonomous agent communication, discovery, and negotiation. No servers. No API keys. No central authority.

> *AIP is to agents what HTTP was to documents: a universal language that enables interoperability between systems built by different teams in different languages.*

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│  L7  │  Applications (your agents)                      │
├──────┼──────────────────────────────────────────────────┤
│  L6  │  Cognition Layer (NietzscheDB, LLMs, etc.)      │
├──────┼──────────────────────────────────────────────────┤
│  L5  │  AIP Protocol  ◄── this project                 │
│      │  • Intent Protocol (negotiation state machine)   │
│      │  • Capability Discovery                          │
│      │  • Reputation Signaling                          │
├──────┼──────────────────────────────────────────────────┤
│  L3  │  AIP Message Format (ULID, Ed25519 signatures)  │
├──────┼──────────────────────────────────────────────────┤
│  L2  │  Discovery (mDNS + Kademlia DHT + Gossipsub)    │
├──────┼──────────────────────────────────────────────────┤
│  L1  │  Identity (DID:key from Ed25519)                │
├──────┼──────────────────────────────────────────────────┤
│  L0  │  Transport (libp2p — TCP, QUIC, WebTransport)   │
└──────┴──────────────────────────────────────────────────┘
```

## The Negotiation Flow

```
Agent A                          Agent B
   │                                │
   │──── INTENT ───────────────────►│  "I need text translated"
   │                                │
   │◄─── PROPOSE ──────────────────│  "I can do it, here's my approach"
   │                                │
   │──── ACCEPT ───────────────────►│  "Go ahead"
   │                                │
   │◄─── DELIVER ──────────────────│  "Here's the result"
   │                                │
   │──── RECEIPT ──────────────────►│  "Got it, rating: 5/5"
   │                                │
```

## Quick Start

### Prerequisites

- Go 1.22+

### Run the Demo

Two agents discover each other, negotiate a translation task, and complete it — all peer-to-peer:

```bash
go run ./examples/two_agents/
```

### Generate an Identity

```bash
go run ./cmd/micelio/ keygen --output my-agent.json
```

### Start an Agent Node

```bash
go run ./cmd/micelio/ agent --port 9000 --identity my-agent.json
```

## Project Structure

```
Micelio/
├── cmd/micelio/           # CLI entry point
├── pkg/
│   ├── identity/          # L1 — DID:key from Ed25519
│   ├── protocol/          # L3/L5 — Message format + negotiation FSM
│   ├── network/           # L0/L2 — libp2p host, mDNS, stream handler
│   └── agent/             # Agent abstraction (ties everything together)
├── schemas/               # JSON Schema for all AIP message types
├── examples/
│   └── two_agents/        # The "absurdly simple" demo
└── docs/                  # Specification documents
```

## Key Design Decisions

### Why P2P (libp2p)?

- No single point of failure
- No vendor lock-in
- Agents own their identity (DID:key, not API keys)
- Works on local network (mDNS), internet (DHT), or both

### Why ULID instead of UUID?

- Lexicographically sortable by creation time
- Efficient range queries in databases
- Same entropy as UUIDv4 (128 bits)

### Why DID:key?

- Self-contained identity (no registry, no CA, no blockchain)
- Derived from Ed25519 keypair
- Verifiable without network access

### Why AIP only lives at L5?

The fatal mistake of Fetch.ai and SingularityNET was mixing protocol with economics, blockchain, and marketplace. AIP is **just a protocol** — what agents say to each other. Storage (L6), payments, and business logic (L7) are separate concerns.

## AIP Message Format

Every AIP message follows the same envelope:

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
  "payload": { "..." : "..." },
  "signature": "base64-ed25519-signature"
}
```

## Analogies

| Human Internet | Agent Internet (AIP) |
|---------------|----------------------|
| TCP/IP        | libp2p               |
| DNS           | Kademlia DHT         |
| HTTP          | **AIP**              |
| Web Apps      | Autonomous Agents    |

## Roadmap

- [x] **Phase 0** — Specification (AIP Draft 0.2)
- [x] **Phase 1** — Transport + Identity (libp2p + DID:key)
- [x] **Phase 2** — Messaging Layer (AIP envelope, signatures, TTL)
- [x] **Phase 3** — Intent Protocol (negotiation state machine)
- [ ] **Phase 4** — Capability Discovery (DHT advertisement + query)
- [ ] **Phase 5** — Reputation Layer (local-first trust scores)
- [ ] **Phase 6** — SDKs (JavaScript, Python, Rust)

## Related Documents

- `AIP_Especificacao_Tecnica_v0.2-Micelio.docx` — Full AIP specification
- `Agent_Internet_Stack_v1.0-Micelio.docx` — Agent Internet Stack design

## License

MIT

---

*"Like mycelium connecting trees in a forest, AIP connects autonomous agents in a decentralized network — sharing resources, negotiating tasks, and building collective intelligence without central control."*
