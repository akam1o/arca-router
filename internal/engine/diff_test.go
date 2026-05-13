package engine

import (
	"testing"

	"github.com/akam1o/arca-router/internal/model"
)

func TestComputeDiffDetectsPolicyTermChanges(t *testing.T) {
	accept := true
	oldCfg := model.NewRouterConfig()
	oldCfg.Policy = &model.PolicyConfig{
		PolicyStatements: map[string]*model.PolicyStatement{
			"IMPORT": {
				Terms: []*model.PolicyTerm{
					{Name: "10", Then: &model.PolicyActions{Accept: &accept}},
				},
			},
		},
	}

	localPref := uint32(200)
	newCfg := model.NewRouterConfig()
	newCfg.Policy = &model.PolicyConfig{
		PolicyStatements: map[string]*model.PolicyStatement{
			"IMPORT": {
				Terms: []*model.PolicyTerm{
					{Name: "10", Then: &model.PolicyActions{Accept: &accept, LocalPreference: &localPref}},
				},
			},
		},
	}

	diff := ComputeDiff(oldCfg, newCfg)
	if !diff.PolicyChanged {
		t.Fatal("ComputeDiff() did not detect policy term content change")
	}
}

func TestComputeDiffDetectsSecurityRateLimitChanges(t *testing.T) {
	oldCfg := model.NewRouterConfig()
	oldCfg.Security = &model.SecurityConfig{
		RateLimit: &model.RateLimitConfig{PerIP: 10},
	}
	newCfg := model.NewRouterConfig()
	newCfg.Security = &model.SecurityConfig{
		RateLimit: &model.RateLimitConfig{PerIP: 20},
	}

	diff := ComputeDiff(oldCfg, newCfg)
	if !diff.SecurityChanged {
		t.Fatal("ComputeDiff() did not detect security rate-limit change")
	}
}

func TestComputeDiffDetectsV06AdvancedChanges(t *testing.T) {
	oldCfg := model.NewRouterConfig()
	newCfg := model.NewRouterConfig()
	newCfg.Chassis = &model.ChassisConfig{
		Cluster: &model.ClusterConfig{
			Enabled: true,
			Nodes: map[string]*model.ClusterNode{
				"node0": {Address: "192.0.2.10"},
			},
		},
	}
	newCfg.RoutingInstances = map[string]*model.RoutingInstance{
		"BLUE": {
			InstanceType:       "vrf",
			RouteDistinguisher: "65000:100",
			VRFTarget:          "target:65000:100",
		},
	}
	newCfg.Protocols = &model.ProtocolsConfig{
		MPLS: &model.MPLSConfig{Interfaces: []string{"ge-0/0/0"}},
		VRRP: &model.VRRPConfig{Groups: map[string]*model.VRRPGroup{
			"10": {Interface: "ge-0/0/0", VirtualAddress: "192.0.2.254"},
		}},
	}
	newCfg.ClassOfService = &model.ClassOfServiceConfig{
		ForwardingClasses: map[string]*model.ForwardingClass{
			"ef": {Queue: 5},
		},
	}

	diff := ComputeDiff(oldCfg, newCfg)
	if !diff.HasChanges() {
		t.Fatal("ComputeDiff() HasChanges = false")
	}
	if !diff.ChassisChanged || !diff.RoutingInstancesChanged || !diff.MPLSChanged || !diff.VRRPChanged || !diff.ClassOfServiceChanged {
		t.Fatalf("v0.6 flags not set: %#v", diff)
	}
}
