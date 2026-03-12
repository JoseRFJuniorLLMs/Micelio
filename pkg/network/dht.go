// Package network implements the P2P transport layer using libp2p.
//
// dht.go provides Kademlia DHT-based peer and capability discovery,
// complementing the existing mDNS local discovery.

package network

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/discovery"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/multiformats/go-multiaddr"

	"github.com/JoseRFJuniorLLMs/Micelio/pkg/logging"
)

// dhtLog is a package-level logger for DHT discovery.
var dhtLog = logging.New("dht")

// DHT re-bootstrap interval. The routing table can become stale if peers
// leave the network, so we periodically re-bootstrap.
const (
	dhtReBootstrapInterval = 5 * time.Minute
	dhtBootstrapTimeout    = 15 * time.Second
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

	wg               sync.WaitGroup
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
// to populate the routing table. A background goroutine periodically
// re-bootstraps to keep the routing table fresh.
func (d *DHTDiscovery) Bootstrap() error {
	connected := d.connectBootstrapPeers()

	dhtLog.Info("bootstrap peers connected", logging.Int("connected", int(connected)), logging.Int("total", len(d.bootstrapPeers)))

	if connected == 0 {
		return fmt.Errorf("dht: bootstrap failed, could not connect to any peer")
	}

	// Bootstrap the DHT routing table
	if err := d.dht.Bootstrap(d.ctx); err != nil {
		return fmt.Errorf("bootstrap DHT: %w", err)
	}

	dhtLog.Info("DHT bootstrap complete")

	// Start periodic re-bootstrap in the background.
	d.wg.Add(1)
	go d.reBootstrapLoop()

	return nil
}

// connectBootstrapPeers connects to all bootstrap peers in parallel and
// returns the number of successful connections.
func (d *DHTDiscovery) connectBootstrapPeers() int64 {
	var wg sync.WaitGroup
	var connected int64

	for _, pi := range d.bootstrapPeers {
		if pi.ID == d.host.ID() {
			continue // skip self
		}
		wg.Add(1)
		go func(pi peer.AddrInfo) {
			defer wg.Done()
			connectCtx, connectCancel := context.WithTimeout(d.ctx, dhtBootstrapTimeout)
			defer connectCancel()

			if err := d.host.Connect(connectCtx, pi); err != nil {
				dhtLog.Debug("bootstrap connect failed", logging.String("peer", pi.ID.String()[:12]), logging.Err(err))
				return
			}
			atomic.AddInt64(&connected, 1)
			dhtLog.Debug("connected to bootstrap peer", logging.String("peer", pi.ID.String()[:12]))
		}(pi)
	}
	wg.Wait()

	return atomic.LoadInt64(&connected)
}

// reBootstrapLoop periodically re-connects to bootstrap peers and refreshes
// the DHT routing table. This keeps the routing table healthy when peers
// churn or network conditions change.
func (d *DHTDiscovery) reBootstrapLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(dhtReBootstrapInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			dhtLog.Debug("periodic re-bootstrap starting")

			connected := d.connectBootstrapPeers()
			dhtLog.Debug("re-bootstrap peers connected", logging.Int("connected", int(connected)), logging.Int("total", len(d.bootstrapPeers)))

			if connected > 0 {
				if err := d.dht.Bootstrap(d.ctx); err != nil {
					dhtLog.Warn("re-bootstrap DHT table refresh failed", logging.Err(err))
				} else {
					dhtLog.Debug("re-bootstrap complete")
				}
			} else {
				dhtLog.Warn("re-bootstrap: no bootstrap peers reachable, will retry next cycle")
			}
		}
	}
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

	dhtLog.Info("advertising capability", logging.String("capability", capabilityName), logging.Duration("ttl", ttl))

	// Background re-advertise loop
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		ticker := time.NewTicker(ttl)
		defer ticker.Stop()
		for {
			select {
			case <-advCtx.Done():
				return
			case <-ticker.C:
				readvCtx, readvCancel := context.WithTimeout(advCtx, 60*time.Second)
				if _, err := d.routingD.Advertise(readvCtx, ns); err != nil {
					dhtLog.Warn("re-advertise capability failed", logging.String("capability", capabilityName), logging.Err(err))
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
		dhtLog.Info("stopped advertising capability", logging.String("capability", capabilityName))
	}
}

// FindCapabilityProviders discovers peers that provide the given capability.
// It queries the DHT for provider records and returns up to limit results.
// The returned AddrInfo structs contain the peer IDs and multiaddresses of
// the providers.
func (d *DHTDiscovery) FindCapabilityProviders(ctx context.Context, capabilityName string, limit int) ([]peer.AddrInfo, error) {
	ns := capabilityNamespace(capabilityName)

	dhtLog.Debug("searching for capability providers", logging.String("capability", capabilityName), logging.Int("limit", limit))

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

	dhtLog.Debug("found capability providers", logging.Int("count", len(providers)), logging.String("capability", capabilityName))
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
	d.wg.Wait()
	return d.dht.Close()
}

// parseBootstrapPeers converts multiaddr strings to peer.AddrInfo structs.
// Invalid addresses are logged and skipped.
func parseBootstrapPeers(addrs []string) []peer.AddrInfo {
	var peers []peer.AddrInfo
	for _, addr := range addrs {
		ma, err := multiaddr.NewMultiaddr(addr)
		if err != nil {
			dhtLog.Warn("invalid bootstrap addr", logging.String("addr", addr), logging.Err(err))
			continue
		}
		pi, err := peer.AddrInfoFromP2pAddr(ma)
		if err != nil {
			dhtLog.Warn("invalid bootstrap peer", logging.String("addr", addr), logging.Err(err))
			continue
		}
		peers = append(peers, *pi)
	}
	return peers
}
