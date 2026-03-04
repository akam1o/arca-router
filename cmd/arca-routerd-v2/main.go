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
	"strings"
	"syscall"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	sbfrr "github.com/akam1o/arca-router/internal/southbound/frr"
	sbvpp "github.com/akam1o/arca-router/internal/southbound/vpp"
	"github.com/akam1o/arca-router/internal/store/sqlite"
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

type daemonFlags struct {
	configPath    string
	hardwarePath  string
	datastorePath string
	logLevel      string
	version       bool
	mockVPP       bool

	// NETCONF settings (merged from arca-netconfd)
	netconfListen  string
	hostKeyPath    string
	userDBPath     string
	grpcSocket     string
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
	flag.StringVar(&f.grpcSocket, "grpc-socket", "/var/run/arca-router/api.sock",
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

	// --- Step 6: Load initial configuration ---
	log.Info("Loading initial configuration")
	initialCfg, err := loadInitialConfig(f, log)
	if err != nil {
		return fmt.Errorf("load initial config: %w", err)
	}

	// Apply initial configuration through the engine
	if err := eng.Apply(ctx, initialCfg, "system", "initial startup"); err != nil {
		return fmt.Errorf("apply initial config: %w", err)
	}
	log.Info("Initial configuration applied")

	// --- Step 7: Open config store ---
	store, err := sqlite.NewFromPath(f.datastorePath)
	if err != nil {
		log.Warn("Failed to open config store (continuing without persistence)", slog.Any("error", err))
	} else {
		defer store.Close()
	}

	// --- Step 8: Start NETCONF server ---
	if f.hostKeyPath != "" {
		go func() {
			log.Info("Starting NETCONF server", slog.String("listen", f.netconfListen))
			ncConfig := netconf.DefaultSSHConfig()
			ncConfig.ListenAddr = f.netconfListen
			ncConfig.HostKeyPath = f.hostKeyPath
			ncConfig.UserDBPath = f.userDBPath
			ncConfig.DatastorePath = f.datastorePath

			server, err := netconf.NewSSHServer(ncConfig)
			if err != nil {
				log.Error("Failed to create NETCONF server", slog.Any("error", err))
				return
			}
			if err := server.Start(ctx); err != nil {
				log.Error("NETCONF server failed", slog.Any("error", err))
			}
		}()
	}

	// --- Step 9: Start gRPC API (for CLI) ---
	go func() {
		log.Info("Starting gRPC API", slog.String("socket", f.grpcSocket))
		// Ensure socket directory exists
		socketDir := f.grpcSocket[:strings.LastIndex(f.grpcSocket, "/")]
		if err := os.MkdirAll(socketDir, 0750); err != nil {
			log.Error("Failed to create socket directory", slog.Any("error", err))
			return
		}
		// Remove stale socket
		os.Remove(f.grpcSocket)

		lis, err := net.Listen("unix", f.grpcSocket)
		if err != nil {
			log.Error("Failed to listen on gRPC socket", slog.Any("error", err))
			return
		}
		defer lis.Close()

		_ = eng  // gRPC server would use eng for config operations
		_ = store // and store for persistence

		// For now, just keep the listener alive until shutdown
		<-ctx.Done()
	}()

	// --- Wait for shutdown ---
	log.Info("Daemon running, waiting for shutdown signal")
	<-ctx.Done()
	log.Info("Shutdown signal received, stopping")

	return nil
}

// loadInitialConfig loads the startup config from file and converts to new model.
func loadInitialConfig(f *daemonFlags, log *logger.Logger) (*model.RouterConfig, error) {
	file, err := os.Open(f.configPath)
	if err != nil {
		log.Warn("Config file not found, using empty config", slog.String("path", f.configPath))
		return model.NewRouterConfig(), nil
	}
	defer file.Close()

	legacyCfg, err := parseLegacyConfig(file)
	if err != nil {
		return nil, fmt.Errorf("parse config %s: %w", f.configPath, err)
	}

	// Validate
	if err := legacyCfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	// Convert to new model
	return model.FromLegacyConfig(legacyCfg), nil
}

func parseLegacyConfig(r io.Reader) (*config.Config, error) {
	parser := config.NewParser(r)
	return parser.Parse()
}
