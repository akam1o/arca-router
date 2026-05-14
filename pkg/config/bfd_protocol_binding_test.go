package config

import (
	"strings"
	"testing"
)

func TestBFDProtocolBindingsParseValidateAndSerialize(t *testing.T) {
	input := strings.Join([]string{
		"set routing-options autonomous-system 65000",
		"set routing-options router-id 192.0.2.1",
		"set interfaces ge-0/0/0 unit 0 family inet address 192.0.2.1/24",
		"set protocols bfd profile fast receive-interval 150",
		"set protocols bgp group EBGP type external",
		"set protocols bgp group EBGP neighbor 192.0.2.2 peer-as 65001",
		"set protocols bgp group EBGP neighbor 192.0.2.2 bfd profile fast",
		"set protocols ospf area 0.0.0.0 interface ge-0/0/0 bfd profile fast",
		"set protocols ospf3 area 0.0.0.0 interface ge-0/0/0 bfd profile fast",
	}, "\n")

	cfg, err := NewParser(strings.NewReader(input)).Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	neighbor := cfg.Protocols.BGP.Groups["EBGP"].Neighbors["192.0.2.2"]
	if neighbor == nil || !neighbor.BFD || neighbor.BFDProfile != "fast" {
		t.Fatalf("BGP BFD binding = %#v, want profile fast", neighbor)
	}
	ospfIface := cfg.Protocols.OSPF.Areas["0.0.0.0"].Interfaces["ge-0/0/0"]
	if ospfIface == nil || !ospfIface.BFD || ospfIface.BFDProfile != "fast" {
		t.Fatalf("OSPF BFD binding = %#v, want profile fast", ospfIface)
	}
	ospf3Iface := cfg.Protocols.OSPF3.Areas["0.0.0.0"].Interfaces["ge-0/0/0"]
	if ospf3Iface == nil || !ospf3Iface.BFD || ospf3Iface.BFDProfile != "fast" {
		t.Fatalf("OSPF3 BFD binding = %#v, want profile fast", ospf3Iface)
	}

	got := ToSetCommands(cfg)
	for _, want := range []string{
		"set protocols bgp group EBGP neighbor 192.0.2.2 bfd profile fast\n",
		"set protocols ospf area 0.0.0.0 interface ge-0/0/0 bfd profile fast\n",
		"set protocols ospf3 area 0.0.0.0 interface ge-0/0/0 bfd profile fast\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("ToSetCommands() missing %q:\n%s", want, got)
		}
	}
}

func TestValidateBFDProtocolBindingsRejectUnknownProfile(t *testing.T) {
	cfg := NewConfig()
	cfg.RoutingOptions = &RoutingOptions{AutonomousSystem: 65000, RouterID: "192.0.2.1"}
	cfg.Interfaces["ge-0/0/0"] = &Interface{}
	cfg.Protocols = &ProtocolConfig{
		BGP: &BGPConfig{Groups: map[string]*BGPGroup{
			"EBGP": {
				Type: "external",
				Neighbors: map[string]*BGPNeighbor{
					"192.0.2.2": {IP: "192.0.2.2", PeerAS: 65001, BFD: true, BFDProfile: "missing"},
				},
			},
		}},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("Validate() error = %v, want missing BFD profile error", err)
	}
}
