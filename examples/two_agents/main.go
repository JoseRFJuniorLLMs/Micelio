// Two-agent demo with NietzscheDB cognition (L6).
//
// Agent Alice wants text translated (INTENT).
// Agent Bob offers to translate (PROPOSE).
// Alice accepts (ACCEPT).
// Bob delivers the translation (DELIVER).
// Alice confirms receipt (RECEIPT).
//
// With NietzscheDB enabled (--nietzsche flag):
//   - Alice remembers the negotiation (episodic memory)
//   - Alice tracks Bob's trust score (Poincare ball)
//   - Bob's capability is cached for future lookups
//
// Usage:
//
//	go run ./examples/two_agents/
//	go run ./examples/two_agents/ --nietzsche localhost:50051
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/JoseRFJuniorLLMs/Micelio/pkg/agent"
	"github.com/JoseRFJuniorLLMs/Micelio/pkg/protocol"
)

func main() {
	nietzscheAddr := flag.String("nietzsche", "", "NietzscheDB gRPC address (e.g. localhost:50051)")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	fmt.Println("=== Micelio AIP Demo — Two Agents, No Server ===")
	if *nietzscheAddr != "" {
		fmt.Printf("    L6 Cognition: NietzscheDB at %s\n", *nietzscheAddr)
	}
	fmt.Println()

	// --- Create Agent Alice (port 9000) ---
	alice, err := agent.New(ctx, agent.Config{
		Name:          "Alice",
		Port:          9000,
		NietzscheAddr: *nietzscheAddr,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "create Alice: %v\n", err)
		os.Exit(1)
	}
	defer alice.Close()

	// --- Create Agent Bob (port 9001) ---
	bob, err := agent.New(ctx, agent.Config{
		Name:          "Bob",
		Port:          9001,
		NietzscheAddr: *nietzscheAddr,
	})
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

	bob.OnMessage(protocol.TypeIntent, func(from peer.ID, msg *protocol.Message) *protocol.Message {
		var intent protocol.IntentPayload
		if err := json.Unmarshal(msg.Payload, &intent); err != nil {
			fmt.Fprintf(os.Stderr, "[Bob] unmarshal intent: %v\n", err)
			return nil
		}

		fmt.Printf("\n[Bob] received INTENT: %q\n", intent.Description)
		fmt.Printf("[Bob] capability requested: %s\n", intent.Capability)

		propose := protocol.ProposePayload{
			Capability: intent.Capability,
			Approach:   "Neural translation using local model, no external API needed",
			Conditions: []string{"text must be under 1000 chars"},
		}

		reply, err := protocol.NewMessage(
			protocol.TypePropose,
			bob.DID(),
			msg.From,
			msg.ConversationID,
			propose,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[Bob] create propose message: %v\n", err)
			return nil
		}
		fmt.Printf("[Bob] sending PROPOSE: %q\n", propose.Approach)
		return reply
	})

	bob.OnMessage(protocol.TypeAccept, func(from peer.ID, msg *protocol.Message) *protocol.Message {
		fmt.Printf("\n[Bob] received ACCEPT — doing the work...\n")
		time.Sleep(500 * time.Millisecond)

		result := map[string]string{
			"original":    "Hello, how are you?",
			"translated":  "Ola, como voce esta?",
			"source_lang": "en",
			"target_lang": "pt",
		}
		resultJSON, err := json.Marshal(result)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[Bob] marshal result: %v\n", err)
			return nil
		}

		deliver := protocol.DeliverPayload{
			Result: resultJSON,
			Metadata: map[string]any{
				"model":    "local-nmt-v3",
				"duration": "0.5s",
			},
		}

		reply, err := protocol.NewMessage(
			protocol.TypeDeliver,
			bob.DID(),
			msg.From,
			msg.ConversationID,
			deliver,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[Bob] create deliver message: %v\n", err)
			return nil
		}
		fmt.Printf("[Bob] sending DELIVER: translation complete\n")
		return reply
	})

	// --- Alice's handlers ---

	alice.OnMessage(protocol.TypePropose, func(from peer.ID, msg *protocol.Message) *protocol.Message {
		var propose protocol.ProposePayload
		if err := json.Unmarshal(msg.Payload, &propose); err != nil {
			fmt.Fprintf(os.Stderr, "[Alice] unmarshal propose: %v\n", err)
			return nil
		}

		fmt.Printf("\n[Alice] received PROPOSE: %q\n", propose.Approach)

		// With cognition: check trust before accepting
		if alice.Cognition != nil {
			trust := alice.Cognition.GetTrustScore(ctx, msg.From)
			fmt.Printf("[Alice] L6: Bob's trust score = %.2f\n", trust)
		}

		fmt.Printf("[Alice] sending ACCEPT\n")

		reply, err := protocol.NewMessage(
			protocol.TypeAccept,
			alice.DID(),
			msg.From,
			msg.ConversationID,
			nil,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[Alice] create accept message: %v\n", err)
			return nil
		}
		return reply
	})

	alice.OnMessage(protocol.TypeDeliver, func(from peer.ID, msg *protocol.Message) *protocol.Message {
		var deliver protocol.DeliverPayload
		if err := json.Unmarshal(msg.Payload, &deliver); err != nil {
			fmt.Fprintf(os.Stderr, "[Alice] unmarshal deliver: %v\n", err)
			return nil
		}

		var result map[string]string
		if err := json.Unmarshal(deliver.Result, &result); err != nil {
			fmt.Fprintf(os.Stderr, "[Alice] unmarshal deliver result: %v\n", err)
			return nil
		}

		fmt.Printf("\n[Alice] received DELIVER:\n")
		fmt.Printf("  Original:   %s\n", result["original"])
		fmt.Printf("  Translated: %s\n", result["translated"])

		receipt := protocol.ReceiptPayload{
			Accepted: true,
			Rating:   5,
			Feedback: "Perfect translation!",
		}

		fmt.Printf("[Alice] sending RECEIPT (rating: 5/5)\n")

		// Use SendReceipt to trigger L6 memory recording
		if err := alice.SendReceipt(ctx, from, msg.ConversationID, receipt); err != nil {
			fmt.Fprintf(os.Stderr, "[Alice] send receipt: %v\n", err)
		}

		go func() {
			time.Sleep(200 * time.Millisecond)

			// With cognition: show what was stored
			if alice.Cognition != nil {
				fmt.Println()
				fmt.Println("--- L6 Cognition Report ---")

				trust := alice.Cognition.GetTrustScore(ctx, bob.DID())
				fmt.Printf("[L6] Bob's trust score after interaction: %.2f\n", trust)

				history, err := alice.Cognition.GetRecentNegotiations(ctx, 5)
				if err != nil {
					fmt.Fprintf(os.Stderr, "[L6] get recent negotiations: %v\n", err)
				} else {
					fmt.Printf("[L6] Total negotiations in memory: %d\n", len(history))
				}

				caps, err := alice.Cognition.FindPeersWithCapability(ctx, "nlp.translate", 0.0, 5)
				if err != nil {
					fmt.Fprintf(os.Stderr, "[L6] find peers with capability: %v\n", err)
				} else {
					fmt.Printf("[L6] Peers cached with nlp.translate: %d\n", len(caps))
				}
			}

			close(done)
		}()

		// Return nil since we already sent via SendReceipt
		return nil
	})

	bob.OnMessage(protocol.TypeReceipt, func(from peer.ID, msg *protocol.Message) *protocol.Message {
		var receipt protocol.ReceiptPayload
		if err := json.Unmarshal(msg.Payload, &receipt); err != nil {
			fmt.Fprintf(os.Stderr, "[Bob] unmarshal receipt: %v\n", err)
			return nil
		}

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
		fmt.Println("Full flow: INTENT -> PROPOSE -> ACCEPT -> DELIVER -> RECEIPT")
		fmt.Println("No server. No API key. No central authority.")
		fmt.Println("Just two agents negotiating peer-to-peer.")
		if *nietzscheAddr != "" {
			fmt.Println()
			fmt.Println("L6 active: trust, memory, and capabilities stored in NietzscheDB.")
			fmt.Println("The hyperbolic brain remembers. The next negotiation will be smarter.")
		}
		fmt.Println()
		fmt.Println("Micelio — The Mycelium Network for Autonomous Agents")
	case <-time.After(10 * time.Second):
		fmt.Println("\nTimeout waiting for negotiation to complete")
	case <-ctx.Done():
		fmt.Println("\nInterrupted")
	}
}
