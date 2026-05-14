package model

import (
	"strings"
	"testing"
)

func TestInterfaceReferenceValidationRejectsUnknownInterfaces(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*RouterConfig)
		want      string
	}{
		{
			name: "mpls",
			configure: func(cfg *RouterConfig) {
				cfg.Protocols = &ProtocolsConfig{
					MPLS: &MPLSConfig{Interfaces: []string{"ge-0/0/0"}},
				}
			},
			want: `mpls: interface "ge-0/0/0" is not configured`,
		},
		{
			name: "vrrp",
			configure: func(cfg *RouterConfig) {
				cfg.Protocols = &ProtocolsConfig{
					VRRP: &VRRPConfig{Groups: map[string]*VRRPGroup{
						"10": {
							Interface:      "ge-0/0/0",
							VirtualAddress: "192.0.2.254",
						},
					}},
				}
			},
			want: `vrrp group 10: interface "ge-0/0/0" is not configured`,
		},
		{
			name: "ospf",
			configure: func(cfg *RouterConfig) {
				cfg.Protocols = &ProtocolsConfig{
					OSPF: &OSPFConfig{Areas: map[string]*OSPFArea{
						"0.0.0.0": {
							Interfaces: map[string]*OSPFInterface{
								"ge-0/0/0": {},
							},
						},
					}},
				}
			},
			want: `ospf area 0.0.0.0: interface "ge-0/0/0" is not configured`,
		},
		{
			name: "ospf3",
			configure: func(cfg *RouterConfig) {
				cfg.Protocols = &ProtocolsConfig{
					OSPF3: &OSPFConfig{Areas: map[string]*OSPFArea{
						"0.0.0.0": {
							Interfaces: map[string]*OSPFInterface{
								"ge-0/0/0": {},
							},
						},
					}},
				}
			},
			want: `ospf3 area 0.0.0.0: interface "ge-0/0/0" is not configured`,
		},
		{
			name: "routing-instance",
			configure: func(cfg *RouterConfig) {
				cfg.RoutingInstances = map[string]*RoutingInstance{
					"BLUE": {
						InstanceType: "vrf",
						Interfaces:   []string{"ge-0/0/0"},
					},
				}
			},
			want: `routing-instance BLUE: interface "ge-0/0/0" is not configured`,
		},
		{
			name: "class-of-service",
			configure: func(cfg *RouterConfig) {
				cfg.ClassOfService = &ClassOfServiceConfig{
					TrafficControlProfiles: map[string]*TrafficControlProfile{
						"WAN": {},
					},
					Interfaces: map[string]*CoSInterface{
						"ge-0/0/0": {OutputTrafficControlProfile: "WAN"},
					},
				}
			},
			want: `class-of-service: interface "ge-0/0/0" is not configured`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewRouterConfig()
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
