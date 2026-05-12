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
