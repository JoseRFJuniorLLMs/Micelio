package cognition

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	nietzsche "nietzsche-sdk"

	"github.com/JoseRFJuniorLLMs/Micelio/pkg/logging"
)

// memoryLog is a package-level logger for negotiation memory operations.
var memoryLog = logging.New("cognition")

// NegotiationMemory is an episodic record of a completed AIP negotiation.
// Stored as Episodic nodes in the Poincare ball.
type NegotiationMemory struct {
	ConversationID string         `json:"conversation_id"`
	PeerDID        string         `json:"peer_did"`
	Capability     string         `json:"capability"`
	Outcome        string         `json:"outcome"` // "success" | "rejected" | "cancelled" | "timeout"
	Rating         int            `json:"rating"`   // 1-5, 0 if no receipt
	DurationMs     int64          `json:"duration_ms"`
	StartedAt      int64          `json:"started_at"`
	CompletedAt    int64          `json:"completed_at"`
	IntentPayload  map[string]any `json:"intent_payload,omitempty"`
	DeliverResult  map[string]any `json:"deliver_result,omitempty"`
	NodeID         string         `json:"node_id,omitempty"`
}

// RecordNegotiation stores a completed negotiation as an episodic memory.
// Creates an Episodic node + TEMPORAL_NEXT edge to the previous negotiation.
func (s *Store) RecordNegotiation(ctx context.Context, mem NegotiationMemory) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if mem.CompletedAt == 0 {
		mem.CompletedAt = time.Now().UnixMilli()
	}

	content := map[string]interface{}{
		"node_label":       "negotiation",
		"conversation_id":  mem.ConversationID,
		"peer_did":         mem.PeerDID,
		"capability":       mem.Capability,
		"outcome":          mem.Outcome,
		"rating":           mem.Rating,
		"duration_ms":      mem.DurationMs,
		"started_at":       mem.StartedAt,
		"completed_at":     mem.CompletedAt,
	}

	if mem.IntentPayload != nil {
		if b, err := json.Marshal(mem.IntentPayload); err == nil {
			content["intent_payload"] = string(b)
		}
	}
	if mem.DeliverResult != nil {
		if b, err := json.Marshal(mem.DeliverResult); err == nil {
			content["deliver_result"] = string(b)
		}
	}

	// Energy based on outcome: success=0.8, rejected=0.3, others=0.5
	var energy float32 = 0.5
	switch mem.Outcome {
	case "success":
		energy = 0.8
	case "rejected":
		energy = 0.3
	case "cancelled", "timeout":
		energy = 0.4
	}

	result, err := s.client.InsertNode(ctx, nietzsche.InsertNodeOpts{
		Coords:     zeroCoords(),
		Content:    content,
		NodeType:   "Episodic",
		Energy:     energy,
		Collection: s.collection,
	})
	if err != nil {
		return "", fmt.Errorf("cognition: record negotiation: %w", err)
	}

	// Link to previous negotiation with same peer (TEMPORAL_NEXT)
	s.linkToPreviousNegotiation(ctx, result.ID, mem.PeerDID)

	return result.ID, nil
}

// GetNegotiationHistory returns past negotiations with a specific peer.
func (s *Store) GetNegotiationHistory(ctx context.Context, peerDID string, limit int) ([]NegotiationMemory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nql := fmt.Sprintf(
		`MATCH (n:Episodic) WHERE n.node_label = "negotiation" AND n.peer_did = "%s" ORDER BY n.energy DESC LIMIT %d RETURN n`,
		escapeNQL(peerDID), limit,
	)

	result, err := s.client.Query(ctx, nql, nil, s.collection)
	if err != nil {
		return nil, fmt.Errorf("cognition: query negotiation history: %w", err)
	}

	var memories []NegotiationMemory
	for _, node := range result.Nodes {
		if !node.Found {
			continue
		}
		memories = append(memories, nodeToNegotiationMemory(node))
	}

	return memories, nil
}

// GetRecentNegotiations returns the most recent negotiations regardless of peer.
func (s *Store) GetRecentNegotiations(ctx context.Context, limit int) ([]NegotiationMemory, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nql := fmt.Sprintf(
		`MATCH (n:Episodic) WHERE n.node_label = "negotiation" ORDER BY n.energy DESC LIMIT %d RETURN n`,
		limit,
	)

	result, err := s.client.Query(ctx, nql, nil, s.collection)
	if err != nil {
		return nil, fmt.Errorf("cognition: query recent negotiations: %w", err)
	}

	var memories []NegotiationMemory
	for _, node := range result.Nodes {
		if !node.Found {
			continue
		}
		memories = append(memories, nodeToNegotiationMemory(node))
	}

	return memories, nil
}

// linkToPreviousNegotiation creates a TEMPORAL_NEXT edge from the most recent
// negotiation with the same peer to this new one.
func (s *Store) linkToPreviousNegotiation(ctx context.Context, newNodeID, peerDID string) {
	nql := fmt.Sprintf(
		`MATCH (n:Episodic) WHERE n.node_label = "negotiation" AND n.peer_did = "%s" ORDER BY n.energy DESC LIMIT 2 RETURN n`,
		escapeNQL(peerDID),
	)

	result, err := s.client.Query(ctx, nql, nil, s.collection)
	if err != nil || result == nil || len(result.Nodes) < 2 {
		return
	}

	// Second result is the previous negotiation
	prevID := result.Nodes[1].ID
	if prevID != "" && prevID != newNodeID {
		if _, err := s.client.InsertEdge(ctx, nietzsche.InsertEdgeOpts{
			From:       prevID,
			To:         newNodeID,
			EdgeType:   "Association",
			Weight:     1.0,
			Collection: s.collection,
		}); err != nil {
			memoryLog.Warn("link to previous negotiation failed", logging.Err(err))
		}
	}
}

func nodeToNegotiationMemory(n nietzsche.NodeResult) NegotiationMemory {
	m := NegotiationMemory{NodeID: n.ID}
	if v, ok := n.Content["conversation_id"].(string); ok {
		m.ConversationID = v
	}
	if v, ok := n.Content["peer_did"].(string); ok {
		m.PeerDID = v
	}
	if v, ok := n.Content["capability"].(string); ok {
		m.Capability = v
	}
	if v, ok := n.Content["outcome"].(string); ok {
		m.Outcome = v
	}
	if v, ok := n.Content["rating"].(float64); ok {
		m.Rating = int(v)
	}
	if v, ok := n.Content["duration_ms"].(float64); ok {
		m.DurationMs = int64(v)
	}
	if v, ok := n.Content["started_at"].(float64); ok {
		m.StartedAt = int64(v)
	}
	if v, ok := n.Content["completed_at"].(float64); ok {
		m.CompletedAt = int64(v)
	}
	return m
}
