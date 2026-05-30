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

func TestComputeDiffDetectsOSPF3Changes(t *testing.T) {
	newCfg := model.NewRouterConfig()
	newCfg.Protocols = &model.ProtocolsConfig{
		OSPF3: &model.OSPFConfig{
			Areas: map[string]*model.OSPFArea{
				"0.0.0.0": {
					Interfaces: map[string]*model.OSPFInterface{
						"ge-0/0/0": {Metric: 20},
					},
				},
			},
		},
	}

	diff := ComputeDiff(model.NewRouterConfig(), newCfg)
	if !diff.OSPF3Changed || diff.NewOSPF3 == nil {
		t.Fatalf("OSPF3 change not detected: %#v", diff)
	}
	if !diff.HasChanges() {
		t.Fatal("HasChanges() = false, want true")
	}
}

func TestComputeDiffDetectsBFDChanges(t *testing.T) {
	newCfg := model.NewRouterConfig()
	newCfg.Protocols = &model.ProtocolsConfig{
		BFD: &model.BFDConfig{
			Peers: map[string]*model.BFDPeer{
				"192.0.2.2": {Profile: "fast"},
			},
		},
	}

	diff := ComputeDiff(model.NewRouterConfig(), newCfg)
	if !diff.BFDChanged || diff.NewBFD == nil {
		t.Fatalf("BFD change not detected: %#v", diff)
	}
	if !diff.HasChanges() {
		t.Fatal("HasChanges() = false, want true")
	}
}

func TestComputeDiffDetectsEVPNChanges(t *testing.T) {
	newCfg := model.NewRouterConfig()
	newCfg.Protocols = &model.ProtocolsConfig{
		EVPN: &model.EVPNConfig{VNIs: map[int]*model.EVPNVNI{
			10010: {VNI: 10010, Type: "l2", BridgeDomain: "BD-10"},
		}},
	}

	diff := ComputeDiff(model.NewRouterConfig(), newCfg)
	if !diff.EVPNChanged || diff.NewEVPN == nil {
		t.Fatalf("EVPN change not detected: %#v", diff)
	}
	if !diff.HasChanges() {
		t.Fatal("HasChanges() = false, want true")
	}
}

func TestComputeDiffDetectsBGPBFDBindingChanges(t *testing.T) {
	oldCfg := model.NewRouterConfig()
	oldCfg.Protocols = &model.ProtocolsConfig{
		BGP: &model.BGPConfig{Groups: map[string]*model.BGPGroup{
			"EBGP": {
				Type: "external",
				Neighbors: map[string]*model.BGPNeighbor{
					"192.0.2.2": {PeerAS: 65001},
				},
			},
		}},
	}
	newCfg := oldCfg.Clone()
	newCfg.Protocols.BGP.Groups["EBGP"].Neighbors["192.0.2.2"].BFD = true
	newCfg.Protocols.BGP.Groups["EBGP"].Neighbors["192.0.2.2"].BFDProfile = "fast"

	diff := ComputeDiff(oldCfg, newCfg)
	if !diff.BGPChanged {
		t.Fatalf("BGP BFD binding change not detected: %#v", diff)
	}
}

func TestComputeDiffDetectsOSPFBFDBindingChanges(t *testing.T) {
	oldCfg := model.NewRouterConfig()
	oldCfg.Protocols = &model.ProtocolsConfig{
		OSPF: &model.OSPFConfig{Areas: map[string]*model.OSPFArea{
			"0.0.0.0": {
				Interfaces: map[string]*model.OSPFInterface{
					"ge-0/0/0": {Metric: 10},
				},
			},
		}},
	}
	newCfg := oldCfg.Clone()
	newCfg.Protocols.OSPF.Areas["0.0.0.0"].Interfaces["ge-0/0/0"].BFD = true
	newCfg.Protocols.OSPF.Areas["0.0.0.0"].Interfaces["ge-0/0/0"].BFDProfile = "fast"

	diff := ComputeDiff(oldCfg, newCfg)
	if !diff.OSPFChanged {
		t.Fatalf("OSPF BFD binding change not detected: %#v", diff)
	}
}

func TestComputeDiffDetectsStaticRouteBFDChanges(t *testing.T) {
	oldCfg := model.NewRouterConfig()
	oldCfg.Routing = &model.RoutingConfig{StaticRoutes: []*model.StaticRoute{
		{Prefix: "203.0.113.0/24", NextHop: "192.0.2.2"},
	}}
	newCfg := oldCfg.Clone()
	newCfg.Routing.StaticRoutes[0].BFD = true
	newCfg.Routing.StaticRoutes[0].BFDProfile = "fast"

	diff := ComputeDiff(oldCfg, newCfg)
	if !diff.StaticRoutesChanged {
		t.Fatalf("Static route BFD change not detected: %#v", diff)
	}
}

func TestComputeDiffHandlesNilInterfaceEntries(t *testing.T) {
	oldCfg := model.NewRouterConfig()
	oldCfg.Interfaces["ge-0/0/0"] = nil
	oldCfg.Interfaces["ge-0/0/1"] = &model.InterfaceConfig{
		Units: map[int]*model.Unit{
			0: nil,
			1: {Family: map[string]*model.AddressFamily{"inet": nil}},
		},
	}

	newCfg := model.NewRouterConfig()
	newCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{Description: "uplink"}
	newCfg.Interfaces["ge-0/0/1"] = &model.InterfaceConfig{
		Units: map[int]*model.Unit{
			0: {Family: map[string]*model.AddressFamily{
				"inet": {Addresses: []string{"192.0.2.1/24"}},
			}},
		},
	}

	diff := ComputeDiff(oldCfg, newCfg)
	change := diff.InterfacesChanged["ge-0/0/0"]
	if change == nil || !change.DescriptionChanged || change.NewDescription != "uplink" {
		t.Fatalf("interface description change = %#v, want nil-safe description change", change)
	}
	change = diff.InterfacesChanged["ge-0/0/1"]
	if change == nil || len(change.AddressesAdded) != 1 {
		t.Fatalf("interface address change = %#v, want nil-safe address addition", change)
	}
}

func TestConfigDiffCloneHandlesNilConfigs(t *testing.T) {
	diff := (&ConfigDiff{}).Clone()
	if diff == nil {
		t.Fatal("ConfigDiff.Clone() = nil, want empty diff")
	}
	if diff.OldConfig == nil || diff.NewConfig == nil {
		t.Fatalf("ConfigDiff.Clone() configs = old %#v new %#v, want initialized configs", diff.OldConfig, diff.NewConfig)
	}
}
