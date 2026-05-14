package frr

import (
	"strings"
	"testing"

	"github.com/akam1o/arca-router/pkg/config"
)

func TestGenerateBFDConfig(t *testing.T) {
	got, err := GenerateBFDConfig(&BFDConfig{
		Profiles: []BFDProfile{
			{Name: "fast", DetectMultiplier: 3, ReceiveInterval: 150, TransmitInterval: 150},
		},
		Peers: []BFDPeer{
			{Address: "192.0.2.2", Interface: "ge0-0-0", LocalAddress: "192.0.2.1", Profile: "fast"},
			{Address: "192.0.2.3", Multihop: true, LocalAddress: "192.0.2.1", Shutdown: true},
		},
	})
	if err != nil {
		t.Fatalf("GenerateBFDConfig() error = %v", err)
	}
	for _, want := range []string{
		"bfd\n",
		" profile fast\n",
		"  receive-interval 150\n",
		"  transmit-interval 150\n",
		"  detect-multiplier 3\n",
		" peer 192.0.2.2 interface ge0-0-0 local-address 192.0.2.1\n",
		"  profile fast\n",
		"  no shutdown\n",
		" peer 192.0.2.3 multihop local-address 192.0.2.1\n",
		"  shutdown\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("GenerateBFDConfig() missing %q:\n%s", want, got)
		}
	}
}

func TestGenerateFRRConfigConvertsBFD(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Interfaces["ge-0/0/0"] = &config.Interface{Units: map[int]*config.Unit{0: {Family: map[string]*config.Family{"inet": {Addresses: []string{"192.0.2.1/24"}}}}}}
	cfg.Protocols = &config.ProtocolConfig{
		BFD: &config.BFDConfig{
			Profiles: map[string]*config.BFDProfile{
				"fast": {Name: "fast", ReceiveInterval: 150},
			},
			Peers: map[string]*config.BFDPeer{
				"192.0.2.2": {Address: "192.0.2.2", Interface: "ge-0/0/0", Profile: "fast"},
			},
		},
	}

	frrCfg, err := GenerateFRRConfig(cfg)
	if err != nil {
		t.Fatalf("GenerateFRRConfig() error = %v", err)
	}
	if frrCfg.BFD == nil || len(frrCfg.BFD.Peers) != 1 {
		t.Fatalf("BFD config = %#v, want one peer", frrCfg.BFD)
	}
	if got := frrCfg.BFD.Peers[0].Interface; got != "ge0-0-0" {
		t.Fatalf("BFD peer interface = %q, want ge0-0-0", got)
	}
}

func TestGenerateFRRConfigConvertsBFDProtocolBindings(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Interfaces["ge-0/0/0"] = &config.Interface{Units: map[int]*config.Unit{0: {Family: map[string]*config.Family{"inet": {Addresses: []string{"192.0.2.1/24"}}}}}}
	cfg.RoutingOptions = &config.RoutingOptions{
		AutonomousSystem: 65000,
		RouterID:         "192.0.2.1",
	}
	cfg.Protocols = &config.ProtocolConfig{
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

	frrCfg, err := GenerateFRRConfig(cfg)
	if err != nil {
		t.Fatalf("GenerateFRRConfig() error = %v", err)
	}
	if got := frrCfg.BGP.Neighbors[0].BFDProfile; got != "fast" {
		t.Fatalf("BGP BFD profile = %q, want fast", got)
	}
	if got := frrCfg.OSPF.Interfaces[0].BFDProfile; got != "fast" {
		t.Fatalf("OSPF BFD profile = %q, want fast", got)
	}
}

func TestGenerateBFDConfigRejectsMultihopEchoMode(t *testing.T) {
	_, err := GenerateBFDConfig(&BFDConfig{
		Peers: []BFDPeer{{Address: "192.0.2.2", Multihop: true, EchoMode: true}},
	})
	if err == nil || !strings.Contains(err.Error(), "echo-mode") {
		t.Fatalf("GenerateBFDConfig() error = %v, want echo-mode multihop error", err)
	}
}
