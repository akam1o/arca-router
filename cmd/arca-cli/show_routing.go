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
func cmdShowRoute(ctx context.Context, f *flags) int {
	debugLog(f, "Executing 'show ip route' via vtysh")

	output, err := RunVtyshCommand(ctx, "show ip route", f)
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
