package network

import (
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/JoseRFJuniorLLMs/Micelio/pkg/logging"
)

// rateLimitLog is a package-level logger for rate limiting.
var rateLimitLog = logging.New("ratelimit")

const (
	// DefaultRateLimit is the default maximum messages per second per peer.
	DefaultRateLimit = 100

	// MaxRateLimitEntries is the maximum number of tracked peers in the rate limiter
	// to prevent unbounded memory growth from many unique peers.
	MaxRateLimitEntries = 10000

	// rateLimitCleanupInterval is how often stale entries are cleaned up.
	rateLimitCleanupInterval = 1 * time.Minute
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
	stopCh    chan struct{}
}

// NewPeerRateLimiter creates a new rate limiter that allows maxPerSec messages
// per second for each peer. Pass 0 to use DefaultRateLimit.
func NewPeerRateLimiter(maxPerSec int) *PeerRateLimiter {
	if maxPerSec <= 0 {
		maxPerSec = DefaultRateLimit
	}
	rl := &PeerRateLimiter{
		counters:  make(map[peer.ID]*rateLimitEntry),
		maxPerSec: maxPerSec,
		window:    time.Second,
		stopCh:    make(chan struct{}),
	}
	go rl.cleanupLoop()
	return rl
}

// SetRate changes the rate limit dynamically.
func (r *PeerRateLimiter) SetRate(maxPerSec int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if maxPerSec > 0 {
		r.maxPerSec = maxPerSec
	}
}

// Stop terminates the background cleanup goroutine.
func (r *PeerRateLimiter) Stop() {
	close(r.stopCh)
}

// Allow returns true if the peer has not exceeded the rate limit.
// It increments the counter for the peer and resets if the window has passed.
func (r *PeerRateLimiter) Allow(p peer.ID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	entry, ok := r.counters[p]
	if !ok {
		// Enforce max tracked peers to prevent memory exhaustion from many unique peers
		if len(r.counters) >= MaxRateLimitEntries {
			rateLimitLog.Warn("max tracked peers reached, rejecting", logging.Int("limit", MaxRateLimitEntries), logging.String("peer", p.String()[:12]))
			return false
		}
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

// cleanupLoop periodically cleans up stale rate limit entries.
func (r *PeerRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rateLimitCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.Cleanup()
		}
	}
}
