package frr

import (
	"io"
	"log/slog"
	"testing"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestBuildFullConfigUsesDiffNewConfig(t *testing.T) {
	newCfg := model.NewRouterConfig()
	newCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{
		Units: map[int]*model.Unit{
			0: {Family: map[string]*model.AddressFamily{"inet": {Addresses: []string{"192.0.2.1/24"}}}},
		},
	}
	newCfg.Routing = &model.RoutingConfig{AutonomousSystem: 65000, RouterID: "192.0.2.1"}
	newCfg.Protocols = &model.ProtocolsConfig{
		BGP: &model.BGPConfig{Groups: map[string]*model.BGPGroup{
			"EBGP": {
				Type: "external",
				Neighbors: map[string]*model.BGPNeighbor{
					"203.0.113.1": {PeerAS: 65001, LocalAddress: "192.0.2.1"},
				},
			},
		}},
	}

	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)
	got := NewFRRPlugin(testLogger()).buildFullConfig(diff)

	if _, ok := got.Interfaces["ge-0/0/0"]; !ok {
		t.Fatalf("buildFullConfig() dropped interfaces: %#v", got.Interfaces)
	}
	if got.Protocols == nil || got.Protocols.BGP == nil {
		t.Fatalf("buildFullConfig() dropped protocols: %#v", got.Protocols)
	}
}
