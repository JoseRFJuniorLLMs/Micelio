# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 0.x     | Yes (current dev)  |

## Reporting a Vulnerability

If you discover a security vulnerability in Micelio, please report it responsibly.

**Do NOT open a public GitHub issue for security vulnerabilities.**

Instead, send an email to the maintainer with the following information:

- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

We will acknowledge receipt within 48 hours and provide an initial assessment within 7 days.

## Security Model

Micelio uses the following cryptographic primitives:

- **Identity**: Ed25519 key pairs for agent identity (`did:key` method)
- **Signatures**: All AIP messages are signed with the sender's Ed25519 private key
- **Transport**: libp2p with Noise protocol for encrypted peer-to-peer communication
- **Key Storage**: Private key seeds are stored as base64 in JSON files with 0600 permissions

## Known Limitations

- **No key rotation**: Agent identities are permanent. There is no built-in key rotation mechanism yet.
- **No revocation**: There is no DID revocation or deactivation mechanism.
- **Trust is local**: Trust scores are stored per-agent and are not shared across the network. A malicious agent could present different behaviors to different peers.
- **mDNS discovery**: Local network discovery via mDNS is unauthenticated. Agents should verify identities through message signatures before trusting peers.
- **No replay protection at transport level**: Message deduplication is handled in-memory and does not persist across restarts. Replay attacks are possible in a narrow window after an agent restart.
- **TTL clock skew**: Message TTL validation allows up to 30 seconds of clock skew. Hosts with significantly drifted clocks may accept expired messages or reject valid ones.

## Best Practices

- Keep identity files (`identity.json`) protected with filesystem permissions
- Do not share private key seeds
- Run agents behind firewalls when possible; expose only necessary libp2p ports
- Monitor agent logs for signature verification failures, which may indicate tampering attempts
