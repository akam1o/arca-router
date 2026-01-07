package auth

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// KeyPermissions defines expected permissions for sensitive key files
const (
	// ExpectedKeyFilePerms is 0600 (owner read/write only)
	ExpectedKeyFilePerms os.FileMode = 0600

	// ExpectedKeyDirPerms is 0750 (owner rwx, group rx)
	ExpectedKeyDirPerms os.FileMode = 0750
)

// KeyPermissionError represents a key file permission violation
type KeyPermissionError struct {
	Path            string
	CurrentPerms    os.FileMode
	ExpectedPerms   os.FileMode
	Owner           uint32
	Group           uint32
	ExpectedOwner   uint32
	ExpectedGroup   uint32
	IsWorldReadable bool
	IsGroupWritable bool
}

func (e *KeyPermissionError) Error() string {
	msg := fmt.Sprintf("insecure permissions on key file %s: mode=%04o (expected %04o)",
		e.Path, e.CurrentPerms, e.ExpectedPerms)

	if e.IsWorldReadable {
		msg += ", world-readable"
	}
	if e.IsGroupWritable {
		msg += ", group-writable"
	}

	if e.ExpectedOwner > 0 && e.Owner != e.ExpectedOwner {
		msg += fmt.Sprintf(", owner=%d (expected %d)", e.Owner, e.ExpectedOwner)
	}
	if e.ExpectedGroup > 0 && e.Group != e.ExpectedGroup {
		msg += fmt.Sprintf(", group=%d (expected %d)", e.Group, e.ExpectedGroup)
	}

	return msg
}

// ValidateKeyFilePermissions verifies that a key file has secure permissions
// It checks:
// - File permissions are 0600 (owner read/write only)
// - File is not world-readable or group-writable
// - Optionally validates ownership (if expectedUID/GID are > 0)
func ValidateKeyFilePermissions(path string, expectedUID, expectedGID uint32) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat key file %s: %w", path, err)
	}

	// Get file permissions
	perms := info.Mode().Perm()

	// Check for insecure permissions
	isWorldReadable := perms&0004 != 0
	isWorldWritable := perms&0002 != 0
	isGroupWritable := perms&0020 != 0
	isGroupReadable := perms&0040 != 0

	// Get owner/group info (Unix only)
	var owner, group uint32
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		owner = stat.Uid
		group = stat.Gid
	}

	// Strict check: must be exactly 0600
	if perms != ExpectedKeyFilePerms {
		return &KeyPermissionError{
			Path:            path,
			CurrentPerms:    perms,
			ExpectedPerms:   ExpectedKeyFilePerms,
			Owner:           owner,
			Group:           group,
			ExpectedOwner:   expectedUID,
			ExpectedGroup:   expectedGID,
			IsWorldReadable: isWorldReadable || isWorldWritable,
			IsGroupWritable: isGroupWritable || isGroupReadable,
		}
	}

	// Check ownership if specified
	if expectedUID > 0 && owner != expectedUID {
		return &KeyPermissionError{
			Path:          path,
			CurrentPerms:  perms,
			ExpectedPerms: ExpectedKeyFilePerms,
			Owner:         owner,
			Group:         group,
			ExpectedOwner: expectedUID,
			ExpectedGroup: expectedGID,
		}
	}

	if expectedGID > 0 && group != expectedGID {
		return &KeyPermissionError{
			Path:          path,
			CurrentPerms:  perms,
			ExpectedPerms: ExpectedKeyFilePerms,
			Owner:         owner,
			Group:         group,
			ExpectedOwner: expectedUID,
			ExpectedGroup: expectedGID,
		}
	}

	return nil
}

// ValidateKeyDirectoryPermissions verifies that a directory containing keys has secure permissions
// Directory should be 0750 or more restrictive (e.g., 0700)
func ValidateKeyDirectoryPermissions(path string, expectedUID, expectedGID uint32) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat directory %s: %w", path, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}

	perms := info.Mode().Perm()

	// Check for insecure permissions
	isWorldAccessible := perms&0007 != 0
	isGroupWritable := perms&0020 != 0

	if isWorldAccessible || isGroupWritable {
		return &KeyPermissionError{
			Path:            path,
			CurrentPerms:    perms,
			ExpectedPerms:   ExpectedKeyDirPerms,
			IsWorldReadable: isWorldAccessible,
			IsGroupWritable: isGroupWritable,
		}
	}

	// Get owner/group info (Unix only)
	var owner, group uint32
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		owner = stat.Uid
		group = stat.Gid
	}

	// Check ownership if specified
	if expectedUID > 0 && owner != expectedUID {
		return &KeyPermissionError{
			Path:          path,
			CurrentPerms:  perms,
			ExpectedPerms: ExpectedKeyDirPerms,
			Owner:         owner,
			Group:         group,
			ExpectedOwner: expectedUID,
			ExpectedGroup: expectedGID,
		}
	}

	if expectedGID > 0 && group != expectedGID {
		return &KeyPermissionError{
			Path:          path,
			CurrentPerms:  perms,
			ExpectedPerms: ExpectedKeyDirPerms,
			Owner:         owner,
			Group:         group,
			ExpectedOwner: expectedUID,
			ExpectedGroup: expectedGID,
		}
	}

	return nil
}

// SecretConfig holds configuration for secrets management
type SecretConfig struct {
	// HostKeyPath is the path to the SSH host key
	HostKeyPath string

	// UserDBPath is the path to the user database (contains password hashes)
	UserDBPath string

	// DatastorePath is the path to the datastore (may contain sensitive config)
	DatastorePath string

	// ValidateOwnership controls whether to validate file ownership
	ValidateOwnership bool

	// ExpectedUID is the expected owner UID (0 = skip check)
	ExpectedUID uint32

	// ExpectedGID is the expected owner GID (0 = skip check)
	ExpectedGID uint32
}

// ValidateSecrets validates all secret file permissions
func ValidateSecrets(config *SecretConfig) error {
	// Validate host key
	if config.HostKeyPath != "" {
		if err := ValidateKeyFilePermissions(config.HostKeyPath, config.ExpectedUID, config.ExpectedGID); err != nil {
			return fmt.Errorf("host key validation failed: %w", err)
		}

		// Also validate the directory
		dir := filepath.Dir(config.HostKeyPath)
		if err := ValidateKeyDirectoryPermissions(dir, config.ExpectedUID, config.ExpectedGID); err != nil {
			return fmt.Errorf("host key directory validation failed: %w", err)
		}
	}

	return nil
}

// SecurelyRemoveFile overwrites a file with zeros before deletion
// This is a best-effort secure deletion for secret files
// Note: This may not be effective on journaling filesystems or SSDs with wear leveling
func SecurelyRemoveFile(path string) error {
	// Get file size
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// Open file for writing
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Overwrite with zeros in chunks to avoid large allocations
	const chunkSize = 4096
	zeros := make([]byte, chunkSize)
	remaining := info.Size()

	for remaining > 0 {
		writeSize := chunkSize
		if remaining < int64(chunkSize) {
			writeSize = int(remaining)
		}

		n, err := file.Write(zeros[:writeSize])
		if err != nil {
			return fmt.Errorf("failed to overwrite file: %w", err)
		}
		remaining -= int64(n)
	}

	// Sync to disk
	if err := file.Sync(); err != nil {
		return fmt.Errorf("failed to sync file: %w", err)
	}

	// Explicitly close before remove
	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to close file: %w", err)
	}

	// Remove file
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to remove file: %w", err)
	}

	return nil
}

// GetSecretFromEnv retrieves a secret from environment variable with fallback
// This supports both direct value and file path (ending with _FILE)
// Example:
//
//	DB_PASSWORD=secret123 or DB_PASSWORD_FILE=/run/secrets/db_password
func GetSecretFromEnv(envVar string) (string, error) {
	// Try direct value first
	if val := os.Getenv(envVar); val != "" {
		return val, nil
	}

	// Try _FILE variant
	fileEnvVar := envVar + "_FILE"
	if filePath := os.Getenv(fileEnvVar); filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("failed to read secret from %s: %w", filePath, err)
		}
		// Trim trailing newline (common in Docker secrets)
		secret := string(data)
		if len(secret) > 0 && secret[len(secret)-1] == '\n' {
			secret = secret[:len(secret)-1]
		}
		return secret, nil
	}

	return "", fmt.Errorf("environment variable %s or %s not set", envVar, fileEnvVar)
}
