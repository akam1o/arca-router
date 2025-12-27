package frr

import (
	"fmt"
	"strings"
	"testing"

	"github.com/akam1o/arca-router/pkg/config"
)

// TestConvertPrefixLists tests prefix-list conversion
func TestConvertPrefixLists(t *testing.T) {
	tests := []struct {
		name          string
		input         map[string]*config.PrefixList
		wantCount     int
		wantFirstName string
		wantIsIPv6    bool
	}{
		{
			name: "single IPv4 prefix-list",
			input: map[string]*config.PrefixList{
				"MYLIST": {
					Name:     "MYLIST",
					Prefixes: []string{"10.0.0.0/8"},
				},
			},
			wantCount:     1,
			wantFirstName: "MYLIST",
			wantIsIPv6:    false,
		},
		{
			name: "single IPv6 prefix-list",
			input: map[string]*config.PrefixList{
				"IPV6LIST": {
					Name:     "IPV6LIST",
					Prefixes: []string{"2001:db8::/32"},
				},
			},
			wantCount:     1,
			wantFirstName: "IPV6LIST",
			wantIsIPv6:    true,
		},
		{
			name: "multiple prefixes in one list",
			input: map[string]*config.PrefixList{
				"PRIVATE": {
					Name: "PRIVATE",
					Prefixes: []string{
						"10.0.0.0/8",
						"172.16.0.0/12",
						"192.168.0.0/16",
					},
				},
			},
			wantCount:     1,
			wantFirstName: "PRIVATE",
			wantIsIPv6:    false,
		},
		{
			name:      "empty prefix-list map",
			input:     map[string]*config.PrefixList{},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := convertPrefixLists(tt.input)
			if err != nil {
				t.Fatalf("convertPrefixLists() error = %v", err)
			}

			if len(result) != tt.wantCount {
				t.Errorf("Expected %d prefix-lists, got %d", tt.wantCount, len(result))
			}

			if tt.wantCount > 0 {
				first := result[0]
				if first.Name != tt.wantFirstName {
					t.Errorf("Expected first name %s, got %s", tt.wantFirstName, first.Name)
				}
				if first.IsIPv6 != tt.wantIsIPv6 {
					t.Errorf("Expected IsIPv6 %v, got %v", tt.wantIsIPv6, first.IsIPv6)
				}
			}
		})
	}
}

// TestConvertPolicyStatements tests policy-statement to route-map conversion
func TestConvertPolicyStatements(t *testing.T) {
	acceptTrue := true
	acceptFalse := false
	localPref := uint32(200)

	tests := []struct {
		name          string
		input         map[string]*config.PolicyStatement
		wantCount     int
		wantFirstName string
		wantAction    string
	}{
		{
			name: "simple accept policy",
			input: map[string]*config.PolicyStatement{
				"ACCEPT-ALL": {
					Name: "ACCEPT-ALL",
					Terms: []*config.PolicyTerm{
						{
							Name: "TERM1",
							From: &config.PolicyMatchConditions{
								PrefixLists: []string{"MYLIST"},
							},
							Then: &config.PolicyActions{
								Accept: &acceptTrue,
							},
						},
					},
				},
			},
			wantCount:     1,
			wantFirstName: "ACCEPT-ALL",
			wantAction:    "permit",
		},
		{
			name: "reject policy",
			input: map[string]*config.PolicyStatement{
				"DENY-PRIVATE": {
					Name: "DENY-PRIVATE",
					Terms: []*config.PolicyTerm{
						{
							Name: "DENY",
							From: &config.PolicyMatchConditions{
								PrefixLists: []string{"PRIVATE"},
							},
							Then: &config.PolicyActions{
								Accept: &acceptFalse,
							},
						},
					},
				},
			},
			wantCount:     1,
			wantFirstName: "DENY-PRIVATE",
			wantAction:    "deny",
		},
		{
			name: "policy with local-preference",
			input: map[string]*config.PolicyStatement{
				"SET-LOCALPREF": {
					Name: "SET-LOCALPREF",
					Terms: []*config.PolicyTerm{
						{
							Name: "TERM1",
							From: &config.PolicyMatchConditions{
								Protocol: "bgp",
							},
							Then: &config.PolicyActions{
								Accept:          &acceptTrue,
								LocalPreference: &localPref,
							},
						},
					},
				},
			},
			wantCount:     1,
			wantFirstName: "SET-LOCALPREF",
			wantAction:    "permit",
		},
		{
			name:      "empty policy map",
			input:     map[string]*config.PolicyStatement{},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := convertPolicyStatements(tt.input)
			if err != nil {
				t.Fatalf("convertPolicyStatements() error = %v", err)
			}

			if len(result) != tt.wantCount {
				t.Errorf("Expected %d route-maps, got %d", tt.wantCount, len(result))
			}

			if tt.wantCount > 0 {
				first := result[0]
				if first.Name != tt.wantFirstName {
					t.Errorf("Expected first name %s, got %s", tt.wantFirstName, first.Name)
				}
				if len(first.Entries) > 0 && first.Entries[0].Action != tt.wantAction {
					t.Errorf("Expected action %s, got %s", tt.wantAction, first.Entries[0].Action)
				}
			}
		})
	}
}

// TestGeneratePrefixListConfig tests FRR prefix-list config generation
func TestGeneratePrefixListConfig(t *testing.T) {
	tests := []struct {
		name     string
		input    []PrefixList
		wantText []string
	}{
		{
			name: "IPv4 prefix-list",
			input: []PrefixList{
				{
					Name:   "MYLIST",
					IsIPv6: false,
					Entries: []PrefixListEntry{
						{Seq: 10, Action: "permit", Prefix: "10.0.0.0/8"},
					},
				},
			},
			wantText: []string{
				"ip prefix-list MYLIST seq 10 permit 10.0.0.0/8",
			},
		},
		{
			name: "IPv6 prefix-list",
			input: []PrefixList{
				{
					Name:   "IPV6LIST",
					IsIPv6: true,
					Entries: []PrefixListEntry{
						{Seq: 10, Action: "permit", Prefix: "2001:db8::/32"},
					},
				},
			},
			wantText: []string{
				"ipv6 prefix-list IPV6LIST seq 10 permit 2001:db8::/32",
			},
		},
		{
			name: "multiple entries",
			input: []PrefixList{
				{
					Name:   "PRIVATE",
					IsIPv6: false,
					Entries: []PrefixListEntry{
						{Seq: 10, Action: "permit", Prefix: "10.0.0.0/8"},
						{Seq: 20, Action: "permit", Prefix: "172.16.0.0/12"},
						{Seq: 30, Action: "permit", Prefix: "192.168.0.0/16"},
					},
				},
			},
			wantText: []string{
				"ip prefix-list PRIVATE seq 10 permit 10.0.0.0/8",
				"ip prefix-list PRIVATE seq 20 permit 172.16.0.0/12",
				"ip prefix-list PRIVATE seq 30 permit 192.168.0.0/16",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GeneratePrefixListConfig(tt.input)
			if err != nil {
				t.Fatalf("GeneratePrefixListConfig() error = %v", err)
			}

			for _, want := range tt.wantText {
				if !strings.Contains(result, want) {
					t.Errorf("Expected config to contain %q", want)
				}
			}
		})
	}
}

// TestGenerateRouteMapConfig tests FRR route-map config generation
func TestGenerateRouteMapConfig(t *testing.T) {
	localPref := uint32(200)

	tests := []struct {
		name     string
		input    []RouteMap
		wantText []string
	}{
		{
			name: "simple route-map with prefix-list",
			input: []RouteMap{
				{
					Name: "MYPOLICY",
					Entries: []RouteMapEntry{
						{
							Seq:              10,
							Action:           "permit",
							MatchPrefixLists: []string{"MYLIST"},
						},
					},
				},
			},
			wantText: []string{
				"route-map MYPOLICY permit 10",
				"match ip address prefix-list MYLIST",
			},
		},
		{
			name: "route-map with local-preference",
			input: []RouteMap{
				{
					Name: "SET-LP",
					Entries: []RouteMapEntry{
						{
							Seq:                10,
							Action:             "permit",
							MatchProtocol:      "bgp",
							SetLocalPreference: &localPref,
						},
					},
				},
			},
			wantText: []string{
				"route-map SET-LP permit 10",
				"match source-protocol bgp",
				"set local-preference 200",
			},
		},
		{
			name: "route-map with community",
			input: []RouteMap{
				{
					Name: "SET-COMM",
					Entries: []RouteMapEntry{
						{
							Seq:          10,
							Action:       "permit",
							SetCommunity: "65000:100",
						},
					},
				},
			},
			wantText: []string{
				"route-map SET-COMM permit 10",
				"set community 65000:100",
			},
		},
		{
			name: "deny route-map",
			input: []RouteMap{
				{
					Name: "DENY-POLICY",
					Entries: []RouteMapEntry{
						{
							Seq:              10,
							Action:           "deny",
							MatchPrefixLists: []string{"PRIVATE"},
						},
					},
				},
			},
			wantText: []string{
				"route-map DENY-POLICY deny 10",
				"match ip address prefix-list PRIVATE",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := GenerateRouteMapConfig(tt.input)
			if err != nil {
				t.Fatalf("GenerateRouteMapConfig() error = %v", err)
			}

			for _, want := range tt.wantText {
				if !strings.Contains(result, want) {
					t.Errorf("Expected config to contain %q\nGot:\n%s", want, result)
				}
			}
		})
	}
}

// TestConvertPolicyOptionsIntegration tests full conversion flow
func TestConvertPolicyOptionsIntegration(t *testing.T) {
	acceptTrue := true

	cfg := &config.Config{
		PolicyOptions: &config.PolicyOptions{
			PrefixLists: map[string]*config.PrefixList{
				"PRIVATE": {
					Name: "PRIVATE",
					Prefixes: []string{
						"10.0.0.0/8",
						"172.16.0.0/12",
						"192.168.0.0/16",
					},
				},
			},
			PolicyStatements: map[string]*config.PolicyStatement{
				"DENY-PRIVATE": {
					Name: "DENY-PRIVATE",
					Terms: []*config.PolicyTerm{
						{
							Name: "DENY",
							From: &config.PolicyMatchConditions{
								PrefixLists: []string{"PRIVATE"},
							},
							Then: &config.PolicyActions{
								Accept: &acceptTrue,
							},
						},
					},
				},
			},
		},
	}

	prefixLists, routeMaps, err := convertPolicyOptions(cfg)
	if err != nil {
		t.Fatalf("convertPolicyOptions() error = %v", err)
	}

	if len(prefixLists) != 1 {
		t.Errorf("Expected 1 prefix-list, got %d", len(prefixLists))
	}

	if len(routeMaps) != 1 {
		t.Errorf("Expected 1 route-map, got %d", len(routeMaps))
	}

	// Test prefix-list generation
	plConfig, err := GeneratePrefixListConfig(prefixLists)
	if err != nil {
		t.Fatalf("GeneratePrefixListConfig() error = %v", err)
	}

	if !strings.Contains(plConfig, "ip prefix-list PRIVATE") {
		t.Error("Expected prefix-list PRIVATE in config")
	}

	// Test route-map generation
	rmConfig, err := GenerateRouteMapConfig(routeMaps)
	if err != nil {
		t.Fatalf("GenerateRouteMapConfig() error = %v", err)
	}

	if !strings.Contains(rmConfig, "route-map DENY-PRIVATE") {
		t.Error("Expected route-map DENY-PRIVATE in config")
	}
}

// TestIsIPv6Prefix tests IPv6 prefix detection
func TestIsIPv6Prefix(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		want   bool
	}{
		{name: "IPv4 prefix", prefix: "10.0.0.0/8", want: false},
		{name: "IPv6 prefix", prefix: "2001:db8::/32", want: true},
		{name: "IPv4 /32", prefix: "192.168.1.1/32", want: false},
		{name: "IPv6 /128", prefix: "2001:db8::1/128", want: true},
		{name: "invalid prefix", prefix: "invalid", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isIPv6Prefix(tt.prefix)
			if got != tt.want {
				t.Errorf("isIPv6Prefix(%q) = %v, want %v", tt.prefix, got, tt.want)
			}
		})
	}
}

// TestGenerateFRRConfigWithPolicy tests full FRR config generation with policy-options
func TestGenerateFRRConfigWithPolicy(t *testing.T) {
	acceptTrue := true

	cfg := &config.Config{
		System: &config.SystemConfig{
			HostName: "test-router",
		},
		PolicyOptions: &config.PolicyOptions{
			PrefixLists: map[string]*config.PrefixList{
				"MYLIST": {
					Name:     "MYLIST",
					Prefixes: []string{"10.0.0.0/8"},
				},
			},
			PolicyStatements: map[string]*config.PolicyStatement{
				"MYPOLICY": {
					Name: "MYPOLICY",
					Terms: []*config.PolicyTerm{
						{
							Name: "TERM1",
							From: &config.PolicyMatchConditions{
								PrefixLists: []string{"MYLIST"},
							},
							Then: &config.PolicyActions{
								Accept: &acceptTrue,
							},
						},
					},
				},
			},
		},
	}

	frrConfig, err := GenerateFRRConfig(cfg)
	if err != nil {
		t.Fatalf("GenerateFRRConfig() error = %v", err)
	}

	if len(frrConfig.PrefixLists) != 1 {
		t.Errorf("Expected 1 prefix-list, got %d", len(frrConfig.PrefixLists))
	}

	if len(frrConfig.RouteMaps) != 1 {
		t.Errorf("Expected 1 route-map, got %d", len(frrConfig.RouteMaps))
	}

	// Generate full config file
	configText, err := GenerateFRRConfigFile(frrConfig)
	if err != nil {
		t.Fatalf("GenerateFRRConfigFile() error = %v", err)
	}

	if !strings.Contains(configText, "ip prefix-list MYLIST") {
		t.Error("Expected prefix-list in FRR config")
	}

	if !strings.Contains(configText, "route-map MYPOLICY") {
		t.Error("Expected route-map in FRR config")
	}
}

// Additional edge case tests to reach 50+ test cases

// TestPrefixListWithEmptyPrefixes tests handling of empty prefix array
func TestPrefixListWithEmptyPrefixes(t *testing.T) {
	input := map[string]*config.PrefixList{
		"EMPTY": {
			Name:     "EMPTY",
			Prefixes: []string{},
		},
	}

	result, err := convertPrefixLists(input)
	if err != nil {
		t.Fatalf("convertPrefixLists() error = %v", err)
	}

	if len(result) == 0 {
		t.Skip("Empty prefix-list skipped (expected)")
	}

	if len(result[0].Entries) != 0 {
		t.Errorf("Expected 0 entries for empty prefix list, got %d", len(result[0].Entries))
	}
}

// TestPolicyStatementWithEmptyTerms tests handling of empty terms array
func TestPolicyStatementWithEmptyTerms(t *testing.T) {
	input := map[string]*config.PolicyStatement{
		"EMPTY": {
			Name:  "EMPTY",
			Terms: []*config.PolicyTerm{},
		},
	}

	result, err := convertPolicyStatements(input)
	if err != nil {
		t.Fatalf("convertPolicyStatements() error = %v", err)
	}

	if len(result) == 0 {
		t.Skip("Empty policy statement skipped (expected)")
	}

	if len(result[0].Entries) != 0 {
		t.Errorf("Expected 0 entries for empty terms, got %d", len(result[0].Entries))
	}
}

// TestRouteMapWithNeighborMatch tests route-map with neighbor match
func TestRouteMapWithNeighborMatch(t *testing.T) {
	acceptTrue := true

	input := map[string]*config.PolicyStatement{
		"NEIGHBOR-POLICY": {
			Name: "NEIGHBOR-POLICY",
			Terms: []*config.PolicyTerm{
				{
					Name: "TERM1",
					From: &config.PolicyMatchConditions{
						Neighbor: "192.168.1.1",
					},
					Then: &config.PolicyActions{
						Accept: &acceptTrue,
					},
				},
			},
		},
	}

	result, err := convertPolicyStatements(input)
	if err != nil {
		t.Fatalf("convertPolicyStatements() error = %v", err)
	}

	if result[0].Entries[0].MatchNeighbor != "192.168.1.1" {
		t.Errorf("Expected neighbor 192.168.1.1, got %s", result[0].Entries[0].MatchNeighbor)
	}
}

// TestRouteMapWithProtocolMatch tests route-map with protocol match
func TestRouteMapWithProtocolMatch(t *testing.T) {
	acceptTrue := true

	protocols := []string{"bgp", "ospf", "static", "connected"}

	for _, proto := range protocols {
		t.Run(proto, func(t *testing.T) {
			input := map[string]*config.PolicyStatement{
				"PROTO": {
					Name: "PROTO",
					Terms: []*config.PolicyTerm{
						{
							Name: "TERM1",
							From: &config.PolicyMatchConditions{
								Protocol: proto,
							},
							Then: &config.PolicyActions{
								Accept: &acceptTrue,
							},
						},
					},
				},
			}

			result, err := convertPolicyStatements(input)
			if err != nil {
				t.Fatalf("convertPolicyStatements() error = %v", err)
			}

			if result[0].Entries[0].MatchProtocol != proto {
				t.Errorf("Expected protocol %s, got %s", proto, result[0].Entries[0].MatchProtocol)
			}
		})
	}
}

// TestRouteMapWithASPathMatch tests route-map with AS path match
func TestRouteMapWithASPathMatch(t *testing.T) {
	acceptTrue := true

	input := map[string]*config.PolicyStatement{
		"AS-PATH-POLICY": {
			Name: "AS-PATH-POLICY",
			Terms: []*config.PolicyTerm{
				{
					Name: "TERM1",
					From: &config.PolicyMatchConditions{
						ASPath: "^65001",
					},
					Then: &config.PolicyActions{
						Accept: &acceptTrue,
					},
				},
			},
		},
	}

	result, err := convertPolicyStatements(input)
	if err != nil {
		t.Fatalf("convertPolicyStatements() error = %v", err)
	}

	if result[0].Entries[0].MatchASPath != "^65001" {
		t.Errorf("Expected AS path ^65001, got %s", result[0].Entries[0].MatchASPath)
	}
}

// TestMultiplePrefixLists tests conversion of multiple prefix-lists
func TestMultiplePrefixLists(t *testing.T) {
	input := map[string]*config.PrefixList{
		"LIST1": {
			Name:     "LIST1",
			Prefixes: []string{"10.0.0.0/8"},
		},
		"LIST2": {
			Name:     "LIST2",
			Prefixes: []string{"172.16.0.0/12"},
		},
		"LIST3": {
			Name:     "LIST3",
			Prefixes: []string{"192.168.0.0/16"},
		},
	}

	result, err := convertPrefixLists(input)
	if err != nil {
		t.Fatalf("convertPrefixLists() error = %v", err)
	}

	if len(result) != 3 {
		t.Errorf("Expected 3 prefix-lists, got %d", len(result))
	}
}

// TestMultiplePolicyStatements tests conversion of multiple policy-statements
func TestMultiplePolicyStatements(t *testing.T) {
	acceptTrue := true

	input := map[string]*config.PolicyStatement{
		"POLICY1": {
			Name: "POLICY1",
			Terms: []*config.PolicyTerm{
				{
					Name: "TERM1",
					From: &config.PolicyMatchConditions{PrefixLists: []string{"LIST1"}},
					Then: &config.PolicyActions{Accept: &acceptTrue},
				},
			},
		},
		"POLICY2": {
			Name: "POLICY2",
			Terms: []*config.PolicyTerm{
				{
					Name: "TERM1",
					From: &config.PolicyMatchConditions{PrefixLists: []string{"LIST2"}},
					Then: &config.PolicyActions{Accept: &acceptTrue},
				},
			},
		},
	}

	result, err := convertPolicyStatements(input)
	if err != nil {
		t.Fatalf("convertPolicyStatements() error = %v", err)
	}

	if len(result) != 2 {
		t.Errorf("Expected 2 policy-statements, got %d", len(result))
	}
}

// TestLocalPreferenceZero tests local-preference with value 0
func TestLocalPreferenceZero(t *testing.T) {
	acceptTrue := true
	localPref := uint32(0)

	input := map[string]*config.PolicyStatement{
		"LP-ZERO": {
			Name: "LP-ZERO",
			Terms: []*config.PolicyTerm{
				{
					Name: "TERM1",
					From: &config.PolicyMatchConditions{PrefixLists: []string{"LIST"}},
					Then: &config.PolicyActions{
						Accept:          &acceptTrue,
						LocalPreference: &localPref,
					},
				},
			},
		},
	}

	result, err := convertPolicyStatements(input)
	if err != nil {
		t.Fatalf("convertPolicyStatements() error = %v", err)
	}

	entry := result[0].Entries[0]
	if entry.SetLocalPreference == nil || *entry.SetLocalPreference != 0 {
		t.Error("Expected local-preference 0")
	}
}

// TestCommunityFormats tests various community string formats
func TestCommunityFormats(t *testing.T) {
	acceptTrue := true

	communities := []string{
		"65000:100",
		"no-export",
		"no-advertise",
		"local-AS",
	}

	for _, comm := range communities {
		t.Run(comm, func(t *testing.T) {
			input := map[string]*config.PolicyStatement{
				"COMM": {
					Name: "COMM",
					Terms: []*config.PolicyTerm{
						{
							Name: "TERM1",
							From: &config.PolicyMatchConditions{PrefixLists: []string{"LIST"}},
							Then: &config.PolicyActions{
								Accept:    &acceptTrue,
								Community: comm,
							},
						},
					},
				},
			}

			result, err := convertPolicyStatements(input)
			if err != nil {
				t.Fatalf("convertPolicyStatements() error = %v", err)
			}

			if result[0].Entries[0].SetCommunity != comm {
				t.Errorf("Expected community %s, got %s", comm, result[0].Entries[0].SetCommunity)
			}
		})
	}
}

// Additional tests for comprehensive coverage

// TestPrefixListConfigEmpty tests empty prefix-list config generation
func TestPrefixListConfigEmpty(t *testing.T) {
	result, err := GeneratePrefixListConfig([]PrefixList{})
	if err != nil {
		t.Fatalf("GeneratePrefixListConfig() error = %v", err)
	}
	if result != "" {
		t.Errorf("Expected empty string for empty input, got %q", result)
	}
}

// TestRouteMapConfigEmpty tests empty route-map config generation
func TestRouteMapConfigEmpty(t *testing.T) {
	result, err := GenerateRouteMapConfig([]RouteMap{})
	if err != nil {
		t.Fatalf("GenerateRouteMapConfig() error = %v", err)
	}
	if result != "" {
		t.Errorf("Expected empty string for empty input, got %q", result)
	}
}

// TestConvertPolicyOptionsNilConfig tests nil config handling
func TestConvertPolicyOptionsNilConfig(t *testing.T) {
	pl, rm, err := convertPolicyOptions(nil)
	if err != nil {
		t.Fatalf("convertPolicyOptions(nil) error = %v", err)
	}
	if pl != nil || rm != nil {
		t.Error("Expected nil results for nil config")
	}
}

// TestPrefixListLargeSequence tests large number of prefixes
func TestPrefixListLargeSequence(t *testing.T) {
	prefixes := make([]string, 15)
	for i := 0; i < 15; i++ {
		prefixes[i] = fmt.Sprintf("10.%d.0.0/16", i)
	}

	input := map[string]*config.PrefixList{
		"LARGE": {
			Name:     "LARGE",
			Prefixes: prefixes,
		},
	}

	result, err := convertPrefixLists(input)
	if err != nil {
		t.Fatalf("convertPrefixLists() error = %v", err)
	}

	if len(result[0].Entries) != 15 {
		t.Errorf("Expected 15 entries, got %d", len(result[0].Entries))
	}

	// Check last sequence number
	if result[0].Entries[14].Seq != 150 {
		t.Errorf("Expected last seq 150, got %d", result[0].Entries[14].Seq)
	}
}

// TestPolicyStatementLargeTerms tests large number of terms
func TestPolicyStatementLargeTerms(t *testing.T) {
	acceptTrue := true

	terms := make([]*config.PolicyTerm, 10)
	for i := 0; i < 10; i++ {
		terms[i] = &config.PolicyTerm{
			Name: fmt.Sprintf("TERM%d", i+1),
			From: &config.PolicyMatchConditions{
				PrefixLists: []string{fmt.Sprintf("LIST%d", i+1)},
			},
			Then: &config.PolicyActions{
				Accept: &acceptTrue,
			},
		}
	}

	input := map[string]*config.PolicyStatement{
		"LARGE": {
			Name:  "LARGE",
			Terms: terms,
		},
	}

	result, err := convertPolicyStatements(input)
	if err != nil {
		t.Fatalf("convertPolicyStatements() error = %v", err)
	}

	if len(result[0].Entries) != 10 {
		t.Errorf("Expected 10 entries, got %d", len(result[0].Entries))
	}

	// Check last sequence number
	if result[0].Entries[9].Seq != 100 {
		t.Errorf("Expected last seq 100, got %d", result[0].Entries[9].Seq)
	}
}

// TestMultiplePrefixListsInOneTerm tests multiple prefix-lists in a single term
func TestMultiplePrefixListsInOneTerm(t *testing.T) {
	acceptTrue := true

	input := map[string]*config.PolicyStatement{
		"MULTI-PL": {
			Name: "MULTI-PL",
			Terms: []*config.PolicyTerm{
				{
					Name: "TERM1",
					From: &config.PolicyMatchConditions{
						PrefixLists: []string{"LIST1", "LIST2", "LIST3"},
					},
					Then: &config.PolicyActions{
						Accept: &acceptTrue,
					},
				},
			},
		},
	}

	result, err := convertPolicyStatements(input)
	if err != nil {
		t.Fatalf("convertPolicyStatements() error = %v", err)
	}

	if len(result[0].Entries[0].MatchPrefixLists) != 3 {
		t.Errorf("Expected 3 prefix-lists, got %d", len(result[0].Entries[0].MatchPrefixLists))
	}
}

// TestGenerateRouteMapWithMultiplePrefixLists tests output with multiple prefix-lists
func TestGenerateRouteMapWithMultiplePrefixLists(t *testing.T) {
	input := []RouteMap{
		{
			Name: "TEST",
			Entries: []RouteMapEntry{
				{
					Seq:              10,
					Action:           "permit",
					MatchPrefixLists: []string{"LIST1", "LIST2"},
				},
			},
		},
	}

	result, err := GenerateRouteMapConfig(input)
	if err != nil {
		t.Fatalf("GenerateRouteMapConfig() error = %v", err)
	}

	// Should have two match statements
	if !strings.Contains(result, "match ip address prefix-list LIST1") {
		t.Error("Expected match for LIST1")
	}
	if !strings.Contains(result, "match ip address prefix-list LIST2") {
		t.Error("Expected match for LIST2")
	}
}

// TestPrefixListHostRoute tests /32 host routes
func TestPrefixListHostRoute(t *testing.T) {
	input := map[string]*config.PrefixList{
		"HOST": {
			Name:     "HOST",
			Prefixes: []string{"192.168.1.1/32"},
		},
	}

	result, err := convertPrefixLists(input)
	if err != nil {
		t.Fatalf("convertPrefixLists() error = %v", err)
	}

	if result[0].Entries[0].Prefix != "192.168.1.1/32" {
		t.Errorf("Expected prefix 192.168.1.1/32, got %s", result[0].Entries[0].Prefix)
	}
}

// TestIPv6HostRoute tests IPv6 /128 host routes
func TestIPv6HostRoute(t *testing.T) {
	input := map[string]*config.PrefixList{
		"HOST6": {
			Name:     "HOST6",
			Prefixes: []string{"2001:db8::1/128"},
		},
	}

	result, err := convertPrefixLists(input)
	if err != nil {
		t.Fatalf("convertPrefixLists() error = %v", err)
	}

	if !result[0].IsIPv6 {
		t.Error("Expected IPv6 prefix-list")
	}

	if result[0].Entries[0].Prefix != "2001:db8::1/128" {
		t.Errorf("Expected prefix 2001:db8::1/128, got %s", result[0].Entries[0].Prefix)
	}
}

// TestRouteMapMultipleEntries tests route-map with multiple entries
func TestRouteMapMultipleEntries(t *testing.T) {
	localPref1 := uint32(100)
	localPref2 := uint32(200)

	input := []RouteMap{
		{
			Name: "MULTI",
			Entries: []RouteMapEntry{
				{
					Seq:                10,
					Action:             "permit",
					MatchPrefixLists:   []string{"LIST1"},
					SetLocalPreference: &localPref1,
				},
				{
					Seq:                20,
					Action:             "permit",
					MatchPrefixLists:   []string{"LIST2"},
					SetLocalPreference: &localPref2,
				},
			},
		},
	}

	result, err := GenerateRouteMapConfig(input)
	if err != nil {
		t.Fatalf("GenerateRouteMapConfig() error = %v", err)
	}

	if !strings.Contains(result, "route-map MULTI permit 10") {
		t.Error("Expected entry 10")
	}
	if !strings.Contains(result, "route-map MULTI permit 20") {
		t.Error("Expected entry 20")
	}
	if !strings.Contains(result, "set local-preference 100") {
		t.Error("Expected LP 100")
	}
	if !strings.Contains(result, "set local-preference 200") {
		t.Error("Expected LP 200")
	}
}

// TestHighLocalPreferenceValue tests maximum local-preference value
func TestHighLocalPreferenceValue(t *testing.T) {
	acceptTrue := true
	localPref := uint32(4294967295) // Max uint32

	input := map[string]*config.PolicyStatement{
		"MAX-LP": {
			Name: "MAX-LP",
			Terms: []*config.PolicyTerm{
				{
					Name: "TERM1",
					From: &config.PolicyMatchConditions{PrefixLists: []string{"LIST"}},
					Then: &config.PolicyActions{
						Accept:          &acceptTrue,
						LocalPreference: &localPref,
					},
				},
			},
		},
	}

	result, err := convertPolicyStatements(input)
	if err != nil {
		t.Fatalf("convertPolicyStatements() error = %v", err)
	}

	entry := result[0].Entries[0]
	if entry.SetLocalPreference == nil || *entry.SetLocalPreference != 4294967295 {
		t.Error("Expected local-preference 4294967295")
	}
}
