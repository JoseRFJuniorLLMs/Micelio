package network

import (
	"context"
	"fmt"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

// mdnsDiscovery handles mDNS peer discovery events.
type mdnsDiscovery struct {
	host  host.Host
	peers []peer.AddrInfo
	ctx   context.Context
}

// HandlePeerFound is called when a new peer is discovered via mDNS.
func (d *mdnsDiscovery) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == d.host.ID() {
		return // skip self
	}
	fmt.Printf("[discovery] found peer: %s\n", pi.ID.String()[:12])
	d.peers = append(d.peers, pi)

	// Auto-connect to discovered peers
	if err := d.host.Connect(d.ctx, pi); err != nil {
		fmt.Printf("[discovery] connect to %s failed: %v\n", pi.ID.String()[:12], err)
	} else {
		fmt.Printf("[discovery] connected to peer: %s\n", pi.ID.String()[:12])
	}
}

// Peers returns all discovered peers.
func (d *mdnsDiscovery) Peers() []peer.AddrInfo {
	return d.peers
}
