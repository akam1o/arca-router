package frr

import (
	"context"
	"fmt"
)

// BackendMode selects how arca-router applies generated FRR configuration.
type BackendMode string

const (
	// BackendModeTransactional applies FRR changes through FRR's management
	// transaction commands. This is the preferred v0.5 path.
	BackendModeTransactional BackendMode = "transactional"

	// BackendModeFile keeps the legacy full-file generation plus reload path.
	BackendModeFile BackendMode = "file"
)

// ParseBackendMode validates a user-facing FRR apply mode.
func ParseBackendMode(mode string) (BackendMode, error) {
	switch BackendMode(mode) {
	case BackendModeTransactional:
		return BackendModeTransactional, nil
	case BackendModeFile:
		return BackendModeFile, nil
	default:
		return "", fmt.Errorf("unsupported FRR apply mode %q (valid: transactional, file)", mode)
	}
}

// Applier applies a generated FRR config through a concrete backend.
type Applier interface {
	ApplyConfig(ctx context.Context, configContent string, cfg *Config) error
}

// NewApplier creates an FRR applier for the selected backend.
func NewApplier(mode BackendMode) Applier {
	switch mode {
	case BackendModeFile:
		return NewFileApplier(NewReloader())
	case BackendModeTransactional:
		return NewTransactionalApplier(NewVtyshMgmtClient())
	default:
		return NewTransactionalApplier(NewVtyshMgmtClient())
	}
}

// FileApplier preserves the legacy config-file reload backend.
type FileApplier struct {
	reloader *Reloader
}

// NewFileApplier creates an applier backed by the existing Reloader.
func NewFileApplier(reloader *Reloader) *FileApplier {
	if reloader == nil {
		reloader = NewReloader()
	}
	return &FileApplier{reloader: reloader}
}

// ApplyConfig writes, validates, and reloads the generated FRR config file.
func (a *FileApplier) ApplyConfig(ctx context.Context, configContent string, _ *Config) error {
	return a.reloader.ApplyConfig(ctx, configContent)
}
