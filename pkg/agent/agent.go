// Package agent implements the AIP agent abstraction.
// An agent has an identity, connects to the P2P network, and can negotiate with other agents.
package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/JoseRFJuniorLLMs/Micelio/pkg/identity"
	"github.com/JoseRFJuniorLLMs/Micelio/pkg/network"
	"github.com/JoseRFJuniorLLMs/Micelio/pkg/protocol"
)

// Capability represents something this agent can do.
type Capability struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
}

// MessageHandler is called when the agent receives a message.
type MessageHandler func(from peer.ID, msg *protocol.Message) *protocol.Message

// Agent is an autonomous AIP agent with identity, network, and negotiation capabilities.
type Agent struct {
	Identity     *identity.Identity
	Host         *network.Host
	Capabilities []Capability
	Conversations *protocol.ConversationStore

	handlers map[protocol.MessageType]MessageHandler
	mu       sync.RWMutex
	ctx      context.Context
}

// Config holds agent configuration.
type Config struct {
	Name       string
	Port       int
	Identity   *identity.Identity
}

// New creates and starts a new AIP agent.
func New(ctx context.Context, cfg Config) (*Agent, error) {
	if cfg.Identity == nil {
		id, err := identity.Generate()
		if err != nil {
			return nil, fmt.Errorf("generate identity: %w", err)
		}
		cfg.Identity = id
	}

	h, err := network.New(ctx, network.Config{
		ListenPort: cfg.Port,
		PrivKey:    cfg.Identity.PrivKey,
	})
	if err != nil {
		return nil, fmt.Errorf("create network host: %w", err)
	}

	a := &Agent{
		Identity:      cfg.Identity,
		Host:          h,
		Capabilities:  make([]Capability, 0),
		Conversations: protocol.NewConversationStore(),
		handlers:      make(map[protocol.MessageType]MessageHandler),
		ctx:           ctx,
	}

	// Register the stream handler
	h.SetStreamHandler(a.handleMessage)

	// Start mDNS discovery
	if err := h.StartDiscovery(); err != nil {
		h.Close()
		return nil, fmt.Errorf("start discovery: %w", err)
	}

	return a, nil
}

// RegisterCapability adds a capability this agent can perform.
func (a *Agent) RegisterCapability(cap Capability) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Capabilities = append(a.Capabilities, cap)
}

// OnMessage registers a handler for a specific message type.
func (a *Agent) OnMessage(msgType protocol.MessageType, handler MessageHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.handlers[msgType] = handler
}

// SendIntent starts a new negotiation by sending an INTENT to a peer.
func (a *Agent) SendIntent(ctx context.Context, target peer.ID, intent protocol.IntentPayload) (*protocol.Conversation, error) {
	convID := protocol.NewConversationID()
	conv := a.Conversations.Create(convID, a.Identity.DID)

	msg, err := protocol.NewMessage(
		protocol.TypeIntent,
		a.Identity.DID,
		target.String(),
		convID,
		intent,
	)
	if err != nil {
		return nil, fmt.Errorf("create intent message: %w", err)
	}

	// Sign the message
	if err := a.signMessage(msg); err != nil {
		return nil, err
	}

	// Record in conversation
	if err := conv.Transition(msg); err != nil {
		return nil, fmt.Errorf("transition: %w", err)
	}

	// Send via direct stream
	if err := a.Host.SendDirect(ctx, target, msg); err != nil {
		return nil, fmt.Errorf("send intent: %w", err)
	}

	return conv, nil
}

// SendPropose sends a PROPOSE in response to an INTENT.
func (a *Agent) SendPropose(ctx context.Context, target peer.ID, convID string, propose protocol.ProposePayload) error {
	return a.sendReply(ctx, target, convID, protocol.TypePropose, propose)
}

// SendAccept sends an ACCEPT in response to a PROPOSE.
func (a *Agent) SendAccept(ctx context.Context, target peer.ID, convID string) error {
	return a.sendReply(ctx, target, convID, protocol.TypeAccept, nil)
}

// SendDeliver sends a DELIVER with the task result.
func (a *Agent) SendDeliver(ctx context.Context, target peer.ID, convID string, deliver protocol.DeliverPayload) error {
	return a.sendReply(ctx, target, convID, protocol.TypeDeliver, deliver)
}

// SendReceipt sends a RECEIPT acknowledging delivery.
func (a *Agent) SendReceipt(ctx context.Context, target peer.ID, convID string, receipt protocol.ReceiptPayload) error {
	return a.sendReply(ctx, target, convID, protocol.TypeReceipt, receipt)
}

func (a *Agent) sendReply(ctx context.Context, target peer.ID, convID string, msgType protocol.MessageType, payload any) error {
	msg, err := protocol.NewMessage(msgType, a.Identity.DID, target.String(), convID, payload)
	if err != nil {
		return fmt.Errorf("create %s message: %w", msgType, err)
	}

	if err := a.signMessage(msg); err != nil {
		return err
	}

	conv, ok := a.Conversations.Get(convID)
	if ok {
		if err := conv.Transition(msg); err != nil {
			return fmt.Errorf("transition: %w", err)
		}
	}

	return a.Host.SendDirect(ctx, target, msg)
}

func (a *Agent) handleMessage(from peer.ID, msg *protocol.Message) {
	fmt.Printf("[%s] received %s from %s (conv: %s)\n",
		a.Identity.DID[:20]+"...",
		msg.Type,
		from.String()[:12],
		msg.ConversationID[:12]+"...",
	)

	// Track conversation
	conv, exists := a.Conversations.Get(msg.ConversationID)
	if !exists {
		conv = a.Conversations.Create(msg.ConversationID, msg.From)
	}
	conv.Transition(msg)

	// Dispatch to handler
	a.mu.RLock()
	handler, ok := a.handlers[msg.Type]
	a.mu.RUnlock()

	if ok {
		reply := handler(from, msg)
		if reply != nil {
			a.signMessage(reply)
			a.Host.SendDirect(a.ctx, from, reply)
		}
	}
}

func (a *Agent) signMessage(msg *protocol.Message) error {
	signable, err := msg.SignableBytes()
	if err != nil {
		return fmt.Errorf("signable bytes: %w", err)
	}
	sig, err := a.Identity.Sign(signable)
	if err != nil {
		return fmt.Errorf("sign message: %w", err)
	}
	msg.Signature = base64.StdEncoding.EncodeToString(sig)
	return nil
}

// DID returns the agent's decentralized identifier.
func (a *Agent) DID() string {
	return a.Identity.DID
}

// PeerID returns the agent's libp2p peer ID.
func (a *Agent) PeerID() peer.ID {
	return a.Host.ID()
}

// Close shuts down the agent.
func (a *Agent) Close() error {
	return a.Host.Close()
}

// Info returns a printable summary of the agent.
func (a *Agent) Info() string {
	addrs := a.Host.Addrs()
	addrStrs := make([]string, len(addrs))
	for i, addr := range addrs {
		addrStrs[i] = addr.String()
	}
	info := map[string]any{
		"did":          a.Identity.DID,
		"peer_id":      a.Host.ID().String(),
		"addrs":        addrStrs,
		"capabilities": a.Capabilities,
	}
	data, _ := json.MarshalIndent(info, "", "  ")
	return string(data)
}
