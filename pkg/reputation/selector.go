package reputation

import (
	"errors"
	"math/rand"
)

// PeerSelector provides trust-based peer selection strategies.
// It wraps a TrustStore to filter and rank candidates by their reputation.
type PeerSelector struct {
	store *TrustStore
}

// NewPeerSelector creates a new PeerSelector backed by the given TrustStore.
func NewPeerSelector(store *TrustStore) *PeerSelector {
	return &PeerSelector{store: store}
}

// SelectBest filters candidates by minimum trust score and returns them
// sorted by trust score descending (most trusted first).
func (ps *PeerSelector) SelectBest(candidates []string, minTrust float32) []string {
	type scored struct {
		did   string
		score float32
	}

	var filtered []scored
	for _, did := range candidates {
		if ps.store.IsBlocked(did) {
			continue
		}
		score := ps.store.GetScore(did)
		if score >= minTrust {
			filtered = append(filtered, scored{did: did, score: score})
		}
	}

	// Sort by score descending (insertion sort, typically small lists)
	for i := 1; i < len(filtered); i++ {
		for j := i; j > 0 && filtered[j].score > filtered[j-1].score; j-- {
			filtered[j], filtered[j-1] = filtered[j-1], filtered[j]
		}
	}

	result := make([]string, len(filtered))
	for i, s := range filtered {
		result[i] = s.did
	}
	return result
}

// SelectForCapability filters candidates by minimum trust score for a
// specific capability. Currently delegates to SelectBest since capability
// tracking is done at the cognition layer. The capability parameter is
// reserved for future per-capability reputation scoring.
func (ps *PeerSelector) SelectForCapability(candidates []string, capability string, minTrust float32) []string {
	// Future: per-capability trust scoring
	_ = capability
	return ps.SelectBest(candidates, minTrust)
}

// WeightedRandom selects a single peer from candidates using weighted random
// selection based on trust scores. Peers with higher trust scores are more
// likely to be selected. Returns an error if no candidates meet the minimum
// trust threshold.
func (ps *PeerSelector) WeightedRandom(candidates []string, minTrust float32) (string, error) {
	type scored struct {
		did   string
		score float32
	}

	var filtered []scored
	var totalScore float64

	for _, did := range candidates {
		if ps.store.IsBlocked(did) {
			continue
		}
		score := ps.store.GetScore(did)
		if score >= minTrust {
			filtered = append(filtered, scored{did: did, score: score})
			totalScore += float64(score)
		}
	}

	if len(filtered) == 0 {
		return "", errors.New("reputation: no candidates meet minimum trust threshold")
	}

	// Weighted random selection
	r := rand.Float64() * totalScore
	var cumulative float64
	for _, s := range filtered {
		cumulative += float64(s.score)
		if r <= cumulative {
			return s.did, nil
		}
	}

	// Fallback (should not reach here, but safe default)
	return filtered[len(filtered)-1].did, nil
}
