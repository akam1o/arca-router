// arca is the CLI that communicates with arca-routerd
// via gRPC over a Unix domain socket. It is a thin client that delegates
// all state, validation, and config management to the daemon.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"

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
)

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
  bgp summary                 Show BGP summary
  bgp neighbor <ip>           Show BGP neighbor details
  ospf neighbor               Show OSPF neighbors
  route                       Show routing table
  route protocol <proto>      Show routes by protocol

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

	case "bgp":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Error: 'show bgp' requires a subcommand (summary or neighbor)\n")
			return ExitUsageError
		}
		switch args[1] {
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

	case "ospf":
		if len(args) < 2 || args[1] != "neighbor" {
			fmt.Fprintf(os.Stderr, "Error: 'show ospf' requires 'neighbor' subcommand\n")
			return ExitUsageError
		}
		output, err := client.GetOSPFNeighborsText(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return ExitOperationError
		}
		printCommandOutput(output)
		return ExitSuccess

	case "vrrp":
		output, err := client.GetVRRPText(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return ExitOperationError
		}
		printCommandOutput(output)
		return ExitSuccess

	case "route":
		protoFilter, err := routeProtocolFilter(args[1:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return ExitUsageError
		}
		output, err := client.GetRouteText(ctx, protoFilter)
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
	GetRoutes(context.Context, string, string) ([]grpcclient.RouteInfo, error)
	GetBGPNeighbors(context.Context) ([]grpcclient.BGPNeighborInfo, error)
}

type showClient interface {
	GetRunning(context.Context) (string, uint64, error)
	GetInterfaces(context.Context, string) ([]grpcclient.InterfaceInfo, error)
	GetRouteText(context.Context, string) (string, error)
	GetBGPSummaryText(context.Context) (string, error)
	GetBGPNeighborText(context.Context, string) (string, error)
	GetOSPFNeighborsText(context.Context) (string, error)
	GetVRRPText(context.Context) (string, error)
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

	case "bgp":
		if sh.mode == modeConfiguration {
			return fmt.Errorf("'show bgp' not available in configuration mode")
		}
		if len(args) < 2 {
			return fmt.Errorf("'show bgp' requires a subcommand (summary or neighbor)")
		}
		switch args[1] {
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

	case "ospf":
		if sh.mode == modeConfiguration {
			return fmt.Errorf("'show ospf' not available in configuration mode")
		}
		if len(args) < 2 || args[1] != "neighbor" {
			return fmt.Errorf("'show ospf' requires 'neighbor' subcommand")
		}
		output, err := sh.client.GetOSPFNeighborsText(ctx)
		if err != nil {
			return err
		}
		printCommandOutput(output)
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

	case "route":
		if sh.mode == modeConfiguration {
			return fmt.Errorf("'show route' not available in configuration mode")
		}
		protoFilter, err := routeProtocolFilter(args[1:])
		if err != nil {
			return err
		}
		output, err := sh.client.GetRouteText(ctx, protoFilter)
		if err != nil {
			return err
		}
		printCommandOutput(output)
		return nil

	default:
		return fmt.Errorf("unknown show subcommand '%s'", subcmd)
	}
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
		return nil
	}

	user := currentUsername()

	commitID, version, err := sh.client.Commit(ctx, sh.sessionID, user, message)
	if err != nil {
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
		fmt.Println("  show interfaces [<name>]      Show interface status")
		fmt.Println("  show bgp summary              Show BGP summary")
		fmt.Println("  show bgp neighbor <ip>        Show BGP neighbor details")
		fmt.Println("  show ospf neighbor            Show OSPF neighbors")
		fmt.Println("  show vrrp                     Show VRRP status")
		fmt.Println("  show route                    Show routing table")
		fmt.Println("  show route protocol <proto>   Show routes by protocol")
		fmt.Println("  exit, quit                    Exit interactive CLI")
	} else {
		fmt.Println("Configuration mode commands:")
		fmt.Println("  help                      Show this help message")
		fmt.Println("  set <config>              Add or modify configuration")
		fmt.Println("  delete <config>           Delete configuration")
		fmt.Println("  show                      Show candidate configuration")
		fmt.Println("  show | compare            Show differences from running config")
		fmt.Println("  commit                    Commit candidate configuration")
		fmt.Println("  commit check              Validate without committing")
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
	fmt.Printf("%-20s %-8s %-8s %-6s %-18s %-10s %-12s %-12s %-16s %s\n",
		"Interface", "Admin", "Oper", "MTU", "MAC", "Speed", "RX-Packets", "TX-Packets", "QoS", "Queues")
	fmt.Println(strings.Repeat("-", 143))
	for _, iface := range ifaces {
		fmt.Printf("%-20s %-8s %-8s %-6d %-18s %-10d %-12d %-12d %-16s %s\n",
			iface.Name, iface.AdminStatus, iface.OperStatus,
			iface.MTU, iface.MAC, iface.Speed, iface.RxPackets, iface.TxPackets, interfaceQoSProfile(iface), interfaceQueueSummary(iface))
	}
}

func interfaceQoSProfile(iface grpcclient.InterfaceInfo) string {
	if iface.QoSProfile == "" {
		return "-"
	}
	return iface.QoSProfile
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

func printCommandOutput(output string) {
	fmt.Print(output)
	if output != "" && !strings.HasSuffix(output, "\n") {
		fmt.Println()
	}
}

func routeProtocolFilter(args []string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	if args[0] != "protocol" {
		return "", fmt.Errorf("'show route' accepts 'protocol <proto>' or no arguments")
	}
	if len(args) < 2 {
		return "", fmt.Errorf("'protocol' requires a protocol name")
	}
	if len(args) > 2 {
		return "", fmt.Errorf("'show route protocol' does not accept extra arguments")
	}
	protocol := args[1]
	if !validRouteProtocols[protocol] {
		return "", fmt.Errorf("invalid protocol '%s'. Valid: bgp, ospf, static, connected, kernel", protocol)
	}
	return protocol, nil
}

var validRouteProtocols = map[string]bool{
	"bgp":       true,
	"ospf":      true,
	"static":    true,
	"connected": true,
	"kernel":    true,
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
			readline.PcItem("configuration"),
			readline.PcItem("interfaces"),
			readline.PcItem("bgp",
				readline.PcItem("summary"),
				readline.PcItem("neighbor"),
			),
			readline.PcItem("ospf",
				readline.PcItem("neighbor"),
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
