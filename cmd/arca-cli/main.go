package main

import (
	"context"
	"flag"
	"fmt"
	"os"
)

var (
	// Version information (set by ldflags during build)
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

// Exit codes
const (
	ExitSuccess       = 0
	ExitOperationError = 1
	ExitUsageError    = 2
)

type flags struct {
	debug       bool
	vppSocket   string
	configPath  string
}

func main() {
	// Parse command line flags
	f := parseFlags()

	ctx := context.Background()

	// If no command is provided, start interactive mode
	if flag.NArg() < 1 {
		exitCode := cmdInteractive(ctx, f)
		os.Exit(exitCode)
	}

	command := flag.Arg(0)

	// Dispatch command
	exitCode := dispatch(ctx, command, flag.Args()[1:], f)
	os.Exit(exitCode)
}

func parseFlags() *flags {
	f := &flags{}

	flag.BoolVar(&f.debug, "debug", false,
		"Enable debug output to stderr")
	flag.StringVar(&f.vppSocket, "socket", "/run/vpp/api.sock",
		"Path to VPP API socket")
	flag.StringVar(&f.configPath, "config", "/etc/arca-router/arca.conf",
		"Path to configuration file")

	flag.Usage = showUsage
	flag.Parse()

	return f
}

func dispatch(ctx context.Context, command string, args []string, f *flags) int {
	debugLog(f, "Dispatching command: %s, args: %v", command, args)

	switch command {
	case "help", "-h", "--help":
		showHelp()
		return ExitSuccess

	case "version", "-v", "--version":
		debugLog(f, "Executing version command")
		return cmdVersion(ctx, f)

	case "show":
		if len(args) < 1 {
			fmt.Fprintf(os.Stderr, "Error: 'show' requires a subcommand\n\n")
			showUsage()
			return ExitUsageError
		}
		debugLog(f, "Executing show subcommand: %s", args[0])
		return cmdShow(ctx, args[0], args[1:], f)

	default:
		fmt.Fprintf(os.Stderr, "Error: unknown command '%s'\n\n", command)
		showUsage()
		return ExitUsageError
	}
}

func showUsage() {
	fmt.Fprintf(os.Stderr, `Usage: arca-cli [options] [command] [args...]

Interactive Mode:
  arca-cli                    Start interactive CLI shell (no command given)

Commands:
  help              Show this help message
  version           Show version information
  show <subcommand> Show configuration or status

Show subcommands:
  configuration               Show full configuration
  configuration interfaces    Show interface configuration
  configuration protocols     Show routing protocol configuration
  interfaces                  Show interface status
  interfaces <name>           Show specific interface details
  bgp summary                 Show BGP summary
  ospf neighbor               Show OSPF neighbors
  route                       Show routing table

Options:
  -debug              Enable debug output to stderr
  -socket <path>      VPP API socket path (default: /run/vpp/api.sock)
  -config <path>      Configuration file path (default: /etc/arca-router/arca.conf)

Phase 3 Features (Interactive mode):
  - Junos-style configuration commands (set, delete, edit)
  - Commit/rollback support
  - Configuration mode with candidate datastore
  - Tab completion and command history

Examples:
  arca-cli                    # Start interactive mode
  arca-cli show configuration # Show configuration (one-shot)
  arca-cli show interfaces    # Show interfaces (one-shot)
  arca-cli show bgp summary
  arca-cli version

`)
}

func showHelp() {
	showUsage()
}

func debugLog(f *flags, format string, args ...interface{}) {
	if f.debug {
		fmt.Fprintf(os.Stderr, "[DEBUG] "+format+"\n", args...)
	}
}
