// Two-agent demo: the "absurdly simple" proof of concept.
//
// Agent Alice wants text translated (INTENT).
// Agent Bob offers to translate (PROPOSE).
// Alice accepts (ACCEPT).
// Bob delivers the translation (DELIVER).
// Alice confirms receipt (RECEIPT).
//
// No server. No API key. No marketplace. Just agents talking P2P.
//
// Usage: go run ./examples/two_agents/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/JoseRFJuniorLLMs/Micelio/pkg/agent"
	"github.com/JoseRFJuniorLLMs/Micelio/pkg/protocol"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	fmt.Println("=== Micélio AIP Demo — Two Agents, No Server ===")
	fmt.Println()

	// --- Create Agent Alice (port 9000) ---
	alice, err := agent.New(ctx, agent.Config{Name: "Alice", Port: 9000})
	if err != nil {
		fmt.Fprintf(os.Stderr, "create Alice: %v\n", err)
		os.Exit(1)
	}
	defer alice.Close()

	// --- Create Agent Bob (port 9001) ---
	bob, err := agent.New(ctx, agent.Config{Name: "Bob", Port: 9001})
	if err != nil {
		fmt.Fprintf(os.Stderr, "create Bob: %v\n", err)
		os.Exit(1)
	}
	defer bob.Close()

	fmt.Printf("Alice DID: %s\n", alice.DID())
	fmt.Printf("Bob   DID: %s\n", bob.DID())
	fmt.Printf("Alice Peer: %s\n", alice.PeerID())
	fmt.Printf("Bob   Peer: %s\n", bob.PeerID())
	fmt.Println()

	// Register Bob's capability
	bob.RegisterCapability(agent.Capability{
		Name:        "nlp.translate",
		Description: "Translate text between languages",
		Version:     "1.0.0",
	})

	// Channel to synchronize the demo flow
	done := make(chan struct{})

	// --- Bob's handlers ---

	// Bob handles INTENT: responds with PROPOSE
	bob.OnMessage(protocol.TypeIntent, func(from peer.ID, msg *protocol.Message) *protocol.Message {
		var intent protocol.IntentPayload
		json.Unmarshal(msg.Payload, &intent)

		fmt.Printf("\n[Bob] received INTENT: %q\n", intent.Description)
		fmt.Printf("[Bob] capability requested: %s\n", intent.Capability)

		// Bob proposes to do the work
		propose := protocol.ProposePayload{
			Capability: intent.Capability,
			Approach:   "Neural translation using local model, no external API needed",
			Conditions: []string{"text must be under 1000 chars"},
		}

		reply, _ := protocol.NewMessage(
			protocol.TypePropose,
			bob.DID(),
			msg.From,
			msg.ConversationID,
			propose,
		)
		fmt.Printf("[Bob] sending PROPOSE: %q\n", propose.Approach)
		return reply
	})

	// Bob handles ACCEPT: does the work and delivers
	bob.OnMessage(protocol.TypeAccept, func(from peer.ID, msg *protocol.Message) *protocol.Message {
		fmt.Printf("\n[Bob] received ACCEPT — doing the work...\n")
		time.Sleep(500 * time.Millisecond) // simulate work

		result := map[string]string{
			"original":    "Hello, how are you?",
			"translated":  "Olá, como você está?",
			"source_lang": "en",
			"target_lang": "pt",
		}
		resultJSON, _ := json.Marshal(result)

		deliver := protocol.DeliverPayload{
			Result: resultJSON,
			Metadata: map[string]any{
				"model":    "local-nmt-v3",
				"duration": "0.5s",
			},
		}

		reply, _ := protocol.NewMessage(
			protocol.TypeDeliver,
			bob.DID(),
			msg.From,
			msg.ConversationID,
			deliver,
		)
		fmt.Printf("[Bob] sending DELIVER: translation complete\n")
		return reply
	})

	// --- Alice's handlers ---

	// Alice handles PROPOSE: auto-accepts
	alice.OnMessage(protocol.TypePropose, func(from peer.ID, msg *protocol.Message) *protocol.Message {
		var propose protocol.ProposePayload
		json.Unmarshal(msg.Payload, &propose)

		fmt.Printf("\n[Alice] received PROPOSE: %q\n", propose.Approach)
		fmt.Printf("[Alice] sending ACCEPT\n")

		reply, _ := protocol.NewMessage(
			protocol.TypeAccept,
			alice.DID(),
			msg.From,
			msg.ConversationID,
			nil,
		)
		return reply
	})

	// Alice handles DELIVER: confirms with RECEIPT
	alice.OnMessage(protocol.TypeDeliver, func(from peer.ID, msg *protocol.Message) *protocol.Message {
		var deliver protocol.DeliverPayload
		json.Unmarshal(msg.Payload, &deliver)

		var result map[string]string
		json.Unmarshal(deliver.Result, &result)

		fmt.Printf("\n[Alice] received DELIVER:\n")
		fmt.Printf("  Original:   %s\n", result["original"])
		fmt.Printf("  Translated: %s\n", result["translated"])

		receipt := protocol.ReceiptPayload{
			Accepted: true,
			Rating:   5,
			Feedback: "Perfect translation!",
		}

		reply, _ := protocol.NewMessage(
			protocol.TypeReceipt,
			alice.DID(),
			msg.From,
			msg.ConversationID,
			receipt,
		)
		fmt.Printf("[Alice] sending RECEIPT (rating: 5/5)\n")

		go func() {
			time.Sleep(100 * time.Millisecond)
			close(done)
		}()

		return reply
	})

	// Bob handles RECEIPT: negotiation complete
	bob.OnMessage(protocol.TypeReceipt, func(from peer.ID, msg *protocol.Message) *protocol.Message {
		var receipt protocol.ReceiptPayload
		json.Unmarshal(msg.Payload, &receipt)

		fmt.Printf("\n[Bob] received RECEIPT: accepted=%v, rating=%d/5\n", receipt.Accepted, receipt.Rating)
		fmt.Printf("[Bob] feedback: %q\n", receipt.Feedback)
		return nil
	})

	// --- Connect Alice to Bob directly ---
	bobInfo := bob.Host.AddrInfo()
	if err := alice.Host.P2P.Connect(ctx, bobInfo); err != nil {
		fmt.Fprintf(os.Stderr, "connect Alice->Bob: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Alice connected to Bob via libp2p")

	// Give the connection a moment to stabilize
	time.Sleep(200 * time.Millisecond)

	// --- Alice sends INTENT ---
	fmt.Println()
	fmt.Println("--- Negotiation Start ---")
	fmt.Println()

	intent := protocol.IntentPayload{
		Capability:  "nlp.translate",
		Description: "Translate 'Hello, how are you?' from English to Portuguese",
		Params: map[string]any{
			"text":        "Hello, how are you?",
			"source_lang": "en",
			"target_lang": "pt",
		},
	}

	fmt.Printf("[Alice] sending INTENT: %q\n", intent.Description)
	conv, err := alice.SendIntent(ctx, bob.PeerID(), intent)
	if err != nil {
		fmt.Fprintf(os.Stderr, "send intent: %v\n", err)
		os.Exit(1)
	}
	_ = conv

	// Wait for negotiation to complete or timeout
	select {
	case <-done:
		fmt.Println()
		fmt.Println("--- Negotiation Complete ---")
		fmt.Println()
		fmt.Println("Full flow: INTENT → PROPOSE → ACCEPT → DELIVER → RECEIPT")
		fmt.Println("No server. No API key. No central authority.")
		fmt.Println("Just two agents negotiating peer-to-peer.")
		fmt.Println()
		fmt.Println("🍄 Micélio — The Mycelium Network for Autonomous Agents")
	case <-time.After(10 * time.Second):
		fmt.Println("\nTimeout waiting for negotiation to complete")
	case <-ctx.Done():
		fmt.Println("\nInterrupted")
	}
}
