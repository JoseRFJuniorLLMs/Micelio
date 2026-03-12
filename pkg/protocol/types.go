// Package protocol implements the AIP message format and negotiation state machine.
package protocol

// MessageType defines the AIP message types per the specification.
type MessageType string

const (
	// Core negotiation flow
	TypeIntent  MessageType = "INTENT"
	TypePropose MessageType = "PROPOSE"
	TypeCounter MessageType = "COUNTER"
	TypeAccept  MessageType = "ACCEPT"
	TypeReject  MessageType = "REJECT"
	TypeDeliver MessageType = "DELIVER"
	TypeReceipt MessageType = "RECEIPT"
	TypeCancel  MessageType = "CANCEL"

	// Discovery
	TypeDiscover            MessageType = "DISCOVER"
	TypeCapabilityAdvertise MessageType = "CAPABILITY_ADVERTISE"
	TypeCapabilityQuery     MessageType = "CAPABILITY_QUERY"

	// System
	TypePing  MessageType = "PING"
	TypePong  MessageType = "PONG"
	TypeError MessageType = "ERROR"
)

// NegotiationState tracks the state of a negotiation conversation.
type NegotiationState string

const (
	StateCreated    NegotiationState = "created"
	StateProposed   NegotiationState = "proposed"
	StateCountered  NegotiationState = "countered"
	StateAccepted   NegotiationState = "accepted"
	StateRejected   NegotiationState = "rejected"
	StateDelivered  NegotiationState = "delivered"
	StateCompleted  NegotiationState = "completed"
	StateCancelled  NegotiationState = "cancelled"
	StateTimedOut   NegotiationState = "timed_out"
)

// ProtocolID is the libp2p protocol identifier for AIP.
const ProtocolID = "/aip/0.1.0"

// Version is the AIP protocol version string.
const Version = "aip/0.1"
