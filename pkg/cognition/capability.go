package cognition

import (
	"context"
	"fmt"
	"time"

	nietzsche "nietzsche-sdk"
)

// PeerCapability is a cached record of a peer's advertised capability.
// Stored as Concept nodes in the Poincare ball.
type PeerCapability struct {
	PeerDID     string `json:"peer_did"`
	Name        string `json:"capability_name"`
	Version     string `json:"capability_version"`
	Description string `json:"description"`
	CachedAt    int64  `json:"cached_at"` // Unix ms
	ExpiresAt   int64  `json:"expires_at"`
	NodeID      string `json:"node_id,omitempty"`
}

// CacheCapability stores a peer's capability advertisement in NietzscheDB.
// Creates a Concept node for the capability linked to the peer's trust node.
func (s *Store) CacheCapability(ctx context.Context, cap PeerCapability) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixMilli()
	if cap.CachedAt == 0 {
		cap.CachedAt = now
	}
	if cap.ExpiresAt == 0 {
		// Default TTL: 24 hours (per AIP spec for DHT provider records)
		cap.ExpiresAt = now + 86400000
	}

	content := map[string]interface{}{
		"node_label":         "capability_cache",
		"peer_did":           cap.PeerDID,
		"capability_name":    cap.Name,
		"capability_version": cap.Version,
		"description":        cap.Description,
		"cached_at":          cap.CachedAt,
		"expires_at":         cap.ExpiresAt,
	}

	_, err := s.client.MergeNode(ctx, nietzsche.MergeNodeOpts{
		Collection:  s.collection,
		NodeType:    "Semantic",
		MatchKeys:   map[string]interface{}{"node_label": "capability_cache", "peer_did": cap.PeerDID, "capability_name": cap.Name},
		OnCreateSet: content,
		OnMatchSet:  content,
		Coords:      zeroCoords(),
		Energy:      0.7,
	})
	if err != nil {
		return fmt.Errorf("cognition: cache capability: %w", err)
	}

	return nil
}

// FindPeersWithCapability searches NietzscheDB for cached peers that have
// a specific capability and meet the minimum trust threshold.
// This avoids hitting the DHT for known peers.
func (s *Store) FindPeersWithCapability(ctx context.Context, capName string, minTrust float32, limit int) ([]PeerCapability, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nql := fmt.Sprintf(
		`MATCH (n:Semantic) WHERE n.node_label = "capability_cache" AND n.capability_name = "%s" LIMIT %d RETURN n`,
		escapeNQL(capName), limit*2, // over-fetch to filter by trust
	)

	result, err := s.client.Query(ctx, nql, nil, s.collection)
	if err != nil {
		return nil, fmt.Errorf("cognition: find peers with capability: %w", err)
	}

	now := time.Now().UnixMilli()
	var caps []PeerCapability

	for _, node := range result.Nodes {
		if !node.Found {
			continue
		}
		cap := nodeToPeerCapability(node)

		// Skip expired
		if cap.ExpiresAt > 0 && cap.ExpiresAt < now {
			continue
		}

		// Check trust
		trust := s.GetTrustScore(ctx, cap.PeerDID)
		if trust < minTrust {
			continue
		}

		caps = append(caps, cap)
		if len(caps) >= limit {
			break
		}
	}

	return caps, nil
}

// PurgeExpiredCapabilities removes cached capabilities past their TTL.
func (s *Store) PurgeExpiredCapabilities(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixMilli()

	nql := fmt.Sprintf(
		`MATCH (n:Semantic) WHERE n.node_label = "capability_cache" AND n.expires_at < %d RETURN n`,
		now,
	)

	result, err := s.client.Query(ctx, nql, nil, s.collection)
	if err != nil {
		return 0, fmt.Errorf("cognition: query expired capabilities: %w", err)
	}

	removed := 0
	for _, node := range result.Nodes {
		if node.Found && node.ID != "" {
			if err := s.client.DeleteNode(ctx, node.ID, s.collection); err == nil {
				removed++
			}
		}
	}

	return removed, nil
}

func nodeToPeerCapability(n nietzsche.NodeResult) PeerCapability {
	c := PeerCapability{NodeID: n.ID}
	if v, ok := n.Content["peer_did"].(string); ok {
		c.PeerDID = v
	}
	if v, ok := n.Content["capability_name"].(string); ok {
		c.Name = v
	}
	if v, ok := n.Content["capability_version"].(string); ok {
		c.Version = v
	}
	if v, ok := n.Content["description"].(string); ok {
		c.Description = v
	}
	if v, ok := n.Content["cached_at"].(float64); ok {
		c.CachedAt = int64(v)
	}
	if v, ok := n.Content["expires_at"].(float64); ok {
		c.ExpiresAt = int64(v)
	}
	return c
}
