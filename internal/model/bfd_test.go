package model

import (
	"strings"
	"testing"

	"github.com/akam1o/arca-router/pkg/config"
)

func TestBFDLegacyConversionRoundTrip(t *testing.T) {
	legacy := config.NewConfig()
	legacy.Protocols = &config.ProtocolConfig{
		BFD: &config.BFDConfig{
			Profiles: map[string]*config.BFDProfile{
				"fast": {Name: "fast", DetectMultiplier: 3, ReceiveInterval: 150, TransmitInterval: 150},
			},
			Peers: map[string]*config.BFDPeer{
				"192.0.2.2": {Address: "192.0.2.2", LocalAddress: "192.0.2.1", Interface: "ge-0/0/0", Profile: "fast"},
			},
		},
		BGP: &config.BGPConfig{Groups: map[string]*config.BGPGroup{
			"EBGP": {
				Type: "external",
				Neighbors: map[string]*config.BGPNeighbor{
					"192.0.2.2": {IP: "192.0.2.2", PeerAS: 65001, BFD: true, BFDProfile: "fast"},
				},
			},
		}},
		OSPF: &config.OSPFConfig{Areas: map[string]*config.OSPFArea{
			"0.0.0.0": {
				AreaID: "0.0.0.0",
				Interfaces: map[string]*config.OSPFInterface{
					"ge-0/0/0": {Name: "ge-0/0/0", BFD: true, BFDProfile: "fast"},
				},
			},
		}},
	}
	legacy.RoutingOptions = &config.RoutingOptions{
		StaticRoutes: []*config.StaticRoute{
			{
				Prefix:      "203.0.113.0/24",
				NextHop:     "192.0.2.2",
				BFD:         true,
				BFDProfile:  "fast",
				BFDSource:   "192.0.2.1",
				BFDMultihop: true,
			},
		},
	}

	modelCfg := FromLegacyConfig(legacy)
	if modelCfg.Protocols == nil || modelCfg.Protocols.BFD == nil {
		t.Fatalf("FromLegacyConfig() dropped BFD: %#v", modelCfg.Protocols)
	}
	roundTrip := modelCfg.ToLegacyConfig()
	if roundTrip.Protocols == nil || roundTrip.Protocols.BFD == nil || roundTrip.Protocols.BFD.Peers["192.0.2.2"] == nil {
		t.Fatalf("ToLegacyConfig() dropped BFD: %#v", roundTrip.Protocols)
	}
	if got := roundTrip.Protocols.BFD.Peers["192.0.2.2"].Profile; got != "fast" {
		t.Fatalf("BFD peer profile = %q, want fast", got)
	}
	if got := roundTrip.Protocols.BGP.Groups["EBGP"].Neighbors["192.0.2.2"].BFDProfile; got != "fast" {
		t.Fatalf("BGP BFD profile = %q, want fast", got)
	}
	if got := roundTrip.Protocols.OSPF.Areas["0.0.0.0"].Interfaces["ge-0/0/0"].BFDProfile; got != "fast" {
		t.Fatalf("OSPF BFD profile = %q, want fast", got)
	}
	route := roundTrip.RoutingOptions.StaticRoutes[0]
	if !route.BFD || route.BFDProfile != "fast" || route.BFDSource != "192.0.2.1" || !route.BFDMultihop {
		t.Fatalf("Static route BFD = %#v, want profile/source/multihop", route)
	}
}

func TestValidateBFDUnknownInterface(t *testing.T) {
	cfg := NewRouterConfig()
	cfg.Protocols = &ProtocolsConfig{
		BFD: &BFDConfig{
			Peers: map[string]*BFDPeer{
				"192.0.2.2": {Interface: "ge-0/0/0"},
			},
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "interface") {
		t.Fatalf("Validate() error = %v, want interface reference error", err)
	}
}

func TestValidateBFDProtocolBindingProfileReference(t *testing.T) {
	cfg := NewRouterConfig()
	cfg.Protocols = &ProtocolsConfig{
		BFD: &BFDConfig{Profiles: map[string]*BFDProfile{
			"fast": {ReceiveInterval: 150},
		}},
		BGP: &BGPConfig{Groups: map[string]*BGPGroup{
			"EBGP": {
				Type: "external",
				Neighbors: map[string]*BGPNeighbor{
					"192.0.2.2": {PeerAS: 65001, BFD: true, BFDProfile: "missing"},
				},
			},
		}},
	}
	cfg.Routing = &RoutingConfig{AutonomousSystem: 65000}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("Validate() error = %v, want missing BFD profile error", err)
	}
}
