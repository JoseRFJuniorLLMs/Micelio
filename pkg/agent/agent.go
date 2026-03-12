// Package agent implements the AIP agent abstraction.
// An agent has an identity, connects to the P2P network, and can negotiate with other agents.
// With NietzscheDB cognition (L6), the agent has a hyperbolic brain that remembers,
// trusts, caches capabilities, and generates desires.
package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/JoseRFJuniorLLMs/Micelio/pkg/cognition"
	"github.com/JoseRFJuniorLLMs/Micelio/pkg/identity"
	"github.com/JoseRFJuniorLLMs/Micelio/pkg/logging"
	"github.com/JoseRFJuniorLLMs/Micelio/pkg/network"
	"github.com/JoseRFJuniorLLMs/Micelio/pkg/protocol"
	"github.com/JoseRFJuniorLLMs/Micelio/pkg/reputation"
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
	Identity      *identity.Identity
	Host          *network.Host
	Capabilities  []Capability
	Conversations *protocol.ConversationStore
	Cognition     *cognition.Store          // L6: NietzscheDB hyperbolic brain (nil if no DB configured)
	Reputation    *reputation.TrustStore    // Standalone trust layer (always available)
	Engine        *cognition.CognitionEngine // Background desire->INTENT loop (nil if no DB)

	log            *logging.Logger
	handlers       map[protocol.MessageType]MessageHandler
	convStartTimes map[string]int64 // conversation_id -> start time (ms)
	reputationFile string           // path to persist reputation JSON (empty = in-memory only)
	mu             sync.RWMutex
	ctx            context.Context
}

// Config holds agent configuration.
type Config struct {
	Name       string
	Port       int
	Identity   *identity.Identity
	// NietzscheAddr is the gRPC address for NietzscheDB (e.g. "localhost:50051").
	// If empty, the agent runs without cognition (L6 disabled).
	NietzscheAddr string
	// Collection overrides the auto-generated NietzscheDB collection name.
	Collection string
	// ReputationFile is the path to persist reputation data as JSON.
	// If empty, reputation is only kept in memory for the session.
	ReputationFile string
	// EnableDHT enables Kademlia DHT discovery alongside mDNS for global
	// peer and capability discovery across the internet.
	EnableDHT bool
	// BootstrapPeers is a list of multiaddr strings for DHT bootstrap nodes.
	// If empty and EnableDHT is true, the default libp2p bootstrap peers are used.
	BootstrapPeers []string
	// Capabilities is a list of capability names to advertise on DHT at startup.
	Capabilities []string
}

// New creates and starts a new AIP agent.
func New(ctx context.Context, cfg Config) (*Agent, error) {
	log := logging.New("agent")

	if cfg.Identity == nil {
		id, err := identity.Generate()
		if err != nil {
			return nil, fmt.Errorf("generate identity: %w", err)
		}
		cfg.Identity = id
	}

	h, err := network.New(ctx, network.Config{
		ListenPort:     cfg.Port,
		PrivKey:        cfg.Identity.PrivKey,
		EnableDHT:      cfg.EnableDHT,
		BootstrapPeers: cfg.BootstrapPeers,
	})
	if err != nil {
		return nil, fmt.Errorf("create network host: %w", err)
	}

	// Initialize standalone reputation layer (always available, no DB needed).
	// Try to load persisted data if a file path is configured.
	var rep *reputation.TrustStore
	if cfg.ReputationFile != "" {
		loaded, err := reputation.Load(cfg.ReputationFile)
		if err == nil {
			rep = loaded
		} else {
			rep = reputation.NewTrustStore()
		}
	} else {
		rep = reputation.NewTrustStore()
	}

	a := &Agent{
		Identity:       cfg.Identity,
		Host:           h,
		Capabilities:   make([]Capability, 0),
		Conversations:  protocol.NewConversationStore(),
		Reputation:     rep,
		log:            log,
		handlers:       make(map[protocol.MessageType]MessageHandler),
		convStartTimes: make(map[string]int64),
		reputationFile: cfg.ReputationFile,
		ctx:            ctx,
	}

	// Initialize L6 cognition if NietzscheDB address is provided
	if cfg.NietzscheAddr != "" {
		store, err := cognition.NewStore(ctx, cognition.Config{
			NietzscheAddr: cfg.NietzscheAddr,
			AgentDID:      cfg.Identity.DID,
			Collection:    cfg.Collection,
		})
		if err != nil {
			a.log.Warn("cognition disabled", logging.String("name", cfg.Name), logging.Err(err))
		} else {
			a.Cognition = store
			a.log.Info("L6 cognition active", logging.String("name", cfg.Name), logging.String("collection", store.Collection()))

			// Start cognition engine (desire -> INTENT loop)
			engine := cognition.NewCognitionEngine(store)
			engine.Start()
			a.Engine = engine
			a.log.Info("cognition engine started", logging.String("name", cfg.Name))

			// Consume desires from cognition engine
			go a.consumeDesires()
		}
	}

	// Start conversation timeout reaper
	go a.reapConversations()

	// Register the stream handler
	h.SetStreamHandler(a.handleMessage)

	// Start mDNS discovery
	if err := h.StartDiscovery(); err != nil {
		h.Close()
		return nil, fmt.Errorf("start discovery: %w", err)
	}

	// Start DHT discovery if enabled
	if cfg.EnableDHT {
		if err := h.StartDHTDiscovery(); err != nil {
			a.log.Warn("DHT discovery failed to start", logging.String("did", cfg.Identity.DID[:12]), logging.Err(err))
		} else {
			// Advertise all capabilities on DHT
			for _, cap := range cfg.Capabilities {
				if err := h.AdvertiseCapability(ctx, cap); err != nil {
					a.log.Warn("failed to advertise capability", logging.String("did", cfg.Identity.DID[:12]), logging.String("capability", cap), logging.Err(err))
				}
			}
		}
	}

	return a, nil
}

// RegisterCapability adds a capability this agent can perform.
// If DHT is enabled, the capability is also advertised on the DHT network.
func (a *Agent) RegisterCapability(cap Capability) {
	a.mu.Lock()
	a.Capabilities = append(a.Capabilities, cap)
	a.mu.Unlock()

	// Also advertise on DHT if available
	if a.Host != nil && a.Host.DHT != nil {
		if err := a.Host.AdvertiseCapability(a.ctx, cap.Name); err != nil {
			a.log.Warn("DHT advertise failed", logging.String("capability", cap.Name), logging.Err(err))
		}
	}
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

	// Track start time for latency measurement
	a.mu.Lock()
	a.convStartTimes[convID] = time.Now().UnixMilli()
	a.mu.Unlock()

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

// SendIntentSmart sends an INTENT, first checking NietzscheDB for trusted peers
// with the requested capability. Returns the conversation and the chosen peer.
func (a *Agent) SendIntentSmart(ctx context.Context, intent protocol.IntentPayload) (*protocol.Conversation, peer.ID, error) {
	if a.Cognition == nil {
		return nil, "", fmt.Errorf("cognition not available; use SendIntent with explicit target")
	}

	// Check cached capabilities for trusted peers
	caps, err := a.Cognition.FindPeersWithCapability(ctx, intent.Capability, 0.3, 5)
	if err != nil || len(caps) == 0 {
		return nil, "", fmt.Errorf("no trusted peers found for capability %q", intent.Capability)
	}

	// Use the most trusted peer (list is pre-sorted by trust)
	bestPeer := caps[0]
	targetID, err := peer.Decode(bestPeer.PeerDID)
	if err != nil {
		return nil, "", fmt.Errorf("decode peer DID: %w", err)
	}

	conv, err := a.SendIntent(ctx, targetID, intent)
	return conv, targetID, err
}

// SendPropose sends a PROPOSE in response to an INTENT.
func (a *Agent) SendPropose(ctx context.Context, target peer.ID, convID string, propose protocol.ProposePayload) error {
	return a.sendReply(ctx, target, convID, protocol.TypePropose, propose)
}

// SendCounter sends a counter-proposal in an existing negotiation.
func (a *Agent) SendCounter(ctx context.Context, target peer.ID, convID string, counter protocol.CounterPayload) error {
	return a.sendReply(ctx, target, convID, protocol.TypeCounter, counter)
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
	err := a.sendReply(ctx, target, convID, protocol.TypeReceipt, receipt)
	if err != nil {
		return err
	}

	// Record negotiation in episodic memory (L6)
	a.recordNegotiationMemory(ctx, convID, receipt)

	return nil
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
	a.log.Info("received message",
		logging.String("type", string(msg.Type)),
		logging.String("from", from.String()[:12]),
		logging.String("conv", msg.ConversationID[:12]+"..."),
	)

	// Check trust before processing (L6 cognition + standalone reputation)
	if a.Cognition != nil && a.Cognition.IsBanned(a.ctx, msg.From) {
		a.log.Warn("blocked message from banned peer (cognition)", logging.String("peer", msg.From[:20]))
		return
	}
	if a.Reputation.IsBlocked(msg.From) {
		a.log.Warn("blocked message from banned peer (reputation)", logging.String("peer", msg.From[:20]))
		return
	}

	// Validate message TTL
	if msg.IsExpired() {
		a.log.Warn("rejected expired message",
			logging.String("from", msg.From[:12]),
			logging.String("type", string(msg.Type)),
			logging.Duration("age", time.Since(msg.Timestamp)),
		)
		return
	}

	// Verify message signature (FIX 1)
	if msg.Signature != "" {
		sig, err := base64.StdEncoding.DecodeString(msg.Signature)
		if err != nil {
			a.log.Error("invalid signature encoding", logging.String("from", from.String()[:12]), logging.Err(err))
			return
		}
		signable, err := msg.SignableBytes()
		if err != nil {
			a.log.Error("failed to get signable bytes", logging.Err(err))
			return
		}
		ok, err := identity.VerifyFrom(msg.From, signable, sig)
		if err != nil {
			a.log.Error("signature verification error", logging.String("from", from.String()[:12]), logging.Err(err))
			return
		}
		if !ok {
			a.log.Error("rejected invalid signature", logging.String("from", from.String()[:12]))
			a.Reputation.RecordSignatureFailure(msg.From)
			return
		}
	} else {
		a.log.Error("rejected unsigned message", logging.String("from", from.String()[:12]), logging.String("type", string(msg.Type)))
		a.Reputation.RecordSignatureFailure(msg.From)
		return
	}

	// Check for duplicate messages (FIX 3)
	if a.Conversations.HasSeen(msg.MsgID) {
		a.log.Warn("dropping duplicate message", logging.String("msg_id", msg.MsgID))
		return
	}
	a.Conversations.MarkSeen(msg.MsgID)

	// Track conversation
	conv, exists := a.Conversations.Get(msg.ConversationID)
	if !exists {
		conv = a.Conversations.Create(msg.ConversationID, msg.From)
		// Track start time
		a.mu.Lock()
		a.convStartTimes[msg.ConversationID] = time.Now().UnixMilli()
		a.mu.Unlock()
	}
	if err := conv.Transition(msg); err != nil {
		a.log.Error("conversation transition error",
			logging.String("conv", msg.ConversationID),
			logging.Err(err),
		)
		return
	}

	// Cache peer capability from PROPOSE messages (L6)
	if msg.Type == protocol.TypePropose && a.Cognition != nil {
		a.cacheCapabilityFromPropose(msg)
	}

	// Dispatch to handler
	a.mu.RLock()
	handler, ok := a.handlers[msg.Type]
	a.mu.RUnlock()

	if ok {
		// Panic recovery (FIX 4) — don't let a handler crash the agent
		var reply *protocol.Message
		func() {
			defer func() {
				if r := recover(); r != nil {
					a.log.Error("handler panic", logging.String("type", string(msg.Type)), logging.String("panic", fmt.Sprintf("%v", r)))
				}
			}()
			reply = handler(from, msg)
		}()
		if reply != nil {
			a.signMessage(reply)
			a.Host.SendDirect(a.ctx, from, reply)
		}
	}
}

// recordNegotiationMemory stores the completed negotiation in NietzscheDB
// and updates the standalone reputation layer.
func (a *Agent) recordNegotiationMemory(ctx context.Context, convID string, receipt protocol.ReceiptPayload) {
	conv, ok := a.Conversations.Get(convID)
	if !ok {
		return
	}

	a.mu.RLock()
	startTime := a.convStartTimes[convID]
	a.mu.RUnlock()

	now := time.Now().UnixMilli()

	outcome := "rejected"
	if receipt.Accepted {
		outcome = "success"
	}

	// Extract capability from the conversation's INTENT message
	var capability string
	for _, msg := range conv.Messages {
		if msg.Type == protocol.TypeIntent {
			var intent protocol.IntentPayload
			if json.Unmarshal(msg.Payload, &intent) == nil {
				capability = intent.Capability
			}
			break
		}
	}

	peerDID := conv.Responder
	if peerDID == "" {
		peerDID = conv.Initiator
	}

	// Update NietzscheDB cognition layer if available
	if a.Cognition != nil {
		a.Cognition.RecordNegotiation(ctx, cognition.NegotiationMemory{
			ConversationID: convID,
			PeerDID:        peerDID,
			Capability:     capability,
			Outcome:        outcome,
			Rating:         receipt.Rating,
			DurationMs:     now - startTime,
			StartedAt:      startTime,
			CompletedAt:    now,
		})

		// Update trust score (cognition layer)
		a.Cognition.RecordInteraction(ctx, peerDID, receipt.Accepted, float64(now-startTime))
	}

	// Update standalone reputation layer (always available)
	latencyMs := float64(now - startTime)
	if receipt.Accepted {
		a.Reputation.RecordSuccess(peerDID, latencyMs)
	} else {
		a.Reputation.RecordFailure(peerDID)
	}
}

// cacheCapabilityFromPropose extracts capability info from a PROPOSE and caches it.
func (a *Agent) cacheCapabilityFromPropose(msg *protocol.Message) {
	var propose protocol.ProposePayload
	if json.Unmarshal(msg.Payload, &propose) != nil {
		return
	}

	a.Cognition.CacheCapability(a.ctx, cognition.PeerCapability{
		PeerDID:     msg.From,
		Name:        propose.Capability,
		Description: propose.Approach,
	})
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

// consumeDesires reads desires from the cognition engine channel and converts
// them into INTENT messages sent to peers that advertise the required capability.
func (a *Agent) consumeDesires() {
	for desire := range a.Engine.Desires() {
		if a.Cognition == nil {
			continue
		}

		// Find peers that can fulfill this desire
		peers, err := a.Cognition.FindPeersWithCapability(a.ctx, desire.Capability, 0.3, 5)
		if err != nil || len(peers) == 0 {
			a.log.Warn("desire: no peers found",
				logging.String("desire_id", desire.ID),
				logging.String("capability", desire.Capability),
			)
			continue
		}

		// Use the best peer (list is pre-sorted by trust)
		bestPeer := peers[0]
		targetID, err := peer.Decode(bestPeer.PeerDID)
		if err != nil {
			a.log.Error("desire: failed to decode peer DID",
				logging.String("desire_id", desire.ID),
				logging.String("peer_did", bestPeer.PeerDID),
				logging.Err(err),
			)
			continue
		}

		intent := protocol.IntentPayload{
			Capability:  desire.Capability,
			Description: desire.Description,
		}
		_, err = a.SendIntent(a.ctx, targetID, intent)
		if err != nil {
			a.log.Error("desire: failed to send intent",
				logging.String("desire_id", desire.ID),
				logging.Err(err),
			)
			continue
		}
		a.log.Info("desire: sent intent",
			logging.String("desire_id", desire.ID),
			logging.String("peer", bestPeer.PeerDID),
			logging.String("capability", desire.Capability),
		)
	}
}

// reapConversations periodically checks for and marks timed-out conversations.
func (a *Agent) reapConversations() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			timedOut := a.Conversations.TimeoutConversations()
			if len(timedOut) > 0 {
				a.log.Info("timed out stale conversations", logging.Int("count", len(timedOut)))
			}
		}
	}
}

// Close shuts down the agent, saves reputation data, and stops the cognition engine.
func (a *Agent) Close() error {
	// Stop the cognition engine if running
	if a.Engine != nil {
		a.Engine.Stop()
	}

	if a.Cognition != nil {
		a.Cognition.Close()
	}

	// Save reputation to file if a path was configured.
	// We check by attempting to save; the reputationFile is stored via closure
	// in the agent's config. Since Config is not stored, we use a helper field.
	if a.reputationFile != "" && a.Reputation != nil {
		if err := a.Reputation.Save(a.reputationFile); err != nil {
			a.log.Warn("failed to save reputation", logging.Err(err))
		}
	}

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
		"did":              a.Identity.DID,
		"peer_id":          a.Host.ID().String(),
		"addrs":            addrStrs,
		"capabilities":     a.Capabilities,
		"cognition":        a.Cognition != nil,
		"cognition_engine": a.Engine != nil,
		"reputation":       true,
	}
	if a.Cognition != nil {
		info["collection"] = a.Cognition.Collection()
	}
	data, _ := json.MarshalIndent(info, "", "  ")
	return string(data)
}
