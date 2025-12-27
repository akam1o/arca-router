// Package cli provides configuration command implementations
package cli

import (
	"context"
	"fmt"
	"strings"
)

// SetCommandWithPath executes a 'set' command with hierarchy path
func (s *Session) SetCommandWithPath(ctx context.Context, args []string) error {
	if s.mode != ModeConfiguration {
		return fmt.Errorf("not in configuration mode")
	}

	// Parse with current configPath
	setLine, err := ParseSetCommand(args, s.configPath)
	if err != nil {
		return err
	}

	candidate, err := s.ds.GetCandidate(ctx, s.id)
	if err != nil {
		return fmt.Errorf("failed to get candidate: %w", err)
	}

	// Append new line
	updatedText := candidate.ConfigText
	if updatedText == "" {
		updatedText = setLine
	} else {
		updatedText += "\n" + setLine
	}

	return s.ds.SaveCandidate(ctx, s.id, updatedText)
}

// DeleteCommandWithPath executes a 'delete' command with hierarchy path
// Deletes all lines that match the prefix
func (s *Session) DeleteCommandWithPath(ctx context.Context, args []string) error {
	if s.mode != ModeConfiguration {
		return fmt.Errorf("not in configuration mode")
	}

	// Parse with current configPath
	deletePrefix, err := ParseDeleteCommand(args, s.configPath)
	if err != nil {
		return err
	}

	candidate, err := s.ds.GetCandidate(ctx, s.id)
	if err != nil {
		return fmt.Errorf("failed to get candidate: %w", err)
	}

	// Remove all lines matching the prefix
	lines := strings.Split(candidate.ConfigText, "\n")
	var newLines []string
	deletedCount := 0

	for _, line := range lines {
		if line == "" {
			continue
		}
		if MatchesPrefix(line, deletePrefix) {
			deletedCount++
			continue
		}
		newLines = append(newLines, line)
	}

	if deletedCount == 0 {
		return fmt.Errorf("no matching configuration found")
	}

	return s.ds.SaveCandidate(ctx, s.id, strings.Join(newLines, "\n"))
}

// ShowConfigCommand displays configuration (candidate or running)
func (s *Session) ShowConfigCommand(ctx context.Context) (string, error) {
	if s.mode == ModeConfiguration {
		candidate, err := s.ds.GetCandidate(ctx, s.id)
		if err != nil {
			return "", fmt.Errorf("failed to get candidate: %w", err)
		}
		return candidate.ConfigText, nil
	}

	running, err := s.ds.GetRunning(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get running: %w", err)
	}
	return running.ConfigText, nil
}

// GetConfigPath returns the current configuration path as a string
func (s *Session) GetConfigPath() string {
	if len(s.configPath) == 0 {
		return ""
	}
	return strings.Join(s.configPath, " ")
}
