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
		if len(args) < 1 {
			fmt.Fprintf(os.Stderr, "Error: 'show bgp' requires a subcommand (summary or neighbor)\n\n")
			showUsage()
			return ExitUsageError
		}
		subcommand := args[0]
		switch subcommand {
		case "summary":
			if len(args) > 1 {
				fmt.Fprintf(os.Stderr, "Error: 'show bgp summary' does not accept extra arguments\n\n")
				showUsage()
				return ExitUsageError
			}
			return cmdShowBGPSummary(ctx, f)
		case "neighbor":
			if len(args) < 2 {
				fmt.Fprintf(os.Stderr, "Error: 'show bgp neighbor' requires an IP address\n\n")
				showUsage()
				return ExitUsageError
			}
			if len(args) > 2 {
				fmt.Fprintf(os.Stderr, "Error: 'show bgp neighbor' accepts only one IP address\n\n")
				showUsage()
				return ExitUsageError
			}
			return cmdShowBGPNeighbor(ctx, args[1], f)
		default:
			fmt.Fprintf(os.Stderr, "Error: unknown bgp subcommand '%s' (valid: summary, neighbor)\n\n", subcommand)
			showUsage()
			return ExitUsageError
		}

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
		// 'show route' or 'show route protocol <proto>'
		return cmdShowRoute(ctx, args, f)

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
