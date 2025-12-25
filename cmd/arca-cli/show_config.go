package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
)

// cmdShowConfiguration displays full configuration or filtered sections
func cmdShowConfiguration(ctx context.Context, args []string, f *flags) int {
	debugLog(f, "Reading configuration from: %s", f.configPath)

	// Read configuration file
	lines, err := readConfigFile(f.configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to read configuration file: %v\n", err)
		return ExitOperationError
	}

	// Determine filter type
	var filteredLines []string
	if len(args) == 0 {
		// Show all configuration
		filteredLines = lines
		debugLog(f, "Showing full configuration (%d lines)", len(lines))
	} else if len(args) == 1 {
		switch args[0] {
		case "interfaces":
			// Filter interface configuration
			filteredLines = FilterConfigLines(lines, "set interfaces")
			debugLog(f, "Showing interface configuration (%d lines)", len(filteredLines))
		case "protocols":
			// Filter routing protocol configuration
			filteredLines = FilterConfigByPrefixes(lines, []string{
				"set protocols",
				"set routing-options",
			})
			debugLog(f, "Showing protocol configuration (%d lines)", len(filteredLines))
		default:
			fmt.Fprintf(os.Stderr, "Error: unknown configuration section '%s'\n", args[0])
			fmt.Fprintf(os.Stderr, "Valid sections: interfaces, protocols\n")
			return ExitUsageError
		}
	} else {
		fmt.Fprintf(os.Stderr, "Error: 'show configuration' accepts at most one argument\n\n")
		showUsage()
		return ExitUsageError
	}

	// Display configuration in set format
	if err := FormatSetConfig(os.Stdout, filteredLines); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to format configuration: %v\n", err)
		return ExitOperationError
	}

	return ExitSuccess
}

// readConfigFile reads configuration file line by line
// This uses line-based filtering approach to preserve unknown/unsupported set commands
func readConfigFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open file: %w", err)
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		// Skip empty lines and comments
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lines = append(lines, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}

	return lines, nil
}
