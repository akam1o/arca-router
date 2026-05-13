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

func TestValidateChangesRejectsOSPF3WithTransactionalBackend(t *testing.T) {
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
	if err == nil || !strings.Contains(err.Error(), "OSPFv3 requires FRR file backend") {
		t.Fatalf("ValidateChanges() error = %v, want OSPF3 transactional rejection", err)
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

type recordingApplier struct {
	configContent string
	cfg           *pkgfrr.Config
}

func (a *recordingApplier) ApplyConfig(ctx context.Context, configContent string, cfg *pkgfrr.Config) error {
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
