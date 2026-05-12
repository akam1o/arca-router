// arca-routerd-v2 is the unified daemon for arca-router.
// It combines the router engine, NETCONF server, and gRPC API
// into a single process with shared state.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	nbgrpc "github.com/akam1o/arca-router/internal/northbound/grpc"
	sbfrr "github.com/akam1o/arca-router/internal/southbound/frr"
	sbvpp "github.com/akam1o/arca-router/internal/southbound/vpp"
	internalstore "github.com/akam1o/arca-router/internal/store"
	storesqlite "github.com/akam1o/arca-router/internal/store/sqlite"
	"github.com/akam1o/arca-router/pkg/config"
	"github.com/akam1o/arca-router/pkg/device"
	"github.com/akam1o/arca-router/pkg/logger"
	"github.com/akam1o/arca-router/pkg/netconf"
	pkgvpp "github.com/akam1o/arca-router/pkg/vpp"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

const (
	secureGRPCSocketDirPerms  os.FileMode = 0750
	secureGRPCSocketFilePerms os.FileMode = 0600
	secureGRPCSocketUmask                 = 0077
)

var grpcSocketUmaskMu sync.Mutex

type daemonFlags struct {
	configPath    string
	hardwarePath  string
	datastorePath string
	logLevel      string
	version       bool
	mockVPP       bool

	// NETCONF settings (merged from arca-netconfd)
	netconfListen string
	hostKeyPath   string
	userDBPath    string
	grpcSocket    string
}

func main() {
	f := parseFlags()

	if f.version {
		fmt.Printf("arca-routerd version %s\n", Version)
		fmt.Printf("  Commit: %s\n", Commit)
		fmt.Printf("  Built:  %s\n", BuildDate)
		os.Exit(0)
	}

	logLevel := parseLogLevel(f.logLevel)
	log := logger.New("arca-routerd", &logger.Config{
		Level:     logLevel,
		AddSource: true,
	})

	log.Info("Starting unified arca-routerd",
		slog.String("version", Version),
		slog.String("commit", Commit),
		slog.String("build_date", BuildDate),
	)

	ctx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer cancel()

	if err := run(ctx, f, log); err != nil {
		log.Error("Daemon failed", slog.Any("error", err))
		os.Exit(1)
	}

	log.Info("arca-routerd stopped gracefully")
}

func parseFlags() *daemonFlags {
	f := &daemonFlags{}

	flag.StringVar(&f.configPath, "config", "/etc/arca-router/arca-router.conf",
		"Path to configuration file")
	flag.StringVar(&f.hardwarePath, "hardware", "/etc/arca-router/hardware.yaml",
		"Path to hardware configuration file")
	flag.StringVar(&f.datastorePath, "datastore", "/var/lib/arca-router/config.db",
		"Path to configuration datastore (SQLite)")
	flag.StringVar(&f.logLevel, "log-level", "info",
		"Log level (debug, info, warn, error)")
	flag.BoolVar(&f.version, "version", false,
		"Print version information and exit")
	flag.BoolVar(&f.mockVPP, "mock-vpp", false,
		"Use mock VPP client for testing")

	// NETCONF flags
	flag.StringVar(&f.netconfListen, "netconf-listen", ":830",
		"NETCONF/SSH listen address")
	flag.StringVar(&f.hostKeyPath, "host-key", "/var/lib/arca-router/ssh_host_ed25519_key",
		"Path to SSH host key")
	flag.StringVar(&f.userDBPath, "user-db", "/var/lib/arca-router/users.db",
		"Path to user database")
	flag.StringVar(&f.grpcSocket, "grpc-socket", "/run/arca-router/routerd.sock",
		"Path to internal gRPC Unix socket")

	flag.Parse()
	return f
}

func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func run(ctx context.Context, f *daemonFlags, log *logger.Logger) error {
	installParserHooks()

	log.Info("Configuration",
		slog.String("config_path", f.configPath),
		slog.String("hardware_path", f.hardwarePath),
		slog.String("datastore_path", f.datastorePath),
		slog.String("netconf_listen", f.netconfListen),
		slog.String("grpc_socket", f.grpcSocket),
	)

	// --- Step 1: Load hardware configuration ---
	log.Info("Loading hardware configuration")
	hwConfig, err := device.LoadHardware(f.hardwarePath, log)
	if err != nil {
		return fmt.Errorf("load hardware config: %w", err)
	}
	log.Info("Hardware loaded", slog.Int("interfaces", len(hwConfig.Interfaces)))

	// --- Step 2: Create VPP client ---
	var vppClient pkgvpp.Client
	if f.mockVPP {
		vppClient = pkgvpp.NewMockClient()
	} else {
		vppClient = pkgvpp.NewGovppClient()
	}

	// --- Step 3: Create southbound plugins ---
	vppPlugin := sbvpp.NewVPPPlugin(vppClient, hwConfig, slog.Default())
	frrPlugin := sbfrr.NewFRRPlugin(slog.Default())

	plugins := []engine.Plugin{vppPlugin, frrPlugin}

	// --- Step 4: Create engine ---
	eng := engine.NewEngine(plugins, slog.Default())

	// --- Step 5: Initialize plugins ---
	log.Info("Initializing southbound plugins")
	for _, p := range plugins {
		if err := p.Init(ctx); err != nil {
			return fmt.Errorf("init plugin %s: %w", p.Name(), err)
		}
		defer func(p engine.Plugin) {
			if closeErr := p.Close(); closeErr != nil {
				log.Error("Failed to close plugin", slog.String("plugin", p.Name()), slog.Any("error", closeErr))
			}
		}(p)
	}

	// --- Step 6: Open config store ---
	configStore, err := storesqlite.NewFromPath(f.datastorePath)
	if err != nil {
		return fmt.Errorf("open config store: %w", err)
	}
	defer configStore.Close()
	if err := configStore.CleanupEphemeralState(ctx); err != nil {
		return fmt.Errorf("cleanup config store ephemeral state: %w", err)
	}

	// --- Step 7: Load initial configuration ---
	log.Info("Loading initial configuration")
	initialSnap, initialSource, err := loadInitialConfig(ctx, f, configStore, log)
	if err != nil {
		return fmt.Errorf("load initial config: %w", err)
	}

	// Apply initial configuration through the engine and keep the legacy
	// datastore in sync for NETCONF running/candidate operations.
	if err := applyInitialConfig(ctx, eng, configStore, initialSnap, initialSource); err != nil {
		return fmt.Errorf("apply initial config: %w", err)
	}
	log.Info("Initial configuration applied", slog.String("source", initialSource))

	// --- Step 8: Start NETCONF server ---
	var netconfServer *netconf.SSHServer
	if f.hostKeyPath != "" {
		netconfServer, err = startNETCONFServer(ctx, f, eng, log)
		if err != nil {
			return err
		}
		defer func() {
			if err := netconfServer.Stop(); err != nil {
				log.Error("Failed to stop NETCONF server", slog.Any("error", err))
			}
		}()
	}

	// --- Step 9: Start gRPC API (for CLI) ---
	log.Info("Starting gRPC API", slog.String("socket", f.grpcSocket))
	if err := prepareGRPCSocketPath(f.grpcSocket); err != nil {
		return err
	}

	lis, err := listenSecureGRPCSocket(f.grpcSocket)
	if err != nil {
		return fmt.Errorf("listen on gRPC socket: %w", err)
	}
	defer lis.Close()

	grpcServer := nbgrpc.NewServer(eng, configStore, slog.Default())
	grpcErr := make(chan error, 1)
	go func() {
		grpcErr <- grpcServer.Serve(lis)
	}()

	// --- Wait for shutdown ---
	log.Info("Daemon running, waiting for shutdown signal")
	select {
	case <-ctx.Done():
		log.Info("Shutdown signal received, stopping")
	case err := <-grpcErr:
		return fmt.Errorf("gRPC API stopped: %w", err)
	}
	grpcServer.Stop()

	return nil
}

func startNETCONFServer(ctx context.Context, f *daemonFlags, eng *engine.Engine, log *logger.Logger) (*netconf.SSHServer, error) {
	log.Info("Starting NETCONF server", slog.String("listen", f.netconfListen))
	ncConfig := netconf.DefaultSSHConfig()
	ncConfig.ListenAddr = f.netconfListen
	ncConfig.HostKeyPath = f.hostKeyPath
	ncConfig.UserDBPath = f.userDBPath
	ncConfig.DatastorePath = f.datastorePath

	server, err := netconf.NewSSHServer(ncConfig)
	if err != nil {
		return nil, fmt.Errorf("create NETCONF server: %w", err)
	}
	server.SetCommitHook(newNETCONFCommitHook(eng))
	if err := server.Start(ctx); err != nil {
		return nil, fmt.Errorf("start NETCONF server: %w", err)
	}
	return server, nil
}

func prepareGRPCSocketPath(socketPath string) error {
	socketDir := filepath.Dir(socketPath)
	if err := os.MkdirAll(socketDir, secureGRPCSocketDirPerms); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}
	if err := validateGRPCSocketDirectory(socketDir); err != nil {
		return err
	}
	if err := removeStaleGRPCSocket(socketPath); err != nil {
		return err
	}
	return nil
}

func validateGRPCSocketDirectory(socketDir string) error {
	info, err := os.Stat(socketDir)
	if err != nil {
		return fmt.Errorf("stat gRPC socket directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("gRPC socket parent path is not a directory: %s", socketDir)
	}
	if perms := info.Mode().Perm(); perms&0022 != 0 {
		return fmt.Errorf("insecure permissions on gRPC socket directory %s: mode=%04o", socketDir, perms)
	}
	return nil
}

func removeStaleGRPCSocket(socketPath string) error {
	info, err := os.Lstat(socketPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat stale gRPC socket: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to remove non-socket gRPC path: %s", socketPath)
	}
	if err := os.Remove(socketPath); err != nil {
		return fmt.Errorf("remove stale socket: %w", err)
	}
	return nil
}

func restrictGRPCSocketPermissions(socketPath string) error {
	if err := os.Chmod(socketPath, secureGRPCSocketFilePerms); err != nil {
		return fmt.Errorf("restrict gRPC socket permissions: %w", err)
	}
	return nil
}

func listenSecureGRPCSocket(socketPath string) (net.Listener, error) {
	grpcSocketUmaskMu.Lock()
	defer grpcSocketUmaskMu.Unlock()

	oldUmask := syscall.Umask(secureGRPCSocketUmask)
	defer syscall.Umask(oldUmask)

	lis, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	if err := restrictGRPCSocketPermissions(socketPath); err != nil {
		_ = lis.Close()
		return nil, err
	}
	return lis, nil
}

// loadInitialConfig loads the startup config from the datastore or file.
func loadInitialConfig(ctx context.Context, f *daemonFlags, st internalstore.ConfigStore, log *logger.Logger) (*model.ConfigSnapshot, string, error) {
	if st != nil {
		snap, err := st.GetLatestSnapshot(ctx)
		if err != nil {
			return nil, "", fmt.Errorf("load config from datastore: %w", err)
		}
		if snap != nil && snap.Config != nil {
			log.Info("Loaded initial configuration from datastore")
			return snap.Clone(), "datastore", nil
		}
	}

	file, err := os.Open(f.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Warn("Config file not found, using empty config", slog.String("path", f.configPath))
			return model.NewSnapshot(model.NewRouterConfig(), 1, "system", "initial startup"), "empty", nil
		}
		return nil, "", fmt.Errorf("open config %s: %w", f.configPath, err)
	}
	defer file.Close()

	legacyCfg, err := parseLegacyConfig(file)
	if err != nil {
		return nil, "", fmt.Errorf("parse config %s: %w", f.configPath, err)
	}

	// Validate
	if err := legacyCfg.Validate(); err != nil {
		return nil, "", fmt.Errorf("validate config: %w", err)
	}

	// Convert to new model
	return model.NewSnapshot(model.FromLegacyConfig(legacyCfg), 1, "system", "initial startup"), "file", nil
}

func applyInitialConfig(ctx context.Context, eng *engine.Engine, st internalstore.ConfigStore, snap *model.ConfigSnapshot, source string) error {
	if snap == nil || snap.Config == nil {
		return fmt.Errorf("initial configuration is nil")
	}

	var prepared internalstore.PreparedCommit
	if source != "datastore" && st != nil {
		var err error
		prepared, err = st.PrepareCommit(ctx, snap)
		if err != nil {
			return fmt.Errorf("prepare initial config persistence: %w", err)
		}
	}

	beforeSnap := eng.RunningSnapshot()
	if err := eng.Apply(ctx, snap.Config, "system", "initial startup"); err != nil {
		if prepared != nil {
			_ = prepared.Abort(context.Background())
		}
		return err
	}

	if source == "datastore" {
		eng.InitializeRunning(snap.Config, initialSnapshotVersion(snap))
		return nil
	}

	if prepared != nil {
		if _, err := prepared.Commit(ctx); err != nil {
			_ = prepared.Abort(context.Background())
			if rollbackErr := rollbackEngineToSnapshot(context.Background(), eng, beforeSnap, "system", "rollback failed initial config persistence"); rollbackErr != nil {
				return fmt.Errorf("persist initial config after apply: %w (rollback failed: %v)", err, rollbackErr)
			}
			return fmt.Errorf("persist initial config after apply: %w", err)
		}
	}
	if eng.RunningSnapshot() == nil {
		eng.InitializeRunning(snap.Config, initialSnapshotVersion(snap))
	}
	return nil
}

func initialSnapshotVersion(snap *model.ConfigSnapshot) uint64 {
	if snap != nil && snap.Version > 0 {
		return snap.Version
	}
	return 1
}

func parseLegacyConfig(r io.Reader) (*config.Config, error) {
	parser := config.NewParser(r)
	return parser.Parse()
}

func installParserHooks() {
	parse := func(text string) (*model.RouterConfig, error) {
		legacyCfg, err := parseLegacyConfig(strings.NewReader(text))
		if err != nil {
			return nil, err
		}
		return model.FromLegacyConfig(legacyCfg), nil
	}
	nbgrpc.ConfigTextParser = parse
	storesqlite.LegacyTextParser = parse
}

func newNETCONFCommitHook(eng *engine.Engine) netconf.CommitHook {
	return func(ctx context.Context, req *netconf.CommitHookRequest, persist func(context.Context) (string, error)) (string, error) {
		if req == nil {
			return "", fmt.Errorf("commit request is nil")
		}
		legacyCfg, err := parseLegacyConfig(strings.NewReader(req.ConfigText))
		if err != nil {
			return "", fmt.Errorf("parse candidate config: %w", err)
		}
		if err := legacyCfg.Validate(); err != nil {
			return "", fmt.Errorf("validate candidate config: %w", err)
		}
		newCfg := model.FromLegacyConfig(legacyCfg)
		if err := eng.Validate(ctx, newCfg); err != nil {
			return "", err
		}

		beforeSnap := eng.RunningSnapshot()
		if !engine.ComputeDiff(snapshotConfig(beforeSnap), newCfg).HasChanges() {
			return "", fmt.Errorf("no configuration changes to commit")
		}
		if err := eng.Apply(ctx, newCfg, req.User, req.Message); err != nil {
			return "", err
		}

		commitID, err := persist(ctx)
		if err != nil {
			if rollbackErr := rollbackEngineToSnapshot(context.Background(), eng, beforeSnap, req.User, "rollback failed NETCONF commit persistence"); rollbackErr != nil {
				return "", fmt.Errorf("persist NETCONF commit after apply: %w (rollback failed: %v)", err, rollbackErr)
			}
			return "", fmt.Errorf("persist NETCONF commit after apply: %w", err)
		}
		return commitID, nil
	}
}

func snapshotConfig(snap *model.ConfigSnapshot) *model.RouterConfig {
	if snap == nil {
		return nil
	}
	return snap.Config
}

func rollbackEngineToSnapshot(ctx context.Context, eng *engine.Engine, snap *model.ConfigSnapshot, user, message string) error {
	cfg := model.NewRouterConfig()
	if snap != nil && snap.Config != nil {
		cfg = snap.Config
	}
	return eng.Apply(ctx, cfg, user, message)
}
