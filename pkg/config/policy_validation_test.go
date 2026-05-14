package config

import (
	"strings"
	"testing"
)

func TestValidatePolicyOptionsAcceptsIPv6PrefixListReference(t *testing.T) {
	accept := true
	cfg := NewConfig()
	cfg.PolicyOptions = &PolicyOptions{
		PrefixLists: map[string]*PrefixList{
			"V6-IN": {Name: "V6-IN", Prefixes: []string{"2001:db8::/32"}},
		},
		PolicyStatements: map[string]*PolicyStatement{
			"IMPORT-V6": {
				Name: "IMPORT-V6",
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

func TestValidatePolicyOptionsRejectsInvalidPrefix(t *testing.T) {
	cfg := NewConfig()
	cfg.PolicyOptions = &PolicyOptions{
		PrefixLists: map[string]*PrefixList{
			"BAD": {Name: "BAD", Prefixes: []string{"not-a-prefix"}},
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), `invalid prefix "not-a-prefix"`) {
		t.Fatalf("Validate() error = %v, want invalid prefix error", err)
	}
}

func TestValidatePolicyOptionsRejectsUnknownPrefixListReference(t *testing.T) {
	cfg := NewConfig()
	cfg.PolicyOptions = &PolicyOptions{
		PolicyStatements: map[string]*PolicyStatement{
			"IMPORT": {
				Name: "IMPORT",
				Terms: []*PolicyTerm{
					{Name: "MATCH", From: &PolicyMatchConditions{PrefixLists: []string{"MISSING"}}},
				},
			},
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "unknown prefix-list MISSING") {
		t.Fatalf("Validate() error = %v, want unknown prefix-list error", err)
	}
}

func TestValidateBGPGroupRejectsUnknownPolicyReferences(t *testing.T) {
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
			want: "BGP group EBGP import references unknown policy-statement IMPORT-MISSING",
		},
		{
			name: "export unknown",
			configure: func(group *BGPGroup) {
				group.Export = "EXPORT-MISSING"
			},
			want: "BGP group EBGP export references unknown policy-statement EXPORT-MISSING",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewConfig()
			cfg.RoutingOptions = &RoutingOptions{AutonomousSystem: 65000}
			cfg.Protocols = &ProtocolConfig{BGP: &BGPConfig{Groups: map[string]*BGPGroup{
				"EBGP": {
					Type: "external",
					Neighbors: map[string]*BGPNeighbor{
						"192.0.2.2": {IP: "192.0.2.2", PeerAS: 65001},
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
