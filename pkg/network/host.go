// Package network implements the P2P transport layer using libp2p.
package network

import (
	"context"
	"fmt"

	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/multiformats/go-multiaddr"

	"github.com/JoseRFJuniorLLMs/Micelio/pkg/protocol"
)

// Host wraps a libp2p host with AIP-specific functionality.
type Host struct {
	P2P      host.Host
	PubSub   *pubsub.PubSub
	Topics   map[string]*pubsub.Topic
	Discovery *mdnsDiscovery
	ctx      context.Context
	cancel   context.CancelFunc
}

// Config holds host configuration.
type Config struct {
	ListenPort int
	PrivKey    crypto.PrivKey
}

// New creates a new AIP network host.
func New(ctx context.Context, cfg Config) (*Host, error) {
	ctx, cancel := context.WithCancel(ctx)

	listenAddr, err := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", cfg.ListenPort))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("parse listen addr: %w", err)
	}

	opts := []libp2p.Option{
		libp2p.ListenAddrs(listenAddr),
		libp2p.Identity(cfg.PrivKey),
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

	node := &Host{
		P2P:    h,
		PubSub: ps,
		Topics: make(map[string]*pubsub.Topic),
		ctx:    ctx,
		cancel: cancel,
	}

	return node, nil
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
	h.P2P.SetStreamHandler(protocol.ProtocolID, newStreamHandler(handler))
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

// Close shuts down the host.
func (h *Host) Close() error {
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
