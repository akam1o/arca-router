package config

import (
	"strings"
	"testing"
)

func TestServiceConfigRoundTrip(t *testing.T) {
	cfg := parseSetCommands(t,
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
		"set system services snmp community monitoring",
		"set security netconf ssh enabled true",
		"set security netconf ssh listen-address 127.0.0.1",
		"set security netconf ssh port 1830",
	)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if cfg.System == nil || cfg.System.Services == nil || cfg.System.Services.WebUI == nil || !cfg.System.Services.WebUI.Enabled {
		t.Fatalf("web-ui service not parsed: %#v", cfg.System)
	}
	if got := cfg.System.Services.Prometheus.Port; got != 9090 {
		t.Fatalf("prometheus port = %d", got)
	}
	if got := cfg.System.Services.SNMP.Port; got != 1161 {
		t.Fatalf("snmp port = %d", got)
	}
	if !cfg.Security.NETCONF.SSH.Enabled {
		t.Fatal("netconf ssh enabled = false, want true")
	}
	if got := cfg.Security.NETCONF.SSH.ListenAddress; got != "127.0.0.1" {
		t.Fatalf("netconf ssh listen-address = %q, want 127.0.0.1", got)
	}
	if got := cfg.Security.NETCONF.SSH.Port; got != 1830 {
		t.Fatalf("netconf ssh port = %d", got)
	}
	assertSetCommandRoundTrip(t, cfg)
}

func TestNETCONFValidationRejectsInvalidListenAddress(t *testing.T) {
	cfg := parseSetCommands(t,
		"set security netconf ssh enabled true",
		"set security netconf ssh listen-address not-an-address",
	)

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want invalid netconf ssh listen-address error")
	}
}

func TestNETCONFValidationAllowsPortZero(t *testing.T) {
	cfg := parseSetCommands(t,
		"set security netconf ssh enabled true",
		"set security netconf ssh port 0",
	)

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got := cfg.Security.NETCONF.SSH.Port; got != 0 {
		t.Fatalf("netconf ssh port = %d, want 0", got)
	}
}

func TestNETCONFParserRejectsPortOutOfRange(t *testing.T) {
	_, err := NewParser(strings.NewReader("set security netconf ssh port 65536")).Parse()
	if err == nil {
		t.Fatal("Parse() error = nil, want out-of-range netconf ssh port error")
	}
}

func TestSNMPValidationRejectsEnabledWithoutCommunity(t *testing.T) {
	cfg := parseSetCommands(t,
		"set system services snmp enabled true",
		"set system services snmp listen-address 127.0.0.1",
		"set system services snmp port 1161",
	)

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want missing snmp community error")
	}
}

func TestSNMPValidationRejectsWeakCommunity(t *testing.T) {
	cfg := parseSetCommands(t,
		"set system services snmp enabled true",
		"set system services snmp listen-address 127.0.0.1",
		"set system services snmp port 1161",
		"set system services snmp community public",
	)

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want weak snmp community error")
	}
}
