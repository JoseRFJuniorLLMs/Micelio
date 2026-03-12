// Package network implements the P2P transport layer using libp2p.
//
// gossip.go provides GossipSub-based capability advertisement broadcasting.
// Peers can broadcast their capabilities to the network and subscribe to
// receive capability advertisements from other peers.

package network

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/JoseRFJuniorLLMs/Micelio/pkg/protocol"
)

// capabilitiesTopicName is the GossipSub topic for capability advertisements.
const capabilitiesTopicName = "aip/capabilities/v1"

// CapabilityHandler is called when a capability advertisement is received
// from a peer via GossipSub.
type CapabilityHandler func(from peer.ID, ad protocol.CapabilityAd)

// GossipBroadcaster manages GossipSub-based capability advertisement
// broadcasting. It wraps a pubsub.Topic to provide typed publish/subscribe
// for CapabilityAd messages.
type GossipBroadcaster struct {
	ps           *pubsub.PubSub
	topic        *pubsub.Topic
	sub          *pubsub.Subscription
	selfID       peer.ID
	ctx          context.Context
	cancel       context.CancelFunc

	mu       sync.RWMutex
	handlers []CapabilityHandler
}

// NewGossipBroadcaster creates a new GossipBroadcaster that publishes and
// subscribes to capability advertisements on the "aip/capabilities/v1" topic.
func NewGossipBroadcaster(ctx context.Context, ps *pubsub.PubSub, selfID peer.ID) (*GossipBroadcaster, error) {
	ctx, cancel := context.WithCancel(ctx)

	topic, err := ps.Join(capabilitiesTopicName)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("join capabilities topic: %w", err)
	}

	sub, err := topic.Subscribe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("subscribe to capabilities topic: %w", err)
	}

	gb := &GossipBroadcaster{
		ps:     ps,
		topic:  topic,
		sub:    sub,
		selfID: selfID,
		ctx:    ctx,
		cancel: cancel,
	}

	return gb, nil
}

// BroadcastCapability publishes a capability advertisement to all peers
// subscribed to the capabilities topic. The advertisement is serialized
// as JSON before publishing.
func (g *GossipBroadcaster) BroadcastCapability(ctx context.Context, capAd protocol.CapabilityAd) error {
	data, err := json.Marshal(capAd)
	if err != nil {
		return fmt.Errorf("marshal capability ad: %w", err)
	}

	if err := g.topic.Publish(ctx, data); err != nil {
		return fmt.Errorf("publish capability ad: %w", err)
	}

	fmt.Printf("[gossip] broadcast capability %q v%s\n", capAd.Name, capAd.Version)
	return nil
}

// OnCapability registers a handler that is called whenever a capability
// advertisement is received from another peer. Multiple handlers can be
// registered and all will be called for each received advertisement.
func (g *GossipBroadcaster) OnCapability(handler CapabilityHandler) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.handlers = append(g.handlers, handler)
}

// SubscribeCapabilities starts a background goroutine that listens for
// capability advertisements from other peers and dispatches them to
// registered handlers. This should be called once after registering
// handlers with OnCapability.
func (g *GossipBroadcaster) SubscribeCapabilities(ctx context.Context) {
	go func() {
		for {
			msg, err := g.sub.Next(ctx)
			if err != nil {
				if ctx.Err() != nil || g.ctx.Err() != nil {
					return // context cancelled, clean shutdown
				}
				fmt.Printf("[gossip] error reading capabilities: %v\n", err)
				return
			}

			// Skip messages from self
			if msg.ReceivedFrom == g.selfID {
				continue
			}

			var capAd protocol.CapabilityAd
			if err := json.Unmarshal(msg.Data, &capAd); err != nil {
				fmt.Printf("[gossip] invalid capability ad from %s: %v\n", msg.ReceivedFrom.String()[:12], err)
				continue
			}

			fmt.Printf("[gossip] received capability %q from %s\n", capAd.Name, msg.ReceivedFrom.String()[:12])

			// Dispatch to registered handlers
			g.mu.RLock()
			handlers := make([]CapabilityHandler, len(g.handlers))
			copy(handlers, g.handlers)
			g.mu.RUnlock()

			for _, h := range handlers {
				h(msg.ReceivedFrom, capAd)
			}
		}
	}()
}

// Close shuts down the gossip broadcaster, unsubscribing from the topic.
func (g *GossipBroadcaster) Close() {
	g.cancel()
	g.sub.Cancel()
}
