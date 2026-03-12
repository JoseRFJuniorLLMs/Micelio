// Package cognition implements Layer 6 (Cognition/Memory) of the AIP stack.
//
// It bridges the Micelio P2P protocol with NietzscheDB, giving each agent
// a hyperbolic brain that remembers negotiations, tracks trust, caches
// capabilities, and generates desires that become AIP INTENTs.
package cognition

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"

	nietzsche "nietzsche-sdk"
)

const (
	// DefaultDim is the Poincare embedding dimension for agent collections.
	DefaultDim = 128
	// DefaultMetric is the distance metric (hyperbolic Poincare ball).
	DefaultMetric = "poincare"
)

// Store is the NietzscheDB-backed cognitive store for a Micelio agent.
// It manages a dedicated collection per agent DID.
type Store struct {
	client     *nietzsche.NietzscheClient
	collection string
	agentDID   string
	mu         sync.RWMutex
}

// Config holds configuration for connecting to NietzscheDB.
type Config struct {
	// NietzscheAddr is the gRPC address (e.g. "localhost:50051" or "136.111.0.47:443").
	NietzscheAddr string
	// AgentDID is this agent's decentralized identifier.
	AgentDID string
	// Collection overrides the auto-generated collection name. Empty = auto.
	Collection string
}

// collectionName generates a deterministic collection name from a DID.
// Format: "micelio_<first 12 hex chars of SHA-256(DID)>"
func collectionName(did string) string {
	h := sha256.Sum256([]byte(did))
	return "micelio_" + hex.EncodeToString(h[:6])
}

// NewStore connects to NietzscheDB and ensures the agent's collection exists.
func NewStore(ctx context.Context, cfg Config) (*Store, error) {
	client, err := nietzsche.ConnectInsecure(cfg.NietzscheAddr)
	if err != nil {
		return nil, fmt.Errorf("cognition: connect to NietzscheDB at %s: %w", cfg.NietzscheAddr, err)
	}

	col := cfg.Collection
	if col == "" {
		col = collectionName(cfg.AgentDID)
	}

	// Ensure collection exists (idempotent)
	_, err = client.CreateCollection(ctx, nietzsche.CollectionConfig{
		Name:   col,
		Dim:    DefaultDim,
		Metric: DefaultMetric,
	})
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("cognition: create collection %s: %w", col, err)
	}

	return &Store{
		client:     client,
		collection: col,
		agentDID:   cfg.AgentDID,
	}, nil
}

// Client returns the underlying NietzscheDB client for advanced operations.
func (s *Store) Client() *nietzsche.NietzscheClient {
	return s.client
}

// Collection returns the agent's collection name.
func (s *Store) Collection() string {
	return s.collection
}

// Close releases the gRPC connection.
func (s *Store) Close() error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

// zeroCoords returns a zero embedding vector of the default dimension.
// Used for relational nodes that don't need semantic positioning.
func zeroCoords() []float64 {
	return make([]float64, DefaultDim)
}
