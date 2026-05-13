package vpp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	"github.com/akam1o/arca-router/pkg/device"
	pkgvpp "github.com/akam1o/arca-router/pkg/vpp"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
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
