package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"go.fd.io/govpp/adapter/socketclient"
	"go.fd.io/govpp/api"
	"go.fd.io/govpp/binapi/vpe"
	"go.fd.io/govpp/core"
)

// PoC: Minimal VPP connection test
//
// This program verifies govpp compatibility with VPP 24.10 by:
// 1. Connecting to VPP via socket
// 2. Executing ShowVersion API call
// 3. Verifying VPP responds correctly
//
// Prerequisites:
// - VPP 24.10 installed and running
// - /run/vpp/api.sock accessible (requires root or vpp group membership)
//
// Note: This PoC uses govpp's built-in binapi/vpe (no custom binapi generation required).
// Full binapi generation for VPP 24.10 is deferred to Task 1.1.

const (
	defaultSocketPath = "/run/vpp/api.sock"
	connectTimeout    = 10 * time.Second
)

func main() {
	socketPath := getSocketPath()

	fmt.Println("==================================================")
	fmt.Println("  VPP Connection PoC - govpp v0.13.0 + VPP 24.10")
	fmt.Println("==================================================")
	fmt.Printf("Socket: %s\n", socketPath)
	fmt.Println("")

	// Step 1: Create adapter
	fmt.Println("[1/4] Creating socket adapter...")
	adapter := socketclient.NewVppClient(socketPath)

	// Step 2: Connect to VPP
	fmt.Println("[2/4] Connecting to VPP...")
	conn, connEvent, err := core.AsyncConnect(adapter, core.DefaultMaxReconnectAttempts, core.DefaultReconnectInterval)
	if err != nil {
		log.Fatalf("FAILED: AsyncConnect error: %v\n", err)
	}
	defer conn.Disconnect()

	// Wait for connection event
	select {
	case e := <-connEvent:
		if e.State != core.Connected {
			log.Fatalf("FAILED: Connection failed (state: %v)\n\nTroubleshooting:\n"+
				"- Check if VPP is running: sudo systemctl status vpp\n"+
				"- Verify socket exists: ls -l %s\n"+
				"- Check permissions: sudo chmod 666 %s\n", e.State, socketPath, socketPath)
		}
		fmt.Println("✓ Connected to VPP")
	case <-time.After(connectTimeout):
		log.Fatalf("FAILED: Connection timeout\n")
	}

	// Step 3: Create API channel
	fmt.Println("[3/4] Creating API channel...")
	ch, err := conn.NewAPIChannel()
	if err != nil {
		log.Fatalf("FAILED: API channel creation error: %v\n", err)
	}
	defer ch.Close()
	fmt.Println("✓ API channel created")

	// Step 4: Execute ShowVersion
	fmt.Println("[4/4] Executing ShowVersion API call...")
	if err := showVersion(ch); err != nil {
		log.Fatalf("FAILED: ShowVersion error: %v\n", err)
	}

	fmt.Println("")
	fmt.Println("==================================================")
	fmt.Println("  PoC: SUCCESS - Connection Established")
	fmt.Println("==================================================")
	fmt.Println("")
	fmt.Println("govpp v0.13.0 successfully connected to VPP")
	fmt.Println("Verify the VPP version above matches 24.10.x")
	fmt.Println("")
	fmt.Println("Next: Update docs/govpp-compatibility.md with findings")
}

func getSocketPath() string {
	if path := os.Getenv("VPP_API_SOCKET_PATH"); path != "" {
		return path
	}
	return defaultSocketPath
}

func showVersion(ch api.Channel) error {
	req := &vpe.ShowVersion{}
	reply := &vpe.ShowVersionReply{}

	if err := ch.SendRequest(req).ReceiveReply(reply); err != nil {
		return fmt.Errorf("API call failed: %w", err)
	}

	fmt.Println("✓ ShowVersion succeeded")
	fmt.Println("")
	fmt.Println("VPP Information:")
	fmt.Printf("  Version:    %s\n", reply.Version)
	fmt.Printf("  Build Date: %s\n", reply.BuildDate)
	fmt.Printf("  Build Dir:  %s\n", reply.BuildDirectory)

	return nil
}
