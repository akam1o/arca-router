package netconf

import (
	"fmt"
	"strings"

	"github.com/akam1o/arca-router/pkg/config"
)

// ConfigToText converts config.Config to text format (set commands)
// This implements Phase 2 Step 4: Datastore Bridge Layer
func ConfigToText(cfg *config.Config) (string, error) {
	return config.ToSetCommands(cfg), nil
}

// TextToConfig converts text format (set commands) to config.Config
// This implements Phase 2 Step 4: Datastore Bridge Layer
func TextToConfig(text string) (*config.Config, error) {
	if text == "" {
		return config.NewConfig(), nil
	}

	// Use existing parser
	reader := strings.NewReader(text)
	parser := config.NewParser(reader)
	cfg, err := parser.Parse()
	if err != nil {
		return nil, fmt.Errorf("failed to parse config text: %w", err)
	}

	return cfg, nil
}
