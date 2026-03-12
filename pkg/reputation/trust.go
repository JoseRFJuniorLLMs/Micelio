// Package reputation implements a standalone trust and reputation layer
// for Micelio agents. It works without NietzscheDB, using only in-memory
// data structures with optional JSON persistence. This allows lightweight
// agents to participate in the P2P network with trust tracking even when
// no database is configured.
package reputation

import (
	"math"
	"sync"
	"time"
)

// TrustRecord tracks the reputation of a peer agent.
type TrustRecord struct {
	PeerDID           string  `json:"peer_did"`
	InteractionsTotal int     `json:"interactions_total"`
	Successes         int     `json:"successes"`
	Failures          int     `json:"failures"`
	AvgLatencyMs      float64 `json:"avg_latency_ms"`
	SignatureFailures int     `json:"signature_failures"`
	LastSeen          time.Time `json:"last_seen"`
	Score             float32 `json:"score"`
	Blocked           bool    `json:"blocked"`
}

// TrustStore is a thread-safe in-memory store of peer trust records.
type TrustStore struct {
	mu    sync.RWMutex
	peers map[string]*TrustRecord
}

// NewTrustStore creates a new empty TrustStore.
func NewTrustStore() *TrustStore {
	return &TrustStore{
		peers: make(map[string]*TrustRecord),
	}
}

// RecordSuccess records a successful interaction with latency measurement.
func (ts *TrustStore) RecordSuccess(peerDID string, latencyMs float64) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	r := ts.getOrCreate(peerDID)
	r.InteractionsTotal++
	r.Successes++
	r.LastSeen = time.Now()

	// Running average latency (exponential moving average)
	if r.InteractionsTotal == 1 {
		r.AvgLatencyMs = latencyMs
	} else {
		r.AvgLatencyMs = r.AvgLatencyMs*0.8 + latencyMs*0.2
	}

	r.Score = computeScore(r)
}

// RecordFailure records a failed interaction with a peer.
func (ts *TrustStore) RecordFailure(peerDID string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	r := ts.getOrCreate(peerDID)
	r.InteractionsTotal++
	r.Failures++
	r.LastSeen = time.Now()
	r.Score = computeScore(r)
}

// RecordSignatureFailure immediately bans the peer by setting score to 0
// and blocking them. A signature failure is considered a critical trust violation.
func (ts *TrustStore) RecordSignatureFailure(peerDID string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	r := ts.getOrCreate(peerDID)
	r.SignatureFailures++
	r.LastSeen = time.Now()
	r.Score = 0.0
	r.Blocked = true
}

// GetScore returns the current trust score for a peer.
// Returns 0.5 (neutral) for unknown peers.
func (ts *TrustStore) GetScore(peerDID string) float32 {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	r, ok := ts.peers[peerDID]
	if !ok {
		return 0.5
	}
	return r.Score
}

// GetTrustedPeers returns all peers with a trust score >= minScore.
// The returned slice is sorted by score descending.
func (ts *TrustStore) GetTrustedPeers(minScore float32) []TrustRecord {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	var result []TrustRecord
	for _, r := range ts.peers {
		// Recompute with temporal decay for accurate snapshot
		current := computeScore(r)
		if current >= minScore && !r.Blocked {
			copy := *r
			copy.Score = current
			result = append(result, copy)
		}
	}

	// Sort by score descending (simple insertion sort, typically small lists)
	for i := 1; i < len(result); i++ {
		for j := i; j > 0 && result[j].Score > result[j-1].Score; j-- {
			result[j], result[j-1] = result[j-1], result[j]
		}
	}

	return result
}

// IsBlocked returns true if a peer has been blocked.
func (ts *TrustStore) IsBlocked(peerDID string) bool {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	r, ok := ts.peers[peerDID]
	if !ok {
		return false
	}
	return r.Blocked
}

// Block manually blocks a peer.
func (ts *TrustStore) Block(peerDID string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	r := ts.getOrCreate(peerDID)
	r.Blocked = true
	r.Score = 0.0
}

// Unblock removes the block on a peer and recomputes their score.
func (ts *TrustStore) Unblock(peerDID string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	r, ok := ts.peers[peerDID]
	if !ok {
		return
	}
	r.Blocked = false
	// Only recompute if there were no signature failures
	if r.SignatureFailures == 0 {
		r.Score = computeScore(r)
	}
}

// Records returns a snapshot of all trust records. Intended for persistence.
func (ts *TrustStore) Records() map[string]*TrustRecord {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	out := make(map[string]*TrustRecord, len(ts.peers))
	for k, v := range ts.peers {
		copy := *v
		out[k] = &copy
	}
	return out
}

// SetRecords replaces the internal map with loaded records. Intended for persistence.
func (ts *TrustStore) SetRecords(records map[string]*TrustRecord) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.peers = records
}

// getOrCreate returns the trust record for a peer, creating one if it does not exist.
// Must be called with ts.mu held for writing.
func (ts *TrustStore) getOrCreate(peerDID string) *TrustRecord {
	r, ok := ts.peers[peerDID]
	if !ok {
		r = &TrustRecord{
			PeerDID: peerDID,
			Score:   0.5, // neutral default
		}
		ts.peers[peerDID] = r
	}
	return r
}

// computeScore calculates the trust score from interaction history.
// Algorithm mirrors cognition/trust.go: computeTrustScore.
//
// Formula: ((successRate*0.5 + latencyScore*0.3 + 0.2) - failurePenalty) * temporalDecay
//   - Immediate ban on signature failure
//   - Temporal decay: 10% per day of inactivity (0.9^daysSinceLastSeen)
//   - Unknown peers: 0.5 (neutral)
func computeScore(r *TrustRecord) float32 {
	// Immediate ban on signature failure or manual block
	if r.SignatureFailures > 0 || r.Blocked {
		return 0.0
	}

	if r.InteractionsTotal == 0 {
		return 0.5
	}

	// Success rate
	rate := float64(r.Successes) / float64(r.InteractionsTotal)

	// Latency score: sub-1s is perfect, degrades inversely above
	var latencyScore float64
	if r.AvgLatencyMs < 1000 {
		latencyScore = 1.0
	} else {
		latencyScore = 1000.0 / r.AvgLatencyMs
	}

	// Failure penalty (max 50%)
	penalty := math.Min(float64(r.Failures)*0.1, 0.5)

	// Temporal decay: 10% per day of inactivity
	var decay float64 = 1.0
	if !r.LastSeen.IsZero() {
		daysSince := time.Since(r.LastSeen).Hours() / 24.0
		if daysSince > 0 {
			decay = math.Pow(0.9, daysSince)
		}
	}

	score := ((rate*0.5 + latencyScore*0.3 + 0.2) - penalty) * decay
	return float32(math.Max(0, math.Min(1, score)))
}
