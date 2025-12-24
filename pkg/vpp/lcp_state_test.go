package vpp

import (
	"context"
	"testing"
)

func TestLCPStateManager_Create(t *testing.T) {
	ctx := context.Background()
	client := NewMockClient()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	manager := NewLCPStateManager(client)

	// Create a VPP interface first
	iface, err := client.CreateInterface(ctx, &CreateInterfaceRequest{
		Type: InterfaceTypeAVF,
		Name: "test0",
	})
	if err != nil {
		t.Fatalf("CreateInterface failed: %v", err)
	}

	// Create LCP pair
	linuxName := "ge000"
	junosName := "ge-0/0/0"
	if err := manager.Create(ctx, iface.SwIfIndex, linuxName, junosName); err != nil {
		t.Fatalf("Create LCP failed: %v", err)
	}

	// Verify cache is updated
	lcp, err := manager.Get(ctx, iface.SwIfIndex)
	if err != nil {
		t.Fatalf("Get LCP failed: %v", err)
	}

	if lcp.VPPSwIfIndex != iface.SwIfIndex {
		t.Errorf("VPPSwIfIndex mismatch: got %d, want %d", lcp.VPPSwIfIndex, iface.SwIfIndex)
	}
	if lcp.LinuxIfName != linuxName {
		t.Errorf("LinuxIfName mismatch: got %s, want %s", lcp.LinuxIfName, linuxName)
	}
	if lcp.JunosName != junosName {
		t.Errorf("JunosName mismatch: got %s, want %s", lcp.JunosName, junosName)
	}
}

func TestLCPStateManager_Delete(t *testing.T) {
	ctx := context.Background()
	client := NewMockClient()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	manager := NewLCPStateManager(client)

	// Create VPP interface and LCP pair
	iface, err := client.CreateInterface(ctx, &CreateInterfaceRequest{
		Type: InterfaceTypeAVF,
		Name: "test0",
	})
	if err != nil {
		t.Fatalf("CreateInterface failed: %v", err)
	}

	if err := manager.Create(ctx, iface.SwIfIndex, "ge000", "ge-0/0/0"); err != nil {
		t.Fatalf("Create LCP failed: %v", err)
	}

	// Delete LCP pair
	if err := manager.Delete(ctx, iface.SwIfIndex); err != nil {
		t.Fatalf("Delete LCP failed: %v", err)
	}

	// Verify cache is updated
	_, err = manager.Get(ctx, iface.SwIfIndex)
	if err == nil {
		t.Error("Expected error when getting deleted LCP, got nil")
	}
}

func TestLCPStateManager_Sync(t *testing.T) {
	ctx := context.Background()
	client := NewMockClient()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	manager := NewLCPStateManager(client)

	// Create VPP interfaces and LCP pairs directly via client
	iface1, _ := client.CreateInterface(ctx, &CreateInterfaceRequest{
		Type: InterfaceTypeAVF,
		Name: "test0",
	})
	client.CreateLCPInterface(ctx, iface1.SwIfIndex, "ge000")

	iface2, _ := client.CreateInterface(ctx, &CreateInterfaceRequest{
		Type: InterfaceTypeAVF,
		Name: "test1",
	})
	client.CreateLCPInterface(ctx, iface2.SwIfIndex, "ge001")

	// Sync should populate cache
	if err := manager.Sync(ctx); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	// Verify both LCP pairs are in cache
	lcps := manager.List()
	if len(lcps) != 2 {
		t.Errorf("Expected 2 LCP pairs in cache, got %d", len(lcps))
	}

	// Verify individual retrieval
	lcp1, err := manager.Get(ctx, iface1.SwIfIndex)
	if err != nil {
		t.Fatalf("Get LCP 1 failed: %v", err)
	}
	if lcp1.LinuxIfName != "ge000" {
		t.Errorf("LCP 1 LinuxIfName mismatch: got %s, want ge000", lcp1.LinuxIfName)
	}

	lcp2, err := manager.Get(ctx, iface2.SwIfIndex)
	if err != nil {
		t.Fatalf("Get LCP 2 failed: %v", err)
	}
	if lcp2.LinuxIfName != "ge001" {
		t.Errorf("LCP 2 LinuxIfName mismatch: got %s, want ge001", lcp2.LinuxIfName)
	}
}

func TestLCPStateManager_GetByJunosName(t *testing.T) {
	ctx := context.Background()
	client := NewMockClient()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	manager := NewLCPStateManager(client)

	// Create VPP interface and LCP pair
	iface, err := client.CreateInterface(ctx, &CreateInterfaceRequest{
		Type: InterfaceTypeAVF,
		Name: "test0",
	})
	if err != nil {
		t.Fatalf("CreateInterface failed: %v", err)
	}

	junosName := "ge-0/0/0"
	if err := manager.Create(ctx, iface.SwIfIndex, "ge000", junosName); err != nil {
		t.Fatalf("Create LCP failed: %v", err)
	}

	// Retrieve by Junos name
	lcp, err := manager.GetByJunosName(junosName)
	if err != nil {
		t.Fatalf("GetByJunosName failed: %v", err)
	}

	if lcp.VPPSwIfIndex != iface.SwIfIndex {
		t.Errorf("VPPSwIfIndex mismatch: got %d, want %d", lcp.VPPSwIfIndex, iface.SwIfIndex)
	}
	if lcp.JunosName != junosName {
		t.Errorf("JunosName mismatch: got %s, want %s", lcp.JunosName, junosName)
	}

	// Test non-existent Junos name
	_, err = manager.GetByJunosName("xe-1/2/3")
	if err == nil {
		t.Error("Expected error for non-existent Junos name, got nil")
	}
}

func TestLCPStateManager_GetByLinuxName(t *testing.T) {
	ctx := context.Background()
	client := NewMockClient()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	manager := NewLCPStateManager(client)

	// Create VPP interface and LCP pair
	iface, err := client.CreateInterface(ctx, &CreateInterfaceRequest{
		Type: InterfaceTypeAVF,
		Name: "test0",
	})
	if err != nil {
		t.Fatalf("CreateInterface failed: %v", err)
	}

	linuxName := "ge000"
	if err := manager.Create(ctx, iface.SwIfIndex, linuxName, "ge-0/0/0"); err != nil {
		t.Fatalf("Create LCP failed: %v", err)
	}

	// Retrieve by Linux name
	lcp, err := manager.GetByLinuxName(linuxName)
	if err != nil {
		t.Fatalf("GetByLinuxName failed: %v", err)
	}

	if lcp.VPPSwIfIndex != iface.SwIfIndex {
		t.Errorf("VPPSwIfIndex mismatch: got %d, want %d", lcp.VPPSwIfIndex, iface.SwIfIndex)
	}
	if lcp.LinuxIfName != linuxName {
		t.Errorf("LinuxIfName mismatch: got %s, want %s", lcp.LinuxIfName, linuxName)
	}

	// Test non-existent Linux name
	_, err = manager.GetByLinuxName("xe123")
	if err == nil {
		t.Error("Expected error for non-existent Linux name, got nil")
	}
}

func TestLCPStateManager_CheckConsistency(t *testing.T) {
	ctx := context.Background()
	client := NewMockClient()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	manager := NewLCPStateManager(client)

	// Create VPP interface and LCP pair
	iface, err := client.CreateInterface(ctx, &CreateInterfaceRequest{
		Type: InterfaceTypeAVF,
		Name: "test0",
	})
	if err != nil {
		t.Fatalf("CreateInterface failed: %v", err)
	}

	if err := manager.Create(ctx, iface.SwIfIndex, "ge000", "ge-0/0/0"); err != nil {
		t.Fatalf("Create LCP failed: %v", err)
	}

	// Check consistency - should be consistent
	inconsistencies, err := manager.CheckConsistency(ctx)
	if err != nil {
		t.Fatalf("CheckConsistency failed: %v", err)
	}
	if len(inconsistencies) > 0 {
		t.Errorf("Expected no inconsistencies, got %d: %v", len(inconsistencies), inconsistencies)
	}

	// Create inconsistency by directly modifying VPP state
	iface2, _ := client.CreateInterface(ctx, &CreateInterfaceRequest{
		Type: InterfaceTypeAVF,
		Name: "test1",
	})
	client.CreateLCPInterface(ctx, iface2.SwIfIndex, "ge001")

	// Check consistency - should find inconsistency
	inconsistencies, err = manager.CheckConsistency(ctx)
	if err != nil {
		t.Fatalf("CheckConsistency failed: %v", err)
	}
	if len(inconsistencies) == 0 {
		t.Error("Expected inconsistencies to be detected")
	}

	// Sync should resolve inconsistencies
	if err := manager.Sync(ctx); err != nil {
		t.Fatalf("Sync failed: %v", err)
	}

	inconsistencies, err = manager.CheckConsistency(ctx)
	if err != nil {
		t.Fatalf("CheckConsistency after Sync failed: %v", err)
	}
	if len(inconsistencies) > 0 {
		t.Errorf("Expected no inconsistencies after sync, got %d: %v", len(inconsistencies), inconsistencies)
	}
}

func TestLCPStateManager_List(t *testing.T) {
	ctx := context.Background()
	client := NewMockClient()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	manager := NewLCPStateManager(client)

	// Initially empty
	lcps := manager.List()
	if len(lcps) != 0 {
		t.Errorf("Expected empty list, got %d items", len(lcps))
	}

	// Create multiple LCP pairs
	for i := 0; i < 3; i++ {
		iface, _ := client.CreateInterface(ctx, &CreateInterfaceRequest{
			Type: InterfaceTypeAVF,
			Name: "test",
		})
		manager.Create(ctx, iface.SwIfIndex, "ge000", "ge-0/0/0")
	}

	// Verify list contains all pairs
	lcps = manager.List()
	if len(lcps) != 3 {
		t.Errorf("Expected 3 LCP pairs, got %d", len(lcps))
	}
}

func TestLCPStateManager_Clear(t *testing.T) {
	ctx := context.Background()
	client := NewMockClient()
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	manager := NewLCPStateManager(client)

	// Create LCP pair
	iface, err := client.CreateInterface(ctx, &CreateInterfaceRequest{
		Type: InterfaceTypeAVF,
		Name: "test0",
	})
	if err != nil {
		t.Fatalf("CreateInterface failed: %v", err)
	}

	if err := manager.Create(ctx, iface.SwIfIndex, "ge000", "ge-0/0/0"); err != nil {
		t.Fatalf("Create LCP failed: %v", err)
	}

	// Verify cache is populated
	lcps := manager.List()
	if len(lcps) == 0 {
		t.Fatal("Expected cache to be populated")
	}

	// Clear cache
	manager.Clear()

	// Verify cache is empty
	lcps = manager.List()
	if len(lcps) != 0 {
		t.Errorf("Expected empty cache after Clear, got %d items", len(lcps))
	}
}
