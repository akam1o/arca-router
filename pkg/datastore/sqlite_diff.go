package datastore

import (
	"context"
	"fmt"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// CompareCandidateRunning generates a diff between candidate and running configurations.
func (ds *sqliteDatastore) CompareCandidateRunning(ctx context.Context, sessionID string) (*DiffResult, error) {
	// Get candidate config
	candidate, err := ds.GetCandidate(ctx, sessionID)
	if err != nil {
		return nil, err // Already wrapped
	}

	// Get running config
	running, err := ds.GetRunning(ctx)
	if err != nil {
		// If no running config exists, treat it as empty string
		if dsErr, ok := err.(*Error); ok && dsErr.Code == ErrCodeNotFound {
			return compareConfigs("", candidate.ConfigText), nil
		}
		return nil, err
	}

	return compareConfigs(running.ConfigText, candidate.ConfigText), nil
}

// CompareCommits generates a diff between two commits.
func (ds *sqliteDatastore) CompareCommits(ctx context.Context, commitID1, commitID2 string) (*DiffResult, error) {
	// Get first commit
	commit1, err := ds.GetCommit(ctx, commitID1)
	if err != nil {
		return nil, err // Already wrapped
	}

	// Get second commit
	commit2, err := ds.GetCommit(ctx, commitID2)
	if err != nil {
		return nil, err // Already wrapped
	}

	return compareConfigs(commit1.ConfigText, commit2.ConfigText), nil
}

// compareConfigs performs the actual diff operation between two config texts.
// It generates a simplified line-based diff output suitable for configuration display.
func compareConfigs(oldText, newText string) *DiffResult {
	// Normalize line endings
	oldText = normalizeLineEndings(oldText)
	newText = normalizeLineEndings(newText)

	// Check if configs are identical
	if oldText == newText {
		return &DiffResult{
			DiffText:   "",
			HasChanges: false,
		}
	}

	// Split into lines for line-based comparison
	oldLines := strings.Split(oldText, "\n")
	newLines := strings.Split(newText, "\n")

	// Use diffmatchpatch for line-level diff
	dmp := diffmatchpatch.New()

	// Convert lines to unique strings for diff algorithm
	oldLineText := strings.Join(oldLines, "\n")
	newLineText := strings.Join(newLines, "\n")

	diffs := dmp.DiffMain(oldLineText, newLineText, false)
	diffs = dmp.DiffCleanupSemantic(diffs)

	// Generate simplified diff output with +/- prefixes
	diffText := generateSimplifiedDiff(diffs)

	return &DiffResult{
		DiffText:   diffText,
		HasChanges: true,
	}
}

// normalizeLineEndings converts all line endings to \n for consistent comparison.
func normalizeLineEndings(text string) string {
	// Replace \r\n with \n
	text = strings.ReplaceAll(text, "\r\n", "\n")
	// Replace remaining \r with \n
	text = strings.ReplaceAll(text, "\r", "\n")
	return text
}

// generateSimplifiedDiff converts DiffMatchPatch diffs to a simplified line-based diff.
// Output format:
//   - Lines starting with '-' are removed from old config
//   - Lines starting with '+' are added to new config
//   - Lines starting with ' ' are unchanged (context, limited to 3 lines)
func generateSimplifiedDiff(diffs []diffmatchpatch.Diff) string {
	var result strings.Builder

	for _, diff := range diffs {
		lines := strings.Split(diff.Text, "\n")

		switch diff.Type {
		case diffmatchpatch.DiffDelete:
			// Lines removed from old config
			for _, line := range lines {
				if line != "" { // Skip empty lines at diff boundaries
					result.WriteString("- ")
					result.WriteString(line)
					result.WriteString("\n")
				}
			}

		case diffmatchpatch.DiffInsert:
			// Lines added to new config
			for _, line := range lines {
				if line != "" {
					result.WriteString("+ ")
					result.WriteString(line)
					result.WriteString("\n")
				}
			}

		case diffmatchpatch.DiffEqual:
			// Context lines (unchanged)
			// Only show a few context lines to keep output concise
			contextLines := 3
			if len(lines) > contextLines*2 {
				// Show first N lines
				for i := 0; i < contextLines && i < len(lines); i++ {
					if lines[i] != "" {
						result.WriteString("  ")
						result.WriteString(lines[i])
						result.WriteString("\n")
					}
				}

				// Add ellipsis
				result.WriteString("  ...\n")

				// Show last N lines
				for i := len(lines) - contextLines; i < len(lines); i++ {
					if i >= 0 && lines[i] != "" {
						result.WriteString("  ")
						result.WriteString(lines[i])
						result.WriteString("\n")
					}
				}
			} else {
				// Show all lines if total is small
				for _, line := range lines {
					if line != "" {
						result.WriteString("  ")
						result.WriteString(line)
						result.WriteString("\n")
					}
				}
			}
		}
	}

	return result.String()
}

// FormatJunosStyleDiff converts unified diff to Junos-style output.
// This is a helper function for CLI output compatibility.
// Junos style uses "set" and "delete" commands instead of +/-.
func FormatJunosStyleDiff(diffText string) string {
	lines := strings.Split(diffText, "\n")
	var result strings.Builder

	for _, line := range lines {
		if len(line) == 0 {
			continue
		}

		prefix := line[0:1]
		content := ""
		if len(line) > 2 {
			content = line[2:]
		}

		switch prefix {
		case "-":
			// Deletion: show as "delete" command
			result.WriteString(fmt.Sprintf("[delete] %s\n", content))
		case "+":
			// Addition: show as "set" command
			result.WriteString(fmt.Sprintf("[set] %s\n", content))
		case " ":
			// Context: show as-is
			result.WriteString(fmt.Sprintf("  %s\n", content))
		}
	}

	return result.String()
}
