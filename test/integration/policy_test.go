package integration

import (
	"strings"
	"testing"

	"github.com/akam1o/arca-router/pkg/config"
	"github.com/akam1o/arca-router/pkg/frr"
)

// parseConfig is a helper to parse config strings
func parseConfig(t *testing.T, input string) *config.Config {
	t.Helper()
	parser := config.NewParser(strings.NewReader(input))
	cfg, err := parser.Parse()
	if err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}
	return cfg
}

// generateFRRConfig is a helper to generate FRR config string
func generateFRRConfig(t *testing.T, cfg *config.Config) string {
	t.Helper()
	frrCfg, err := frr.GenerateFRRConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to generate FRR config: %v", err)
	}
	frrConfigStr, err := frr.GenerateFRRConfigFile(frrCfg)
	if err != nil {
		t.Fatalf("Failed to generate FRR config file: %v", err)
	}
	return frrConfigStr
}

// TestPrefixListGeneration tests prefix-list generation
func TestPrefixListGeneration(t *testing.T) {
	input := `
set routing-options autonomous-system 65001
set routing-options router-id 10.0.1.1

set policy-options prefix-list PRIVATE 10.0.0.0/8
set policy-options prefix-list PRIVATE 172.16.0.0/12
set policy-options prefix-list PRIVATE 192.168.0.0/16

set policy-options prefix-list PUBLIC 8.8.8.0/24
set policy-options prefix-list PUBLIC 1.1.1.0/24
`

	cfg := parseConfig(t, input)
	frrConfig := generateFRRConfig(t, cfg)

	// Check for prefix-lists
	expected := []string{
		"ip prefix-list PRIVATE seq 10 permit 10.0.0.0/8",
		"ip prefix-list PRIVATE seq 20 permit 172.16.0.0/12",
		"ip prefix-list PRIVATE seq 30 permit 192.168.0.0/16",
		"ip prefix-list PUBLIC seq 10 permit 8.8.8.0/24",
		"ip prefix-list PUBLIC seq 20 permit 1.1.1.0/24",
	}

	for _, exp := range expected {
		if !strings.Contains(frrConfig, exp) {
			t.Errorf("Expected prefix-list entry not found: %s\nGenerated config:\n%s", exp, frrConfig)
		}
	}
}

// TestIPv6PrefixListSplitting tests IPv4/IPv6 prefix-list splitting
func TestIPv6PrefixListSplitting(t *testing.T) {
	input := `
set routing-options autonomous-system 65001
set routing-options router-id 10.0.1.1

set policy-options prefix-list MIXED 10.0.0.0/8
set policy-options prefix-list MIXED 2001:db8::/32
`

	cfg := parseConfig(t, input)
	frrConfig := generateFRRConfig(t, cfg)

	// Check for split prefix-lists
	expected := []string{
		"ip prefix-list MIXED seq 10 permit 10.0.0.0/8",
		"ipv6 prefix-list MIXED-v6 seq 10 permit 2001:db8::/32",
	}

	for _, exp := range expected {
		if !strings.Contains(frrConfig, exp) {
			t.Errorf("Expected prefix-list entry not found: %s\nGenerated config:\n%s", exp, frrConfig)
		}
	}
}

// TestPolicyStatementConversion tests policy-statement to route-map conversion
func TestPolicyStatementConversion(t *testing.T) {
	input := `
set routing-options autonomous-system 65001
set routing-options router-id 10.0.1.1

set policy-options prefix-list PRIVATE 10.0.0.0/8

set policy-options policy-statement DENY-PRIVATE term DENY from prefix-list PRIVATE
set policy-options policy-statement DENY-PRIVATE term DENY then reject

set policy-options policy-statement DENY-PRIVATE term ALLOW then accept
`

	cfg := parseConfig(t, input)
	frrConfig := generateFRRConfig(t, cfg)

	// Check for route-map entries
	expected := []string{
		"route-map DENY-PRIVATE deny 10",
		"match ip address prefix-list PRIVATE",
		"route-map DENY-PRIVATE permit 20",
	}

	for _, exp := range expected {
		if !strings.Contains(frrConfig, exp) {
			t.Errorf("Expected route-map entry not found: %s\nGenerated config:\n%s", exp, frrConfig)
		}
	}
}

// TestBGPPolicyApplication tests BGP policy application
func TestBGPPolicyApplication(t *testing.T) {
	input := `
set routing-options autonomous-system 65001
set routing-options router-id 10.0.1.1

set policy-options prefix-list CUSTOMER 10.100.0.0/16

set policy-options policy-statement IMPORT-POLICY term ALLOW from prefix-list CUSTOMER
set policy-options policy-statement IMPORT-POLICY term ALLOW then accept
set policy-options policy-statement IMPORT-POLICY term DEFAULT then reject

set policy-options policy-statement EXPORT-POLICY term DEFAULT then accept

set protocols bgp group external type external
set protocols bgp group external import IMPORT-POLICY
set protocols bgp group external export EXPORT-POLICY
set protocols bgp group external neighbor 10.0.1.2 peer-as 65002
`

	cfg := parseConfig(t, input)
	frrConfig := generateFRRConfig(t, cfg)

	// Check for BGP route-map application
	expected := []string{
		"neighbor 10.0.1.2 remote-as 65002",
		"neighbor 10.0.1.2 route-map IMPORT-POLICY in",
		"neighbor 10.0.1.2 route-map EXPORT-POLICY out",
	}

	for _, exp := range expected {
		if !strings.Contains(frrConfig, exp) {
			t.Errorf("Expected BGP policy entry not found: %s\nGenerated config:\n%s", exp, frrConfig)
		}
	}
}

// TestASPathAccessList tests AS-path access-list generation
func TestASPathAccessList(t *testing.T) {
	input := `
set routing-options autonomous-system 65001
set routing-options router-id 10.0.1.1

set policy-options policy-statement FILTER-AS term MATCH from as-path ".*65002.*"
set policy-options policy-statement FILTER-AS term MATCH then reject
set policy-options policy-statement FILTER-AS term DEFAULT then accept

set protocols bgp group external type external
set protocols bgp group external import FILTER-AS
set protocols bgp group external neighbor 10.0.1.2 peer-as 65002
`

	cfg := parseConfig(t, input)
	frrConfig := generateFRRConfig(t, cfg)

	// Check for AS-path access-list with specific regex
	expected := []string{
		"bgp as-path access-list AS-PATH-1 seq 10 permit .*65002.*",
		"route-map FILTER-AS deny 10",
		"match as-path AS-PATH-1",
		"route-map FILTER-AS permit 20",
	}

	for _, exp := range expected {
		if !strings.Contains(frrConfig, exp) {
			t.Errorf("Expected AS-path entry not found: %s\nGenerated config:\n%s", exp, frrConfig)
		}
	}
}

// TestLocalPreferencePolicy tests local-preference policy
func TestLocalPreferencePolicy(t *testing.T) {
	input := `
set routing-options autonomous-system 65001
set routing-options router-id 10.0.1.1

set policy-options prefix-list CUSTOMER 10.100.0.0/16

set policy-options policy-statement LP-POLICY term CUSTOMER from prefix-list CUSTOMER
set policy-options policy-statement LP-POLICY term CUSTOMER then local-preference 200
set policy-options policy-statement LP-POLICY term CUSTOMER then accept

set policy-options policy-statement LP-POLICY term DEFAULT then accept

set protocols bgp group external type external
set protocols bgp group external import LP-POLICY
set protocols bgp group external neighbor 10.0.1.2 peer-as 65002
`

	cfg := parseConfig(t, input)
	frrConfig := generateFRRConfig(t, cfg)

	// Check for local-preference in route-map
	expected := []string{
		"route-map LP-POLICY permit 10",
		"match ip address prefix-list CUSTOMER",
		"set local-preference 200",
	}

	for _, exp := range expected {
		if !strings.Contains(frrConfig, exp) {
			t.Errorf("Expected local-preference entry not found: %s\nGenerated config:\n%s", exp, frrConfig)
		}
	}
}

// TestCommunityPolicy tests community policy
func TestCommunityPolicy(t *testing.T) {
	input := `
set routing-options autonomous-system 65001
set routing-options router-id 10.0.1.1

set policy-options prefix-list TRANSIT 10.200.0.0/16

set policy-options policy-statement TAG-ROUTES term TRANSIT from prefix-list TRANSIT
set policy-options policy-statement TAG-ROUTES term TRANSIT then community no-export
set policy-options policy-statement TAG-ROUTES term TRANSIT then accept

set policy-options policy-statement TAG-ROUTES term DEFAULT then accept

set protocols bgp group upstream type external
set protocols bgp group upstream export TAG-ROUTES
set protocols bgp group upstream neighbor 10.0.2.1 peer-as 65003
`

	cfg := parseConfig(t, input)
	frrConfig := generateFRRConfig(t, cfg)

	// Check for community in route-map
	expected := []string{
		"route-map TAG-ROUTES permit 10",
		"match ip address prefix-list TRANSIT",
		"set community no-export",
		"neighbor 10.0.2.1 route-map TAG-ROUTES out",
	}

	for _, exp := range expected {
		if !strings.Contains(frrConfig, exp) {
			t.Errorf("Expected community entry not found: %s\nGenerated config:\n%s", exp, frrConfig)
		}
	}
}

// TestPolicyValidation tests policy reference validation
func TestPolicyValidation(t *testing.T) {
	input := `
set routing-options autonomous-system 65001
set routing-options router-id 10.0.1.1

set protocols bgp group external type external
set protocols bgp group external import NONEXISTENT-POLICY
set protocols bgp group external neighbor 10.0.1.2 peer-as 65002
`

	cfg := parseConfig(t, input)
	_, err := frr.GenerateFRRConfig(cfg)
	if err == nil {
		t.Fatal("Expected error for missing policy reference, but got none")
	}

	// Check for error message about missing policy
	if !strings.Contains(err.Error(), "NONEXISTENT-POLICY") ||
	   !strings.Contains(err.Error(), "policy") {
		t.Errorf("Expected error message about missing policy, got: %v", err)
	}
}

// TestMultiplePolicies tests multiple policy-statements
func TestMultiplePolicies(t *testing.T) {
	input := `
set routing-options autonomous-system 65001
set routing-options router-id 10.0.1.1

set policy-options prefix-list CUSTOMER 10.100.0.0/16
set policy-options prefix-list PEER 10.200.0.0/16

set policy-options policy-statement IMPORT-CUSTOMER term ALLOW from prefix-list CUSTOMER
set policy-options policy-statement IMPORT-CUSTOMER term ALLOW then local-preference 200
set policy-options policy-statement IMPORT-CUSTOMER term ALLOW then accept

set policy-options policy-statement IMPORT-PEER term ALLOW from prefix-list PEER
set policy-options policy-statement IMPORT-PEER term ALLOW then local-preference 100
set policy-options policy-statement IMPORT-PEER term ALLOW then accept

set protocols bgp group customers type external
set protocols bgp group customers import IMPORT-CUSTOMER
set protocols bgp group customers neighbor 10.0.1.2 peer-as 65002

set protocols bgp group peers type external
set protocols bgp group peers import IMPORT-PEER
set protocols bgp group peers neighbor 10.0.2.1 peer-as 65003
`

	cfg := parseConfig(t, input)
	frrConfig := generateFRRConfig(t, cfg)

	// Check for multiple route-maps
	expected := []string{
		"route-map IMPORT-CUSTOMER permit",
		"route-map IMPORT-PEER permit",
		"neighbor 10.0.1.2 route-map IMPORT-CUSTOMER in",
		"neighbor 10.0.2.1 route-map IMPORT-PEER in",
	}

	for _, exp := range expected {
		if !strings.Contains(frrConfig, exp) {
			t.Errorf("Expected policy entry not found: %s\nGenerated config:\n%s", exp, frrConfig)
		}
	}
}
