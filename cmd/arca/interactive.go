package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"

	grpcclient "github.com/akam1o/arca-router/internal/northbound/grpc"
	configcli "github.com/akam1o/arca-router/pkg/cli"
	"github.com/chzyer/readline"
)

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
	ReplaceCandidate(context.Context, string, string) error
	Commit(context.Context, string, string, string) (string, uint64, error)
	ValidateCandidate(context.Context, string) error
	Discard(context.Context, string) error
	Rollback(context.Context, string, string, string, string) (string, uint64, error)
	Diff(context.Context, string) (string, bool, error)
	AcquireLock(context.Context, string, string) error
	ReleaseLock(context.Context, string) error
}

type showClient interface {
	GetRunning(context.Context) (string, uint64, error)
	ListHistory(context.Context, int, int) ([]grpcclient.CommitInfo, error)
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
	client, err := dialGRPC(f)
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

	parts, err := configcli.TokenizeCommand(line)
	if err != nil {
		return fmt.Errorf("parse command: %w", err)
	}
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
	case "check":
		return sh.cmdCheck(ctx, args)
	case "set":
		return sh.cmdSet(ctx, args)
	case "delete":
		return sh.cmdDelete(ctx, args)
	case "commit":
		return sh.cmdCommit(ctx, args)
	case "rollback":
		return sh.cmdRollback(ctx, args)
	case "backup":
		return sh.cmdBackup(ctx, args)
	case "restore":
		return sh.cmdRestore(ctx, args)
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

func (sh *interactiveShell) cmdCheck(ctx context.Context, args []string) error {
	options, err := parseUpgradePreflightOptions(args)
	if err != nil {
		return err
	}
	if sh.mode != modeOperational {
		return fmt.Errorf("'check upgrade' only available in operational mode")
	}
	lines, err := upgradePreflightLinesWithOptions(ctx, sh.client, options)
	if err != nil {
		return err
	}
	for _, line := range lines {
		fmt.Println(line)
	}
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

	case "compatibility":
		if len(args) > 1 {
			return fmt.Errorf("'show compatibility' does not accept extra arguments")
		}
		printCompatibilityPolicy()
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
	text, err := sh.archivedConfiguration(ctx, rollbackNum)
	if err != nil {
		return err
	}
	fmt.Println(text)
	return nil
}

func (sh *interactiveShell) archivedConfiguration(ctx context.Context, rollbackNum int) (string, error) {
	return archivedConfigurationText(ctx, sh.client, rollbackNum)
}

func archivedConfigurationText(ctx context.Context, client showClient, rollbackNum int) (string, error) {
	history, err := client.ListHistory(ctx, rollbackNum+1, 0)
	if err != nil {
		return "", fmt.Errorf("failed to load commit history: %w", err)
	}
	if len(history) <= rollbackNum {
		if rollbackNum == 0 {
			text, _, err := client.GetRunning(ctx)
			if err != nil {
				return "", err
			}
			return text, nil
		}
		availableCommits := len(history) - 1
		if availableCommits < 0 {
			availableCommits = 0
		}
		return "", fmt.Errorf("not enough history for rollback %d (only %d commits available)", rollbackNum, availableCommits)
	}

	entry := history[rollbackNum]
	if entry.ConfigText == "" {
		if rollbackNum == 0 {
			text, _, err := client.GetRunning(ctx)
			if err != nil {
				return "", err
			}
			return text, nil
		}
		return "", fmt.Errorf("archived config text unavailable for rollback %d", rollbackNum)
	}
	return entry.ConfigText, nil
}
