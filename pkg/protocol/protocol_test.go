package protocol

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewMessage(t *testing.T) {
	payload := IntentPayload{
		Capability:  "translate",
		Description: "Translate text to Portuguese",
	}
	msg, err := NewMessage(TypeIntent, "did:key:zAlice", "did:key:zBob", "conv-123", payload)
	if err != nil {
		t.Fatalf("NewMessage() error: %v", err)
	}

	// MsgID must be a valid ULID (26 chars, uppercase alphanumeric)
	if len(msg.MsgID) != 26 {
		t.Errorf("MsgID length: got %d, want 26", len(msg.MsgID))
	}

	// Version
	if msg.Version != Version {
		t.Errorf("Version: got %q, want %q", msg.Version, Version)
	}

	// Type
	if msg.Type != TypeIntent {
		t.Errorf("Type: got %q, want %q", msg.Type, TypeIntent)
	}

	// Timestamp must be recent (within 2 seconds)
	age := time.Since(msg.Timestamp)
	if age < 0 || age > 2*time.Second {
		t.Errorf("Timestamp age %v is not recent", age)
	}

	// Default TTL
	if msg.TTL != 60 {
		t.Errorf("TTL: got %d, want 60", msg.TTL)
	}

	// Payload must be valid JSON
	if !json.Valid(msg.Payload) {
		t.Error("Payload is not valid JSON")
	}

	// From and To
	if msg.From != "did:key:zAlice" {
		t.Errorf("From: got %q", msg.From)
	}
	if msg.To != "did:key:zBob" {
		t.Errorf("To: got %q", msg.To)
	}
}

func TestMessageEncodeDecodeRoundTrip(t *testing.T) {
	payload := ProposePayload{
		Capability: "translate",
		Approach:   "Using neural MT",
	}
	orig, err := NewMessage(TypePropose, "did:key:zAlice", "did:key:zBob", "conv-456", payload)
	if err != nil {
		t.Fatalf("NewMessage() error: %v", err)
	}
	orig.Signature = "test-sig-base64"

	data, err := orig.Encode()
	if err != nil {
		t.Fatalf("Encode() error: %v", err)
	}

	decoded, err := DecodeMessage(data)
	if err != nil {
		t.Fatalf("DecodeMessage() error: %v", err)
	}

	if decoded.MsgID != orig.MsgID {
		t.Errorf("MsgID mismatch: %q != %q", decoded.MsgID, orig.MsgID)
	}
	if decoded.Type != orig.Type {
		t.Errorf("Type mismatch: %q != %q", decoded.Type, orig.Type)
	}
	if decoded.Version != orig.Version {
		t.Errorf("Version mismatch")
	}
	if decoded.From != orig.From {
		t.Errorf("From mismatch")
	}
	if decoded.To != orig.To {
		t.Errorf("To mismatch")
	}
	if decoded.ConversationID != orig.ConversationID {
		t.Errorf("ConversationID mismatch")
	}
	if decoded.Signature != orig.Signature {
		t.Errorf("Signature mismatch: %q != %q", decoded.Signature, orig.Signature)
	}
	if decoded.TTL != orig.TTL {
		t.Errorf("TTL mismatch")
	}

	// Verify payload round-trip
	var decodedPayload ProposePayload
	if err := json.Unmarshal(decoded.Payload, &decodedPayload); err != nil {
		t.Fatalf("unmarshal decoded payload: %v", err)
	}
	if decodedPayload.Capability != "translate" {
		t.Errorf("payload Capability: got %q", decodedPayload.Capability)
	}
	if decodedPayload.Approach != "Using neural MT" {
		t.Errorf("payload Approach: got %q", decodedPayload.Approach)
	}
}

func TestSignableBytes(t *testing.T) {
	msg, err := NewMessage(TypePing, "did:key:zA", "did:key:zB", "conv-789", nil)
	if err != nil {
		t.Fatalf("NewMessage() error: %v", err)
	}

	// Set a signature
	msg.Signature = "should-not-appear"

	signable, err := msg.SignableBytes()
	if err != nil {
		t.Fatalf("SignableBytes() error: %v", err)
	}

	// The signable bytes must NOT contain the signature value
	if strings.Contains(string(signable), "should-not-appear") {
		t.Error("SignableBytes() contains the signature field value")
	}

	// The original message signature must remain unchanged
	if msg.Signature != "should-not-appear" {
		t.Error("SignableBytes() modified the original message")
	}

	// SignableBytes must be deterministic
	signable2, err := msg.SignableBytes()
	if err != nil {
		t.Fatalf("second SignableBytes() error: %v", err)
	}
	if string(signable) != string(signable2) {
		t.Error("SignableBytes() is not deterministic")
	}
}

func TestNewConversationID(t *testing.T) {
	id := NewConversationID()

	// ULID is 26 characters
	if len(id) != 26 {
		t.Errorf("ConversationID length: got %d, want 26", len(id))
	}

	// Must be unique
	id2 := NewConversationID()
	if id == id2 {
		t.Error("two ConversationIDs are identical")
	}

	// Must be uppercase alphanumeric (ULID encoding: 0-9A-Z excluding I,L,O,U)
	for _, c := range id {
		if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z')) {
			t.Errorf("ConversationID contains invalid character: %c", c)
			break
		}
	}
}

func TestConversationFSM(t *testing.T) {
	// Test the full happy path: created -> intent -> propose -> accept -> deliver -> receipt(completed)
	conv := NewConversation("test-conv", "did:key:zAlice")

	if conv.State != StateCreated {
		t.Fatalf("initial state: got %q, want %q", conv.State, StateCreated)
	}

	transitions := []struct {
		msgType       MessageType
		expectedState NegotiationState
	}{
		{TypeIntent, StateCreated},   // INTENT keeps state as created
		{TypePropose, StateProposed}, // PROPOSE moves to proposed
		{TypeAccept, StateAccepted},  // ACCEPT moves to accepted
		{TypeDeliver, StateDelivered},
		{TypeReceipt, StateCompleted},
	}

	for i, tr := range transitions {
		msg := &Message{Type: tr.msgType, From: "did:key:zBob"}
		if err := conv.Transition(msg); err != nil {
			t.Fatalf("step %d (%s): Transition() error: %v", i, tr.msgType, err)
		}
		if conv.State != tr.expectedState {
			t.Errorf("step %d (%s): state = %q, want %q", i, tr.msgType, conv.State, tr.expectedState)
		}
	}
}

func TestConversationFSMCounter(t *testing.T) {
	// Test counter flow: created -> intent -> propose -> counter -> counter -> accept -> deliver -> receipt
	conv := NewConversation("test-counter", "did:key:zAlice")

	steps := []struct {
		msgType       MessageType
		expectedState NegotiationState
	}{
		{TypeIntent, StateCreated},
		{TypePropose, StateProposed},
		{TypeCounter, StateCountered},
		{TypeCounter, StateCountered}, // multiple counters allowed
		{TypeAccept, StateAccepted},
		{TypeDeliver, StateDelivered},
		{TypeReceipt, StateCompleted},
	}

	for i, s := range steps {
		msg := &Message{Type: s.msgType, From: "did:key:zBob"}
		if err := conv.Transition(msg); err != nil {
			t.Fatalf("step %d (%s): error: %v", i, s.msgType, err)
		}
		if conv.State != s.expectedState {
			t.Errorf("step %d (%s): state = %q, want %q", i, s.msgType, conv.State, s.expectedState)
		}
	}
}

func TestConversationInvalidTransition(t *testing.T) {
	conv := NewConversation("test-invalid", "did:key:zAlice")

	// From created, DELIVER is not allowed
	msg := &Message{Type: TypeDeliver, From: "did:key:zBob"}
	err := conv.Transition(msg)
	if err == nil {
		t.Error("expected error for invalid transition created -> DELIVER")
	}

	// From created, RECEIPT is not allowed
	msg = &Message{Type: TypeReceipt, From: "did:key:zBob"}
	err = conv.Transition(msg)
	if err == nil {
		t.Error("expected error for invalid transition created -> RECEIPT")
	}

	// Move to proposed
	conv.Transition(&Message{Type: TypeIntent, From: "did:key:zAlice"})
	conv.Transition(&Message{Type: TypePropose, From: "did:key:zBob"})

	// From proposed, DELIVER is not allowed
	msg = &Message{Type: TypeDeliver, From: "did:key:zBob"}
	err = conv.Transition(msg)
	if err == nil {
		t.Error("expected error for invalid transition proposed -> DELIVER")
	}
}

func TestConversationTerminalState(t *testing.T) {
	terminalStates := []struct {
		name    string
		setup   func() *Conversation
	}{
		{
			name: "completed",
			setup: func() *Conversation {
				c := NewConversation("t1", "did:key:zA")
				c.Transition(&Message{Type: TypeIntent, From: "did:key:zA"})
				c.Transition(&Message{Type: TypePropose, From: "did:key:zB"})
				c.Transition(&Message{Type: TypeAccept, From: "did:key:zA"})
				c.Transition(&Message{Type: TypeDeliver, From: "did:key:zB"})
				c.Transition(&Message{Type: TypeReceipt, From: "did:key:zA"})
				return c
			},
		},
		{
			name: "rejected",
			setup: func() *Conversation {
				c := NewConversation("t2", "did:key:zA")
				c.Transition(&Message{Type: TypeReject, From: "did:key:zB"})
				return c
			},
		},
		{
			name: "cancelled",
			setup: func() *Conversation {
				c := NewConversation("t3", "did:key:zA")
				c.Transition(&Message{Type: TypeIntent, From: "did:key:zA"})
				c.Transition(&Message{Type: TypePropose, From: "did:key:zB"})
				c.Transition(&Message{Type: TypeCancel, From: "did:key:zA"})
				return c
			},
		},
	}

	for _, tc := range terminalStates {
		t.Run(tc.name, func(t *testing.T) {
			conv := tc.setup()

			// Any transition from a terminal state must fail
			for _, mt := range []MessageType{TypeIntent, TypePropose, TypeAccept, TypeDeliver, TypeReceipt, TypeCancel} {
				err := conv.Transition(&Message{Type: mt, From: "did:key:zX"})
				if err == nil {
					t.Errorf("expected error for transition from %s with %s, got nil", conv.State, mt)
				}
			}
		})
	}
}

func TestConversationStoreCreateGet(t *testing.T) {
	store := NewConversationStore()

	conv := store.Create("conv-1", "did:key:zAlice")
	if conv == nil {
		t.Fatal("Create() returned nil")
	}
	if conv.ID != "conv-1" {
		t.Errorf("conv.ID: got %q", conv.ID)
	}
	if conv.Initiator != "did:key:zAlice" {
		t.Errorf("conv.Initiator: got %q", conv.Initiator)
	}

	// Get existing
	got, ok := store.Get("conv-1")
	if !ok {
		t.Fatal("Get() returned false for existing conversation")
	}
	if got.ID != "conv-1" {
		t.Errorf("Get().ID: got %q", got.ID)
	}

	// Get non-existent
	_, ok = store.Get("conv-nonexistent")
	if ok {
		t.Error("Get() returned true for non-existent conversation")
	}
}

func TestConversationStoreList(t *testing.T) {
	store := NewConversationStore()

	// Empty store
	if len(store.List()) != 0 {
		t.Error("List() on empty store is not empty")
	}

	store.Create("c1", "did:key:zA")
	store.Create("c2", "did:key:zB")
	store.Create("c3", "did:key:zC")

	list := store.List()
	if len(list) != 3 {
		t.Errorf("List() length: got %d, want 3", len(list))
	}

	// All IDs must be present
	ids := make(map[string]bool)
	for _, c := range list {
		ids[c.ID] = true
	}
	for _, expected := range []string{"c1", "c2", "c3"} {
		if !ids[expected] {
			t.Errorf("List() missing conversation %q", expected)
		}
	}
}

func TestConversationStoreConcurrency(t *testing.T) {
	store := NewConversationStore()
	const n = 100

	var wg sync.WaitGroup

	// Concurrent Creates
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			id := NewConversationID()
			store.Create(id, "did:key:zAgent")
		}(i)
	}
	wg.Wait()

	list := store.List()
	if len(list) != n {
		t.Errorf("after %d concurrent Creates: List() length = %d", n, len(list))
	}

	// Concurrent Gets
	ids := make([]string, len(list))
	for i, c := range list {
		ids[i] = c.ID
	}

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			_, ok := store.Get(ids[i])
			if !ok {
				t.Errorf("concurrent Get(%q) returned false", ids[i])
			}
		}(i)
	}
	wg.Wait()
}
