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
				Community:     "monitoring",
			},
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want invalid snmp listen-address error")
	}
}

func TestSNMPValidationRejectsEnabledWithoutCommunity(t *testing.T) {
	cfg := NewRouterConfig()
	cfg.System = &SystemConfig{
		Services: &SystemServicesConfig{
			SNMP: &SNMPConfig{
				Enabled:       true,
				ListenAddress: "127.0.0.1",
			},
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want missing snmp community error")
	}
}

func TestSNMPValidationRejectsWeakCommunity(t *testing.T) {
	cfg := NewRouterConfig()
	cfg.System = &SystemConfig{
		Services: &SystemServicesConfig{
			SNMP: &SNMPConfig{
				Enabled:       true,
				ListenAddress: "127.0.0.1",
				Community:     "public",
			},
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want weak snmp community error")
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

func TestNETCONFValidationRejectsInvalidSSHListenAddress(t *testing.T) {
	cfg := NewRouterConfig()
	cfg.Security = &SecurityConfig{
		NETCONF: &NETCONFSecurityConfig{
			SSH: &NETCONFSSHConfig{
				Enabled:       true,
				ListenAddress: "not an address",
			},
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want invalid netconf ssh listen-address error")
	}
}

func TestNETCONFValidationAllowsZeroSSHPort(t *testing.T) {
	cfg := NewRouterConfig()
	cfg.Security = &SecurityConfig{
		NETCONF: &NETCONFSecurityConfig{
			SSH: &NETCONFSSHConfig{
				Enabled: true,
				Port:    0,
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestNETCONFValidationAllowsMaxSSHPort(t *testing.T) {
	cfg := NewRouterConfig()
	cfg.Security = &SecurityConfig{
		NETCONF: &NETCONFSecurityConfig{
			SSH: &NETCONFSSHConfig{
				Enabled: true,
				Port:    65535,
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
