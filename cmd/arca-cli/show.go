package main

import (
	"context"
	"fmt"
	"os"
)

func cmdShow(ctx context.Context, subcommand string, args []string, f *flags) int {
	switch subcommand {
	case "configuration":
		return cmdShowConfiguration(ctx, args, f)

	case "interfaces":
		return cmdShowInterfaces(ctx, args, f)

	case "bgp":
		if len(args) < 1 || args[0] != "summary" {
			fmt.Fprintf(os.Stderr, "Error: 'show bgp' requires 'summary' subcommand\n\n")
			showUsage()
			return ExitUsageError
		}
		if len(args) > 1 {
			fmt.Fprintf(os.Stderr, "Error: 'show bgp summary' does not accept extra arguments\n\n")
			showUsage()
			return ExitUsageError
		}
		return cmdShowBGPSummary(ctx, f)

	case "ospf":
		if len(args) < 1 || args[0] != "neighbor" {
			fmt.Fprintf(os.Stderr, "Error: 'show ospf' requires 'neighbor' subcommand\n\n")
			showUsage()
			return ExitUsageError
		}
		if len(args) > 1 {
			fmt.Fprintf(os.Stderr, "Error: 'show ospf neighbor' does not accept extra arguments\n\n")
			showUsage()
			return ExitUsageError
		}
		return cmdShowOSPFNeighbor(ctx, f)

	case "route":
		if len(args) > 0 {
			fmt.Fprintf(os.Stderr, "Error: 'show route' does not accept extra arguments\n\n")
			showUsage()
			return ExitUsageError
		}
		return cmdShowRoute(ctx, f)

	default:
		fmt.Fprintf(os.Stderr, "Error: unknown show subcommand '%s'\n\n", subcommand)
		showUsage()
		return ExitUsageError
	}
}

// cmdShowConfiguration is implemented in show_config.go

// cmdShowInterfaces displays interface status (implemented in show_interfaces.go - Task 5.4)
func cmdShowInterfaces(ctx context.Context, args []string, f *flags) int {
	fmt.Fprintf(os.Stderr, "Error: 'show interfaces' not yet implemented (Task 5.4)\n")
	return ExitOperationError
}

// cmdShowBGPSummary displays BGP summary (implemented in show_routing.go - Task 5.5)
func cmdShowBGPSummary(ctx context.Context, f *flags) int {
	fmt.Fprintf(os.Stderr, "Error: 'show bgp summary' not yet implemented (Task 5.5)\n")
	return ExitOperationError
}

// cmdShowOSPFNeighbor displays OSPF neighbors (implemented in show_routing.go - Task 5.5)
func cmdShowOSPFNeighbor(ctx context.Context, f *flags) int {
	fmt.Fprintf(os.Stderr, "Error: 'show ospf neighbor' not yet implemented (Task 5.5)\n")
	return ExitOperationError
}

// cmdShowRoute displays routing table (implemented in show_routing.go - Task 5.5)
func cmdShowRoute(ctx context.Context, f *flags) int {
	fmt.Fprintf(os.Stderr, "Error: 'show route' not yet implemented (Task 5.5)\n")
	return ExitOperationError
}
