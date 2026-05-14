package frr

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	pkgfrr "github.com/akam1o/arca-router/pkg/frr"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestBuildFullConfigUsesDiffNewConfig(t *testing.T) {
	newCfg := model.NewRouterConfig()
	newCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{
		Units: map[int]*model.Unit{
			0: {Family: map[string]*model.AddressFamily{"inet": {Addresses: []string{"192.0.2.1/24"}}}},
		},
	}
	newCfg.Routing = &model.RoutingConfig{AutonomousSystem: 65000, RouterID: "192.0.2.1"}
	newCfg.Protocols = &model.ProtocolsConfig{
		BGP: &model.BGPConfig{Groups: map[string]*model.BGPGroup{
			"EBGP": {
				Type: "external",
				Neighbors: map[string]*model.BGPNeighbor{
					"203.0.113.1": {PeerAS: 65001, LocalAddress: "192.0.2.1"},
				},
			},
		}},
	}

	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)
	got := NewFRRPlugin(testLogger()).buildFullConfig(diff)

	if _, ok := got.Interfaces["ge-0/0/0"]; !ok {
		t.Fatalf("buildFullConfig() dropped interfaces: %#v", got.Interfaces)
	}
	if got.Protocols == nil || got.Protocols.BGP == nil {
		t.Fatalf("buildFullConfig() dropped protocols: %#v", got.Protocols)
	}
}

func TestFRRRelevantInterfaceChangesIncludesAddressChanges(t *testing.T) {
	oldCfg := model.NewRouterConfig()
	oldCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{
		Units: map[int]*model.Unit{
			0: {Family: map[string]*model.AddressFamily{"inet": {Addresses: []string{"192.0.2.1/24"}}}},
		},
	}
	newCfg := model.NewRouterConfig()
	newCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{
		Units: map[int]*model.Unit{
			0: {Family: map[string]*model.AddressFamily{"inet": {Addresses: []string{"198.51.100.1/24"}}}},
		},
	}

	diff := engine.ComputeDiff(oldCfg, newCfg)
	if !hasFRRRelevantInterfaceChanges(diff) {
		t.Fatal("hasFRRRelevantInterfaceChanges() did not detect address change")
	}
}

func TestBuildFullConfigPreservesOSPF3FromDiffFields(t *testing.T) {
	diff := &engine.ConfigDiff{
		NewOSPF3: &model.OSPFConfig{
			Areas: map[string]*model.OSPFArea{
				"0.0.0.0": {
					Interfaces: map[string]*model.OSPFInterface{
						"ge-0/0/0": {},
					},
				},
			},
		},
	}

	got := NewFRRPlugin(testLogger()).buildFullConfig(diff)
	if got.Protocols == nil || got.Protocols.OSPF3 == nil {
		t.Fatalf("buildFullConfig() dropped OSPF3: %#v", got.Protocols)
	}
}

func TestBuildFullConfigPreservesBFDFromDiffFields(t *testing.T) {
	diff := &engine.ConfigDiff{
		NewBFD: &model.BFDConfig{
			Peers: map[string]*model.BFDPeer{
				"192.0.2.2": {Profile: "fast"},
			},
		},
	}

	got := NewFRRPlugin(testLogger()).buildFullConfig(diff)
	if got.Protocols == nil || got.Protocols.BFD == nil {
		t.Fatalf("buildFullConfig() dropped BFD: %#v", got.Protocols)
	}
}

func TestValidateChangesAllowsTransactionalVRRP(t *testing.T) {
	newCfg := model.NewRouterConfig()
	newCfg.Protocols = &model.ProtocolsConfig{
		VRRP: &model.VRRPConfig{Groups: map[string]*model.VRRPGroup{
			"10": {Interface: "ge-0/0/0", VirtualAddress: "192.0.2.254"},
		}},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)

	err := NewFRRPlugin(testLogger()).ValidateChanges(context.Background(), diff)
	if err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}

func TestValidateChangesAllowsVRRPWithFileBackend(t *testing.T) {
	newCfg := model.NewRouterConfig()
	newCfg.Protocols = &model.ProtocolsConfig{
		VRRP: &model.VRRPConfig{Groups: map[string]*model.VRRPGroup{
			"10": {Interface: "ge-0/0/0", VirtualAddress: "192.0.2.254"},
		}},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)

	err := NewFRRPluginWithApplyMode(testLogger(), pkgfrr.BackendModeFile).ValidateChanges(context.Background(), diff)
	if err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}

func TestValidateChangesAllowsMPLSConfig(t *testing.T) {
	newCfg := model.NewRouterConfig()
	newCfg.Protocols = &model.ProtocolsConfig{
		MPLS: &model.MPLSConfig{Interfaces: []string{"ge-0/0/0"}},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)

	err := NewFRRPlugin(testLogger()).ValidateChanges(context.Background(), diff)
	if err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}

func TestValidateChangesAllowsOSPF3WithTransactionalBackend(t *testing.T) {
	newCfg := model.NewRouterConfig()
	newCfg.Protocols = &model.ProtocolsConfig{
		OSPF3: &model.OSPFConfig{Areas: map[string]*model.OSPFArea{
			"0.0.0.0": {
				Interfaces: map[string]*model.OSPFInterface{
					"ge-0/0/0": {},
				},
			},
		}},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)

	err := NewFRRPlugin(testLogger()).ValidateChanges(context.Background(), diff)
	if err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}

func TestValidateChangesAllowsBFDWithTransactionalBackend(t *testing.T) {
	newCfg := model.NewRouterConfig()
	newCfg.Protocols = &model.ProtocolsConfig{
		BFD: &model.BFDConfig{Peers: map[string]*model.BFDPeer{
			"192.0.2.2": {},
		}},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)

	err := NewFRRPlugin(testLogger()).ValidateChanges(context.Background(), diff)
	if err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}

func TestValidateChangesAllowsBGPBFDBindingWithTransactionalBackend(t *testing.T) {
	newCfg := model.NewRouterConfig()
	newCfg.Protocols = &model.ProtocolsConfig{
		BGP: &model.BGPConfig{Groups: map[string]*model.BGPGroup{
			"EBGP": {
				Type: "external",
				Neighbors: map[string]*model.BGPNeighbor{
					"192.0.2.2": {PeerAS: 65001, BFD: true},
				},
			},
		}},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)

	err := NewFRRPlugin(testLogger()).ValidateChanges(context.Background(), diff)
	if err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}

func TestValidateChangesAllowsBGPBFDProfileWithTransactionalBackend(t *testing.T) {
	newCfg := model.NewRouterConfig()
	newCfg.Protocols = &model.ProtocolsConfig{
		BGP: &model.BGPConfig{Groups: map[string]*model.BGPGroup{
			"EBGP": {
				Type: "external",
				Neighbors: map[string]*model.BGPNeighbor{
					"192.0.2.2": {PeerAS: 65001, BFD: true, BFDProfile: "fast"},
				},
			},
		}},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)

	err := NewFRRPlugin(testLogger()).ValidateChanges(context.Background(), diff)
	if err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}

func TestValidateChangesAllowsOSPFBFDBindingWithTransactionalBackend(t *testing.T) {
	newCfg := model.NewRouterConfig()
	newCfg.Protocols = &model.ProtocolsConfig{
		OSPF: &model.OSPFConfig{Areas: map[string]*model.OSPFArea{
			"0.0.0.0": {
				Interfaces: map[string]*model.OSPFInterface{
					"ge-0/0/0": {BFD: true},
				},
			},
		}},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)

	err := NewFRRPlugin(testLogger()).ValidateChanges(context.Background(), diff)
	if err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}

func TestValidateChangesAllowsOSPFBFDProfileWithTransactionalBackend(t *testing.T) {
	newCfg := model.NewRouterConfig()
	newCfg.Protocols = &model.ProtocolsConfig{
		OSPF: &model.OSPFConfig{Areas: map[string]*model.OSPFArea{
			"0.0.0.0": {
				Interfaces: map[string]*model.OSPFInterface{
					"ge-0/0/0": {BFD: true, BFDProfile: "fast"},
				},
			},
		}},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)

	err := NewFRRPlugin(testLogger()).ValidateChanges(context.Background(), diff)
	if err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}

func TestValidateChangesAllowsBFDStaticRouteWithTransactionalBackend(t *testing.T) {
	newCfg := model.NewRouterConfig()
	newCfg.Routing = &model.RoutingConfig{StaticRoutes: []*model.StaticRoute{
		{Prefix: "203.0.113.0/24", NextHop: "192.0.2.2", BFD: true, BFDProfile: "fast"},
	}}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)

	err := NewFRRPlugin(testLogger()).ValidateChanges(context.Background(), diff)
	if err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}

func TestValidateChangesAllowsOSPF3WithFileBackend(t *testing.T) {
	newCfg := model.NewRouterConfig()
	newCfg.Protocols = &model.ProtocolsConfig{
		OSPF3: &model.OSPFConfig{Areas: map[string]*model.OSPFArea{
			"0.0.0.0": {
				Interfaces: map[string]*model.OSPFInterface{
					"ge-0/0/0": {},
				},
			},
		}},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)

	err := NewFRRPluginWithApplyMode(testLogger(), pkgfrr.BackendModeFile).ValidateChanges(context.Background(), diff)
	if err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}

func TestValidateChangesAllowsBFDProtocolBindingWithFileBackend(t *testing.T) {
	newCfg := model.NewRouterConfig()
	newCfg.Protocols = &model.ProtocolsConfig{
		BGP: &model.BGPConfig{Groups: map[string]*model.BGPGroup{
			"EBGP": {
				Type: "external",
				Neighbors: map[string]*model.BGPNeighbor{
					"192.0.2.2": {PeerAS: 65001, BFD: true, BFDProfile: "fast"},
				},
			},
		}},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)

	err := NewFRRPluginWithApplyMode(testLogger(), pkgfrr.BackendModeFile).ValidateChanges(context.Background(), diff)
	if err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}

func TestValidateChangesAllowsBFDStaticRouteWithFileBackend(t *testing.T) {
	newCfg := model.NewRouterConfig()
	newCfg.Routing = &model.RoutingConfig{StaticRoutes: []*model.StaticRoute{
		{Prefix: "203.0.113.0/24", NextHop: "192.0.2.2", BFD: true, BFDProfile: "fast"},
	}}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)

	err := NewFRRPluginWithApplyMode(testLogger(), pkgfrr.BackendModeFile).ValidateChanges(context.Background(), diff)
	if err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}

func TestValidateChangesAllowsBFDWithFileBackend(t *testing.T) {
	newCfg := model.NewRouterConfig()
	newCfg.Protocols = &model.ProtocolsConfig{
		BFD: &model.BFDConfig{Peers: map[string]*model.BFDPeer{
			"192.0.2.2": {},
		}},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)

	err := NewFRRPluginWithApplyMode(testLogger(), pkgfrr.BackendModeFile).ValidateChanges(context.Background(), diff)
	if err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}

func TestValidateChangesAllowsRoutingInstances(t *testing.T) {
	newCfg := model.NewRouterConfig()
	newCfg.RoutingInstances = map[string]*model.RoutingInstance{
		"BLUE": {InstanceType: "vrf", RouteDistinguisher: "65000:100", VRFTarget: "target:65000:100"},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)

	err := NewFRRPlugin(testLogger()).ValidateChanges(context.Background(), diff)
	if err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}

func TestApplyChangesPassesVRRPToTransactionalApplier(t *testing.T) {
	newCfg := model.NewRouterConfig()
	newCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{Units: map[int]*model.Unit{}}
	newCfg.Protocols = &model.ProtocolsConfig{
		VRRP: &model.VRRPConfig{Groups: map[string]*model.VRRPGroup{
			"10": {Interface: "ge-0/0/0", VirtualAddress: "192.0.2.254", Priority: 110, Preempt: true},
		}},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)
	applier := &recordingApplier{}
	plugin := NewFRRPlugin(testLogger())
	plugin.applier = applier
	plugin.statusReader = fakeVRRPStatusReader{status: &pkgfrr.VRRPStatus{
		Groups: []pkgfrr.VRRPRouterStatus{
			{Interface: "ge0-0-0", VRID: 10, IPv4State: "Master"},
		},
	}}

	if err := plugin.ApplyChanges(context.Background(), diff); err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
	if applier.cfg == nil || applier.cfg.VRRP == nil || len(applier.cfg.VRRP.Groups) != 1 {
		t.Fatalf("applied VRRP config = %#v, want one group", applier.cfg)
	}
	group := applier.cfg.VRRP.Groups[0]
	if group.Interface != "ge0-0-0" || group.ID != 10 || group.VirtualAddress != "192.0.2.254" {
		t.Fatalf("applied VRRP group = %#v, want converted group", group)
	}
	status := plugin.VRRPOperationalStatus()
	if status.ConfiguredGroups != 1 || status.ObservedGroups != 1 || status.ActiveGroups != 1 || len(status.Issues) != 0 {
		t.Fatalf("VRRPOperationalStatus() = %#v, want converged group", status)
	}
	if len(status.Groups) != 1 || status.Groups[0].Interface != "ge0-0-0" || status.Groups[0].ID != 10 ||
		status.Groups[0].State != "Master" || !status.Groups[0].Observed || !status.Groups[0].Active {
		t.Fatalf("VRRPOperationalStatus().Groups = %#v, want active group detail", status.Groups)
	}
}

func TestApplyChangesPassesRoutingInstancesToApplier(t *testing.T) {
	newCfg := model.NewRouterConfig()
	newCfg.Routing = &model.RoutingConfig{AutonomousSystem: 65000}
	newCfg.RoutingInstances = map[string]*model.RoutingInstance{
		"BLUE": {
			InstanceType:       "vrf",
			RouteDistinguisher: "65000:100",
			VRFTarget:          "target:65000:100",
		},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)
	applier := &recordingApplier{}
	plugin := NewFRRPlugin(testLogger())
	plugin.applier = applier

	if err := plugin.ApplyChanges(context.Background(), diff); err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
	if applier.cfg == nil || len(applier.cfg.VRFs) != 1 {
		t.Fatalf("applied VRFs = %#v, want one VRF", applier.cfg)
	}
	vrf := applier.cfg.VRFs[0]
	if vrf.Name != "BLUE" || vrf.ASN != 65000 || vrf.RouteDistinguisher != "65000:100" {
		t.Fatalf("applied VRF = %#v, want BLUE L3VPN config", vrf)
	}
	if len(vrf.ImportTargets) != 1 || vrf.ImportTargets[0] != "65000:100" {
		t.Fatalf("ImportTargets = %#v, want 65000:100", vrf.ImportTargets)
	}
	if len(vrf.ExportTargets) != 1 || vrf.ExportTargets[0] != "65000:100" {
		t.Fatalf("ExportTargets = %#v, want 65000:100", vrf.ExportTargets)
	}
}

func TestApplyChangesFallsBackToFileBackendForOSPF3(t *testing.T) {
	newCfg := model.NewRouterConfig()
	newCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{
		Units: map[int]*model.Unit{
			0: {Family: map[string]*model.AddressFamily{"inet6": {Addresses: []string{"2001:db8::1/64"}}}},
		},
	}
	newCfg.Protocols = &model.ProtocolsConfig{
		OSPF3: &model.OSPFConfig{Areas: map[string]*model.OSPFArea{
			"0.0.0.0": {
				Interfaces: map[string]*model.OSPFInterface{
					"ge-0/0/0": {Metric: 20},
				},
			},
		}},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)
	transactionalApplier := &recordingApplier{}
	fileApplier := &recordingApplier{}
	plugin := NewFRRPlugin(testLogger())
	plugin.applier = transactionalApplier
	plugin.fileApplier = fileApplier

	if err := plugin.ApplyChanges(context.Background(), diff); err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
	if transactionalApplier.calls != 0 {
		t.Fatalf("transactional ApplyConfig calls = %d, want 0", transactionalApplier.calls)
	}
	if fileApplier.calls != 1 {
		t.Fatalf("file ApplyConfig calls = %d, want 1", fileApplier.calls)
	}
	if plugin.currentApplyMode != pkgfrr.BackendModeFile {
		t.Fatalf("currentApplyMode = %q, want file", plugin.currentApplyMode)
	}
	if fileApplier.cfg == nil || fileApplier.cfg.OSPF3 == nil {
		t.Fatalf("file applier config OSPF3 = %#v, want OSPF3 config", fileApplier.cfg)
	}
	if !strings.Contains(fileApplier.configContent, "router ospf6") {
		t.Fatalf("file applier config missing OSPFv3:\n%s", fileApplier.configContent)
	}
}

func TestRollbackUsesFileBackendAfterFileFallback(t *testing.T) {
	newCfg := model.NewRouterConfig()
	newCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{
		Units: map[int]*model.Unit{
			0: {Family: map[string]*model.AddressFamily{"inet6": {Addresses: []string{"2001:db8::1/64"}}}},
		},
	}
	newCfg.Protocols = &model.ProtocolsConfig{
		OSPF3: &model.OSPFConfig{Areas: map[string]*model.OSPFArea{
			"0.0.0.0": {
				Interfaces: map[string]*model.OSPFInterface{
					"ge-0/0/0": {Metric: 20},
				},
			},
		}},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)
	transactionalApplier := &recordingApplier{}
	fileApplier := &recordingApplier{}
	plugin := NewFRRPlugin(testLogger())
	plugin.applier = transactionalApplier
	plugin.fileApplier = fileApplier

	if err := plugin.ApplyChanges(context.Background(), diff); err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
	if err := plugin.RollbackChanges(context.Background(), diff); err != nil {
		t.Fatalf("RollbackChanges() error = %v", err)
	}
	if transactionalApplier.calls != 0 {
		t.Fatalf("transactional ApplyConfig calls = %d, want 0", transactionalApplier.calls)
	}
	if fileApplier.calls != 2 {
		t.Fatalf("file ApplyConfig calls = %d, want 2", fileApplier.calls)
	}
	if plugin.currentApplyMode != pkgfrr.BackendModeFile {
		t.Fatalf("currentApplyMode after rollback = %q, want file", plugin.currentApplyMode)
	}
}

func TestApplyChangesUsesConfiguredFileBackend(t *testing.T) {
	newCfg := model.NewRouterConfig()
	newCfg.Routing = &model.RoutingConfig{StaticRoutes: []*model.StaticRoute{
		{Prefix: "203.0.113.0/24", NextHop: "192.0.2.1"},
	}}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)
	configuredFileApplier := &recordingApplier{}
	fallbackFileApplier := &recordingApplier{}
	plugin := NewFRRPluginWithApplyMode(testLogger(), pkgfrr.BackendModeFile)
	plugin.applier = configuredFileApplier
	plugin.fileApplier = fallbackFileApplier

	if err := plugin.ApplyChanges(context.Background(), diff); err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
	if configuredFileApplier.calls != 1 {
		t.Fatalf("configured file ApplyConfig calls = %d, want 1", configuredFileApplier.calls)
	}
	if fallbackFileApplier.calls != 0 {
		t.Fatalf("fallback file ApplyConfig calls = %d, want 0", fallbackFileApplier.calls)
	}
	if plugin.currentApplyMode != pkgfrr.BackendModeFile {
		t.Fatalf("currentApplyMode = %q, want file", plugin.currentApplyMode)
	}
}

func TestValidateChangesAllowsRemovingUnsupportedV06FRRConfig(t *testing.T) {
	oldCfg := model.NewRouterConfig()
	oldCfg.Protocols = &model.ProtocolsConfig{
		VRRP: &model.VRRPConfig{Groups: map[string]*model.VRRPGroup{
			"10": {Interface: "ge-0/0/0", VirtualAddress: "192.0.2.254"},
		}},
	}
	diff := engine.ComputeDiff(oldCfg, model.NewRouterConfig())

	if err := NewFRRPlugin(testLogger()).ValidateChanges(context.Background(), diff); err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}

func TestCheckVRRPOperationalStatusReportsMissingGroup(t *testing.T) {
	plugin := NewFRRPlugin(testLogger())
	plugin.statusReader = fakeVRRPStatusReader{status: &pkgfrr.VRRPStatus{}}

	status := plugin.checkVRRPOperationalStatus(context.Background(), &pkgfrr.Config{
		VRRP: &pkgfrr.VRRPConfig{Groups: []pkgfrr.VRRPGroup{
			{ID: 10, Interface: "ge0-0-0", VirtualAddress: "192.0.2.254"},
		}},
	})
	if status.ConfiguredGroups != 1 || status.ObservedGroups != 0 || status.ActiveGroups != 0 ||
		len(status.Issues) != 1 || status.LastError != "" {
		t.Fatalf("checkVRRPOperationalStatus() = %#v, want one missing group issue", status)
	}
	if len(status.Groups) != 1 || status.Groups[0].State != "missing" || status.Groups[0].Observed || status.Groups[0].Active {
		t.Fatalf("checkVRRPOperationalStatus().Groups = %#v, want missing group detail", status.Groups)
	}
}

func TestCheckVRRPOperationalStatusRecordsReaderError(t *testing.T) {
	plugin := NewFRRPlugin(testLogger())
	plugin.statusReader = fakeVRRPStatusReader{err: errors.New("vtysh failed")}

	status := plugin.checkVRRPOperationalStatus(context.Background(), &pkgfrr.Config{
		VRRP: &pkgfrr.VRRPConfig{Groups: []pkgfrr.VRRPGroup{
			{ID: 10, Interface: "ge0-0-0", VirtualAddress: "192.0.2.254"},
		}},
	})
	if status.LastError == "" || len(status.Issues) != 1 {
		t.Fatalf("checkVRRPOperationalStatus() = %#v, want reader error issue", status)
	}
}

func TestCheckBFDOperationalStatusReportsMissingPeer(t *testing.T) {
	plugin := NewFRRPlugin(testLogger())
	plugin.bfdStatusReader = fakeBFDStatusReader{status: &pkgfrr.BFDStatus{}}

	status := plugin.checkBFDOperationalStatus(context.Background(), &pkgfrr.Config{
		BFD: &pkgfrr.BFDConfig{Peers: []pkgfrr.BFDPeer{
			{Address: "192.0.2.2", LocalAddress: "192.0.2.1", Interface: "ge0-0-0"},
		}},
	})
	if status.ConfiguredPeers != 1 || status.ObservedPeers != 0 || status.UpPeers != 0 ||
		len(status.Issues) != 1 || status.LastError != "" {
		t.Fatalf("checkBFDOperationalStatus() = %#v, want one missing peer issue", status)
	}
	if len(status.Peers) != 1 || status.Peers[0].Status != "missing" || status.Peers[0].Observed || status.Peers[0].Up {
		t.Fatalf("checkBFDOperationalStatus().Peers = %#v, want missing peer detail", status.Peers)
	}
}

func TestCheckBFDOperationalStatusReportsDownPeerAndCounters(t *testing.T) {
	plugin := NewFRRPlugin(testLogger())
	plugin.bfdStatusReader = fakeBFDStatusReader{status: &pkgfrr.BFDStatus{
		Peers: []pkgfrr.BFDPeerStatus{
			{
				Peer:              "192.0.2.2",
				Status:            "down",
				SessionDownEvents: 2,
				RxFailPackets:     1,
			},
		},
	}}

	status := plugin.checkBFDOperationalStatus(context.Background(), &pkgfrr.Config{
		BGP: &pkgfrr.BGPConfig{Neighbors: []pkgfrr.BGPNeighbor{
			{IP: "192.0.2.2", RemoteAS: 65001, BFD: true},
		}},
	})
	if status.ConfiguredPeers != 1 || status.ObservedPeers != 1 || status.UpPeers != 0 ||
		status.DownPeers != 1 || status.SessionDownEvents != 2 || status.RxFailPackets != 1 ||
		len(status.Issues) != 1 {
		t.Fatalf("checkBFDOperationalStatus() = %#v, want down peer and counters", status)
	}
}

func TestCheckBFDOperationalStatusConverged(t *testing.T) {
	plugin := NewFRRPlugin(testLogger())
	plugin.bfdStatusReader = fakeBFDStatusReader{status: &pkgfrr.BFDStatus{
		Peers: []pkgfrr.BFDPeerStatus{
			{Peer: "2001:db8::2", LocalAddress: "2001:db8::1", Status: "up", SessionDownEvents: 1},
		},
	}}

	status := plugin.checkBFDOperationalStatus(context.Background(), &pkgfrr.Config{
		StaticRoutes: []pkgfrr.StaticRoute{
			{Prefix: "2001:db8:100::/64", NextHop: "2001:db8::2", BFD: true, BFDSource: "2001:db8::1"},
		},
	})
	if status.ConfiguredPeers != 1 || status.ObservedPeers != 1 || status.UpPeers != 1 ||
		status.DownPeers != 0 || len(status.Issues) != 0 || status.SessionDownEvents != 1 {
		t.Fatalf("checkBFDOperationalStatus() = %#v, want converged peer", status)
	}
}

type recordingApplier struct {
	configContent string
	cfg           *pkgfrr.Config
	calls         int
}

func (a *recordingApplier) ApplyConfig(ctx context.Context, configContent string, cfg *pkgfrr.Config) error {
	a.calls++
	a.configContent = configContent
	a.cfg = cfg
	return nil
}

type fakeVRRPStatusReader struct {
	status *pkgfrr.VRRPStatus
	err    error
}

func (r fakeVRRPStatusReader) ReadVRRPStatus(ctx context.Context) (*pkgfrr.VRRPStatus, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.status == nil {
		return &pkgfrr.VRRPStatus{}, nil
	}
	return r.status, nil
}

type fakeBFDStatusReader struct {
	status *pkgfrr.BFDStatus
	err    error
}

func (r fakeBFDStatusReader) ReadBFDStatus(ctx context.Context) (*pkgfrr.BFDStatus, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.status == nil {
		return &pkgfrr.BFDStatus{}, nil
	}
	return r.status, nil
}
