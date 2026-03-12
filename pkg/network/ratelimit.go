package network

import (
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// rateLimitEntry tracks message counts within a time window for a single peer.
type rateLimitEntry struct {
	count       int
	windowStart time.Time
}

// PeerRateLimiter limits the number of messages per peer within a time window.
type PeerRateLimiter struct {
	mu        sync.Mutex
	counters  map[peer.ID]*rateLimitEntry
	maxPerSec int
	window    time.Duration
}

// NewPeerRateLimiter creates a new rate limiter that allows maxPerSec messages
// per second for each peer.
func NewPeerRateLimiter(maxPerSec int) *PeerRateLimiter {
	return &PeerRateLimiter{
		counters:  make(map[peer.ID]*rateLimitEntry),
		maxPerSec: maxPerSec,
		window:    time.Second,
	}
}

// Allow returns true if the peer has not exceeded the rate limit.
// It increments the counter for the peer and resets if the window has passed.
func (r *PeerRateLimiter) Allow(p peer.ID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	entry, ok := r.counters[p]
	if !ok {
		r.counters[p] = &rateLimitEntry{
			count:       1,
			windowStart: now,
		}
		return true
	}

	// Reset window if it has expired
	if now.Sub(entry.windowStart) >= r.window {
		entry.count = 1
		entry.windowStart = now
		return true
	}

	// Check if within limit
	if entry.count >= r.maxPerSec {
		return false
	}

	entry.count++
	return true
}

// Cleanup removes entries for peers that haven't sent messages recently.
// Should be called periodically to prevent memory leaks.
func (r *PeerRateLimiter) Cleanup() {
	r.mu.Lock()
	defer r.mu.Unlock()

	cutoff := time.Now().Add(-5 * time.Minute)
	for id, entry := range r.counters {
		if entry.windowStart.Before(cutoff) {
			delete(r.counters, id)
		}
	}
}
