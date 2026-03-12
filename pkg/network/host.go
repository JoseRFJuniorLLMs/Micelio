// Package network implements the P2P transport layer using libp2p.
package network

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	libp2pnet "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	tls "github.com/libp2p/go-libp2p/p2p/security/tls"
	"github.com/multiformats/go-multiaddr"

	"github.com/JoseRFJuniorLLMs/Micelio/pkg/logging"
	"github.com/JoseRFJuniorLLMs/Micelio/pkg/protocol"
)

// hostLog is a package-level logger for host-related functions.
var hostLog = logging.New("host")

// Default connection manager limits.
const (
	DefaultConnLow  = 50
	DefaultConnHigh = 200

	// Default retry settings for SendDirect.
	DefaultMaxRetries    = 3
	DefaultRetryBaseWait = 1 * time.Second
	DefaultRetryMaxWait  = 30 * time.Second

	// Reconnection settings.
	reconnectBaseDelay = 1 * time.Second
	reconnectMaxDelay  = 2 * time.Minute
	reconnectMaxTries  = 10
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

	log    *logging.Logger
	ctx    context.Context
	cancel context.CancelFunc

	// Reconnection tracking.
	peersMu    sync.RWMutex
	knownPeers map[peer.ID]peer.AddrInfo // peers we want to stay connected to

	// Send retry configuration.
	MaxRetries    int
	RetryBaseWait time.Duration
	RetryMaxWait  time.Duration
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

	// ConnLow is the low watermark for the connection manager. Connections
	// will be pruned when the count exceeds ConnHigh, down to ConnLow.
	// Defaults to 50 if zero.
	ConnLow int

	// ConnHigh is the high watermark for the connection manager. When the
	// connection count exceeds this value, pruning begins.
	// Defaults to 200 if zero.
	ConnHigh int

	// MaxSendRetries is the maximum number of retries for SendDirect on
	// failure. Defaults to 3 if zero.
	MaxSendRetries int

	// RetryBaseWait is the initial backoff duration for send retries.
	// Defaults to 1s if zero.
	RetryBaseWait time.Duration

	// RetryMaxWait is the maximum backoff duration for send retries.
	// Defaults to 30s if zero.
	RetryMaxWait time.Duration
}

// Validate checks the network Config for invalid values.
// Port 0 is allowed (OS-assigned random port). Non-zero ports must be in the
// range 1024-65535 to avoid privileged ports. Bootstrap peer strings, if
// provided, must be valid multiaddr format.
func (c Config) Validate() error {
	if c.ListenPort < 0 || c.ListenPort > 65535 {
		return fmt.Errorf("network config: ListenPort must be 0-65535, got %d", c.ListenPort)
	}
	if c.ListenPort > 0 && c.ListenPort < 1024 {
		return fmt.Errorf("network config: ListenPort %d is in the privileged range (1-1023); use 0 or 1024-65535", c.ListenPort)
	}
	for i, addr := range c.BootstrapPeers {
		if _, err := multiaddr.NewMultiaddr(addr); err != nil {
			return fmt.Errorf("network config: BootstrapPeers[%d] %q is not a valid multiaddr: %w", i, addr, err)
		}
	}
	if c.ConnLow < 0 {
		return fmt.Errorf("network config: ConnLow must be >= 0, got %d", c.ConnLow)
	}
	if c.ConnHigh < 0 {
		return fmt.Errorf("network config: ConnHigh must be >= 0, got %d", c.ConnHigh)
	}
	if c.ConnLow > 0 && c.ConnHigh > 0 && c.ConnLow > c.ConnHigh {
		return fmt.Errorf("network config: ConnLow (%d) must be <= ConnHigh (%d)", c.ConnLow, c.ConnHigh)
	}
	return nil
}

// New creates a new AIP network host.
func New(ctx context.Context, cfg Config) (*Host, error) {
	// Validate configuration before proceeding.
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(ctx)

	// Apply defaults for connection manager.
	connLow := cfg.ConnLow
	if connLow <= 0 {
		connLow = DefaultConnLow
	}
	connHigh := cfg.ConnHigh
	if connHigh <= 0 {
		connHigh = DefaultConnHigh
	}

	// Build listen addresses
	listenAddrs, err := buildListenAddrs(cfg)
	if err != nil {
		cancel()
		return nil, err
	}

	// Connection manager with configurable limits
	cm, err := connmgr.NewConnManager(connLow, connHigh, connmgr.WithGracePeriod(time.Minute))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create connection manager: %w", err)
	}

	hostLog.Info("connection manager configured", logging.Int("low", connLow), logging.Int("high", connHigh))

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

	// Apply retry defaults
	maxRetries := cfg.MaxSendRetries
	if maxRetries <= 0 {
		maxRetries = DefaultMaxRetries
	}
	retryBase := cfg.RetryBaseWait
	if retryBase <= 0 {
		retryBase = DefaultRetryBaseWait
	}
	retryMax := cfg.RetryMaxWait
	if retryMax <= 0 {
		retryMax = DefaultRetryMaxWait
	}

	node := &Host{
		P2P:           h,
		PubSub:        ps,
		Topics:        make(map[string]*pubsub.Topic),
		RateLimiter:   rateLimiter,
		log:           hostLog,
		ctx:           ctx,
		cancel:        cancel,
		knownPeers:    make(map[peer.ID]peer.AddrInfo),
		MaxRetries:    maxRetries,
		RetryBaseWait: retryBase,
		RetryMaxWait:  retryMax,
	}

	// Register the connection notifiee for reconnection tracking.
	h.Network().Notify(&connectionNotifiee{host: node})

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

// TrackPeer registers a peer for automatic reconnection. If the connection
// to this peer drops, the host will attempt to reconnect with exponential
// backoff.
func (h *Host) TrackPeer(info peer.AddrInfo) {
	h.peersMu.Lock()
	defer h.peersMu.Unlock()
	h.knownPeers[info.ID] = info
	h.log.Debug("tracking peer for reconnection", logging.String("peer", info.ID.String()[:12]))
}

// UntrackPeer removes a peer from the automatic reconnection list.
func (h *Host) UntrackPeer(id peer.ID) {
	h.peersMu.Lock()
	defer h.peersMu.Unlock()
	delete(h.knownPeers, id)
}

// TrackedPeers returns a copy of all tracked peer infos.
func (h *Host) TrackedPeers() []peer.AddrInfo {
	h.peersMu.RLock()
	defer h.peersMu.RUnlock()
	result := make([]peer.AddrInfo, 0, len(h.knownPeers))
	for _, info := range h.knownPeers {
		result = append(result, info)
	}
	return result
}

// reconnectPeer attempts to reconnect to a peer with exponential backoff.
// Runs as a goroutine; stops when the connection succeeds, max retries are
// exhausted, or the host context is cancelled.
func (h *Host) reconnectPeer(info peer.AddrInfo) {
	for attempt := 0; attempt < reconnectMaxTries; attempt++ {
		// Check if host is shutting down.
		if h.ctx.Err() != nil {
			return
		}

		// Check if peer is still tracked.
		h.peersMu.RLock()
		_, tracked := h.knownPeers[info.ID]
		h.peersMu.RUnlock()
		if !tracked {
			h.log.Debug("peer no longer tracked, stopping reconnect", logging.String("peer", info.ID.String()[:12]))
			return
		}

		// Check if already connected.
		if h.P2P.Network().Connectedness(info.ID) == libp2pnet.Connected {
			h.log.Debug("peer already reconnected", logging.String("peer", info.ID.String()[:12]))
			return
		}

		// Exponential backoff: 1s, 2s, 4s, 8s, ... capped at reconnectMaxDelay.
		delay := time.Duration(float64(reconnectBaseDelay) * math.Pow(2, float64(attempt)))
		if delay > reconnectMaxDelay {
			delay = reconnectMaxDelay
		}

		h.log.Info("reconnecting to peer", logging.String("peer", info.ID.String()[:12]), logging.Int("attempt", attempt+1), logging.Duration("backoff", delay))

		select {
		case <-time.After(delay):
		case <-h.ctx.Done():
			return
		}

		connectCtx, connectCancel := context.WithTimeout(h.ctx, 15*time.Second)
		err := h.P2P.Connect(connectCtx, info)
		connectCancel()

		if err == nil {
			h.log.Info("reconnected to peer", logging.String("peer", info.ID.String()[:12]))
			return
		}
		h.log.Warn("reconnect failed", logging.String("peer", info.ID.String()[:12]), logging.Err(err))
	}

	h.log.Warn("gave up reconnecting to peer", logging.String("peer", info.ID.String()[:12]), logging.Int("attempts", reconnectMaxTries))
}

// connectionNotifiee implements the libp2p network.Notifiee interface to
// track peer connections and trigger reconnections on disconnect.
type connectionNotifiee struct {
	host *Host
}

func (n *connectionNotifiee) Connected(_ libp2pnet.Network, conn libp2pnet.Conn) {
	remotePeer := conn.RemotePeer()
	n.host.log.Info("peer connected", logging.String("peer", remotePeer.String()[:12]))

	// Auto-track newly connected peers so we reconnect if they drop.
	info := peer.AddrInfo{
		ID:    remotePeer,
		Addrs: []multiaddr.Multiaddr{conn.RemoteMultiaddr()},
	}
	n.host.TrackPeer(info)
}

func (n *connectionNotifiee) Disconnected(_ libp2pnet.Network, conn libp2pnet.Conn) {
	remotePeer := conn.RemotePeer()
	n.host.log.Info("peer disconnected", logging.String("peer", remotePeer.String()[:12]))

	// Check if this peer is tracked for reconnection.
	n.host.peersMu.RLock()
	info, tracked := n.host.knownPeers[remotePeer]
	n.host.peersMu.RUnlock()

	if tracked {
		// Update address from the connection that just dropped, in case
		// the stored address is stale.
		updatedInfo := peer.AddrInfo{
			ID:    remotePeer,
			Addrs: append(info.Addrs, conn.RemoteMultiaddr()),
		}
		go n.host.reconnectPeer(updatedInfo)
	}
}

func (n *connectionNotifiee) Listen(libp2pnet.Network, multiaddr.Multiaddr)      {}
func (n *connectionNotifiee) ListenClose(libp2pnet.Network, multiaddr.Multiaddr)  {}

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

		hostLog.Info("QUIC and WebTransport listeners enabled", logging.Int("port", cfg.ListenPort))
	}

	return addrs, nil
}

// buildNATOptions returns the libp2p options for NAT traversal.
func buildNATOptions() []libp2p.Option {
	hostLog.Info("NAT traversal enabled (UPnP, AutoRelay, HolePunching, NATService)")
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
		h.log.Warn("DHT general advertise failed (non-fatal)", logging.Err(err))
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
			h.log.Warn("gossip broadcast failed (non-fatal)", logging.String("capability", name), logging.Err(err))
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
	disc.mdnsSvc = svc
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
	h.P2P.SetStreamHandler(protocol.ProtocolID, newStreamHandler(h.log, h.RateLimiter, handler))
}

// SendDirect sends a message directly to a specific peer via a stream.
// If the initial send fails, it retries with exponential backoff up to
// MaxRetries times.
func (h *Host) SendDirect(ctx context.Context, target peer.ID, msg *protocol.Message) error {
	var lastErr error

	for attempt := 0; attempt <= h.MaxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: base * 2^(attempt-1), capped at max.
			delay := time.Duration(float64(h.RetryBaseWait) * math.Pow(2, float64(attempt-1)))
			if delay > h.RetryMaxWait {
				delay = h.RetryMaxWait
			}
			h.log.Debug("retrying send", logging.String("peer", target.String()[:12]), logging.Int("attempt", attempt+1), logging.Duration("backoff", delay))

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return fmt.Errorf("send cancelled during retry backoff: %w", ctx.Err())
			}
		}

		lastErr = h.sendDirectOnce(ctx, target, msg)
		if lastErr == nil {
			return nil
		}
		h.log.Warn("send failed", logging.String("peer", target.String()[:12]), logging.Int("attempt", attempt+1), logging.Err(lastErr))
	}

	return fmt.Errorf("send to %s failed after %d attempts: %w", target, h.MaxRetries+1, lastErr)
}

// sendDirectOnce performs a single send attempt.
func (h *Host) sendDirectOnce(ctx context.Context, target peer.ID, msg *protocol.Message) error {
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

// Close shuts down the host and all subsystems. It cancels the host context
// first to signal all child goroutines (reconnect loops, discovery, DHT
// advertisement loops, gossip subscriber) to stop, then waits for each
// subsystem to finish its cleanup before closing the underlying libp2p host.
func (h *Host) Close() error {
	// Cancel host context first so all subsystems using h.ctx are signalled.
	h.cancel()

	if h.Gossip != nil {
		h.Gossip.Close()
	}
	if h.DHT != nil {
		h.DHT.Close()
	}
	if h.Discovery != nil {
		h.Discovery.Close()
	}
	return h.P2P.Close()
}

// AddrInfo returns full address info for connecting to this host.
func (h *Host) AddrInfo() peer.AddrInfo {
	return peer.AddrInfo{
		ID:    h.P2P.ID(),
		Addrs: h.P2P.Addrs(),
	}
}
