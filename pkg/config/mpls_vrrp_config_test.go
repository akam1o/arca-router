package config

import (
	"strings"
	"testing"
)

func TestMPLSAndVRRPConfigRoundTrip(t *testing.T) {
	cfg := parseSetCommands(t,
		"set interfaces ge-0/0/0 unit 0 family inet address 192.0.2.1/24",
		"set protocols mpls interface ge-0/0/0",
		"set protocols vrrp group 10 interface ge-0/0/0",
		"set protocols vrrp group 10 virtual-address 192.0.2.254",
		"set protocols vrrp group 10 priority 110",
		"set protocols vrrp group 10 preempt",
	)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if got := cfg.Protocols.MPLS.Interfaces; len(got) != 1 || got[0] != "ge-0/0/0" {
		t.Fatalf("MPLS interfaces = %#v, want [ge-0/0/0]", got)
	}
	group := cfg.Protocols.VRRP.Groups["10"]
	if group == nil || group.Interface != "ge-0/0/0" || group.VirtualAddress != "192.0.2.254" || group.Priority != 110 || !group.Preempt {
		t.Fatalf("VRRP group = %#v, want configured group", group)
	}
	assertSetCommandRoundTrip(t, cfg)
}

func TestMPLSAndVRRPValidationRejectsUnknownInterfaceReferences(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*Config)
		want      string
	}{
		{
			name: "mpls",
			configure: func(cfg *Config) {
				cfg.Protocols = &ProtocolConfig{
					MPLS: &MPLSConfig{Interfaces: []string{"ge-0/0/0"}},
				}
			},
			want: "MPLS references non-existent interface ge-0/0/0",
		},
		{
			name: "vrrp",
			configure: func(cfg *Config) {
				cfg.Protocols = &ProtocolConfig{
					VRRP: &VRRPConfig{Groups: map[string]*VRRPGroup{
						"10": {
							Name:           "10",
							Interface:      "ge-0/0/0",
							VirtualAddress: "192.0.2.254",
						},
					}},
				}
			},
			want: "VRRP group 10 references non-existent interface ge-0/0/0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewConfig()
			tt.configure(cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil, want unknown interface reference error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}
