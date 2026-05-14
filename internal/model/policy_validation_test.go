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
