package model

import (
	"testing"

	"github.com/akam1o/arca-router/pkg/config"
)

func TestOSPF3LegacyConversion(t *testing.T) {
	priority := 0
	cfg := FromLegacyConfig(&config.Config{
		Protocols: &config.ProtocolConfig{
			OSPF3: &config.OSPFConfig{
				RouterID: "10.0.1.2",
				Areas: map[string]*config.OSPFArea{
					"0.0.0.0": {
						AreaID: "0.0.0.0",
						Interfaces: map[string]*config.OSPFInterface{
							"ge-0/0/0": {
								Name:        "ge-0/0/0",
								Metric:      20,
								Priority:    priority,
								PrioritySet: true,
							},
						},
					},
				},
			},
		},
	})

	if cfg.Protocols == nil || cfg.Protocols.OSPF3 == nil {
		t.Fatalf("OSPF3 was not converted: %#v", cfg.Protocols)
	}
	gotPriority := cfg.Protocols.OSPF3.Areas["0.0.0.0"].Interfaces["ge-0/0/0"].Priority
	if gotPriority == nil || *gotPriority != 0 {
		t.Fatalf("converted OSPF3 priority = %v, want explicit 0", gotPriority)
	}

	legacy := cfg.ToLegacyConfig()
	ospfIface := legacy.Protocols.OSPF3.Areas["0.0.0.0"].Interfaces["ge-0/0/0"]
	if !ospfIface.PrioritySet || ospfIface.Priority != 0 {
		t.Fatalf("legacy OSPF3 interface = %#v, want explicit priority 0", ospfIface)
	}
}

func TestResolveRouterIDOSPF3(t *testing.T) {
	cfg := NewRouterConfig()
	cfg.Routing = &RoutingConfig{RouterID: "10.0.1.1"}
	cfg.Protocols = &ProtocolsConfig{
		OSPF3: &OSPFConfig{RouterID: "10.0.1.2"},
	}

	if got := cfg.ResolveRouterID("ospf3"); got != "10.0.1.2" {
		t.Fatalf("ResolveRouterID(ospf3) = %q, want 10.0.1.2", got)
	}
	cfg.Protocols.OSPF3.RouterID = ""
	if got := cfg.ResolveRouterID("ospf3"); got != "10.0.1.1" {
		t.Fatalf("ResolveRouterID(ospf3 fallback) = %q, want 10.0.1.1", got)
	}
}
