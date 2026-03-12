package cognition

import (
	"context"
	"fmt"
	"math"
	"time"

	nietzsche "nietzsche-sdk"
)

// TrustRecord tracks the reputation of a peer agent.
// Stored as a Semantic node in the Poincare ball where:
//   - magnitude encodes trust (closer to center = MORE trusted)
//   - energy encodes activity level
//   - metadata holds interaction counters
type TrustRecord struct {
	PeerDID            string  `json:"peer_did"`
	NodeID             string  `json:"node_id,omitempty"`
	InteractionsTotal  int64   `json:"interactions_total"`
	DeliveriesSuccess  int64   `json:"deliveries_successful"`
	DeliveriesLate     int64   `json:"deliveries_late"`
	DeliveriesFailed   int64   `json:"deliveries_failed"`
	AvgLatencyMs       float64 `json:"avg_latency_ms"`
	SignatureFailures  int64   `json:"signature_failures"`
	LastSeenMs         int64   `json:"last_seen_ms"`
	TrustScore         float32 `json:"trust_score"`
	ManualBlock        bool    `json:"manual_block"`
}

// trustToMagnitude converts a trust score [0,1] to a Poincare ball magnitude.
// High trust = low magnitude (closer to center = more abstract/important).
// Low trust = high magnitude (periphery = less important).
// Banned (0.0) → magnitude 0.95 (edge of ball, almost phantom).
// Very trusted (1.0) → magnitude 0.05 (near center, core knowledge).
func trustToMagnitude(trust float32) float64 {
	// Invert: trust 1.0 → mag 0.05, trust 0.0 → mag 0.95
	return 0.95 - float64(trust)*0.90
}

// trustEmbedding creates a 128D Poincare embedding where the magnitude
// encodes trust level. Direction is derived from a hash of the peer DID
// to give each peer a unique angular position.
func trustEmbedding(peerDID string, trust float32) []float64 {
	mag := trustToMagnitude(trust)
	coords := make([]float64, DefaultDim)

	// Use DID bytes to create a deterministic direction
	h := []byte(peerDID)
	for i := 0; i < DefaultDim; i++ {
		// Simple deterministic direction from DID hash
		coords[i] = float64(h[i%len(h)]) - 128.0
	}

	// Normalize to unit vector then scale by magnitude
	var norm float64
	for _, c := range coords {
		norm += c * c
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range coords {
			coords[i] = (coords[i] / norm) * mag
		}
	}

	return coords
}

// computeTrustScore calculates the trust score from interaction history.
// Algorithm from AIP spec v0.2 Section: Reputation Layer.
func computeTrustScore(r *TrustRecord) float32 {
	// Immediate ban on signature failure
	if r.SignatureFailures > 0 || r.ManualBlock {
		return 0.0
	}

	if r.InteractionsTotal == 0 {
		return 0.5 // neutral for unknown peers
	}

	// Delivery rate
	rate := float64(r.DeliveriesSuccess) / float64(r.InteractionsTotal)

	// Latency score
	var latencyScore float64
	if r.AvgLatencyMs < 1000 {
		latencyScore = 1.0
	} else {
		latencyScore = 1000.0 / r.AvgLatencyMs
	}

	// Failure penalty (max 50%)
	penalty := math.Min(float64(r.DeliveriesFailed)*0.1, 0.5)

	// Temporal decay: 10% per day of inactivity
	var decay float64 = 1.0
	if r.LastSeenMs > 0 {
		daysSince := float64(time.Now().UnixMilli()-r.LastSeenMs) / 86400000.0
		if daysSince > 0 {
			decay = math.Pow(0.9, daysSince)
		}
	}

	// Final formula from spec
	score := ((rate*0.5 + latencyScore*0.3 + 0.2) - penalty) * decay
	return float32(math.Max(0, math.Min(1, score)))
}

// RecordInteraction updates trust after a completed negotiation.
func (s *Store) RecordInteraction(ctx context.Context, peerDID string, success bool, latencyMs float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, err := s.getTrustRecord(ctx, peerDID)
	if err != nil {
		// New peer
		record = &TrustRecord{
			PeerDID:    peerDID,
			TrustScore: 0.5,
		}
	}

	record.InteractionsTotal++
	record.LastSeenMs = time.Now().UnixMilli()

	if success {
		record.DeliveriesSuccess++
	} else {
		record.DeliveriesFailed++
	}

	// Running average latency
	if record.InteractionsTotal == 1 {
		record.AvgLatencyMs = latencyMs
	} else {
		record.AvgLatencyMs = record.AvgLatencyMs*0.8 + latencyMs*0.2
	}

	record.TrustScore = computeTrustScore(record)

	return s.upsertTrustNode(ctx, record)
}

// RecordSignatureFailure immediately bans a peer (trust = 0).
func (s *Store) RecordSignatureFailure(ctx context.Context, peerDID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, err := s.getTrustRecord(ctx, peerDID)
	if err != nil {
		record = &TrustRecord{PeerDID: peerDID}
	}

	record.SignatureFailures++
	record.TrustScore = 0.0
	record.LastSeenMs = time.Now().UnixMilli()

	return s.upsertTrustNode(ctx, record)
}

// GetTrustScore returns the trust score for a peer. Returns 0.5 for unknown peers.
func (s *Store) GetTrustScore(ctx context.Context, peerDID string) float32 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	record, err := s.getTrustRecord(ctx, peerDID)
	if err != nil {
		return 0.5 // neutral for unknown
	}
	return record.TrustScore
}

// GetTrustedPeers returns peers with trust >= minTrust, ordered by trust descending.
func (s *Store) GetTrustedPeers(ctx context.Context, minTrust float32, limit int) ([]TrustRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nql := fmt.Sprintf(
		`MATCH (n:Semantic) WHERE n.node_label = "trust_record" AND n.energy >= %f ORDER BY n.energy DESC LIMIT %d RETURN n`,
		minTrust, limit,
	)

	result, err := s.client.Query(ctx, nql, nil, s.collection)
	if err != nil {
		return nil, fmt.Errorf("cognition: query trusted peers: %w", err)
	}

	var records []TrustRecord
	for _, node := range result.Nodes {
		if !node.Found {
			continue
		}
		r := nodeToTrustRecord(node)
		if r.TrustScore >= minTrust {
			records = append(records, r)
		}
	}

	return records, nil
}

// IsBanned returns true if a peer has been banned (signature failure or manual block).
func (s *Store) IsBanned(ctx context.Context, peerDID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	record, err := s.getTrustRecord(ctx, peerDID)
	if err != nil {
		return false
	}
	return record.TrustScore == 0 && (record.SignatureFailures > 0 || record.ManualBlock)
}

// --- internal helpers ---

func (s *Store) getTrustRecord(ctx context.Context, peerDID string) (*TrustRecord, error) {
	nql := fmt.Sprintf(
		`MATCH (n:Semantic) WHERE n.node_label = "trust_record" AND n.peer_did = "%s" RETURN n LIMIT 1`,
		escapeNQL(peerDID),
	)

	result, err := s.client.Query(ctx, nql, nil, s.collection)
	if err != nil {
		return nil, err
	}

	if len(result.Nodes) == 0 || !result.Nodes[0].Found {
		return nil, fmt.Errorf("not found")
	}

	r := nodeToTrustRecord(result.Nodes[0])
	return &r, nil
}

func (s *Store) upsertTrustNode(ctx context.Context, r *TrustRecord) error {
	content := map[string]interface{}{
		"node_label":            "trust_record",
		"peer_did":              r.PeerDID,
		"interactions_total":    r.InteractionsTotal,
		"deliveries_successful": r.DeliveriesSuccess,
		"deliveries_late":       r.DeliveriesLate,
		"deliveries_failed":     r.DeliveriesFailed,
		"avg_latency_ms":        r.AvgLatencyMs,
		"signature_failures":    r.SignatureFailures,
		"last_seen_ms":          r.LastSeenMs,
		"trust_score":           r.TrustScore,
		"manual_block":          r.ManualBlock,
	}

	coords := trustEmbedding(r.PeerDID, r.TrustScore)

	_, err := s.client.MergeNode(ctx, nietzsche.MergeNodeOpts{
		Collection:  s.collection,
		NodeType:    "Semantic",
		MatchKeys:   map[string]interface{}{"node_label": "trust_record", "peer_did": r.PeerDID},
		OnCreateSet: content,
		OnMatchSet:  content,
		Coords:      coords,
		Energy:      r.TrustScore, // energy = trust score
	})
	if err != nil {
		return fmt.Errorf("cognition: upsert trust node: %w", err)
	}

	return nil
}

func nodeToTrustRecord(n nietzsche.NodeResult) TrustRecord {
	r := TrustRecord{
		TrustScore: n.Energy,
	}
	if v, ok := n.Content["peer_did"].(string); ok {
		r.PeerDID = v
	}
	if v, ok := n.Content["interactions_total"].(float64); ok {
		r.InteractionsTotal = int64(v)
	}
	if v, ok := n.Content["deliveries_successful"].(float64); ok {
		r.DeliveriesSuccess = int64(v)
	}
	if v, ok := n.Content["deliveries_late"].(float64); ok {
		r.DeliveriesLate = int64(v)
	}
	if v, ok := n.Content["deliveries_failed"].(float64); ok {
		r.DeliveriesFailed = int64(v)
	}
	if v, ok := n.Content["avg_latency_ms"].(float64); ok {
		r.AvgLatencyMs = v
	}
	if v, ok := n.Content["signature_failures"].(float64); ok {
		r.SignatureFailures = int64(v)
	}
	if v, ok := n.Content["last_seen_ms"].(float64); ok {
		r.LastSeenMs = int64(v)
	}
	if v, ok := n.Content["manual_block"].(bool); ok {
		r.ManualBlock = v
	}
	r.NodeID = n.ID
	return r
}
