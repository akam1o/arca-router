package model

import (
	"strings"
	"testing"
)

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
			want: `routing-instance BLUE vrf-target: invalid vrf-target "invalid"`,
		},
		{
			name: "import",
			configure: func(instance *RoutingInstance) {
				instance.VRFTargetImport = []string{"invalid"}
			},
			want: `routing-instance BLUE vrf-target import: invalid vrf-target "invalid"`,
		},
		{
			name: "export",
			configure: func(instance *RoutingInstance) {
				instance.VRFTargetExport = []string{"invalid"}
			},
			want: `routing-instance BLUE vrf-target export: invalid vrf-target "invalid"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewRouterConfig()
			cfg.RoutingInstances = map[string]*RoutingInstance{
				"BLUE": {InstanceType: "vrf"},
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

func TestRoutingInstanceValidationRejectsUnknownPolicies(t *testing.T) {
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
			want: `routing-instance BLUE vrf-import: policy-statement "MISSING-IN" not found in policy-options`,
		},
		{
			name: "vrf-export",
			configure: func(instance *RoutingInstance) {
				instance.VRFExport = []string{"MISSING-OUT"}
			},
			want: `routing-instance BLUE vrf-export: policy-statement "MISSING-OUT" not found in policy-options`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewRouterConfig()
			cfg.RoutingInstances = map[string]*RoutingInstance{
				"BLUE": {InstanceType: "vrf"},
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

func TestRoutingInstanceValidationRejectsL3VPNSafetyViolations(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*RouterConfig, *RoutingInstance)
		want      string
	}{
		{
			name: "vrf-import without import target",
			configure: func(cfg *RouterConfig, instance *RoutingInstance) {
				cfg.Routing = &RoutingConfig{AutonomousSystem: 65000}
				instance.VRFImport = []string{"BLUE-IN"}
			},
			want: "routing-instance BLUE: vrf-import requires an import vrf-target",
		},
		{
			name: "vrf-export without export target",
			configure: func(cfg *RouterConfig, instance *RoutingInstance) {
				cfg.Routing = &RoutingConfig{AutonomousSystem: 65000}
				instance.RouteDistinguisher = "65000:100"
				instance.VRFExport = []string{"BLUE-OUT"}
			},
			want: "routing-instance BLUE: vrf-export requires an export vrf-target",
		},
		{
			name: "export target without route distinguisher",
			configure: func(cfg *RouterConfig, instance *RoutingInstance) {
				cfg.Routing = &RoutingConfig{AutonomousSystem: 65000}
				instance.VRFTargetExport = []string{"target:65000:100"}
			},
			want: "routing-instance BLUE: route-distinguisher is required for VPN export",
		},
		{
			name: "vpn target without autonomous system",
			configure: func(cfg *RouterConfig, instance *RoutingInstance) {
				instance.RouteDistinguisher = "65000:100"
				instance.VRFTarget = "target:65000:100"
			},
			want: "routing-instance BLUE: routing-options autonomous-system is required for VPN import/export",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewRouterConfig()
			cfg.Policy = &PolicyConfig{
				PolicyStatements: map[string]*PolicyStatement{
					"BLUE-IN":  {},
					"BLUE-OUT": {},
				},
			}
			cfg.RoutingInstances = map[string]*RoutingInstance{
				"BLUE": {InstanceType: "vrf"},
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
