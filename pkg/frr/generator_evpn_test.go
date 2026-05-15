package frr

import (
	"strings"
	"testing"

	"github.com/akam1o/arca-router/pkg/config"
)

func TestGenerateBGPConfigWithEVPN(t *testing.T) {
	got, err := GenerateBGPConfig(&BGPConfig{
		ASN:      65000,
		RouterID: "192.0.2.1",
		Neighbors: []BGPNeighbor{
			{IP: "192.0.2.2", RemoteAS: 65000},
			{IP: "2001:db8::2", RemoteAS: 65000, IsIPv6: true},
		},
		EVPN: &EVPNConfig{VNIs: []EVPNVNI{
			{
				VNI:           10010,
				Type:          "l2",
				BridgeDomain:  "BD-10",
				ImportTargets: []string{"65000:10010"},
				ExportTargets: []string{"65000:10010", "65000:10011"},
			},
			{
				VNI:             20010,
				Type:            "l3",
				RoutingInstance: "BLUE",
			},
		}},
	})
	if err != nil {
		t.Fatalf("GenerateBGPConfig() error = %v", err)
	}
	for _, want := range []string{
		"address-family l2vpn evpn",
		"neighbor 192.0.2.2 activate",
		"neighbor 2001:db8::2 activate",
		"advertise-all-vni",
		"vni 10010",
		"route-target import 65000:10010",
		"route-target export 65000:10010 65000:10011",
		"exit-vni",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("BGP config missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "vni 20010") {
		t.Fatalf("global BGP EVPN config should not render L3 VNI route-target block:\n%s", got)
	}
}

func TestGenerateFRRConfigConvertsEVPNVNIs(t *testing.T) {
	cfg := &config.Config{
		Interfaces: map[string]*config.Interface{
			"ge-0/0/0": {Units: map[int]*config.Unit{}},
		},
		RoutingOptions: &config.RoutingOptions{AutonomousSystem: 65000, RouterID: "192.0.2.1"},
		Protocols: &config.ProtocolConfig{
			EVPN: &config.EVPNConfig{VNIs: map[int]*config.EVPNVNI{
				10010: {
					VNI:             10010,
					Type:            "l2",
					BridgeDomain:    "BD-10",
					VRFTarget:       "target:65000:10010",
					SourceInterface: "ge-0/0/0",
				},
				20010: {
					VNI:             20010,
					Type:            "l3",
					RoutingInstance: "BLUE",
					VRFTargetImport: []string{"target:65000:20001"},
					VRFTargetExport: []string{"target:65000:20002"},
				},
			}},
		},
		RoutingInstances: map[string]*config.RoutingInstance{
			"BLUE": {Name: "BLUE", InstanceType: "vrf"},
		},
	}

	frrCfg, err := GenerateFRRConfig(cfg)
	if err != nil {
		t.Fatalf("GenerateFRRConfig() error = %v", err)
	}
	if frrCfg.BGP == nil || frrCfg.BGP.EVPN == nil {
		t.Fatalf("BGP EVPN config = %#v, want generated", frrCfg.BGP)
	}
	if len(frrCfg.VRFs) != 1 || frrCfg.VRFs[0].VNI != 20010 || frrCfg.VRFs[0].EVPN == nil {
		t.Fatalf("VRFs = %#v, want BLUE with L3 VNI EVPN config", frrCfg.VRFs)
	}
	if got := frrCfg.BGP.EVPN.VNIs[0].SourceInterface; got != "ge0-0-0" {
		t.Fatalf("EVPN source interface = %q, want converted Linux name", got)
	}

	text, err := GenerateFRRConfigFile(frrCfg)
	if err != nil {
		t.Fatalf("GenerateFRRConfigFile() error = %v", err)
	}
	for _, want := range []string{
		"router bgp 65000",
		" address-family l2vpn evpn",
		"  advertise-all-vni",
		"  vni 10010",
		"   route-target import 65000:10010",
		"   route-target export 65000:10010",
		"vrf BLUE",
		" vni 20010",
		"router bgp 65000 vrf BLUE",
		"  advertise ipv4 unicast",
		"  advertise ipv6 unicast",
		"  route-target import 65000:20001",
		"  route-target export 65000:20002",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("FRR config missing %q:\n%s", want, text)
		}
	}
}

func TestGenerateFRRConfigRejectsEVPNWithoutASN(t *testing.T) {
	_, err := GenerateFRRConfig(&config.Config{
		Protocols: &config.ProtocolConfig{
			EVPN: &config.EVPNConfig{VNIs: map[int]*config.EVPNVNI{
				10010: {VNI: 10010, Type: "l2", BridgeDomain: "BD-10"},
			}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "EVPN requires autonomous-system") {
		t.Fatalf("GenerateFRRConfig() error = %v, want autonomous-system error", err)
	}
}

func TestGenerateFRRConfigRejectsUnknownEVPNRoutingInstance(t *testing.T) {
	_, err := GenerateFRRConfig(&config.Config{
		RoutingOptions: &config.RoutingOptions{AutonomousSystem: 65000},
		Protocols: &config.ProtocolConfig{
			EVPN: &config.EVPNConfig{VNIs: map[int]*config.EVPNVNI{
				20010: {VNI: 20010, Type: "l3", RoutingInstance: "BLUE"},
			}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown routing-instance BLUE") {
		t.Fatalf("GenerateFRRConfig() error = %v, want routing-instance error", err)
	}
}
