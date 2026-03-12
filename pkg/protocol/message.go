package protocol

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"
)

// Message is the AIP v0.1 envelope. All fields are required except where noted.
type Message struct {
	Version        string          `json:"version"`
	MsgID          string          `json:"msg_id"`
	Type           MessageType     `json:"type"`
	From           string          `json:"from"`
	To             string          `json:"to"`
	ConversationID string          `json:"conversation_id"`
	Timestamp      time.Time       `json:"timestamp"`
	TTL            int             `json:"ttl"`
	Payload        json.RawMessage `json:"payload"`
	Signature      string          `json:"signature,omitempty"`
}

// IntentPayload is the payload for INTENT messages.
type IntentPayload struct {
	Capability  string            `json:"capability"`
	Description string            `json:"description"`
	Params      map[string]any    `json:"params,omitempty"`
	Budget      *Budget           `json:"budget,omitempty"`
	Deadline    *time.Time        `json:"deadline,omitempty"`
}

// ProposePayload is the payload for PROPOSE messages.
type ProposePayload struct {
	Capability string         `json:"capability"`
	Approach   string         `json:"approach"`
	Cost       *Budget        `json:"cost,omitempty"`
	ETA        *time.Duration `json:"eta,omitempty"`
	Conditions []string       `json:"conditions,omitempty"`
}

// DeliverPayload is the payload for DELIVER messages.
type DeliverPayload struct {
	Result     json.RawMessage `json:"result"`
	Metadata   map[string]any  `json:"metadata,omitempty"`
}

// ReceiptPayload is the payload for RECEIPT messages.
type ReceiptPayload struct {
	Accepted bool   `json:"accepted"`
	Rating   int    `json:"rating,omitempty"` // 1-5
	Feedback string `json:"feedback,omitempty"`
}

// Budget represents a cost/budget constraint.
type Budget struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

// CapabilityAd is the payload for CAPABILITY_ADVERTISE.
type CapabilityAd struct {
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Description string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema,omitempty"`
	OutputSchema json.RawMessage `json:"output_schema,omitempty"`
	Tags        []string        `json:"tags,omitempty"`
}

// NewMessage creates a new AIP message with auto-generated ID and timestamp.
func NewMessage(msgType MessageType, from, to, conversationID string, payload any) (*Message, error) {
	id, err := ulid.New(ulid.Timestamp(time.Now()), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ULID: %w", err)
	}

	var payloadBytes json.RawMessage
	if payload != nil {
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal payload: %w", err)
		}
	}

	return &Message{
		Version:        Version,
		MsgID:          id.String(),
		Type:           msgType,
		From:           from,
		To:             to,
		ConversationID: conversationID,
		Timestamp:      time.Now().UTC(),
		TTL:            60,
		Payload:        payloadBytes,
	}, nil
}

// NewConversationID generates a new ULID for a conversation.
func NewConversationID() string {
	id, _ := ulid.New(ulid.Timestamp(time.Now()), rand.Reader)
	return id.String()
}

// Encode serializes a message to JSON bytes.
func (m *Message) Encode() ([]byte, error) {
	return json.Marshal(m)
}

// DecodeMessage deserializes a message from JSON bytes.
func DecodeMessage(data []byte) (*Message, error) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("decode message: %w", err)
	}
	return &msg, nil
}

// maxClockSkew is the maximum allowed clock difference for TTL checks.
const maxClockSkew = 30 * time.Second

// ValidateTTL checks whether the message has expired based on its TTL and timestamp.
// It allows up to maxClockSkew for messages slightly in the future.
func (m *Message) ValidateTTL() error {
	age := time.Since(m.Timestamp)
	if age < -maxClockSkew {
		return fmt.Errorf("message timestamp is too far in the future (%v ahead)", -age)
	}
	if m.TTL > 0 && age > time.Duration(m.TTL)*time.Second {
		return fmt.Errorf("message expired: age %v exceeds TTL %ds", age, m.TTL)
	}
	return nil
}

// IsExpired returns true if the message TTL has been exceeded.
func (m *Message) IsExpired() bool {
	if m.TTL <= 0 {
		return false
	}
	return time.Since(m.Timestamp) > time.Duration(m.TTL)*time.Second
}

// SignableBytes returns the canonical bytes to sign (message without signature field).
func (m *Message) SignableBytes() ([]byte, error) {
	cp := *m
	cp.Signature = ""
	return json.Marshal(cp)
}

// CounterPayload for COUNTER messages (modified proposal).
type CounterPayload struct {
	Capability string         `json:"capability"`
	Approach   string         `json:"approach"`
	Cost       *Budget        `json:"cost,omitempty"`
	ETA        *time.Duration `json:"eta,omitempty"`
	Conditions []string       `json:"conditions,omitempty"`
	Reason     string         `json:"reason"` // why the original was countered
}

// RejectPayload for REJECT messages.
type RejectPayload struct {
	Reason string `json:"reason"`
	Code   string `json:"code,omitempty"` // e.g. "budget_exceeded", "capability_mismatch"
}

// CancelPayload for CANCEL messages.
type CancelPayload struct {
	Reason string `json:"reason"`
}

// ErrorPayload for ERROR messages.
type ErrorPayload struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// PingPayload for PING/PONG messages.
type PingPayload struct {
	Nonce     string    `json:"nonce"`
	Timestamp time.Time `json:"timestamp"`
}

const (
	// MaxTTL is the maximum allowed TTL in seconds (1 hour).
	MaxTTL = 3600

	// MaxFieldLength is the maximum allowed length for string fields (From, To, ConversationID).
	MaxFieldLength = 512
)

// validMessageTypes is the set of known AIP message types.
var validMessageTypes = map[MessageType]bool{
	TypeIntent:              true,
	TypePropose:             true,
	TypeCounter:             true,
	TypeAccept:              true,
	TypeReject:              true,
	TypeDeliver:             true,
	TypeReceipt:             true,
	TypeCancel:              true,
	TypeDiscover:            true,
	TypeCapabilityAdvertise: true,
	TypeCapabilityQuery:     true,
	TypePing:                true,
	TypePong:                true,
	TypeError:               true,
}

// Validate checks the message for valid required fields, known type, and
// reasonable TTL. It is a method form of ValidateMessage.
func (m *Message) Validate() error {
	return ValidateMessage(m)
}

// ValidateMessage checks that a message has valid, non-empty required fields,
// a known message type, and reasonable TTL values. Returns an error describing
// the first validation failure, or nil if the message is valid.
func ValidateMessage(msg *Message) error {
	if msg == nil {
		return fmt.Errorf("message is nil")
	}

	// Validate From
	if msg.From == "" {
		return fmt.Errorf("message field 'from' is empty")
	}
	if len(msg.From) > MaxFieldLength {
		return fmt.Errorf("message field 'from' exceeds max length (%d > %d)", len(msg.From), MaxFieldLength)
	}

	// Validate To
	if msg.To == "" {
		return fmt.Errorf("message field 'to' is empty")
	}
	if len(msg.To) > MaxFieldLength {
		return fmt.Errorf("message field 'to' exceeds max length (%d > %d)", len(msg.To), MaxFieldLength)
	}

	// Validate ConversationID
	if msg.ConversationID == "" {
		return fmt.Errorf("message field 'conversation_id' is empty")
	}
	if len(msg.ConversationID) > MaxFieldLength {
		return fmt.Errorf("message field 'conversation_id' exceeds max length (%d > %d)", len(msg.ConversationID), MaxFieldLength)
	}

	// Validate MsgID
	if msg.MsgID == "" {
		return fmt.Errorf("message field 'msg_id' is empty")
	}

	// Validate message type
	if !validMessageTypes[msg.Type] {
		return fmt.Errorf("unknown message type: %q", msg.Type)
	}

	// Validate TTL
	if msg.TTL < 0 {
		return fmt.Errorf("message TTL is negative: %d", msg.TTL)
	}
	if msg.TTL > MaxTTL {
		return fmt.Errorf("message TTL exceeds maximum (%d > %d)", msg.TTL, MaxTTL)
	}

	// Validate timestamp is not zero
	if msg.Timestamp.IsZero() {
		return fmt.Errorf("message timestamp is zero")
	}

	// Validate payload is present for types that require it
	switch msg.Type {
	case TypeIntent:
		if len(msg.Payload) == 0 {
			return fmt.Errorf("INTENT message requires a payload")
		}
		var intent IntentPayload
		if err := json.Unmarshal(msg.Payload, &intent); err != nil {
			return fmt.Errorf("INTENT payload invalid: %w", err)
		}
		if intent.Capability == "" {
			return fmt.Errorf("INTENT payload missing required field 'capability'")
		}

	case TypePropose:
		if len(msg.Payload) == 0 {
			return fmt.Errorf("PROPOSE message requires a payload")
		}
		var propose ProposePayload
		if err := json.Unmarshal(msg.Payload, &propose); err != nil {
			return fmt.Errorf("PROPOSE payload invalid: %w", err)
		}
		if propose.Capability == "" {
			return fmt.Errorf("PROPOSE payload missing required field 'capability'")
		}

	case TypeDeliver:
		if len(msg.Payload) == 0 {
			return fmt.Errorf("DELIVER message requires a payload")
		}

	case TypeCounter:
		if len(msg.Payload) == 0 {
			return fmt.Errorf("COUNTER message requires a payload")
		}
		var counter CounterPayload
		if err := json.Unmarshal(msg.Payload, &counter); err != nil {
			return fmt.Errorf("COUNTER payload invalid: %w", err)
		}

	case TypeAccept, TypeReceipt, TypeCancel, TypeReject,
		TypePing, TypePong, TypeDiscover,
		TypeCapabilityAdvertise, TypeCapabilityQuery, TypeError:
		// These types may or may not have payloads; no strict requirement.
	}

	return nil
}
