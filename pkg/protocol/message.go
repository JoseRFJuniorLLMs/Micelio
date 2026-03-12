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

// SignableBytes returns the canonical bytes to sign (message without signature field).
func (m *Message) SignableBytes() ([]byte, error) {
	cp := *m
	cp.Signature = ""
	return json.Marshal(cp)
}
