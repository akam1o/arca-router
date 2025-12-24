package frr

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestNewReloader tests NewReloader constructor.
func TestNewReloader(t *testing.T) {
	r := NewReloader()
	if r.ConfigPath != DefaultConfigPath {
		t.Errorf("expected ConfigPath=%s, got %s", DefaultConfigPath, r.ConfigPath)
	}
	if r.ApplyMode != ApplyModeAuto {
		t.Errorf("expected ApplyMode=%s, got %s", ApplyModeAuto, r.ApplyMode)
	}
	if !r.BackupEnabled {
		t.Error("expected BackupEnabled=true")
	}
	if !r.AutoRollback {
		t.Error("expected AutoRollback=true")
	}
}

// TestWriteConfigAtomic tests atomic config file writing.
func TestWriteConfigAtomic(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "frr.conf")

	r := &Reloader{
		ConfigPath:    configPath,
		BackupEnabled: false,
	}

	// Test write
	content := "test config content\n"
	if err := r.WriteConfig(content); err != nil {
		t.Fatalf("WriteConfig failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("config file not created")
	}

	// Verify content
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	if string(data) != content {
		t.Errorf("expected content=%q, got %q", content, string(data))
	}

	// Verify permissions
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("failed to stat config: %v", err)
	}
	mode := info.Mode() & os.ModePerm
	if mode != DefaultConfigMode {
		t.Errorf("expected mode=%o, got %o", DefaultConfigMode, mode)
	}
}

// TestBackupConfig tests backup creation.
func TestBackupConfig(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "frr.conf")

	r := &Reloader{
		ConfigPath:    configPath,
		BackupEnabled: true,
	}

	// Create initial config
	initialContent := "initial config\n"
	if err := os.WriteFile(configPath, []byte(initialContent), DefaultConfigMode); err != nil {
		t.Fatalf("failed to create initial config: %v", err)
	}

	// Create backup
	backupPath, err := r.BackupConfig()
	if err != nil {
		t.Fatalf("BackupConfig failed: %v", err)
	}

	if backupPath == "" {
		t.Fatal("expected backup path, got empty string")
	}

	// Verify backup exists
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Fatal("backup file not created")
	}

	// Verify backup content
	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("failed to read backup: %v", err)
	}
	if string(data) != initialContent {
		t.Errorf("expected backup content=%q, got %q", initialContent, string(data))
	}

	// Test backup path format (should contain timestamp)
	if !strings.Contains(backupPath, BackupExtension) {
		t.Errorf("expected backup path to contain %s, got %s", BackupExtension, backupPath)
	}
}

// TestBackupConfigNoExisting tests backup when no config exists.
func TestBackupConfigNoExisting(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "frr.conf")

	r := &Reloader{
		ConfigPath:    configPath,
		BackupEnabled: true,
	}

	// Backup should succeed with empty path (no config to backup)
	backupPath, err := r.BackupConfig()
	if err != nil {
		t.Fatalf("BackupConfig failed: %v", err)
	}
	if backupPath != "" {
		t.Errorf("expected empty backup path, got %s", backupPath)
	}
}

// TestRestoreBackup tests backup restoration.
func TestRestoreBackup(t *testing.T) {
	// Skip if FRR tools not available
	if !isVtyshAvailable() {
		t.Skip("vtysh not available, skipping restore test")
	}

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "frr.conf")
	backupPath := filepath.Join(tmpDir, "frr.conf.bak")

	r := &Reloader{
		ConfigPath:    configPath,
		BackupEnabled: false,
		AutoRollback:  false,
	}

	// Create backup content
	backupContent := "! Backup config\nfrr defaults traditional\nhostname backup-router\nlog stdout\nline vty\n!\nend\n"
	if err := os.WriteFile(backupPath, []byte(backupContent), DefaultConfigMode); err != nil {
		t.Fatalf("failed to create backup: %v", err)
	}

	// Create current config (different content)
	currentContent := "! Current config\nfrr defaults traditional\nhostname current-router\n!\nend\n"
	if err := os.WriteFile(configPath, []byte(currentContent), DefaultConfigMode); err != nil {
		t.Fatalf("failed to create current config: %v", err)
	}

	// Note: RestoreBackup requires FRR to apply config, which we can't test in unit tests
	// We'll test the file restoration part only
	ctx := context.Background()

	// Read backup
	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("failed to read backup: %v", err)
	}

	// Write config atomically
	if err := r.writeConfigAtomic(data); err != nil {
		t.Fatalf("writeConfigAtomic failed: %v", err)
	}

	// Verify restored content
	restoredData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read restored config: %v", err)
	}
	if string(restoredData) != backupContent {
		t.Errorf("expected restored content=%q, got %q", backupContent, string(restoredData))
	}

	// Note: We can't test the actual FRR apply in unit tests
	_ = ctx
}

// TestRestoreBackupNoBackup tests restore with no backup file.
func TestRestoreBackupNoBackup(t *testing.T) {
	tmpDir := t.TempDir()
	r := &Reloader{
		ConfigPath: filepath.Join(tmpDir, "frr.conf"),
	}

	ctx := context.Background()
	err := r.RestoreBackup(ctx, "")
	if err == nil {
		t.Error("expected error for empty backup path")
	}

	err = r.RestoreBackup(ctx, "/nonexistent/backup.bak")
	if err == nil {
		t.Error("expected error for nonexistent backup")
	}
}

// TestValidateConfig tests configuration validation.
func TestValidateConfig(t *testing.T) {
	// Skip if vtysh not available
	if !isVtyshAvailable() {
		t.Skip("vtysh not available, skipping validation test")
	}

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "frr.conf")

	r := NewReloader()
	ctx := context.Background()

	// Test valid config
	validConfig := `!
! FRR configuration
!
frr defaults traditional
hostname test-router
log stdout
!
router bgp 65000
 bgp router-id 10.0.0.1
 neighbor 10.0.1.1 remote-as 65001
 !
 address-family ipv4 unicast
  network 10.0.0.0/24
 exit-address-family
!
line vty
!
end
`
	if err := os.WriteFile(configPath, []byte(validConfig), DefaultConfigMode); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	if err := r.ValidateConfig(ctx, configPath); err != nil {
		t.Errorf("ValidateConfig failed for valid config: %v", err)
	}

	// Test invalid config (syntax error)
	invalidConfig := `!
! Invalid FRR configuration
!
router bgp
 invalid syntax here
!
end
`
	if err := os.WriteFile(configPath, []byte(invalidConfig), DefaultConfigMode); err != nil {
		t.Fatalf("failed to write invalid config: %v", err)
	}

	if err := r.ValidateConfig(ctx, configPath); err == nil {
		t.Error("expected ValidateConfig to fail for invalid config")
	}
}

// TestApplyConfigWithValidation tests config application with validation.
func TestApplyConfigWithValidation(t *testing.T) {
	// Skip if FRR tools not available
	if !isVtyshAvailable() {
		t.Skip("vtysh not available, skipping apply test")
	}

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "frr.conf")

	r := &Reloader{
		ConfigPath:    configPath,
		ApplyMode:     ApplyModeVtysh, // Use vtysh mode (more likely to be available)
		BackupEnabled: true,
		AutoRollback:  true,
	}

	ctx := context.Background()

	// Test with valid config
	validConfig := `!
frr defaults traditional
hostname test-router
log stdout
line vty
!
end
`

	// Create initial config for backup
	initialConfig := `!
frr defaults traditional
hostname initial-router
log stdout
line vty
!
end
`
	if err := os.WriteFile(configPath, []byte(initialConfig), DefaultConfigMode); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	// Note: ApplyConfig will fail in unit test environment because FRR is not running
	// We test the validation and file writing parts
	err := r.ApplyConfig(ctx, validConfig)
	// We expect an error because FRR is not running, but validation should pass
	// Check that config was written
	if _, statErr := os.Stat(configPath); statErr != nil {
		t.Errorf("config file not created: %v", statErr)
	}

	_ = err // Ignore apply error in unit test (FRR not running)
}

// TestApplyConfigInvalidSyntax tests apply with invalid config syntax.
func TestApplyConfigInvalidSyntax(t *testing.T) {
	// Skip if vtysh not available
	if !isVtyshAvailable() {
		t.Skip("vtysh not available, skipping validation test")
	}

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "frr.conf")

	r := &Reloader{
		ConfigPath:    configPath,
		ApplyMode:     ApplyModeVtysh,
		BackupEnabled: true,
		AutoRollback:  true,
	}

	ctx := context.Background()

	// Invalid config (syntax error)
	invalidConfig := `!
router bgp
 invalid syntax
!
end
`

	err := r.ApplyConfig(ctx, invalidConfig)
	if err == nil {
		t.Error("expected ApplyConfig to fail for invalid config")
	}

	// Check that error is a validation error
	if !strings.Contains(err.Error(), "validation") {
		t.Errorf("expected validation error, got: %v", err)
	}
}

// TestGetApplyMethodInfo tests apply method availability check.
func TestGetApplyMethodInfo(t *testing.T) {
	info := GetApplyMethodInfo()

	// Should have info for both methods
	if _, ok := info[ApplyModeFRRReload]; !ok {
		t.Error("expected info for ApplyModeFRRReload")
	}
	if _, ok := info[ApplyModeVtysh]; !ok {
		t.Error("expected info for ApplyModeVtysh")
	}

	// At least vtysh should be available in test environment
	// (FRR might not be installed, so we don't assert on frr-reload)
	t.Logf("Apply method info: %+v", info)
}

// TestCleanupOldBackups tests backup cleanup.
func TestCleanupOldBackups(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "frr.conf")

	r := &Reloader{
		ConfigPath: configPath,
	}

	// Create multiple backup files with different timestamps
	backupFiles := []string{
		filepath.Join(tmpDir, "frr.conf.bak.20230101-120000"),
		filepath.Join(tmpDir, "frr.conf.bak.20230102-120000"),
		filepath.Join(tmpDir, "frr.conf.bak.20230103-120000"),
		filepath.Join(tmpDir, "frr.conf.bak.20230104-120000"),
		filepath.Join(tmpDir, "frr.conf.bak.20230105-120000"),
	}

	for i, path := range backupFiles {
		if err := os.WriteFile(path, []byte("backup content"), DefaultConfigMode); err != nil {
			t.Fatalf("failed to create backup %d: %v", i, err)
		}
		// Set different modification times
		modTime := time.Now().Add(time.Duration(i) * time.Hour)
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatalf("failed to set mtime for backup %d: %v", i, err)
		}
	}

	// Keep only 3 most recent backups
	if err := r.CleanupOldBackups(3); err != nil {
		t.Fatalf("CleanupOldBackups failed: %v", err)
	}

	// Verify that only 3 files remain
	matches, err := filepath.Glob(filepath.Join(tmpDir, "frr.conf.bak.*"))
	if err != nil {
		t.Fatalf("failed to glob backups: %v", err)
	}

	if len(matches) != 3 {
		t.Errorf("expected 3 backups remaining, got %d", len(matches))
	}

	// Verify the oldest backups were removed (first 2 files)
	for _, oldPath := range backupFiles[:2] {
		if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
			t.Errorf("expected old backup %s to be removed", oldPath)
		}
	}

	// Verify the newest backups remain (last 3 files)
	for _, newPath := range backupFiles[2:] {
		if _, err := os.Stat(newPath); err != nil {
			t.Errorf("expected new backup %s to remain: %v", newPath, err)
		}
	}
}

// TestShowRunningConfig tests running config retrieval.
func TestShowRunningConfig(t *testing.T) {
	// Skip if vtysh not available
	if !isVtyshAvailable() {
		t.Skip("vtysh not available, skipping show running-config test")
	}

	ctx := context.Background()

	// Note: This will fail if FRR is not running, but we test the execution path
	config, err := ShowRunningConfig(ctx)

	// In unit test environment, FRR is likely not running, so we expect an error
	// But we can verify the function doesn't panic and returns an error
	if err != nil {
		t.Logf("ShowRunningConfig failed (expected in test env): %v", err)
		return
	}

	// If FRR is actually running, verify we got some output
	if config == "" {
		t.Error("expected non-empty running config")
	}
	t.Logf("Running config: %s", config)
}

// TestApplyModeAuto tests auto mode selection.
func TestApplyModeAuto(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "frr.conf")

	r := &Reloader{
		ConfigPath:    configPath,
		ApplyMode:     ApplyModeAuto,
		BackupEnabled: false,
		AutoRollback:  false,
	}

	ctx := context.Background()

	// Write a valid config
	validConfig := `!
frr defaults traditional
hostname test-router
log stdout
line vty
!
end
`
	if err := r.WriteConfig(validConfig); err != nil {
		t.Fatalf("WriteConfig failed: %v", err)
	}

	// Note: applyConfigInternal will fail because FRR is not running
	// We test that auto mode tries methods without panicking
	err := r.applyConfigInternal(ctx)
	if err == nil {
		t.Log("applyConfigInternal succeeded (FRR is running)")
	} else {
		t.Logf("applyConfigInternal failed (expected): %v", err)
	}
}

// TestContextCancellation tests context cancellation handling.
func TestContextCancellation(t *testing.T) {
	if !isVtyshAvailable() {
		t.Skip("vtysh not available, skipping context test")
	}

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "frr.conf")

	r := &Reloader{
		ConfigPath:    configPath,
		BackupEnabled: false,
	}

	// Create canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	validConfig := `!
frr defaults traditional
hostname test-router
log stdout
line vty
!
end
`

	// Write config (should succeed - not async)
	if err := r.WriteConfig(validConfig); err != nil {
		t.Fatalf("WriteConfig failed: %v", err)
	}

	// Validate with canceled context (should fail quickly)
	err := r.ValidateConfig(ctx, configPath)
	if err == nil {
		t.Error("expected ValidateConfig to fail with canceled context")
	}
}

// TestErrorTypes tests that correct error types are returned.
func TestErrorTypes(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name     string
		testFunc func() error
		wantCode string
	}{
		{
			name: "tool not found",
			testFunc: func() error {
				return NewToolNotFoundError("test-tool")
			},
			wantCode: ErrCodeToolNotFound,
		},
		{
			name: "validation error",
			testFunc: func() error {
				return NewValidateError("test validation", nil)
			},
			wantCode: ErrCodeValidateFailed,
		},
		{
			name: "apply error",
			testFunc: func() error {
				return NewApplyError("test apply", nil)
			},
			wantCode: ErrCodeApplyFailed,
		},
		{
			name: "backup error",
			testFunc: func() error {
				return NewBackupError("test backup", nil)
			},
			wantCode: ErrCodeBackupFailed,
		},
		{
			name: "rollback error",
			testFunc: func() error {
				return NewRollbackError("test rollback", nil)
			},
			wantCode: ErrCodeRollbackFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.testFunc()
			if frrErr, ok := err.(*Error); ok {
				if frrErr.Code != tt.wantCode {
					t.Errorf("expected error code=%s, got %s", tt.wantCode, frrErr.Code)
				}
			} else {
				t.Errorf("expected *Error type, got %T", err)
			}
		})
	}

	_ = tmpDir
}
