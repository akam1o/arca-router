// arca is the CLI that communicates with arca-routerd
// via gRPC over a Unix domain socket. It is a thin client that delegates
// all state, validation, and config management to the daemon.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	grpcclient "github.com/akam1o/arca-router/internal/northbound/grpc"
	configcli "github.com/akam1o/arca-router/pkg/cli"
	"github.com/chzyer/readline"
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

	maxChangeImpactInterfaceDetails = 5
	maxChangeImpactRouteDetails     = 5
	maxChangeImpactPolicyDetails    = 5
)

var errTelemetryUsage = errors.New("telemetry usage error")

type cliFlags struct {
	grpcSocket  string
	debug       bool
	showHelp    bool
	showVersion bool
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

Show subcommands:
  configuration               Show full configuration
  configuration interfaces    Show interface configuration
  configuration protocols     Show routing protocol configuration
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
  -socket <path>     arca-routerd gRPC socket (default: %s)
  -debug             Enable debug output
  -help, -h          Show this help message
  -version, -v       Show version information

`, defaultSocket)
}

func debugLog(f *cliFlags, format string, args ...interface{}) {
	if f.debug {
		fmt.Fprintf(os.Stderr, "[DEBUG] "+format+"\n", args...)
	}
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
	client, err := grpcclient.Dial(f.grpcSocket)
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

func runLocalOneShotCommand(args []string) (bool, int) {
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

// --- Interactive mode ---

type interactiveShell struct {
	client    interactiveClient
	rl        *readline.Instance
	hostname  string
	mode      cliMode
	sessionID string
	hasLock   bool
	editPath  []string
	flags     *cliFlags
}

type interactiveClient interface {
	showClient
	GetCandidate(context.Context, string) (string, error)
	EditCandidate(context.Context, string, string) error
	Commit(context.Context, string, string, string) (string, uint64, error)
	ValidateCandidate(context.Context, string) error
	Discard(context.Context, string) error
	Rollback(context.Context, string, string, string, string) (string, uint64, error)
	Diff(context.Context, string) (string, bool, error)
	ListHistory(context.Context, int, int) ([]grpcclient.CommitInfo, error)
	AcquireLock(context.Context, string, string) error
	ReleaseLock(context.Context, string) error
}

type showClient interface {
	GetRunning(context.Context) (string, uint64, error)
	GetInterfaces(context.Context, string) ([]grpcclient.InterfaceInfo, error)
	GetRoutingInstances(context.Context) ([]grpcclient.RoutingInstanceInfo, error)
	GetRoutes(context.Context, string, string) ([]grpcclient.RouteInfo, error)
	GetBGPNeighbors(context.Context) ([]grpcclient.BGPNeighborInfo, error)
	GetOSPFNeighbors(context.Context, string) ([]grpcclient.OSPFNeighborInfo, error)
	GetRouteText(context.Context, string, string) (string, error)
	GetBGPSummaryText(context.Context) (string, error)
	GetBGPNeighborText(context.Context, string) (string, error)
	GetOSPFNeighborsText(context.Context, string) (string, error)
	GetVRRPText(context.Context) (string, error)
	GetBFDText(context.Context, string, bool, bool) (string, error)
	GetBFDStatus(context.Context) (*grpcclient.BFDStatusInfo, error)
	GetLCPReconciliation(context.Context) (*grpcclient.LCPReconciliationInfo, error)
	GetHAStatus(context.Context) (*grpcclient.HAStatusInfo, error)
	GetClassOfService(context.Context) (*grpcclient.ClassOfServiceInfo, error)
	GetTelemetryCatalog(context.Context) (grpcclient.TelemetryCatalog, error)
	GetFilteredTelemetryCatalog(context.Context, []string, []string) (grpcclient.TelemetryCatalog, error)
	GetPathFilteredTelemetryCatalog(context.Context, []string, []string, []string) (grpcclient.TelemetryCatalog, error)
	GetTelemetryCatalogWithFilter(context.Context, grpcclient.TelemetryCatalogFilter) (grpcclient.TelemetryCatalog, error)
	SubscribeTelemetry(context.Context, []string, time.Duration, bool) (grpcclient.TelemetryReceiver, error)
}

type cliMode int

const (
	modeOperational cliMode = iota
	modeConfiguration
)

func runInteractive(ctx context.Context, f *cliFlags) int {
	client, err := grpcclient.Dial(f.grpcSocket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to connect to arca-routerd: %v\n", err)
		return ExitOperationError
	}
	defer func() { _ = client.Close() }()

	// Get hostname from daemon
	hostname := "arca-router"
	info, err := client.GetSystemInfo(ctx)
	if err == nil && info.Hostname != "" {
		hostname = info.Hostname
	}

	username := currentUsername()

	sh := &interactiveShell{
		client:   client,
		hostname: hostname,
		mode:     modeOperational,
		flags:    f,
	}

	completer := createCompleter()
	rl, err := readline.NewEx(&readline.Config{
		Prompt:              sh.buildPrompt(),
		HistoryFile:         "/tmp/.arca-history",
		AutoComplete:        completer,
		InterruptPrompt:     "^C",
		EOFPrompt:           "exit",
		HistorySearchFold:   true,
		FuncFilterInputRune: filterInput,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return ExitOperationError
	}
	sh.rl = rl
	defer func() { _ = rl.Close() }()

	// Create a session with the daemon
	sessionID, err := client.CreateSession(ctx, username)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create configuration session: %v\n", err)
		return ExitOperationError
	}
	if sessionID == "" {
		fmt.Fprintln(os.Stderr, "Error: daemon returned empty configuration session ID")
		return ExitOperationError
	}
	sh.sessionID = sessionID
	defer func() {
		_ = client.CloseSession(ctx, sh.sessionID)
	}()

	// Handle Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		fmt.Println("\nInterrupted. Use 'exit' or 'quit' to leave the shell.")
	}()

	fmt.Println("Welcome to arca-router interactive CLI")
	fmt.Println("Type 'help' for available commands, 'exit' or 'quit' to exit")
	fmt.Println()

	for {
		sh.rl.SetPrompt(sh.buildPrompt())
		line, err := sh.rl.Readline()
		if err != nil {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if err := sh.processCommand(ctx, line); err != nil {
			if err.Error() == "exit" {
				break
			}
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
	}

	return ExitSuccess
}

func (sh *interactiveShell) buildPrompt() string {
	if sh.mode == modeOperational {
		return fmt.Sprintf("%s> ", sh.hostname)
	}
	if len(sh.editPath) > 0 {
		return fmt.Sprintf("%s# [edit %s] ", sh.hostname, strings.Join(sh.editPath, " "))
	}
	return fmt.Sprintf("%s# ", sh.hostname)
}

func (sh *interactiveShell) processCommand(ctx context.Context, line string) error {
	// Handle pipe commands
	if hasPipeOutsideQuotes(line) {
		parts := strings.SplitN(line, "|", 2)
		left := strings.TrimSpace(parts[0])
		right := strings.TrimSpace(parts[1])
		if left == "show" && right == "compare" {
			return sh.cmdCompare(ctx)
		}
		return fmt.Errorf("unsupported pipe command: %s | %s", left, right)
	}

	parts := tokenize(line)
	if len(parts) == 0 {
		return nil
	}

	cmd := parts[0]
	args := parts[1:]

	switch cmd {
	case "help", "?":
		sh.showHelp()
		return nil
	case "exit", "quit":
		if sh.mode == modeConfiguration {
			fmt.Println("Warning: Exiting configuration mode. Uncommitted changes will be lost.")
			fmt.Print("Exit anyway? [yes/no]: ")
			reader := bufio.NewReader(os.Stdin)
			response, _ := reader.ReadString('\n')
			response = strings.TrimSpace(strings.ToLower(response))
			if response != "yes" && response != "y" {
				return nil
			}
			if err := sh.exitConfigurationMode(ctx); err != nil {
				return fmt.Errorf("exit configuration mode: %w", err)
			}
			return nil
		}
		return fmt.Errorf("exit")
	case "configure":
		return sh.cmdConfigure(ctx)
	case "show":
		return sh.cmdShow(ctx, args)
	case "set":
		return sh.cmdSet(ctx, args)
	case "delete":
		return sh.cmdDelete(ctx, args)
	case "commit":
		return sh.cmdCommit(ctx, args)
	case "rollback":
		return sh.cmdRollback(ctx, args)
	case "compare":
		return sh.cmdCompare(ctx)
	case "discard-changes":
		return sh.cmdDiscardChanges(ctx)
	case "edit":
		return sh.cmdEdit(args)
	case "up":
		return sh.cmdUp()
	case "top":
		return sh.cmdTop()
	default:
		return fmt.Errorf("unknown command: %s. Type 'help' for available commands", cmd)
	}
}

// --- Command handlers ---

func (sh *interactiveShell) cmdConfigure(ctx context.Context) error {
	if sh.mode == modeConfiguration {
		fmt.Println("Already in configuration mode")
		return nil
	}
	if sh.sessionID == "" {
		return fmt.Errorf("configuration session is not available")
	}

	// Acquire candidate lock via gRPC
	if err := sh.client.AcquireLock(ctx, sh.sessionID, currentUsername()); err != nil {
		return fmt.Errorf("failed to acquire candidate lock: %w", err)
	}
	sh.hasLock = true

	sh.mode = modeConfiguration
	fmt.Println("Entering configuration mode")
	return nil
}

func (sh *interactiveShell) exitConfigurationMode(ctx context.Context) error {
	if sh.sessionID == "" {
		return fmt.Errorf("configuration session is not available")
	}
	if err := sh.client.Discard(ctx, sh.sessionID); err != nil {
		return fmt.Errorf("discard changes: %w", err)
	}
	return sh.releaseConfigurationLock(ctx)
}

func (sh *interactiveShell) releaseConfigurationLock(ctx context.Context) error {
	if sh.sessionID == "" {
		return fmt.Errorf("configuration session is not available")
	}
	if err := sh.client.ReleaseLock(ctx, sh.sessionID); err != nil {
		return fmt.Errorf("release candidate lock: %w", err)
	}
	sh.mode = modeOperational
	sh.editPath = nil
	sh.hasLock = false
	return nil
}

func (sh *interactiveShell) cmdShow(ctx context.Context, args []string) error {
	if len(args) == 0 {
		if sh.mode == modeConfiguration {
			// Show candidate config
			text, err := sh.client.GetCandidate(ctx, sh.sessionID)
			if err != nil {
				return err
			}
			fmt.Println(text)
		} else {
			// Show running config
			text, _, err := sh.client.GetRunning(ctx)
			if err != nil {
				return err
			}
			fmt.Println(text)
		}
		return nil
	}

	subcmd := args[0]
	switch subcmd {
	case "configuration":
		if len(args) > 1 {
			return sh.cmdShowArchivedConfiguration(ctx, args[1:])
		}
		var text string
		var err error
		if sh.mode == modeConfiguration {
			text, err = sh.client.GetCandidate(ctx, sh.sessionID)
		} else {
			text, _, err = sh.client.GetRunning(ctx)
		}
		if err != nil {
			return err
		}
		fmt.Println(text)
		return nil

	case "compare":
		return sh.cmdCompare(ctx)

	case "history":
		limit := 10
		if len(args) > 1 {
			var err error
			limit, err = parseHistoryLimit(args[1])
			if err != nil {
				return err
			}
		}
		entries, err := sh.client.ListHistory(ctx, limit, 0)
		if err != nil {
			return err
		}
		for _, e := range entries {
			rb := ""
			if e.IsRollback {
				rb = " (rollback)"
			}
			fmt.Printf("  %s  %s  by %s%s  %s\n", shortCommitID(e.CommitID), e.Timestamp, e.User, rb, e.Message)
		}
		return nil

	case "interfaces":
		if sh.mode == modeConfiguration {
			return fmt.Errorf("'show interfaces' not available in configuration mode")
		}
		nameFilter := ""
		if len(args) > 1 {
			nameFilter = args[1]
		}
		ifaces, err := sh.client.GetInterfaces(ctx, nameFilter)
		if err != nil {
			return err
		}
		printInterfaces(ifaces)
		return nil

	case "routing-instances":
		if sh.mode == modeConfiguration {
			return fmt.Errorf("'show routing-instances' not available in configuration mode")
		}
		nameFilter, err := routingInstancesNameFilter(args[1:])
		if err != nil {
			return err
		}
		instances, err := sh.client.GetRoutingInstances(ctx)
		if err != nil {
			return err
		}
		printRoutingInstances(filterRoutingInstances(instances, nameFilter))
		return nil

	case "routes":
		if sh.mode == modeConfiguration {
			return fmt.Errorf("'show routes' not available in configuration mode")
		}
		prefixFilter, protoFilter, err := routeStateOptions(args[1:])
		if err != nil {
			return err
		}
		routes, err := sh.client.GetRoutes(ctx, prefixFilter, protoFilter)
		if err != nil {
			return err
		}
		printRoutes(routes)
		return nil

	case "bgp":
		if sh.mode == modeConfiguration {
			return fmt.Errorf("'show bgp' not available in configuration mode")
		}
		if len(args) < 2 {
			return fmt.Errorf("'show bgp' requires a subcommand (neighbors, summary, or neighbor)")
		}
		switch args[1] {
		case "neighbors":
			if len(args) > 2 {
				return fmt.Errorf("'show bgp neighbors' does not accept extra arguments")
			}
			neighbors, err := sh.client.GetBGPNeighbors(ctx)
			if err != nil {
				return err
			}
			printBGPNeighbors(neighbors)
			return nil
		case "summary":
			output, err := sh.client.GetBGPSummaryText(ctx)
			if err != nil {
				return err
			}
			printCommandOutput(output)
			return nil
		case "neighbor":
			if len(args) < 3 {
				return fmt.Errorf("'show bgp neighbor' requires an IP address")
			}
			output, err := sh.client.GetBGPNeighborText(ctx, args[2])
			if err != nil {
				return err
			}
			printCommandOutput(output)
			return nil
		default:
			return fmt.Errorf("unknown bgp subcommand '%s'", args[1])
		}

	case "ospf", "ospf3":
		if sh.mode == modeConfiguration {
			return fmt.Errorf("'show %s' not available in configuration mode", subcmd)
		}
		if len(args) < 2 || args[1] != "neighbor" {
			return fmt.Errorf("'show %s' requires 'neighbor' subcommand", subcmd)
		}
		if len(args) > 2 {
			return fmt.Errorf("'show %s neighbor' does not accept extra arguments", subcmd)
		}
		addressFamily := routeAddressFamilyIPv4
		if subcmd == "ospf3" {
			addressFamily = routeAddressFamilyIPv6
		}
		neighbors, err := sh.client.GetOSPFNeighbors(ctx, addressFamily)
		if err != nil {
			return err
		}
		printOSPFNeighbors(neighbors)
		return nil

	case "vrrp":
		if sh.mode == modeConfiguration {
			return fmt.Errorf("'show vrrp' not available in configuration mode")
		}
		output, err := sh.client.GetVRRPText(ctx)
		if err != nil {
			return err
		}
		printCommandOutput(output)
		return nil

	case "bfd":
		if sh.mode == modeConfiguration {
			return fmt.Errorf("'show bfd' not available in configuration mode")
		}
		statusRequested, err := bfdStatusRequested(args[1:])
		if err != nil {
			return err
		}
		if statusRequested {
			info, err := sh.client.GetBFDStatus(ctx)
			if err != nil {
				return err
			}
			printBFDStatus(info)
			return nil
		}
		peerAddress, brief, counters, err := bfdTextOptions(args[1:])
		if err != nil {
			return err
		}
		output, err := sh.client.GetBFDText(ctx, peerAddress, brief, counters)
		if err != nil {
			return err
		}
		printCommandOutput(output)
		return nil

	case "lcp":
		if sh.mode == modeConfiguration {
			return fmt.Errorf("'show lcp' not available in configuration mode")
		}
		info, err := sh.client.GetLCPReconciliation(ctx)
		if err != nil {
			return err
		}
		printLCPReconciliation(info)
		return nil

	case "ha":
		if sh.mode == modeConfiguration {
			return fmt.Errorf("'show ha' not available in configuration mode")
		}
		info, err := sh.client.GetHAStatus(ctx)
		if err != nil {
			return err
		}
		printHAStatus(info)
		return nil

	case "class-of-service":
		if sh.mode == modeConfiguration {
			return fmt.Errorf("'show class-of-service' not available in configuration mode")
		}
		info, err := sh.client.GetClassOfService(ctx)
		if err != nil {
			return err
		}
		printClassOfService(info)
		return nil

	case "evpn":
		if sh.mode == modeConfiguration {
			return fmt.Errorf("'show evpn' not available in configuration mode")
		}
		return showEVPN(ctx, sh.client)

	case "telemetry":
		if sh.mode == modeConfiguration {
			return fmt.Errorf("'show telemetry' not available in configuration mode")
		}
		return showTelemetry(ctx, sh.client, args[1:])

	case "route":
		if sh.mode == modeConfiguration {
			return fmt.Errorf("'show route' not available in configuration mode")
		}
		protoFilter, addressFamily, err := routeTextOptions(args[1:])
		if err != nil {
			return err
		}
		output, err := sh.client.GetRouteText(ctx, protoFilter, addressFamily)
		if err != nil {
			return err
		}
		printCommandOutput(output)
		return nil

	default:
		return fmt.Errorf("unknown show subcommand '%s'", subcmd)
	}
}

func (sh *interactiveShell) cmdShowArchivedConfiguration(ctx context.Context, args []string) error {
	if len(args) != 2 || args[0] != "rollback" {
		return fmt.Errorf("usage: show configuration rollback <N>")
	}
	rollbackNum, err := parseRollbackNumber(args[1])
	if err != nil {
		return err
	}

	history, err := sh.client.ListHistory(ctx, rollbackNum+1, 0)
	if err != nil {
		return fmt.Errorf("failed to load commit history: %w", err)
	}
	if len(history) <= rollbackNum {
		if rollbackNum == 0 {
			text, _, err := sh.client.GetRunning(ctx)
			if err != nil {
				return err
			}
			fmt.Println(text)
			return nil
		}
		availableCommits := len(history) - 1
		if availableCommits < 0 {
			availableCommits = 0
		}
		return fmt.Errorf("not enough history for rollback %d (only %d commits available)", rollbackNum, availableCommits)
	}

	entry := history[rollbackNum]
	if entry.ConfigText == "" {
		if rollbackNum == 0 {
			text, _, err := sh.client.GetRunning(ctx)
			if err != nil {
				return err
			}
			fmt.Println(text)
			return nil
		}
		return fmt.Errorf("archived config text unavailable for rollback %d", rollbackNum)
	}
	fmt.Println(entry.ConfigText)
	return nil
}

func (sh *interactiveShell) cmdSet(ctx context.Context, args []string) error {
	if sh.mode != modeConfiguration {
		return fmt.Errorf("'set' command only available in configuration mode")
	}
	// Build the full set command and send to daemon
	fullPath := append(sh.editPath, args...)
	setCmd := "set " + configcli.NormalizeConfigPath(fullPath)
	if err := sh.client.EditCandidate(ctx, sh.sessionID, setCmd); err != nil {
		return err
	}
	fmt.Println("[edit]")
	return nil
}

func (sh *interactiveShell) cmdDelete(ctx context.Context, args []string) error {
	if sh.mode != modeConfiguration {
		return fmt.Errorf("'delete' command only available in configuration mode")
	}
	fullPath := append(sh.editPath, args...)
	delCmd := "delete " + configcli.NormalizeConfigPath(fullPath)
	if err := sh.client.EditCandidate(ctx, sh.sessionID, delCmd); err != nil {
		return err
	}
	fmt.Println("[edit]")
	return nil
}

func (sh *interactiveShell) cmdCommit(ctx context.Context, args []string) error {
	if sh.mode != modeConfiguration {
		return fmt.Errorf("'commit' command only available in configuration mode")
	}

	message := ""
	check := false
	andQuit := false

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "check":
			check = true
		case "and-quit":
			andQuit = true
		case "comment":
			if i+1 < len(args) {
				message = args[i+1]
				i++
			} else {
				return fmt.Errorf("'comment' requires an argument")
			}
		default:
			return fmt.Errorf("unknown commit option: %s", args[i])
		}
	}

	if check && andQuit {
		return fmt.Errorf("'check' and 'and-quit' cannot be used together")
	}
	if check && message != "" {
		return fmt.Errorf("'check' and 'comment' cannot be used together")
	}

	if check {
		if err := sh.client.ValidateCandidate(ctx, sh.sessionID); err != nil {
			return fmt.Errorf("configuration check failed: %w", err)
		}
		fmt.Println("configuration check succeeds")
		if err := sh.printChangeImpactPreview(ctx); err != nil {
			return fmt.Errorf("change impact preview failed: %w", err)
		}
		return nil
	}

	user := currentUsername()

	commitID, version, err := sh.client.Commit(ctx, sh.sessionID, user, message)
	if err != nil {
		if diagErr := sh.printCommitFailureDiagnostics(ctx); diagErr != nil {
			return fmt.Errorf("commit failed: %w (diagnostics unavailable: %v)", err, diagErr)
		}
		return fmt.Errorf("commit failed: %w", err)
	}
	fmt.Printf("commit complete (id: %s, version: %d)\n", shortCommitID(commitID), version)

	if andQuit {
		if err := sh.releaseConfigurationLock(ctx); err != nil {
			return fmt.Errorf("commit complete but failed to exit configuration mode: %w", err)
		}
	}
	return nil
}

func (sh *interactiveShell) cmdRollback(ctx context.Context, args []string) error {
	if sh.mode != modeConfiguration {
		return fmt.Errorf("'rollback' command only available in configuration mode")
	}
	rollbackNum := 0
	if len(args) > 0 {
		var err error
		rollbackNum, err = parseRollbackNumber(args[0])
		if err != nil {
			return err
		}
	}
	if rollbackNum == 0 {
		return sh.cmdDiscardChanges(ctx)
	}

	history, err := sh.client.ListHistory(ctx, rollbackNum+1, 0)
	if err != nil {
		return fmt.Errorf("failed to load commit history: %w", err)
	}
	if len(history) <= rollbackNum {
		availableCommits := len(history) - 1
		if availableCommits < 0 {
			availableCommits = 0
		}
		return fmt.Errorf("not enough history for rollback %d (only %d commits available)", rollbackNum, availableCommits)
	}
	target := history[rollbackNum]
	user := currentUsername()
	newCommitID, version, err := sh.client.Rollback(ctx, sh.sessionID, target.CommitID, user, fmt.Sprintf("CLI rollback %d", rollbackNum))
	if err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}
	fmt.Printf("rollback complete (id: %s, version: %d)\n", shortCommitID(newCommitID), version)
	return nil
}

func (sh *interactiveShell) cmdDiscardChanges(ctx context.Context) error {
	if sh.mode != modeConfiguration {
		return fmt.Errorf("'discard-changes' command only available in configuration mode")
	}
	if err := sh.client.Discard(ctx, sh.sessionID); err != nil {
		return err
	}
	fmt.Println("Changes discarded")
	return nil
}

func (sh *interactiveShell) cmdCompare(ctx context.Context) error {
	if sh.mode != modeConfiguration {
		return fmt.Errorf("'compare' command only available in configuration mode")
	}
	diffText, hasChanges, err := sh.client.Diff(ctx, sh.sessionID)
	if err != nil {
		return err
	}
	if !hasChanges {
		fmt.Println("No changes")
	} else {
		fmt.Println(diffText)
	}
	return nil
}

func (sh *interactiveShell) printChangeImpactPreview(ctx context.Context) error {
	diffText, hasChanges, err := sh.client.Diff(ctx, sh.sessionID)
	if err != nil {
		return err
	}
	for _, line := range formatChangeImpactPreview(diffText, hasChanges) {
		fmt.Println(line)
	}
	for _, line := range sh.classOfServicePreflightLines(ctx, diffText, hasChanges) {
		fmt.Println(line)
	}
	return nil
}

func (sh *interactiveShell) printCommitFailureDiagnostics(ctx context.Context) error {
	diffText, hasChanges, err := sh.client.Diff(ctx, sh.sessionID)
	if err != nil {
		return err
	}
	fmt.Println("commit failure diagnostics:")
	for _, line := range formatChangeImpactPreview(diffText, hasChanges) {
		fmt.Printf("  %s\n", line)
	}
	for _, line := range sh.classOfServicePreflightLines(ctx, diffText, hasChanges) {
		fmt.Printf("  %s\n", line)
	}
	fmt.Println("  next step: resolve the error and run 'commit check'")
	return nil
}

func (sh *interactiveShell) classOfServicePreflightLines(ctx context.Context, diffText string, hasChanges bool) []string {
	if !hasChanges || !analyzeChangeImpact(diffText).classOfService.hasChanges() {
		return nil
	}
	info, err := sh.client.GetClassOfService(ctx)
	if err != nil {
		return []string{"qos preflight: capability check unavailable: " + err.Error()}
	}
	return formatClassOfServicePreflight(info)
}

type changeImpactPreview struct {
	addedLines             int
	removedLines           int
	interfaces             changeImpactLineCount
	interfaceChanges       []changeImpactInterfaceChange
	staticRoutes           changeImpactLineCount
	staticRouteChanges     []changeImpactRouteChange
	policyOptions          changeImpactLineCount
	policyChanges          []changeImpactPolicyChange
	bgpPolicyBindings      []changeImpactBGPPolicyBinding
	bgp                    changeImpactLineCount
	ospf                   changeImpactLineCount
	bfd                    changeImpactLineCount
	evpn                   changeImpactLineCount
	routingInstances       changeImpactLineCount
	classOfService         changeImpactLineCount
	interfaceAddressChange bool
	defaultRouteChange     bool
}

type changeImpactLineCount struct {
	added   int
	removed int
}

type changeImpactInterfaceChange struct {
	sign      byte
	name      string
	operation string
	value     string
}

type changeImpactRouteChange struct {
	sign            byte
	prefix          string
	nextHop         string
	routingInstance string
}

type changeImpactPolicyChange struct {
	sign   byte
	kind   string
	name   string
	term   string
	prefix string
}

type changeImpactBGPPolicyBinding struct {
	sign      byte
	groupName string
	direction string
	policy    string
}

func formatChangeImpactPreview(diffText string, hasChanges bool) []string {
	if !hasChanges || strings.TrimSpace(diffText) == "" {
		return []string{"change impact preview: no candidate changes"}
	}

	preview := analyzeChangeImpact(diffText)
	lines := []string{
		"change impact preview:",
		fmt.Sprintf("  changed lines: +%d -%d", preview.addedLines, preview.removedLines),
	}
	lines = appendChangeImpactLine(lines, "interfaces", preview.interfaces)
	lines = appendInterfaceImpactLines(lines, preview.interfaceChanges)
	lines = appendChangeImpactLine(lines, "static routes", preview.staticRoutes)
	lines = appendStaticRouteImpactLines(lines, preview.staticRouteChanges)
	lines = appendChangeImpactLine(lines, "policy-options", preview.policyOptions)
	lines = appendPolicyImpactLines(lines, preview.policyChanges, preview.bgpPolicyBindings)
	lines = appendChangeImpactLine(lines, "bgp", preview.bgp)
	lines = appendChangeImpactLine(lines, "ospf", preview.ospf)
	lines = appendChangeImpactLine(lines, "bfd", preview.bfd)
	lines = appendChangeImpactLine(lines, "evpn", preview.evpn)
	lines = appendChangeImpactLine(lines, "routing-instances", preview.routingInstances)
	lines = appendChangeImpactLine(lines, "class-of-service", preview.classOfService)
	if preview.defaultRouteChange {
		lines = append(lines, "  warning: default route changes can affect all unmatched traffic")
	}
	if preview.interfaces.hasChanges() {
		lines = append(lines, "  warning: interface changes can affect link state or attached services")
	}
	if preview.interfaceAddressChange {
		lines = append(lines, "  warning: interface address changes can alter connected route reachability")
	}
	if preview.staticRoutes.removed > 0 {
		lines = append(lines, "  warning: static route removals can withdraw forwarding entries")
	}
	if preview.policyOptions.hasChanges() {
		lines = append(lines, "  warning: policy-options changes can regenerate FRR route-maps")
	}
	if preview.bgp.hasChanges() {
		lines = append(lines, "  warning: BGP changes can reset sessions or change route advertisements")
	}
	if preview.ospf.hasChanges() {
		lines = append(lines, "  warning: OSPF changes can trigger adjacency updates or SPF recalculation")
	}
	if preview.bfd.hasChanges() {
		lines = append(lines, "  warning: BFD changes can affect fast failure detection")
	}
	if preview.evpn.hasChanges() {
		lines = append(lines, "  warning: EVPN changes can alter overlay VNI reachability")
	}
	if preview.routingInstances.hasChanges() {
		lines = append(lines, "  warning: routing-instance changes can move interfaces or VRF routing state")
	}
	if preview.classOfService.hasChanges() {
		lines = append(lines, "  warning: class-of-service changes can alter traffic treatment")
	}
	return lines
}

func appendInterfaceImpactLines(lines []string, changes []changeImpactInterfaceChange) []string {
	if len(changes) == 0 {
		return lines
	}
	lines = append(lines, "  interface diff:")
	limit := len(changes)
	if limit > maxChangeImpactInterfaceDetails {
		limit = maxChangeImpactInterfaceDetails
	}
	for _, change := range changes[:limit] {
		lines = append(lines, "    "+change.summary())
	}
	if remaining := len(changes) - limit; remaining > 0 {
		lines = append(lines, fmt.Sprintf("    ... %d more interface changes", remaining))
	}
	return lines
}

func appendPolicyImpactLines(lines []string, policyChanges []changeImpactPolicyChange, bgpBindings []changeImpactBGPPolicyBinding) []string {
	if len(policyChanges) == 0 && len(bgpBindings) == 0 {
		return lines
	}
	lines = append(lines, "  policy diff:")
	remainingSlots := maxChangeImpactPolicyDetails
	for _, change := range policyChanges {
		if remainingSlots == 0 {
			break
		}
		lines = append(lines, "    "+change.summary())
		remainingSlots--
	}
	for _, binding := range bgpBindings {
		if remainingSlots == 0 {
			break
		}
		lines = append(lines, "    "+binding.summary())
		remainingSlots--
	}
	if remaining := len(policyChanges) + len(bgpBindings) - maxChangeImpactPolicyDetails; remaining > 0 {
		lines = append(lines, fmt.Sprintf("    ... %d more policy changes", remaining))
	}
	return lines
}

func appendStaticRouteImpactLines(lines []string, changes []changeImpactRouteChange) []string {
	if len(changes) == 0 {
		return lines
	}
	lines = append(lines, "  route diff:")
	limit := len(changes)
	if limit > maxChangeImpactRouteDetails {
		limit = maxChangeImpactRouteDetails
	}
	for _, change := range changes[:limit] {
		lines = append(lines, "    "+change.summary())
	}
	if remaining := len(changes) - limit; remaining > 0 {
		lines = append(lines, fmt.Sprintf("    ... %d more static route changes", remaining))
	}
	return lines
}

func appendChangeImpactLine(lines []string, label string, count changeImpactLineCount) []string {
	if !count.hasChanges() {
		return lines
	}
	return append(lines, fmt.Sprintf("  %s: +%d -%d", label, count.added, count.removed))
}

func (c changeImpactLineCount) hasChanges() bool {
	return c.added > 0 || c.removed > 0
}

func (c *changeImpactLineCount) add(sign byte) {
	if sign == '+' {
		c.added++
	} else {
		c.removed++
	}
}

func (c changeImpactInterfaceChange) summary() string {
	action := "add"
	if c.sign == '-' {
		action = "remove"
	}
	if c.value == "" {
		return fmt.Sprintf("%s interface %s %s", action, c.name, c.operation)
	}
	return fmt.Sprintf("%s interface %s %s %s", action, c.name, c.operation, c.value)
}

func (c changeImpactRouteChange) summary() string {
	action := "add"
	if c.sign == '-' {
		action = "remove"
	}
	target := c.prefix
	if c.routingInstance != "" {
		target = fmt.Sprintf("routing-instance %s %s", c.routingInstance, target)
	}
	if c.nextHop == "" {
		return fmt.Sprintf("%s %s", action, target)
	}
	return fmt.Sprintf("%s %s via %s", action, target, c.nextHop)
}

func (c changeImpactPolicyChange) summary() string {
	action := "add"
	if c.sign == '-' {
		action = "remove"
	}
	switch c.kind {
	case "prefix-list":
		if c.prefix != "" {
			return fmt.Sprintf("%s prefix-list %s %s", action, c.name, c.prefix)
		}
		return fmt.Sprintf("%s prefix-list %s", action, c.name)
	case "route-map":
		if c.term != "" {
			return fmt.Sprintf("%s route-map %s term %s", action, c.name, c.term)
		}
		return fmt.Sprintf("%s route-map %s", action, c.name)
	default:
		return fmt.Sprintf("%s policy %s", action, c.name)
	}
}

func (c changeImpactBGPPolicyBinding) summary() string {
	action := "add"
	if c.sign == '-' {
		action = "remove"
	}
	return fmt.Sprintf("%s bgp group %s %s route-map %s", action, c.groupName, c.direction, c.policy)
}

func formatClassOfServicePreflight(info *grpcclient.ClassOfServiceInfo) []string {
	if info == nil || info.Capabilities == nil {
		return []string{"qos preflight: capability snapshot unavailable"}
	}
	capabilities := info.Capabilities
	lines := []string{
		"qos preflight:",
		fmt.Sprintf("  metadata binding: %s", yesNo(capabilities.MetadataBindingSupported)),
		fmt.Sprintf("  queue scheduler: %s", yesNo(capabilities.QueueSchedulerSupported)),
		fmt.Sprintf("  policer: %s", yesNo(capabilities.PolicerSupported)),
		fmt.Sprintf("  counters: %s", yesNo(capabilities.CountersSupported)),
	}
	if capabilities.LastError != "" {
		lines = append(lines, "  warning: capability detection error: "+capabilities.LastError)
	}
	if !capabilities.MetadataBindingSupported {
		lines = append(lines, "  warning: metadata binding is unavailable; QoS intent may not persist on VPP interfaces")
	}
	if !capabilities.QueueSchedulerSupported {
		lines = append(lines, "  warning: queue scheduler is unavailable; output QoS remains intent-only")
	}
	if !capabilities.PolicerSupported {
		lines = append(lines, "  warning: policer is unavailable; traffic policing remains intent-only")
	}
	for _, diagnostic := range capabilities.Diagnostics {
		if diagnostic == "" {
			continue
		}
		lines = append(lines, "  diagnostic: "+diagnostic)
	}
	return lines
}

func analyzeChangeImpact(diffText string) changeImpactPreview {
	var preview changeImpactPreview
	for _, rawLine := range strings.Split(diffText, "\n") {
		line := strings.TrimSpace(rawLine)
		if len(line) < 3 {
			continue
		}
		sign := line[0]
		if sign != '+' && sign != '-' {
			continue
		}
		configLine := strings.TrimSpace(line[1:])
		if configLine == "" {
			continue
		}
		if sign == '+' {
			preview.addedLines++
		} else {
			preview.removedLines++
		}
		if strings.HasPrefix(configLine, "set interfaces ") {
			preview.interfaces.add(sign)
			if change, ok := parseInterfaceImpactChange(sign, configLine); ok {
				preview.interfaceChanges = append(preview.interfaceChanges, change)
				if change.operation == "address" {
					preview.interfaceAddressChange = true
				}
			}
		}
		if isStaticRouteConfigLine(configLine) {
			preview.staticRoutes.add(sign)
			if change, ok := parseStaticRouteImpactChange(sign, configLine); ok {
				preview.staticRouteChanges = append(preview.staticRouteChanges, change)
			}
			if isDefaultRouteConfigLine(configLine) {
				preview.defaultRouteChange = true
			}
		}
		if strings.HasPrefix(configLine, "set policy-options ") {
			preview.policyOptions.add(sign)
			if change, ok := parsePolicyImpactChange(sign, configLine); ok {
				preview.policyChanges = append(preview.policyChanges, change)
			}
		}
		if strings.HasPrefix(configLine, "set protocols bgp ") {
			preview.bgp.add(sign)
			if binding, ok := parseBGPPolicyImpactBinding(sign, configLine); ok {
				preview.bgpPolicyBindings = append(preview.bgpPolicyBindings, binding)
			}
		}
		if strings.HasPrefix(configLine, "set protocols ospf ") || strings.HasPrefix(configLine, "set protocols ospf3 ") {
			preview.ospf.add(sign)
		}
		if strings.HasPrefix(configLine, "set protocols bfd ") {
			preview.bfd.add(sign)
		}
		if strings.HasPrefix(configLine, "set protocols evpn ") {
			preview.evpn.add(sign)
		}
		if strings.HasPrefix(configLine, "set routing-instances ") {
			preview.routingInstances.add(sign)
		}
		if strings.HasPrefix(configLine, "set class-of-service ") {
			preview.classOfService.add(sign)
		}
	}
	return preview
}

func isStaticRouteConfigLine(line string) bool {
	return strings.HasPrefix(line, "set routing-options static route ") ||
		(strings.HasPrefix(line, "set routing-instances ") && strings.Contains(line, " routing-options static route "))
}

func isDefaultRouteConfigLine(line string) bool {
	return strings.Contains(line, " static route 0.0.0.0/0 ") ||
		strings.Contains(line, " static route ::/0 ")
}

func parseStaticRouteImpactChange(sign byte, line string) (changeImpactRouteChange, bool) {
	tokens := tokenize(line)
	if len(tokens) == 0 || tokens[0] != "set" {
		return changeImpactRouteChange{}, false
	}

	change := changeImpactRouteChange{sign: sign}
	if len(tokens) > 2 && tokens[1] == "routing-instances" {
		change.routingInstance = tokens[2]
	}

	for i := 1; i+3 < len(tokens); i++ {
		if tokens[i] != "static" || tokens[i+1] != "route" {
			continue
		}
		change.prefix = tokens[i+2]
		for j := i + 3; j+1 < len(tokens); j++ {
			if tokens[j] == "next-hop" {
				change.nextHop = tokens[j+1]
				break
			}
		}
		return change, change.prefix != ""
	}
	return changeImpactRouteChange{}, false
}

func parseInterfaceImpactChange(sign byte, line string) (changeImpactInterfaceChange, bool) {
	tokens := tokenize(line)
	if len(tokens) < 3 || tokens[0] != "set" || tokens[1] != "interfaces" {
		return changeImpactInterfaceChange{}, false
	}
	change := changeImpactInterfaceChange{
		sign:      sign,
		name:      tokens[2],
		operation: "configuration",
	}
	for i := 3; i < len(tokens); i++ {
		switch tokens[i] {
		case "address":
			change.operation = "address"
			if i+1 < len(tokens) {
				change.value = tokens[i+1]
			}
			return change, change.name != ""
		case "description", "mtu", "speed":
			change.operation = tokens[i]
			if i+1 < len(tokens) {
				change.value = tokens[i+1]
			}
			return change, change.name != ""
		case "disable":
			change.operation = "disable"
			return change, change.name != ""
		}
	}
	if len(tokens) > 3 {
		change.operation = strings.Join(tokens[3:], " ")
	}
	return change, change.name != ""
}

func parsePolicyImpactChange(sign byte, line string) (changeImpactPolicyChange, bool) {
	tokens := tokenize(line)
	if len(tokens) < 4 || tokens[0] != "set" || tokens[1] != "policy-options" {
		return changeImpactPolicyChange{}, false
	}
	switch tokens[2] {
	case "prefix-list":
		change := changeImpactPolicyChange{sign: sign, kind: "prefix-list", name: tokens[3]}
		if len(tokens) > 4 {
			change.prefix = tokens[4]
		}
		return change, change.name != ""
	case "policy-statement":
		change := changeImpactPolicyChange{sign: sign, kind: "route-map", name: tokens[3]}
		for i := 4; i+1 < len(tokens); i++ {
			if tokens[i] == "term" {
				change.term = tokens[i+1]
				break
			}
		}
		return change, change.name != ""
	default:
		return changeImpactPolicyChange{}, false
	}
}

func parseBGPPolicyImpactBinding(sign byte, line string) (changeImpactBGPPolicyBinding, bool) {
	tokens := tokenize(line)
	if len(tokens) < 7 || tokens[0] != "set" || tokens[1] != "protocols" || tokens[2] != "bgp" || tokens[3] != "group" {
		return changeImpactBGPPolicyBinding{}, false
	}
	direction := tokens[5]
	if direction != "import" && direction != "export" {
		return changeImpactBGPPolicyBinding{}, false
	}
	binding := changeImpactBGPPolicyBinding{
		sign:      sign,
		groupName: tokens[4],
		direction: direction,
		policy:    tokens[6],
	}
	return binding, binding.groupName != "" && binding.policy != ""
}

func (sh *interactiveShell) cmdEdit(args []string) error {
	if sh.mode != modeConfiguration {
		return fmt.Errorf("'edit' command only available in configuration mode")
	}
	if len(args) == 0 {
		return fmt.Errorf("'edit' requires a configuration path")
	}
	sh.editPath = append(sh.editPath, args...)
	fmt.Printf("Entering edit mode at [edit %s]\n", strings.Join(sh.editPath, " "))
	return nil
}

func (sh *interactiveShell) cmdUp() error {
	if sh.mode != modeConfiguration {
		return fmt.Errorf("'up' command only available in configuration mode")
	}
	if len(sh.editPath) > 0 {
		sh.editPath = sh.editPath[:len(sh.editPath)-1]
	}
	if len(sh.editPath) == 0 {
		fmt.Println("At top level")
	} else {
		fmt.Printf("Now at [edit %s]\n", strings.Join(sh.editPath, " "))
	}
	return nil
}

func (sh *interactiveShell) cmdTop() error {
	if sh.mode != modeConfiguration {
		return fmt.Errorf("'top' command only available in configuration mode")
	}
	sh.editPath = nil
	fmt.Println("At top level")
	return nil
}

func (sh *interactiveShell) showHelp() {
	fmt.Println("Available commands:")
	fmt.Println()
	if sh.mode == modeOperational {
		fmt.Println("Operational mode commands:")
		fmt.Println("  help                          Show this help message")
		fmt.Println("  configure                     Enter configuration mode")
		fmt.Println("  show configuration            Show running configuration")
		fmt.Println("  show configuration rollback <N> Show archived config N commits back")
		fmt.Println("  show interfaces [<name>]      Show interface status")
		fmt.Println("  show routing-instances [name] Show routing-instance table mapping")
		fmt.Println("  show routes [prefix <cidr>] [protocol <proto>] Show route status")
		fmt.Println("  show bgp neighbors            Show BGP neighbor status")
		fmt.Println("  show bgp summary              Show raw BGP summary")
		fmt.Println("  show bgp neighbor <ip>        Show raw BGP neighbor details")
		fmt.Println("  show ospf neighbor            Show OSPFv2 neighbors")
		fmt.Println("  show ospf3 neighbor           Show OSPFv3 neighbors")
		fmt.Println("  show vrrp                     Show VRRP status")
		fmt.Println("  show bfd status               Show BFD operational state")
		fmt.Println("  show bfd [brief|counters]     Show raw BFD status")
		fmt.Println("  show bfd peer <ip> [counters] Show BFD peer details")
		fmt.Println("  show evpn                     Show EVPN/VXLAN overlay intent")
		fmt.Println("  show telemetry [path <path>]... [interval <duration>] [count <events>]")
		fmt.Println("                                Show telemetry events as JSON lines")
		fmt.Println("  show lcp                      Show VPP LCP reconciliation status")
		fmt.Println("  show ha                       Show HA convergence status")
		fmt.Println("  show class-of-service         Show class-of-service intent")
		fmt.Println("  show route [inet|inet6]                 Show routing table")
		fmt.Println("  show route [inet|inet6] protocol <proto> Show routes by protocol")
		fmt.Println("  exit, quit                    Exit interactive CLI")
	} else {
		fmt.Println("Configuration mode commands:")
		fmt.Println("  help                      Show this help message")
		fmt.Println("  set <config>              Add or modify configuration")
		fmt.Println("  delete <config>           Delete configuration")
		fmt.Println("  show                      Show candidate configuration")
		fmt.Println("  show configuration rollback <N> Show archived config N commits back")
		fmt.Println("  show | compare            Show differences from running config")
		fmt.Println("  commit                    Commit candidate configuration")
		fmt.Println("  commit check              Validate and preview impact without committing")
		fmt.Println("  commit and-quit           Commit and exit configuration mode")
		fmt.Println("  commit comment <msg>      Commit with custom message")
		fmt.Println("  rollback <N>              Roll back N commits")
		fmt.Println("  discard-changes           Discard all candidate changes")
		fmt.Println("  show history [N]          Show last N commits")
		fmt.Println("  edit <path>               Navigate to configuration hierarchy")
		fmt.Println("  up                        Move up one level in hierarchy")
		fmt.Println("  top                       Move to top level of hierarchy")
		fmt.Println("  exit, quit                Exit configuration mode")
		if len(sh.editPath) > 0 {
			fmt.Printf("\nCurrent edit path: [edit %s]\n", strings.Join(sh.editPath, " "))
		}
	}
	fmt.Println()
}

// --- Output formatters ---

func printInterfaces(ifaces []grpcclient.InterfaceInfo) {
	if len(ifaces) == 0 {
		fmt.Println("No interfaces found")
		return
	}
	fmt.Printf("%-20s %-8s %-8s %-6s %-18s %-10s %-12s %-12s %-16s %-15s %s\n",
		"Interface", "Admin", "Oper", "MTU", "MAC", "Speed", "RX-Packets", "TX-Packets", "QoS", "Tables", "Queues")
	fmt.Println(strings.Repeat("-", 159))
	for _, iface := range ifaces {
		fmt.Printf("%-20s %-8s %-8s %-6d %-18s %-10d %-12d %-12d %-16s %-15s %s\n",
			iface.Name, iface.AdminStatus, iface.OperStatus,
			iface.MTU, iface.MAC, iface.Speed, iface.RxPackets, iface.TxPackets, interfaceQoSProfile(iface), interfaceTableSummary(iface), interfaceQueueSummary(iface))
	}
}

func interfaceQoSProfile(iface grpcclient.InterfaceInfo) string {
	if iface.QoSProfile == "" {
		return "-"
	}
	return iface.QoSProfile
}

func interfaceTableSummary(iface grpcclient.InterfaceInfo) string {
	if iface.IPv4TableID == 0 && iface.IPv6TableID == 0 {
		return "-"
	}
	if iface.IPv4TableID == iface.IPv6TableID {
		return fmt.Sprintf("v4/v6:%d", iface.IPv4TableID)
	}
	return fmt.Sprintf("v4:%d v6:%d", iface.IPv4TableID, iface.IPv6TableID)
}

func interfaceQueueSummary(iface grpcclient.InterfaceInfo) string {
	var parts []string
	for _, queue := range iface.RxQueues {
		mode := queue.Mode
		if mode == "" {
			mode = "unknown"
		}
		parts = append(parts, fmt.Sprintf("rx%d:w%d/%s", queue.QueueID, queue.WorkerID, mode))
	}
	for _, queue := range iface.TxQueues {
		suffix := ""
		if queue.Shared {
			suffix = "*"
		}
		parts = append(parts, fmt.Sprintf("tx%d:%s%s", queue.QueueID, formatQueueThreads(queue.Threads), suffix))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " ")
}

func formatQueueThreads(threads []uint32) string {
	if len(threads) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(threads))
	for _, thread := range threads {
		parts = append(parts, strconv.FormatUint(uint64(thread), 10))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func printRoutingInstances(instances []grpcclient.RoutingInstanceInfo) {
	if len(instances) == 0 {
		fmt.Println("No routing instances found")
		return
	}
	fmt.Printf("%-24s %-8s %-18s %-15s %s\n", "Instance", "Type", "RD", "VPP tables", "Interfaces")
	fmt.Println(strings.Repeat("-", 98))
	for _, instance := range instances {
		fmt.Printf("%-24s %-8s %-18s %-15s %s\n",
			instance.Name,
			formatRoutingInstanceValue(instance.InstanceType),
			formatRoutingInstanceValue(instance.RouteDistinguisher),
			routingInstanceTableSummary(instance),
			formatRoutingInstanceList(instance.Interfaces),
		)
	}

	if !routingInstancesHavePolicy(instances) {
		return
	}
	fmt.Println()
	fmt.Println("Import/export")
	fmt.Printf("%-24s %-32s %-32s %-24s %-24s\n", "Instance", "Import RT", "Export RT", "Import policy", "Export policy")
	fmt.Println(strings.Repeat("-", 140))
	for _, instance := range instances {
		fmt.Printf("%-24s %-32s %-32s %-24s %-24s\n",
			instance.Name,
			formatRoutingInstanceList(instance.ImportTargets),
			formatRoutingInstanceList(instance.ExportTargets),
			formatRoutingInstanceList(instance.ImportPolicies),
			formatRoutingInstanceList(instance.ExportPolicies),
		)
	}
}

func routingInstanceTableSummary(instance grpcclient.RoutingInstanceInfo) string {
	if instance.IPv4TableID == 0 && instance.IPv6TableID == 0 {
		return "-"
	}
	if instance.IPv4TableID == instance.IPv6TableID {
		return fmt.Sprintf("v4/v6:%d", instance.IPv4TableID)
	}
	return fmt.Sprintf("v4:%d v6:%d", instance.IPv4TableID, instance.IPv6TableID)
}

func routingInstancesHavePolicy(instances []grpcclient.RoutingInstanceInfo) bool {
	for _, instance := range instances {
		if len(instance.ImportTargets) > 0 || len(instance.ExportTargets) > 0 ||
			len(instance.ImportPolicies) > 0 || len(instance.ExportPolicies) > 0 {
			return true
		}
	}
	return false
}

func formatRoutingInstanceValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func formatRoutingInstanceList(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ",")
}

func printRoutes(routes []grpcclient.RouteInfo) {
	if len(routes) == 0 {
		fmt.Println("No routes found")
		return
	}
	fmt.Printf("%-43s %-39s %-12s %-8s %-16s %-8s\n",
		"Prefix", "Next hop", "Protocol", "Metric", "Interface", "Active")
	fmt.Println(strings.Repeat("-", 131))
	for _, route := range routes {
		fmt.Printf("%-43s %-39s %-12s %-8d %-16s %-8s\n",
			formatRouteValue(route.Prefix),
			formatRouteValue(route.NextHop),
			formatRouteValue(route.Protocol),
			route.Metric,
			formatRouteValue(route.Interface),
			yesNo(route.Active),
		)
	}
}

func formatRouteValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func printBGPNeighbors(neighbors []grpcclient.BGPNeighborInfo) {
	if len(neighbors) == 0 {
		fmt.Println("No BGP neighbors found")
		return
	}
	fmt.Printf("%-39s %-10s %-16s %-14s %-12s %-12s\n",
		"Peer", "AS", "State", "Uptime", "Prefixes in", "Prefixes out")
	fmt.Println(strings.Repeat("-", 109))
	for _, neighbor := range neighbors {
		fmt.Printf("%-39s %-10d %-16s %-14s %-12d %-12d\n",
			formatBGPValue(neighbor.PeerAddress),
			neighbor.PeerAS,
			formatBGPValue(neighbor.State),
			formatBGPUptime(neighbor.UptimeSecs),
			neighbor.PrefixReceived,
			neighbor.PrefixSent,
		)
	}
}

func printOSPFNeighbors(neighbors []grpcclient.OSPFNeighborInfo) {
	if len(neighbors) == 0 {
		fmt.Println("No OSPF neighbors found")
		return
	}
	fmt.Printf("%-15s %-39s %-16s %-14s %-10s %-10s %-10s\n",
		"Router ID", "Address", "Interface", "State", "Role", "Dead", "Uptime")
	fmt.Println(strings.Repeat("-", 122))
	for _, neighbor := range neighbors {
		fmt.Printf("%-15s %-39s %-16s %-14s %-10s %-10s %-10s\n",
			formatBGPValue(neighbor.RouterID),
			formatBGPValue(neighbor.Address),
			formatBGPValue(neighbor.Interface),
			formatBGPValue(neighbor.State),
			formatBGPValue(neighbor.Role),
			formatBGPUptime(neighbor.DeadTimeSecs),
			formatBGPUptime(neighbor.UptimeSecs),
		)
	}
}

func formatBGPValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func formatBGPUptime(seconds uint64) string {
	if seconds == 0 {
		return "-"
	}
	days := seconds / 86400
	seconds %= 86400
	hours := seconds / 3600
	seconds %= 3600
	minutes := seconds / 60
	seconds %= 60
	if days > 0 {
		return fmt.Sprintf("%dd%02dh%02dm%02ds", days, hours, minutes, seconds)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh%02dm%02ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm%02ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

func printLCPReconciliation(info *grpcclient.LCPReconciliationInfo) {
	if info == nil {
		fmt.Println("No LCP reconciliation status found")
		return
	}
	fmt.Printf("%-18s %s\n", "State", lcpReconciliationState(info))
	fmt.Printf("%-18s %s\n", "Last check", formatOptionalTime(info.LastRun))
	fmt.Printf("%-18s %d\n", "Pairs", info.PairCount)
	if info.LastError != "" {
		fmt.Printf("%-18s %s\n", "Last error", info.LastError)
	}
	if len(info.Inconsistencies) == 0 {
		return
	}
	fmt.Println("Inconsistencies")
	for _, issue := range info.Inconsistencies {
		fmt.Printf("  - %s\n", issue)
	}
}

func printHAStatus(info *grpcclient.HAStatusInfo) {
	if info == nil {
		fmt.Println("No HA status found")
		return
	}
	fmt.Printf("%-18s %s\n", "State", haState(info))
	fmt.Printf("%-18s %s\n", "Configured", yesNo(info.Configured))
	fmt.Printf("%-18s %s\n", "Converged", yesNo(info.Converged))
	fmt.Printf("%-18s %d\n", "VRRP groups", info.VRRPGroups)
	fmt.Printf("%-18s %d\n", "Cluster nodes", info.ClusterNodes)
	fmt.Printf("%-18s %s\n", "Cluster sync", clusterSyncState(info))
	fmt.Printf("%-18s %d/%d\n", "FRR VRRP", info.FRRVRRPActiveGroups, info.FRRVRRPConfiguredGroups)
	fmt.Printf("%-18s %s\n", "FRR last check", formatOptionalTime(info.FRRVRRPLastCheck))
	fmt.Printf("%-18s %s\n", "FRR BFD", haBFDState(info))
	fmt.Printf("%-18s %s\n", "BFD last check", formatOptionalTime(info.FRRBFDLastCheck))
	fmt.Printf("%-18s %s\n", "VPP LCP", lcpReconciliationState(&grpcclient.LCPReconciliationInfo{
		LastRun:         info.VPPLCPLastCheck,
		PairCount:       info.VPPLCPPairs,
		Inconsistencies: info.VPPLCPInconsistencies,
		LastError:       info.VPPLCPLastError,
	}))
	fmt.Printf("%-18s %s\n", "LCP last check", formatOptionalTime(info.VPPLCPLastCheck))
	if len(info.Issues) == 0 {
		return
	}
	fmt.Println("Issues")
	for _, issue := range info.Issues {
		fmt.Printf("  - %s\n", issue)
	}
}

func printBFDStatus(info *grpcclient.BFDStatusInfo) {
	if !hasBFDStatus(info) {
		fmt.Println("No BFD operational status found")
		return
	}
	fmt.Printf("%-18s %s\n", "State", bfdOperationalState(info))
	fmt.Printf("%-18s %s\n", "Last check", formatOptionalTime(info.LastRun))
	fmt.Printf("%-18s %d\n", "Configured peers", info.ConfiguredPeers)
	fmt.Printf("%-18s %d\n", "Observed peers", info.ObservedPeers)
	fmt.Printf("%-18s %d\n", "Up peers", info.UpPeers)
	fmt.Printf("%-18s %d\n", "Down peers", info.DownPeers)
	fmt.Printf("%-18s %d\n", "Session down", info.SessionDownEvents)
	fmt.Printf("%-18s %d\n", "RX fail packets", info.RxFailPackets)
	if info.LastError != "" {
		fmt.Printf("%-18s %s\n", "Last error", info.LastError)
	}
	if len(info.Peers) > 0 {
		fmt.Println()
		fmt.Println("Peers")
		fmt.Printf("%-39s %-39s %-16s %-12s %-10s %-8s %-12s %-12s\n",
			"Peer", "Local", "Interface", "VRF", "Status", "Up", "Down events", "RX fails")
		fmt.Println(strings.Repeat("-", 158))
		for _, peer := range info.Peers {
			fmt.Printf("%-39s %-39s %-16s %-12s %-10s %-8s %-12d %-12d\n",
				formatBFDValue(peer.Peer),
				formatBFDValue(peer.LocalAddress),
				formatBFDValue(peer.Interface),
				formatBFDValue(peer.VRF),
				formatBFDValue(peer.Status),
				yesNo(peer.Up),
				peer.SessionDownEvents,
				peer.RxFailPackets,
			)
			if peer.Diagnostic != "" || peer.RemoteDiagnostic != "" || !peer.Observed {
				fmt.Printf("  diagnostic: %s remote: %s observed: %s\n",
					formatBFDValue(peer.Diagnostic),
					formatBFDValue(peer.RemoteDiagnostic),
					yesNo(peer.Observed),
				)
			}
		}
	}
	if len(info.Issues) == 0 {
		return
	}
	fmt.Println()
	fmt.Println("Issues")
	for _, issue := range info.Issues {
		fmt.Printf("  - %s\n", issue)
	}
}

func printClassOfService(info *grpcclient.ClassOfServiceInfo) {
	if info == nil || (len(info.ForwardingClasses) == 0 && len(info.TrafficControlProfiles) == 0 && len(info.Interfaces) == 0 && info.Capabilities == nil) {
		fmt.Println("No class-of-service configuration found")
		return
	}
	if len(info.ForwardingClasses) == 0 && len(info.TrafficControlProfiles) == 0 && len(info.Interfaces) == 0 {
		fmt.Println("No class-of-service configuration found")
	} else {
		fmt.Printf("%-18s %s\n", "Enforcement", formatCoSValue(info.EnforcementStatus))
	}

	if len(info.ForwardingClasses) > 0 {
		fmt.Println()
		fmt.Println("Forwarding classes")
		fmt.Printf("%-32s %-8s\n", "Name", "Queue")
		fmt.Println(strings.Repeat("-", 41))
		for _, fc := range info.ForwardingClasses {
			fmt.Printf("%-32s %-8d\n", fc.Name, fc.Queue)
		}
	}

	if len(info.TrafficControlProfiles) > 0 {
		fmt.Println()
		fmt.Println("Traffic-control profiles")
		fmt.Printf("%-32s %-16s %-24s %-14s\n", "Name", "Shaping rate", "Scheduler map", "Enforcement")
		fmt.Println(strings.Repeat("-", 88))
		for _, profile := range info.TrafficControlProfiles {
			fmt.Printf("%-32s %-16s %-24s %-14s\n",
				profile.Name,
				formatCoSRate(profile.ShapingRate),
				formatCoSValue(profile.SchedulerMap),
				formatCoSValue(profile.EnforcementStatus),
			)
		}
	}

	if len(info.Interfaces) > 0 {
		fmt.Println()
		fmt.Println("Interfaces")
		fmt.Printf("%-24s %-32s %-14s\n", "Interface", "Output profile", "Enforcement")
		fmt.Println(strings.Repeat("-", 72))
		for _, iface := range info.Interfaces {
			fmt.Printf("%-24s %-32s %-14s\n",
				iface.Name,
				formatCoSValue(iface.OutputTrafficControlProfile),
				formatCoSValue(iface.EnforcementStatus),
			)
		}
	}

	if info.Capabilities != nil {
		fmt.Println()
		fmt.Println("VPP QoS capabilities")
		fmt.Printf("%-24s %s\n", "Metadata binding", yesNo(info.Capabilities.MetadataBindingSupported))
		fmt.Printf("%-24s %s\n", "Queue scheduler", yesNo(info.Capabilities.QueueSchedulerSupported))
		fmt.Printf("%-24s %s\n", "Policer", yesNo(info.Capabilities.PolicerSupported))
		fmt.Printf("%-24s %s\n", "Counters", yesNo(info.Capabilities.CountersSupported))
		fmt.Printf("%-24s %s\n", "Last check", formatOptionalTime(info.Capabilities.LastCheck))
		fmt.Printf("%-24s %s\n", "Last error", formatCoSValue(info.Capabilities.LastError))
		if len(info.Capabilities.Diagnostics) > 0 {
			fmt.Println()
			fmt.Println("VPP QoS diagnostics")
			for _, diagnostic := range info.Capabilities.Diagnostics {
				fmt.Printf("  - %s\n", diagnostic)
			}
		}
	}
}

type evpnTelemetrySnapshot struct {
	VNIs []evpnTelemetryVNI `json:"vnis"`
}

type evpnTelemetryVNI struct {
	VNI                int      `json:"vni"`
	Type               string   `json:"type,omitempty"`
	BridgeDomain       string   `json:"bridge_domain,omitempty"`
	VLANID             int      `json:"vlan_id,omitempty"`
	RoutingInstance    string   `json:"routing_instance,omitempty"`
	RouteDistinguisher string   `json:"route_distinguisher,omitempty"`
	VRFTarget          string   `json:"vrf_target,omitempty"`
	VRFTargetImport    []string `json:"vrf_target_import,omitempty"`
	VRFTargetExport    []string `json:"vrf_target_export,omitempty"`
	SourceInterface    string   `json:"source_interface,omitempty"`
	SourceAddress      string   `json:"source_address,omitempty"`
	MulticastGroup     string   `json:"multicast_group,omitempty"`
	RemoteVTEP         string   `json:"remote_vtep,omitempty"`
}

type evpnTelemetryCounts struct {
	total     int
	l2        int
	l3        int
	multicast int
}

func showEVPN(ctx context.Context, client showClient) error {
	snapshot, err := fetchEVPNTelemetrySnapshot(ctx, client)
	if err != nil {
		return err
	}
	printEVPN(snapshot)
	return nil
}

func fetchEVPNTelemetrySnapshot(ctx context.Context, client showClient) (*evpnTelemetrySnapshot, error) {
	stream, err := client.SubscribeTelemetry(ctx, []string{"/overlays/evpn"}, 0, true)
	if err != nil {
		return nil, err
	}
	event, err := stream.Recv()
	if err == io.EOF {
		return nil, fmt.Errorf("EVPN telemetry snapshot was empty")
	}
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, fmt.Errorf("EVPN telemetry snapshot was nil")
	}
	var snapshot evpnTelemetrySnapshot
	if err := json.Unmarshal([]byte(event.JSONPayload), &snapshot); err != nil {
		return nil, fmt.Errorf("decode EVPN telemetry snapshot: %w", err)
	}
	return &snapshot, nil
}

func printEVPN(snapshot *evpnTelemetrySnapshot) {
	if snapshot == nil || len(snapshot.VNIs) == 0 {
		fmt.Println("No EVPN/VXLAN VNI configuration found")
		return
	}
	counts := countEVPNVNIs(snapshot.VNIs)
	fmt.Printf("%-18s %s\n", "Configured", yesNo(counts.total > 0))
	fmt.Printf("%-18s %d\n", "VNIs", counts.total)
	fmt.Printf("%-18s %d\n", "L2 VNIs", counts.l2)
	fmt.Printf("%-18s %d\n", "L3 VNIs", counts.l3)
	fmt.Printf("%-18s %d\n", "Multicast VNIs", counts.multicast)

	fmt.Println()
	fmt.Println("VNIs")
	fmt.Printf("%-8s %-6s %-20s %-20s %-8s %-18s %-28s %-24s %s\n",
		"VNI", "Type", "Bridge domain", "Routing instance", "VLAN", "RD", "Route targets", "Source", "Endpoint")
	fmt.Println(strings.Repeat("-", 169))
	for _, vni := range snapshot.VNIs {
		fmt.Printf("%-8d %-6s %-20s %-20s %-8s %-18s %-28s %-24s %s\n",
			vni.VNI,
			formatEVPNValue(vni.Type),
			formatEVPNValue(vni.BridgeDomain),
			formatEVPNValue(vni.RoutingInstance),
			formatEVPNVLAN(vni.VLANID),
			formatEVPNValue(vni.RouteDistinguisher),
			formatEVPNRouteTargets(vni),
			formatEVPNSource(vni),
			formatEVPNEndpoint(vni),
		)
	}
}

func countEVPNVNIs(vnis []evpnTelemetryVNI) evpnTelemetryCounts {
	var counts evpnTelemetryCounts
	for _, vni := range vnis {
		counts.total++
		switch strings.ToLower(vni.Type) {
		case "l2":
			counts.l2++
		case "l3":
			counts.l3++
		}
		if vni.MulticastGroup != "" {
			counts.multicast++
		}
	}
	return counts
}

func formatEVPNValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func formatEVPNVLAN(vlanID int) string {
	if vlanID == 0 {
		return "-"
	}
	return strconv.Itoa(vlanID)
}

func formatEVPNRouteTargets(vni evpnTelemetryVNI) string {
	var parts []string
	if vni.VRFTarget != "" {
		parts = append(parts, vni.VRFTarget)
	}
	if len(vni.VRFTargetImport) > 0 {
		parts = append(parts, "import:"+strings.Join(vni.VRFTargetImport, ","))
	}
	if len(vni.VRFTargetExport) > 0 {
		parts = append(parts, "export:"+strings.Join(vni.VRFTargetExport, ","))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " ")
}

func formatEVPNSource(vni evpnTelemetryVNI) string {
	switch {
	case vni.SourceInterface != "" && vni.SourceAddress != "":
		return vni.SourceInterface + "@" + vni.SourceAddress
	case vni.SourceInterface != "":
		return vni.SourceInterface
	case vni.SourceAddress != "":
		return vni.SourceAddress
	default:
		return "-"
	}
}

func formatEVPNEndpoint(vni evpnTelemetryVNI) string {
	switch {
	case vni.MulticastGroup != "":
		return "multicast:" + vni.MulticastGroup
	case vni.RemoteVTEP != "":
		return "remote:" + vni.RemoteVTEP
	default:
		return "-"
	}
}

func formatCoSValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func formatCoSRate(value uint64) string {
	if value == 0 {
		return "-"
	}
	return strconv.FormatUint(value, 10)
}

func haState(info *grpcclient.HAStatusInfo) string {
	if info == nil || !info.Configured {
		return "not configured"
	}
	if info.Converged {
		return "converged"
	}
	return "issues"
}

func clusterSyncState(info *grpcclient.HAStatusInfo) string {
	if info == nil || !info.ClusterEtcdSync {
		return "not configured"
	}
	if info.ClusterSyncAligned {
		return "aligned"
	}
	return "mismatch"
}

func haBFDState(info *grpcclient.HAStatusInfo) string {
	if info == nil || (info.FRRBFDConfiguredPeers == 0 && info.FRRBFDObservedPeers == 0) {
		return "not configured"
	}
	totalPeers := info.FRRBFDConfiguredPeers
	if totalPeers == 0 {
		totalPeers = info.FRRBFDObservedPeers
	}
	state := fmt.Sprintf("%d/%d up", info.FRRBFDUpPeers, totalPeers)
	if info.FRRBFDLastError != "" || len(info.FRRBFDIssues) > 0 ||
		info.FRRBFDDownPeers > 0 || info.FRRBFDUpPeers < info.FRRBFDConfiguredPeers {
		return state + " (issues)"
	}
	if info.FRRBFDLastCheck.IsZero() {
		return state + " (unknown)"
	}
	return state
}

func hasBFDStatus(info *grpcclient.BFDStatusInfo) bool {
	if info == nil {
		return false
	}
	return !info.LastRun.IsZero() ||
		info.ConfiguredPeers != 0 ||
		info.ObservedPeers != 0 ||
		info.UpPeers != 0 ||
		info.DownPeers != 0 ||
		info.SessionDownEvents != 0 ||
		info.RxFailPackets != 0 ||
		len(info.Peers) != 0 ||
		len(info.Issues) != 0 ||
		info.LastError != ""
}

func bfdOperationalState(info *grpcclient.BFDStatusInfo) string {
	if !hasBFDStatus(info) {
		return "unknown"
	}
	if info.LastError != "" {
		return "check failed"
	}
	if len(info.Issues) > 0 || info.DownPeers > 0 {
		return "issues"
	}
	return "converged"
}

func formatBFDValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func lcpReconciliationState(info *grpcclient.LCPReconciliationInfo) string {
	if info == nil || info.LastRun.IsZero() {
		return "unknown"
	}
	if info.LastError != "" {
		return "check failed"
	}
	if len(info.Inconsistencies) > 0 {
		return "mismatch"
	}
	return "consistent"
}

func formatOptionalTime(ts time.Time) string {
	if ts.IsZero() {
		return "never"
	}
	return ts.Local().Format(time.RFC3339)
}

func printCommandOutput(output string) {
	fmt.Print(output)
	if output != "" && !strings.HasSuffix(output, "\n") {
		fmt.Println()
	}
}

type telemetryCLIOptions struct {
	paths    []string
	interval time.Duration
	once     bool
	count    int
}

type telemetryOutputEvent struct {
	Sequence      uint64          `json:"sequence"`
	Timestamp     string          `json:"timestamp,omitempty"`
	Path          string          `json:"path"`
	Cardinality   string          `json:"cardinality,omitempty"`
	PayloadSchema string          `json:"payload_schema,omitempty"`
	EventType     string          `json:"event_type"`
	Encoding      string          `json:"encoding"`
	SchemaVersion string          `json:"schema_version"`
	PayloadBytes  int             `json:"payload_bytes"`
	Payload       json.RawMessage `json:"payload"`
}

func showTelemetry(ctx context.Context, client showClient, args []string) error {
	catalogOpts, isCatalog, err := telemetryCatalogOptions(args)
	if isCatalog {
		if err != nil {
			return err
		}
		catalog := grpcclient.NewTelemetryCatalog()
		if catalogOpts.live {
			liveCatalog, err := client.GetTelemetryCatalogWithFilter(ctx, grpcclient.TelemetryCatalogFilter{
				Paths:          catalogOpts.paths,
				Cardinalities:  catalogOpts.cardinalities,
				PayloadSchemas: catalogOpts.payloadSchemas,
				Encodings:      catalogOpts.encodings,
				DefaultOnly:    catalogOpts.defaultOnly,
			})
			if err != nil {
				return err
			}
			catalog = liveCatalog
			catalogOpts.defaultOnly = false
			catalogOpts.paths = nil
			catalogOpts.cardinalities = nil
			catalogOpts.payloadSchemas = nil
			catalogOpts.encodings = nil
		}
		printTelemetryCatalog(catalog, filterTelemetryPathCatalog(catalog.Paths, catalogOpts))
		return nil
	}
	opts, err := telemetryOptions(args)
	if err != nil {
		return err
	}
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream, err := client.SubscribeTelemetry(streamCtx, opts.paths, opts.interval, opts.once)
	if err != nil {
		return err
	}
	events := 0
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := printTelemetryEvent(event); err != nil {
			return err
		}
		events++
		if opts.count > 0 && events >= opts.count {
			return nil
		}
	}
}

type telemetryCatalogCLIOptions struct {
	live           bool
	defaultOnly    bool
	paths          []string
	cardinalities  []string
	payloadSchemas []string
	encodings      []string
}

func isTelemetryCatalogCommand(args []string) bool {
	_, ok, err := telemetryCatalogOptions(args)
	return ok && err == nil
}

func telemetryCatalogOptions(args []string) (telemetryCatalogCLIOptions, bool, error) {
	var opts telemetryCatalogCLIOptions
	if len(args) == 0 || (args[0] != "paths" && args[0] != "catalog") {
		return opts, false, nil
	}
	args = args[1:]
	for len(args) > 0 {
		switch args[0] {
		case "live":
			if opts.live {
				return opts, true, telemetryUsageError("'show telemetry paths live' specified more than once")
			}
			opts.live = true
			args = args[1:]
		case "default", "default-only":
			opts.defaultOnly = true
			args = args[1:]
		case "cardinality":
			if len(args) < 2 {
				return opts, true, telemetryUsageError("'show telemetry paths cardinality' requires a cardinality hint")
			}
			opts.cardinalities = append(opts.cardinalities, args[1])
			args = args[2:]
		case "path":
			if len(args) < 2 {
				return opts, true, telemetryUsageError("'show telemetry paths path' requires a telemetry path or alias")
			}
			opts.paths = append(opts.paths, args[1])
			args = args[2:]
		case "payload-schema", "schema":
			if len(args) < 2 {
				return opts, true, telemetryUsageError("'show telemetry paths payload-schema' requires a schema ID")
			}
			opts.payloadSchemas = append(opts.payloadSchemas, args[1])
			args = args[2:]
		case "encoding":
			if len(args) < 2 {
				return opts, true, telemetryUsageError("'show telemetry paths encoding' requires a payload encoding")
			}
			opts.encodings = append(opts.encodings, args[1])
			args = args[2:]
		default:
			return opts, true, telemetryUsageError("unknown telemetry catalog option: %s", args[0])
		}
	}
	return opts, true, nil
}

func filterTelemetryPathCatalog(catalog []grpcclient.TelemetryPathInfo, opts telemetryCatalogCLIOptions) []grpcclient.TelemetryPathInfo {
	paths := normalizedCatalogPathFilterSet(opts.paths)
	cardinalities := normalizedCatalogFilterSet(opts.cardinalities)
	payloadSchemas := normalizedCatalogFilterSet(opts.payloadSchemas)
	encodings := normalizedCatalogFilterSet(opts.encodings)
	if len(encodings) > 0 {
		if _, ok := encodings[normalizedCatalogFilterValue(grpcclient.TelemetryEncoding())]; !ok {
			return nil
		}
	}
	if !opts.defaultOnly && len(paths) == 0 && len(cardinalities) == 0 && len(payloadSchemas) == 0 && len(encodings) == 0 {
		return catalog
	}

	filtered := make([]grpcclient.TelemetryPathInfo, 0, len(catalog))
	for _, info := range catalog {
		if opts.defaultOnly && !info.Default {
			continue
		}
		if len(paths) > 0 && !telemetryCatalogInfoMatchesPath(info, paths) {
			continue
		}
		if len(cardinalities) > 0 {
			if _, ok := cardinalities[normalizedCatalogFilterValue(info.Cardinality)]; !ok {
				continue
			}
		}
		if len(payloadSchemas) > 0 {
			if _, ok := payloadSchemas[normalizedCatalogFilterValue(info.PayloadSchema)]; !ok {
				continue
			}
		}
		filtered = append(filtered, info)
	}
	return filtered
}

func normalizedCatalogPathFilterSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := normalizedCatalogPathFilterValue(value)
		if normalized != "" {
			set[normalized] = struct{}{}
		}
	}
	return set
}

func telemetryCatalogInfoMatchesPath(info grpcclient.TelemetryPathInfo, paths map[string]struct{}) bool {
	if _, ok := paths[normalizedCatalogPathFilterValue(info.Path)]; ok {
		return true
	}
	for _, alias := range info.Aliases {
		if _, ok := paths[normalizedCatalogPathFilterValue(alias)]; ok {
			return true
		}
	}
	return false
}

func normalizedCatalogPathFilterValue(value string) string {
	path := strings.ToLower(strings.TrimSpace(value))
	if path == "" {
		return ""
	}
	return "/" + strings.Trim(path, "/")
}

func normalizedCatalogFilterSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := normalizedCatalogFilterValue(value)
		if normalized != "" {
			set[normalized] = struct{}{}
		}
	}
	return set
}

func normalizedCatalogFilterValue(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func printTelemetryCatalog(catalog grpcclient.TelemetryCatalog, paths []grpcclient.TelemetryPathInfo) {
	if hint := formatTelemetryCatalogIntervalHints(catalog); hint != "" {
		fmt.Println(hint)
	}
	printTelemetryPathCatalog(paths)
}

func formatTelemetryCatalogIntervalHints(catalog grpcclient.TelemetryCatalog) string {
	if catalog.DefaultSampleIntervalMs == 0 && catalog.MinSampleIntervalMs == 0 && catalog.MaxSampleIntervalMs == 0 {
		return ""
	}
	return fmt.Sprintf("Sample interval: default=%dms min=%dms max=%dms",
		catalog.DefaultSampleIntervalMs,
		catalog.MinSampleIntervalMs,
		catalog.MaxSampleIntervalMs)
}

func printTelemetryPathCatalog(catalog []grpcclient.TelemetryPathInfo) {
	if len(catalog) == 0 {
		fmt.Println("No telemetry paths found")
		return
	}
	fmt.Printf("%-28s %-18s %-8s %-28s %-42s %s\n", "Path", "Cardinality", "Default", "Aliases", "Payload schema", "Description")
	fmt.Println(strings.Repeat("-", 168))
	for _, info := range catalog {
		fmt.Printf("%-28s %-18s %-8s %-28s %-42s %s\n",
			formatTelemetryCatalogValue(info.Path),
			formatTelemetryCatalogValue(info.Cardinality),
			yesNo(info.Default),
			formatTelemetryCatalogList(info.Aliases),
			formatTelemetryCatalogValue(info.PayloadSchema),
			formatTelemetryCatalogValue(info.Description),
		)
	}
}

func formatTelemetryCatalogValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func formatTelemetryCatalogList(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ",")
}

func telemetryOptions(args []string) (telemetryCLIOptions, error) {
	opts := telemetryCLIOptions{once: true}
	for len(args) > 0 {
		switch args[0] {
		case "path":
			if len(args) < 2 {
				return opts, telemetryUsageError("'show telemetry path' requires a path")
			}
			opts.paths = append(opts.paths, args[1])
			args = args[2:]
		case "interval":
			if len(args) < 2 {
				return opts, telemetryUsageError("'show telemetry interval' requires a duration such as 5s")
			}
			interval, err := time.ParseDuration(args[1])
			if err != nil || interval <= 0 {
				return opts, telemetryUsageError("invalid telemetry interval %q", args[1])
			}
			opts.interval = interval
			args = args[2:]
		case "count":
			if len(args) < 2 {
				return opts, telemetryUsageError("'show telemetry count' requires a positive event count")
			}
			count, err := strconv.Atoi(args[1])
			if err != nil || count <= 0 {
				return opts, telemetryUsageError("invalid telemetry event count %q", args[1])
			}
			opts.count = count
			opts.once = false
			args = args[2:]
		case "once":
			opts.once = true
			opts.count = 0
			args = args[1:]
		default:
			opts.paths = append(opts.paths, args[0])
			args = args[1:]
		}
	}
	return opts, nil
}

func telemetryUsageError(format string, args ...interface{}) error {
	return fmt.Errorf("%w: %s", errTelemetryUsage, fmt.Sprintf(format, args...))
}

func isTelemetryUsageError(err error) bool {
	return errors.Is(err, errTelemetryUsage)
}

func printTelemetryEvent(event *grpcclient.TelemetryEvent) error {
	if event == nil {
		return nil
	}
	payload := json.RawMessage(event.JSONPayload)
	if len(payload) == 0 {
		payload = json.RawMessage("null")
	} else if !json.Valid(payload) {
		encoded, err := json.Marshal(event.JSONPayload)
		if err != nil {
			return err
		}
		payload = json.RawMessage(encoded)
	}

	timestamp := ""
	if !event.Timestamp.IsZero() {
		timestamp = event.Timestamp.UTC().Format(time.RFC3339Nano)
	}
	output := telemetryOutputEvent{
		Sequence:      event.Sequence,
		Timestamp:     timestamp,
		Path:          event.Path,
		Cardinality:   event.Cardinality,
		PayloadSchema: event.PayloadSchema,
		EventType:     event.EventType,
		Encoding:      event.Encoding,
		SchemaVersion: event.SchemaVersion,
		PayloadBytes:  telemetryPayloadBytes(event),
		Payload:       payload,
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(output)
}

func telemetryPayloadBytes(event *grpcclient.TelemetryEvent) int {
	if event.PayloadBytes > 0 {
		return event.PayloadBytes
	}
	return len(event.JSONPayload)
}

func routeTextOptions(args []string) (protocol, addressFamily string, err error) {
	addressFamily = routeAddressFamilyIPv4
	if len(args) > 0 && isRouteAddressFamily(args[0]) {
		addressFamily = args[0]
		args = args[1:]
	}
	if len(args) == 0 {
		return "", addressFamily, nil
	}
	if args[0] != "protocol" {
		return "", "", fmt.Errorf("'show route' accepts '[inet|inet6] protocol <proto>' or no arguments")
	}
	if len(args) < 2 {
		return "", "", fmt.Errorf("'protocol' requires a protocol name")
	}
	if len(args) > 2 {
		return "", "", fmt.Errorf("'show route protocol' does not accept extra arguments")
	}
	protocol = args[1]
	if !validRouteProtocol(protocol, addressFamily) {
		return "", "", fmt.Errorf("invalid protocol '%s' for %s. Valid: %s", protocol, addressFamily, validRouteProtocolList(addressFamily))
	}
	return protocol, addressFamily, nil
}

func routeStateOptions(args []string) (prefix, protocol string, err error) {
	for len(args) > 0 {
		switch args[0] {
		case "prefix":
			if prefix != "" {
				return "", "", fmt.Errorf("'show routes' accepts prefix only once")
			}
			if len(args) < 2 {
				return "", "", fmt.Errorf("'show routes prefix' requires a CIDR prefix")
			}
			prefix = args[1]
			args = args[2:]
		case "protocol":
			if protocol != "" {
				return "", "", fmt.Errorf("'show routes' accepts protocol only once")
			}
			if len(args) < 2 {
				return "", "", fmt.Errorf("'show routes protocol' requires a protocol name")
			}
			protocol = args[1]
			if !validRouteStateProtocol(protocol) {
				return "", "", fmt.Errorf("invalid protocol '%s'. Valid: %s", protocol, validRouteStateProtocolList())
			}
			args = args[2:]
		default:
			return "", "", fmt.Errorf("'show routes' accepts '[prefix <cidr>] [protocol <proto>]'")
		}
	}
	return prefix, protocol, nil
}

func bfdTextOptions(args []string) (peerAddress string, brief bool, counters bool, err error) {
	if len(args) == 0 {
		return "", false, false, nil
	}
	switch args[0] {
	case "brief":
		if len(args) > 1 {
			return "", false, false, fmt.Errorf("'show bfd brief' does not accept extra arguments")
		}
		return "", true, false, nil
	case "counters":
		if len(args) > 1 {
			return "", false, false, fmt.Errorf("'show bfd counters' does not accept extra arguments")
		}
		return "", false, true, nil
	case "peer":
		if len(args) < 2 {
			return "", false, false, fmt.Errorf("'show bfd peer' requires an IP address")
		}
		if len(args) > 3 {
			return "", false, false, fmt.Errorf("'show bfd peer' accepts only an optional counters argument")
		}
		if len(args) == 3 && args[2] != "counters" {
			return "", false, false, fmt.Errorf("'show bfd peer' accepts only an optional counters argument")
		}
		return args[1], false, len(args) == 3, nil
	case "status":
		return "", false, false, fmt.Errorf("'show bfd status' is handled as structured operational state")
	default:
		return "", false, false, fmt.Errorf("'show bfd' accepts status, brief, counters, peer <ip>, or no arguments")
	}
}

func bfdStatusRequested(args []string) (bool, error) {
	if len(args) == 0 || args[0] != "status" {
		return false, nil
	}
	if len(args) > 1 {
		return false, fmt.Errorf("'show bfd status' does not accept extra arguments")
	}
	return true, nil
}

func routingInstancesNameFilter(args []string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	if len(args) > 1 {
		return "", fmt.Errorf("'show routing-instances' accepts at most one instance name")
	}
	return args[0], nil
}

func filterRoutingInstances(instances []grpcclient.RoutingInstanceInfo, name string) []grpcclient.RoutingInstanceInfo {
	if name == "" {
		return instances
	}
	filtered := make([]grpcclient.RoutingInstanceInfo, 0, 1)
	for _, instance := range instances {
		if instance.Name == name {
			filtered = append(filtered, instance)
		}
	}
	return filtered
}

const (
	routeAddressFamilyIPv4 = "inet"
	routeAddressFamilyIPv6 = "inet6"
)

var validIPv4RouteProtocols = map[string]bool{
	"bgp":       true,
	"ospf":      true,
	"static":    true,
	"connected": true,
	"kernel":    true,
}

var validIPv6RouteProtocols = map[string]bool{
	"bgp":       true,
	"ospf3":     true,
	"ospf6":     true,
	"static":    true,
	"connected": true,
	"kernel":    true,
}

func isRouteAddressFamily(value string) bool {
	return value == routeAddressFamilyIPv4 || value == routeAddressFamilyIPv6
}

func validRouteProtocol(protocol, addressFamily string) bool {
	if addressFamily == routeAddressFamilyIPv6 {
		return validIPv6RouteProtocols[protocol]
	}
	return validIPv4RouteProtocols[protocol]
}

func validRouteProtocolList(addressFamily string) string {
	if addressFamily == routeAddressFamilyIPv6 {
		return "bgp, ospf3, ospf6, static, connected, kernel"
	}
	return "bgp, ospf, static, connected, kernel"
}

func validRouteStateProtocol(protocol string) bool {
	return validIPv4RouteProtocols[protocol] || validIPv6RouteProtocols[protocol]
}

func validRouteStateProtocolList() string {
	return "bgp, ospf, ospf3, ospf6, static, connected, kernel"
}

// --- Utilities ---

func hasPipeOutsideQuotes(line string) bool {
	inQuote := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '"':
			inQuote = !inQuote
		case '|':
			if !inQuote {
				return true
			}
		}
	}
	return false
}

func tokenize(line string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := false

	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case ch == '"':
			inQuote = !inQuote
		case ch == ' ' && !inQuote:
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

func filterInput(r rune) (rune, bool) {
	if r == readline.CharCtrlZ {
		return r, false
	}
	return r, true
}

func createCompleter() *readline.PrefixCompleter {
	return readline.NewPrefixCompleter(
		readline.PcItem("help"),
		readline.PcItem("configure"),
		readline.PcItem("exit"),
		readline.PcItem("quit"),
		readline.PcItem("show",
			readline.PcItem("configuration",
				readline.PcItem("rollback"),
			),
			readline.PcItem("interfaces"),
			readline.PcItem("bgp",
				readline.PcItem("summary"),
				readline.PcItem("neighbor"),
			),
			readline.PcItem("ospf",
				readline.PcItem("neighbor"),
			),
			readline.PcItem("vrrp"),
			readline.PcItem("lcp"),
			readline.PcItem("ha"),
			readline.PcItem("class-of-service"),
			readline.PcItem("evpn"),
			readline.PcItem("telemetry",
				readline.PcItem("path"),
				readline.PcItem("interval"),
				readline.PcItem("count"),
				readline.PcItem("once"),
			),
			readline.PcItem("route",
				readline.PcItem("protocol"),
			),
			readline.PcItem("compare"),
			readline.PcItem("history"),
		),
		readline.PcItem("set",
			readline.PcItem("system",
				readline.PcItem("host-name"),
			),
			readline.PcItem("interfaces"),
			readline.PcItem("routing-options",
				readline.PcItem("autonomous-system"),
				readline.PcItem("router-id"),
				readline.PcItem("static",
					readline.PcItem("route"),
				),
			),
			readline.PcItem("protocols",
				readline.PcItem("bgp",
					readline.PcItem("group"),
				),
				readline.PcItem("ospf",
					readline.PcItem("router-id"),
					readline.PcItem("area"),
				),
			),
		),
		readline.PcItem("delete",
			readline.PcItem("system"),
			readline.PcItem("interfaces"),
			readline.PcItem("routing-options"),
			readline.PcItem("protocols"),
		),
		readline.PcItem("commit",
			readline.PcItem("check"),
			readline.PcItem("and-quit"),
			readline.PcItem("comment"),
		),
		readline.PcItem("rollback"),
		readline.PcItem("discard-changes"),
		readline.PcItem("compare"),
		readline.PcItem("edit",
			readline.PcItem("interfaces"),
			readline.PcItem("protocols"),
			readline.PcItem("routing-options"),
		),
		readline.PcItem("up"),
		readline.PcItem("top"),
	)
}
