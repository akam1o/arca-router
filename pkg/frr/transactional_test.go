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
				{IP: "198.51.100.2", RemoteAS: 65001, Description: "upstream peer", RouteMapIn: "IMPORT", BFD: true},
			},
		},
		RouteMaps: []RouteMap{
			{Name: "IMPORT", Entries: []RouteMapEntry{{Seq: 10, Action: "permit"}}},
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
		"mgmt set-config /frr-routing:routing/control-plane-protocols/control-plane-protocol[type='frr-bgp:bgp'][name='bgp'][vrf='default']/frr-bgp:bgp/neighbors/neighbor[remote-address='198.51.100.2']/bfd-options/enable true",
		"mgmt set-config /frr-routing:routing/control-plane-protocols/control-plane-protocol[type='frr-bgp:bgp'][name='bgp'][vrf='default']/frr-bgp:bgp/neighbors/neighbor[remote-address='198.51.100.2']/afi-safis/afi-safi[afi-safi-name='frr-routing:ipv4-unicast']/ipv4-unicast/filter-config/rmap-import IMPORT",
	} {
		if !strings.Contains(commands, want) {
			t.Fatalf("commands missing %q:\n%s", want, commands)
		}
	}
}

func TestBuildMgmtOperationsRejectsInvalidBGP(t *testing.T) {
	tests := []struct {
		name string
		bgp  *BGPConfig
		want string
	}{
		{
			name: "missing asn",
			bgp: &BGPConfig{
				Neighbors: []BGPNeighbor{{IP: "192.0.2.2", RemoteAS: 65001}},
			},
			want: "BGP ASN is required",
		},
		{
			name: "invalid router id",
			bgp: &BGPConfig{
				ASN:      65000,
				RouterID: "2001:db8::1",
				Neighbors: []BGPNeighbor{
					{IP: "192.0.2.2", RemoteAS: 65001},
				},
			},
			want: "invalid BGP router-id",
		},
		{
			name: "missing neighbor ip",
			bgp: &BGPConfig{
				ASN:       65000,
				Neighbors: []BGPNeighbor{{RemoteAS: 65001}},
			},
			want: "BGP neighbor IP is required",
		},
		{
			name: "invalid neighbor ip",
			bgp: &BGPConfig{
				ASN:       65000,
				Neighbors: []BGPNeighbor{{IP: "not-an-ip", RemoteAS: 65001}},
			},
			want: "invalid BGP neighbor IP",
		},
		{
			name: "missing remote as",
			bgp: &BGPConfig{
				ASN:       65000,
				Neighbors: []BGPNeighbor{{IP: "192.0.2.2"}},
			},
			want: "remote-as is required",
		},
		{
			name: "ipv4 marked ipv6",
			bgp: &BGPConfig{
				ASN:       65000,
				Neighbors: []BGPNeighbor{{IP: "192.0.2.2", RemoteAS: 65001, IsIPv6: true}},
			},
			want: "address family does not match",
		},
		{
			name: "ipv6 marked ipv4",
			bgp: &BGPConfig{
				ASN:       65000,
				Neighbors: []BGPNeighbor{{IP: "2001:db8::2", RemoteAS: 65001}},
			},
			want: "address family does not match",
		},
		{
			name: "duplicate neighbor",
			bgp: &BGPConfig{
				ASN: 65000,
				Neighbors: []BGPNeighbor{
					{IP: "192.0.2.2", RemoteAS: 65001},
					{IP: "192.0.2.2", RemoteAS: 65002},
				},
			},
			want: "BGP neighbor 192.0.2.2 is duplicated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildMgmtOperations(&Config{BGP: tt.bgp})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("BuildMgmtOperations() error = %v, want %q", err, tt.want)
			}
		})
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

func TestBuildMgmtOperationsIPv6RouteMapPrefixList(t *testing.T) {
	ops, err := BuildMgmtOperations(&Config{
		PrefixLists: []PrefixList{
			{
				Name:   "V6-IN",
				IsIPv6: true,
				Entries: []PrefixListEntry{
					{Seq: 10, Action: "permit", Prefix: "2001:db8::/32"},
				},
			},
		},
		RouteMaps: []RouteMap{
			{
				Name: "IMPORT-V6",
				Entries: []RouteMapEntry{
					{Seq: 10, Action: "permit", MatchPrefixLists: []string{"V6-IN"}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildMgmtOperations() error = %v", err)
	}
	commands := commandsFromOps(ops)
	for _, want := range []string{
		"mgmt set-config /frr-filter:lib/prefix-list[type='ipv6'][name='V6-IN']/entry[sequence='10']/ipv6-prefix 2001:db8::/32",
		"mgmt set-config /frr-route-map:lib/route-map[name='IMPORT-V6']/entry[sequence='10']/match-condition[condition='frr-route-map:ipv6-prefix-list']/condition frr-route-map:ipv6-prefix-list",
		"mgmt set-config /frr-route-map:lib/route-map[name='IMPORT-V6']/entry[sequence='10']/match-condition[condition='frr-route-map:ipv6-prefix-list']/rmap-match-condition/list-name V6-IN",
	} {
		if !strings.Contains(commands, want) {
			t.Fatalf("commands missing %q:\n%s", want, commands)
		}
	}
}

func TestBuildMgmtOperationsAggregatesRouteMapPrefixLists(t *testing.T) {
	ops, err := BuildMgmtOperations(&Config{
		PrefixLists: []PrefixList{
			{
				Name: "V4-A",
				Entries: []PrefixListEntry{
					{Seq: 10, Action: "permit", Prefix: "192.0.2.0/24"},
				},
			},
			{
				Name: "V4-B",
				Entries: []PrefixListEntry{
					{Seq: 10, Action: "permit", Prefix: "198.51.100.0/24"},
				},
			},
		},
		RouteMaps: []RouteMap{
			{
				Name: "IMPORT",
				Entries: []RouteMapEntry{
					{Seq: 10, Action: "permit", MatchPrefixLists: []string{"V4-A", "V4-B"}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildMgmtOperations() error = %v", err)
	}
	commands := commandsFromOps(ops)
	for _, want := range []string{
		"mgmt set-config /frr-filter:lib/prefix-list[type='ipv4'][name='ARCA-IMPORT-10-V4']/entry[sequence='10']/ipv4-prefix 192.0.2.0/24",
		"mgmt set-config /frr-filter:lib/prefix-list[type='ipv4'][name='ARCA-IMPORT-10-V4']/entry[sequence='20']/ipv4-prefix 198.51.100.0/24",
		"mgmt set-config /frr-route-map:lib/route-map[name='IMPORT']/entry[sequence='10']/match-condition[condition='frr-route-map:ipv4-prefix-list']/rmap-match-condition/list-name ARCA-IMPORT-10-V4",
	} {
		if !strings.Contains(commands, want) {
			t.Fatalf("commands missing %q:\n%s", want, commands)
		}
	}
	for _, unexpected := range []string{
		"mgmt set-config /frr-route-map:lib/route-map[name='IMPORT']/entry[sequence='10']/match-condition[condition='frr-route-map:ipv4-prefix-list']/rmap-match-condition/list-name V4-A",
		"mgmt set-config /frr-route-map:lib/route-map[name='IMPORT']/entry[sequence='10']/match-condition[condition='frr-route-map:ipv4-prefix-list']/rmap-match-condition/list-name V4-B",
	} {
		if strings.Contains(commands, unexpected) {
			t.Fatalf("commands retained unaggregated match %q:\n%s", unexpected, commands)
		}
	}
}

func TestBuildMgmtOperationsRejectsInvalidPolicyObjects(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{
			name: "empty prefix-list name",
			cfg: &Config{PrefixLists: []PrefixList{
				{Name: ""},
			}},
			want: "prefix-list name is required",
		},
		{
			name: "invalid prefix-list sequence",
			cfg: &Config{PrefixLists: []PrefixList{
				{Name: "CUSTOMER", Entries: []PrefixListEntry{{Action: "permit", Prefix: "192.0.2.0/24"}}},
			}},
			want: "entry sequence must be positive",
		},
		{
			name: "duplicate prefix-list",
			cfg: &Config{PrefixLists: []PrefixList{
				{Name: "CUSTOMER"},
				{Name: "CUSTOMER"},
			}},
			want: "prefix-list CUSTOMER is duplicated",
		},
		{
			name: "duplicate prefix-list sequence",
			cfg: &Config{PrefixLists: []PrefixList{
				{Name: "CUSTOMER", Entries: []PrefixListEntry{
					{Seq: 10, Action: "permit", Prefix: "192.0.2.0/24"},
					{Seq: 10, Action: "deny", Prefix: "198.51.100.0/24"},
				}},
			}},
			want: "prefix-list CUSTOMER entry 10 is duplicated",
		},
		{
			name: "invalid prefix-list action",
			cfg: &Config{PrefixLists: []PrefixList{
				{Name: "CUSTOMER", Entries: []PrefixListEntry{{Seq: 10, Action: "drop", Prefix: "192.0.2.0/24"}}},
			}},
			want: "invalid action drop",
		},
		{
			name: "invalid prefix-list prefix",
			cfg: &Config{PrefixLists: []PrefixList{
				{Name: "CUSTOMER", Entries: []PrefixListEntry{{Seq: 10, Action: "permit", Prefix: "192.0.2.0"}}},
			}},
			want: "invalid prefix 192.0.2.0",
		},
		{
			name: "prefix-list family mismatch",
			cfg: &Config{PrefixLists: []PrefixList{
				{Name: "CUSTOMER-V6", IsIPv6: true, Entries: []PrefixListEntry{{Seq: 10, Action: "permit", Prefix: "192.0.2.0/24"}}},
			}},
			want: "address family does not match",
		},
		{
			name: "invalid route-map sequence",
			cfg: &Config{RouteMaps: []RouteMap{
				{Name: "IMPORT", Entries: []RouteMapEntry{{Action: "permit"}}},
			}},
			want: "entry sequence must be positive",
		},
		{
			name: "duplicate route-map",
			cfg: &Config{RouteMaps: []RouteMap{
				{Name: "IMPORT", Entries: []RouteMapEntry{{Seq: 10, Action: "permit"}}},
				{Name: "IMPORT", Entries: []RouteMapEntry{{Seq: 20, Action: "deny"}}},
			}},
			want: "route-map IMPORT is duplicated",
		},
		{
			name: "duplicate route-map sequence",
			cfg: &Config{RouteMaps: []RouteMap{
				{Name: "IMPORT", Entries: []RouteMapEntry{
					{Seq: 10, Action: "permit"},
					{Seq: 10, Action: "deny"},
				}},
			}},
			want: "route-map IMPORT entry 10 is duplicated",
		},
		{
			name: "invalid route-map action",
			cfg: &Config{RouteMaps: []RouteMap{
				{Name: "IMPORT", Entries: []RouteMapEntry{{Seq: 10, Action: "drop"}}},
			}},
			want: "invalid action drop",
		},
		{
			name: "empty route-map prefix-list reference",
			cfg: &Config{RouteMaps: []RouteMap{
				{Name: "IMPORT", Entries: []RouteMapEntry{{Seq: 10, Action: "permit", MatchPrefixLists: []string{""}}}},
			}},
			want: "references empty prefix-list",
		},
		{
			name: "unknown route-map prefix-list reference",
			cfg: &Config{RouteMaps: []RouteMap{
				{Name: "IMPORT", Entries: []RouteMapEntry{{Seq: 10, Action: "permit", MatchPrefixLists: []string{"MISSING"}}}},
			}},
			want: "references unknown prefix-list MISSING",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildMgmtOperations(tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("BuildMgmtOperations() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestBuildMgmtOperationsRejectsUnsupportedRouteMapMatches(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *Config
		wantText string
	}{
		{
			name: "source protocol match",
			cfg: &Config{RouteMaps: []RouteMap{
				{Name: "IMPORT", Entries: []RouteMapEntry{{Seq: 10, Action: "permit", MatchProtocol: "bgp"}}},
			}},
			wantText: "match source-protocol",
		},
		{
			name: "peer match",
			cfg: &Config{RouteMaps: []RouteMap{
				{Name: "IMPORT", Entries: []RouteMapEntry{{Seq: 10, Action: "permit", MatchNeighbor: "192.0.2.2"}}},
			}},
			wantText: "match peer",
		},
		{
			name: "as path match",
			cfg: &Config{RouteMaps: []RouteMap{
				{Name: "IMPORT", Entries: []RouteMapEntry{{Seq: 10, Action: "permit", MatchASPath: "AS-PATH-1"}}},
			}},
			wantText: "match as-path",
		},
		{
			name: "as path access list",
			cfg: &Config{ASPathAccessLists: []ASPathAccessList{
				{Name: "AS-PATH-1", Entries: []ASPathAccessListEntry{{Seq: 10, Action: "permit", Regex: "^65001"}}},
			}},
			wantText: "AS-path access-lists",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildMgmtOperations(tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.wantText) {
				t.Fatalf("BuildMgmtOperations() error = %v, want %q", err, tt.wantText)
			}
		})
	}
}

func TestBuildMgmtOperationsVRFVPN(t *testing.T) {
	ops, err := BuildMgmtOperations(&Config{
		RouteMaps: []RouteMap{
			{Name: "BLUE-IN", Entries: []RouteMapEntry{{Seq: 10, Action: "permit"}}},
			{Name: "BLUE-OUT", Entries: []RouteMapEntry{{Seq: 10, Action: "permit"}}},
		},
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

func TestBuildMgmtOperationsRejectsUnknownRouteMapReferences(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{
			name: "bgp import",
			cfg: &Config{BGP: &BGPConfig{ASN: 65000, Neighbors: []BGPNeighbor{
				{IP: "192.0.2.2", RemoteAS: 65001, RouteMapIn: "MISSING-IN"},
			}}},
			want: "BGP neighbor 192.0.2.2 import references unknown route-map MISSING-IN",
		},
		{
			name: "bgp export",
			cfg: &Config{BGP: &BGPConfig{ASN: 65000, Neighbors: []BGPNeighbor{
				{IP: "192.0.2.2", RemoteAS: 65001, RouteMapOut: "MISSING-OUT"},
			}}},
			want: "BGP neighbor 192.0.2.2 export references unknown route-map MISSING-OUT",
		},
		{
			name: "vrf import",
			cfg: &Config{VRFs: []VRFConfig{
				{Name: "BLUE", ASN: 65000, ImportTargets: []string{"65000:100"}, ImportRouteMap: "MISSING-IN"},
			}},
			want: "VRF BLUE import references unknown route-map MISSING-IN",
		},
		{
			name: "vrf export",
			cfg: &Config{VRFs: []VRFConfig{
				{Name: "BLUE", ASN: 65000, RouteDistinguisher: "65000:100", ExportTargets: []string{"65000:100"}, ExportRouteMap: "MISSING-OUT"},
			}},
			want: "VRF BLUE export references unknown route-map MISSING-OUT",
		},
		{
			name: "empty route-map name",
			cfg:  &Config{RouteMaps: []RouteMap{{Name: ""}}},
			want: "route-map name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildMgmtOperations(tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("BuildMgmtOperations() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestBuildMgmtOperationsRejectsInvalidVRFVPN(t *testing.T) {
	tests := []struct {
		name string
		vrf  VRFConfig
		want string
	}{
		{
			name: "empty name",
			vrf:  VRFConfig{Name: ""},
			want: "VRF name is required",
		},
		{
			name: "missing asn",
			vrf:  VRFConfig{Name: "BLUE", ImportTargets: []string{"65000:100"}},
			want: "BGP ASN is required",
		},
		{
			name: "export without rd",
			vrf:  VRFConfig{Name: "BLUE", ASN: 65000, ExportTargets: []string{"65000:100"}},
			want: "route-distinguisher is required",
		},
		{
			name: "invalid rd",
			vrf:  VRFConfig{Name: "BLUE", ASN: 65000, RouteDistinguisher: "bad", ExportTargets: []string{"65000:100"}},
			want: "invalid route-distinguisher",
		},
		{
			name: "invalid import target",
			vrf:  VRFConfig{Name: "BLUE", ASN: 65000, ImportTargets: []string{"bad"}},
			want: "invalid import route-target",
		},
		{
			name: "import route-map without target",
			vrf:  VRFConfig{Name: "BLUE", ASN: 65000, ImportRouteMap: "BLUE-IN"},
			want: "route-map import requires an import route-target",
		},
		{
			name: "export route-map without target",
			vrf:  VRFConfig{Name: "BLUE", ASN: 65000, ExportRouteMap: "BLUE-OUT"},
			want: "route-map export requires an export route-target",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildMgmtOperations(&Config{VRFs: []VRFConfig{tt.vrf}})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("BuildMgmtOperations() error = %v, want %q", err, tt.want)
			}
		})
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

func TestBuildMgmtOperationsBFD(t *testing.T) {
	ops, err := BuildMgmtOperations(&Config{
		VRFs: []VRFConfig{{Name: "BLUE"}},
		BFD: &BFDConfig{
			Profiles: []BFDProfile{
				{Name: "fast", DetectMultiplier: 3, ReceiveInterval: 150, TransmitInterval: 150, EchoMode: true, PassiveMode: true},
				{Name: "slow", DetectMultiplier: 5},
			},
			Peers: []BFDPeer{
				{
					Address:          "192.0.2.2",
					Interface:        "ge0-0-0",
					LocalAddress:     "192.0.2.1",
					Profile:          "fast",
					DetectMultiplier: 4,
					ReceiveInterval:  200,
					TransmitInterval: 250,
					PassiveMode:      true,
				},
				{
					Address:      "192.0.2.3",
					LocalAddress: "192.0.2.1",
					VRF:          "BLUE",
					Multihop:     true,
					Profile:      "slow",
					Shutdown:     true,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildMgmtOperations() error = %v", err)
	}
	commands := commandsFromOps(ops)
	for _, want := range []string{
		"mgmt delete-config /frr-bfdd:bfdd",
		"mgmt set-config /frr-bfdd:bfdd/bfd/profile[name='fast']/name fast",
		"mgmt set-config /frr-bfdd:bfdd/bfd/profile[name='fast']/detection-multiplier 3",
		"mgmt set-config /frr-bfdd:bfdd/bfd/profile[name='fast']/desired-transmission-interval 150000",
		"mgmt set-config /frr-bfdd:bfdd/bfd/profile[name='fast']/required-receive-interval 150000",
		"mgmt set-config /frr-bfdd:bfdd/bfd/profile[name='fast']/echo-mode true",
		"mgmt set-config /frr-bfdd:bfdd/bfd/profile[name='fast']/passive-mode true",
		"mgmt set-config /frr-bfdd:bfdd/bfd/profile[name='slow']/detection-multiplier 5",
		"mgmt set-config /frr-bfdd:bfdd/bfd/sessions/single-hop[dest-addr='192.0.2.2'][interface='ge0-0-0'][vrf='default']/source-addr 192.0.2.1",
		"mgmt set-config /frr-bfdd:bfdd/bfd/sessions/single-hop[dest-addr='192.0.2.2'][interface='ge0-0-0'][vrf='default']/profile fast",
		"mgmt set-config /frr-bfdd:bfdd/bfd/sessions/single-hop[dest-addr='192.0.2.2'][interface='ge0-0-0'][vrf='default']/detection-multiplier 4",
		"mgmt set-config /frr-bfdd:bfdd/bfd/sessions/single-hop[dest-addr='192.0.2.2'][interface='ge0-0-0'][vrf='default']/desired-transmission-interval 250000",
		"mgmt set-config /frr-bfdd:bfdd/bfd/sessions/single-hop[dest-addr='192.0.2.2'][interface='ge0-0-0'][vrf='default']/required-receive-interval 200000",
		"mgmt set-config /frr-bfdd:bfdd/bfd/sessions/multi-hop[source-addr='192.0.2.1'][dest-addr='192.0.2.3'][vrf='BLUE']/profile slow",
		"mgmt set-config /frr-bfdd:bfdd/bfd/sessions/multi-hop[source-addr='192.0.2.1'][dest-addr='192.0.2.3'][vrf='BLUE']/administrative-down true",
	} {
		if !strings.Contains(commands, want) {
			t.Fatalf("commands missing %q:\n%s", want, commands)
		}
	}
}

func TestBuildMgmtOperationsRejectsUnknownBFDPeerVRF(t *testing.T) {
	_, err := BuildMgmtOperations(&Config{
		BFD: &BFDConfig{Peers: []BFDPeer{
			{Address: "192.0.2.2", Interface: "ge0-0-0", VRF: "BLUE"},
		}},
	})
	if err == nil || !strings.Contains(err.Error(), "BFD peer 192.0.2.2 references unknown VRF BLUE") {
		t.Fatalf("BuildMgmtOperations() error = %v, want unknown BFD peer VRF", err)
	}
}

func TestBuildMgmtOperationsRejectsDuplicateBFDObjects(t *testing.T) {
	tests := []struct {
		name string
		bfd  *BFDConfig
		want string
	}{
		{
			name: "profile",
			bfd: &BFDConfig{Profiles: []BFDProfile{
				{Name: "fast"},
				{Name: "fast", DetectMultiplier: 3},
			}},
			want: "BFD profile fast is duplicated",
		},
		{
			name: "peer",
			bfd: &BFDConfig{Peers: []BFDPeer{
				{Address: "192.0.2.2", Interface: "ge0-0-0"},
				{Address: "192.0.2.2", Interface: "ge0-0-1"},
			}},
			want: "BFD peer 192.0.2.2 is duplicated",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildMgmtOperations(&Config{BFD: tt.bfd})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("BuildMgmtOperations() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestBuildMgmtOperationsRejectsUnsupportedBFDPeerShape(t *testing.T) {
	for _, tt := range []struct {
		name string
		peer BFDPeer
		want string
	}{
		{name: "single hop without interface", peer: BFDPeer{Address: "192.0.2.2"}, want: "requires interface"},
		{name: "multihop without local address", peer: BFDPeer{Address: "192.0.2.2", Multihop: true}, want: "requires local-address"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildMgmtOperations(&Config{BFD: &BFDConfig{Peers: []BFDPeer{tt.peer}}})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("BuildMgmtOperations() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestBuildMgmtOperationsRejectsUnsupportedBFDProtocolBindings(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{
			name: "bgp profile",
			cfg: &Config{
				BGP: &BGPConfig{
					ASN: 65000,
					Neighbors: []BGPNeighbor{
						{IP: "192.0.2.2", RemoteAS: 65001, BFDProfile: "fast"},
					},
				},
			},
			want: "BGP BFD profiles",
		},
		{
			name: "ospf",
			cfg: &Config{
				OSPF: &OSPFConfig{
					Interfaces: []OSPFInterface{{Name: "ge0-0-0", BFD: true, BFDProfile: "fast"}},
				},
			},
			want: "OSPF BFD profiles",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildMgmtOperations(tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("BuildMgmtOperations() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestBuildMgmtOperationsOSPFInterfaceAttributes(t *testing.T) {
	priority := 10
	ops, err := BuildMgmtOperations(&Config{
		OSPF: &OSPFConfig{
			RouterID: "192.0.2.1",
			Networks: []OSPFNetwork{
				{Prefix: "192.0.2.0/24", AreaID: "0.0.0.0"},
			},
			Interfaces: []OSPFInterface{
				{Name: "ge0-0-0", AreaID: "0.0.0.0", Passive: true, Metric: 20, Priority: &priority, BFD: true},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildMgmtOperations() error = %v", err)
	}
	commands := commandsFromOps(ops)
	for _, want := range []string{
		"mgmt delete-config /frr-interface:lib/interface/frr-ospfd:ospf",
		"mgmt set-config /frr-routing:routing/control-plane-protocols/control-plane-protocol[type='frr-ospfd:ospf'][name='ospf'][vrf='default']/frr-ospfd:ospf/router-id 192.0.2.1",
		"mgmt set-config /frr-routing:routing/control-plane-protocols/control-plane-protocol[type='frr-ospfd:ospf'][name='ospf'][vrf='default']/frr-ospfd:ospf/network[prefix='192.0.2.0/24']/area 0.0.0.0",
		"mgmt set-config /frr-routing:routing/control-plane-protocols/control-plane-protocol[type='frr-ospfd:ospf'][name='ospf'][vrf='default']/frr-ospfd:ospf/passive-interface[interface='ge0-0-0']/interface ge0-0-0",
		"mgmt set-config /frr-interface:lib/interface[name='ge0-0-0']/name ge0-0-0",
		"mgmt set-config /frr-interface:lib/interface[name='ge0-0-0']/frr-ospfd:ospf/instance[id='0']/id 0",
		"mgmt set-config /frr-interface:lib/interface[name='ge0-0-0']/frr-ospfd:ospf/instance[id='0']/cost 20",
		"mgmt set-config /frr-interface:lib/interface[name='ge0-0-0']/frr-ospfd:ospf/instance[id='0']/priority 10",
		"mgmt set-config /frr-interface:lib/interface[name='ge0-0-0']/frr-ospfd:ospf/instance[id='0']/bfd true",
	} {
		if !strings.Contains(commands, want) {
			t.Fatalf("commands missing %q:\n%s", want, commands)
		}
	}
}

func TestBuildMgmtOperationsRejectsInvalidOSPF(t *testing.T) {
	priorityTooHigh := 256
	tests := []struct {
		name string
		ospf *OSPFConfig
		want string
	}{
		{
			name: "ospfv3 in ospf slot",
			ospf: &OSPFConfig{IsOSPFv3: true},
			want: "OSPFv3 is not supported",
		},
		{
			name: "missing router id",
			ospf: &OSPFConfig{Networks: []OSPFNetwork{{Prefix: "192.0.2.0/24", AreaID: "0.0.0.0"}}},
			want: "OSPF router-id is required",
		},
		{
			name: "invalid router id",
			ospf: &OSPFConfig{RouterID: "2001:db8::1"},
			want: "OSPF router-id must be IPv4",
		},
		{
			name: "missing network prefix",
			ospf: &OSPFConfig{RouterID: "192.0.2.1", Networks: []OSPFNetwork{{AreaID: "0.0.0.0"}}},
			want: "OSPF network prefix is required",
		},
		{
			name: "invalid network prefix",
			ospf: &OSPFConfig{RouterID: "192.0.2.1", Networks: []OSPFNetwork{{Prefix: "192.0.2.0", AreaID: "0.0.0.0"}}},
			want: "invalid OSPF network prefix",
		},
		{
			name: "ipv6 network",
			ospf: &OSPFConfig{RouterID: "192.0.2.1", Networks: []OSPFNetwork{{Prefix: "2001:db8::/64", AreaID: "0.0.0.0"}}},
			want: "address family does not match OSPFv2",
		},
		{
			name: "duplicate network",
			ospf: &OSPFConfig{RouterID: "192.0.2.1", Networks: []OSPFNetwork{
				{Prefix: "192.0.2.0/24", AreaID: "0.0.0.0"},
				{Prefix: "192.0.2.0/24", AreaID: "0.0.0.1"},
			}},
			want: "OSPF network 192.0.2.0/24 is duplicated",
		},
		{
			name: "missing network area",
			ospf: &OSPFConfig{RouterID: "192.0.2.1", Networks: []OSPFNetwork{{Prefix: "192.0.2.0/24"}}},
			want: "area-id is required",
		},
		{
			name: "invalid network area",
			ospf: &OSPFConfig{RouterID: "192.0.2.1", Networks: []OSPFNetwork{{Prefix: "192.0.2.0/24", AreaID: "2001:db8::1"}}},
			want: "invalid OSPF area-id",
		},
		{
			name: "missing interface name",
			ospf: &OSPFConfig{RouterID: "192.0.2.1", Interfaces: []OSPFInterface{{AreaID: "0.0.0.0"}}},
			want: "OSPF interface name is required",
		},
		{
			name: "missing interface area",
			ospf: &OSPFConfig{RouterID: "192.0.2.1", Interfaces: []OSPFInterface{{Name: "ge0-0-0"}}},
			want: "area-id is required",
		},
		{
			name: "duplicate interface",
			ospf: &OSPFConfig{RouterID: "192.0.2.1", Interfaces: []OSPFInterface{
				{Name: "ge0-0-0", AreaID: "0.0.0.0"},
				{Name: "ge0-0-0", AreaID: "0.0.0.1"},
			}},
			want: "OSPF interface ge0-0-0 is duplicated",
		},
		{
			name: "invalid interface metric",
			ospf: &OSPFConfig{RouterID: "192.0.2.1", Interfaces: []OSPFInterface{{Name: "ge0-0-0", AreaID: "0.0.0.0", Metric: 65536}}},
			want: "invalid metric",
		},
		{
			name: "invalid interface priority",
			ospf: &OSPFConfig{RouterID: "192.0.2.1", Interfaces: []OSPFInterface{{Name: "ge0-0-0", AreaID: "0.0.0.0", Priority: &priorityTooHigh}}},
			want: "invalid priority",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildMgmtOperations(&Config{OSPF: tt.ospf})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("BuildMgmtOperations() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestBuildMgmtOperationsRejectsInvalidStaticRoutes(t *testing.T) {
	tests := []struct {
		name  string
		route StaticRoute
		want  string
	}{
		{
			name:  "missing prefix",
			route: StaticRoute{NextHop: "192.0.2.1"},
			want:  "static route prefix is required",
		},
		{
			name:  "invalid prefix",
			route: StaticRoute{Prefix: "192.0.2.0", NextHop: "192.0.2.1"},
			want:  "invalid static route prefix",
		},
		{
			name:  "missing next-hop",
			route: StaticRoute{Prefix: "192.0.2.0/24"},
			want:  "next-hop is required",
		},
		{
			name:  "invalid next-hop",
			route: StaticRoute{Prefix: "192.0.2.0/24", NextHop: "not-an-ip"},
			want:  "invalid next-hop IP",
		},
		{
			name:  "invalid distance",
			route: StaticRoute{Prefix: "192.0.2.0/24", NextHop: "192.0.2.1", Distance: 256},
			want:  "invalid distance",
		},
		{
			name:  "next-hop family mismatch",
			route: StaticRoute{Prefix: "2001:db8::/64", NextHop: "192.0.2.1", IsIPv6: true},
			want:  "next-hop family does not match prefix",
		},
		{
			name:  "ipv4 marked ipv6",
			route: StaticRoute{Prefix: "192.0.2.0/24", NextHop: "192.0.2.1", IsIPv6: true},
			want:  "address family does not match",
		},
		{
			name:  "ipv6 marked ipv4",
			route: StaticRoute{Prefix: "2001:db8::/64", NextHop: "2001:db8::1"},
			want:  "address family does not match",
		},
		{
			name:  "bfd option without bfd",
			route: StaticRoute{Prefix: "192.0.2.0/24", NextHop: "192.0.2.1", BFDSource: "192.0.2.2"},
			want:  "BFD options require BFD to be enabled",
		},
		{
			name:  "distance with bfd",
			route: StaticRoute{Prefix: "192.0.2.0/24", NextHop: "192.0.2.1", Distance: 10, BFD: true},
			want:  "distance is not supported with BFD monitoring",
		},
		{
			name:  "invalid bfd source",
			route: StaticRoute{Prefix: "192.0.2.0/24", NextHop: "192.0.2.1", BFD: true, BFDSource: "not-an-ip"},
			want:  "invalid BFD source IP",
		},
		{
			name:  "bfd source family mismatch",
			route: StaticRoute{Prefix: "192.0.2.0/24", NextHop: "192.0.2.1", BFD: true, BFDSource: "2001:db8::1"},
			want:  "BFD source family does not match next-hop",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildMgmtOperations(&Config{StaticRoutes: []StaticRoute{tt.route}})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("BuildMgmtOperations() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestBuildMgmtOperationsRejectsDuplicateStaticRoute(t *testing.T) {
	_, err := BuildMgmtOperations(&Config{StaticRoutes: []StaticRoute{
		{Prefix: "203.0.113.0/24", NextHop: "192.0.2.1"},
		{Prefix: "203.0.113.0/24", NextHop: "192.0.2.1", Distance: 10},
	}})
	if err == nil || !strings.Contains(err.Error(), "static route 203.0.113.0/24 via 192.0.2.1 is duplicated") {
		t.Fatalf("BuildMgmtOperations() error = %v, want duplicate static route", err)
	}
}

func TestBuildMgmtOperationsStaticRouteBFD(t *testing.T) {
	ops, err := BuildMgmtOperations(&Config{
		BFD: &BFDConfig{
			Profiles: []BFDProfile{{Name: "fast"}},
		},
		StaticRoutes: []StaticRoute{
			{
				Prefix:      "203.0.113.0/24",
				NextHop:     "192.0.2.2",
				BFD:         true,
				BFDProfile:  "fast",
				BFDSource:   "192.0.2.1",
				BFDMultihop: true,
			},
			{
				Prefix:  "2001:db8::/64",
				NextHop: "2001:db8::1",
				IsIPv6:  true,
				BFD:     true,
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildMgmtOperations() error = %v", err)
	}
	commands := commandsFromOps(ops)
	for _, want := range []string{
		"mgmt set-config /frr-routing:routing/control-plane-protocols/control-plane-protocol[type='frr-staticd:staticd'][name='staticd'][vrf='default']/frr-staticd:staticd/route-list[prefix='203.0.113.0/24'][src-prefix='::/0'][afi-safi='frr-routing:ipv4-unicast']/path-list[table-id='0'][nh-type='ip4'][vrf='default'][gateway='192.0.2.2'][interface='']/bfd-monitoring/multi-hop true",
		"mgmt set-config /frr-routing:routing/control-plane-protocols/control-plane-protocol[type='frr-staticd:staticd'][name='staticd'][vrf='default']/frr-staticd:staticd/route-list[prefix='203.0.113.0/24'][src-prefix='::/0'][afi-safi='frr-routing:ipv4-unicast']/path-list[table-id='0'][nh-type='ip4'][vrf='default'][gateway='192.0.2.2'][interface='']/bfd-monitoring/source 192.0.2.1",
		"mgmt set-config /frr-routing:routing/control-plane-protocols/control-plane-protocol[type='frr-staticd:staticd'][name='staticd'][vrf='default']/frr-staticd:staticd/route-list[prefix='203.0.113.0/24'][src-prefix='::/0'][afi-safi='frr-routing:ipv4-unicast']/path-list[table-id='0'][nh-type='ip4'][vrf='default'][gateway='192.0.2.2'][interface='']/bfd-monitoring/profile fast",
		"mgmt set-config /frr-routing:routing/control-plane-protocols/control-plane-protocol[type='frr-staticd:staticd'][name='staticd'][vrf='default']/frr-staticd:staticd/route-list[prefix='2001:db8::/64'][src-prefix='::/0'][afi-safi='frr-routing:ipv6-unicast']/path-list[table-id='0'][nh-type='ip6'][vrf='default'][gateway='2001:db8::1'][interface='']/bfd-monitoring/multi-hop false",
	} {
		if !strings.Contains(commands, want) {
			t.Fatalf("commands missing %q:\n%s", want, commands)
		}
	}
}

func TestBuildMgmtOperationsRejectsUnknownStaticRouteBFDProfile(t *testing.T) {
	_, err := BuildMgmtOperations(&Config{
		StaticRoutes: []StaticRoute{
			{
				Prefix:     "203.0.113.0/24",
				NextHop:    "192.0.2.2",
				BFD:        true,
				BFDProfile: "missing",
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "static route 203.0.113.0/24 references unknown BFD profile missing") {
		t.Fatalf("BuildMgmtOperations() error = %v, want unknown static route BFD profile", err)
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
