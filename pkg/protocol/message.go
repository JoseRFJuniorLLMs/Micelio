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
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
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
