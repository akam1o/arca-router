package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/akam1o/arca-router/pkg/config"
	"github.com/akam1o/arca-router/pkg/datastore"
	"github.com/akam1o/arca-router/pkg/device"
	"github.com/akam1o/arca-router/pkg/errors"
	"github.com/akam1o/arca-router/pkg/logger"
)

// datastoreConfigLoader loads configuration from the datastore (running config)
// Falls back to file-based loading if datastore is not available or empty
type datastoreConfigLoader struct {
	fileLoader  configLoader
	datastorePath string
}

func newDatastoreConfigLoader(fileLoader configLoader, datastorePath string) *datastoreConfigLoader {
	return &datastoreConfigLoader{
		fileLoader:  fileLoader,
		datastorePath: datastorePath,
	}
}

func (d *datastoreConfigLoader) LoadHardware(path string, log *logger.Logger) (*device.HardwareConfig, error) {
	return d.fileLoader.LoadHardware(path, log)
}

func (d *datastoreConfigLoader) LoadConfig(path string) (*config.Config, error) {
	// Try to load from datastore first
	ds, err := datastore.NewDatastore(&datastore.Config{
		Backend:    datastore.BackendSQLite,
		SQLitePath: d.datastorePath,
	})
	if err != nil {
		// Datastore not available, fall back to file
		slog.Warn("Failed to open datastore, falling back to file", slog.Any("error", err))
		return d.fileLoader.LoadConfig(path)
	}
	defer ds.Close()

	ctx := context.Background()
	running, err := ds.GetRunning(ctx)
	if err != nil {
		// No running config in datastore, fall back to file
		slog.Warn("Failed to get running config from datastore, falling back to file", slog.Any("error", err))
		return d.fileLoader.LoadConfig(path)
	}

	if running.ConfigText == "" {
		// Empty running config, fall back to file for initial bootstrap
		slog.Info("Datastore running config is empty, bootstrapping from file", slog.String("path", path))
		cfg, err := d.fileLoader.LoadConfig(path)
		if err != nil {
			return nil, err
		}

		// Save the file config to datastore as initial running config
		if err := d.bootstrapDatastore(ds, cfg); err != nil {
			slog.Warn("Failed to bootstrap datastore", slog.Any("error", err))
		} else {
			slog.Info("Bootstrapped datastore with initial running config")
		}

		return cfg, nil
	}

	// Parse running config from datastore
	slog.Info("Loading configuration from datastore",
		slog.String("commit_id", running.CommitID),
		slog.Time("timestamp", running.Timestamp))

	parser := config.NewParser(strings.NewReader(running.ConfigText))
	cfg, err := parser.Parse()
	if err != nil {
		return nil, errors.ConfigParseError("datastore:running", err)
	}

	return cfg, nil
}

// bootstrapDatastore saves the initial file-based config to datastore
func (d *datastoreConfigLoader) bootstrapDatastore(ds datastore.Datastore, cfg *config.Config) error {
	ctx := context.Background()

	// Convert config to set commands (full config serialization would be needed for production)
	// For Phase 3, we'll use a simplified approach that notes the bootstrap
	// Phase 4 should implement full config-to-text serialization
	var configText strings.Builder
	configText.WriteString("# Bootstrapped from file at startup\n")
	configText.WriteString("# Note: This is a minimal bootstrap marker\n")
	configText.WriteString("# Full config is loaded from file on this boot\n")
	if cfg.System != nil && cfg.System.HostName != "" {
		configText.WriteString(fmt.Sprintf("set system host-name %s\n", cfg.System.HostName))
	}

	// Generate a session ID for bootstrap
	sessionID := "bootstrap-session"

	// Save as candidate
	if err := ds.SaveCandidate(ctx, sessionID, configText.String()); err != nil {
		return fmt.Errorf("failed to save candidate: %w", err)
	}

	// Acquire lock for commit (required by datastore)
	lockReq := &datastore.LockRequest{
		Target:    "candidate",
		SessionID: sessionID,
		User:      "system",
		Timeout:   30 * time.Second,
	}
	if err := ds.AcquireLock(ctx, lockReq); err != nil {
		// If lock acquisition fails, still try to commit (might be ok)
		slog.Warn("Failed to acquire lock for bootstrap commit", slog.Any("error", err))
	} else {
		// Release lock after commit
		defer ds.ReleaseLock(ctx, "candidate", sessionID)
	}

	// Commit to running
	_, err := ds.Commit(ctx, &datastore.CommitRequest{
		SessionID: sessionID,
		User:      "system",
		Message:   "Bootstrapped from file configuration",
		SourceIP:  "127.0.0.1",
	})
	if err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	return nil
}
