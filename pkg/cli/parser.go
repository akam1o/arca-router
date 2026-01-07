// Package cli provides CLI command parsing utilities
package cli

import (
	"fmt"
	"strings"
)

// ParseSetCommand parses a set command with hierarchy context
// Returns a normalized "set" line that can be stored in candidate config
func ParseSetCommand(args []string, basePath []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("'set' requires arguments")
	}

	// Combine basePath (from 'edit') with args
	// Copy basePath to avoid mutating the caller's slice
	fullPath := make([]string, 0, len(basePath)+len(args))
	fullPath = append(fullPath, basePath...)
	fullPath = append(fullPath, args...)

	// Normalize the path
	normalized := NormalizeConfigPath(fullPath)

	return "set " + normalized, nil
}

// ParseDeleteCommand parses a delete command with hierarchy context
// Returns a prefix pattern for matching lines to delete
func ParseDeleteCommand(args []string, basePath []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("'delete' requires arguments")
	}

	// Combine basePath with args
	// Copy basePath to avoid mutating the caller's slice
	fullPath := make([]string, 0, len(basePath)+len(args))
	fullPath = append(fullPath, basePath...)
	fullPath = append(fullPath, args...)

	// Normalize the path
	normalized := NormalizeConfigPath(fullPath)

	return "set " + normalized, nil
}

// NormalizeConfigPath converts a path slice to a normalized string
// Example: ["interfaces", "ge-0/0/0", "unit", "0"] -> "interfaces ge-0/0/0 unit 0"
func NormalizeConfigPath(path []string) string {
	if len(path) == 0 {
		return ""
	}

	// Join with spaces, preserving quoted strings
	var result []string
	for _, token := range path {
		// If token contains spaces and not already quoted, quote it
		if strings.Contains(token, " ") && !strings.HasPrefix(token, "\"") {
			result = append(result, fmt.Sprintf("\"%s\"", token))
		} else {
			result = append(result, token)
		}
	}

	return strings.Join(result, " ")
}

// TokenizeCommand splits a command line into tokens, respecting quotes
// Example: `set description "test interface"` -> ["set", "description", "test interface"], nil
// Returns error if quotes are unmatched
// Treats both spaces and tabs as whitespace delimiters
func TokenizeCommand(line string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	inQuote := false

	for i := 0; i < len(line); i++ {
		char := line[i]

		switch char {
		case '"':
			inQuote = !inQuote
		case ' ', '\t': // Treat both space and tab as whitespace
			if inQuote {
				current.WriteByte(char)
			} else if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(char)
		}
	}

	if inQuote {
		return nil, fmt.Errorf("unmatched quote in command")
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens, nil
}

// MatchesPrefix checks if a config line matches a delete prefix
// with token boundary checking to avoid over-deletion
// Example: line="set interfaces ge-0/0/0 unit 0 family inet"
//
//	prefix="set interfaces ge-0/0/0"
//	returns: true
//
// Example: line="set system host-name2"
//
//	prefix="set system host-name"
//	returns: false (boundary check)
func MatchesPrefix(line, prefix string) bool {
	// Empty prefix matches everything
	if prefix == "" {
		return true
	}

	if !strings.HasPrefix(line, prefix) {
		return false
	}

	// If exact match, always true
	if line == prefix {
		return true
	}

	// Check token boundary: next character must be space or end of line
	if len(line) > len(prefix) {
		nextChar := line[len(prefix)]
		return nextChar == ' ' || nextChar == '\t'
	}

	return true
}
