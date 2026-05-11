package vpp

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	"github.com/akam1o/arca-router/pkg/device"
	pkgvpp "github.com/akam1o/arca-router/pkg/vpp"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
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
			"ge-0/0/0": {Units: map[int]*model.Unit{}},
		},
	})
	if err := plugin.ApplyChanges(ctx, diff); err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
	if _, ok := plugin.GetInterfaceIndex("ge-0/0/0"); !ok {
		t.Fatal("ApplyChanges() did not add interface index")
	}

	if err := plugin.RollbackChanges(ctx, diff); err != nil {
		t.Fatalf("RollbackChanges() error = %v", err)
	}
	if _, ok := plugin.GetInterfaceIndex("ge-0/0/0"); ok {
		t.Fatal("RollbackChanges() left added interface in index")
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
