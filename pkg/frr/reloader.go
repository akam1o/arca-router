package frr

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	// DefaultConfigPath is the default FRR configuration file path
	DefaultConfigPath = "/etc/frr/frr.conf"

	// DefaultConfigMode is the default file mode for FRR config
	DefaultConfigMode = 0640 // root:frr read/write

	// FRRReloadScript is the path to frr-reload.py
	FRRReloadScript = "/usr/lib/frr/frr-reload.py"

	// BackupExtension is the extension for backup files
	BackupExtension = ".bak"
)

// ApplyMode defines how to apply FRR configuration
type ApplyMode string

const (
	// ApplyModeAuto automatically selects the best available method
	ApplyModeAuto ApplyMode = "auto"

	// ApplyModeFRRReload uses frr-reload.py script
	ApplyModeFRRReload ApplyMode = "frr-reload"

	// ApplyModeVtysh uses vtysh -f command
	ApplyModeVtysh ApplyMode = "vtysh"
)

// Reloader manages FRR configuration application.
type Reloader struct {
	// ConfigPath is the FRR configuration file path
	ConfigPath string

	// ApplyMode determines how to apply configuration
	ApplyMode ApplyMode

	// BackupEnabled enables automatic backup before applying
	BackupEnabled bool

	// AutoRollback enables automatic rollback on apply failure
	AutoRollback bool
}

// NewReloader creates a new FRR configuration reloader.
func NewReloader() *Reloader {
	return &Reloader{
		ConfigPath:    DefaultConfigPath,
		ApplyMode:     ApplyModeAuto,
		BackupEnabled: true,
		AutoRollback:  true,
	}
}

// ValidateConfig validates FRR configuration file using vtysh --check.
func (r *Reloader) ValidateConfig(ctx context.Context, configPath string) error {
	// Check if vtysh exists and get its path
	vtyshPath, err := exec.LookPath("vtysh")
	if err != nil {
		return NewToolNotFoundError("vtysh")
	}

	// Run vtysh --check
	cmd := exec.CommandContext(ctx, vtyshPath, "--check", configPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return NewValidateError(
			fmt.Sprintf("FRR configuration validation failed: %s", string(output)),
			err,
		)
	}

	return nil
}

// BackupConfig creates a backup of the current FRR configuration.
func (r *Reloader) BackupConfig() (string, error) {
	// Check if config file exists
	if _, err := os.Stat(r.ConfigPath); os.IsNotExist(err) {
		// No config to backup (first time setup)
		return "", nil
	}

	// Create backup path with timestamp
	timestamp := time.Now().Format("20060102-150405")
	backupPath := fmt.Sprintf("%s%s.%s", r.ConfigPath, BackupExtension, timestamp)

	// Read current config
	data, err := os.ReadFile(r.ConfigPath)
	if err != nil {
		return "", NewBackupError("failed to read current config", err)
	}

	// Write backup file
	if err := os.WriteFile(backupPath, data, DefaultConfigMode); err != nil {
		return "", NewBackupError("failed to write backup file", err)
	}

	return backupPath, nil
}

// RestoreBackup restores FRR configuration from backup.
func (r *Reloader) RestoreBackup(ctx context.Context, backupPath string) error {
	if backupPath == "" {
		return NewRollbackError("no backup to restore", nil)
	}

	// Check if backup exists
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return NewRollbackError(fmt.Sprintf("backup file not found: %s", backupPath), err)
	}

	// Read backup
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return NewRollbackError("failed to read backup file", err)
	}

	// Write config atomically
	if err := r.writeConfigAtomic(data); err != nil {
		return NewRollbackError("failed to restore backup", err)
	}

	// Apply restored config
	if err := r.applyConfigInternal(ctx); err != nil {
		return NewRollbackError("failed to apply restored config", err)
	}

	return nil
}

// WriteConfig writes FRR configuration to file atomically.
func (r *Reloader) WriteConfig(configContent string) error {
	return r.writeConfigAtomic([]byte(configContent))
}

// writeConfigAtomic writes config file atomically using temp file + rename.
// Preserves existing file ownership, group, and permissions if the file exists.
func (r *Reloader) writeConfigAtomic(data []byte) error {
	// Get existing file info to preserve ownership and permissions
	var existingStat *os.FileInfo
	if stat, err := os.Stat(r.ConfigPath); err == nil {
		existingStat = &stat
	}

	// Create temp file in same directory as target config
	dir := filepath.Dir(r.ConfigPath)
	tmpFile, err := os.CreateTemp(dir, "frr.conf.tmp.*")
	if err != nil {
		return NewApplyError("failed to create temp file", err)
	}
	tmpPath := tmpFile.Name()

	// Clean up temp file on error
	defer func() {
		if tmpFile != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
		}
	}()

	// Write data to temp file
	if _, err := tmpFile.Write(data); err != nil {
		return NewApplyError("failed to write temp file", err)
	}

	// Fsync to ensure data is written to disk
	if err := tmpFile.Sync(); err != nil {
		return NewApplyError("failed to sync temp file", err)
	}

	// Close temp file
	if err := tmpFile.Close(); err != nil {
		return NewApplyError("failed to close temp file", err)
	}
	tmpFile = nil // Mark as closed

	// Set permissions
	mode := os.FileMode(DefaultConfigMode)
	if existingStat != nil {
		mode = (*existingStat).Mode().Perm()
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		return NewPermissionDeniedError("chmod config file", err)
	}

	// Preserve ownership if we have existing file stats
	// Note: Chown requires root/CAP_CHOWN, so this may fail in some environments
	if existingStat != nil {
		if err := preserveOwnership(tmpPath, *existingStat); err != nil {
			// Ownership preservation is best-effort - continue on failure
			// This is acceptable because arca-routerd runs as root in production
			// where chown will succeed
			_ = err
		}
	}

	// Atomic rename
	if err := os.Rename(tmpPath, r.ConfigPath); err != nil {
		return NewApplyError("failed to rename config file", err)
	}

	// Fsync parent directory to ensure rename is durable
	if err := syncDir(dir); err != nil {
		// Directory fsync is best-effort - continue on failure
		// The rename already succeeded; this is an optimization for crash consistency
		_ = err
	}

	return nil
}

// preserveOwnership preserves the ownership (uid/gid) of the original file.
// This requires appropriate privileges (root or CAP_CHOWN).
func preserveOwnership(path string, origStat os.FileInfo) error {
	// Get original file's uid/gid using syscall
	if sysStat, ok := origStat.Sys().(*syscall.Stat_t); ok {
		return os.Chown(path, int(sysStat.Uid), int(sysStat.Gid))
	}
	return nil // Best effort - if we can't get sys info, skip
}

// syncDir fsyncs a directory to ensure metadata changes are durable.
func syncDir(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// ApplyConfig applies FRR configuration with validation and optional rollback.
func (r *Reloader) ApplyConfig(ctx context.Context, configContent string) error {
	var backupPath string
	var err error

	// Step 1: Create backup if enabled
	if r.BackupEnabled {
		backupPath, err = r.BackupConfig()
		if err != nil {
			return err
		}
	}

	// Step 2: Write new config atomically
	if err := r.WriteConfig(configContent); err != nil {
		return err
	}

	// Step 3: Validate new config
	if err := r.ValidateConfig(ctx, r.ConfigPath); err != nil {
		// Validation failed - rollback if enabled
		if r.AutoRollback && backupPath != "" {
			if rollbackErr := r.RestoreBackup(ctx, backupPath); rollbackErr != nil {
				// Rollback also failed - return both errors
				return NewApplyError(
					fmt.Sprintf("validation failed and rollback failed: validation=%v, rollback=%v", err, rollbackErr),
					err,
				)
			}
			return NewApplyError("validation failed, rolled back to previous config", err)
		}
		return err
	}

	// Step 4: Apply config to FRR
	if err := r.applyConfigInternal(ctx); err != nil {
		// Apply failed - rollback if enabled
		if r.AutoRollback && backupPath != "" {
			if rollbackErr := r.RestoreBackup(ctx, backupPath); rollbackErr != nil {
				return NewApplyError(
					fmt.Sprintf("apply failed and rollback failed: apply=%v, rollback=%v", err, rollbackErr),
					err,
				)
			}
			return NewApplyError("apply failed, rolled back to previous config", err)
		}
		return err
	}

	return nil
}

// applyConfigInternal applies FRR configuration using the selected mode.
func (r *Reloader) applyConfigInternal(ctx context.Context) error {
	mode := r.ApplyMode

	// Auto mode: try frr-reload.py first, fall back to vtysh
	if mode == ApplyModeAuto {
		if err := r.applyWithFRRReload(ctx); err == nil {
			return nil
		}
		// frr-reload failed, try vtysh
		mode = ApplyModeVtysh
	}

	switch mode {
	case ApplyModeFRRReload:
		return r.applyWithFRRReload(ctx)
	case ApplyModeVtysh:
		return r.applyWithVtysh(ctx)
	default:
		return NewApplyError(fmt.Sprintf("unsupported apply mode: %s", mode), nil)
	}
}

// applyWithFRRReload applies config using frr-reload.py script.
func (r *Reloader) applyWithFRRReload(ctx context.Context) error {
	// Check if frr-reload.py exists
	if _, err := os.Stat(FRRReloadScript); os.IsNotExist(err) {
		return NewToolNotFoundError("frr-reload.py")
	}

	// Run frr-reload.py --reload <config>
	cmd := exec.CommandContext(ctx, FRRReloadScript, "--reload", r.ConfigPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return NewApplyError(
			fmt.Sprintf("frr-reload.py failed: %s", string(output)),
			err,
		)
	}

	return nil
}

// applyWithVtysh applies config using vtysh -f command.
func (r *Reloader) applyWithVtysh(ctx context.Context) error {
	// Check if vtysh exists and get its path
	vtyshPath, err := exec.LookPath("vtysh")
	if err != nil {
		return NewToolNotFoundError("vtysh")
	}

	// Step 1: Apply config with vtysh -f
	cmd := exec.CommandContext(ctx, vtyshPath, "-f", r.ConfigPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return NewApplyError(
			fmt.Sprintf("vtysh -f failed: %s", string(output)),
			err,
		)
	}

	// Step 2: Save config with vtysh -c 'write memory'
	cmd = exec.CommandContext(ctx, vtyshPath, "-c", "write memory")
	output, err = cmd.CombinedOutput()
	if err != nil {
		return NewApplyError(
			fmt.Sprintf("vtysh -c 'write memory' failed: %s", string(output)),
			err,
		)
	}

	return nil
}

// GetApplyMethodInfo returns information about available apply methods.
func GetApplyMethodInfo() map[ApplyMode]bool {
	return map[ApplyMode]bool{
		ApplyModeFRRReload: isFRRReloadAvailable(),
		ApplyModeVtysh:     isVtyshAvailable(),
	}
}

// isFRRReloadAvailable checks if frr-reload.py is available.
func isFRRReloadAvailable() bool {
	_, err := os.Stat(FRRReloadScript)
	return err == nil
}

// isVtyshAvailable checks if vtysh is available.
func isVtyshAvailable() bool {
	_, err := exec.LookPath("vtysh")
	return err == nil
}

// CleanupOldBackups removes old backup files, keeping only the most recent N backups.
func (r *Reloader) CleanupOldBackups(keepCount int) error {
	// Find all backup files
	pattern := fmt.Sprintf("%s%s.*", r.ConfigPath, BackupExtension)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to find backup files: %w", err)
	}

	// Sort by modification time (newest first)
	type fileInfo struct {
		path    string
		modTime time.Time
	}
	var files []fileInfo
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			continue
		}
		files = append(files, fileInfo{path: match, modTime: info.ModTime()})
	}

	// Sort by modification time descending
	for i := 0; i < len(files)-1; i++ {
		for j := i + 1; j < len(files); j++ {
			if files[i].modTime.Before(files[j].modTime) {
				files[i], files[j] = files[j], files[i]
			}
		}
	}

	// Remove old backups
	for i := keepCount; i < len(files); i++ {
		if err := os.Remove(files[i].path); err != nil {
			return fmt.Errorf("failed to remove old backup %s: %w", files[i].path, err)
		}
	}

	return nil
}

// ShowRunningConfig retrieves the current running configuration from FRR.
func ShowRunningConfig(ctx context.Context) (string, error) {
	// Check if vtysh exists and get its path
	vtyshPath, err := exec.LookPath("vtysh")
	if err != nil {
		return "", NewToolNotFoundError("vtysh")
	}

	// Run vtysh -c 'show running-config'
	cmd := exec.CommandContext(ctx, vtyshPath, "-c", "show running-config")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", NewApplyError(
			fmt.Sprintf("vtysh -c 'show running-config' failed: %s", string(output)),
			err,
		)
	}

	return strings.TrimSpace(string(output)), nil
}
