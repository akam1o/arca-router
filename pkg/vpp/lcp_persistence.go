package vpp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/akam1o/arca-router/pkg/errors"
)

const (
	// Default path for LCP mapping persistence
	defaultLCPMappingPath = "/var/lib/arca-router/lcp_mapping.json"
	// File permissions for LCP mapping file (owner: rw, group: r, other: none)
	lcpMappingFileMode = 0640
	// Directory permissions for parent directory
	lcpMappingDirMode = 0750
)

// LCPMapping represents a single LCP name mapping entry for persistence
type LCPMapping struct {
	SwIfIndex  uint32 `json:"sw_if_index"`
	LinuxName  string `json:"linux_name"`
	JunosName  string `json:"junos_name"`
	HostIfType string `json:"host_if_type"`
	Netns      string `json:"netns"`
}

// LCPPersistence manages persistence of LCP name mappings to disk
type LCPPersistence struct {
	mu   sync.RWMutex
	path string
}

// NewLCPPersistence creates a new LCP persistence manager
func NewLCPPersistence() *LCPPersistence {
	return &LCPPersistence{
		path: defaultLCPMappingPath,
	}
}

// NewLCPPersistenceWithPath creates a new LCP persistence manager with custom path
func NewLCPPersistenceWithPath(path string) *LCPPersistence {
	return &LCPPersistence{
		path: path,
	}
}

// SaveMapping persists LCP mappings to disk in JSON format
// This method is atomic - writes to a temp file and renames to avoid partial writes
func (p *LCPPersistence) SaveMapping(mappings []*LCPMapping) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Ensure parent directory exists with correct permissions
	dir := filepath.Dir(p.path)
	if err := os.MkdirAll(dir, lcpMappingDirMode); err != nil {
		return errors.Wrap(err, errors.ErrCodeSystemError,
			"Failed to create LCP mapping directory",
			fmt.Sprintf("Could not create directory: %s", dir),
			"Ensure the process has permission to create directories")
	}

	// Marshal mappings to JSON
	data, err := json.MarshalIndent(mappings, "", "  ")
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeSystemError,
			"Failed to marshal LCP mappings to JSON",
			"Could not serialize LCP mapping data",
			"This is an internal error - contact support")
	}

	// Write to temporary file first (atomic write pattern with security)
	// Use CreateTemp with O_EXCL to avoid symlink attacks
	tempFile, err := os.CreateTemp(dir, ".lcp_mapping.*.tmp")
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeSystemError,
			"Failed to create LCP mapping temporary file",
			fmt.Sprintf("Could not create temp file in: %s", dir),
			"Ensure the process has permission to write to /var/lib/arca-router/")
	}
	tempPath := tempFile.Name()

	cleanupTemp := func() {
		if err := tempFile.Close(); err != nil {
			_ = err
		}
		if err := os.Remove(tempPath); err != nil && !os.IsNotExist(err) {
			_ = err
		}
	}

	// Set correct permissions before writing
	if err := tempFile.Chmod(lcpMappingFileMode); err != nil {
		cleanupTemp()
		return errors.Wrap(err, errors.ErrCodeSystemError,
			"Failed to set LCP mapping file permissions",
			fmt.Sprintf("Could not chmod %s", tempPath),
			"Ensure the process has permission to set file permissions")
	}

	// Write data to temp file
	if _, err := tempFile.Write(data); err != nil {
		cleanupTemp()
		return errors.Wrap(err, errors.ErrCodeSystemError,
			"Failed to write LCP mapping temporary file",
			fmt.Sprintf("Could not write to: %s", tempPath),
			"Ensure the process has permission to write to /var/lib/arca-router/")
	}

	// Fsync file to ensure data is persisted to disk
	if err := tempFile.Sync(); err != nil {
		cleanupTemp()
		return errors.Wrap(err, errors.ErrCodeSystemError,
			"Failed to sync LCP mapping file to disk",
			fmt.Sprintf("Could not fsync %s", tempPath),
			"Check disk health and filesystem")
	}

	// Close file before rename
	if err := tempFile.Close(); err != nil {
		if err := os.Remove(tempPath); err != nil && !os.IsNotExist(err) {
			_ = err
		}
		return errors.Wrap(err, errors.ErrCodeSystemError,
			"Failed to close LCP mapping temporary file",
			fmt.Sprintf("Could not close %s", tempPath),
			"Ensure the process has permission to write to /var/lib/arca-router/")
	}

	// Atomic rename from temp to target path
	if err := os.Rename(tempPath, p.path); err != nil {
		// Clean up temp file on failure
		if err := os.Remove(tempPath); err != nil && !os.IsNotExist(err) {
			_ = err
		}
		return errors.Wrap(err, errors.ErrCodeSystemError,
			"Failed to rename LCP mapping file",
			fmt.Sprintf("Could not rename %s to %s", tempPath, p.path),
			"Ensure the process has permission to write to /var/lib/arca-router/")
	}

	// Fsync directory to ensure rename is persisted
	dirFile, err := os.Open(dir)
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeSystemError,
			"Failed to open directory for fsync",
			fmt.Sprintf("Could not open directory: %s", dir),
			"Check directory permissions and filesystem health")
	}
	defer func() {
		if err := dirFile.Close(); err != nil {
			_ = err
		}
	}()

	if err := dirFile.Sync(); err != nil {
		return errors.Wrap(err, errors.ErrCodeSystemError,
			"Failed to fsync directory after rename",
			fmt.Sprintf("Could not fsync directory: %s", dir),
			"Check disk health and filesystem - rename may not be durable")
	}

	return nil
}

// LoadMapping reads LCP mappings from disk
// Returns an empty slice if the file doesn't exist (not an error)
func (p *LCPPersistence) LoadMapping() ([]*LCPMapping, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Check if file exists
	if _, err := os.Stat(p.path); os.IsNotExist(err) {
		// File doesn't exist - return empty slice (not an error)
		return []*LCPMapping{}, nil
	}

	// Read file contents
	data, err := os.ReadFile(p.path)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeSystemError,
			"Failed to read LCP mapping file",
			fmt.Sprintf("Could not read from: %s", p.path),
			"Ensure the file exists and the process has read permission")
	}

	// Unmarshal JSON data
	var mappings []*LCPMapping
	if err := json.Unmarshal(data, &mappings); err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeConfigParseError,
			"Failed to parse LCP mapping JSON",
			fmt.Sprintf("Invalid JSON in: %s", p.path),
			"The LCP mapping file may be corrupted - restore from backup or delete to start fresh")
	}

	return mappings, nil
}

// DeleteMapping removes the LCP mapping file from disk
// This is used when clearing all LCP pairs or during cleanup
func (p *LCPPersistence) DeleteMapping() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if file exists
	if _, err := os.Stat(p.path); os.IsNotExist(err) {
		// File doesn't exist - nothing to delete (not an error)
		return nil
	}

	// Remove the file
	if err := os.Remove(p.path); err != nil {
		// Ignore if file was already deleted by another process (race condition)
		if os.IsNotExist(err) {
			return nil
		}
		return errors.Wrap(err, errors.ErrCodeSystemError,
			"Failed to delete LCP mapping file",
			fmt.Sprintf("Could not delete: %s", p.path),
			"Ensure the process has permission to delete files in /var/lib/arca-router/")
	}

	// Fsync directory to ensure deletion is persisted (crash safety)
	dir := filepath.Dir(p.path)
	dirFile, err := os.Open(dir)
	if err != nil {
		return errors.Wrap(err, errors.ErrCodeSystemError,
			"Failed to open directory for fsync after delete",
			fmt.Sprintf("Could not open directory: %s", dir),
			"Check directory permissions and filesystem health")
	}
	defer func() {
		if err := dirFile.Close(); err != nil {
			_ = err
		}
	}()

	if err := dirFile.Sync(); err != nil {
		return errors.Wrap(err, errors.ErrCodeSystemError,
			"Failed to fsync directory after delete",
			fmt.Sprintf("Could not fsync directory: %s", dir),
			"Check disk health and filesystem - deletion may not be durable")
	}

	return nil
}

// ValidateMapping checks if the loaded mappings are valid
// Returns a list of validation errors (empty if valid)
func ValidateMapping(mappings []*LCPMapping) []string {
	validationErrors := make([]string, 0)

	seenSwIfIndex := make(map[uint32]bool)
	seenLinuxName := make(map[string]bool)
	seenJunosName := make(map[string]bool)

	for i, mapping := range mappings {
		// Check for required fields
		if mapping.LinuxName == "" {
			validationErrors = append(validationErrors,
				fmt.Sprintf("mapping[%d]: missing linux_name", i))
		}
		if mapping.JunosName == "" {
			validationErrors = append(validationErrors,
				fmt.Sprintf("mapping[%d]: missing junos_name", i))
		}

		// Check for duplicates
		if seenSwIfIndex[mapping.SwIfIndex] {
			validationErrors = append(validationErrors,
				fmt.Sprintf("mapping[%d]: duplicate sw_if_index: %d", i, mapping.SwIfIndex))
		}
		seenSwIfIndex[mapping.SwIfIndex] = true

		if mapping.LinuxName != "" && seenLinuxName[mapping.LinuxName] {
			validationErrors = append(validationErrors,
				fmt.Sprintf("mapping[%d]: duplicate linux_name: %s", i, mapping.LinuxName))
		}
		seenLinuxName[mapping.LinuxName] = true

		if mapping.JunosName != "" && seenJunosName[mapping.JunosName] {
			validationErrors = append(validationErrors,
				fmt.Sprintf("mapping[%d]: duplicate junos_name: %s", i, mapping.JunosName))
		}
		seenJunosName[mapping.JunosName] = true

		// Validate Linux interface name format
		if mapping.LinuxName != "" {
			if err := ValidateLinuxIfName(mapping.LinuxName); err != nil {
				validationErrors = append(validationErrors,
					fmt.Sprintf("mapping[%d]: invalid linux_name format: %s", i, err.Error()))
			}
		}
	}

	return validationErrors
}
