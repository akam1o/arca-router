package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/akam1o/arca-router/pkg/logger"
)

var (
	// Version information (set by ldflags during build)
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

type flags struct {
	configPath   string
	hardwarePath string
	logLevel     string
	version      bool
	mockVPP      bool // Use mock VPP client for testing
}

func main() {
	// Parse command line flags
	f := parseFlags()

	// Handle version flag
	if f.version {
		printVersion()
		os.Exit(0)
	}

	// Setup logger
	logLevel := parseLogLevel(f.logLevel)
	log := logger.New("main", &logger.Config{
		Level:     logLevel,
		AddSource: true,
	})

	log.Info("Starting arca-routerd",
		slog.String("version", Version),
		slog.String("commit", Commit),
		slog.String("build_date", BuildDate),
	)

	// Setup signal handling for graceful shutdown
	// Note: os.Interrupt is equivalent to syscall.SIGINT on Unix systems
	ctx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt, // SIGINT
		syscall.SIGTERM,
	)
	defer cancel()

	// Run the daemon
	if err := run(ctx, f, log); err != nil {
		log.Error("Daemon failed", slog.Any("error", err))
		os.Exit(1)
	}

	log.Info("arca-routerd stopped gracefully")
}

func parseFlags() *flags {
	f := &flags{}

	flag.StringVar(&f.configPath, "config", "/etc/arca-router/arca.conf",
		"Path to configuration file")
	flag.StringVar(&f.hardwarePath, "hardware", "/etc/arca-router/hardware.yaml",
		"Path to hardware configuration file")
	flag.StringVar(&f.logLevel, "log-level", "info",
		"Log level (debug, info, warn, error)")
	flag.BoolVar(&f.version, "version", false,
		"Print version information and exit")
	flag.BoolVar(&f.mockVPP, "mock-vpp", true,
		"Use mock VPP client (default: true for Phase 1)")

	flag.Parse()

	return f
}

func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		fmt.Fprintf(os.Stderr, "Invalid log level: %s, using info\n", level)
		return slog.LevelInfo
	}
}

func printVersion() {
	fmt.Printf("arca-routerd version %s\n", Version)
	fmt.Printf("  Commit: %s\n", Commit)
	fmt.Printf("  Built:  %s\n", BuildDate)
}

func run(ctx context.Context, f *flags, log *logger.Logger) error {
	log.Info("Configuration",
		slog.String("config_path", f.configPath),
		slog.String("hardware_path", f.hardwarePath),
	)

	// Apply configuration to VPP
	if err := applyConfig(ctx, f, log); err != nil {
		return err
	}

	// Configuration applied successfully, now wait for shutdown signal
	log.Info("Daemon running, waiting for shutdown signal")
	<-ctx.Done()
	log.Info("Shutdown signal received")

	// Graceful shutdown
	// VPP connection is already closed by applyConfig's defer
	log.Info("Performing graceful shutdown")

	return nil
}
