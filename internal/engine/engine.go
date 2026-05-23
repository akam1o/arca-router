// Package engine implements the core configuration engine for arca-router.
// It computes diffs between configuration snapshots, coordinates atomic
// application of changes across southbound plugins (VPP, FRR), and provides
// transactional commit/rollback semantics.
package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/akam1o/arca-router/internal/model"
)

var ErrConfigValidation = errors.New("configuration validation error")

type configValidationError struct {
	cause error
}

func (e configValidationError) Error() string {
	return fmt.Sprintf("config validation failed: %v", e.cause)
}

func (e configValidationError) Unwrap() []error {
	return []error{ErrConfigValidation, e.cause}
}

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

// ApplyError describes a failed configuration apply phase with rollback status.
type ApplyError struct {
	Plugin              string
	Phase               string
	RollbackAttempted   bool
	RollbackSucceeded   bool
	RollbackDiagnostics []string
	Err                 error
}

func (e *ApplyError) Error() string {
	if e == nil {
		return ""
	}
	phase := e.Phase
	if phase == "" {
		phase = "apply"
	}
	msg := fmt.Sprintf("plugin %s %s failed", e.Plugin, phase)
	if e.RollbackAttempted {
		if e.RollbackSucceeded {
			msg += " (rollback succeeded)"
		} else {
			msg += " (rollback failed"
			if len(e.RollbackDiagnostics) > 0 {
				msg += ": " + strings.Join(e.RollbackDiagnostics, "; ")
			}
			msg += ")"
		}
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *ApplyError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
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
	if cfg := e.running.Config.Clone(); cfg != nil {
		return cfg
	}
	return model.NewRouterConfig()
}

// RunningSnapshot returns the current running snapshot (version, hash, etc.).
func (e *Engine) RunningSnapshot() *model.ConfigSnapshot {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.running == nil {
		return nil
	}
	return e.running.Clone()
}

// Validate checks whether a candidate configuration can be applied without
// mutating engine or southbound state.
func (e *Engine) Validate(ctx context.Context, candidate *model.RouterConfig) error {
	if candidate == nil {
		return fmt.Errorf("configuration is nil")
	}
	if err := candidate.Validate(); err != nil {
		return configValidationError{cause: err}
	}

	e.mu.RLock()
	var oldCfg *model.RouterConfig
	if e.running != nil {
		oldCfg = e.running.Config.Clone()
	}
	plugins := append([]Plugin(nil), e.plugins...)
	e.mu.RUnlock()

	diff := ComputeDiff(oldCfg, candidate.Clone())
	for _, p := range plugins {
		if err := p.ValidateChanges(ctx, diff.Clone()); err != nil {
			return fmt.Errorf("plugin %s validation failed: %w", p.Name(), err)
		}
	}
	return nil
}

// Apply validates and atomically applies a new configuration.
// It computes the diff from the current running config, validates through all
// plugins, and applies changes transactionally (rollback on failure).
func (e *Engine) Apply(ctx context.Context, candidate *model.RouterConfig, author, message string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if candidate == nil {
		return fmt.Errorf("configuration is nil")
	}
	candidate = candidate.Clone()

	// Validate the candidate config
	if err := candidate.Validate(); err != nil {
		return configValidationError{cause: err}
	}

	// Compute diff from running → candidate
	var oldCfg *model.RouterConfig
	if e.running != nil {
		oldCfg = e.running.Config.Clone()
	}
	diff := ComputeDiff(oldCfg, candidate.Clone())

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
		slog.Bool("ospf3_changed", diff.OSPF3Changed),
		slog.Bool("policy_changed", diff.PolicyChanged),
		slog.Bool("static_routes_changed", diff.StaticRoutesChanged),
	)

	// Phase 1: Validate across all plugins (dry-run)
	for _, p := range e.plugins {
		if err := p.ValidateChanges(ctx, diff.Clone()); err != nil {
			return fmt.Errorf("plugin %s validation failed: %w", p.Name(), err)
		}
	}

	// Phase 2: Apply with rollback-on-failure
	tx := &transaction{
		applied: make([]appliedPlugin, 0, len(e.plugins)),
		log:     e.log,
	}

	for _, p := range e.plugins {
		applyDiff := diff.Clone()
		rollbackDiff := diff.Clone()
		tx.applied = append(tx.applied, appliedPlugin{
			plugin: p,
			diff:   rollbackDiff,
		})
		if err := p.ApplyChanges(ctx, applyDiff); err != nil {
			e.log.Error("Plugin apply failed, initiating rollback",
				slog.String("plugin", p.Name()),
				slog.Any("error", err))
			rollbackErr := tx.rollback(ctx)
			diagnostics := rollbackDiagnostics(rollbackErr)
			return &ApplyError{
				Plugin:              p.Name(),
				Phase:               "apply",
				RollbackAttempted:   true,
				RollbackSucceeded:   rollbackErr == nil,
				RollbackDiagnostics: diagnostics,
				Err:                 err,
			}
		}
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
	applied []appliedPlugin
	log     *slog.Logger
}

type appliedPlugin struct {
	plugin Plugin
	diff   *ConfigDiff
}

func (t *transaction) rollback(ctx context.Context) error {
	// Rollback in reverse order
	var rollbackErrs []error
	for i := len(t.applied) - 1; i >= 0; i-- {
		applied := t.applied[i]
		p := applied.plugin
		if err := p.RollbackChanges(ctx, applied.diff); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("plugin %s rollback failed: %w", p.Name(), err))
			t.log.Error("Plugin rollback failed (manual intervention may be required)",
				slog.String("plugin", p.Name()),
				slog.Any("error", err))
		}
	}
	return errors.Join(rollbackErrs...)
}

func rollbackDiagnostics(err error) []string {
	if err == nil {
		return nil
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return []string{err.Error()}
	}
	errs := joined.Unwrap()
	diagnostics := make([]string, 0, len(errs))
	for _, rollbackErr := range errs {
		if rollbackErr != nil {
			diagnostics = append(diagnostics, rollbackErr.Error())
		}
	}
	return diagnostics
}
