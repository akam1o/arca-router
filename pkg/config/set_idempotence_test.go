package config

import (
	"strings"
	"testing"
)

func TestRepeatedSetListValuesAreIdempotent(t *testing.T) {
	input := strings.Join([]string{
		"set chassis cluster sync etcd endpoint http://127.0.0.1:2379",
		"set chassis cluster sync etcd endpoint http://127.0.0.1:2379",
		"set interfaces ge-0/0/0 unit 0 family inet address 192.0.2.1/24",
		"set interfaces ge-0/0/0 unit 0 family inet address 192.0.2.1/24",
		"set protocols mpls interface ge-0/0/0",
		"set protocols mpls interface ge-0/0/0",
		"set protocols evpn vni 10010 type l2",
		"set protocols evpn vni 10010 bridge-domain BD-10",
		"set protocols evpn vni 10010 vrf-target import target:65000:10010",
		"set protocols evpn vni 10010 vrf-target import target:65000:10010",
		"set protocols evpn vni 10010 vrf-target export target:65000:10011",
		"set protocols evpn vni 10010 vrf-target export target:65000:10011",
		"set routing-instances BLUE interface ge-0/0/0",
		"set routing-instances BLUE interface ge-0/0/0",
		"set routing-instances BLUE vrf-target import target:65000:101",
		"set routing-instances BLUE vrf-target import target:65000:101",
		"set routing-instances BLUE vrf-target export target:65000:102",
		"set routing-instances BLUE vrf-target export target:65000:102",
		"set routing-instances BLUE vrf-import BLUE-IN",
		"set routing-instances BLUE vrf-import BLUE-IN",
		"set routing-instances BLUE vrf-export BLUE-OUT",
		"set routing-instances BLUE vrf-export BLUE-OUT",
		"set policy-options prefix-list CUSTOMER 192.0.2.0/24",
		"set policy-options prefix-list CUSTOMER 192.0.2.0/24",
	}, "\n")

	cfg, err := NewParser(strings.NewReader(input)).Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if got := len(cfg.Chassis.Cluster.Sync.Etcd.Endpoints); got != 1 {
		t.Fatalf("etcd endpoints = %d, want 1", got)
	}
	addresses := cfg.Interfaces["ge-0/0/0"].Units[0].Family["inet"].Addresses
	if got := len(addresses); got != 1 {
		t.Fatalf("interface addresses = %d, want 1", got)
	}
	if got := len(cfg.Protocols.MPLS.Interfaces); got != 1 {
		t.Fatalf("MPLS interfaces = %d, want 1", got)
	}
	if got := len(cfg.Protocols.EVPN.VNIs[10010].VRFTargetImport); got != 1 {
		t.Fatalf("EVPN VNI import targets = %d, want 1", got)
	}
	if got := len(cfg.Protocols.EVPN.VNIs[10010].VRFTargetExport); got != 1 {
		t.Fatalf("EVPN VNI export targets = %d, want 1", got)
	}
	if got := len(cfg.RoutingInstances["BLUE"].Interfaces); got != 1 {
		t.Fatalf("routing-instance interfaces = %d, want 1", got)
	}
	if got := len(cfg.RoutingInstances["BLUE"].VRFTargetImport); got != 1 {
		t.Fatalf("routing-instance vrf-target import = %d, want 1", got)
	}
	if got := len(cfg.RoutingInstances["BLUE"].VRFTargetExport); got != 1 {
		t.Fatalf("routing-instance vrf-target export = %d, want 1", got)
	}
	if got := len(cfg.RoutingInstances["BLUE"].VRFImport); got != 1 {
		t.Fatalf("routing-instance vrf-import = %d, want 1", got)
	}
	if got := len(cfg.RoutingInstances["BLUE"].VRFExport); got != 1 {
		t.Fatalf("routing-instance vrf-export = %d, want 1", got)
	}
	if got := len(cfg.PolicyOptions.PrefixLists["CUSTOMER"].Prefixes); got != 1 {
		t.Fatalf("prefix-list entries = %d, want 1", got)
	}

	text := ToSetCommands(cfg)
	for _, line := range []string{
		"set chassis cluster sync etcd endpoint http://127.0.0.1:2379",
		"set interfaces ge-0/0/0 unit 0 family inet address 192.0.2.1/24",
		"set protocols mpls interface ge-0/0/0",
		"set protocols evpn vni 10010 vrf-target import target:65000:10010",
		"set protocols evpn vni 10010 vrf-target export target:65000:10011",
		"set routing-instances BLUE interface ge-0/0/0",
		"set routing-instances BLUE vrf-target import target:65000:101",
		"set routing-instances BLUE vrf-target export target:65000:102",
		"set routing-instances BLUE vrf-import BLUE-IN",
		"set routing-instances BLUE vrf-export BLUE-OUT",
		"set policy-options prefix-list CUSTOMER 192.0.2.0/24",
	} {
		if got := strings.Count(text, line); got != 1 {
			t.Fatalf("%q appears %d times in set output, want 1\n%s", line, got, text)
		}
	}
}
