package protocol

import (
	"fmt"
	"sync"
	"time"
)

// Conversation tracks the state of a negotiation between two agents.
type Conversation struct {
	ID        string
	State     NegotiationState
	Initiator string
	Responder string
	Messages  []*Message
	CreatedAt time.Time
	UpdatedAt time.Time
	mu        sync.Mutex
}

// NewConversation creates a new negotiation conversation.
func NewConversation(id, initiator string) *Conversation {
	now := time.Now().UTC()
	return &Conversation{
		ID:        id,
		State:     StateCreated,
		Initiator: initiator,
		Messages:  make([]*Message, 0),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// validTransitions defines the allowed state transitions in the negotiation FSM.
var validTransitions = map[NegotiationState]map[MessageType]NegotiationState{
	StateCreated: {
		TypeIntent: StateCreated, // INTENT initializes the conversation
	},
	// After INTENT is sent, we expect PROPOSE or REJECT
	StateCreated + "_sent": {
		TypePropose: StateProposed,
		TypeReject:  StateRejected,
		TypeCounter: StateCountered,
	},
}

// Transition applies a message to the conversation, advancing the state machine.
func (c *Conversation) Transition(msg *Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.validateTransition(msg.Type); err != nil {
		return err
	}

	c.Messages = append(c.Messages, msg)
	c.UpdatedAt = time.Now().UTC()

	if c.Responder == "" && msg.From != c.Initiator {
		c.Responder = msg.From
	}

	switch msg.Type {
	case TypeIntent:
		// State stays created, waiting for response
	case TypePropose:
		c.State = StateProposed
	case TypeCounter:
		c.State = StateCountered
	case TypeAccept:
		c.State = StateAccepted
	case TypeReject:
		c.State = StateRejected
	case TypeDeliver:
		c.State = StateDelivered
	case TypeReceipt:
		c.State = StateCompleted
	case TypeCancel:
		c.State = StateCancelled
	}

	return nil
}

func (c *Conversation) validateTransition(msgType MessageType) error {
	allowed := map[NegotiationState][]MessageType{
		StateCreated:   {TypeIntent, TypePropose, TypeReject},
		StateProposed:  {TypeAccept, TypeReject, TypeCounter, TypeCancel},
		StateCountered: {TypeAccept, TypeReject, TypeCounter, TypeCancel},
		StateAccepted:  {TypeDeliver, TypeCancel},
		StateDelivered: {TypeReceipt},
	}

	validTypes, ok := allowed[c.State]
	if !ok {
		return fmt.Errorf("conversation %s is in terminal state %s", c.ID, c.State)
	}

	for _, t := range validTypes {
		if t == msgType {
			return nil
		}
	}

	return fmt.Errorf("invalid transition: %s -> %s (conversation %s)", c.State, msgType, c.ID)
}

// ConversationStore manages active conversations.
type ConversationStore struct {
	conversations map[string]*Conversation
	mu            sync.RWMutex
}

// NewConversationStore creates a new conversation store.
func NewConversationStore() *ConversationStore {
	return &ConversationStore{
		conversations: make(map[string]*Conversation),
	}
}

// Create starts a new conversation.
func (s *ConversationStore) Create(id, initiator string) *Conversation {
	s.mu.Lock()
	defer s.mu.Unlock()
	conv := NewConversation(id, initiator)
	s.conversations[id] = conv
	return conv
}

// Get retrieves a conversation by ID.
func (s *ConversationStore) Get(id string) (*Conversation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	conv, ok := s.conversations[id]
	return conv, ok
}

// List returns all active conversations.
func (s *ConversationStore) List() []*Conversation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Conversation, 0, len(s.conversations))
	for _, conv := range s.conversations {
		result = append(result, conv)
	}
	return result
}
