package vpp

import (
	"context"
	"net"
	"testing"

	"github.com/akam1o/arca-router/pkg/errors"
)

func TestMockClient_Connect(t *testing.T) {
	client := NewMockClient()
	ctx := context.Background()

	// First connection should succeed
	err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect() error = %v, want nil", err)
	}

	// Second connection should fail
	err = client.Connect(ctx)
	if err == nil {
		t.Error("Connect() expected error for duplicate connection, got nil")
	}

	// Close and reconnect should succeed
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}

	err = client.Connect(ctx)
	if err != nil {
		t.Fatalf("Reconnect() error = %v, want nil", err)
	}
}

func TestMockClient_ConnectError(t *testing.T) {
	client := NewMockClient()
	client.ConnectError = errors.New(
		errors.ErrCodeVPPConnection,
		"Mock connection error",
		"Test error",
		"Test action",
	)

	ctx := context.Background()
	err := client.Connect(ctx)
	if err == nil {
		t.Error("Connect() expected error, got nil")
	}
}

func TestMockClient_Close(t *testing.T) {
	client := NewMockClient()
	ctx := context.Background()

	// Close without connect should fail
	err := client.Close()
	if err == nil {
		t.Error("Close() expected error when not connected, got nil")
	}

	// Connect and close should succeed
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v, want nil", err)
	}

	err = client.Close()
	if err != nil {
		t.Fatalf("Close() error = %v, want nil", err)
	}
}

func TestMockClient_CreateInterface(t *testing.T) {
	client := NewMockClient()
	ctx := context.Background()

	// Create without connect should fail
	_, err := client.CreateInterface(ctx, &CreateInterfaceRequest{Type: InterfaceTypeAVF})
	if err == nil {
		t.Error("CreateInterface() expected error when not connected, got nil")
	}

	// Connect
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v, want nil", err)
	}

	// Create interface should succeed
	iface, err := client.CreateInterface(ctx, &CreateInterfaceRequest{
		Type:           InterfaceTypeAVF,
		DeviceInstance: "0000:00:04.0",
		NumRxQueues:    1,
		NumTxQueues:    1,
	})
	if err != nil {
		t.Fatalf("CreateInterface() error = %v, want nil", err)
	}

	if iface == nil {
		t.Fatal("CreateInterface() returned nil interface")
	}

	if iface.SwIfIndex == 0 {
		t.Error("SwIfIndex = 0, want non-zero")
	}

	if iface.Name == "" {
		t.Error("Name is empty, want non-empty")
	}

	if iface.AdminUp {
		t.Error("AdminUp = true, want false (initial state)")
	}
}

func TestMockClient_CreateInterfaceInvalidType(t *testing.T) {
	client := NewMockClient()
	ctx := context.Background()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v, want nil", err)
	}

	// Create with empty type should fail
	_, err := client.CreateInterface(ctx, &CreateInterfaceRequest{Type: ""})
	if err == nil {
		t.Error("CreateInterface() expected error for empty type, got nil")
	}

	// Create with invalid type should fail
	_, err = client.CreateInterface(ctx, &CreateInterfaceRequest{Type: "invalid"})
	if err == nil {
		t.Error("CreateInterface() expected error for invalid type, got nil")
	}
}

func TestMockClient_SetInterfaceUp(t *testing.T) {
	client := NewMockClient()
	ctx := context.Background()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v, want nil", err)
	}

	iface, err := client.CreateInterface(ctx, &CreateInterfaceRequest{Type: InterfaceTypeAVF})
	if err != nil {
		t.Fatalf("CreateInterface() error = %v, want nil", err)
	}

	// Set interface up
	err = client.SetInterfaceUp(ctx, iface.SwIfIndex)
	if err != nil {
		t.Fatalf("SetInterfaceUp() error = %v, want nil", err)
	}

	// Verify state
	updatedIface, err := client.GetInterface(ctx, iface.SwIfIndex)
	if err != nil {
		t.Fatalf("GetInterface() error = %v, want nil", err)
	}

	if !updatedIface.AdminUp {
		t.Error("AdminUp = false, want true")
	}

	if !updatedIface.LinkUp {
		t.Error("LinkUp = false, want true")
	}
}

func TestMockClient_SetInterfaceDown(t *testing.T) {
	client := NewMockClient()
	ctx := context.Background()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v, want nil", err)
	}

	iface, err := client.CreateInterface(ctx, &CreateInterfaceRequest{Type: InterfaceTypeAVF})
	if err != nil {
		t.Fatalf("CreateInterface() error = %v, want nil", err)
	}

	// Set up then down
	if err := client.SetInterfaceUp(ctx, iface.SwIfIndex); err != nil {
		t.Fatalf("SetInterfaceUp() error = %v, want nil", err)
	}

	err = client.SetInterfaceDown(ctx, iface.SwIfIndex)
	if err != nil {
		t.Fatalf("SetInterfaceDown() error = %v, want nil", err)
	}

	// Verify state
	updatedIface, err := client.GetInterface(ctx, iface.SwIfIndex)
	if err != nil {
		t.Fatalf("GetInterface() error = %v, want nil", err)
	}

	if updatedIface.AdminUp {
		t.Error("AdminUp = true, want false")
	}

	if updatedIface.LinkUp {
		t.Error("LinkUp = true, want false")
	}
}

func TestMockClient_SetInterfaceAddress(t *testing.T) {
	client := NewMockClient()
	ctx := context.Background()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v, want nil", err)
	}

	iface, err := client.CreateInterface(ctx, &CreateInterfaceRequest{Type: InterfaceTypeAVF})
	if err != nil {
		t.Fatalf("CreateInterface() error = %v, want nil", err)
	}

	// Add address
	_, ipnet, _ := net.ParseCIDR("192.168.1.1/24")
	err = client.SetInterfaceAddress(ctx, iface.SwIfIndex, ipnet)
	if err != nil {
		t.Fatalf("SetInterfaceAddress() error = %v, want nil", err)
	}

	// Verify address
	updatedIface, err := client.GetInterface(ctx, iface.SwIfIndex)
	if err != nil {
		t.Fatalf("GetInterface() error = %v, want nil", err)
	}

	if len(updatedIface.Addresses) != 1 {
		t.Fatalf("len(Addresses) = %d, want 1", len(updatedIface.Addresses))
	}

	if !updatedIface.Addresses[0].IP.Equal(ipnet.IP) {
		t.Errorf("Address IP = %v, want %v", updatedIface.Addresses[0].IP, ipnet.IP)
	}

	// Adding duplicate address should fail
	err = client.SetInterfaceAddress(ctx, iface.SwIfIndex, ipnet)
	if err == nil {
		t.Error("SetInterfaceAddress() expected error for duplicate address, got nil")
	}
}

func TestMockClient_DeleteInterfaceAddress(t *testing.T) {
	client := NewMockClient()
	ctx := context.Background()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v, want nil", err)
	}

	iface, err := client.CreateInterface(ctx, &CreateInterfaceRequest{Type: InterfaceTypeAVF})
	if err != nil {
		t.Fatalf("CreateInterface() error = %v, want nil", err)
	}

	// Add address
	_, ipnet, _ := net.ParseCIDR("192.168.1.1/24")
	if err := client.SetInterfaceAddress(ctx, iface.SwIfIndex, ipnet); err != nil {
		t.Fatalf("SetInterfaceAddress() error = %v, want nil", err)
	}

	// Delete address
	err = client.DeleteInterfaceAddress(ctx, iface.SwIfIndex, ipnet)
	if err != nil {
		t.Fatalf("DeleteInterfaceAddress() error = %v, want nil", err)
	}

	// Verify address removed
	updatedIface, err := client.GetInterface(ctx, iface.SwIfIndex)
	if err != nil {
		t.Fatalf("GetInterface() error = %v, want nil", err)
	}

	if len(updatedIface.Addresses) != 0 {
		t.Errorf("len(Addresses) = %d, want 0", len(updatedIface.Addresses))
	}

	// Deleting non-existent address should fail
	err = client.DeleteInterfaceAddress(ctx, iface.SwIfIndex, ipnet)
	if err == nil {
		t.Error("DeleteInterfaceAddress() expected error for non-existent address, got nil")
	}
}

func TestMockClient_GetInterface(t *testing.T) {
	client := NewMockClient()
	ctx := context.Background()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v, want nil", err)
	}

	// Get non-existent interface should fail
	_, err := client.GetInterface(ctx, 999)
	if err == nil {
		t.Error("GetInterface() expected error for non-existent interface, got nil")
	}

	// Create and get interface
	iface, err := client.CreateInterface(ctx, &CreateInterfaceRequest{Type: InterfaceTypeAVF})
	if err != nil {
		t.Fatalf("CreateInterface() error = %v, want nil", err)
	}

	retrievedIface, err := client.GetInterface(ctx, iface.SwIfIndex)
	if err != nil {
		t.Fatalf("GetInterface() error = %v, want nil", err)
	}

	if retrievedIface.SwIfIndex != iface.SwIfIndex {
		t.Errorf("SwIfIndex = %d, want %d", retrievedIface.SwIfIndex, iface.SwIfIndex)
	}
}

func TestMockClient_ListInterfaces(t *testing.T) {
	client := NewMockClient()
	ctx := context.Background()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v, want nil", err)
	}

	// List empty interfaces
	interfaces, err := client.ListInterfaces(ctx)
	if err != nil {
		t.Fatalf("ListInterfaces() error = %v, want nil", err)
	}

	if len(interfaces) != 0 {
		t.Errorf("len(interfaces) = %d, want 0", len(interfaces))
	}

	// Create interfaces
	for i := 0; i < 3; i++ {
		_, err := client.CreateInterface(ctx, &CreateInterfaceRequest{Type: InterfaceTypeAVF})
		if err != nil {
			t.Fatalf("CreateInterface() error = %v, want nil", err)
		}
	}

	// List interfaces
	interfaces, err = client.ListInterfaces(ctx)
	if err != nil {
		t.Fatalf("ListInterfaces() error = %v, want nil", err)
	}

	if len(interfaces) != 3 {
		t.Errorf("len(interfaces) = %d, want 3", len(interfaces))
	}
}

func TestMockClient_Reset(t *testing.T) {
	client := NewMockClient()
	ctx := context.Background()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v, want nil", err)
	}

	_, err := client.CreateInterface(ctx, &CreateInterfaceRequest{Type: InterfaceTypeAVF})
	if err != nil {
		t.Fatalf("CreateInterface() error = %v, want nil", err)
	}

	// Reset
	client.Reset()

	// Connection should be closed
	interfaces, err := client.ListInterfaces(ctx)
	if err == nil {
		t.Error("ListInterfaces() expected error after reset, got nil")
	}
	if interfaces != nil {
		t.Error("ListInterfaces() expected nil interfaces after reset")
	}

	// Should be able to reconnect
	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() after reset error = %v, want nil", err)
	}

	// Interfaces should be empty
	interfaces, err = client.ListInterfaces(ctx)
	if err != nil {
		t.Fatalf("ListInterfaces() error = %v, want nil", err)
	}

	if len(interfaces) != 0 {
		t.Errorf("len(interfaces) = %d, want 0 after reset", len(interfaces))
	}
}

func TestMockClient_ErrorInjection(t *testing.T) {
	client := NewMockClient()
	ctx := context.Background()

	// Test various error injections
	testErr := errors.New(errors.ErrCodeVPPOperation, "Test error", "Test cause", "Test action")

	tests := []struct {
		name      string
		setup     func()
		operation func() error
	}{
		{
			name: "CreateInterfaceError",
			setup: func() {
				client.Reset()
				if err := client.Connect(ctx); err != nil {
					t.Fatalf("Connect failed: %v", err)
				}
				client.CreateInterfaceError = testErr
			},
			operation: func() error {
				_, err := client.CreateInterface(ctx, &CreateInterfaceRequest{Type: InterfaceTypeAVF})
				return err
			},
		},
		{
			name: "SetInterfaceUpError",
			setup: func() {
				client.Reset()
				if err := client.Connect(ctx); err != nil {
					t.Fatalf("Connect failed: %v", err)
				}
				iface, err := client.CreateInterface(ctx, &CreateInterfaceRequest{Type: InterfaceTypeAVF})
				if err != nil {
					t.Fatalf("CreateInterface failed: %v", err)
				}
				client.SetInterfaceUpError = testErr
				client.mu.Lock()
				client.interfaces[iface.SwIfIndex] = iface
				client.mu.Unlock()
			},
			operation: func() error {
				return client.SetInterfaceUp(ctx, 1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup()
			err := tt.operation()
			if err == nil {
				t.Error("operation() expected error, got nil")
			}
		})
	}
}

func TestMockClient_ImmutabilityAfterSetAddress(t *testing.T) {
	client := NewMockClient()
	ctx := context.Background()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v, want nil", err)
	}

	iface, err := client.CreateInterface(ctx, &CreateInterfaceRequest{Type: InterfaceTypeAVF})
	if err != nil {
		t.Fatalf("CreateInterface() error = %v, want nil", err)
	}

	// Add address
	_, ipnet, _ := net.ParseCIDR("192.168.1.1/24")
	originalIP := ipnet.IP.String()
	originalMask := ipnet.Mask.String()

	if err := client.SetInterfaceAddress(ctx, iface.SwIfIndex, ipnet); err != nil {
		t.Fatalf("SetInterfaceAddress() error = %v, want nil", err)
	}

	// Try to mutate the address after adding
	ipnet.IP[3] = 99
	ipnet.Mask[3] = 0

	// Retrieve interface and verify address is unchanged
	updatedIface, err := client.GetInterface(ctx, iface.SwIfIndex)
	if err != nil {
		t.Fatalf("GetInterface() error = %v, want nil", err)
	}

	if len(updatedIface.Addresses) != 1 {
		t.Fatalf("len(Addresses) = %d, want 1", len(updatedIface.Addresses))
	}

	storedAddr := updatedIface.Addresses[0]
	if storedAddr.IP.String() != originalIP {
		t.Errorf("Address IP changed after external mutation: got %s, want %s", storedAddr.IP.String(), originalIP)
	}
	if storedAddr.Mask.String() != originalMask {
		t.Errorf("Address Mask changed after external mutation: got %s, want %s", storedAddr.Mask.String(), originalMask)
	}
}

func TestMockClient_CreateInterfaceNilRequest(t *testing.T) {
	client := NewMockClient()
	ctx := context.Background()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v, want nil", err)
	}

	// Create with nil request should fail
	_, err := client.CreateInterface(ctx, nil)
	if err == nil {
		t.Error("CreateInterface(nil) expected error, got nil")
	}
}
