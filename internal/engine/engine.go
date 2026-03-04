// Package engine implements the core configuration engine for arca-router.
// It computes diffs between configuration snapshots, coordinates atomic
// application of changes across southbound plugins (VPP, FRR), and provides
// transactional commit/rollback semantics.
package engine

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/akam1o/arca-router/internal/model"
)

// Engine is the central configuration engine. It holds the current running
// configuration and coordinates diff computation and atomic application
// of changes across all southbound plugins.
type Engine struct {
	mu      sync.RWMutex
	running *model.ConfigSnapshot
	plugins []Plugin
	log     *slog.Logger
	version uint64
}

// NewEngine creates a new Engine with the given plugins and logger.
func NewEngine(plugins []Plugin, log *slog.Logger) *Engine {
	return &Engine{
		plugins: plugins,
		log:     log,
	}
}

// Running returns a copy of the current running configuration.
func (e *Engine) Running() *model.RouterConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.running == nil {
		return model.NewRouterConfig()
	}
	return e.running.Config
}

// RunningSnapshot returns the current running snapshot (version, hash, etc.).
func (e *Engine) RunningSnapshot() *model.ConfigSnapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.running
}

// Apply validates and atomically applies a new configuration.
// It computes the diff from the current running config, validates through all
// plugins, and applies changes transactionally (rollback on failure).
func (e *Engine) Apply(ctx context.Context, candidate *model.RouterConfig, author, message string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Validate the candidate config
	if err := candidate.Validate(); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	// Compute diff from running → candidate
	var oldCfg *model.RouterConfig
	if e.running != nil {
		oldCfg = e.running.Config
	}
	diff := ComputeDiff(oldCfg, candidate)

	if !diff.HasChanges() {
		e.log.Info("No configuration changes detected")
		return nil
	}

	e.log.Info("Configuration diff computed",
		slog.Int("interfaces_added", len(diff.InterfacesAdded)),
		slog.Int("interfaces_removed", len(diff.InterfacesRemoved)),
		slog.Int("interfaces_changed", len(diff.InterfacesChanged)),
		slog.Bool("bgp_changed", diff.BGPChanged),
		slog.Bool("ospf_changed", diff.OSPFChanged),
		slog.Bool("policy_changed", diff.PolicyChanged),
		slog.Bool("static_routes_changed", diff.StaticRoutesChanged),
	)

	// Phase 1: Validate across all plugins (dry-run)
	for _, p := range e.plugins {
		if err := p.ValidateChanges(ctx, diff); err != nil {
			return fmt.Errorf("plugin %s validation failed: %w", p.Name(), err)
		}
	}

	// Phase 2: Apply with rollback-on-failure
	tx := &transaction{
		applied: make([]Plugin, 0, len(e.plugins)),
		diff:    diff,
		log:     e.log,
	}

	for _, p := range e.plugins {
		if err := p.ApplyChanges(ctx, diff); err != nil {
			e.log.Error("Plugin apply failed, initiating rollback",
				slog.String("plugin", p.Name()),
				slog.Any("error", err))
			tx.rollback(ctx)
			return fmt.Errorf("plugin %s apply failed (rolled back): %w", p.Name(), err)
		}
		tx.applied = append(tx.applied, p)
	}

	// Phase 3: Commit — update running config
	e.version++
	e.running = model.NewSnapshot(candidate, e.version, author, message)

	e.log.Info("Configuration committed",
		slog.Uint64("version", e.version),
		slog.String("author", author),
	)

	return nil
}

// InitializeRunning sets the initial running configuration without applying a diff.
// Used at startup when loading from datastore.
func (e *Engine) InitializeRunning(cfg *model.RouterConfig, version uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.version = version
	e.running = model.NewSnapshot(cfg, version, "system", "initial load")
}

// transaction tracks which plugins have been applied so we can rollback on failure.
type transaction struct {
	applied []Plugin
	diff    *ConfigDiff
	log     *slog.Logger
}

func (t *transaction) rollback(ctx context.Context) {
	// Rollback in reverse order
	for i := len(t.applied) - 1; i >= 0; i-- {
		p := t.applied[i]
		if err := p.RollbackChanges(ctx, t.diff); err != nil {
			t.log.Error("Plugin rollback failed (manual intervention may be required)",
				slog.String("plugin", p.Name()),
				slog.Any("error", err))
		}
	}
}
