package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func cmdVersion(ctx context.Context, f *flags) int {
	fmt.Printf("arca-router CLI\n")
	fmt.Printf("  Version:    %s\n", Version)
	fmt.Printf("  Commit:     %s\n", Commit)
	fmt.Printf("  Build Date: %s\n", BuildDate)
	fmt.Printf("\n")

	// VPP version
	vppVersion := getVPPVersion(ctx, f)
	fmt.Printf("VPP:  %s\n", vppVersion)

	// FRR version
	frrVersion := getFRRVersion(ctx, f)
	fmt.Printf("FRR:  %s\n", frrVersion)

	return ExitSuccess
}

// getVPPVersion retrieves VPP version via VPP API
func getVPPVersion(ctx context.Context, f *flags) string {
	debugLog(f, "Fetching VPP version")

	// Create VPP client
	client, err := createVPPClient(f)
	if err != nil {
		debugLog(f, "Failed to create VPP client: %v", err)
		return "N/A (client creation failed)"
	}
	defer client.Close()

	// Connect to VPP
	if err := client.Connect(ctx); err != nil {
		debugLog(f, "Failed to connect to VPP: %v", err)
		return "N/A (not running)"
	}

	// Get VPP version using ShowVersion API
	version, err := client.GetVersion(ctx)
	if err != nil {
		debugLog(f, "Failed to get VPP version: %v", err)
		return "N/A (API error)"
	}

	debugLog(f, "VPP version: %s", version)
	return version
}

// getFRRVersion retrieves FRR version via vtysh
func getFRRVersion(ctx context.Context, f *flags) string {
	debugLog(f, "Fetching FRR version")

	// Locate vtysh binary
	vtyshPath, err := exec.LookPath("vtysh")
	if err != nil {
		debugLog(f, "vtysh not found: %v", err)
		return "N/A (not installed)"
	}

	// Execute: vtysh --version
	cmd := exec.CommandContext(ctx, vtyshPath, "--version")
	output, err := cmd.Output()
	if err != nil {
		debugLog(f, "Failed to get FRR version: %v", err)
		return "N/A (not running)"
	}

	// Parse version from output
	// FRR version output format: "FRRouting 8.x.x (frrVersion)"
	version := strings.TrimSpace(string(output))
	lines := strings.Split(version, "\n")
	if len(lines) > 0 {
		// First line usually contains the version
		firstLine := strings.TrimSpace(lines[0])
		debugLog(f, "FRR version: %s", firstLine)
		return firstLine
	}

	debugLog(f, "Unable to parse FRR version from output")
	return "N/A (parse error)"
}
