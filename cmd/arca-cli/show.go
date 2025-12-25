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

// cmdShowInterfaces is implemented in show_interfaces.go

// cmdShowBGPSummary is implemented in show_routing.go

// cmdShowOSPFNeighbor is implemented in show_routing.go

// cmdShowRoute is implemented in show_routing.go
