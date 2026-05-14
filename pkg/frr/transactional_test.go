package frr

import (
	"context"
	"strings"
	"testing"
)

func TestParseBackendMode(t *testing.T) {
	for _, mode := range []string{"transactional", "file"} {
		if _, err := ParseBackendMode(mode); err != nil {
			t.Fatalf("ParseBackendMode(%q) error = %v", mode, err)
		}
	}
	if _, err := ParseBackendMode("mgmtd"); err == nil {
		t.Fatal("ParseBackendMode(mgmtd) error = nil, want unsupported mode")
	}
}

func TestBuildMgmtOperationsStaticAndBGP(t *testing.T) {
	cfg := &Config{
		StaticRoutes: []StaticRoute{
			{Prefix: "203.0.113.0/24", NextHop: "192.0.2.1", Distance: 10},
		},
		BGP: &BGPConfig{
			ASN:      65000,
			RouterID: "192.0.2.1",
			Neighbors: []BGPNeighbor{
				{IP: "198.51.100.2", RemoteAS: 65001, Description: "upstream peer", RouteMapIn: "IMPORT"},
			},
		},
	}

	ops, err := BuildMgmtOperations(cfg)
	if err != nil {
		t.Fatalf("BuildMgmtOperations() error = %v", err)
	}
	commands := commandsFromOps(ops)
	for _, want := range []string{
		"mgmt delete-config /frr-routing:routing/control-plane-protocols/control-plane-protocol[type='frr-staticd:staticd'][name='staticd'][vrf='default']",
		"mgmt set-config /frr-routing:routing/control-plane-protocols/control-plane-protocol[type='frr-staticd:staticd'][name='staticd'][vrf='default']/frr-staticd:staticd/route-list[prefix='203.0.113.0/24'][src-prefix='::/0'][afi-safi='frr-routing:ipv4-unicast']/path-list[table-id='0'][nh-type='ip4'][vrf='default'][gateway='192.0.2.1'][interface='']/distance 10",
		"mgmt set-config /frr-routing:routing/control-plane-protocols/control-plane-protocol[type='frr-bgp:bgp'][name='bgp'][vrf='default']/frr-bgp:bgp/global/local-as 65000",
		`mgmt set-config /frr-routing:routing/control-plane-protocols/control-plane-protocol[type='frr-bgp:bgp'][name='bgp'][vrf='default']/frr-bgp:bgp/neighbors/neighbor[remote-address='198.51.100.2']/description "upstream peer"`,
		"mgmt set-config /frr-routing:routing/control-plane-protocols/control-plane-protocol[type='frr-bgp:bgp'][name='bgp'][vrf='default']/frr-bgp:bgp/neighbors/neighbor[remote-address='198.51.100.2']/afi-safis/afi-safi[afi-safi-name='frr-routing:ipv4-unicast']/ipv4-unicast/filter-config/rmap-import IMPORT",
	} {
		if !strings.Contains(commands, want) {
			t.Fatalf("commands missing %q:\n%s", want, commands)
		}
	}
}

func TestBuildMgmtOperationsVRRP(t *testing.T) {
	ops, err := BuildMgmtOperations(&Config{
		VRRP: &VRRPConfig{Groups: []VRRPGroup{
			{ID: 10, Interface: "ge0-0-0", VirtualAddress: "192.0.2.254", Priority: 110, Preempt: true},
			{ID: 20, Interface: "ge0-0-0", VirtualAddress: "2001:db8::1"},
		}},
	})
	if err != nil {
		t.Fatalf("BuildMgmtOperations() error = %v", err)
	}
	commands := commandsFromOps(ops)
	for _, want := range []string{
		"mgmt delete-config /frr-interface:lib/interface/frr-vrrpd:vrrp",
		"mgmt set-config /frr-interface:lib/interface[name='ge0-0-0']/name ge0-0-0",
		"mgmt set-config /frr-interface:lib/interface[name='ge0-0-0']/frr-vrrpd:vrrp/vrrp-group[virtual-router-id='10']/virtual-router-id 10",
		"mgmt set-config /frr-interface:lib/interface[name='ge0-0-0']/frr-vrrpd:vrrp/vrrp-group[virtual-router-id='10']/version 3",
		"mgmt set-config /frr-interface:lib/interface[name='ge0-0-0']/frr-vrrpd:vrrp/vrrp-group[virtual-router-id='10']/priority 110",
		"mgmt set-config /frr-interface:lib/interface[name='ge0-0-0']/frr-vrrpd:vrrp/vrrp-group[virtual-router-id='10']/preempt true",
		"mgmt set-config /frr-interface:lib/interface[name='ge0-0-0']/frr-vrrpd:vrrp/vrrp-group[virtual-router-id='10']/v4/virtual-address 192.0.2.254",
		"mgmt set-config /frr-interface:lib/interface[name='ge0-0-0']/frr-vrrpd:vrrp/vrrp-group[virtual-router-id='20']/v6/virtual-address 2001:db8::1",
	} {
		if !strings.Contains(commands, want) {
			t.Fatalf("commands missing %q:\n%s", want, commands)
		}
	}
}

func TestBuildMgmtOperationsVRFVPN(t *testing.T) {
	ops, err := BuildMgmtOperations(&Config{
		VRFs: []VRFConfig{
			{
				Name:               "BLUE",
				ASN:                65000,
				RouteDistinguisher: "65000:100",
				ImportTargets:      []string{"65000:100", "65000:101"},
				ExportTargets:      []string{"65000:100", "65000:102"},
				ImportRouteMap:     "BLUE-IN",
				ExportRouteMap:     "BLUE-OUT",
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildMgmtOperations() error = %v", err)
	}
	commands := commandsFromOps(ops)
	for _, want := range []string{
		"mgmt delete-config /frr-vrf:lib",
		"mgmt delete-config /frr-routing:routing/control-plane-protocols/control-plane-protocol[type='frr-bgp:bgp']",
		"mgmt set-config /frr-vrf:lib/vrf[name='BLUE']/name BLUE",
		"mgmt set-config /frr-routing:routing/control-plane-protocols/control-plane-protocol[type='frr-bgp:bgp'][name='bgp'][vrf='BLUE']/vrf BLUE",
		"mgmt set-config /frr-routing:routing/control-plane-protocols/control-plane-protocol[type='frr-bgp:bgp'][name='bgp'][vrf='BLUE']/frr-bgp:bgp/global/local-as 65000",
		"mgmt set-config /frr-routing:routing/control-plane-protocols/control-plane-protocol[type='frr-bgp:bgp'][name='bgp'][vrf='BLUE']/frr-bgp:bgp/global/afi-safis/afi-safi[afi-safi-name='frr-routing:ipv4-unicast']/ipv4-unicast/vpn-config/rd 65000:100",
		"mgmt set-config /frr-routing:routing/control-plane-protocols/control-plane-protocol[type='frr-bgp:bgp'][name='bgp'][vrf='BLUE']/frr-bgp:bgp/global/afi-safis/afi-safi[afi-safi-name='frr-routing:ipv4-unicast']/ipv4-unicast/vpn-config/import-rt-list target:65000:101",
		"mgmt set-config /frr-routing:routing/control-plane-protocols/control-plane-protocol[type='frr-bgp:bgp'][name='bgp'][vrf='BLUE']/frr-bgp:bgp/global/afi-safis/afi-safi[afi-safi-name='frr-routing:ipv4-unicast']/ipv4-unicast/vpn-config/export-rt-list target:65000:102",
		"mgmt set-config /frr-routing:routing/control-plane-protocols/control-plane-protocol[type='frr-bgp:bgp'][name='bgp'][vrf='BLUE']/frr-bgp:bgp/global/afi-safis/afi-safi[afi-safi-name='frr-routing:ipv4-unicast']/ipv4-unicast/vpn-config/rmap-import BLUE-IN",
		"mgmt set-config /frr-routing:routing/control-plane-protocols/control-plane-protocol[type='frr-bgp:bgp'][name='bgp'][vrf='BLUE']/frr-bgp:bgp/global/afi-safis/afi-safi[afi-safi-name='frr-routing:ipv6-unicast']/ipv6-unicast/vpn-config/rmap-export BLUE-OUT",
	} {
		if !strings.Contains(commands, want) {
			t.Fatalf("commands missing %q:\n%s", want, commands)
		}
	}
}

func TestBuildMgmtOperationsRejectsOSPF3(t *testing.T) {
	_, err := BuildMgmtOperations(&Config{
		OSPF3: &OSPFConfig{
			IsOSPFv3: true,
			Interfaces: []OSPFInterface{
				{Name: "ge0-0-0", AreaID: "0.0.0.0"},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "OSPFv3 is not supported") {
		t.Fatalf("BuildMgmtOperations() error = %v, want OSPFv3 unsupported", err)
	}
}

func TestBuildMgmtOperationsRejectsBFD(t *testing.T) {
	_, err := BuildMgmtOperations(&Config{
		BFD: &BFDConfig{
			Peers: []BFDPeer{{Address: "192.0.2.2"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "BFD is not supported") {
		t.Fatalf("BuildMgmtOperations() error = %v, want BFD unsupported", err)
	}
}

func TestBuildMgmtOperationsRejectsBFDProtocolBindings(t *testing.T) {
	_, err := BuildMgmtOperations(&Config{
		BGP: &BGPConfig{
			ASN: 65000,
			Neighbors: []BGPNeighbor{
				{IP: "192.0.2.2", RemoteAS: 65001, BFD: true, BFDProfile: "fast"},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "BFD protocol bindings") {
		t.Fatalf("BuildMgmtOperations() error = %v, want BFD protocol binding unsupported", err)
	}
}

func TestVtyshMgmtClientApplySequence(t *testing.T) {
	var got []string
	client := NewVtyshMgmtClientWithRunner(func(ctx context.Context, command string) ([]byte, error) {
		got = append(got, command)
		return nil, nil
	})

	err := client.Apply(context.Background(), []MgmtOperation{
		setOp("/x", "1"),
		deleteOp("/y"),
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	want := []string{
		"mgmt commit abort",
		"mgmt set-config /x 1",
		"mgmt delete-config /y",
		"mgmt commit check",
		"mgmt commit apply",
		"write memory",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands = %#v, want %#v", got, want)
	}
}

func commandsFromOps(ops []MgmtOperation) string {
	commands := make([]string, 0, len(ops))
	for _, op := range ops {
		commands = append(commands, op.Command())
	}
	return strings.Join(commands, "\n")
}
