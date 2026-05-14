package model

import (
	"strings"
	"testing"
)

func TestValidatePolicyAcceptsIPv6PrefixListReference(t *testing.T) {
	accept := true
	cfg := NewRouterConfig()
	cfg.Policy = &PolicyConfig{
		PrefixLists: map[string]*PrefixList{
			"V6-IN": {Prefixes: []string{"2001:db8::/32"}},
		},
		PolicyStatements: map[string]*PolicyStatement{
			"IMPORT-V6": {
				Terms: []*PolicyTerm{
					{
						Name: "ALLOW",
						From: &PolicyMatchConditions{
							PrefixLists: []string{"V6-IN"},
							Protocol:    "ospf3",
							Neighbor:    "2001:db8::2",
							ASPath:      ".*65001.*",
						},
						Then: &PolicyActions{Accept: &accept, Community: "65000:100"},
					},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidatePolicyRejectsUnknownPrefixListReference(t *testing.T) {
	cfg := NewRouterConfig()
	cfg.Policy = &PolicyConfig{
		PolicyStatements: map[string]*PolicyStatement{
			"IMPORT": {
				Terms: []*PolicyTerm{
					{Name: "MATCH", From: &PolicyMatchConditions{PrefixLists: []string{"MISSING"}}},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), `prefix-list "MISSING" not found`) {
		t.Fatalf("Validate() error = %v, want unknown prefix-list error", err)
	}
}

func TestValidatePolicyRejectsInvalidASPath(t *testing.T) {
	cfg := NewRouterConfig()
	cfg.Policy = &PolicyConfig{
		PolicyStatements: map[string]*PolicyStatement{
			"IMPORT": {
				Terms: []*PolicyTerm{
					{Name: "MATCH", From: &PolicyMatchConditions{ASPath: "["}},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "invalid as-path") {
		t.Fatalf("Validate() error = %v, want invalid as-path error", err)
	}
}

func TestValidateBGPRejectsUnknownPolicyReferences(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*BGPGroup)
		want      string
	}{
		{
			name: "import without policy-options",
			configure: func(group *BGPGroup) {
				group.Import = "IMPORT-MISSING"
			},
			want: `bgp group EBGP import: policy-statement "IMPORT-MISSING" not found in policy-options`,
		},
		{
			name: "export unknown",
			configure: func(group *BGPGroup) {
				group.Export = "EXPORT-MISSING"
			},
			want: `bgp group EBGP export: policy-statement "EXPORT-MISSING" not found in policy-options`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewRouterConfig()
			cfg.Routing = &RoutingConfig{AutonomousSystem: 65000}
			cfg.Protocols = &ProtocolsConfig{BGP: &BGPConfig{Groups: map[string]*BGPGroup{
				"EBGP": {
					Type: "external",
					Neighbors: map[string]*BGPNeighbor{
						"192.0.2.2": {PeerAS: 65001},
					},
				},
			}}}
			tt.configure(cfg.Protocols.BGP.Groups["EBGP"])

			err := cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want %q", err, tt.want)
			}
		})
	}
}
