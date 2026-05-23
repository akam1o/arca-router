package model

import (
	"strings"
	"testing"
)

func TestValidateAllowsLegacyInterfaceNames(t *testing.T) {
	for _, name := range []string{"ge-0/0/0", "xe-1/2/3", "et-4/5/6", "ae0", "lo0", "irb", "fxp0"} {
		t.Run(name, func(t *testing.T) {
			cfg := NewRouterConfig()
			cfg.Interfaces[name] = &InterfaceConfig{}
			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestValidateRejectsNilNestedConfigEntries(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*RouterConfig)
		want      string
	}{
		{
			name: "interface",
			configure: func(cfg *RouterConfig) {
				cfg.Interfaces["ge-0/0/0"] = nil
			},
			want: "interface ge-0/0/0 is nil",
		},
		{
			name: "unit",
			configure: func(cfg *RouterConfig) {
				cfg.Interfaces["ge-0/0/0"] = &InterfaceConfig{Units: map[int]*Unit{0: nil}}
			},
			want: "interface ge-0/0/0 unit 0 is nil",
		},
		{
			name: "family",
			configure: func(cfg *RouterConfig) {
				cfg.Interfaces["ge-0/0/0"] = &InterfaceConfig{
					Units: map[int]*Unit{
						0: {Family: map[string]*AddressFamily{"inet": nil}},
					},
				}
			},
			want: "interface ge-0/0/0 unit 0 family inet is nil",
		},
		{
			name: "static route",
			configure: func(cfg *RouterConfig) {
				cfg.Routing = &RoutingConfig{StaticRoutes: []*StaticRoute{nil}}
			},
			want: "static route entry is nil",
		},
		{
			name: "bgp group",
			configure: func(cfg *RouterConfig) {
				cfg.Routing = &RoutingConfig{AutonomousSystem: 65000}
				cfg.Protocols = &ProtocolsConfig{
					BGP: &BGPConfig{Groups: map[string]*BGPGroup{"EBGP": nil}},
				}
			},
			want: "bgp group EBGP is nil",
		},
		{
			name: "bgp neighbor",
			configure: func(cfg *RouterConfig) {
				cfg.Routing = &RoutingConfig{AutonomousSystem: 65000}
				cfg.Protocols = &ProtocolsConfig{
					BGP: &BGPConfig{Groups: map[string]*BGPGroup{
						"EBGP": {Neighbors: map[string]*BGPNeighbor{"192.0.2.2": nil}},
					}},
				}
			},
			want: "bgp group EBGP neighbor 192.0.2.2 is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewRouterConfig()
			tt.configure(cfg)
			err := cfg.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil, want validation error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}
