package model

import (
	"strings"
	"testing"

	"github.com/akam1o/arca-router/pkg/config"
)

func TestV06ConfigConversionAndClone(t *testing.T) {
	text := strings.Join([]string{
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
		"set chassis cluster node node0 address 192.0.2.10",
		"set interfaces ge-0/0/0 unit 0 family inet address 192.0.2.1/24",
		"set protocols mpls interface ge-0/0/0",
		"set protocols vrrp group 10 interface ge-0/0/0",
		"set protocols vrrp group 10 virtual-address 192.0.2.254",
		"set routing-instances BLUE instance-type vrf",
		"set routing-instances BLUE route-distinguisher 65000:100",
		"set routing-instances BLUE vrf-target target:65000:100",
		"set routing-instances BLUE interface ge-0/0/0",
		"set class-of-service traffic-control-profile WAN shaping-rate 1000",
		"set class-of-service interfaces ge-0/0/0 output-traffic-control-profile WAN",
	}, "\n")

	legacy, err := config.NewParser(strings.NewReader(text)).Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	cfg := FromLegacyConfig(legacy)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	clone := cfg.Clone()
	clone.Protocols.VRRP.Groups["10"].VirtualAddress = "192.0.2.253"
	if got := cfg.Protocols.VRRP.Groups["10"].VirtualAddress; got != "192.0.2.254" {
		t.Fatalf("original VRRP virtual-address mutated to %q", got)
	}

	roundTrip := cfg.ToLegacyConfig()
	if got := roundTrip.RoutingInstances["BLUE"].RouteDistinguisher; got != "65000:100" {
		t.Fatalf("route distinguisher = %q", got)
	}
	if got := roundTrip.ClassOfService.Interfaces["ge-0/0/0"].OutputTrafficControlProfile; got != "WAN" {
		t.Fatalf("CoS interface profile = %q", got)
	}
	if got := roundTrip.System.Services.Prometheus.Port; got != 9090 {
		t.Fatalf("prometheus port = %d", got)
	}
	if got := roundTrip.System.Services.SNMP.Community; got != "public" {
		t.Fatalf("snmp community = %q", got)
	}
}

func TestV06ModelValidationRejectsInvalidQueue(t *testing.T) {
	cfg := NewRouterConfig()
	cfg.ClassOfService = &ClassOfServiceConfig{
		ForwardingClasses: map[string]*ForwardingClass{
			"bad": {Queue: 9},
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want invalid queue error")
	}
}

func TestV06ModelValidationRejectsInvalidWebUI(t *testing.T) {
	cfg := NewRouterConfig()
	cfg.System = &SystemConfig{
		Services: &SystemServicesConfig{
			WebUI: &WebUIConfig{
				Enabled:       true,
				ListenAddress: "not an address",
			},
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want invalid web-ui listen-address error")
	}
}

func TestV06ModelValidationRejectsInvalidSNMP(t *testing.T) {
	cfg := NewRouterConfig()
	cfg.System = &SystemConfig{
		Services: &SystemServicesConfig{
			SNMP: &SNMPConfig{
				Enabled:       true,
				ListenAddress: "not an address",
			},
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want invalid snmp listen-address error")
	}
}

func TestV06ModelValidationRejectsInvalidPrometheus(t *testing.T) {
	cfg := NewRouterConfig()
	cfg.System = &SystemConfig{
		Services: &SystemServicesConfig{
			Prometheus: &PrometheusConfig{
				Enabled:       true,
				ListenAddress: "not an address",
			},
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want invalid prometheus listen-address error")
	}
}
