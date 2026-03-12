// Package network implements the P2P transport layer using libp2p.
package network

import (
	"context"
	"fmt"
	"time"

	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	tls "github.com/libp2p/go-libp2p/p2p/security/tls"
	"github.com/multiformats/go-multiaddr"

	"github.com/JoseRFJuniorLLMs/Micelio/pkg/protocol"
)

// Host wraps a libp2p host with AIP-specific functionality.
type Host struct {
	P2P         host.Host
	PubSub      *pubsub.PubSub
	Topics      map[string]*pubsub.Topic
	Discovery   *mdnsDiscovery
	RateLimiter *PeerRateLimiter

	// DHT provides Kademlia DHT-based peer and capability discovery.
	// It is nil when EnableDHT is false in the Config.
	DHT *DHTDiscovery

	// Gossip provides GossipSub-based capability advertisement broadcasting.
	// It is nil when EnableDHT is false in the Config (gossip depends on DHT
	// for full capability discovery).
	Gossip *GossipBroadcaster

	ctx    context.Context
	cancel context.CancelFunc
}

// Config holds host configuration.
type Config struct {
	ListenPort int
	PrivKey    crypto.PrivKey

	// EnableDHT enables Kademlia DHT discovery alongside mDNS. When enabled,
	// the host can discover peers and advertise capabilities across the
	// internet, not just the local network.
	EnableDHT bool

	// BootstrapPeers is a list of multiaddr strings for DHT bootstrap nodes.
	// If empty and EnableDHT is true, the default libp2p bootstrap peers are
	// used.
	BootstrapPeers []string

	// EnableWebTransport enables QUIC transport in addition to TCP. This
	// allows peers using QUIC-based transports (including WebTransport from
	// browsers) to connect. The QUIC listener uses the same port number as
	// TCP but over UDP.
	EnableWebTransport bool

	// EnableNATTraversal enables NAT traversal features including UPnP port
	// mapping, AutoRelay for circuit relay, hole punching for direct
	// connections through NATs, and NAT service to help other peers determine
	// their NAT status.
	EnableNATTraversal bool
}

// New creates a new AIP network host.
func New(ctx context.Context, cfg Config) (*Host, error) {
	ctx, cancel := context.WithCancel(ctx)

	// Build listen addresses
	listenAddrs, err := buildListenAddrs(cfg)
	if err != nil {
		cancel()
		return nil, err
	}

	// Connection manager: maintain 50-200 connections with 1 minute grace period
	cm, err := connmgr.NewConnManager(50, 200, connmgr.WithGracePeriod(time.Minute))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create connection manager: %w", err)
	}

	opts := []libp2p.Option{
		libp2p.ListenAddrs(listenAddrs...),
		libp2p.Identity(cfg.PrivKey),
		libp2p.ConnectionManager(cm),
		libp2p.Security(tls.ID, tls.New),
		libp2p.Security(noise.ID, noise.New),
	}

	// Phase 9: NAT Traversal options
	if cfg.EnableNATTraversal {
		natOpts := buildNATOptions()
		opts = append(opts, natOpts...)
	}

	h, err := libp2p.New(opts...)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create libp2p host: %w", err)
	}

	// Create GossipSub pubsub
	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		h.Close()
		cancel()
		return nil, fmt.Errorf("create gossipsub: %w", err)
	}

	// Rate limiter: max 100 messages per second per peer
	rateLimiter := NewPeerRateLimiter(100)

	node := &Host{
		P2P:         h,
		PubSub:      ps,
		Topics:      make(map[string]*pubsub.Topic),
		RateLimiter: rateLimiter,
		ctx:         ctx,
		cancel:      cancel,
	}

	// Phase 4: Initialize DHT and Gossip if enabled
	if cfg.EnableDHT {
		if err := node.initDHT(cfg); err != nil {
			h.Close()
			cancel()
			return nil, fmt.Errorf("init DHT: %w", err)
		}
		if err := node.initGossip(); err != nil {
			h.Close()
			cancel()
			return nil, fmt.Errorf("init gossip: %w", err)
		}
	}

	return node, nil
}

// buildListenAddrs constructs the multiaddr listen addresses based on config.
// Always includes TCP; adds QUIC when EnableWebTransport is true.
func buildListenAddrs(cfg Config) ([]multiaddr.Multiaddr, error) {
	tcpAddr, err := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", cfg.ListenPort))
	if err != nil {
		return nil, fmt.Errorf("parse TCP listen addr: %w", err)
	}

	addrs := []multiaddr.Multiaddr{tcpAddr}

	// Phase 7: Add QUIC transport for WebTransport/browser agent support
	if cfg.EnableWebTransport {
		quicAddr, err := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/udp/%d/quic-v1", cfg.ListenPort))
		if err != nil {
			return nil, fmt.Errorf("parse QUIC listen addr: %w", err)
		}
		addrs = append(addrs, quicAddr)

		// Also add WebTransport address for browser agents
		wtAddr, err := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/udp/%d/quic-v1/webtransport", cfg.ListenPort))
		if err != nil {
			return nil, fmt.Errorf("parse WebTransport listen addr: %w", err)
		}
		addrs = append(addrs, wtAddr)

		fmt.Printf("[host] QUIC and WebTransport listeners enabled on UDP port %d\n", cfg.ListenPort)
	}

	return addrs, nil
}

// buildNATOptions returns the libp2p options for NAT traversal.
func buildNATOptions() []libp2p.Option {
	fmt.Println("[host] NAT traversal enabled (UPnP, AutoRelay, HolePunching, NATService)")
	return []libp2p.Option{
		// UPnP port mapping: automatically opens ports on the router
		libp2p.NATPortMap(),

		// AutoRelay: use circuit relay nodes to make this peer reachable
		// when behind NAT. Uses DHT to find relay nodes automatically.
		libp2p.EnableAutoRelay(),

		// Hole punching: attempt direct connections through NATs using
		// the DCUtR protocol
		libp2p.EnableHolePunching(),

		// NAT service: help other peers determine their NAT status by
		// responding to AutoNAT dial-back requests
		libp2p.EnableNATService(),
	}
}

// initDHT creates and bootstraps the Kademlia DHT.
func (h *Host) initDHT(cfg Config) error {
	bootstrapPeers := parseBootstrapPeers(cfg.BootstrapPeers)

	dhtDisc, err := NewDHTDiscovery(h.ctx, h.P2P, bootstrapPeers)
	if err != nil {
		return err
	}
	h.DHT = dhtDisc
	return nil
}

// initGossip creates the GossipBroadcaster for capability advertisements.
func (h *Host) initGossip() error {
	gossip, err := NewGossipBroadcaster(h.ctx, h.PubSub, h.P2P.ID())
	if err != nil {
		return err
	}
	h.Gossip = gossip
	return nil
}

// StartDHTDiscovery bootstraps the DHT and begins advertising this peer
// on the network. Returns an error if DHT is not enabled. This should be
// called after New() to start the DHT bootstrap process.
func (h *Host) StartDHTDiscovery() error {
	if h.DHT == nil {
		return fmt.Errorf("DHT not enabled; set EnableDHT in Config")
	}

	if err := h.DHT.Bootstrap(); err != nil {
		return fmt.Errorf("DHT bootstrap: %w", err)
	}

	// Advertise this peer in the general AIP namespace
	if err := h.DHT.Advertise(h.ctx); err != nil {
		fmt.Printf("[host] DHT general advertise failed (non-fatal): %v\n", err)
	}

	// Start listening for gossip capability advertisements
	if h.Gossip != nil {
		h.Gossip.SubscribeCapabilities(h.ctx)
	}

	return nil
}

// AdvertiseCapability registers this peer as a provider of the named
// capability. The capability is advertised both via DHT provider records
// and via GossipSub broadcast. Returns an error if DHT is not enabled.
func (h *Host) AdvertiseCapability(ctx context.Context, name string) error {
	if h.DHT == nil {
		return fmt.Errorf("DHT not enabled; set EnableDHT in Config")
	}

	// Advertise via DHT provider records
	if err := h.DHT.AdvertiseCapability(ctx, name); err != nil {
		return fmt.Errorf("DHT advertise capability: %w", err)
	}

	// Also broadcast via GossipSub if available
	if h.Gossip != nil {
		capAd := protocol.CapabilityAd{
			Name:    name,
			Version: protocol.Version,
		}
		if err := h.Gossip.BroadcastCapability(ctx, capAd); err != nil {
			fmt.Printf("[host] gossip broadcast for %q failed (non-fatal): %v\n", name, err)
		}
	}

	return nil
}

// FindCapabilityProviders discovers peers providing the named capability
// via DHT lookup. Returns up to limit results. Returns an error if DHT
// is not enabled.
func (h *Host) FindCapabilityProviders(ctx context.Context, name string, limit int) ([]peer.AddrInfo, error) {
	if h.DHT == nil {
		return nil, fmt.Errorf("DHT not enabled; set EnableDHT in Config")
	}

	return h.DHT.FindCapabilityProviders(ctx, name, limit)
}

// StartDiscovery enables mDNS peer discovery on the local network.
func (h *Host) StartDiscovery() error {
	disc := &mdnsDiscovery{host: h.P2P, ctx: h.ctx}
	svc := mdns.NewMdnsService(h.P2P, "aip-network", disc)
	if err := svc.Start(); err != nil {
		return fmt.Errorf("start mDNS: %w", err)
	}
	h.Discovery = disc
	return nil
}

// JoinTopic joins a GossipSub topic and returns it.
func (h *Host) JoinTopic(name string) (*pubsub.Topic, error) {
	if t, ok := h.Topics[name]; ok {
		return t, nil
	}
	topic, err := h.PubSub.Join(name)
	if err != nil {
		return nil, fmt.Errorf("join topic %s: %w", name, err)
	}
	h.Topics[name] = topic
	return topic, nil
}

// SetStreamHandler registers the AIP protocol stream handler.
func (h *Host) SetStreamHandler(handler func(peer.ID, *protocol.Message)) {
	h.P2P.SetStreamHandler(protocol.ProtocolID, newStreamHandler(h.RateLimiter, handler))
}

// SendDirect sends a message directly to a specific peer via a stream.
func (h *Host) SendDirect(ctx context.Context, target peer.ID, msg *protocol.Message) error {
	s, err := h.P2P.NewStream(ctx, target, protocol.ProtocolID)
	if err != nil {
		return fmt.Errorf("open stream to %s: %w", target, err)
	}
	defer s.Close()

	data, err := msg.Encode()
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}

	// Write length-prefixed message
	length := uint32(len(data))
	header := []byte{byte(length >> 24), byte(length >> 16), byte(length >> 8), byte(length)}
	if _, err := s.Write(header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if _, err := s.Write(data); err != nil {
		return fmt.Errorf("write payload: %w", err)
	}

	return nil
}

// Addrs returns the host's multiaddresses.
func (h *Host) Addrs() []multiaddr.Multiaddr {
	return h.P2P.Addrs()
}

// ID returns the host's peer ID.
func (h *Host) ID() peer.ID {
	return h.P2P.ID()
}

// Close shuts down the host and all subsystems.
func (h *Host) Close() error {
	if h.Gossip != nil {
		h.Gossip.Close()
	}
	if h.DHT != nil {
		h.DHT.Close()
	}
	h.cancel()
	return h.P2P.Close()
}

// AddrInfo returns full address info for connecting to this host.
func (h *Host) AddrInfo() peer.AddrInfo {
	return peer.AddrInfo{
		ID:    h.P2P.ID(),
		Addrs: h.P2P.Addrs(),
	}
}
