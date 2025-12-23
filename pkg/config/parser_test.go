package config

import (
	"strings"
	"testing"
)

func TestParser_SystemHostName(t *testing.T) {
	input := `set system host-name arca-router-01`

	parser := NewParser(strings.NewReader(input))
	config, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if config.System == nil {
		t.Fatal("System is nil")
	}

	if config.System.HostName != "arca-router-01" {
		t.Errorf("HostName = %q, want %q", config.System.HostName, "arca-router-01")
	}
}

func TestParser_InterfaceDescription(t *testing.T) {
	input := `set interfaces ge-0/0/0 description "WAN Uplink to ISP"`

	parser := NewParser(strings.NewReader(input))
	config, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	iface, ok := config.Interfaces["ge-0/0/0"]
	if !ok {
		t.Fatal("Interface ge-0/0/0 not found")
	}

	want := "WAN Uplink to ISP"
	if iface.Description != want {
		t.Errorf("Description = %q, want %q", iface.Description, want)
	}
}

func TestParser_InterfaceAddress(t *testing.T) {
	input := `set interfaces ge-0/0/0 unit 0 family inet address 198.51.100.1/30`

	parser := NewParser(strings.NewReader(input))
	config, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	iface, ok := config.Interfaces["ge-0/0/0"]
	if !ok {
		t.Fatal("Interface ge-0/0/0 not found")
	}

	unit, ok := iface.Units[0]
	if !ok {
		t.Fatal("Unit 0 not found")
	}

	family, ok := unit.Family["inet"]
	if !ok {
		t.Fatal("Family inet not found")
	}

	if len(family.Addresses) != 1 {
		t.Fatalf("Addresses count = %d, want 1", len(family.Addresses))
	}

	want := "198.51.100.1/30"
	if family.Addresses[0] != want {
		t.Errorf("Address = %q, want %q", family.Addresses[0], want)
	}
}

func TestParser_MultipleStatements(t *testing.T) {
	input := `set system host-name router-01
set interfaces ge-0/0/0 description "WAN Interface"
set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.1/24
set interfaces ge-0/0/1 description "LAN Interface"
set interfaces ge-0/0/1 unit 0 family inet address 10.0.0.1/8`

	parser := NewParser(strings.NewReader(input))
	config, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Check system hostname
	if config.System.HostName != "router-01" {
		t.Errorf("HostName = %q, want %q", config.System.HostName, "router-01")
	}

	// Check first interface
	iface0, ok := config.Interfaces["ge-0/0/0"]
	if !ok {
		t.Fatal("Interface ge-0/0/0 not found")
	}
	if iface0.Description != "WAN Interface" {
		t.Errorf("Interface 0 description = %q, want %q", iface0.Description, "WAN Interface")
	}
	if iface0.Units[0].Family["inet"].Addresses[0] != "192.168.1.1/24" {
		t.Errorf("Interface 0 address mismatch")
	}

	// Check second interface
	iface1, ok := config.Interfaces["ge-0/0/1"]
	if !ok {
		t.Fatal("Interface ge-0/0/1 not found")
	}
	if iface1.Description != "LAN Interface" {
		t.Errorf("Interface 1 description = %q, want %q", iface1.Description, "LAN Interface")
	}
	if iface1.Units[0].Family["inet"].Addresses[0] != "10.0.0.1/8" {
		t.Errorf("Interface 1 address mismatch")
	}
}

func TestParser_WithComments(t *testing.T) {
	input := `# System configuration
set system host-name test-router

# Interface configuration
set interfaces ge-0/0/0 description "Test Interface"
# Set IP address
set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.1/24`

	parser := NewParser(strings.NewReader(input))
	config, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if config.System.HostName != "test-router" {
		t.Errorf("HostName = %q, want %q", config.System.HostName, "test-router")
	}

	iface, ok := config.Interfaces["ge-0/0/0"]
	if !ok {
		t.Fatal("Interface not found")
	}

	if iface.Description != "Test Interface" {
		t.Errorf("Description mismatch")
	}
}

func TestParser_MultipleAddresses(t *testing.T) {
	input := `set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.1/24
set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.2/24`

	parser := NewParser(strings.NewReader(input))
	config, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	iface := config.Interfaces["ge-0/0/0"]
	family := iface.Units[0].Family["inet"]

	if len(family.Addresses) != 2 {
		t.Fatalf("Addresses count = %d, want 2", len(family.Addresses))
	}

	if family.Addresses[0] != "192.168.1.1/24" {
		t.Errorf("Address[0] = %q, want %q", family.Addresses[0], "192.168.1.1/24")
	}
	if family.Addresses[1] != "192.168.1.2/24" {
		t.Errorf("Address[1] = %q, want %q", family.Addresses[1], "192.168.1.2/24")
	}
}

func TestParser_ErrorCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "missing set keyword",
			input: "interfaces ge-0/0/0 description test",
		},
		{
			name:  "invalid keyword",
			input: "set invalid-keyword value",
		},
		{
			name:  "missing interface name",
			input: "set interfaces description test",
		},
		{
			name:  "missing unit number",
			input: "set interfaces ge-0/0/0 unit family inet address 192.168.1.1/24",
		},
		{
			name:  "missing family keyword",
			input: "set interfaces ge-0/0/0 unit 0 inet address 192.168.1.1/24",
		},
		{
			name:  "missing address keyword",
			input: "set interfaces ge-0/0/0 unit 0 family inet 192.168.1.1/24",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser(strings.NewReader(tt.input))
			_, err := parser.Parse()
			if err == nil {
				t.Errorf("Parse() expected error, got nil")
			}
		})
	}
}

func TestParser_Empty(t *testing.T) {
	input := ``

	parser := NewParser(strings.NewReader(input))
	config, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if config == nil {
		t.Fatal("Config is nil")
	}

	if len(config.Interfaces) != 0 {
		t.Errorf("Expected empty config, got %d interfaces", len(config.Interfaces))
	}
}

func TestParser_NewlineBoundary(t *testing.T) {
	// This should fail because arguments span multiple lines
	input := `set system host-name
router-01`

	parser := NewParser(strings.NewReader(input))
	_, err := parser.Parse()
	if err == nil {
		t.Error("Parse() expected error for arguments spanning multiple lines, got nil")
	}
}

func TestParser_UnexpectedCharacter(t *testing.T) {
	// Test that lexer errors are properly propagated
	// Using $$ at the beginning to trigger lexer error immediately
	input := "$$invalid"

	parser := NewParser(strings.NewReader(input))
	_, err := parser.Parse()
	if err == nil {
		t.Error("Parse() expected error for unexpected character, got nil")
	}

	// Check that the error message mentions "unexpected character" or "Lexer error"
	errMsg := err.Error()
	if !strings.Contains(errMsg, "unexpected character") && !strings.Contains(errMsg, "Lexer error") {
		t.Errorf("Error message should mention lexer error, got: %s", errMsg)
	}
}

func TestParser_OnlyComments(t *testing.T) {
	input := `# Comment 1
# Comment 2
# Comment 3`

	parser := NewParser(strings.NewReader(input))
	config, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if len(config.Interfaces) != 0 {
		t.Errorf("Expected empty config, got %d interfaces", len(config.Interfaces))
	}
}
