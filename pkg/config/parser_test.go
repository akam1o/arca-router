package config

import (
	"os"
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

// Test routing-options parsing
func TestParser_RoutingOptions(t *testing.T) {
	input := `set routing-options autonomous-system 65001
set routing-options router-id 10.0.1.1
set routing-options static route 0.0.0.0/0 next-hop 192.168.1.254
set routing-options static route 10.0.0.0/8 next-hop 10.0.0.1 distance 10`

	parser := NewParser(strings.NewReader(input))
	config, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if config.RoutingOptions == nil {
		t.Fatal("RoutingOptions is nil")
	}

	// Check AS number
	if config.RoutingOptions.AutonomousSystem != 65001 {
		t.Errorf("Expected AS 65001, got %d", config.RoutingOptions.AutonomousSystem)
	}

	// Check router-id
	if config.RoutingOptions.RouterID != "10.0.1.1" {
		t.Errorf("Expected router-id 10.0.1.1, got %s", config.RoutingOptions.RouterID)
	}

	// Check static routes
	if len(config.RoutingOptions.StaticRoutes) != 2 {
		t.Fatalf("Expected 2 static routes, got %d", len(config.RoutingOptions.StaticRoutes))
	}

	// Check default route
	if config.RoutingOptions.StaticRoutes[0].Prefix != "0.0.0.0/0" {
		t.Errorf("Expected prefix 0.0.0.0/0, got %s", config.RoutingOptions.StaticRoutes[0].Prefix)
	}
	if config.RoutingOptions.StaticRoutes[0].NextHop != "192.168.1.254" {
		t.Errorf("Expected next-hop 192.168.1.254, got %s", config.RoutingOptions.StaticRoutes[0].NextHop)
	}

	// Check route with distance
	if config.RoutingOptions.StaticRoutes[1].Distance != 10 {
		t.Errorf("Expected distance 10, got %d", config.RoutingOptions.StaticRoutes[1].Distance)
	}
}

// Test BGP parsing
func TestParser_BGP(t *testing.T) {
	input := `set routing-options autonomous-system 65001
set protocols bgp group IBGP type internal
set protocols bgp group IBGP neighbor 10.0.1.2 peer-as 65001
set protocols bgp group IBGP neighbor 10.0.1.2 description "Internal Peer"
set protocols bgp group IBGP neighbor 10.0.1.2 local-address 10.0.1.1
set protocols bgp group EBGP type external
set protocols bgp group EBGP neighbor 10.0.2.2 peer-as 65002`

	parser := NewParser(strings.NewReader(input))
	config, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if config.Protocols == nil {
		t.Fatal("Protocols is nil")
	}

	if config.Protocols.BGP == nil {
		t.Fatal("BGP is nil")
	}

	// Check groups
	if len(config.Protocols.BGP.Groups) != 2 {
		t.Fatalf("Expected 2 BGP groups, got %d", len(config.Protocols.BGP.Groups))
	}

	// Check IBGP group
	ibgp := config.Protocols.BGP.Groups["IBGP"]
	if ibgp == nil {
		t.Fatal("IBGP group is nil")
	}
	if ibgp.Type != "internal" {
		t.Errorf("Expected IBGP type internal, got %s", ibgp.Type)
	}
	if len(ibgp.Neighbors) != 1 {
		t.Fatalf("Expected 1 IBGP neighbor, got %d", len(ibgp.Neighbors))
	}

	// Check IBGP neighbor
	neighbor := ibgp.Neighbors["10.0.1.2"]
	if neighbor == nil {
		t.Fatal("IBGP neighbor 10.0.1.2 is nil")
	}
	if neighbor.PeerAS != 65001 {
		t.Errorf("Expected peer-as 65001, got %d", neighbor.PeerAS)
	}
	if neighbor.Description != "Internal Peer" {
		t.Errorf("Expected description 'Internal Peer', got %s", neighbor.Description)
	}
	if neighbor.LocalAddress != "10.0.1.1" {
		t.Errorf("Expected local-address 10.0.1.1, got %s", neighbor.LocalAddress)
	}

	// Check EBGP group
	ebgp := config.Protocols.BGP.Groups["EBGP"]
	if ebgp == nil {
		t.Fatal("EBGP group is nil")
	}
	if ebgp.Type != "external" {
		t.Errorf("Expected EBGP type external, got %s", ebgp.Type)
	}
}

// Test OSPF parsing
func TestParser_OSPF(t *testing.T) {
	input := `set routing-options router-id 10.0.1.1
set protocols ospf router-id 10.0.1.2
set protocols ospf area 0.0.0.0 interface ge-0/0/0
set protocols ospf area 0.0.0.0 interface ge-0/0/1 passive
set protocols ospf area 0.0.0.0 interface ge-0/0/1 metric 100
set protocols ospf area 0.0.0.0 interface ge-0/0/1 priority 1`

	parser := NewParser(strings.NewReader(input))
	config, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if config.Protocols == nil {
		t.Fatal("Protocols is nil")
	}

	if config.Protocols.OSPF == nil {
		t.Fatal("OSPF is nil")
	}

	// Check OSPF router-id
	if config.Protocols.OSPF.RouterID != "10.0.1.2" {
		t.Errorf("Expected OSPF router-id 10.0.1.2, got %s", config.Protocols.OSPF.RouterID)
	}

	// Check areas
	if len(config.Protocols.OSPF.Areas) != 1 {
		t.Fatalf("Expected 1 OSPF area, got %d", len(config.Protocols.OSPF.Areas))
	}

	// Check area 0
	area := config.Protocols.OSPF.Areas["0.0.0.0"]
	if area == nil {
		t.Fatal("OSPF area 0.0.0.0 is nil")
	}

	// Check interfaces
	if len(area.Interfaces) != 2 {
		t.Fatalf("Expected 2 OSPF interfaces, got %d", len(area.Interfaces))
	}

	// Check ge-0/0/0
	if0 := area.Interfaces["ge-0/0/0"]
	if if0 == nil {
		t.Fatal("OSPF interface ge-0/0/0 is nil")
	}
	if if0.Passive {
		t.Error("Expected ge-0/0/0 to be non-passive")
	}

	// Check ge-0/0/1
	if1 := area.Interfaces["ge-0/0/1"]
	if if1 == nil {
		t.Fatal("OSPF interface ge-0/0/1 is nil")
	}
	if !if1.Passive {
		t.Error("Expected ge-0/0/1 to be passive")
	}
	if if1.Metric != 100 {
		t.Errorf("Expected metric 100, got %d", if1.Metric)
	}
	if if1.Priority != 1 {
		t.Errorf("Expected priority 1, got %d", if1.Priority)
	}
}

// Test complete Phase 2 configuration
func TestParser_Phase2Complete(t *testing.T) {
	input := `set system host-name router1
set interfaces ge-0/0/0 description "Uplink"
set interfaces ge-0/0/0 unit 0 family inet address 10.0.1.1/24
set routing-options autonomous-system 65001
set routing-options router-id 10.0.1.1
set routing-options static route 0.0.0.0/0 next-hop 10.0.1.254
set protocols bgp group IBGP type internal
set protocols bgp group IBGP neighbor 10.0.1.2 peer-as 65001
set protocols ospf area 0.0.0.0 interface ge-0/0/0`

	parser := NewParser(strings.NewReader(input))
	config, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Validate system
	if config.System == nil || config.System.HostName != "router1" {
		t.Error("System hostname not parsed correctly")
	}

	// Validate interfaces
	if len(config.Interfaces) != 1 {
		t.Errorf("Expected 1 interface, got %d", len(config.Interfaces))
	}

	// Validate routing options
	if config.RoutingOptions == nil {
		t.Fatal("RoutingOptions is nil")
	}
	if config.RoutingOptions.AutonomousSystem != 65001 {
		t.Errorf("Expected AS 65001, got %d", config.RoutingOptions.AutonomousSystem)
	}
	if len(config.RoutingOptions.StaticRoutes) != 1 {
		t.Errorf("Expected 1 static route, got %d", len(config.RoutingOptions.StaticRoutes))
	}

	// Validate BGP
	if config.Protocols == nil || config.Protocols.BGP == nil {
		t.Fatal("BGP is nil")
	}
	if len(config.Protocols.BGP.Groups) != 1 {
		t.Errorf("Expected 1 BGP group, got %d", len(config.Protocols.BGP.Groups))
	}

	// Validate OSPF
	if config.Protocols.OSPF == nil {
		t.Fatal("OSPF is nil")
	}
	if len(config.Protocols.OSPF.Areas) != 1 {
		t.Errorf("Expected 1 OSPF area, got %d", len(config.Protocols.OSPF.Areas))
	}
}

// Test parsing errors
func TestParser_RoutingErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "duplicate static route",
			input: `set routing-options static route 0.0.0.0/0 next-hop 10.0.0.1
set routing-options static route 0.0.0.0/0 next-hop 10.0.0.2`,
		},
		{
			name:  "invalid AS number",
			input: `set routing-options autonomous-system invalid`,
		},
		{
			name:  "BGP without group name",
			input: `set protocols bgp type internal`,
		},
		{
			name:  "OSPF without area",
			input: `set protocols ospf interface ge-0/0/0`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser(strings.NewReader(tt.input))
			_, err := parser.Parse()
			if err == nil {
				t.Errorf("Expected error for %s, got nil", tt.name)
			}
		})
	}
}

// Test Phase 2 sample configuration file
func TestParser_Phase2SampleFile(t *testing.T) {
	// Read the sample configuration file
	file, err := os.Open("../../examples/arca-phase2.conf")
	if err != nil {
		t.Fatalf("Failed to open arca-phase2.conf: %v", err)
	}
	defer file.Close()

	parser := NewParser(file)
	config, err := parser.Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Validate system
	if config.System == nil || config.System.HostName != "router1" {
		t.Error("Expected hostname 'router1'")
	}

	// Validate interfaces
	if len(config.Interfaces) != 3 {
		t.Errorf("Expected 3 interfaces, got %d", len(config.Interfaces))
	}

	// Validate routing options
	if config.RoutingOptions == nil {
		t.Fatal("RoutingOptions is nil")
	}
	if config.RoutingOptions.AutonomousSystem != 65001 {
		t.Errorf("Expected AS 65001, got %d", config.RoutingOptions.AutonomousSystem)
	}
	if config.RoutingOptions.RouterID != "10.0.1.1" {
		t.Errorf("Expected router-id 10.0.1.1, got %s", config.RoutingOptions.RouterID)
	}
	if len(config.RoutingOptions.StaticRoutes) != 2 {
		t.Errorf("Expected 2 static routes, got %d", len(config.RoutingOptions.StaticRoutes))
	}

	// Validate BGP
	if config.Protocols == nil || config.Protocols.BGP == nil {
		t.Fatal("BGP is nil")
	}
	if len(config.Protocols.BGP.Groups) != 2 {
		t.Errorf("Expected 2 BGP groups, got %d", len(config.Protocols.BGP.Groups))
	}

	// Validate IBGP group
	ibgp := config.Protocols.BGP.Groups["IBGP"]
	if ibgp == nil {
		t.Fatal("IBGP group not found")
	}
	if ibgp.Type != "internal" {
		t.Errorf("Expected IBGP type internal, got %s", ibgp.Type)
	}

	// Validate EBGP group
	ebgp := config.Protocols.BGP.Groups["EBGP"]
	if ebgp == nil {
		t.Fatal("EBGP group not found")
	}
	if ebgp.Type != "external" {
		t.Errorf("Expected EBGP type external, got %s", ebgp.Type)
	}

	// Validate OSPF
	if config.Protocols.OSPF == nil {
		t.Fatal("OSPF is nil")
	}
	if config.Protocols.OSPF.RouterID != "10.0.1.1" {
		t.Errorf("Expected OSPF router-id 10.0.1.1, got %s", config.Protocols.OSPF.RouterID)
	}
	if len(config.Protocols.OSPF.Areas) != 1 {
		t.Errorf("Expected 1 OSPF area, got %d", len(config.Protocols.OSPF.Areas))
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		t.Errorf("Validation failed: %v", err)
	}
}
