// Package network implements the P2P transport layer using libp2p.
//
// dht.go provides Kademlia DHT-based peer and capability discovery,
// complementing the existing mDNS local discovery.

package network

import (
	"context"
	"fmt"
	"sync"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/multiformats/go-multiaddr"
)

// capabilityNamespace returns the DHT namespace for a given capability name.
// All capability namespaces are prefixed with "aip/cap/" to avoid collisions.
func capabilityNamespace(name string) string {
	return "aip/cap/" + name
}

// DHTDiscovery manages Kademlia DHT-based peer discovery and capability
// advertisement. It uses DHT provider records to let peers advertise which
// capabilities they offer, and allows other peers to find providers of
// specific capabilities.
type DHTDiscovery struct {
	dht       *dht.IpfsDHT
	routingD  *drouting.RoutingDiscovery
	host      host.Host
	ctx       context.Context
	cancel    context.CancelFunc

	mu               sync.RWMutex
	advertisedCaps   map[string]context.CancelFunc // capability name -> cancel for re-advertise loop
	bootstrapPeers   []peer.AddrInfo
}

// NewDHTDiscovery creates a new DHTDiscovery instance. The DHT is created in
// ModeAuto, which automatically switches between client and server mode based
// on the node's reachability. If bootstrapPeers is empty, the default libp2p
// bootstrap peers are used.
func NewDHTDiscovery(ctx context.Context, h host.Host, bootstrapPeers []peer.AddrInfo) (*DHTDiscovery, error) {
	ctx, cancel := context.WithCancel(ctx)

	// Use default bootstrap peers if none provided
	if len(bootstrapPeers) == 0 {
		bootstrapPeers = dht.GetDefaultBootstrapPeerAddrInfos()
	}

	// Create DHT in auto mode (client when behind NAT, server when public)
	kademlia, err := dht.New(ctx, h, dht.Mode(dht.ModeAuto))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create kademlia DHT: %w", err)
	}

	routingDiscovery := drouting.NewRoutingDiscovery(kademlia)

	return &DHTDiscovery{
		dht:            kademlia,
		routingD:       routingDiscovery,
		host:           h,
		ctx:            ctx,
		cancel:         cancel,
		advertisedCaps: make(map[string]context.CancelFunc),
		bootstrapPeers: bootstrapPeers,
	}, nil
}

// Bootstrap connects to the bootstrap peers and initializes the DHT routing
// table. This should be called once after creating the DHTDiscovery. It
// connects to bootstrap peers in parallel and then triggers a DHT bootstrap
// to populate the routing table.
func (d *DHTDiscovery) Bootstrap() error {
	// Connect to bootstrap peers in parallel
	var wg sync.WaitGroup
	connected := 0
	var mu sync.Mutex

	for _, pi := range d.bootstrapPeers {
		if pi.ID == d.host.ID() {
			continue // skip self
		}
		wg.Add(1)
		go func(pi peer.AddrInfo) {
			defer wg.Done()
			connectCtx, connectCancel := context.WithTimeout(d.ctx, 15*time.Second)
			defer connectCancel()

			if err := d.host.Connect(connectCtx, pi); err != nil {
				fmt.Printf("[dht] bootstrap connect to %s failed: %v\n", pi.ID.String()[:12], err)
				return
			}
			mu.Lock()
			connected++
			mu.Unlock()
			fmt.Printf("[dht] connected to bootstrap peer: %s\n", pi.ID.String()[:12])
		}(pi)
	}
	wg.Wait()

	fmt.Printf("[dht] connected to %d/%d bootstrap peers\n", connected, len(d.bootstrapPeers))

	// Bootstrap the DHT routing table
	if err := d.dht.Bootstrap(d.ctx); err != nil {
		return fmt.Errorf("bootstrap DHT: %w", err)
	}

	fmt.Println("[dht] DHT bootstrap complete")
	return nil
}

// AdvertiseCapability registers this peer as a provider of the given
// capability in the DHT. It starts a background goroutine that periodically
// re-advertises the capability (the DHT TTL is typically 3 hours). Calling
// this multiple times for the same capability is safe and idempotent.
func (d *DHTDiscovery) AdvertiseCapability(ctx context.Context, capabilityName string) error {
	ns := capabilityNamespace(capabilityName)

	d.mu.Lock()
	// If already advertising this capability, do nothing
	if _, exists := d.advertisedCaps[capabilityName]; exists {
		d.mu.Unlock()
		return nil
	}

	// Create a cancelable context for the re-advertise loop
	advCtx, advCancel := context.WithCancel(d.ctx)
	d.advertisedCaps[capabilityName] = advCancel
	d.mu.Unlock()

	// Initial advertisement
	ttl, err := d.routingD.Advertise(ctx, ns)
	if err != nil {
		d.mu.Lock()
		delete(d.advertisedCaps, capabilityName)
		d.mu.Unlock()
		advCancel()
		return fmt.Errorf("advertise capability %q: %w", capabilityName, err)
	}

	fmt.Printf("[dht] advertising capability %q (re-advertise every %v)\n", capabilityName, ttl)

	// Background re-advertise loop
	go func() {
		ticker := time.NewTicker(ttl)
		defer ticker.Stop()
		for {
			select {
			case <-advCtx.Done():
				return
			case <-ticker.C:
				readvCtx, readvCancel := context.WithTimeout(advCtx, 60*time.Second)
				if _, err := d.routingD.Advertise(readvCtx, ns); err != nil {
					fmt.Printf("[dht] re-advertise capability %q failed: %v\n", capabilityName, err)
				}
				readvCancel()
			}
		}
	}()

	return nil
}

// StopAdvertisingCapability stops advertising a capability in the DHT.
func (d *DHTDiscovery) StopAdvertisingCapability(capabilityName string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if cancel, exists := d.advertisedCaps[capabilityName]; exists {
		cancel()
		delete(d.advertisedCaps, capabilityName)
		fmt.Printf("[dht] stopped advertising capability %q\n", capabilityName)
	}
}

// FindCapabilityProviders discovers peers that provide the given capability.
// It queries the DHT for provider records and returns up to limit results.
// The returned AddrInfo structs contain the peer IDs and multiaddresses of
// the providers.
func (d *DHTDiscovery) FindCapabilityProviders(ctx context.Context, capabilityName string, limit int) ([]peer.AddrInfo, error) {
	ns := capabilityNamespace(capabilityName)

	fmt.Printf("[dht] searching for providers of capability %q (limit %d)\n", capabilityName, limit)

	peerCh, err := d.routingD.FindPeers(ctx, ns, discovery.Limit(limit))
	if err != nil {
		return nil, fmt.Errorf("find providers for %q: %w", capabilityName, err)
	}

	var providers []peer.AddrInfo
	for pi := range peerCh {
		if pi.ID == d.host.ID() {
			continue // skip self
		}
		providers = append(providers, pi)
	}

	fmt.Printf("[dht] found %d providers for capability %q\n", len(providers), capabilityName)
	return providers, nil
}

// FindPeers discovers peers in the DHT network. This is a general-purpose
// peer discovery that finds any peers in the "aip-network" namespace.
func (d *DHTDiscovery) FindPeers(ctx context.Context, limit int) ([]peer.AddrInfo, error) {
	peerCh, err := d.routingD.FindPeers(ctx, "aip-network", discovery.Limit(limit))
	if err != nil {
		return nil, fmt.Errorf("find peers: %w", err)
	}

	var peers []peer.AddrInfo
	for pi := range peerCh {
		if pi.ID == d.host.ID() {
			continue
		}
		peers = append(peers, pi)
	}

	return peers, nil
}

// Advertise registers this peer in the general "aip-network" namespace so
// that other AIP peers can discover it via FindPeers.
func (d *DHTDiscovery) Advertise(ctx context.Context) error {
	_, err := d.routingD.Advertise(ctx, "aip-network")
	if err != nil {
		return fmt.Errorf("advertise on DHT: %w", err)
	}
	return nil
}

// Close shuts down the DHT discovery, stopping all advertisement loops.
func (d *DHTDiscovery) Close() error {
	d.mu.Lock()
	for name, cancel := range d.advertisedCaps {
		cancel()
		delete(d.advertisedCaps, name)
	}
	d.mu.Unlock()

	d.cancel()
	return d.dht.Close()
}

// parseBootstrapPeers converts multiaddr strings to peer.AddrInfo structs.
// Invalid addresses are logged and skipped.
func parseBootstrapPeers(addrs []string) []peer.AddrInfo {
	var peers []peer.AddrInfo
	for _, addr := range addrs {
		ma, err := multiaddr.NewMultiaddr(addr)
		if err != nil {
			fmt.Printf("[dht] invalid bootstrap addr %q: %v\n", addr, err)
			continue
		}
		pi, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			fmt.Printf("[dht] invalid bootstrap peer %q: %v\n", addr, err)
			continue
		}
		peers = append(peers, *pi)
	}
	return peers
}
