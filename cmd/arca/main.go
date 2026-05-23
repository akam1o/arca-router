// arca is the CLI that communicates with arca-routerd
// via gRPC over a Unix domain socket. It is a thin client that delegates
// all state, validation, and config management to the daemon.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	grpcclient "github.com/akam1o/arca-router/internal/northbound/grpc"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

const (
	ExitSuccess        = 0
	ExitOperationError = 1
	ExitUsageError     = 2

	defaultSocket = "/run/arca-router/routerd.sock"

	checkUpgradeUsage = "usage: check upgrade [backup <path>]"

	maxChangeImpactInterfaceDetails = 5
	maxChangeImpactRouteDetails     = 5
	maxChangeImpactPolicyDetails    = 5
)

var errTelemetryUsage = errors.New("telemetry usage error")

type cliFlags struct {
	grpcSocket     string
	grpcAddress    string
	grpcCAFile     string
	grpcServerName string
	grpcClientCert string
	grpcClientKey  string
	debug          bool
	showHelp       bool
	showVersion    bool
}

func main() {
	ctx := context.Background()

	f := parseFlags()

	if f.showHelp {
		showUsage()
		os.Exit(ExitSuccess)
	}
	if f.showVersion {
		fmt.Printf("arca %s (commit %s, built %s)\n", Version, Commit, BuildDate)
		os.Exit(ExitSuccess)
	}

	// One-shot command mode
	if flag.NArg() >= 1 {
		os.Exit(runOneShotCommand(ctx, f, flag.Args()))
	}

	// Interactive mode
	os.Exit(runInteractive(ctx, f))
}

func parseFlags() *cliFlags {
	f := &cliFlags{}
	flag.StringVar(&f.grpcSocket, "socket", defaultSocket, "Path to arca-routerd gRPC Unix socket")
	flag.StringVar(&f.grpcAddress, "grpc-address", "", "arca-routerd TCP/TLS gRPC address (host:port; overrides -socket)")
	flag.StringVar(&f.grpcCAFile, "grpc-ca", "", "CA certificate path for verifying arca-routerd gRPC TLS")
	flag.StringVar(&f.grpcServerName, "grpc-server-name", "", "Expected gRPC TLS server name")
	flag.StringVar(&f.grpcClientCert, "grpc-client-cert", "", "Client certificate path for gRPC mTLS")
	flag.StringVar(&f.grpcClientKey, "grpc-client-key", "", "Client private key path for gRPC mTLS")
	flag.BoolVar(&f.debug, "debug", false, "Enable debug output")
	flag.BoolVar(&f.showHelp, "help", false, "Show help")
	flag.BoolVar(&f.showHelp, "h", false, "Show help (shorthand)")
	flag.BoolVar(&f.showVersion, "version", false, "Show version")
	flag.BoolVar(&f.showVersion, "v", false, "Show version (shorthand)")
	flag.Usage = showUsage
	flag.Parse()
	return f
}

func showUsage() {
	fmt.Fprintf(os.Stderr, `Usage: arca [options] [command] [args...]

Interactive Mode:
  arca                    Start interactive CLI shell

Commands:
  help              Show this help message
  version           Show version information
  show <subcommand> Show configuration or status
  check upgrade [backup <path>]
                    Run upgrade preflight checks
  backup configuration <path>
                    Save running configuration to a new file
  backup configuration rollback <N> <path>
                    Save archived configuration to a new file

Show subcommands:
  configuration               Show full configuration
  configuration rollback <N>  Show archived configuration N commits back
  configuration interfaces    Show interface configuration
  configuration protocols     Show routing protocol configuration
  compatibility               Show v0.10 compatibility policy
  interfaces                  Show interface status
  interfaces <name>           Show specific interface details
  routing-instances [name]    Show routing-instance table mapping
  routes [prefix <cidr>] [protocol <proto>] Show route status
  bgp neighbors               Show BGP neighbor status
  bgp summary                 Show raw BGP summary
  bgp neighbor <ip>           Show raw BGP neighbor details
  ospf neighbor               Show OSPFv2 neighbors
  ospf3 neighbor              Show OSPFv3 neighbors
  vrrp                        Show VRRP status
  bfd status                  Show BFD operational state
  bfd [brief|counters]        Show raw BFD status
  bfd peer <ip> [counters]    Show BFD peer details
  evpn                        Show EVPN/VXLAN overlay intent
  telemetry paths [live] [default] [path <path>] [cardinality <hint>] [payload-schema <id>] [encoding <encoding>]
                              Show supported telemetry path catalog
  telemetry [path <path>]... [interval <duration>] [count <events>]
                              Show telemetry events as JSON lines
  route [inet|inet6]                 Show routing table
  route [inet|inet6] protocol <proto> Show routes by protocol

Options:
  -socket <path>             arca-routerd gRPC socket (default: %s)
  -grpc-address <host:port>  arca-routerd TCP/TLS gRPC address (overrides -socket)
  -grpc-ca <path>            CA certificate for gRPC TLS verification
  -grpc-server-name <name>   Expected gRPC TLS server name
  -grpc-client-cert <path>   Client certificate for gRPC mTLS
  -grpc-client-key <path>    Client private key for gRPC mTLS
  -debug                     Enable debug output
  -help, -h                  Show this help message
  -version, -v               Show version information

`, defaultSocket)
}

func debugLog(f *cliFlags, format string, args ...interface{}) {
	if f.debug {
		fmt.Fprintf(os.Stderr, "[DEBUG] "+format+"\n", args...)
	}
}

func dialGRPC(f *cliFlags) (*grpcclient.Client, error) {
	if address := strings.TrimSpace(f.grpcAddress); address != "" {
		return grpcclient.DialTCP(address, grpcclient.TLSClientOptions{
			CAFile:         f.grpcCAFile,
			ServerName:     f.grpcServerName,
			ClientCertFile: f.grpcClientCert,
			ClientKeyFile:  f.grpcClientKey,
		})
	}
	if f.grpcCAFile != "" || f.grpcServerName != "" || f.grpcClientCert != "" || f.grpcClientKey != "" {
		return nil, fmt.Errorf("gRPC TLS flags require -grpc-address")
	}
	return grpcclient.Dial(f.grpcSocket)
}

func currentUsername() string {
	username := os.Getenv("USER")
	if username == "" {
		return "admin"
	}
	return username
}

func shortCommitID(commitID string) string {
	if len(commitID) > 8 {
		return commitID[:8]
	}
	return commitID
}

func parseHistoryLimit(raw string) (int, error) {
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		return 0, fmt.Errorf("invalid limit: %s", raw)
	}
	return limit, nil
}

func parseRollbackNumber(raw string) (int, error) {
	rollbackNum, err := strconv.Atoi(raw)
	if err != nil || rollbackNum < 0 {
		return 0, fmt.Errorf("invalid rollback number: %s", raw)
	}
	return rollbackNum, nil
}

// --- One-shot command ---

func runOneShotCommand(ctx context.Context, f *cliFlags, args []string) int {
	if handled, code := runLocalOneShotCommand(args); handled {
		return code
	}
	client, err := dialGRPC(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return ExitOperationError
	}
	defer func() { _ = client.Close() }()

	command := args[0]
	switch command {
	case "show":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Error: 'show' requires a subcommand\n\n")
			showUsage()
			return ExitUsageError
		}
		return oneShotShow(ctx, client, args[1:], f)
	case "check":
		return oneShotCheck(ctx, client, args[1:])
	case "backup":
		return oneShotBackup(ctx, client, args[1:])
	case "version":
		fmt.Printf("arca %s (commit %s, built %s)\n", Version, Commit, BuildDate)
		return ExitSuccess
	case "help":
		showUsage()
		return ExitSuccess
	default:
		fmt.Fprintf(os.Stderr, "Error: unknown command '%s'\n\n", command)
		showUsage()
		return ExitUsageError
	}
}

func oneShotCheck(ctx context.Context, client showClient, args []string) int {
	options, err := parseUpgradePreflightOptions(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return ExitUsageError
	}
	lines, err := upgradePreflightLinesWithOptions(ctx, client, options)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return ExitOperationError
	}
	for _, line := range lines {
		fmt.Println(line)
	}
	return ExitSuccess
}

func oneShotBackup(ctx context.Context, client showClient, args []string) int {
	var text, path string
	var err error
	if len(args) == 2 && args[0] == "configuration" {
		text, _, err = client.GetRunning(ctx)
		path = args[1]
	} else if len(args) == 4 && args[0] == "configuration" && args[1] == "rollback" {
		rollbackNum, parseErr := parseRollbackNumber(args[2])
		if parseErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", parseErr)
			return ExitUsageError
		}
		text, err = archivedConfigurationText(ctx, client, rollbackNum)
		path = args[3]
	} else {
		fmt.Fprintln(os.Stderr, "Error: usage: backup configuration [rollback <N>] <path>")
		return ExitUsageError
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return ExitOperationError
	}
	if err := writeConfigBackupFile(path, text); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return ExitOperationError
	}
	fmt.Printf("configuration backup written to %s\n", path)
	return ExitSuccess
}

func runLocalOneShotCommand(args []string) (bool, int) {
	if len(args) >= 2 && args[0] == "show" && args[1] == "compatibility" {
		if len(args) > 2 {
			fmt.Fprintln(os.Stderr, "Error: 'show compatibility' does not accept extra arguments")
			return true, ExitUsageError
		}
		printCompatibilityPolicy()
		return true, ExitSuccess
	}
	if len(args) >= 3 && args[0] == "show" && args[1] == "telemetry" {
		opts, ok, err := telemetryCatalogOptions(args[2:])
		if !ok {
			return false, ExitSuccess
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return true, ExitUsageError
		}
		if opts.live {
			return false, ExitSuccess
		}
		catalog := grpcclient.NewTelemetryCatalog()
		printTelemetryCatalog(catalog, filterTelemetryPathCatalog(catalog.Paths, opts))
		return true, ExitSuccess
	}
	return false, ExitSuccess
}

func oneShotShow(ctx context.Context, client showClient, args []string, f *cliFlags) int {
	subcmd := args[0]
	switch subcmd {
	case "configuration":
		if len(args) > 1 {
			if len(args) != 3 || args[1] != "rollback" {
				fmt.Fprintln(os.Stderr, "Error: usage: show configuration rollback <N>")
				return ExitUsageError
			}
			rollbackNum, err := parseRollbackNumber(args[2])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return ExitUsageError
			}
			text, err := archivedConfigurationText(ctx, client, rollbackNum)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return ExitOperationError
			}
			fmt.Println(text)
			return ExitSuccess
		}
		debugLog(f, "Fetching running configuration via gRPC")
		text, _, err := client.GetRunning(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return ExitOperationError
		}
		fmt.Println(text)
		return ExitSuccess

	case "interfaces":
		nameFilter := ""
		if len(args) > 1 {
			nameFilter = args[1]
		}
		ifaces, err := client.GetInterfaces(ctx, nameFilter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return ExitOperationError
		}
		printInterfaces(ifaces)
		return ExitSuccess

	case "routing-instances":
		nameFilter, err := routingInstancesNameFilter(args[1:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return ExitUsageError
		}
		instances, err := client.GetRoutingInstances(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return ExitOperationError
		}
		printRoutingInstances(filterRoutingInstances(instances, nameFilter))
		return ExitSuccess

	case "routes":
		prefixFilter, protoFilter, err := routeStateOptions(args[1:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return ExitUsageError
		}
		routes, err := client.GetRoutes(ctx, prefixFilter, protoFilter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return ExitOperationError
		}
		printRoutes(routes)
		return ExitSuccess

	case "bgp":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Error: 'show bgp' requires a subcommand (neighbors, summary, or neighbor)\n")
			return ExitUsageError
		}
		switch args[1] {
		case "neighbors":
			if len(args) > 2 {
				fmt.Fprintf(os.Stderr, "Error: 'show bgp neighbors' does not accept extra arguments\n")
				return ExitUsageError
			}
			neighbors, err := client.GetBGPNeighbors(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return ExitOperationError
			}
			printBGPNeighbors(neighbors)
			return ExitSuccess
		case "summary":
			output, err := client.GetBGPSummaryText(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return ExitOperationError
			}
			printCommandOutput(output)
			return ExitSuccess
		case "neighbor":
			if len(args) < 3 {
				fmt.Fprintf(os.Stderr, "Error: 'show bgp neighbor' requires an IP address\n")
				return ExitUsageError
			}
			output, err := client.GetBGPNeighborText(ctx, args[2])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return ExitOperationError
			}
			printCommandOutput(output)
			return ExitSuccess
		default:
			fmt.Fprintf(os.Stderr, "Error: unknown bgp subcommand '%s'\n", args[1])
			return ExitUsageError
		}

	case "ospf", "ospf3":
		if len(args) < 2 || args[1] != "neighbor" {
			fmt.Fprintf(os.Stderr, "Error: 'show %s' requires 'neighbor' subcommand\n", subcmd)
			return ExitUsageError
		}
		if len(args) > 2 {
			fmt.Fprintf(os.Stderr, "Error: 'show %s neighbor' does not accept extra arguments\n", subcmd)
			return ExitUsageError
		}
		addressFamily := routeAddressFamilyIPv4
		if subcmd == "ospf3" {
			addressFamily = routeAddressFamilyIPv6
		}
		neighbors, err := client.GetOSPFNeighbors(ctx, addressFamily)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return ExitOperationError
		}
		printOSPFNeighbors(neighbors)
		return ExitSuccess

	case "vrrp":
		output, err := client.GetVRRPText(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return ExitOperationError
		}
		printCommandOutput(output)
		return ExitSuccess

	case "bfd":
		statusRequested, err := bfdStatusRequested(args[1:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return ExitUsageError
		}
		if statusRequested {
			info, err := client.GetBFDStatus(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				return ExitOperationError
			}
			printBFDStatus(info)
			return ExitSuccess
		}
		peerAddress, brief, counters, err := bfdTextOptions(args[1:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return ExitUsageError
		}
		output, err := client.GetBFDText(ctx, peerAddress, brief, counters)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return ExitOperationError
		}
		printCommandOutput(output)
		return ExitSuccess

	case "lcp":
		info, err := client.GetLCPReconciliation(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return ExitOperationError
		}
		printLCPReconciliation(info)
		return ExitSuccess

	case "ha":
		info, err := client.GetHAStatus(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return ExitOperationError
		}
		printHAStatus(info)
		return ExitSuccess

	case "class-of-service":
		info, err := client.GetClassOfService(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return ExitOperationError
		}
		printClassOfService(info)
		return ExitSuccess

	case "evpn":
		if err := showEVPN(ctx, client); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return ExitOperationError
		}
		return ExitSuccess

	case "telemetry":
		if err := showTelemetry(ctx, client, args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			if isTelemetryUsageError(err) {
				return ExitUsageError
			}
			return ExitOperationError
		}
		return ExitSuccess

	case "route":
		protoFilter, addressFamily, err := routeTextOptions(args[1:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return ExitUsageError
		}
		output, err := client.GetRouteText(ctx, protoFilter, addressFamily)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return ExitOperationError
		}
		printCommandOutput(output)
		return ExitSuccess

	default:
		fmt.Fprintf(os.Stderr, "Error: unknown show subcommand '%s'\n", subcmd)
		return ExitUsageError
	}
}
