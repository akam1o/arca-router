package config

import (
	"strings"
	"testing"
)

func TestRoutingInstanceConfigRoundTrip(t *testing.T) {
	cfg := parseSetCommands(t,
		"set interfaces ge-0/0/0 unit 0 family inet address 192.0.2.1/24",
		"set routing-options autonomous-system 65000",
		"set routing-instances BLUE instance-type vrf",
		"set routing-instances BLUE route-distinguisher 65000:100",
		"set routing-instances BLUE vrf-target target:65000:100",
		"set routing-instances BLUE vrf-target import target:65000:101",
		"set routing-instances BLUE vrf-target export target:65000:102",
		"set routing-instances BLUE vrf-import BLUE-IN",
		"set routing-instances BLUE vrf-export BLUE-OUT",
		"set routing-instances BLUE interface ge-0/0/0",
		"set policy-options policy-statement BLUE-IN term ACCEPT then accept",
		"set policy-options policy-statement BLUE-OUT term ACCEPT then accept",
	)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	instance := cfg.RoutingInstances["BLUE"]
	if instance == nil || instance.RouteDistinguisher != "65000:100" {
		t.Fatalf("routing instance = %#v, want BLUE with route distinguisher", instance)
	}
	if got := instance.VRFTarget; got != "target:65000:100" {
		t.Fatalf("VRF target = %q", got)
	}
	if got := instance.VRFTargetImport; len(got) != 1 || got[0] != "target:65000:101" {
		t.Fatalf("VRF target import = %#v, want [target:65000:101]", got)
	}
	if got := instance.VRFTargetExport; len(got) != 1 || got[0] != "target:65000:102" {
		t.Fatalf("VRF target export = %#v, want [target:65000:102]", got)
	}
	if got := instance.VRFImport; len(got) != 1 || got[0] != "BLUE-IN" {
		t.Fatalf("VRF import = %#v, want [BLUE-IN]", got)
	}
	if got := instance.VRFExport; len(got) != 1 || got[0] != "BLUE-OUT" {
		t.Fatalf("VRF export = %#v, want [BLUE-OUT]", got)
	}
	assertSetCommandRoundTrip(t, cfg)
}

func TestRoutingInstanceValidationRejectsUnknownInterfaceReference(t *testing.T) {
	cfg := NewConfig()
	cfg.RoutingInstances = map[string]*RoutingInstance{
		"BLUE": {
			Name:         "BLUE",
			InstanceType: "vrf",
			Interfaces:   []string{"ge-0/0/0"},
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want unknown interface reference error")
	}
	if want := "Routing instance BLUE references non-existent interface ge-0/0/0"; !strings.Contains(err.Error(), want) {
		t.Fatalf("Validate() error = %v, want substring %q", err, want)
	}
}

func TestRoutingInstanceValidationRejectsInvalidVRFTargets(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*RoutingInstance)
		want      string
	}{
		{
			name: "common",
			configure: func(instance *RoutingInstance) {
				instance.VRFTarget = "invalid"
			},
			want: "Invalid routing-instance BLUE vrf-target: invalid",
		},
		{
			name: "import",
			configure: func(instance *RoutingInstance) {
				instance.VRFTargetImport = []string{"invalid"}
			},
			want: "Invalid routing-instance BLUE vrf-target import: invalid",
		},
		{
			name: "export",
			configure: func(instance *RoutingInstance) {
				instance.VRFTargetExport = []string{"invalid"}
			},
			want: "Invalid routing-instance BLUE vrf-target export: invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewConfig()
			cfg.RoutingInstances = map[string]*RoutingInstance{
				"BLUE": {
					Name:         "BLUE",
					InstanceType: "vrf",
				},
			}
			tt.configure(cfg.RoutingInstances["BLUE"])

			err := cfg.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil, want invalid vrf-target error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestRoutingInstanceValidationRejectsL3VPNSafetyViolations(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*Config, *RoutingInstance)
		want      string
	}{
		{
			name: "vrf-import without import target",
			configure: func(cfg *Config, instance *RoutingInstance) {
				cfg.RoutingOptions = &RoutingOptions{AutonomousSystem: 65000}
				instance.VRFImport = []string{"BLUE-IN"}
			},
			want: "Routing instance BLUE vrf-import requires an import vrf-target",
		},
		{
			name: "vrf-export without export target",
			configure: func(cfg *Config, instance *RoutingInstance) {
				cfg.RoutingOptions = &RoutingOptions{AutonomousSystem: 65000}
				instance.RouteDistinguisher = "65000:100"
				instance.VRFExport = []string{"BLUE-OUT"}
			},
			want: "Routing instance BLUE vrf-export requires an export vrf-target",
		},
		{
			name: "export target without route distinguisher",
			configure: func(cfg *Config, instance *RoutingInstance) {
				cfg.RoutingOptions = &RoutingOptions{AutonomousSystem: 65000}
				instance.VRFTargetExport = []string{"target:65000:100"}
			},
			want: "Routing instance BLUE route-distinguisher is required for VPN export",
		},
		{
			name: "vpn target without autonomous system",
			configure: func(cfg *Config, instance *RoutingInstance) {
				instance.RouteDistinguisher = "65000:100"
				instance.VRFTarget = "target:65000:100"
			},
			want: "Routing instance BLUE routing-options autonomous-system is required for VPN import/export",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewConfig()
			cfg.PolicyOptions = &PolicyOptions{
				PolicyStatements: map[string]*PolicyStatement{
					"BLUE-IN":  {},
					"BLUE-OUT": {},
				},
			}
			cfg.RoutingInstances = map[string]*RoutingInstance{
				"BLUE": {Name: "BLUE", InstanceType: "vrf"},
			}
			tt.configure(cfg, cfg.RoutingInstances["BLUE"])

			err := cfg.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil, want L3VPN safety error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestRoutingInstanceValidationRejectsUnknownVRFPolicies(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*RoutingInstance)
		want      string
	}{
		{
			name: "vrf-import",
			configure: func(instance *RoutingInstance) {
				instance.VRFImport = []string{"MISSING-IN"}
			},
			want: "Routing instance BLUE vrf-import references unknown policy-statement MISSING-IN",
		},
		{
			name: "vrf-export",
			configure: func(instance *RoutingInstance) {
				instance.VRFExport = []string{"MISSING-OUT"}
			},
			want: "Routing instance BLUE vrf-export references unknown policy-statement MISSING-OUT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewConfig()
			cfg.RoutingInstances = map[string]*RoutingInstance{
				"BLUE": {
					Name:         "BLUE",
					InstanceType: "vrf",
				},
			}
			tt.configure(cfg.RoutingInstances["BLUE"])

			err := cfg.Validate()
			if err == nil {
				t.Fatal("Validate() error = nil, want unknown policy-statement error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Validate() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}
