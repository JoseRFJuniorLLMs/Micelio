package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/JoseRFJuniorLLMs/Micelio/pkg/identity"
	"github.com/JoseRFJuniorLLMs/Micelio/pkg/protocol"
)

func TestAgentNew(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	id, err := identity.Generate()
	if err != nil {
		t.Fatalf("Generate identity: %v", err)
	}

	a, err := New(ctx, Config{
		Name:     "test-agent",
		Port:     0, // random port
		Identity: id,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	if a.Identity == nil {
		t.Error("agent Identity is nil")
	}
	if a.Host == nil {
		t.Error("agent Host is nil")
	}
	if a.Conversations == nil {
		t.Error("agent Conversations is nil")
	}
	if a.Cognition != nil {
		t.Error("agent Cognition should be nil when no NietzscheAddr is set")
	}
}

func TestAgentNewGeneratesIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	a, err := New(ctx, Config{
		Name: "auto-id-agent",
		Port: 0,
		// Identity is nil, should auto-generate
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	if a.Identity == nil {
		t.Fatal("agent Identity is nil after auto-generation")
	}
	if !strings.HasPrefix(a.Identity.DID, "did:key:z") {
		t.Errorf("auto-generated DID format invalid: %q", a.Identity.DID)
	}
}

func TestAgentRegisterCapability(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	a, err := New(ctx, Config{
		Name: "cap-agent",
		Port: 0,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	if len(a.Capabilities) != 0 {
		t.Errorf("initial capabilities should be empty, got %d", len(a.Capabilities))
	}

	a.RegisterCapability(Capability{
		Name:        "translate",
		Description: "Translate text between languages",
		Version:     "1.0",
	})

	if len(a.Capabilities) != 1 {
		t.Fatalf("capabilities length: got %d, want 1", len(a.Capabilities))
	}
	if a.Capabilities[0].Name != "translate" {
		t.Errorf("capability name: got %q", a.Capabilities[0].Name)
	}

	// Register a second capability
	a.RegisterCapability(Capability{
		Name:        "summarize",
		Description: "Summarize documents",
		Version:     "1.0",
	})

	if len(a.Capabilities) != 2 {
		t.Errorf("capabilities length after second add: got %d, want 2", len(a.Capabilities))
	}
}

func TestAgentDID(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	id, err := identity.Generate()
	if err != nil {
		t.Fatalf("Generate identity: %v", err)
	}

	a, err := New(ctx, Config{
		Name:     "did-agent",
		Port:     0,
		Identity: id,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	if a.DID() != id.DID {
		t.Errorf("DID() = %q, want %q", a.DID(), id.DID)
	}

	if !strings.HasPrefix(a.DID(), "did:key:z") {
		t.Errorf("DID() format invalid: %q", a.DID())
	}
}

func TestAgentClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	a, err := New(ctx, Config{
		Name: "close-agent",
		Port: 0,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	// Close should not error
	if err := a.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}

	// Second close should not panic (libp2p host handles this)
	// Just verify it doesn't panic
	a.Close()
}

func TestAgentOnMessage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	a, err := New(ctx, Config{
		Name: "handler-agent",
		Port: 0,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer a.Close()

	// Registering a handler should not panic or error
	a.OnMessage(protocol.TypeIntent, func(from peer.ID, msg *protocol.Message) *protocol.Message {
		return nil
	})
}
