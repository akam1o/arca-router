package config

import (
	"strings"
	"testing"
)

func TestV06AdvancedConfigRoundTrip(t *testing.T) {
	input := strings.Join([]string{
		"set system host-name edge-01",
		"set system services web-ui enabled true",
		"set system services web-ui listen-address 127.0.0.1",
		"set system services web-ui port 8443",
		"set system services prometheus enabled true",
		"set system services prometheus listen-address 127.0.0.1",
		"set system services prometheus port 9090",
		"set system services snmp enabled true",
		"set system services snmp listen-address 127.0.0.1",
		"set system services snmp port 1161",
		"set system services snmp community public",
		"set security netconf ssh port 1830",
		"set chassis cluster enabled true",
		"set chassis cluster node node0 address 192.0.2.10",
		"set chassis cluster node node0 priority 120",
		"set chassis cluster sync etcd endpoint http://127.0.0.1:2379",
		"set interfaces ge-0/0/0 unit 0 family inet address 192.0.2.1/24",
		"set protocols mpls interface ge-0/0/0",
		"set protocols vrrp group 10 interface ge-0/0/0",
		"set protocols vrrp group 10 virtual-address 192.0.2.254",
		"set protocols vrrp group 10 priority 110",
		"set protocols vrrp group 10 preempt",
		"set routing-instances BLUE instance-type vrf",
		"set routing-instances BLUE route-distinguisher 65000:100",
		"set routing-instances BLUE vrf-target target:65000:100",
		"set routing-instances BLUE interface ge-0/0/0",
		"set class-of-service forwarding-class expedited-forwarding queue 5",
		"set class-of-service traffic-control-profile WAN shaping-rate 1000000000",
		"set class-of-service traffic-control-profile WAN scheduler-map WAN-SCHED",
		"set class-of-service interfaces ge-0/0/0 output-traffic-control-profile WAN",
	}, "\n")

	cfg, err := NewParser(strings.NewReader(input)).Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if cfg.Chassis == nil || cfg.Chassis.Cluster == nil || !cfg.Chassis.Cluster.Enabled {
		t.Fatalf("chassis cluster not parsed: %#v", cfg.Chassis)
	}
	if got := cfg.Protocols.VRRP.Groups["10"].VirtualAddress; got != "192.0.2.254" {
		t.Fatalf("VRRP virtual-address = %q", got)
	}
	if got := cfg.RoutingInstances["BLUE"].VRFTarget; got != "target:65000:100" {
		t.Fatalf("VRF target = %q", got)
	}
	if got := cfg.ClassOfService.TrafficControlProfiles["WAN"].ShapingRate; got != 1000000000 {
		t.Fatalf("shaping-rate = %d", got)
	}
	if got := cfg.System.Services.Prometheus.Port; got != 9090 {
		t.Fatalf("prometheus port = %d", got)
	}
	if got := cfg.System.Services.SNMP.Port; got != 1161 {
		t.Fatalf("snmp port = %d", got)
	}
	if got := cfg.Security.NETCONF.SSH.Port; got != 1830 {
		t.Fatalf("netconf ssh port = %d", got)
	}

	text := ToSetCommands(cfg)
	parsed, err := NewParser(strings.NewReader(text)).Parse()
	if err != nil {
		t.Fatalf("round-trip parse failed:\n%s\nerror: %v", text, err)
	}
	if got := ToSetCommands(parsed); got != text {
		t.Fatalf("round-trip mismatch\nwant:\n%s\ngot:\n%s", text, got)
	}
}

func TestV06AdvancedConfigValidationRejectsInvalidReferences(t *testing.T) {
	cfg := NewConfig()
	cfg.ClassOfService = &ClassOfServiceConfig{
		Interfaces: map[string]*CoSInterface{
			"ge-0/0/0": {
				Name:                        "ge-0/0/0",
				OutputTrafficControlProfile: "missing",
			},
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want missing traffic-control-profile error")
	}
}

func TestRepeatedSetListValuesAreIdempotent(t *testing.T) {
	input := strings.Join([]string{
		"set chassis cluster sync etcd endpoint http://127.0.0.1:2379",
		"set chassis cluster sync etcd endpoint http://127.0.0.1:2379",
		"set interfaces ge-0/0/0 unit 0 family inet address 192.0.2.1/24",
		"set interfaces ge-0/0/0 unit 0 family inet address 192.0.2.1/24",
		"set protocols mpls interface ge-0/0/0",
		"set protocols mpls interface ge-0/0/0",
		"set routing-instances BLUE interface ge-0/0/0",
		"set routing-instances BLUE interface ge-0/0/0",
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
	if got := len(cfg.RoutingInstances["BLUE"].Interfaces); got != 1 {
		t.Fatalf("routing-instance interfaces = %d, want 1", got)
	}
	if got := len(cfg.PolicyOptions.PrefixLists["CUSTOMER"].Prefixes); got != 1 {
		t.Fatalf("prefix-list entries = %d, want 1", got)
	}

	text := ToSetCommands(cfg)
	for _, line := range []string{
		"set chassis cluster sync etcd endpoint http://127.0.0.1:2379",
		"set interfaces ge-0/0/0 unit 0 family inet address 192.0.2.1/24",
		"set protocols mpls interface ge-0/0/0",
		"set routing-instances BLUE interface ge-0/0/0",
		"set policy-options prefix-list CUSTOMER 192.0.2.0/24",
	} {
		if got := strings.Count(text, line); got != 1 {
			t.Fatalf("%q appears %d times in set output, want 1\n%s", line, got, text)
		}
	}
}
