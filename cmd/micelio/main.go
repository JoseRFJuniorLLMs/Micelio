// Command micelio is the CLI for running an AIP agent node.
//
// Usage:
//
//	micelio agent --port 9000                    # Start an agent node
//	micelio agent --port 9000 --identity id.json # Start with saved identity
//	micelio keygen --output id.json              # Generate a new identity
//	micelio demo                                 # Run the two-agent demo
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"

	"github.com/JoseRFJuniorLLMs/Micelio/pkg/agent"
	"github.com/JoseRFJuniorLLMs/Micelio/pkg/identity"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "keygen":
		cmdKeygen()
	case "agent":
		cmdAgent()
	case "info":
		cmdInfo()
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Micélio — Agent Internet Protocol (AIP)")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  micelio keygen [--output FILE]   Generate a new agent identity")
	fmt.Println("  micelio agent [--port PORT] [--identity FILE]  Start an agent node")
	fmt.Println("  micelio info --identity FILE     Show identity info")
	fmt.Println()
	fmt.Println("Run the two-agent demo:")
	fmt.Println("  go run ./examples/two_agents/")
}

func cmdKeygen() {
	output := "identity.json"
	for i := 2; i < len(os.Args)-1; i++ {
		if os.Args[i] == "--output" || os.Args[i] == "-o" {
			output = os.Args[i+1]
		}
	}

	id, err := identity.Generate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate identity: %v\n", err)
		os.Exit(1)
	}

	if err := id.Save(output); err != nil {
		fmt.Fprintf(os.Stderr, "save identity: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Identity generated:\n")
	fmt.Printf("  DID:  %s\n", id.DID)
	fmt.Printf("  File: %s\n", output)
}

func cmdAgent() {
	port := 9000
	identityFile := ""

	for i := 2; i < len(os.Args)-1; i++ {
		switch os.Args[i] {
		case "--port", "-p":
			p, err := strconv.Atoi(os.Args[i+1])
			if err != nil {
				fmt.Fprintf(os.Stderr, "invalid port: %s\n", os.Args[i+1])
				os.Exit(1)
			}
			port = p
		case "--identity", "-i":
			identityFile = os.Args[i+1]
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	cfg := agent.Config{Port: port}

	if identityFile != "" {
		id, err := identity.Load(identityFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "load identity: %v\n", err)
			os.Exit(1)
		}
		cfg.Identity = id
	}

	a, err := agent.New(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create agent: %v\n", err)
		os.Exit(1)
	}
	defer a.Close()

	fmt.Println("Micélio Agent started")
	fmt.Println(a.Info())
	fmt.Println()
	fmt.Println("Listening for peers... (Ctrl+C to stop)")

	<-ctx.Done()
	fmt.Println("\nShutting down...")
}

func cmdInfo() {
	identityFile := ""
	for i := 2; i < len(os.Args)-1; i++ {
		if os.Args[i] == "--identity" || os.Args[i] == "-i" {
			identityFile = os.Args[i+1]
		}
	}

	if identityFile == "" {
		fmt.Fprintln(os.Stderr, "usage: micelio info --identity FILE")
		os.Exit(1)
	}

	id, err := identity.Load(identityFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load identity: %v\n", err)
		os.Exit(1)
	}

	info := map[string]string{
		"did":  id.DID,
		"file": identityFile,
	}
	data, _ := json.MarshalIndent(info, "", "  ")
	fmt.Println(string(data))
}
