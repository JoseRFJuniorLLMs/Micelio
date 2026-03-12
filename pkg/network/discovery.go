package network

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
)

const maxDiscoveredPeers = 200

// mdnsDiscovery handles mDNS peer discovery events.
type mdnsDiscovery struct {
	host    host.Host
	peers   []peer.AddrInfo
	mu      sync.RWMutex
	ctx     context.Context
	backoff map[peer.ID]*backoffState
}

// backoffState tracks exponential backoff for failed peer connections.
type backoffState struct {
	failures int
	nextTry  time.Time
}

// HandlePeerFound is called when a new peer is discovered via mDNS.
func (d *mdnsDiscovery) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == d.host.ID() {
		return // skip self
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if we already know this peer
	for _, existing := range d.peers {
		if existing.ID == pi.ID {
			return // already known
		}
	}

	// Don't add more peers if at the limit
	if len(d.peers) >= maxDiscoveredPeers {
		fmt.Printf("[discovery] max peers reached (%d), ignoring %s\n", maxDiscoveredPeers, pi.ID.String()[:12])
		return
	}

	fmt.Printf("[discovery] found peer: %s\n", pi.ID.String()[:12])

	// Check backoff before connecting
	if d.backoff == nil {
		d.backoff = make(map[peer.ID]*backoffState)
	}

	if bs, ok := d.backoff[pi.ID]; ok {
		if time.Now().Before(bs.nextTry) {
			fmt.Printf("[discovery] peer %s in backoff until %s\n", pi.ID.String()[:12], bs.nextTry.Format(time.RFC3339))
			return
		}
	}

	// Auto-connect to discovered peers
	if err := d.host.Connect(d.ctx, pi); err != nil {
		fmt.Printf("[discovery] connect to %s failed: %v\n", pi.ID.String()[:12], err)
		d.applyBackoff(pi.ID)
		return
	}

	fmt.Printf("[discovery] connected to peer: %s\n", pi.ID.String()[:12])
	d.peers = append(d.peers, pi)

	// Clear backoff on successful connection
	delete(d.backoff, pi.ID)
}

// applyBackoff increases the backoff duration for a peer after a failed connection.
// Must be called with d.mu held.
func (d *mdnsDiscovery) applyBackoff(id peer.ID) {
	if d.backoff == nil {
		d.backoff = make(map[peer.ID]*backoffState)
	}

	bs, ok := d.backoff[id]
	if !ok {
		bs = &backoffState{}
		d.backoff[id] = bs
	}

	bs.failures++
	// Exponential backoff: 2^failures seconds, capped at 5 minutes
	delaySec := math.Min(math.Pow(2, float64(bs.failures)), 300)
	bs.nextTry = time.Now().Add(time.Duration(delaySec) * time.Second)
}

// RemovePeer removes a peer from the discovered peers list.
func (d *mdnsDiscovery) RemovePeer(id peer.ID) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for i, p := range d.peers {
		if p.ID == id {
			d.peers = append(d.peers[:i], d.peers[i+1:]...)
			return
		}
	}
}

// Peers returns all discovered peers.
func (d *mdnsDiscovery) Peers() []peer.AddrInfo {
	d.mu.RLock()
	defer d.mu.RUnlock()

	result := make([]peer.AddrInfo, len(d.peers))
	copy(result, d.peers)
	return result
}
