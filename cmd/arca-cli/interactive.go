package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/akam1o/arca-router/pkg/cli"
	"github.com/akam1o/arca-router/pkg/datastore"
	"github.com/chzyer/readline"
)

// InteractiveShell manages the interactive CLI session
type InteractiveShell struct {
	session  *cli.Session
	rl       *readline.Instance
	hostname string
}

// NewInteractiveShell creates a new interactive shell
func NewInteractiveShell(username string, ds datastore.Datastore, hostname string) (*InteractiveShell, error) {
	session := cli.NewSession(username, ds)

	// Create readline instance with tab completion
	completer := createCompleter()
	rl, err := readline.NewEx(&readline.Config{
		Prompt:              buildPrompt(hostname, session),
		HistoryFile:         "/tmp/.arca-cli-history",
		AutoComplete:        completer,
		InterruptPrompt:     "^C",
		EOFPrompt:           "exit",
		HistorySearchFold:   true,
		FuncFilterInputRune: filterInput,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize readline: %w", err)
	}

	return &InteractiveShell{
		session:  session,
		rl:       rl,
		hostname: hostname,
	}, nil
}

// Run starts the interactive shell
func (sh *InteractiveShell) Run(ctx context.Context) error {
	defer sh.rl.Close()
	defer sh.session.Close(ctx)

	// Handle Ctrl+C gracefully
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nInterrupted. Use 'exit' or 'quit' to leave the shell.")
	}()

	fmt.Println("Welcome to arca-router interactive CLI")
	fmt.Println("Type 'help' for available commands, 'exit' or 'quit' to exit")
	fmt.Println()

	for {
		// Update prompt based on current mode
		sh.rl.SetPrompt(buildPrompt(sh.hostname, sh.session))

		line, err := sh.rl.Readline()
		if err != nil { // io.EOF, readline.ErrInterrupt
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

	return nil
}

// processCommand processes a single command
func (sh *InteractiveShell) processCommand(ctx context.Context, line string) error {
	// Handle pipe commands (e.g., "show | compare")
	// Check if pipe is outside quotes
	if sh.hasPipeOutsideQuotes(line) {
		return sh.processPipeCommand(ctx, line)
	}

	// Use TokenizeCommand to handle quoted strings properly
	parts, err := cli.TokenizeCommand(line)
	if err != nil {
		return fmt.Errorf("syntax error: %w", err)
	}
	if len(parts) == 0 {
		return nil
	}

	command := parts[0]
	args := parts[1:]

	switch command {
	case "help", "?":
		sh.showHelp()
		return nil

	case "exit", "quit":
		if sh.session.Mode() == cli.ModeConfiguration {
			fmt.Println("Warning: Exiting configuration mode. Changes not committed will be lost.")
			fmt.Print("Exit anyway? [yes/no]: ")
			reader := bufio.NewReader(os.Stdin)
			response, _ := reader.ReadString('\n')
			response = strings.TrimSpace(strings.ToLower(response))
			if response != "yes" && response != "y" {
				return nil
			}
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

	case "edit":
		return sh.cmdEdit(args)

	case "up":
		return sh.cmdUp()

	case "top":
		return sh.cmdTop()

	case "discard-changes":
		return sh.cmdDiscardChanges(ctx)

	default:
		return fmt.Errorf("unknown command: %s. Type 'help' for available commands", command)
	}
}

// Command handlers

func (sh *InteractiveShell) cmdConfigure(ctx context.Context) error {
	if sh.session.Mode() == cli.ModeConfiguration {
		fmt.Println("Already in configuration mode")
		return nil
	}

	err := sh.session.EnterConfigurationMode(ctx)
	if err != nil {
		return err
	}

	fmt.Println("Entering configuration mode")
	return nil
}

func (sh *InteractiveShell) cmdShow(ctx context.Context, args []string) error {
	if len(args) == 0 {
		// 'show' without args shows current config in config mode, running config in operational mode
		output, err := sh.session.ShowCommand(ctx, args)
		if err != nil {
			return err
		}
		fmt.Println(output)
		return nil
	}

	subcommand := args[0]
	switch subcommand {
	case "configuration":
		output, err := sh.session.ShowCommand(ctx, args[1:])
		if err != nil {
			return err
		}
		fmt.Println(output)
		return nil

	case "compare":
		return sh.cmdCompare(ctx)

	case "history":
		// show history [N] - show last N commits (default 10)
		limit := 10
		if len(args) > 1 {
			_, err := fmt.Sscanf(args[1], "%d", &limit)
			if err != nil {
				return fmt.Errorf("invalid limit: %s", args[1])
			}
		}
		return sh.session.ShowCommitHistory(ctx, limit)

	case "interfaces":
		// 'show interfaces' or 'show interfaces <name>'
		if sh.session.Mode() == cli.ModeConfiguration {
			return fmt.Errorf("'show interfaces' not available in configuration mode")
		}
		// Create default flags for VPP/FRR access
		f := &flags{
			vppSocket:  "/run/vpp/api.sock",
			configPath: "/etc/arca-router/arca-router.conf",
			debug:      false,
		}
		exitCode := cmdShowInterfaces(ctx, args[1:], f)
		if exitCode != ExitSuccess {
			return fmt.Errorf("command failed with exit code %d", exitCode)
		}
		return nil

	case "bgp":
		if sh.session.Mode() == cli.ModeConfiguration {
			return fmt.Errorf("'show bgp' not available in configuration mode")
		}
		if len(args) < 2 {
			return fmt.Errorf("'show bgp' requires a subcommand (summary or neighbor)")
		}
		f := &flags{
			vppSocket:  "/run/vpp/api.sock",
			configPath: "/etc/arca-router/arca-router.conf",
			debug:      false,
		}
		bgpSubcmd := args[1]
		switch bgpSubcmd {
		case "summary":
			if len(args) > 2 {
				return fmt.Errorf("'show bgp summary' does not accept extra arguments")
			}
			exitCode := cmdShowBGPSummary(ctx, f)
			if exitCode != ExitSuccess {
				return fmt.Errorf("command failed with exit code %d", exitCode)
			}
			return nil
		case "neighbor":
			if len(args) < 3 {
				return fmt.Errorf("'show bgp neighbor' requires an IP address")
			}
			if len(args) > 3 {
				return fmt.Errorf("'show bgp neighbor' accepts only one IP address")
			}
			exitCode := cmdShowBGPNeighbor(ctx, args[2], f)
			if exitCode != ExitSuccess {
				return fmt.Errorf("command failed with exit code %d", exitCode)
			}
			return nil
		default:
			return fmt.Errorf("unknown bgp subcommand '%s' (valid: summary, neighbor)", bgpSubcmd)
		}

	case "ospf":
		if sh.session.Mode() == cli.ModeConfiguration {
			return fmt.Errorf("'show ospf' not available in configuration mode")
		}
		if len(args) < 2 || args[1] != "neighbor" {
			return fmt.Errorf("'show ospf' requires 'neighbor' subcommand")
		}
		if len(args) > 2 {
			return fmt.Errorf("'show ospf neighbor' does not accept extra arguments")
		}
		f := &flags{
			vppSocket:  "/run/vpp/api.sock",
			configPath: "/etc/arca-router/arca-router.conf",
			debug:      false,
		}
		exitCode := cmdShowOSPFNeighbor(ctx, f)
		if exitCode != ExitSuccess {
			return fmt.Errorf("command failed with exit code %d", exitCode)
		}
		return nil

	case "route":
		// 'show route' or 'show route protocol <proto>'
		if sh.session.Mode() == cli.ModeConfiguration {
			return fmt.Errorf("'show route' not available in configuration mode")
		}
		f := &flags{
			vppSocket:  "/run/vpp/api.sock",
			configPath: "/etc/arca-router/arca-router.conf",
			debug:      false,
		}
		exitCode := cmdShowRoute(ctx, args[1:], f)
		if exitCode != ExitSuccess {
			return fmt.Errorf("command failed with exit code %d", exitCode)
		}
		return nil

	default:
		// Unknown show subcommand
		if sh.session.Mode() == cli.ModeConfiguration {
			return fmt.Errorf("unknown show subcommand '%s'. Use 'show configuration' or 'show | compare'", subcommand)
		}
		return fmt.Errorf("unknown show subcommand '%s'", subcommand)
	}
}

func (sh *InteractiveShell) cmdSet(ctx context.Context, args []string) error {
	if sh.session.Mode() != cli.ModeConfiguration {
		return fmt.Errorf("'set' command only available in configuration mode")
	}

	err := sh.session.SetCommandWithPath(ctx, args)
	if err != nil {
		return err
	}

	fmt.Println("[edit]")
	return nil
}

func (sh *InteractiveShell) cmdDelete(ctx context.Context, args []string) error {
	if sh.session.Mode() != cli.ModeConfiguration {
		return fmt.Errorf("'delete' command only available in configuration mode")
	}

	err := sh.session.DeleteCommandWithPath(ctx, args)
	if err != nil {
		return err
	}

	fmt.Println("[edit]")
	return nil
}

// hasPipeOutsideQuotes checks if line contains pipe outside quotes
func (sh *InteractiveShell) hasPipeOutsideQuotes(line string) bool {
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

// processPipeCommand handles commands with pipes (e.g., "show | compare")
func (sh *InteractiveShell) processPipeCommand(ctx context.Context, line string) error {
	parts := strings.Split(line, "|")
	if len(parts) != 2 {
		return fmt.Errorf("invalid pipe syntax: %s", line)
	}

	leftCmd := strings.TrimSpace(parts[0])
	rightCmd := strings.TrimSpace(parts[1])

	// Currently only support "show | compare"
	if leftCmd == "show" && rightCmd == "compare" {
		return sh.cmdCompare(ctx)
	}

	return fmt.Errorf("unsupported pipe command: %s | %s", leftCmd, rightCmd)
}

func (sh *InteractiveShell) cmdCommit(ctx context.Context, args []string) error {
	if sh.session.Mode() != cli.ModeConfiguration {
		return fmt.Errorf("'commit' command only available in configuration mode")
	}

	opts := cli.CommitOptions{}

	// Parse commit options
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "check":
			opts.Check = true
		case "and-quit":
			opts.AndQuit = true
		case "comment":
			if i+1 < len(args) {
				opts.Message = args[i+1]
				i++ // Skip next arg
			} else {
				return fmt.Errorf("'comment' requires an argument")
			}
		default:
			return fmt.Errorf("unknown commit option: %s", args[i])
		}
	}

	// Validate option combinations
	if opts.Check && opts.AndQuit {
		return fmt.Errorf("'check' and 'and-quit' cannot be used together")
	}
	if opts.Check && opts.Message != "" {
		return fmt.Errorf("'check' and 'comment' cannot be used together")
	}

	// Execute commit
	err := sh.session.CommitWithOptions(ctx, opts)
	if err != nil {
		return err
	}

	// Exit configuration mode if and-quit
	if opts.AndQuit {
		return sh.session.ExitConfigurationMode(ctx)
	}

	return nil
}

func (sh *InteractiveShell) cmdRollback(ctx context.Context, args []string) error {
	if sh.session.Mode() != cli.ModeConfiguration {
		return fmt.Errorf("'rollback' command only available in configuration mode")
	}

	rollbackNum := 0
	if len(args) > 0 {
		_, err := fmt.Sscanf(args[0], "%d", &rollbackNum)
		if err != nil {
			return fmt.Errorf("invalid rollback number: %s", args[0])
		}
	}

	return sh.session.RollbackWithNumber(ctx, rollbackNum)
}

func (sh *InteractiveShell) cmdDiscardChanges(ctx context.Context) error {
	if sh.session.Mode() != cli.ModeConfiguration {
		return fmt.Errorf("'discard-changes' command only available in configuration mode")
	}

	return sh.session.DiscardChangesWithMessage(ctx)
}

func (sh *InteractiveShell) cmdCompare(ctx context.Context) error {
	if sh.session.Mode() != cli.ModeConfiguration {
		return fmt.Errorf("'compare' command only available in configuration mode")
	}

	diff, err := sh.session.CompareCommand(ctx)
	if err != nil {
		return err
	}

	if diff == "" || diff == "No changes\n" {
		fmt.Println("No changes")
	} else {
		fmt.Println(diff)
	}
	return nil
}

func (sh *InteractiveShell) cmdEdit(args []string) error {
	if sh.session.Mode() != cli.ModeConfiguration {
		return fmt.Errorf("'edit' command only available in configuration mode")
	}

	if len(args) == 0 {
		return fmt.Errorf("'edit' requires a configuration path")
	}

	sh.session.EditHierarchy(args)
	fmt.Printf("Entering edit mode at [edit %s]\n", strings.Join(args, " "))
	return nil
}

func (sh *InteractiveShell) cmdUp() error {
	if sh.session.Mode() != cli.ModeConfiguration {
		return fmt.Errorf("'up' command only available in configuration mode")
	}

	sh.session.UpHierarchy()
	path := sh.session.ConfigPath()
	if len(path) == 0 {
		fmt.Println("At top level")
	} else {
		fmt.Printf("Now at [edit %s]\n", strings.Join(path, " "))
	}
	return nil
}

func (sh *InteractiveShell) cmdTop() error {
	if sh.session.Mode() != cli.ModeConfiguration {
		return fmt.Errorf("'top' command only available in configuration mode")
	}

	sh.session.TopHierarchy()
	fmt.Println("At top level")
	return nil
}

func (sh *InteractiveShell) showHelp() {
	mode := sh.session.Mode()

	fmt.Println("Available commands:")
	fmt.Println()

	if mode == cli.ModeOperational {
		fmt.Println("Operational mode commands:")
		fmt.Println("  help                          Show this help message")
		fmt.Println("  configure                     Enter configuration mode")
		fmt.Println("  show configuration            Show running configuration")
		fmt.Println("  show interfaces [<name>]      Show interface status")
		fmt.Println("  show bgp summary              Show BGP summary")
		fmt.Println("  show bgp neighbor <ip>        Show BGP neighbor details")
		fmt.Println("  show ospf neighbor            Show OSPF neighbors")
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
		fmt.Println("  commit check              Validate configuration without committing")
		fmt.Println("  commit and-quit           Commit and exit configuration mode")
		fmt.Println("  commit comment <msg>      Commit with custom message")
		fmt.Println("  rollback <N>              Roll back N commits (0 = discard changes)")
		fmt.Println("  discard-changes           Discard all candidate changes (same as rollback 0)")
		fmt.Println("  show history [N]          Show last N commits (default: 10)")
		fmt.Println("  edit <path>               Navigate to configuration hierarchy")
		fmt.Println("  up                        Move up one level in hierarchy")
		fmt.Println("  top                       Move to top level of hierarchy")
		fmt.Println("  exit, quit                Exit configuration mode (prompts if uncommitted)")
		fmt.Println()

		path := sh.session.ConfigPath()
		if len(path) > 0 {
			fmt.Printf("Current edit path: [edit %s]\n", strings.Join(path, " "))
		}
	}
	fmt.Println()
}

// Prompt builder
func buildPrompt(hostname string, session *cli.Session) string {
	if hostname == "" {
		hostname = "arca-router"
	}

	mode := session.Mode()
	path := session.ConfigPath()

	var prompt string
	if mode == cli.ModeOperational {
		prompt = fmt.Sprintf("%s> ", hostname)
	} else {
		if len(path) > 0 {
			prompt = fmt.Sprintf("%s# [edit %s] ", hostname, strings.Join(path, " "))
		} else {
			prompt = fmt.Sprintf("%s# ", hostname)
		}
	}

	return prompt
}

// Tab completion
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
		),
		readline.PcItem("rollback"),
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

// Filter input runes (allow standard characters)
func filterInput(r rune) (rune, bool) {
	switch r {
	case readline.CharCtrlZ: // Disable Ctrl+Z
		return r, false
	}
	return r, true
}

// cmdInteractive starts the interactive shell
func cmdInteractive(ctx context.Context, f *flags) int {
	// Initialize datastore
	dsCfg := &datastore.Config{
		Backend:    datastore.BackendSQLite,
		SQLitePath: "/var/lib/arca-router/config.db",
	}
	ds, err := datastore.NewSQLiteDatastore(dsCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to initialize datastore: %v\n", err)
		return ExitOperationError
	}
	defer ds.Close()

	// Get hostname (use default if not set)
	hostname := "arca-router"
	// TODO: Read hostname from running config

	// Get username (from environment or default)
	username := os.Getenv("USER")
	if username == "" {
		username = "admin"
	}

	// Create and run interactive shell
	shell, err := NewInteractiveShell(username, ds, hostname)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to initialize interactive shell: %v\n", err)
		return ExitOperationError
	}

	err = shell.Run(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return ExitOperationError
	}

	return ExitSuccess
}
