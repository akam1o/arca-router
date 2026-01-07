package main

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// cmdShowBGPSummary displays BGP summary via vtysh
func cmdShowBGPSummary(ctx context.Context, f *flags) int {
	debugLog(f, "Executing 'show bgp summary' via vtysh")

	output, err := RunVtyshCommand(ctx, "show bgp summary", f)
	if err != nil {
		printVtyshError(err, "bgpd")
		return ExitOperationError
	}

	// Display output as-is
	fmt.Print(output)
	return ExitSuccess
}

// cmdShowBGPNeighbor displays BGP neighbor details via vtysh
func cmdShowBGPNeighbor(ctx context.Context, neighborIP string, f *flags) int {
	// IP validation (allow both IPv4 and IPv6)
	if neighborIP == "" {
		fmt.Fprintf(os.Stderr, "Error: 'show bgp neighbor' requires an IP address\n")
		return ExitUsageError
	}

	// Validate IP address format
	if !isValidIP(neighborIP) {
		fmt.Fprintf(os.Stderr, "Error: invalid IP address '%s'\n", neighborIP)
		return ExitUsageError
	}

	command := fmt.Sprintf("show bgp neighbor %s", neighborIP)
	debugLog(f, "Executing '%s' via vtysh", command)

	output, err := RunVtyshCommand(ctx, command, f)
	if err != nil {
		printVtyshError(err, "bgpd")
		return ExitOperationError
	}

	// Display output as-is
	fmt.Print(output)
	return ExitSuccess
}

// isValidIP checks if a string is a valid IPv4 or IPv6 address
func isValidIP(ip string) bool {
	// Simple validation: check if it contains valid IP characters
	// More thorough validation would require net.ParseIP, but we accept
	// relaxed validation here to allow FRR to handle edge cases
	if len(ip) == 0 {
		return false
	}

	// Must contain at least one separator (. for IPv4, : for IPv6)
	hasSeparator := false
	for _, ch := range ip {
		if ch == '.' || ch == ':' {
			hasSeparator = true
		}
	}
	if !hasSeparator {
		return false
	}

	// Basic character check for IPv4/IPv6
	for _, ch := range ip {
		isDigit := ch >= '0' && ch <= '9'
		isLowerHex := ch >= 'a' && ch <= 'f'
		isUpperHex := ch >= 'A' && ch <= 'F'
		isSeparator := ch == '.' || ch == ':'
		if !isDigit && !isLowerHex && !isUpperHex && !isSeparator {
			return false
		}
	}
	return true
}

// cmdShowOSPFNeighbor displays OSPF neighbors via vtysh
func cmdShowOSPFNeighbor(ctx context.Context, f *flags) int {
	debugLog(f, "Executing 'show ip ospf neighbor' via vtysh")

	output, err := RunVtyshCommand(ctx, "show ip ospf neighbor", f)
	if err != nil {
		printVtyshError(err, "ospfd")
		return ExitOperationError
	}

	// Display output as-is
	fmt.Print(output)
	return ExitSuccess
}

// cmdShowRoute displays routing table via vtysh
// Supports optional protocol filtering: show route protocol <proto>
func cmdShowRoute(ctx context.Context, args []string, f *flags) int {
	command := "show ip route"

	// Parse optional protocol filter
	if len(args) > 0 {
		if args[0] != "protocol" {
			fmt.Fprintf(os.Stderr, "Error: 'show route' accepts 'protocol <proto>' or no arguments\n")
			return ExitUsageError
		}
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Error: 'protocol' requires a protocol name\n")
			return ExitUsageError
		}
		if len(args) > 2 {
			fmt.Fprintf(os.Stderr, "Error: 'show route protocol' does not accept extra arguments\n")
			return ExitUsageError
		}
		protocol := args[1]
		// Validate protocol
		validProtocols := map[string]bool{
			"bgp":       true,
			"ospf":      true,
			"static":    true,
			"connected": true,
			"kernel":    true,
		}
		if !validProtocols[protocol] {
			fmt.Fprintf(os.Stderr, "Error: invalid protocol '%s'. Valid: bgp, ospf, static, connected, kernel\n", protocol)
			return ExitUsageError
		}
		command = fmt.Sprintf("show ip route %s", protocol)
	}

	debugLog(f, "Executing '%s' via vtysh", command)

	output, err := RunVtyshCommand(ctx, command, f)
	if err != nil {
		printVtyshError(err, "zebra")
		return ExitOperationError
	}

	// Display output as-is
	fmt.Print(output)
	return ExitSuccess
}

// printVtyshError prints appropriate error message based on error type
func printVtyshError(err error, daemon string) {
	var vtyshErr *VtyshError
	if !errors.As(err, &vtyshErr) {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}

	fmt.Fprintf(os.Stderr, "Error: %s\n", vtyshErr.Message)

	// Print stderr output if available
	if vtyshErr.Output != "" {
		fmt.Fprintf(os.Stderr, "%s\n", vtyshErr.Output)
	}

	// Print appropriate hint based on error type
	switch vtyshErr.Type {
	case VtyshErrorNotFound:
		fmt.Fprintf(os.Stderr, "Hint: Ensure FRR is installed (vtysh command not found)\n")
	case VtyshErrorTimeout:
		fmt.Fprintf(os.Stderr, "Hint: FRR may be unresponsive or overloaded\n")
	case VtyshErrorExitCode:
		fmt.Fprintf(os.Stderr, "Hint: Ensure FRR is running and %s daemon is enabled\n", daemon)
	case VtyshErrorExec:
		fmt.Fprintf(os.Stderr, "Hint: Check FRR installation and permissions\n")
	}
}
