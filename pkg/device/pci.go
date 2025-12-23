package device

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/akam1o/arca-router/pkg/errors"
	"github.com/akam1o/arca-router/pkg/logger"
)

// PCIDevice represents information about a PCI device
type PCIDevice struct {
	Address  string
	VendorID string
	DeviceID string
	Driver   string
}

// VerifyPCIDevice checks if a PCI device exists and retrieves its information
func VerifyPCIDevice(pciAddr string, log *logger.Logger) (*PCIDevice, error) {
	sysfsPath := filepath.Join("/sys/bus/pci/devices", pciAddr)

	if log != nil {
		log.Debug("Verifying PCI device", slog.String("pci_address", pciAddr))
	}

	// Check if device exists
	if _, err := os.Stat(sysfsPath); os.IsNotExist(err) {
		return nil, errors.PCINotFound(pciAddr)
	}

	device := &PCIDevice{
		Address: pciAddr,
	}

	// Read vendor ID
	vendorID, err := readSysfsFile(filepath.Join(sysfsPath, "vendor"))
	if err != nil {
		// Preserve structured errors from readSysfsFile (e.g., permission denied)
		if _, ok := err.(*errors.Error); ok {
			return nil, err
		}
		return nil, errors.Wrap(
			err,
			errors.ErrCodeSystemError,
			fmt.Sprintf("Failed to read vendor ID for %s", pciAddr),
			"Cannot read PCI device information from sysfs",
			"Check system permissions and sysfs availability",
		)
	}
	device.VendorID = strings.TrimSpace(vendorID)

	// Read device ID
	deviceID, err := readSysfsFile(filepath.Join(sysfsPath, "device"))
	if err != nil {
		// Preserve structured errors from readSysfsFile (e.g., permission denied)
		if _, ok := err.(*errors.Error); ok {
			return nil, err
		}
		return nil, errors.Wrap(
			err,
			errors.ErrCodeSystemError,
			fmt.Sprintf("Failed to read device ID for %s", pciAddr),
			"Cannot read PCI device information from sysfs",
			"Check system permissions and sysfs availability",
		)
	}
	device.DeviceID = strings.TrimSpace(deviceID)

	// Read current driver (optional - may not be bound)
	driverPath := filepath.Join(sysfsPath, "driver")
	if _, err := os.Stat(driverPath); err == nil {
		// Driver is bound - read the driver name
		driverLink, err := os.Readlink(driverPath)
		if err == nil {
			device.Driver = filepath.Base(driverLink)
		}
	}

	if log != nil {
		log.Debug("PCI device verified",
			slog.String("pci_address", pciAddr),
			slog.String("vendor_id", device.VendorID),
			slog.String("device_id", device.DeviceID),
			slog.String("driver", device.Driver),
		)
	}

	return device, nil
}

// VerifyAllPCIDevices verifies all PCI devices in hardware configuration
func VerifyAllPCIDevices(config *HardwareConfig, log *logger.Logger) (map[string]*PCIDevice, error) {
	if config == nil {
		return nil, fmt.Errorf("hardware configuration is nil")
	}

	devices := make(map[string]*PCIDevice)

	for _, iface := range config.Interfaces {
		device, err := VerifyPCIDevice(iface.PCI, log)
		if err != nil {
			// Preserve structured errors by re-wrapping with context
			if arcaErr, ok := err.(*errors.Error); ok {
				return nil, errors.Wrap(
					arcaErr,
					arcaErr.Code,
					fmt.Sprintf("Interface %s: %s", iface.Name, arcaErr.Message),
					arcaErr.Cause,
					arcaErr.Action,
				)
			}
			return nil, fmt.Errorf("interface %s: %w", iface.Name, err)
		}
		devices[iface.PCI] = device
	}

	return devices, nil
}

// GetVendorName returns human-readable vendor name
func GetVendorName(vendorID string) string {
	// Common vendor IDs (can be expanded)
	vendors := map[string]string{
		"0x8086": "Intel Corporation",
		"0x15b3": "Mellanox Technologies",
		"0x14e4": "Broadcom",
		"0x1924": "Solarflare Communications",
	}

	if name, ok := vendors[vendorID]; ok {
		return name
	}
	return "Unknown Vendor"
}

// readSysfsFile reads a single-line file from sysfs
func readSysfsFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsPermission(err) {
			return "", errors.New(
				errors.ErrCodePermissionDenied,
				fmt.Sprintf("Permission denied reading: %s", path),
				"Insufficient permissions to access sysfs",
				"Run with appropriate permissions (e.g., sudo) or check file permissions",
			)
		}
		return "", err
	}
	return string(data), nil
}
