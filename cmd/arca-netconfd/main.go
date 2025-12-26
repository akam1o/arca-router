package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/akam1o/arca-router/pkg/logger"
	"github.com/akam1o/arca-router/pkg/netconf"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	// Command line flags
	var (
		listenAddr  = flag.String("listen", ":830", "SSH listen address")
		hostKeyPath = flag.String("host-key", "/var/lib/arca-router/ssh_host_ed25519_key", "Path to SSH host key")
		userDBPath  = flag.String("user-db", "/var/lib/arca-router/users.db", "Path to user database")
		showVersion = flag.Bool("version", false, "Show version information")
	)
	flag.Parse()

	if *showVersion {
		fmt.Printf("arca-netconfd version %s (commit: %s)\n", version, commit)
		os.Exit(0)
	}

	// Create logger
	log := logger.New("arca-netconfd", logger.DefaultConfig())

	log.Info("Starting arca-netconfd", "version", version, "commit", commit)

	// Create SSH config
	config := netconf.DefaultSSHConfig()
	config.ListenAddr = *listenAddr
	config.HostKeyPath = *hostKeyPath
	config.UserDBPath = *userDBPath

	// Create SSH server
	server, err := netconf.NewSSHServer(config)
	if err != nil {
		log.Error("Failed to create SSH server", "error", err)
		os.Exit(1)
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start server
	if err := server.Start(ctx); err != nil {
		log.Error("Failed to start SSH server", "error", err)
		os.Exit(1)
	}

	log.Info("NETCONF server started successfully")

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	log.Info("Received shutdown signal")

	// Cancel context to trigger cleanup
	cancel()

	// Stop server
	if err := server.Stop(); err != nil {
		log.Error("Error during shutdown", "error", err)
		os.Exit(1)
	}

	log.Info("Shutdown complete")
}
