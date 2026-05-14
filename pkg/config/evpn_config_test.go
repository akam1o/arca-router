package config

import (
	"strings"
	"testing"
)

func TestEVPNConfigRoundTrip(t *testing.T) {
	cfg := parseSetCommands(t,
		"set routing-options autonomous-system 65000",
		"set interfaces ge-0/0/0 unit 0 family inet address 192.0.2.1/24",
		"set routing-instances BLUE instance-type vrf",
		"set protocols evpn vni 10010 type l2",
		"set protocols evpn vni 10010 bridge-domain BD-10",
		"set protocols evpn vni 10010 vlan-id 10",
		"set protocols evpn vni 10010 route-distinguisher 65000:10010",
		"set protocols evpn vni 10010 vrf-target target:65000:10010",
		"set protocols evpn vni 10010 vrf-target import target:65000:10011",
		"set protocols evpn vni 10010 vrf-target export target:65000:10012",
		"set protocols evpn vni 10010 source-interface ge-0/0/0",
		"set protocols evpn vni 10010 source-address 192.0.2.1",
		"set protocols evpn vni 10010 multicast-group 239.0.0.10",
		"set protocols evpn vni 20010 type l3",
		"set protocols evpn vni 20010 routing-instance BLUE",
	)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	l2 := cfg.Protocols.EVPN.VNIs[10010]
	if l2 == nil || l2.Type != "l2" || l2.BridgeDomain != "BD-10" || l2.VLANID != 10 || l2.SourceInterface != "ge-0/0/0" {
		t.Fatalf("L2 EVPN VNI = %#v, want configured VNI", l2)
	}
	l3 := cfg.Protocols.EVPN.VNIs[20010]
	if l3 == nil || l3.Type != "l3" || l3.RoutingInstance != "BLUE" {
		t.Fatalf("L3 EVPN VNI = %#v, want routing-instance BLUE", l3)
	}
	assertSetCommandRoundTrip(t, cfg)
}

func TestEVPNValidationRejectsInvalidVNIReferences(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*Config)
		want      string
	}{
		{
			name: "missing bridge domain",
			configure: func(cfg *Config) {
				cfg.Protocols = &ProtocolConfig{EVPN: &EVPNConfig{VNIs: map[int]*EVPNVNI{
					10010: {VNI: 10010, Type: "l2"},
				}}}
			},
			want: "EVPN VNI 10010 is missing bridge-domain",
		},
		{
			name: "unknown routing instance",
			configure: func(cfg *Config) {
				cfg.Protocols = &ProtocolConfig{EVPN: &EVPNConfig{VNIs: map[int]*EVPNVNI{
					20010: {VNI: 20010, Type: "l3", RoutingInstance: "BLUE"},
				}}}
			},
			want: "EVPN VNI 20010 references unknown routing-instance BLUE",
		},
		{
			name: "unknown source interface",
			configure: func(cfg *Config) {
				cfg.Protocols = &ProtocolConfig{EVPN: &EVPNConfig{VNIs: map[int]*EVPNVNI{
					10010: {VNI: 10010, Type: "l2", BridgeDomain: "BD-10", SourceInterface: "ge-0/0/0"},
				}}}
			},
			want: "EVPN VNI 10010 references non-existent interface ge-0/0/0",
		},
		{
			name: "invalid multicast group",
			configure: func(cfg *Config) {
				cfg.Protocols = &ProtocolConfig{EVPN: &EVPNConfig{VNIs: map[int]*EVPNVNI{
					10010: {VNI: 10010, Type: "l2", BridgeDomain: "BD-10", MulticastGroup: "192.0.2.10"},
				}}}
			},
			want: "EVPN VNI 10010 has invalid multicast-group",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewConfig()
			tt.configure(cfg)

			err := cfg.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil, want EVPN validation error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}
