package cognition

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// CognitionEngine is a background loop that polls NietzscheDB for desires,
// purges expired capabilities, and cleans up stale message IDs.
// It bridges the desire layer with the agent's INTENT generation.
type CognitionEngine struct {
	store     *Store
	desires   chan Desire
	ctx       context.Context
	cancel    context.CancelFunc
	interval  time.Duration
	startOnce sync.Once
	done      chan struct{}
}

// NewCognitionEngine creates a new CognitionEngine tied to the given Store.
// The engine uses a 30-second poll interval for desires and 60-second intervals
// for maintenance tasks (capability purge, seen message cleanup).
func NewCognitionEngine(store *Store) *CognitionEngine {
	ctx, cancel := context.WithCancel(context.Background())
	return &CognitionEngine{
		store:    store,
		desires:  make(chan Desire, 64),
		ctx:      ctx,
		cancel:   cancel,
		interval: 30 * time.Second,
		done:     make(chan struct{}),
	}
}

// Start launches the background goroutine that polls desires and performs
// periodic maintenance. It is safe to call Start only once.
func (e *CognitionEngine) Start() {
	e.startOnce.Do(func() {
		go e.run()
	})
}

// Stop signals the background goroutine to exit and waits for cleanup.
func (e *CognitionEngine) Stop() {
	e.cancel()
	<-e.done
}

// Desires returns a read-only channel that emits unfulfilled desires.
// The agent should consume this channel and convert desires into AIP INTENTs.
func (e *CognitionEngine) Desires() <-chan Desire {
	return e.desires
}

// run is the main loop of the cognition engine.
func (e *CognitionEngine) run() {
	desireTicker := time.NewTicker(e.interval)
	defer desireTicker.Stop()

	maintenanceTicker := time.NewTicker(60 * time.Second)
	defer maintenanceTicker.Stop()

	for {
		select {
		case <-e.ctx.Done():
			close(e.desires)
			close(e.done)
			return

		case <-desireTicker.C:
			e.pollDesires()

		case <-maintenanceTicker.C:
			e.maintenance()
		}
	}
}

// pollDesires queries NietzscheDB for unfulfilled desires and emits them
// on the desires channel. Each desire is converted to an AIP capability
// name via DesireToCapability before emission.
func (e *CognitionEngine) pollDesires() {
	desires, err := e.store.PollDesires(e.ctx)
	if err != nil {
		fmt.Printf("[cognition-engine] poll desires error: %v\n", err)
		return
	}

	for _, d := range desires {
		// Ensure the desire has a capability mapping
		if d.Capability == "" {
			d.Capability = DesireToCapability(d)
		}

		// Non-blocking send: drop if channel is full
		select {
		case e.desires <- d:
		default:
			fmt.Printf("[cognition-engine] desire channel full, dropping desire %s\n", d.ID)
		}
	}
}

// maintenance performs periodic cleanup: purging expired capabilities.
func (e *CognitionEngine) maintenance() {
	removed, err := e.store.PurgeExpiredCapabilities(e.ctx)
	if err != nil {
		fmt.Printf("[cognition-engine] purge capabilities error: %v\n", err)
	} else if removed > 0 {
		fmt.Printf("[cognition-engine] purged %d expired capabilities\n", removed)
	}
}
