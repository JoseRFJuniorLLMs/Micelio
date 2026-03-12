package network

import (
	"encoding/binary"
	"io"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/JoseRFJuniorLLMs/Micelio/pkg/logging"
	"github.com/JoseRFJuniorLLMs/Micelio/pkg/protocol"
)

// Default stream timeouts. These can be overridden via StreamTimeoutConfig.
const (
	DefaultStreamReadTimeout  = 30 * time.Second
	DefaultStreamWriteTimeout = 30 * time.Second
)

// MaxMessageSize is the maximum allowed message size (1 MB).
// Shared with the Python SDK for consistency.
const MaxMessageSize = 1 << 20

// StreamTimeoutConfig holds configurable timeouts for stream I/O.
type StreamTimeoutConfig struct {
	// ReadTimeout is the deadline for reading the header and payload from
	// an incoming stream. Defaults to 30s.
	ReadTimeout time.Duration

	// WriteTimeout is the deadline for writing responses on a stream.
	// Defaults to 30s.
	WriteTimeout time.Duration
}

// applyDefaults fills in zero-valued fields with defaults.
func (c *StreamTimeoutConfig) applyDefaults() {
	if c.ReadTimeout <= 0 {
		c.ReadTimeout = DefaultStreamReadTimeout
	}
	if c.WriteTimeout <= 0 {
		c.WriteTimeout = DefaultStreamWriteTimeout
	}
}

// newStreamHandler creates a libp2p stream handler that decodes AIP messages.
// It uses the default timeout values.
func newStreamHandler(log *logging.Logger, rateLimiter *PeerRateLimiter, onMessage func(peer.ID, *protocol.Message)) network.StreamHandler {
	cfg := StreamTimeoutConfig{}
	cfg.applyDefaults()
	return newStreamHandlerWithConfig(log, rateLimiter, onMessage, cfg)
}

// newStreamHandlerWithConfig creates a libp2p stream handler with explicit
// timeout configuration.
func newStreamHandlerWithConfig(log *logging.Logger, rateLimiter *PeerRateLimiter, onMessage func(peer.ID, *protocol.Message), cfg StreamTimeoutConfig) network.StreamHandler {
	cfg.applyDefaults()

	return func(s network.Stream) {
		defer s.Close()
		remotePeer := s.Conn().RemotePeer()
		peerStr := remotePeer.String()[:12]

		// Check rate limit before processing
		if rateLimiter != nil && !rateLimiter.Allow(remotePeer) {
			log.Warn("rate limited peer", logging.String("peer", peerStr))
			return
		}

		// Set read deadline for header
		if err := s.SetReadDeadline(time.Now().Add(cfg.ReadTimeout)); err != nil {
			log.Error("set read deadline failed", logging.String("peer", peerStr), logging.Err(err))
			return
		}

		// Set write deadline so we don't hang on writes either.
		if err := s.SetWriteDeadline(time.Now().Add(cfg.WriteTimeout)); err != nil {
			log.Error("set write deadline failed", logging.String("peer", peerStr), logging.Err(err))
			return
		}

		// Read length-prefixed message
		var length uint32
		if err := binary.Read(s, binary.BigEndian, &length); err != nil {
			if err != io.EOF {
				log.Error("read header failed", logging.String("peer", peerStr), logging.Err(err))
			}
			return
		}

		// Validate message size BEFORE allocating buffer to prevent DoS
		if length == 0 {
			log.Warn("zero-length message", logging.String("peer", peerStr))
			return
		}
		if length > MaxMessageSize {
			log.Warn("message too large", logging.String("peer", peerStr), logging.Int("bytes", int(length)))
			return
		}

		// Refresh read deadline for the payload (may be large).
		if err := s.SetReadDeadline(time.Now().Add(cfg.ReadTimeout)); err != nil {
			log.Error("set read deadline failed", logging.String("peer", peerStr), logging.Err(err))
			return
		}

		data := make([]byte, length)
		if _, err := io.ReadFull(s, data); err != nil {
			log.Error("read payload failed", logging.String("peer", peerStr), logging.Err(err))
			return
		}

		msg, err := protocol.DecodeMessage(data)
		if err != nil {
			log.Error("decode message failed", logging.String("peer", peerStr), logging.Err(err))
			return
		}

		// Validate message fields before dispatching
		if err := protocol.ValidateMessage(msg); err != nil {
			log.Warn("invalid message", logging.String("peer", peerStr), logging.Err(err))
			return
		}

		onMessage(remotePeer, msg)
	}
}
