package protocol

import (
	"fmt"
	"sync"
	"time"
)

const (
	// DefaultConversationTimeout is the default timeout for conversations.
	DefaultConversationTimeout = 5 * time.Minute

	// DefaultMaxCounterRounds is the maximum number of COUNTER messages allowed.
	DefaultMaxCounterRounds = 10
)

// Conversation tracks the state of a negotiation between two agents.
type Conversation struct {
	ID               string
	State            NegotiationState
	Initiator        string
	Responder        string
	Messages         []*Message
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Timeout          time.Duration
	MaxCounterRounds int
	mu               sync.Mutex
}

// NewConversation creates a new negotiation conversation.
func NewConversation(id, initiator string) *Conversation {
	now := time.Now().UTC()
	return &Conversation{
		ID:               id,
		State:            StateCreated,
		Initiator:        initiator,
		Messages:         make([]*Message, 0),
		CreatedAt:        now,
		UpdatedAt:        now,
		Timeout:          DefaultConversationTimeout,
		MaxCounterRounds: DefaultMaxCounterRounds,
	}
}

// IsTimedOut returns true if the conversation has exceeded its timeout duration.
func (c *Conversation) IsTimedOut() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Timeout > 0 && time.Since(c.UpdatedAt) > c.Timeout
}

// counterCount returns the number of COUNTER messages in the conversation.
func (c *Conversation) counterCount() int {
	count := 0
	for _, m := range c.Messages {
		if m.Type == TypeCounter {
			count++
		}
	}
	return count
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
			// Check counter round limit
			if msgType == TypeCounter && c.counterCount() >= c.MaxCounterRounds {
				return fmt.Errorf("conversation %s exceeded max counter rounds (%d)", c.ID, c.MaxCounterRounds)
			}
			return nil
		}
	}

	return fmt.Errorf("invalid transition: %s -> %s (conversation %s)", c.State, msgType, c.ID)
}

// ConversationStore manages active conversations and message deduplication.
type ConversationStore struct {
	conversations map[string]*Conversation
	seenMsgIDs    map[string]time.Time // FIX 3: message deduplication
	mu            sync.RWMutex
}

// NewConversationStore creates a new conversation store.
func NewConversationStore() *ConversationStore {
	return &ConversationStore{
		conversations: make(map[string]*Conversation),
		seenMsgIDs:    make(map[string]time.Time),
	}
}

// HasSeen returns true if a message ID has already been processed.
func (s *ConversationStore) HasSeen(msgID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.seenMsgIDs[msgID]
	return ok
}

// MarkSeen records a message ID as processed.
func (s *ConversationStore) MarkSeen(msgID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seenMsgIDs[msgID] = time.Now()
}

// CleanupSeen removes seen message IDs older than 5 minutes.
func (s *ConversationStore) CleanupSeen() {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-5 * time.Minute)
	for id, ts := range s.seenMsgIDs {
		if ts.Before(cutoff) {
			delete(s.seenMsgIDs, id)
		}
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

// Remove deletes a conversation from the store.
func (s *ConversationStore) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conversations, id)
}

// TimeoutConversations marks all expired conversations as StateTimedOut.
// It returns the IDs of conversations that were timed out.
func (s *ConversationStore) TimeoutConversations() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	var timedOut []string
	for id, conv := range s.conversations {
		conv.mu.Lock()
		if conv.Timeout > 0 && time.Since(conv.UpdatedAt) > conv.Timeout {
			// Only timeout non-terminal conversations
			switch conv.State {
			case StateCompleted, StateCancelled, StateRejected, StateTimedOut:
				// Already in terminal state, skip
			default:
				conv.State = StateTimedOut
				timedOut = append(timedOut, id)
			}
		}
		conv.mu.Unlock()
	}
	return timedOut
}
