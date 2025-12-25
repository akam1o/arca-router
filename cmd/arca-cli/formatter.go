package main

import (
	"fmt"
	"strings"
	"text/tabwriter"
	"io"
)

// FormatTable formats data as a table with aligned columns
func FormatTable(w io.Writer, headers []string, rows [][]string) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)

	// Print headers
	fmt.Fprintln(tw, strings.Join(headers, "\t"))

	// Print separator
	sep := make([]string, len(headers))
	for i := range headers {
		sep[i] = strings.Repeat("-", len(headers[i]))
	}
	fmt.Fprintln(tw, strings.Join(sep, "\t"))

	// Print rows
	for _, row := range rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}

	// Return flush error
	return tw.Flush()
}

// FormatSetConfig formats configuration lines in set command format
// This is a pass-through for displaying configuration as-is
func FormatSetConfig(w io.Writer, lines []string) error {
	for _, line := range lines {
		fmt.Fprintln(w, line)
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
