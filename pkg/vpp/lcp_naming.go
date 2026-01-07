package vpp

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
)

const (
	// MaxLinuxIfNameLen is the maximum length for Linux interface names (IFNAMSIZ - 1)
	MaxLinuxIfNameLen = 15

	// hashSuffixLen is the length of the hash suffix for collision avoidance
	hashSuffixLen = 5
)

var (
	// junosIfNamePattern matches Junos interface names like ge-0/0/0, xe-1/2/3, et-4/5/6
	junosIfNamePattern = regexp.MustCompile(`^([a-z]+)-(\d+)/(\d+)/(\d+)(?:\.(\d+))?$`)
)

// ConvertJunosToLinuxName converts a Junos interface name to Linux format.
// Examples:
//
//	ge-0/0/0     → ge0-0-0
//	xe-1/2/3     → xe1-2-3
//	et-0/1/2     → et0-1-2
//	ge-0/0/0.10  → ge0-0-0v10
//	ge-0/0/10    → ge0-0-10
//
// For names that would exceed 15 characters or have potential collisions,
// a deterministic hash suffix is appended.
func ConvertJunosToLinuxName(junosName string) (string, error) {
	if junosName == "" {
		return "", fmt.Errorf("empty Junos interface name")
	}

	// Parse Junos interface name
	matches := junosIfNamePattern.FindStringSubmatch(junosName)
	if matches == nil {
		return "", fmt.Errorf("invalid Junos interface name format: %s (expected format: ge-0/0/0 or ge-0/0/0.10)", junosName)
	}

	ifType := matches[1] // ge, xe, et, etc.
	fpc := matches[2]    // FPC (Flexible PIC Concentrator)
	pic := matches[3]    // PIC (Physical Interface Card)
	port := matches[4]   // Port number
	vlan := matches[5]   // VLAN (optional, from .N)

	// Basic conversion with separators to avoid ambiguity
	// ge-0/0/0 → ge0-0-0 (prevents ge-1/11/1 vs ge-11/1/1 collision)
	baseLinuxName := fmt.Sprintf("%s%s-%s-%s", ifType, fpc, pic, port)

	// Add VLAN suffix if present
	// ge-0/0/0.10 → ge0-0-0v10
	if vlan != "" {
		baseLinuxName = fmt.Sprintf("%sv%s", baseLinuxName, vlan)
	}

	// Check length
	if len(baseLinuxName) <= MaxLinuxIfNameLen {
		return baseLinuxName, nil
	}

	// Name too long - use hash suffix
	// Truncate and add hash to ensure uniqueness
	return generateHashedName(junosName, ifType)
}

// generateHashedName creates a Linux interface name with a hash suffix
// to handle long names or potential collisions.
// Format: <prefix><hash> (max 15 chars)
// Example: ge-0/0/100 → ge0a3f8 (if base name would be too long)
func generateHashedName(junosName, prefix string) (string, error) {
	// Generate deterministic hash from full Junos name
	hash := sha256.Sum256([]byte(junosName))

	// Encode to base64 and take first hashSuffixLen chars
	// Use URL-safe encoding (no special chars in interface names)
	hashStr := base64.RawURLEncoding.EncodeToString(hash[:])
	hashSuffix := strings.ToLower(hashStr[:hashSuffixLen])

	// Determine how many chars we have for prefix
	maxPrefixLen := MaxLinuxIfNameLen - hashSuffixLen

	// Truncate prefix if needed
	if len(prefix) > maxPrefixLen {
		prefix = prefix[:maxPrefixLen]
	}

	linuxName := prefix + hashSuffix

	if len(linuxName) > MaxLinuxIfNameLen {
		return "", fmt.Errorf("generated linux name too long: %s (%d chars, max %d)", linuxName, len(linuxName), MaxLinuxIfNameLen)
	}

	return linuxName, nil
}

// ValidateLinuxIfName checks if a Linux interface name is valid
func ValidateLinuxIfName(name string) error {
	if name == "" {
		return fmt.Errorf("empty Linux interface name")
	}

	if len(name) > MaxLinuxIfNameLen {
		return fmt.Errorf("linux interface name too long: %s (%d chars, max %d)", name, len(name), MaxLinuxIfNameLen)
	}

	// Check for invalid characters (allow alphanumeric, dash, underscore, dot)
	validNamePattern := regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	if !validNamePattern.MatchString(name) {
		return fmt.Errorf("linux interface name contains invalid characters: %s", name)
	}

	return nil
}

// IsJunosInterfaceName checks if a string matches Junos interface naming convention
func IsJunosInterfaceName(name string) bool {
	return junosIfNamePattern.MatchString(name)
}

// ParseJunosInterfaceName extracts components from a Junos interface name
type JunosInterfaceComponents struct {
	Type string // Interface type (ge, xe, et, etc.)
	FPC  string // FPC number
	PIC  string // PIC number
	Port string // Port number
	VLAN string // VLAN ID (empty if not a subinterface)
}

// ParseJunosInterfaceName parses a Junos interface name into its components
func ParseJunosInterfaceName(junosName string) (*JunosInterfaceComponents, error) {
	matches := junosIfNamePattern.FindStringSubmatch(junosName)
	if matches == nil {
		return nil, fmt.Errorf("invalid Junos interface name format: %s", junosName)
	}

	return &JunosInterfaceComponents{
		Type: matches[1],
		FPC:  matches[2],
		PIC:  matches[3],
		Port: matches[4],
		VLAN: matches[5],
	}, nil
}
