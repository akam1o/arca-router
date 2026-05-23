package vpp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	"github.com/akam1o/arca-router/pkg/device"
	pkgvpp "github.com/akam1o/arca-router/pkg/vpp"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewVPPPluginDefaultsNilLoggerAndHardwareConfig(t *testing.T) {
	plugin := NewVPPPlugin(pkgvpp.NewMockClient(), nil, nil)
	if plugin.log == nil {
		t.Fatal("NewVPPPlugin() left log nil")
	}
	if plugin.hwConfig == nil {
		t.Fatal("NewVPPPlugin() left hwConfig nil")
	}
	if plugin.hasHardwareConfig("ge-0/0/0") {
		t.Fatal("hasHardwareConfig() = true for nil hardware config")
	}
	if got := plugin.getHardwareConfig("ge-0/0/0"); got != nil {
		t.Fatalf("getHardwareConfig() = %#v, want nil", got)
	}
}

func TestInitRecordsQoSCapabilities(t *testing.T) {
	ctx := context.Background()
	client := pkgvpp.NewMockClient()
	client.SetQoSCapabilities(pkgvpp.QoSCapabilities{
		MetadataBinding:     true,
		QueueScheduler:      false,
		Policer:             false,
		OperationalCounters: false,
		Diagnostics:         []string{"scheduler api unavailable"},
	})
	plugin := NewVPPPlugin(client, &device.HardwareConfig{}, testLogger())
	if err := plugin.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { _ = plugin.Close() })

	status := plugin.QoSCapabilityStatus()
	if status.LastCheck.IsZero() {
		t.Fatal("QoSCapabilityStatus().LastCheck is zero")
	}
	if !status.Capabilities.MetadataBinding || status.Capabilities.QueueScheduler || status.Capabilities.Policer ||
		status.Capabilities.OperationalCounters {
		t.Fatalf("QoSCapabilityStatus().Capabilities = %#v, want metadata-only support", status.Capabilities)
	}
	if len(status.Capabilities.Diagnostics) != 1 || status.Capabilities.Diagnostics[0] != "scheduler api unavailable" {
		t.Fatalf("QoSCapabilityStatus().Diagnostics = %#v", status.Capabilities.Diagnostics)
	}
}

func TestInitRecordsRedactedQoSCapabilityError(t *testing.T) {
	ctx := context.Background()
	client := pkgvpp.NewMockClient()
	client.GetQoSCapabilitiesError = errors.New("connect /run/vpp/api.sock: permission denied with token=secret")
	plugin := NewVPPPlugin(client, &device.HardwareConfig{}, testLogger())
	if err := plugin.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { _ = plugin.Close() })

	status := plugin.QoSCapabilityStatus()
	if status.LastError != vppQoSCapabilityErrorMessage {
		t.Fatalf("QoSCapabilityStatus().LastError = %q, want redacted capability error", status.LastError)
	}
	assertVPPStatusErrorRedacted(t, status.LastError)
}

func TestApplyChangesPreservesInterfaceAddressHostIP(t *testing.T) {
	ctx := context.Background()
	client := pkgvpp.NewMockClient()
	plugin := NewVPPPlugin(client, &device.HardwareConfig{
		Interfaces: []device.PhysicalInterface{
			{Name: "ge-0/0/0", PCI: "0000:03:00.0", Driver: "avf"},
		},
	}, testLogger())
	if err := plugin.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { _ = plugin.Close() })

	diff := engine.ComputeDiff(model.NewRouterConfig(), &model.RouterConfig{
		Interfaces: map[string]*model.InterfaceConfig{
			"ge-0/0/0": {
				Units: map[int]*model.Unit{
					0: {Family: map[string]*model.AddressFamily{"inet": {Addresses: []string{"192.0.2.1/24"}}}},
				},
			},
		},
	})
	if err := plugin.ApplyChanges(ctx, diff); err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
	idx, ok := plugin.GetInterfaceIndex("ge-0/0/0")
	if !ok {
		t.Fatal("ApplyChanges() did not add interface index")
	}
	iface, err := client.GetInterface(ctx, idx)
	if err != nil {
		t.Fatalf("GetInterface() error = %v", err)
	}
	if len(iface.Addresses) != 1 {
		t.Fatalf("addresses = %d, want 1", len(iface.Addresses))
	}
	if got, want := iface.Addresses[0].IP, net.ParseIP("192.0.2.1").To4(); !got.Equal(want) {
		t.Fatalf("address IP = %s, want %s", got, want)
	}
}

func TestApplyChangesRollsBackInterfaceIndexOnAddressFailure(t *testing.T) {
	ctx := context.Background()
	client := pkgvpp.NewMockClient()
	plugin := NewVPPPlugin(client, &device.HardwareConfig{
		Interfaces: []device.PhysicalInterface{
			{Name: "ge-0/0/0", PCI: "0000:03:00.0", Driver: "avf"},
		},
	}, testLogger())
	if err := plugin.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { _ = plugin.Close() })

	client.SetInterfaceAddressError = errors.New("address failed")
	diff := engine.ComputeDiff(model.NewRouterConfig(), &model.RouterConfig{
		Interfaces: map[string]*model.InterfaceConfig{
			"ge-0/0/0": {
				Units: map[int]*model.Unit{
					0: {Family: map[string]*model.AddressFamily{"inet": {Addresses: []string{"192.0.2.1/24"}}}},
				},
			},
		},
	})

	if err := plugin.ApplyChanges(ctx, diff); err == nil {
		t.Fatal("ApplyChanges() expected error")
	}
	if _, ok := plugin.GetInterfaceIndex("ge-0/0/0"); ok {
		t.Fatal("ApplyChanges() left rolled-back interface in index")
	}
}

func TestRollbackChangesRemovesAddedInterfaceIndex(t *testing.T) {
	ctx := context.Background()
	client := pkgvpp.NewMockClient()
	plugin := NewVPPPlugin(client, &device.HardwareConfig{
		Interfaces: []device.PhysicalInterface{
			{Name: "ge-0/0/0", PCI: "0000:03:00.0", Driver: "avf"},
		},
	}, testLogger())
	if err := plugin.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { _ = plugin.Close() })

	diff := engine.ComputeDiff(model.NewRouterConfig(), &model.RouterConfig{
		Interfaces: map[string]*model.InterfaceConfig{
			"ge-0/0/0": {
				Units: map[int]*model.Unit{
					0: {Family: map[string]*model.AddressFamily{"inet": {Addresses: []string{"192.0.2.1/24"}}}},
				},
			},
		},
	})
	if err := plugin.ApplyChanges(ctx, diff); err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
	idx, ok := plugin.GetInterfaceIndex("ge-0/0/0")
	if !ok {
		t.Fatal("ApplyChanges() did not add interface index")
	}

	if err := plugin.RollbackChanges(ctx, diff); err != nil {
		t.Fatalf("RollbackChanges() error = %v", err)
	}
	if _, ok := plugin.GetInterfaceIndex("ge-0/0/0"); ok {
		t.Fatal("RollbackChanges() left added interface in index")
	}
	iface, err := client.GetInterface(ctx, idx)
	if err != nil {
		t.Fatalf("GetInterface() error = %v", err)
	}
	if len(iface.Addresses) != 0 {
		t.Fatalf("RollbackChanges() left addresses on added interface: %#v", iface.Addresses)
	}
}

func TestApplyChangesFailsOnRemovedInterfaceError(t *testing.T) {
	ctx := context.Background()
	client := pkgvpp.NewMockClient()
	plugin := NewVPPPlugin(client, &device.HardwareConfig{
		Interfaces: []device.PhysicalInterface{
			{Name: "ge-0/0/0", PCI: "0000:03:00.0", Driver: "avf"},
		},
	}, testLogger())
	if err := plugin.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { _ = plugin.Close() })

	oldCfg := model.NewRouterConfig()
	oldCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{Units: map[int]*model.Unit{}}
	if err := plugin.ApplyChanges(ctx, engine.ComputeDiff(model.NewRouterConfig(), oldCfg)); err != nil {
		t.Fatalf("initial ApplyChanges() error = %v", err)
	}

	client.SetInterfaceDownError = errors.New("down failed")
	diff := engine.ComputeDiff(oldCfg, model.NewRouterConfig())
	if err := plugin.ApplyChanges(ctx, diff); err == nil {
		t.Fatal("ApplyChanges() expected removed interface error")
	}
	if _, ok := plugin.GetInterfaceIndex("ge-0/0/0"); !ok {
		t.Fatal("ApplyChanges() removed interface index after failed removal")
	}
}

func TestApplyChangesRemovesInterfaceWithoutLCPPair(t *testing.T) {
	ctx := context.Background()
	client := pkgvpp.NewMockClient()
	plugin := NewVPPPlugin(client, &device.HardwareConfig{
		Interfaces: []device.PhysicalInterface{
			{Name: "ge-0/0/0", PCI: "0000:03:00.0", Driver: "avf"},
		},
	}, testLogger())
	if err := plugin.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { _ = plugin.Close() })

	oldCfg := model.NewRouterConfig()
	oldCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{Units: map[int]*model.Unit{}}
	client.CreateLCPInterfaceError = errors.New("lcp create failed")
	if err := plugin.ApplyChanges(ctx, engine.ComputeDiff(model.NewRouterConfig(), oldCfg)); err != nil {
		t.Fatalf("initial ApplyChanges() error = %v", err)
	}

	client.CreateLCPInterfaceError = nil
	if err := plugin.ApplyChanges(ctx, engine.ComputeDiff(oldCfg, model.NewRouterConfig())); err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
	if _, ok := plugin.GetInterfaceIndex("ge-0/0/0"); ok {
		t.Fatal("ApplyChanges() left removed interface index")
	}
}

func TestCollectStateIncludesInterfaceCounters(t *testing.T) {
	ctx := context.Background()
	client := pkgvpp.NewMockClient()
	plugin := NewVPPPlugin(client, &device.HardwareConfig{
		Interfaces: []device.PhysicalInterface{
			{Name: "ge-0/0/0", PCI: "0000:03:00.0", Driver: "avf"},
		},
	}, testLogger())
	if err := plugin.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { _ = plugin.Close() })

	cfg := model.NewRouterConfig()
	cfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{Units: map[int]*model.Unit{}}
	if err := plugin.ApplyChanges(ctx, engine.ComputeDiff(model.NewRouterConfig(), cfg)); err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
	idx, ok := plugin.GetInterfaceIndex("ge-0/0/0")
	if !ok {
		t.Fatal("ApplyChanges() did not add interface index")
	}
	client.SetInterfaceCounters(idx, pkgvpp.InterfaceCounters{
		RxPackets: 10,
		TxPackets: 20,
		RxBytes:   1000,
		TxBytes:   2000,
		RxErrors:  1,
		TxErrors:  2,
		Drops:     3,
	})
	client.SetInterfaceQueuePlacements(idx, pkgvpp.InterfaceQueuePlacements{
		Rx: []pkgvpp.InterfaceRxQueuePlacement{
			{QueueID: 0, WorkerID: 1, Mode: "polling"},
		},
		Tx: []pkgvpp.InterfaceTxQueuePlacement{
			{QueueID: 0, Shared: true, Threads: []uint32{0, 2}},
		},
	})
	if err := client.SetQoSProfile(ctx, idx, pkgvpp.QoSProfile{Name: "WAN"}); err != nil {
		t.Fatalf("SetQoSProfile() error = %v", err)
	}

	state, err := plugin.CollectState(ctx)
	if err != nil {
		t.Fatalf("CollectState() error = %v", err)
	}
	if state["ge-0/0/0"].Counters == nil {
		t.Fatal("CollectState() did not include counters")
	}
	if got := *state["ge-0/0/0"].Counters; got.RxPackets != 10 || got.TxPackets != 20 || got.RxBytes != 1000 || got.TxBytes != 2000 || got.RxErrors != 1 || got.TxErrors != 2 || got.Drops != 3 {
		t.Fatalf("CollectState() counters = %#v, want VPP counters", got)
	}
	if got := state["ge-0/0/0"].QoSProfile; got != "WAN" {
		t.Fatalf("CollectState() QoSProfile = %q, want WAN", got)
	}
	if state["ge-0/0/0"].Queues == nil {
		t.Fatal("CollectState() did not include queue placements")
	}
	if got := state["ge-0/0/0"].Queues.Rx[0]; got.QueueID != 0 || got.WorkerID != 1 || got.Mode != "polling" {
		t.Fatalf("CollectState() RX queue = %#v, want VPP queue placement", got)
	}
	if got := state["ge-0/0/0"].Queues.Tx[0]; got.QueueID != 0 || !got.Shared || len(got.Threads) != 2 || got.Threads[1] != 2 {
		t.Fatalf("CollectState() TX queue = %#v, want VPP queue placement", got)
	}
}

func TestInitRecordsLCPReconciliationStatus(t *testing.T) {
	ctx := context.Background()
	client := pkgvpp.NewMockClient()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	iface, err := client.CreateInterface(ctx, &pkgvpp.CreateInterfaceRequest{
		Type:           pkgvpp.InterfaceTypeAVF,
		DeviceInstance: "0000:03:00.0",
		PCIAddress:     "0000:03:00.0",
		Name:           "ge-0/0/0",
	})
	if err != nil {
		t.Fatalf("CreateInterface() error = %v", err)
	}
	if err := client.CreateLCPInterface(ctx, iface.SwIfIndex, "ge000"); err != nil {
		t.Fatalf("CreateLCPInterface() error = %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	plugin := NewVPPPlugin(client, &device.HardwareConfig{}, testLogger())
	if err := plugin.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { _ = plugin.Close() })

	status := plugin.LCPReconciliationStatus()
	if status.LastRun.IsZero() {
		t.Fatal("LCPReconciliationStatus().LastRun is zero")
	}
	if status.PairCount != 1 {
		t.Fatalf("LCPReconciliationStatus().PairCount = %d, want 1", status.PairCount)
	}
	if len(status.Inconsistencies) != 0 {
		t.Fatalf("LCPReconciliationStatus().Inconsistencies = %#v, want none", status.Inconsistencies)
	}
	if status.LastError != "" {
		t.Fatalf("LCPReconciliationStatus().LastError = %q, want empty", status.LastError)
	}
}

func TestInitRecordsRedactedLCPReconciliationError(t *testing.T) {
	ctx := context.Background()
	client := pkgvpp.NewMockClient()
	client.ListLCPInterfacesError = errors.New("list /run/vpp/lcp.sock: permission denied with token=secret")
	plugin := NewVPPPlugin(client, &device.HardwareConfig{}, testLogger())
	if err := plugin.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { _ = plugin.Close() })

	status := plugin.LCPReconciliationStatus()
	if status.LastError != vppLCPReconciliationErrorMessage {
		t.Fatalf("LCPReconciliationStatus().LastError = %q, want redacted reconciliation error", status.LastError)
	}
	assertVPPStatusErrorRedacted(t, status.LastError)
}

func TestValidateChangesAllowsMPLSConfig(t *testing.T) {
	plugin := NewVPPPlugin(pkgvpp.NewMockClient(), &device.HardwareConfig{}, testLogger())
	newCfg := model.NewRouterConfig()
	newCfg.Protocols = &model.ProtocolsConfig{
		MPLS: &model.MPLSConfig{Interfaces: []string{"ge-0/0/0"}},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)

	if err := plugin.ValidateChanges(context.Background(), diff); err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}

func assertVPPStatusErrorRedacted(t *testing.T, msg string) {
	t.Helper()
	for _, leaked := range []string{"/run/vpp", "api.sock", "lcp.sock", "permission denied", "secret"} {
		if strings.Contains(msg, leaked) {
			t.Fatalf("VPP status error leaked %q in %q", leaked, msg)
		}
	}
}

func TestValidateChangesAllowsEVPNL2VXLAN(t *testing.T) {
	plugin := NewVPPPlugin(pkgvpp.NewMockClient(), &device.HardwareConfig{
		Interfaces: []device.PhysicalInterface{
			{Name: "ge-0/0/0", PCI: "0000:03:00.0", Driver: "avf"},
		},
	}, testLogger())
	newCfg := model.NewRouterConfig()
	newCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{Units: map[int]*model.Unit{
		0: {Family: map[string]*model.AddressFamily{
			"inet": {Addresses: []string{"192.0.2.1/24"}},
		}},
	}}
	newCfg.Protocols = &model.ProtocolsConfig{
		EVPN: &model.EVPNConfig{VNIs: map[int]*model.EVPNVNI{
			10010: {
				VNI:             10010,
				Type:            "l2",
				BridgeDomain:    "BD-10",
				SourceInterface: "ge-0/0/0",
				MulticastGroup:  "239.0.0.10",
			},
		}},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)

	if err := plugin.ValidateChanges(context.Background(), diff); err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}

func TestValidateChangesAllowsEVPNL3VXLAN(t *testing.T) {
	plugin := NewVPPPlugin(pkgvpp.NewMockClient(), &device.HardwareConfig{
		Interfaces: []device.PhysicalInterface{
			{Name: "ge-0/0/0", PCI: "0000:03:00.0", Driver: "avf"},
		},
	}, testLogger())
	newCfg := model.NewRouterConfig()
	newCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{Units: map[int]*model.Unit{
		0: {Family: map[string]*model.AddressFamily{
			"inet": {Addresses: []string{"192.0.2.1/24"}},
		}},
	}}
	newCfg.RoutingInstances = map[string]*model.RoutingInstance{
		"BLUE": {InstanceType: "vrf", RouteDistinguisher: "65000:200"},
	}
	newCfg.Protocols = &model.ProtocolsConfig{
		EVPN: &model.EVPNConfig{VNIs: map[int]*model.EVPNVNI{
			20010: {
				VNI:             20010,
				Type:            "l3",
				RoutingInstance: "BLUE",
				SourceInterface: "ge-0/0/0",
				MulticastGroup:  "239.0.0.20",
			},
		}},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)

	if err := plugin.ValidateChanges(context.Background(), diff); err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}

func TestValidateChangesAllowsEVPNRemoteVTEP(t *testing.T) {
	plugin := NewVPPPlugin(pkgvpp.NewMockClient(), &device.HardwareConfig{
		Interfaces: []device.PhysicalInterface{
			{Name: "ge-0/0/0", PCI: "0000:03:00.0", Driver: "avf"},
		},
	}, testLogger())
	newCfg := model.NewRouterConfig()
	newCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{Units: map[int]*model.Unit{
		0: {Family: map[string]*model.AddressFamily{
			"inet": {Addresses: []string{"192.0.2.1/24"}},
		}},
	}}
	newCfg.Protocols = &model.ProtocolsConfig{
		EVPN: &model.EVPNConfig{VNIs: map[int]*model.EVPNVNI{
			10010: {
				VNI:             10010,
				Type:            "l2",
				BridgeDomain:    "BD-10",
				SourceInterface: "ge-0/0/0",
				RemoteVTEP:      "198.51.100.10",
			},
		}},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)

	if err := plugin.ValidateChanges(context.Background(), diff); err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}

func TestValidateChangesRejectsInvalidEVPNDataplane(t *testing.T) {
	tests := []struct {
		name string
		vni  *model.EVPNVNI
		want string
	}{
		{
			name: "missing multicast",
			vni:  &model.EVPNVNI{VNI: 10010, Type: "l2", BridgeDomain: "BD-10", SourceInterface: "ge-0/0/0"},
			want: "multicast-group or remote-vtep is required",
		},
		{
			name: "unknown l3 routing instance",
			vni:  &model.EVPNVNI{VNI: 20010, Type: "l3", RoutingInstance: "RED", SourceInterface: "ge-0/0/0", MulticastGroup: "239.0.0.20"},
			want: "routing-instance RED is not configured for VPP VXLAN L3 dataplane",
		},
		{
			name: "multicast and remote vtep",
			vni:  &model.EVPNVNI{VNI: 10010, Type: "l2", BridgeDomain: "BD-10", SourceInterface: "ge-0/0/0", MulticastGroup: "239.0.0.10", RemoteVTEP: "198.51.100.10"},
			want: "multicast-group and remote-vtep are mutually exclusive",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := NewVPPPlugin(pkgvpp.NewMockClient(), &device.HardwareConfig{
				Interfaces: []device.PhysicalInterface{
					{Name: "ge-0/0/0", PCI: "0000:03:00.0", Driver: "avf"},
				},
			}, testLogger())
			newCfg := model.NewRouterConfig()
			newCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{Units: map[int]*model.Unit{
				0: {Family: map[string]*model.AddressFamily{
					"inet": {Addresses: []string{"192.0.2.1/24"}},
				}},
			}}
			newCfg.RoutingInstances = map[string]*model.RoutingInstance{"BLUE": {InstanceType: "vrf"}}
			newCfg.Protocols = &model.ProtocolsConfig{
				EVPN: &model.EVPNConfig{VNIs: map[int]*model.EVPNVNI{tt.vni.VNI: tt.vni}},
			}
			diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)

			err := plugin.ValidateChanges(context.Background(), diff)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateChanges() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateChangesAllowsRemovingEVPNIntent(t *testing.T) {
	plugin := NewVPPPlugin(pkgvpp.NewMockClient(), &device.HardwareConfig{}, testLogger())
	oldCfg := model.NewRouterConfig()
	oldCfg.Protocols = &model.ProtocolsConfig{
		EVPN: &model.EVPNConfig{VNIs: map[int]*model.EVPNVNI{
			10010: {VNI: 10010, Type: "l2", BridgeDomain: "BD-10"},
		}},
	}
	diff := engine.ComputeDiff(oldCfg, model.NewRouterConfig())

	if err := plugin.ValidateChanges(context.Background(), diff); err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil for EVPN removal", err)
	}
}

func TestValidateChangesAllowsRoutingInstances(t *testing.T) {
	plugin := NewVPPPlugin(pkgvpp.NewMockClient(), &device.HardwareConfig{}, testLogger())
	newCfg := model.NewRouterConfig()
	newCfg.RoutingInstances = map[string]*model.RoutingInstance{
		"BLUE": {InstanceType: "vrf", RouteDistinguisher: "65000:100"},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)

	if err := plugin.ValidateChanges(context.Background(), diff); err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}

func TestValidateChangesAllowsClassOfService(t *testing.T) {
	plugin := NewVPPPlugin(pkgvpp.NewMockClient(), &device.HardwareConfig{
		Interfaces: []device.PhysicalInterface{
			{Name: "ge-0/0/0", PCI: "0000:03:00.0", Driver: "avf"},
		},
	}, testLogger())
	newCfg := model.NewRouterConfig()
	newCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{Units: map[int]*model.Unit{}}
	newCfg.ClassOfService = &model.ClassOfServiceConfig{
		ForwardingClasses: map[string]*model.ForwardingClass{
			"expedited-forwarding": {Queue: 5},
		},
		TrafficControlProfiles: map[string]*model.TrafficControlProfile{
			"WAN": {ShapingRate: 1000000000, SchedulerMap: "WAN-SCHED"},
		},
		Interfaces: map[string]*model.CoSInterface{
			"ge-0/0/0": {OutputTrafficControlProfile: "WAN"},
		},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)

	if err := plugin.ValidateChanges(context.Background(), diff); err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}

func TestValidateChangesAllowsRemovingUnsupportedV06VPPConfig(t *testing.T) {
	plugin := NewVPPPlugin(pkgvpp.NewMockClient(), &device.HardwareConfig{}, testLogger())
	oldCfg := model.NewRouterConfig()
	oldCfg.RoutingInstances = map[string]*model.RoutingInstance{
		"BLUE": {InstanceType: "vrf"},
	}
	diff := engine.ComputeDiff(oldCfg, model.NewRouterConfig())

	if err := plugin.ValidateChanges(context.Background(), diff); err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}

func TestApplyChangesMapsRoutingInstanceTables(t *testing.T) {
	ctx := context.Background()
	client := pkgvpp.NewMockClient()
	plugin := NewVPPPlugin(client, &device.HardwareConfig{
		Interfaces: []device.PhysicalInterface{
			{Name: "ge-0/0/0", PCI: "0000:03:00.0", Driver: "avf"},
		},
	}, testLogger())
	if err := plugin.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { _ = plugin.Close() })

	newCfg := model.NewRouterConfig()
	newCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{Units: map[int]*model.Unit{
		0: {Family: map[string]*model.AddressFamily{
			"inet": {Addresses: []string{"192.0.2.1/24"}},
		}},
	}}
	newCfg.RoutingInstances = map[string]*model.RoutingInstance{
		"BLUE": {
			InstanceType:       "vrf",
			RouteDistinguisher: "65000:100",
			Interfaces:         []string{"ge-0/0/0"},
		},
	}

	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)
	if err := plugin.ValidateChanges(ctx, diff); err != nil {
		t.Fatalf("ValidateChanges() error = %v", err)
	}
	if err := plugin.ApplyChanges(ctx, diff); err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}

	idx, ok := plugin.GetInterfaceIndex("ge-0/0/0")
	if !ok {
		t.Fatal("ApplyChanges() did not add interface index")
	}
	for _, isIPv6 := range []bool{false, true} {
		if !client.IPTableExists(100, isIPv6) {
			t.Fatalf("routing table 100 IPv6=%t was not created", isIPv6)
		}
		if got := client.InterfaceTableID(idx, isIPv6); got != 100 {
			t.Fatalf("InterfaceTableID(IPv6=%t) = %d, want 100", isIPv6, got)
		}
	}
	state, err := plugin.CollectState(ctx)
	if err != nil {
		t.Fatalf("CollectState() error = %v", err)
	}
	if got := state["ge-0/0/0"]; got == nil || got.IPv4TableID != 100 || got.IPv6TableID != 100 {
		t.Fatalf("CollectState() table IDs = %#v, want IPv4/IPv6 table 100", got)
	}
	iface, err := client.GetInterface(ctx, idx)
	if err != nil {
		t.Fatalf("GetInterface() error = %v", err)
	}
	if len(iface.Addresses) != 1 || iface.Addresses[0].String() != "192.0.2.1/24" {
		t.Fatalf("interface addresses = %#v, want 192.0.2.1/24", iface.Addresses)
	}

	withoutRI := model.NewRouterConfig()
	withoutRI.Interfaces["ge-0/0/0"] = newCfg.Interfaces["ge-0/0/0"].Clone()
	if err := plugin.ApplyChanges(ctx, engine.ComputeDiff(newCfg, withoutRI)); err != nil {
		t.Fatalf("ApplyChanges() remove routing-instance error = %v", err)
	}
	for _, isIPv6 := range []bool{false, true} {
		if got := client.InterfaceTableID(idx, isIPv6); got != 0 {
			t.Fatalf("InterfaceTableID(IPv6=%t) after removal = %d, want 0", isIPv6, got)
		}
		if client.IPTableExists(100, isIPv6) {
			t.Fatalf("routing table 100 IPv6=%t still exists after removal", isIPv6)
		}
	}
	state, err = plugin.CollectState(ctx)
	if err != nil {
		t.Fatalf("CollectState() after removal error = %v", err)
	}
	if got := state["ge-0/0/0"]; got == nil || got.IPv4TableID != 0 || got.IPv6TableID != 0 {
		t.Fatalf("CollectState() table IDs after removal = %#v, want default table 0", got)
	}
}

func TestApplyChangesConfiguresEVPNL2VXLAN(t *testing.T) {
	ctx := context.Background()
	client := pkgvpp.NewMockClient()
	plugin := NewVPPPlugin(client, &device.HardwareConfig{
		Interfaces: []device.PhysicalInterface{
			{Name: "ge-0/0/0", PCI: "0000:03:00.0", Driver: "avf"},
		},
	}, testLogger())
	if err := plugin.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { _ = plugin.Close() })

	newCfg := model.NewRouterConfig()
	newCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{Units: map[int]*model.Unit{
		0: {Family: map[string]*model.AddressFamily{
			"inet": {Addresses: []string{"192.0.2.1/24"}},
		}},
	}}
	newCfg.Protocols = &model.ProtocolsConfig{
		EVPN: &model.EVPNConfig{VNIs: map[int]*model.EVPNVNI{
			10010: {
				VNI:             10010,
				Type:            "l2",
				BridgeDomain:    "BD-10",
				SourceInterface: "ge-0/0/0",
				MulticastGroup:  "239.0.0.10",
			},
		}},
	}

	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)
	if err := plugin.ValidateChanges(ctx, diff); err != nil {
		t.Fatalf("ValidateChanges() error = %v", err)
	}
	if err := plugin.ApplyChanges(ctx, diff); err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
	sourceIndex, ok := plugin.GetInterfaceIndex("ge-0/0/0")
	if !ok {
		t.Fatal("ApplyChanges() did not add source interface index")
	}
	vxlanIndex, ok := plugin.vxlanIfIndex[10010]
	if !ok {
		t.Fatal("ApplyChanges() did not add VXLAN interface index")
	}
	vxlanReq := pkgvpp.VXLANRequest{
		VNI:                     10010,
		SourceAddress:           net.ParseIP("192.0.2.1").To4(),
		DestinationAddress:      net.ParseIP("239.0.0.10").To4(),
		MulticastInterfaceIndex: sourceIndex,
	}
	if !client.BridgeDomainExists(10010) {
		t.Fatal("ApplyChanges() did not create bridge domain 10010")
	}
	if !client.VXLANExists(vxlanReq) {
		t.Fatalf("ApplyChanges() did not create VXLAN tunnel %#v", vxlanReq)
	}
	if bdID, ok := client.L2BridgeDomain(vxlanIndex); !ok || bdID != 10010 {
		t.Fatalf("L2BridgeDomain(%d) = %d, %t; want 10010, true", vxlanIndex, bdID, ok)
	}
	vxlanIface, err := client.GetInterface(ctx, vxlanIndex)
	if err != nil {
		t.Fatalf("GetInterface(VXLAN) error = %v", err)
	}
	if !vxlanIface.AdminUp {
		t.Fatal("VXLAN interface is not admin up")
	}

	withoutEVPN := newCfg.Clone()
	withoutEVPN.Protocols.EVPN = nil
	if err := plugin.ApplyChanges(ctx, engine.ComputeDiff(newCfg, withoutEVPN)); err != nil {
		t.Fatalf("ApplyChanges() remove EVPN error = %v", err)
	}
	if _, ok := plugin.vxlanIfIndex[10010]; ok {
		t.Fatal("ApplyChanges() left VXLAN interface index after removing EVPN")
	}
	if client.BridgeDomainExists(10010) {
		t.Fatal("ApplyChanges() left bridge domain after removing EVPN")
	}
	if client.VXLANExists(vxlanReq) {
		t.Fatal("ApplyChanges() left VXLAN tunnel after removing EVPN")
	}
}

func TestApplyChangesConfiguresEVPNL2RemoteVTEP(t *testing.T) {
	ctx := context.Background()
	client := pkgvpp.NewMockClient()
	plugin := NewVPPPlugin(client, &device.HardwareConfig{
		Interfaces: []device.PhysicalInterface{
			{Name: "ge-0/0/0", PCI: "0000:03:00.0", Driver: "avf"},
		},
	}, testLogger())
	if err := plugin.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { _ = plugin.Close() })

	newCfg := model.NewRouterConfig()
	newCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{Units: map[int]*model.Unit{
		0: {Family: map[string]*model.AddressFamily{
			"inet": {Addresses: []string{"192.0.2.1/24"}},
		}},
	}}
	newCfg.Protocols = &model.ProtocolsConfig{
		EVPN: &model.EVPNConfig{VNIs: map[int]*model.EVPNVNI{
			10010: {
				VNI:             10010,
				Type:            "l2",
				BridgeDomain:    "BD-10",
				SourceInterface: "ge-0/0/0",
				RemoteVTEP:      "198.51.100.10",
			},
		}},
	}

	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)
	if err := plugin.ValidateChanges(ctx, diff); err != nil {
		t.Fatalf("ValidateChanges() error = %v", err)
	}
	if err := plugin.ApplyChanges(ctx, diff); err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
	vxlanIndex, ok := plugin.vxlanIfIndex[10010]
	if !ok {
		t.Fatal("ApplyChanges() did not add remote VTEP VXLAN interface index")
	}
	vxlanReq := pkgvpp.VXLANRequest{
		VNI:                10010,
		SourceAddress:      net.ParseIP("192.0.2.1").To4(),
		DestinationAddress: net.ParseIP("198.51.100.10").To4(),
	}
	if !client.VXLANExists(vxlanReq) {
		t.Fatalf("ApplyChanges() did not create remote VTEP VXLAN tunnel %#v", vxlanReq)
	}
	if bdID, ok := client.L2BridgeDomain(vxlanIndex); !ok || bdID != 10010 {
		t.Fatalf("L2BridgeDomain(%d) = %d, %t; want 10010, true", vxlanIndex, bdID, ok)
	}
}

func TestApplyChangesConfiguresEVPNL3VXLAN(t *testing.T) {
	ctx := context.Background()
	client := pkgvpp.NewMockClient()
	plugin := NewVPPPlugin(client, &device.HardwareConfig{
		Interfaces: []device.PhysicalInterface{
			{Name: "ge-0/0/0", PCI: "0000:03:00.0", Driver: "avf"},
		},
	}, testLogger())
	if err := plugin.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { _ = plugin.Close() })

	newCfg := model.NewRouterConfig()
	newCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{Units: map[int]*model.Unit{
		0: {Family: map[string]*model.AddressFamily{
			"inet": {Addresses: []string{"192.0.2.1/24"}},
		}},
	}}
	newCfg.RoutingInstances = map[string]*model.RoutingInstance{
		"BLUE": {InstanceType: "vrf", RouteDistinguisher: "65000:200"},
	}
	newCfg.Protocols = &model.ProtocolsConfig{
		EVPN: &model.EVPNConfig{VNIs: map[int]*model.EVPNVNI{
			20010: {
				VNI:             20010,
				Type:            "l3",
				RoutingInstance: "BLUE",
				SourceInterface: "ge-0/0/0",
				MulticastGroup:  "239.0.0.20",
			},
		}},
	}

	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)
	if err := plugin.ValidateChanges(ctx, diff); err != nil {
		t.Fatalf("ValidateChanges() error = %v", err)
	}
	if err := plugin.ApplyChanges(ctx, diff); err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
	sourceIndex, ok := plugin.GetInterfaceIndex("ge-0/0/0")
	if !ok {
		t.Fatal("ApplyChanges() did not add source interface index")
	}
	vxlanIndex, ok := plugin.vxlanIfIndex[20010]
	if !ok {
		t.Fatal("ApplyChanges() did not add VXLAN interface index")
	}
	vxlanReq := pkgvpp.VXLANRequest{
		VNI:                     20010,
		SourceAddress:           net.ParseIP("192.0.2.1").To4(),
		DestinationAddress:      net.ParseIP("239.0.0.20").To4(),
		MulticastInterfaceIndex: sourceIndex,
		EncapsulationTable:      200,
		L3:                      true,
	}
	if !client.IPTableExists(200, false) || !client.IPTableExists(200, true) {
		t.Fatal("ApplyChanges() did not create routing-instance table pair 200")
	}
	if !client.VXLANExists(vxlanReq) {
		t.Fatalf("ApplyChanges() did not create L3 VXLAN tunnel %#v", vxlanReq)
	}
	if client.BridgeDomainExists(20010) {
		t.Fatal("ApplyChanges() created a bridge domain for L3 VXLAN")
	}
	if _, ok := client.L2BridgeDomain(vxlanIndex); ok {
		t.Fatal("ApplyChanges() attached L3 VXLAN to an L2 bridge domain")
	}
	if got := client.InterfaceTableID(vxlanIndex, false); got != 200 {
		t.Fatalf("L3 VXLAN IPv4 table = %d, want 200", got)
	}
	if got := client.InterfaceTableID(vxlanIndex, true); got != 200 {
		t.Fatalf("L3 VXLAN IPv6 table = %d, want 200", got)
	}

	withoutOverlay := model.NewRouterConfig()
	withoutOverlay.Interfaces["ge-0/0/0"] = newCfg.Interfaces["ge-0/0/0"].Clone()
	if err := plugin.ApplyChanges(ctx, engine.ComputeDiff(newCfg, withoutOverlay)); err != nil {
		t.Fatalf("ApplyChanges() remove L3 EVPN error = %v", err)
	}
	if _, ok := plugin.vxlanIfIndex[20010]; ok {
		t.Fatal("ApplyChanges() left L3 VXLAN interface index after removing EVPN")
	}
	if client.VXLANExists(vxlanReq) {
		t.Fatal("ApplyChanges() left L3 VXLAN tunnel after removing EVPN")
	}
	if client.IPTableExists(200, false) || client.IPTableExists(200, true) {
		t.Fatal("ApplyChanges() left routing-instance table pair after removing routing-instance")
	}
}

func TestApplyChangesEnablesMPLSInterfaces(t *testing.T) {
	ctx := context.Background()
	client := pkgvpp.NewMockClient()
	plugin := NewVPPPlugin(client, &device.HardwareConfig{
		Interfaces: []device.PhysicalInterface{
			{Name: "ge-0/0/0", PCI: "0000:03:00.0", Driver: "avf"},
		},
	}, testLogger())
	if err := plugin.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { _ = plugin.Close() })

	newCfg := model.NewRouterConfig()
	newCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{Units: map[int]*model.Unit{}}
	newCfg.Protocols = &model.ProtocolsConfig{
		MPLS: &model.MPLSConfig{Interfaces: []string{"ge-0/0/0"}},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)
	if err := plugin.ValidateChanges(ctx, diff); err != nil {
		t.Fatalf("ValidateChanges() error = %v", err)
	}
	if err := plugin.ApplyChanges(ctx, diff); err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
	idx, ok := plugin.GetInterfaceIndex("ge-0/0/0")
	if !ok {
		t.Fatal("ApplyChanges() did not add interface index")
	}
	if !client.MPLSInterfaceEnabled(idx) {
		t.Fatal("ApplyChanges() did not enable MPLS on interface")
	}

	withoutMPLS := model.NewRouterConfig()
	withoutMPLS.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{Units: map[int]*model.Unit{}}
	if err := plugin.ApplyChanges(ctx, engine.ComputeDiff(newCfg, withoutMPLS)); err != nil {
		t.Fatalf("ApplyChanges() disable MPLS error = %v", err)
	}
	if client.MPLSInterfaceEnabled(idx) {
		t.Fatal("ApplyChanges() left MPLS enabled after removing MPLS config")
	}
}

func TestApplyChangesAppliesClassOfServiceProfiles(t *testing.T) {
	ctx := context.Background()
	client := pkgvpp.NewMockClient()
	plugin := NewVPPPlugin(client, &device.HardwareConfig{
		Interfaces: []device.PhysicalInterface{
			{Name: "ge-0/0/0", PCI: "0000:03:00.0", Driver: "avf"},
		},
	}, testLogger())
	if err := plugin.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { _ = plugin.Close() })

	newCfg := model.NewRouterConfig()
	newCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{Units: map[int]*model.Unit{}}
	newCfg.ClassOfService = &model.ClassOfServiceConfig{
		ForwardingClasses: map[string]*model.ForwardingClass{
			"best-effort":          {Queue: 0},
			"expedited-forwarding": {Queue: 5},
		},
		TrafficControlProfiles: map[string]*model.TrafficControlProfile{
			"WAN": {ShapingRate: 1000000000, SchedulerMap: "WAN-SCHED"},
		},
		Interfaces: map[string]*model.CoSInterface{
			"ge-0/0/0": {OutputTrafficControlProfile: "WAN"},
		},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)
	if err := plugin.ValidateChanges(ctx, diff); err != nil {
		t.Fatalf("ValidateChanges() error = %v", err)
	}
	if err := plugin.ApplyChanges(ctx, diff); err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
	idx, ok := plugin.GetInterfaceIndex("ge-0/0/0")
	if !ok {
		t.Fatal("ApplyChanges() did not add interface index")
	}
	profile, ok := client.QoSProfile(idx)
	if !ok {
		t.Fatal("ApplyChanges() did not bind QoS profile")
	}
	if profile.Name != "WAN" || profile.ShapingRate != 1000000000 || profile.SchedulerMap != "WAN-SCHED" {
		t.Fatalf("QoSProfile() = %#v, want WAN shaping profile", profile)
	}
	if len(profile.Queues) != 2 || profile.Queues[0].ForwardingClass != "best-effort" || profile.Queues[0].Queue != 0 || profile.Queues[1].ForwardingClass != "expedited-forwarding" || profile.Queues[1].Queue != 5 {
		t.Fatalf("QoSProfile().Queues = %#v, want sorted forwarding-class queues", profile.Queues)
	}

	withoutCoS := model.NewRouterConfig()
	withoutCoS.Interfaces["ge-0/0/0"] = newCfg.Interfaces["ge-0/0/0"].Clone()
	if err := plugin.ApplyChanges(ctx, engine.ComputeDiff(newCfg, withoutCoS)); err != nil {
		t.Fatalf("ApplyChanges() clear class-of-service error = %v", err)
	}
	if _, ok := client.QoSProfile(idx); ok {
		t.Fatal("ApplyChanges() left QoS profile bound after removing class-of-service config")
	}
}

func TestApplyChangesRollsBackMPLSOnLaterFailure(t *testing.T) {
	ctx := context.Background()
	client := pkgvpp.NewMockClient()
	plugin := NewVPPPlugin(client, &device.HardwareConfig{
		Interfaces: []device.PhysicalInterface{
			{Name: "ge-0/0/0", PCI: "0000:03:00.0", Driver: "avf"},
			{Name: "ge-0/0/1", PCI: "0000:03:00.1", Driver: "avf"},
		},
	}, testLogger())
	if err := plugin.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { _ = plugin.Close() })

	oldCfg := model.NewRouterConfig()
	oldCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{Units: map[int]*model.Unit{}}
	oldCfg.Interfaces["ge-0/0/1"] = &model.InterfaceConfig{Units: map[int]*model.Unit{}}
	if err := plugin.ApplyChanges(ctx, engine.ComputeDiff(model.NewRouterConfig(), oldCfg)); err != nil {
		t.Fatalf("initial ApplyChanges() error = %v", err)
	}

	newCfg := model.NewRouterConfig()
	newCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{Units: map[int]*model.Unit{}}
	newCfg.Protocols = &model.ProtocolsConfig{
		MPLS: &model.MPLSConfig{Interfaces: []string{"ge-0/0/0"}},
	}
	idx, ok := plugin.GetInterfaceIndex("ge-0/0/0")
	if !ok {
		t.Fatal("initial ApplyChanges() did not add interface index")
	}
	client.SetInterfaceDownError = errors.New("down failed")
	if err := plugin.ApplyChanges(ctx, engine.ComputeDiff(oldCfg, newCfg)); err == nil {
		t.Fatal("ApplyChanges() error = nil, want remove interface failure")
	}
	if client.MPLSInterfaceEnabled(idx) {
		t.Fatal("ApplyChanges() left MPLS enabled after rollback")
	}
}

func TestRollbackChangesRestoresMPLSInterfaces(t *testing.T) {
	ctx := context.Background()
	client := pkgvpp.NewMockClient()
	plugin := NewVPPPlugin(client, &device.HardwareConfig{
		Interfaces: []device.PhysicalInterface{
			{Name: "ge-0/0/0", PCI: "0000:03:00.0", Driver: "avf"},
		},
	}, testLogger())
	if err := plugin.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { _ = plugin.Close() })

	oldCfg := model.NewRouterConfig()
	oldCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{Units: map[int]*model.Unit{}}
	oldCfg.Protocols = &model.ProtocolsConfig{
		MPLS: &model.MPLSConfig{Interfaces: []string{"ge-0/0/0"}},
	}
	if err := plugin.ApplyChanges(ctx, engine.ComputeDiff(model.NewRouterConfig(), oldCfg)); err != nil {
		t.Fatalf("initial ApplyChanges() error = %v", err)
	}
	idx, ok := plugin.GetInterfaceIndex("ge-0/0/0")
	if !ok {
		t.Fatal("initial ApplyChanges() did not add interface index")
	}

	newCfg := model.NewRouterConfig()
	newCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{Units: map[int]*model.Unit{}}
	diff := engine.ComputeDiff(oldCfg, newCfg)
	if err := plugin.ApplyChanges(ctx, diff); err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
	if client.MPLSInterfaceEnabled(idx) {
		t.Fatal("ApplyChanges() left MPLS enabled after removing MPLS config")
	}
	if err := plugin.RollbackChanges(ctx, diff); err != nil {
		t.Fatalf("RollbackChanges() error = %v", err)
	}
	if !client.MPLSInterfaceEnabled(idx) {
		t.Fatal("RollbackChanges() did not restore MPLS on interface")
	}
}

func TestRollbackChangesRestoresClassOfServiceProfiles(t *testing.T) {
	ctx := context.Background()
	client := pkgvpp.NewMockClient()
	plugin := NewVPPPlugin(client, &device.HardwareConfig{
		Interfaces: []device.PhysicalInterface{
			{Name: "ge-0/0/0", PCI: "0000:03:00.0", Driver: "avf"},
		},
	}, testLogger())
	if err := plugin.Init(ctx); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(func() { _ = plugin.Close() })

	oldCfg := model.NewRouterConfig()
	oldCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{Units: map[int]*model.Unit{}}
	oldCfg.ClassOfService = &model.ClassOfServiceConfig{
		ForwardingClasses: map[string]*model.ForwardingClass{
			"expedited-forwarding": {Queue: 5},
		},
		TrafficControlProfiles: map[string]*model.TrafficControlProfile{
			"WAN": {ShapingRate: 1000000000, SchedulerMap: "WAN-SCHED"},
		},
		Interfaces: map[string]*model.CoSInterface{
			"ge-0/0/0": {OutputTrafficControlProfile: "WAN"},
		},
	}
	if err := plugin.ApplyChanges(ctx, engine.ComputeDiff(model.NewRouterConfig(), oldCfg)); err != nil {
		t.Fatalf("initial ApplyChanges() error = %v", err)
	}
	idx, ok := plugin.GetInterfaceIndex("ge-0/0/0")
	if !ok {
		t.Fatal("initial ApplyChanges() did not add interface index")
	}

	newCfg := model.NewRouterConfig()
	newCfg.Interfaces["ge-0/0/0"] = &model.InterfaceConfig{Units: map[int]*model.Unit{}}
	diff := engine.ComputeDiff(oldCfg, newCfg)
	if err := plugin.ApplyChanges(ctx, diff); err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
	if _, ok := client.QoSProfile(idx); ok {
		t.Fatal("ApplyChanges() left QoS profile bound after removing class-of-service config")
	}
	if err := plugin.RollbackChanges(ctx, diff); err != nil {
		t.Fatalf("RollbackChanges() error = %v", err)
	}
	profile, ok := client.QoSProfile(idx)
	if !ok {
		t.Fatal("RollbackChanges() did not restore QoS profile")
	}
	if profile.Name != "WAN" || profile.ShapingRate != 1000000000 || profile.SchedulerMap != "WAN-SCHED" {
		t.Fatalf("QoSProfile() after rollback = %#v, want WAN shaping profile", profile)
	}
}
