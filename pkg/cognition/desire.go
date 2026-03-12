package cognition

import (
	"context"
	"fmt"
	"time"

	nietzsche "nietzsche-sdk"
)

// Desire represents a knowledge gap that the agent wants to fill.
// These are generated from NietzscheDB's internal desire engine or
// from the agent's own analysis of its knowledge graph.
type Desire struct {
	ID             string     `json:"id"`
	Description    string     `json:"description"`
	Capability     string     `json:"capability"`      // AIP capability name to seek
	Priority       float32    `json:"priority"`         // 0.0-1.0
	DepthRange     [2]float32 `json:"depth_range"`      // Poincare depth range
	SuggestedQuery string     `json:"suggested_query"`  // NQL template
	CreatedAt      int64      `json:"created_at"`
	Fulfilled      bool       `json:"fulfilled"`
}

// PollDesires queries NietzscheDB's agency engine for unfulfilled desires
// and converts them into AIP-compatible desire objects.
// This is the bridge: NietzscheDB desire.rs -> Micelio INTENT.
func (s *Store) PollDesires(ctx context.Context) ([]Desire, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nql := `MATCH (n:Semantic) WHERE n.node_label = "desire" AND n.fulfilled = false ORDER BY n.energy DESC LIMIT 20 RETURN n`

	result, err := s.client.Query(ctx, nql, nil, s.collection)
	if err != nil {
		return nil, fmt.Errorf("cognition: poll desires: %w", err)
	}

	var desires []Desire
	for _, node := range result.Nodes {
		if !node.Found {
			continue
		}
		d := nodeToDesire(node)
		if !d.Fulfilled {
			desires = append(desires, d)
		}
	}

	return desires, nil
}

// CreateDesire manually registers a knowledge desire (e.g., from agent logic).
func (s *Store) CreateDesire(ctx context.Context, d Desire) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if d.CreatedAt == 0 {
		d.CreatedAt = time.Now().UnixMilli()
	}

	content := map[string]interface{}{
		"node_label":      "desire",
		"description":     d.Description,
		"capability":      d.Capability,
		"priority":        d.Priority,
		"depth_min":       d.DepthRange[0],
		"depth_max":       d.DepthRange[1],
		"suggested_query": d.SuggestedQuery,
		"created_at":      d.CreatedAt,
		"fulfilled":       false,
	}

	result, err := s.client.InsertNode(ctx, nietzsche.InsertNodeOpts{
		Coords:     zeroCoords(),
		Content:    content,
		NodeType:   "Semantic",
		Energy:     d.Priority,
		Collection: s.collection,
	})
	if err != nil {
		return "", fmt.Errorf("cognition: create desire: %w", err)
	}

	return result.ID, nil
}

// FulfillDesire marks a desire as satisfied after receiving knowledge via AIP.
func (s *Store) FulfillDesire(ctx context.Context, desireNodeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.client.UpdateEnergy(ctx, desireNodeID, 0.0, s.collection)
	if err != nil {
		return fmt.Errorf("cognition: fulfill desire: %w", err)
	}

	return nil
}

// DesireToCapability maps a NietzscheDB desire's suggested_query to an AIP
// capability name. This is the semantic bridge between what the graph wants
// and what the P2P network can provide.
func DesireToCapability(d Desire) string {
	if d.Capability != "" {
		return d.Capability
	}

	avgDepth := (d.DepthRange[0] + d.DepthRange[1]) / 2

	switch {
	case avgDepth < 0.3:
		return "knowledge.conceptual"
	case avgDepth < 0.6:
		return "knowledge.analytical"
	default:
		return "knowledge.specific"
	}
}

func nodeToDesire(n nietzsche.NodeResult) Desire {
	d := Desire{ID: n.ID}
	if v, ok := n.Content["description"].(string); ok {
		d.Description = v
	}
	if v, ok := n.Content["capability"].(string); ok {
		d.Capability = v
	}
	if v, ok := n.Content["priority"].(float64); ok {
		d.Priority = float32(v)
	}
	if v, ok := n.Content["depth_min"].(float64); ok {
		d.DepthRange[0] = float32(v)
	}
	if v, ok := n.Content["depth_max"].(float64); ok {
		d.DepthRange[1] = float32(v)
	}
	if v, ok := n.Content["suggested_query"].(string); ok {
		d.SuggestedQuery = v
	}
	if v, ok := n.Content["created_at"].(float64); ok {
		d.CreatedAt = int64(v)
	}
	if v, ok := n.Content["fulfilled"].(bool); ok {
		d.Fulfilled = v
	}
	d.Priority = n.Energy
	return d
}
