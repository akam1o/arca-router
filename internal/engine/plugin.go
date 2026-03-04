package engine

import "context"

// Plugin is the interface that southbound integrations (VPP, FRR) must implement.
// The engine calls these methods during the diff-based commit workflow:
//
//  1. ValidateChanges — dry-run validation (no side effects)
//  2. ApplyChanges    — apply the diff (may have side effects)
//  3. RollbackChanges — undo applied changes on failure
type Plugin interface {
	// Name returns the plugin's identifier (e.g., "vpp", "frr").
	Name() string

	// Init initializes the plugin (connect to VPP, etc.).
	Init(ctx context.Context) error

	// Close cleanly shuts down the plugin.
	Close() error

	// HealthCheck verifies the plugin is operational.
	HealthCheck(ctx context.Context) error

	// ValidateChanges performs a dry-run validation of the proposed changes.
	// Returns an error if the changes cannot be applied.
	ValidateChanges(ctx context.Context, diff *ConfigDiff) error

	// ApplyChanges applies the diff to the underlying system.
	ApplyChanges(ctx context.Context, diff *ConfigDiff) error

	// RollbackChanges undoes previously applied changes.
	// Called when a later plugin in the chain fails.
	RollbackChanges(ctx context.Context, diff *ConfigDiff) error
}
