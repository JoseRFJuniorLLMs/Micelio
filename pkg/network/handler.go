package network

import (
	"encoding/binary"
	"fmt"
	"io"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/JoseRFJuniorLLMs/Micelio/pkg/protocol"
)

const streamReadTimeout = 30 * time.Second

// newStreamHandler creates a libp2p stream handler that decodes AIP messages.
func newStreamHandler(rateLimiter *PeerRateLimiter, onMessage func(peer.ID, *protocol.Message)) network.StreamHandler {
	return func(s network.Stream) {
		defer s.Close()
		remotePeer := s.Conn().RemotePeer()

		// Check rate limit before processing
		if rateLimiter != nil && !rateLimiter.Allow(remotePeer) {
			fmt.Printf("[handler] rate limited peer %s\n", remotePeer.String()[:12])
			return
		}

		// Set read deadline for header
		if err := s.SetReadDeadline(time.Now().Add(streamReadTimeout)); err != nil {
			fmt.Printf("[handler] set read deadline from %s: %v\n", remotePeer.String()[:12], err)
			return
		}

		// Read length-prefixed message
		var length uint32
		if err := binary.Read(s, binary.BigEndian, &length); err != nil {
			if err != io.EOF {
				fmt.Printf("[handler] read header from %s: %v\n", remotePeer.String()[:12], err)
			}
			return
		}

		if length > 1<<20 { // 1MB max message size
			fmt.Printf("[handler] message too large from %s: %d bytes\n", remotePeer.String()[:12], length)
			return
		}

		// Set read deadline for payload
		if err := s.SetReadDeadline(time.Now().Add(streamReadTimeout)); err != nil {
			fmt.Printf("[handler] set read deadline from %s: %v\n", remotePeer.String()[:12], err)
			return
		}

		data := make([]byte, length)
		if _, err := io.ReadFull(s, data); err != nil {
			fmt.Printf("[handler] read payload from %s: %v\n", remotePeer.String()[:12], err)
			return
		}

		msg, err := protocol.DecodeMessage(data)
		if err != nil {
			fmt.Printf("[handler] decode message from %s: %v\n", remotePeer.String()[:12], err)
			return
		}

		onMessage(remotePeer, msg)
	}
}
