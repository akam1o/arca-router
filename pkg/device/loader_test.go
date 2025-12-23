package device

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadHardware_Success(t *testing.T) {
	// Create temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "hardware.yaml")

	validYAML := `interfaces:
  - name: "ge-0/0/0"
    pci: "0000:03:00.0"
    driver: "avf"
    description: "Test Interface"
`

	if err := os.WriteFile(testFile, []byte(validYAML), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test loading (with nil logger)
	config, err := LoadHardware(testFile, nil)
	if err != nil {
		t.Fatalf("LoadHardware failed: %v", err)
	}

	// Verify results
	if len(config.Interfaces) != 1 {
		t.Errorf("Expected 1 interface, got %d", len(config.Interfaces))
	}

	iface := config.Interfaces[0]
	if iface.Name != "ge-0/0/0" {
		t.Errorf("Expected name 'ge-0/0/0', got '%s'", iface.Name)
	}
	if iface.PCI != "0000:03:00.0" {
		t.Errorf("Expected PCI '0000:03:00.0', got '%s'", iface.PCI)
	}
	if iface.Driver != "avf" {
		t.Errorf("Expected driver 'avf', got '%s'", iface.Driver)
	}
}

func TestLoadHardware_FileNotFound(t *testing.T) {
	_, err := LoadHardware("/nonexistent/hardware.yaml", nil)
	if err == nil {
		t.Error("Expected error for nonexistent file, got nil")
	}
}

func TestLoadHardware_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "invalid.yaml")

	invalidYAML := `interfaces:
  - name: "ge-0/0/0
    pci: "invalid yaml
`

	if err := os.WriteFile(testFile, []byte(invalidYAML), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	_, err := LoadHardware(testFile, nil)
	if err == nil {
		t.Error("Expected error for invalid YAML, got nil")
	}
}

func TestLoadHardware_UnknownField(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "unknown_field.yaml")

	unknownFieldYAML := `interfaces:
  - name: "ge-0/0/0"
    pci: "0000:03:00.0"
    driver: "avf"
    unknown_field: "this should fail"
`

	if err := os.WriteFile(testFile, []byte(unknownFieldYAML), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	_, err := LoadHardware(testFile, nil)
	if err == nil {
		t.Error("Expected error for unknown field, got nil")
	}
}

func TestLoadHardware_InvalidDriver(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "invalid_driver.yaml")

	invalidDriverYAML := `interfaces:
  - name: "ge-0/0/0"
    pci: "0000:03:00.0"
    driver: "invalid_driver"
`

	if err := os.WriteFile(testFile, []byte(invalidDriverYAML), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	_, err := LoadHardware(testFile, nil)
	if err == nil {
		t.Error("Expected error for invalid driver, got nil")
	}
}

func TestValidateHardwareConfig_EmptyInterfaces(t *testing.T) {
	config := &HardwareConfig{
		Interfaces: []PhysicalInterface{},
	}

	err := ValidateHardwareConfig(config)
	if err == nil {
		t.Error("Expected error for empty interfaces, got nil")
	}
}

func TestValidateHardwareConfig_InvalidPCIFormat(t *testing.T) {
	config := &HardwareConfig{
		Interfaces: []PhysicalInterface{
			{
				Name:   "ge-0/0/0",
				PCI:    "invalid-pci",
				Driver: "avf",
			},
		},
	}

	err := ValidateHardwareConfig(config)
	if err == nil {
		t.Error("Expected error for invalid PCI format, got nil")
	}
}

func TestValidateHardwareConfig_DuplicateName(t *testing.T) {
	config := &HardwareConfig{
		Interfaces: []PhysicalInterface{
			{
				Name:   "ge-0/0/0",
				PCI:    "0000:03:00.0",
				Driver: "avf",
			},
			{
				Name:   "ge-0/0/0", // Duplicate
				PCI:    "0000:03:00.1",
				Driver: "avf",
			},
		},
	}

	err := ValidateHardwareConfig(config)
	if err == nil {
		t.Error("Expected error for duplicate interface name, got nil")
	}
}

func TestValidateHardwareConfig_DuplicatePCI(t *testing.T) {
	config := &HardwareConfig{
		Interfaces: []PhysicalInterface{
			{
				Name:   "ge-0/0/0",
				PCI:    "0000:03:00.0",
				Driver: "avf",
			},
			{
				Name:   "ge-0/0/1",
				PCI:    "0000:03:00.0", // Duplicate
				Driver: "avf",
			},
		},
	}

	err := ValidateHardwareConfig(config)
	if err == nil {
		t.Error("Expected error for duplicate PCI address, got nil")
	}
}

func TestValidateHardwareConfig_InvalidInterfaceName(t *testing.T) {
	testCases := []struct {
		name       string
		ifaceName  string
		shouldFail bool
	}{
		{"valid ge", "ge-0/0/0", false},
		{"valid xe", "xe-1/2/3", false},
		{"invalid prefix", "eth0", true},
		{"invalid format", "ge-0-0-0", true},
		{"missing parts", "ge-0/0", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := &HardwareConfig{
				Interfaces: []PhysicalInterface{
					{
						Name:   tc.ifaceName,
						PCI:    "0000:03:00.0",
						Driver: "avf",
					},
				},
			}

			err := ValidateHardwareConfig(config)
			if tc.shouldFail && err == nil {
				t.Errorf("Expected error for interface name '%s', got nil", tc.ifaceName)
			}
			if !tc.shouldFail && err != nil {
				t.Errorf("Expected no error for interface name '%s', got: %v", tc.ifaceName, err)
			}
		})
	}
}

func TestIsValidInterfaceName(t *testing.T) {
	testCases := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid ge", "ge-0/0/0", true},
		{"valid xe", "xe-1/2/3", true},
		{"valid multi-digit", "ge-10/20/30", true},
		{"invalid prefix", "eth0", false},
		{"invalid separator", "ge-0-0-0", false},
		{"missing parts", "ge-0/0", false},
		{"extra parts", "ge-0/0/0/0", false},
		{"empty", "", false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := isValidInterfaceName(tc.input)
			if got != tc.want {
				t.Errorf("isValidInterfaceName(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}
