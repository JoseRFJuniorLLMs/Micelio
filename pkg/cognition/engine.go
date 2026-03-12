package cognition

import (
	"context"
	"sync"
	"time"

	"github.com/JoseRFJuniorLLMs/Micelio/pkg/logging"
)

const (
	// DefaultDesirePollInterval is how often the engine polls NietzscheDB for
	// unfulfilled desires.
	DefaultDesirePollInterval = 30 * time.Second

	// DefaultMaintenanceInterval is how often the engine runs maintenance
	// tasks such as purging expired capabilities.
	DefaultMaintenanceInterval = 60 * time.Second

	// DefaultDesireChannelSize is the buffer size for the desire channel.
	DefaultDesireChannelSize = 64
)

// CognitionEngine is a background loop that polls NietzscheDB for desires,
// purges expired capabilities, and cleans up stale message IDs.
// It bridges the desire layer with the agent's INTENT generation.
type CognitionEngine struct {
	store     *Store
	desires   chan Desire
	log       *logging.Logger
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
		desires:  make(chan Desire, DefaultDesireChannelSize),
		log:      logging.New("cognition-engine"),
		ctx:      ctx,
		cancel:   cancel,
		interval: DefaultDesirePollInterval,
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

	maintenanceTicker := time.NewTicker(DefaultMaintenanceInterval)
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
		e.log.Error("poll desires failed", logging.Err(err))
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
			e.log.Warn("desire channel full, dropping desire", logging.String("desire_id", d.ID))
		}
	}
}

// maintenance performs periodic cleanup: purging expired capabilities.
func (e *CognitionEngine) maintenance() {
	removed, err := e.store.PurgeExpiredCapabilities(e.ctx)
	if err != nil {
		e.log.Error("purge capabilities failed", logging.Err(err))
	} else if removed > 0 {
		e.log.Info("purged expired capabilities", logging.Int("count", removed))
	}
}
