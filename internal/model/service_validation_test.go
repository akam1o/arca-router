package model

import "testing"

func TestWebUIValidationRejectsInvalidListenAddress(t *testing.T) {
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

func TestSNMPValidationRejectsInvalidListenAddress(t *testing.T) {
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

func TestPrometheusValidationRejectsInvalidListenAddress(t *testing.T) {
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

func TestNETCONFValidationRejectsInvalidSSHPort(t *testing.T) {
	cfg := NewRouterConfig()
	cfg.Security = &SecurityConfig{
		NETCONF: &NETCONFSecurityConfig{
			SSH: &NETCONFSSHConfig{Port: 70000},
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want invalid netconf ssh port error")
	}
}
