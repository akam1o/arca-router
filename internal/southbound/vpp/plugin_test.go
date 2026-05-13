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

func TestValidateChangesRejectsUnsupportedV06VPPConfig(t *testing.T) {
	plugin := NewVPPPlugin(pkgvpp.NewMockClient(), &device.HardwareConfig{}, testLogger())
	newCfg := model.NewRouterConfig()
	newCfg.Protocols = &model.ProtocolsConfig{
		MPLS: &model.MPLSConfig{Interfaces: []string{"ge-0/0/0"}},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)

	err := plugin.ValidateChanges(context.Background(), diff)
	if err == nil {
		t.Fatal("ValidateChanges() error = nil, want unsupported MPLS error")
	}
	if !strings.Contains(err.Error(), "protocols mpls") {
		t.Fatalf("ValidateChanges() error = %v, want protocols mpls", err)
	}
}

func TestValidateChangesRejectsUnsupportedClassOfService(t *testing.T) {
	plugin := NewVPPPlugin(pkgvpp.NewMockClient(), &device.HardwareConfig{}, testLogger())
	newCfg := model.NewRouterConfig()
	newCfg.ClassOfService = &model.ClassOfServiceConfig{
		ForwardingClasses: map[string]*model.ForwardingClass{
			"expedited-forwarding": {Queue: 5},
		},
	}
	diff := engine.ComputeDiff(model.NewRouterConfig(), newCfg)

	err := plugin.ValidateChanges(context.Background(), diff)
	if err == nil {
		t.Fatal("ValidateChanges() error = nil, want unsupported class-of-service error")
	}
	if !strings.Contains(err.Error(), "class-of-service") {
		t.Fatalf("ValidateChanges() error = %v, want class-of-service", err)
	}
}

func TestValidateChangesAllowsRemovingUnsupportedV06VPPConfig(t *testing.T) {
	plugin := NewVPPPlugin(pkgvpp.NewMockClient(), &device.HardwareConfig{}, testLogger())
	oldCfg := model.NewRouterConfig()
	oldCfg.Protocols = &model.ProtocolsConfig{
		MPLS: &model.MPLSConfig{Interfaces: []string{"ge-0/0/0"}},
	}
	diff := engine.ComputeDiff(oldCfg, model.NewRouterConfig())

	if err := plugin.ValidateChanges(context.Background(), diff); err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}
