package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// FormatTable formats data as a table with aligned columns
func FormatTable(w io.Writer, headers []string, rows [][]string) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	// Print headers
	if _, err := fmt.Fprintln(tw, strings.Join(headers, "\t")); err != nil {
		return err
	}

	// Print separator
	sep := make([]string, len(headers))
	for i := range headers {
		sep[i] = strings.Repeat("-", len(headers[i]))
	}
	if _, err := fmt.Fprintln(tw, strings.Join(sep, "\t")); err != nil {
		return err
	}

	// Print rows
	for _, row := range rows {
		if _, err := fmt.Fprintln(tw, strings.Join(row, "\t")); err != nil {
			return err
		}
	}

	// Return flush error
	return tw.Flush()
}

// FormatSetConfig formats configuration lines in set command format
// This is a pass-through for displaying configuration as-is
func FormatSetConfig(w io.Writer, lines []string) error {
	for _, line := range lines {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

// FilterConfigLines filters configuration lines by prefix
// prefix: e.g., "set interfaces", "set protocols", "set routing-options"
func FilterConfigLines(lines []string, prefix string) []string {
	var filtered []string
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			filtered = append(filtered, line)
		}
	}
	return filtered
}

// FilterConfigByPrefixes filters configuration lines by multiple prefixes
func FilterConfigByPrefixes(lines []string, prefixes []string) []string {
	var filtered []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		for _, prefix := range prefixes {
			if strings.HasPrefix(trimmed, prefix) {
				filtered = append(filtered, line)
				break
			}
		}
	}
	return filtered
}
