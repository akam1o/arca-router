package device

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/akam1o/arca-router/pkg/errors"
)

func TestGetVendorName(t *testing.T) {
	testCases := []struct {
		vendorID string
		want     string
	}{
		{"0x8086", "Intel Corporation"},
		{"0x15b3", "Mellanox Technologies"},
		{"0x14e4", "Broadcom"},
		{"0x9999", "Unknown Vendor"},
	}

	for _, tc := range testCases {
		t.Run(tc.vendorID, func(t *testing.T) {
			got := GetVendorName(tc.vendorID)
			if got != tc.want {
				t.Errorf("GetVendorName(%s) = %s, want %s", tc.vendorID, got, tc.want)
			}
		})
	}
}

func TestVerifyAllPCIDevices_NilConfig(t *testing.T) {
	_, err := VerifyAllPCIDevices(nil, nil)
	if err == nil {
		t.Error("Expected error for nil config, got nil")
	}
}

func TestVerifyAllPCIDevices_EmptyConfig(t *testing.T) {
	config := &HardwareConfig{
		Interfaces: []PhysicalInterface{},
	}

	devices, err := VerifyAllPCIDevices(config, nil)
	if err != nil {
		t.Errorf("VerifyAllPCIDevices failed: %v", err)
	}

	if len(devices) != 0 {
		t.Errorf("Expected 0 devices, got %d", len(devices))
	}
}

func TestReadSysfsFile_PermissionDenied(t *testing.T) {
	// Skip if running as root (permission checks don't apply)
	if os.Geteuid() == 0 {
		t.Skip("Skipping permission test when running as root")
	}

	// Create a file with no read permissions
	tmpDir := t.TempDir()
	noReadFile := filepath.Join(tmpDir, "no_read")

	if err := os.WriteFile(noReadFile, []byte("test"), 0000); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Try to read it
	_, err := readSysfsFile(noReadFile)
	if err == nil {
		t.Fatal("Expected permission denied error, got nil")
	}

	// Verify it's a structured error with correct code
	arcaErr, ok := err.(*errors.Error)
	if !ok {
		t.Fatalf("Expected *errors.Error, got %T", err)
	}

	if arcaErr.Code != errors.ErrCodePermissionDenied {
		t.Errorf("Expected error code %s, got %s", errors.ErrCodePermissionDenied, arcaErr.Code)
	}
}

func TestVerifyPCIDevice_PreservesPermissionError(t *testing.T) {
	// This test verifies that permission errors from readSysfsFile
	// are preserved through VerifyPCIDevice (not wrapped with SYSTEM_ERROR)
	//
	// Note: This test cannot run in real sysfs environment without root,
	// so we document the expected behavior and skip the test.
	t.Skip("Requires mock sysfs with permission-denied files")

	// Expected behavior:
	// 1. readSysfsFile returns ErrCodePermissionDenied
	// 2. VerifyPCIDevice preserves the error code (not wrapped)
	// 3. VerifyAllPCIDevices preserves the error code (re-wrapped with context)
}
