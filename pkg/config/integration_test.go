package config

import (
	"os"
	"testing"
)

func TestIntegration_ParseExampleConfig(t *testing.T) {
	// Parse the example configuration file
	f, err := os.Open("../../examples/arca-router.conf")
	if err != nil {
		t.Fatalf("Failed to open examples/arca-router.conf: %v", err)
	}
	defer f.Close()

	parser := NewParser(f)
	config, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Validate the configuration
	if err := config.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	// Check system hostname
	if config.System == nil {
		t.Fatal("System config is nil")
	}
	if config.System.HostName != "router1" {
		t.Errorf("HostName = %q, want %q", config.System.HostName, "router1")
	}

	// Check interface ge-0/0/0
	iface0, ok := config.Interfaces["ge-0/0/0"]
	if !ok {
		t.Fatal("Interface ge-0/0/0 not found")
	}
	if iface0.Description != "Uplink to Core" {
		t.Errorf("ge-0/0/0 description = %q, want %q", iface0.Description, "Uplink to Core")
	}

	unit0, ok := iface0.Units[0]
	if !ok {
		t.Fatal("Unit 0 on ge-0/0/0 not found")
	}
	family0, ok := unit0.Family["inet"]
	if !ok {
		t.Fatal("Family inet on ge-0/0/0 unit 0 not found")
	}
	if len(family0.Addresses) != 1 {
		t.Fatalf("Expected 1 address on ge-0/0/0, got %d", len(family0.Addresses))
	}
	if family0.Addresses[0] != "10.0.1.1/24" {
		t.Errorf("ge-0/0/0 address = %q, want %q", family0.Addresses[0], "10.0.1.1/24")
	}

	// Check interface ge-0/0/1
	iface1, ok := config.Interfaces["ge-0/0/1"]
	if !ok {
		t.Fatal("Interface ge-0/0/1 not found")
	}
	if iface1.Description != "Internal LAN" {
		t.Errorf("ge-0/0/1 description = %q, want %q", iface1.Description, "Internal LAN")
	}

	unit1, ok := iface1.Units[0]
	if !ok {
		t.Fatal("Unit 0 on ge-0/0/1 not found")
	}
	family1, ok := unit1.Family["inet"]
	if !ok {
		t.Fatal("Family inet on ge-0/0/1 unit 0 not found")
	}
	if len(family1.Addresses) != 1 {
		t.Fatalf("Expected 1 address on ge-0/0/1, got %d", len(family1.Addresses))
	}
	if family1.Addresses[0] != "192.168.1.1/24" {
		t.Errorf("ge-0/0/1 address = %q, want %q", family1.Addresses[0], "192.168.1.1/24")
	}
}

func TestIntegration_ParseAndValidateFullWorkflow(t *testing.T) {
	input := `# Full workflow test
set system host-name test-router

# Multiple interfaces with different configurations
set interfaces ge-0/0/0 description "First Interface"
set interfaces ge-0/0/0 unit 0 family inet address 10.0.1.1/24
set interfaces ge-0/0/0 unit 0 family inet address 10.0.1.2/24

set interfaces xe-1/0/0 description "Second Interface"
set interfaces xe-1/0/0 unit 100 family inet address 172.16.0.1/16

set interfaces et-2/0/0 unit 0 family inet address 192.168.100.1/24
`

	parser := NewParser(os.Stdin)
	// Replace stdin with our test input
	r, w, _ := os.Pipe()
	oldStdin := os.Stdin
	os.Stdin = r
	go func() {
		w.Write([]byte(input))
		w.Close()
	}()

	parser = NewParser(r)
	config, err := parser.Parse()
	os.Stdin = oldStdin

	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Validate
	if err := config.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	// Verify structure
	if len(config.Interfaces) != 3 {
		t.Errorf("Expected 3 interfaces, got %d", len(config.Interfaces))
	}

	// Check first interface has 2 addresses
	iface0 := config.Interfaces["ge-0/0/0"]
	if len(iface0.Units[0].Family["inet"].Addresses) != 2 {
		t.Errorf("Expected 2 addresses on ge-0/0/0, got %d", len(iface0.Units[0].Family["inet"].Addresses))
	}

	// Check second interface has unit 100
	iface1 := config.Interfaces["xe-1/0/0"]
	if _, ok := iface1.Units[100]; !ok {
		t.Error("Expected unit 100 on xe-1/0/0")
	}

	// Check third interface exists
	if _, ok := config.Interfaces["et-2/0/0"]; !ok {
		t.Error("Expected interface et-2/0/0")
	}
}

func BenchmarkParse(b *testing.B) {
	input := `set system host-name router
set interfaces ge-0/0/0 description "Test"
set interfaces ge-0/0/0 unit 0 family inet address 192.168.1.1/24
set interfaces ge-0/0/1 unit 0 family inet address 10.0.0.1/8
`

	for i := 0; i < b.N; i++ {
		r, w, _ := os.Pipe()
		go func() {
			w.Write([]byte(input))
			w.Close()
		}()
		parser := NewParser(r)
		_, _ = parser.Parse()
	}
}
