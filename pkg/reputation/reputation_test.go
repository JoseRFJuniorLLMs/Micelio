package reputation

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewTrustStore(t *testing.T) {
	ts := NewTrustStore()
	if ts == nil {
		t.Fatal("NewTrustStore returned nil")
	}
	if ts.peers == nil {
		t.Fatal("peers map is nil")
	}
	if len(ts.peers) != 0 {
		t.Errorf("expected empty peers map, got %d entries", len(ts.peers))
	}
}

func TestRecordSuccess(t *testing.T) {
	ts := NewTrustStore()
	peer := "did:key:z6MkTest1"

	// Unknown peer should have neutral score
	if score := ts.GetScore(peer); score != 0.5 {
		t.Errorf("unknown peer score: got %f, want 0.5", score)
	}

	ts.RecordSuccess(peer, 100.0)

	score := ts.GetScore(peer)
	if score <= 0.5 {
		t.Errorf("score after success should be > 0.5, got %f", score)
	}

	// Record multiple successes
	ts.RecordSuccess(peer, 200.0)
	ts.RecordSuccess(peer, 150.0)

	score2 := ts.GetScore(peer)
	if score2 < score {
		t.Errorf("score should not decrease with more successes: %f -> %f", score, score2)
	}

	// Check interaction counts
	ts.mu.RLock()
	r := ts.peers[peer]
	ts.mu.RUnlock()

	if r.InteractionsTotal != 3 {
		t.Errorf("interactions: got %d, want 3", r.InteractionsTotal)
	}
	if r.Successes != 3 {
		t.Errorf("successes: got %d, want 3", r.Successes)
	}
}

func TestRecordFailure(t *testing.T) {
	ts := NewTrustStore()
	peer := "did:key:z6MkTest2"

	// First record some successes
	ts.RecordSuccess(peer, 100.0)
	ts.RecordSuccess(peer, 100.0)
	goodScore := ts.GetScore(peer)

	// Now record a failure
	ts.RecordFailure(peer)

	badScore := ts.GetScore(peer)
	if badScore >= goodScore {
		t.Errorf("score after failure should decrease: %f -> %f", goodScore, badScore)
	}

	ts.mu.RLock()
	r := ts.peers[peer]
	ts.mu.RUnlock()

	if r.Failures != 1 {
		t.Errorf("failures: got %d, want 1", r.Failures)
	}
	if r.InteractionsTotal != 3 {
		t.Errorf("interactions: got %d, want 3", r.InteractionsTotal)
	}
}

func TestSignatureFailureBan(t *testing.T) {
	ts := NewTrustStore()
	peer := "did:key:z6MkTest3"

	// Build up good reputation
	for i := 0; i < 10; i++ {
		ts.RecordSuccess(peer, 50.0)
	}

	if ts.IsBlocked(peer) {
		t.Error("peer should not be blocked before signature failure")
	}

	// Signature failure should immediately ban
	ts.RecordSignatureFailure(peer)

	if !ts.IsBlocked(peer) {
		t.Error("peer should be blocked after signature failure")
	}

	score := ts.GetScore(peer)
	if score != 0.0 {
		t.Errorf("score after signature failure: got %f, want 0.0", score)
	}

	ts.mu.RLock()
	r := ts.peers[peer]
	ts.mu.RUnlock()

	if r.SignatureFailures != 1 {
		t.Errorf("signature failures: got %d, want 1", r.SignatureFailures)
	}
}

func TestTemporalDecay(t *testing.T) {
	ts := NewTrustStore()
	peer := "did:key:z6MkTest4"

	ts.RecordSuccess(peer, 100.0)
	freshScore := ts.GetScore(peer)

	// Simulate 2 days of inactivity by manipulating LastSeen
	ts.mu.Lock()
	ts.peers[peer].LastSeen = time.Now().Add(-48 * time.Hour)
	ts.mu.Unlock()

	// Recompute by fetching trusted peers (which recomputes scores)
	trusted := ts.GetTrustedPeers(0.0)
	if len(trusted) == 0 {
		t.Fatal("expected at least one peer in trusted list")
	}

	decayedScore := trusted[0].Score
	if decayedScore >= freshScore {
		t.Errorf("decayed score should be less than fresh: %f >= %f", decayedScore, freshScore)
	}

	// After 2 days, decay should be 0.9^2 = 0.81
	expectedDecay := 0.81
	ratio := float64(decayedScore) / float64(freshScore)
	if ratio < expectedDecay-0.05 || ratio > expectedDecay+0.05 {
		t.Errorf("decay ratio: got %f, expected ~%f", ratio, expectedDecay)
	}
}

func TestGetTrustedPeers(t *testing.T) {
	ts := NewTrustStore()

	// Add peers with varying trust levels
	for i := 0; i < 5; i++ {
		ts.RecordSuccess("did:key:z6MkGood"+string(rune('A'+i)), 100.0)
	}
	ts.RecordFailure("did:key:z6MkBad")
	ts.RecordFailure("did:key:z6MkBad")
	ts.RecordFailure("did:key:z6MkBad")

	// Get peers with minScore 0.5
	trusted := ts.GetTrustedPeers(0.5)

	// All good peers should be included
	if len(trusted) < 5 {
		t.Errorf("expected at least 5 trusted peers, got %d", len(trusted))
	}

	// Verify sorted by score descending
	for i := 1; i < len(trusted); i++ {
		if trusted[i].Score > trusted[i-1].Score {
			t.Errorf("trusted peers not sorted: index %d score %f > index %d score %f",
				i, trusted[i].Score, i-1, trusted[i-1].Score)
		}
	}
}

func TestBlockUnblock(t *testing.T) {
	ts := NewTrustStore()
	peer := "did:key:z6MkTest5"

	ts.RecordSuccess(peer, 100.0)

	if ts.IsBlocked(peer) {
		t.Error("peer should not be blocked initially")
	}

	ts.Block(peer)

	if !ts.IsBlocked(peer) {
		t.Error("peer should be blocked after Block()")
	}
	if ts.GetScore(peer) != 0.0 {
		t.Errorf("blocked peer score: got %f, want 0.0", ts.GetScore(peer))
	}

	ts.Unblock(peer)

	if ts.IsBlocked(peer) {
		t.Error("peer should not be blocked after Unblock()")
	}

	// Score should be recomputed after unblock
	score := ts.GetScore(peer)
	if score <= 0.0 {
		t.Errorf("unblocked peer score should be positive, got %f", score)
	}
}

func TestSavePersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.json")

	// Create and populate a trust store
	ts := NewTrustStore()
	ts.RecordSuccess("did:key:z6MkPeerA", 100.0)
	ts.RecordSuccess("did:key:z6MkPeerA", 200.0)
	ts.RecordFailure("did:key:z6MkPeerB")
	ts.RecordSignatureFailure("did:key:z6MkPeerC")

	// Save
	if err := ts.Save(path); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("trust file was not created")
	}

	// Load
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// Verify loaded data matches
	if score := loaded.GetScore("did:key:z6MkPeerA"); score <= 0.5 {
		t.Errorf("loaded PeerA score: got %f, expected > 0.5", score)
	}
	if !loaded.IsBlocked("did:key:z6MkPeerC") {
		t.Error("loaded PeerC should be blocked")
	}

	loaded.mu.RLock()
	rA := loaded.peers["did:key:z6MkPeerA"]
	rB := loaded.peers["did:key:z6MkPeerB"]
	loaded.mu.RUnlock()

	if rA == nil {
		t.Fatal("PeerA not found in loaded store")
	}
	if rA.Successes != 2 {
		t.Errorf("loaded PeerA successes: got %d, want 2", rA.Successes)
	}
	if rB == nil {
		t.Fatal("PeerB not found in loaded store")
	}
	if rB.Failures != 1 {
		t.Errorf("loaded PeerB failures: got %d, want 1", rB.Failures)
	}
}

func TestPeerSelector(t *testing.T) {
	ts := NewTrustStore()

	// Build up varied trust scores
	for i := 0; i < 10; i++ {
		ts.RecordSuccess("did:key:z6MkHigh", 50.0)
	}
	ts.RecordSuccess("did:key:z6MkMedium", 500.0)
	ts.RecordFailure("did:key:z6MkLow")
	ts.RecordFailure("did:key:z6MkLow")
	ts.RecordSignatureFailure("did:key:z6MkBanned")

	selector := NewPeerSelector(ts)

	candidates := []string{
		"did:key:z6MkHigh",
		"did:key:z6MkMedium",
		"did:key:z6MkLow",
		"did:key:z6MkBanned",
		"did:key:z6MkUnknown",
	}

	// SelectBest with minTrust 0.5
	best := selector.SelectBest(candidates, 0.5)

	// Banned peer should be excluded
	for _, did := range best {
		if did == "did:key:z6MkBanned" {
			t.Error("banned peer should not be in SelectBest results")
		}
	}

	// High trust peer should be first
	if len(best) > 0 && best[0] != "did:key:z6MkHigh" {
		t.Errorf("SelectBest first result: got %q, want did:key:z6MkHigh", best[0])
	}

	// SelectForCapability should work the same
	capResults := selector.SelectForCapability(candidates, "translate", 0.5)
	if len(capResults) != len(best) {
		t.Errorf("SelectForCapability length mismatch: %d vs %d", len(capResults), len(best))
	}

	// WeightedRandom should not return banned peers
	goodCandidates := []string{"did:key:z6MkHigh", "did:key:z6MkMedium"}
	selected, err := selector.WeightedRandom(goodCandidates, 0.3)
	if err != nil {
		t.Fatalf("WeightedRandom error: %v", err)
	}
	if selected != "did:key:z6MkHigh" && selected != "did:key:z6MkMedium" {
		t.Errorf("WeightedRandom returned unexpected: %q", selected)
	}

	// WeightedRandom with only blocked/low-trust candidates
	blockedOnly := []string{"did:key:z6MkBanned"}
	_, err = selector.WeightedRandom(blockedOnly, 0.3)
	if err == nil {
		t.Error("WeightedRandom should error when no candidates meet threshold")
	}
}
