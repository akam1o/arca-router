package model

import (
	"strings"
	"testing"
)

func TestEVPNValidationAcceptsL2AndL3VNIs(t *testing.T) {
	cfg := NewRouterConfig()
	cfg.Interfaces["ge-0/0/0"] = &InterfaceConfig{Units: map[int]*Unit{}}
	cfg.RoutingInstances = map[string]*RoutingInstance{
		"BLUE": {InstanceType: "vrf"},
	}
	cfg.Protocols = &ProtocolsConfig{
		EVPN: &EVPNConfig{VNIs: map[int]*EVPNVNI{
			10010: {
				VNI:                10010,
				Type:               "l2",
				BridgeDomain:       "BD-10",
				VLANID:             10,
				RouteDistinguisher: "65000:10010",
				VRFTarget:          "target:65000:10010",
				SourceInterface:    "ge-0/0/0",
				SourceAddress:      "192.0.2.1",
				MulticastGroup:     "239.0.0.10",
			},
			20010: {
				VNI:             20010,
				Type:            "l3",
				RoutingInstance: "BLUE",
				RemoteVTEP:      "198.51.100.20",
			},
		}},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestEVPNValidationRejectsInvalidVNIs(t *testing.T) {
	tests := []struct {
		name string
		vni  *EVPNVNI
		want string
	}{
		{
			name: "invalid type",
			vni:  &EVPNVNI{VNI: 10010, Type: "bad", BridgeDomain: "BD-10"},
			want: "evpn vni 10010: type must be l2 or l3",
		},
		{
			name: "invalid target",
			vni:  &EVPNVNI{VNI: 10010, Type: "l2", BridgeDomain: "BD-10", VRFTarget: "bad"},
			want: `evpn vni 10010 vrf-target: invalid vrf-target "bad"`,
		},
		{
			name: "l3 without routing instance",
			vni:  &EVPNVNI{VNI: 20010, Type: "l3"},
			want: "evpn vni 20010: routing-instance is required for L3 VNI",
		},
		{
			name: "bad multicast",
			vni:  &EVPNVNI{VNI: 10010, Type: "l2", BridgeDomain: "BD-10", MulticastGroup: "192.0.2.10"},
			want: `evpn vni 10010: invalid multicast-group "192.0.2.10"`,
		},
		{
			name: "bad remote vtep",
			vni:  &EVPNVNI{VNI: 10010, Type: "l2", BridgeDomain: "BD-10", RemoteVTEP: "239.0.0.10"},
			want: `evpn vni 10010: invalid remote-vtep "239.0.0.10"`,
		},
		{
			name: "multicast and remote vtep",
			vni:  &EVPNVNI{VNI: 10010, Type: "l2", BridgeDomain: "BD-10", MulticastGroup: "239.0.0.10", RemoteVTEP: "198.51.100.10"},
			want: "evpn vni 10010: multicast-group and remote-vtep are mutually exclusive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewRouterConfig()
			cfg.Protocols = &ProtocolsConfig{
				EVPN: &EVPNConfig{VNIs: map[int]*EVPNVNI{tt.vni.VNI: tt.vni}},
			}

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
