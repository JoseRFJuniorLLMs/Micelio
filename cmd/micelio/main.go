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
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"

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
	fmt.Println("  micelio keygen [--output FILE]             Generate a new agent identity")
	fmt.Println("  micelio agent [--name NAME] [--port PORT]  Start an agent node")
	fmt.Println("                [--identity FILE]")
	fmt.Println("  micelio info --identity FILE               Show identity info")
	fmt.Println()
	fmt.Println("Run the two-agent demo:")
	fmt.Println("  go run ./examples/two_agents/")
	fmt.Println()
	fmt.Println("Environment Variables:")
	fmt.Println("  MICELIO_NAME              Agent name (required, default: micelio-node)")
	fmt.Println("  MICELIO_PORT              Agent listen port (default: 9000, range: 0 or 1024-65535)")
	fmt.Println("  MICELIO_IDENTITY          Path to identity JSON file")
	fmt.Println("  MICELIO_NIETZSCHE_ADDR    NietzscheDB gRPC address (host:port, e.g., localhost:50051)")
	fmt.Println("  MICELIO_REPUTATION_FILE   Path to reputation persistence file")
	fmt.Println("  MICELIO_ENABLE_DHT        Enable DHT discovery (true/1)")
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
	name := ""
	port := -1 // sentinel: not set via flag
	identityFile := ""

	for i := 2; i < len(os.Args)-1; i++ {
		switch os.Args[i] {
		case "--port", "-p":
			p, err := strconv.Atoi(os.Args[i+1])
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: invalid port %q: %v\n", os.Args[i+1], err)
				os.Exit(1)
			}
			port = p
		case "--identity", "-i":
			identityFile = os.Args[i+1]
		case "--name", "-n":
			name = os.Args[i+1]
		}
	}

	// ---- Environment variable fallbacks with validation ----

	// Name: flag > env > default
	if name == "" {
		name = os.Getenv("MICELIO_NAME")
	}
	if name == "" {
		name = "micelio-node"
	}

	// Port: flag > env > default (9000)
	if port < 0 {
		if envPort := os.Getenv("MICELIO_PORT"); envPort != "" {
			p, err := strconv.Atoi(envPort)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: MICELIO_PORT %q is not a valid integer: %v\n", envPort, err)
				os.Exit(1)
			}
			port = p
		} else {
			port = 9000
		}
	}
	if port < 0 || port > 65535 {
		fmt.Fprintf(os.Stderr, "error: port %d out of range (0-65535)\n", port)
		os.Exit(1)
	}

	if identityFile == "" {
		identityFile = os.Getenv("MICELIO_IDENTITY")
	}

	nietzscheAddr := os.Getenv("MICELIO_NIETZSCHE_ADDR")
	if nietzscheAddr != "" {
		// Validate host:port format
		host, _, err := net.SplitHostPort(nietzscheAddr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: MICELIO_NIETZSCHE_ADDR %q is not valid host:port: %v\n", nietzscheAddr, err)
			os.Exit(1)
		}
		if strings.TrimSpace(host) == "" {
			fmt.Fprintf(os.Stderr, "error: MICELIO_NIETZSCHE_ADDR host part must not be empty\n")
			os.Exit(1)
		}
	}

	reputationFile := os.Getenv("MICELIO_REPUTATION_FILE")

	dhtEnv := os.Getenv("MICELIO_ENABLE_DHT")
	enableDHT := dhtEnv == "true" || dhtEnv == "1"
	if dhtEnv != "" && !enableDHT && dhtEnv != "false" && dhtEnv != "0" {
		fmt.Fprintf(os.Stderr, "warning: MICELIO_ENABLE_DHT=%q not recognized, expected true/1/false/0\n", dhtEnv)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	cfg := agent.Config{
		Name:           name,
		Port:           port,
		NietzscheAddr:  nietzscheAddr,
		ReputationFile: reputationFile,
		EnableDHT:      enableDHT,
	}

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

	fmt.Println("Micelio Agent started")
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
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal info: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(data))
}
