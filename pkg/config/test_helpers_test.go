package config

import (
	"strings"
	"testing"
)

func parseSetCommands(t *testing.T, lines ...string) *Config {
	t.Helper()
	cfg, err := NewParser(strings.NewReader(strings.Join(lines, "\n"))).Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	return cfg
}

func assertSetCommandRoundTrip(t *testing.T, cfg *Config) {
	t.Helper()
	text := ToSetCommands(cfg)
	parsed, err := NewParser(strings.NewReader(text)).Parse()
	if err != nil {
		t.Fatalf("round-trip parse failed:\n%s\nerror: %v", text, err)
	}
	if got := ToSetCommands(parsed); got != text {
		t.Fatalf("round-trip mismatch\nwant:\n%s\ngot:\n%s", text, got)
	}
}
