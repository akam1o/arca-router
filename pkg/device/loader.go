package device

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"

	"github.com/akam1o/arca-router/pkg/errors"
	"github.com/akam1o/arca-router/pkg/logger"
)

var (
	// pciAddressPattern validates PCI address format (e.g., "0000:03:00.0")
	pciAddressPattern = regexp.MustCompile(`^[0-9a-fA-F]{4}:[0-9a-fA-F]{2}:[0-9a-fA-F]{2}\.[0-7]$`)
)

// LoadHardware loads and validates hardware configuration from YAML file
// Note: This function logs diagnostic information if a logger is provided
func LoadHardware(path string, log *logger.Logger) (*HardwareConfig, error) {
	if log != nil {
		log.Debug("Loading hardware configuration", slog.String("path", path))
	}

	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, errors.New(
			errors.ErrCodeHardwareNotFound,
			fmt.Sprintf("Hardware configuration file not found: %s", path),
			"The specified hardware.yaml file does not exist",
			"Create the hardware.yaml file or specify a valid path with --hardware flag",
		)
	}

	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.Wrap(
			err,
			errors.ErrCodeConfigPermission,
			fmt.Sprintf("Failed to read hardware configuration: %s", path),
			"Permission denied or file is not readable",
			"Check file permissions with 'ls -l' and ensure the file is readable",
		)
	}

	// Parse YAML with strict mode to detect unknown fields (typo detection)
	var config HardwareConfig
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true) // Reject unknown fields
	if err := decoder.Decode(&config); err != nil {
		return nil, errors.Wrap(
			err,
			errors.ErrCodeHardwareParseError,
			fmt.Sprintf("Failed to parse hardware configuration: %s", path),
			"Invalid YAML syntax, structure, or unknown fields (check for typos)",
			"Verify YAML syntax with a validator or check against examples/hardware.yaml",
		)
	}

	// Validate configuration
	if err := ValidateHardwareConfig(&config); err != nil {
		return nil, errors.Wrap(
			err,
			errors.ErrCodeConfigValidation,
			"Hardware configuration validation failed",
			"Configuration contains invalid values",
			"Review the error details and fix the hardware.yaml file",
		)
	}

	if log != nil {
		log.Info("Hardware configuration loaded successfully",
			slog.Int("interface_count", len(config.Interfaces)),
		)
	}

	return &config, nil
}

// ValidateHardwareConfig performs comprehensive validation of hardware configuration
func ValidateHardwareConfig(config *HardwareConfig) error {
	if config == nil {
		return fmt.Errorf("hardware configuration is nil")
	}

	if len(config.Interfaces) == 0 {
		return fmt.Errorf("no interfaces defined in hardware configuration")
	}

	// Track seen names and PCI addresses to detect duplicates
	seenNames := make(map[string]bool)
	seenPCIs := make(map[string]bool)

	for i, iface := range config.Interfaces {
		// Basic validation
		if err := iface.Validate(); err != nil {
			return fmt.Errorf("interface %d: %w", i, err)
		}

		// Validate PCI address format
		if !pciAddressPattern.MatchString(iface.PCI) {
			return fmt.Errorf("interface %d (%s): invalid PCI address format: %s (expected format: 0000:00:00.0)",
				i, iface.Name, iface.PCI)
		}

		// Check for duplicate interface names
		if seenNames[iface.Name] {
			return fmt.Errorf("duplicate interface name: %s", iface.Name)
		}
		seenNames[iface.Name] = true

		// Check for duplicate PCI addresses
		if seenPCIs[iface.PCI] {
			return fmt.Errorf("duplicate PCI address: %s (used by multiple interfaces)", iface.PCI)
		}
		seenPCIs[iface.PCI] = true

		// Validate interface name format (ge-X/Y/Z or xe-X/Y/Z)
		if !isValidInterfaceName(iface.Name) {
			return fmt.Errorf("interface %d: invalid name format: %s (expected format: ge-X/Y/Z or xe-X/Y/Z)",
				i, iface.Name)
		}
	}

	return nil
}

// isValidInterfaceName checks if the interface name follows Junos-style naming
func isValidInterfaceName(name string) bool {
	// Match patterns like: ge-0/0/0, xe-1/2/3
	pattern := regexp.MustCompile(`^(ge|xe)-\d+/\d+/\d+$`)
	return pattern.MatchString(name)
}
