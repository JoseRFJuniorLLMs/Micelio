package network

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/JoseRFJuniorLLMs/Micelio/pkg/protocol"
)

// newStreamHandler creates a libp2p stream handler that decodes AIP messages.
func newStreamHandler(onMessage func(peer.ID, *protocol.Message)) network.StreamHandler {
	return func(s network.Stream) {
		defer s.Close()
		remotePeer := s.Conn().RemotePeer()

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
