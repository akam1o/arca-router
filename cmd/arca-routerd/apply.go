package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"

	"github.com/akam1o/arca-router/pkg/config"
	"github.com/akam1o/arca-router/pkg/device"
	"github.com/akam1o/arca-router/pkg/errors"
	"github.com/akam1o/arca-router/pkg/logger"
	"github.com/akam1o/arca-router/pkg/vpp"
)

// configLoader abstracts configuration file loading for testing
type configLoader interface {
	LoadHardware(path string, log *logger.Logger) (*device.HardwareConfig, error)
	LoadConfig(path string) (*config.Config, error)
}

// defaultConfigLoader implements configLoader using actual file I/O
type defaultConfigLoader struct{}

func (d *defaultConfigLoader) LoadHardware(path string, log *logger.Logger) (*device.HardwareConfig, error) {
	return device.LoadHardware(path, log)
}

func (d *defaultConfigLoader) LoadConfig(path string) (*config.Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.ConfigNotFound(path)
	}
	defer file.Close()

	return parseConfig(file, path)
}

func parseConfig(r io.Reader, path string) (*config.Config, error) {
	parser := config.NewParser(r)
	cfg, err := parser.Parse()
	if err != nil {
		return nil, errors.ConfigParseError(path, err)
	}
	return cfg, nil
}

// applyResult tracks the results of configuration application
type applyResult struct {
	TotalInterfaces   int
	CreatedInterfaces int
	FailedInterfaces  []string
	TotalAddresses    int
	AppliedAddresses  int
	FailedAddresses   []failedAddress
}

type failedAddress struct {
	Interface string
	Unit      int
	Address   string
	Error     string
}

func (r *applyResult) HasErrors() bool {
	return len(r.FailedInterfaces) > 0 || len(r.FailedAddresses) > 0
}

func (r *applyResult) ErrorCount() int {
	return len(r.FailedInterfaces) + len(r.FailedAddresses)
}

func (r *applyResult) String() string {
	return fmt.Sprintf("Interfaces: %d/%d created, Addresses: %d/%d applied, Errors: %d",
		r.CreatedInterfaces, r.TotalInterfaces,
		r.AppliedAddresses, r.TotalAddresses,
		r.ErrorCount())
}

// applyConfig loads hardware and configuration, connects to VPP, and applies settings.
// It implements partial application: if some operations fail, it logs errors but continues
// with remaining operations where possible.
func applyConfig(ctx context.Context, f *flags, log *logger.Logger) error {
	factory := newVPPClientFactory(f.mockVPP)
	loader := &defaultConfigLoader{}
	return applyConfigWithDeps(ctx, f, log, factory, loader)
}

// applyConfigWithDeps is the testable version that accepts dependencies
func applyConfigWithDeps(
	ctx context.Context,
	f *flags,
	log *logger.Logger,
	factory vppClientFactory,
	loader configLoader,
) error {
	// Step 1: Load hardware.yaml
	log.Info("Loading hardware configuration", slog.String("path", f.hardwarePath))
	hwConfig, err := loader.LoadHardware(f.hardwarePath, log)
	if err != nil {
		log.ErrorWithCause("Failed to load hardware configuration", err,
			"hardware.yaml could not be loaded or parsed",
			fmt.Sprintf("Check file exists and is valid YAML: %s", f.hardwarePath))
		return err
	}
	log.Info("Hardware configuration loaded successfully",
		slog.Int("interface_count", len(hwConfig.Interfaces)))

	// Step 2: Verify PCI devices
	log.Info("Verifying PCI devices")
	pciDevices, err := device.VerifyAllPCIDevices(hwConfig, log)
	if err != nil {
		log.ErrorWithCause("PCI device verification failed", err,
			"One or more PCI devices could not be verified",
			"Ensure PCI addresses in hardware.yaml are correct and devices are present")
		return err
	}
	log.Info("All PCI devices verified", slog.Int("device_count", len(pciDevices)))

	// Step 3: Parse arca.conf
	log.Info("Parsing configuration", slog.String("path", f.configPath))
	cfg, err := loader.LoadConfig(f.configPath)
	if err != nil {
		return err
	}
	log.Info("Configuration parsed successfully")

	// Step 4: Validate configuration
	log.Info("Validating configuration")
	if err := cfg.Validate(); err != nil {
		log.ErrorWithCause("Configuration validation failed", err,
			"Configuration contains invalid values",
			"Review arca.conf for errors")
		return errors.Wrap(err, errors.ErrCodeConfigValidation,
			"Configuration validation failed",
			"Configuration contains invalid values",
			"Review arca.conf for errors")
	}
	log.Info("Configuration validated successfully")

	// Step 5: Connect to VPP
	log.Info("Connecting to VPP", slog.Bool("mock", f.mockVPP))
	vppClient := factory()
	if err := vppClient.Connect(ctx); err != nil {
		return errors.VPPConnectionError(err)
	}
	defer func() {
		if closeErr := vppClient.Close(); closeErr != nil {
			log.Error("Failed to close VPP connection", slog.Any("error", closeErr))
		}
	}()
	log.Info("Connected to VPP")

	// Step 6: Apply configuration
	log.Info("Applying configuration to VPP")
	result := applyVPPConfig(ctx, hwConfig, cfg, vppClient, log)

	// Report detailed results
	log.Info("Configuration application completed", slog.String("result", result.String()))

	if len(result.FailedInterfaces) > 0 {
		log.Warn("Failed to create interfaces", slog.Any("interfaces", result.FailedInterfaces))
	}

	if len(result.FailedAddresses) > 0 {
		for _, fa := range result.FailedAddresses {
			log.Warn("Failed to apply address",
				slog.String("interface", fa.Interface),
				slog.Int("unit", fa.Unit),
				slog.String("address", fa.Address),
				slog.String("error", fa.Error))
		}
	}

	if result.HasErrors() {
		return fmt.Errorf("configuration applied with %d errors: %s", result.ErrorCount(), result.String())
	}

	log.Info("Configuration applied successfully")
	return nil
}

// applyVPPConfig creates interfaces and applies IP addresses based on hardware and configuration.
// Returns detailed results including any failures.
func applyVPPConfig(
	ctx context.Context,
	hwConfig *device.HardwareConfig,
	cfg *config.Config,
	vppClient vpp.Client,
	log *logger.Logger,
) *applyResult {
	result := &applyResult{
		TotalInterfaces:  len(hwConfig.Interfaces),
		FailedInterfaces: make([]string, 0),
		FailedAddresses:  make([]failedAddress, 0),
	}

	// Track created interfaces: interface name -> sw_if_index
	createdInterfaces := make(map[string]uint32)

	// Step 6.1: Create VPP interfaces from hardware.yaml
	for _, iface := range hwConfig.Interfaces {
		ifaceLog := log.WithField("interface", iface.Name)
		ifaceLog.Info("Creating VPP interface",
			slog.String("pci", iface.PCI),
			slog.String("driver", iface.Driver))

		// Determine interface type from driver
		var ifaceType vpp.InterfaceType
		switch iface.Driver {
		case "avf":
			ifaceType = vpp.InterfaceTypeAVF
		case "rdma":
			ifaceType = vpp.InterfaceTypeRDMA
		default:
			ifaceLog.Error("Unsupported driver type",
				slog.String("driver", iface.Driver))
			result.FailedInterfaces = append(result.FailedInterfaces,
				fmt.Sprintf("%s (unsupported driver: %s)", iface.Name, iface.Driver))
			continue
		}

		// Create interface
		req := &vpp.CreateInterfaceRequest{
			Type:           ifaceType,
			DeviceInstance: iface.PCI,
			Name:           iface.Name,
			NumRxQueues:    1,
			NumTxQueues:    1,
		}

		vppIface, err := vppClient.CreateInterface(ctx, req)
		if err != nil {
			ifaceLog.Error("Failed to create VPP interface", slog.Any("error", err))
			result.FailedInterfaces = append(result.FailedInterfaces,
				fmt.Sprintf("%s (create failed: %v)", iface.Name, err))
			continue
		}

		createdInterfaces[iface.Name] = vppIface.SwIfIndex
		result.CreatedInterfaces++
		ifaceLog.Info("VPP interface created",
			slog.Uint64("sw_if_index", uint64(vppIface.SwIfIndex)))

		// Set interface administratively up
		if err := vppClient.SetInterfaceUp(ctx, vppIface.SwIfIndex); err != nil {
			ifaceLog.Error("Failed to set interface up", slog.Any("error", err))
			result.FailedInterfaces = append(result.FailedInterfaces,
				fmt.Sprintf("%s (set up failed: %v)", iface.Name, err))
		} else {
			ifaceLog.Info("Interface set administratively up")
		}
	}

	// Step 6.2: Apply IP addresses from arca.conf
	for ifaceName, ifaceCfg := range cfg.Interfaces {
		ifaceLog := log.WithField("interface", ifaceName)

		// Check if interface was created
		swIfIndex, exists := createdInterfaces[ifaceName]
		if !exists {
			// Determine the reason for missing interface
			var errorReason string
			foundInHardware := false
			for _, hwIface := range hwConfig.Interfaces {
				if hwIface.Name == ifaceName {
					foundInHardware = true
					break
				}
			}

			if foundInHardware {
				errorReason = "interface creation failed (see earlier errors)"
			} else {
				errorReason = "interface not found in hardware configuration"
			}

			ifaceLog.Warn("Skipping IP address assignment", slog.String("reason", errorReason))

			// Count addresses that couldn't be applied
			for unitNum, unit := range ifaceCfg.Units {
				for _, family := range unit.Family {
					result.TotalAddresses += len(family.Addresses)
					for _, addr := range family.Addresses {
						result.FailedAddresses = append(result.FailedAddresses, failedAddress{
							Interface: ifaceName,
							Unit:      unitNum,
							Address:   addr,
							Error:     errorReason,
						})
					}
				}
			}
			continue
		}

		// Apply IP addresses for each unit and family
		for unitNum, unit := range ifaceCfg.Units {
			unitLog := ifaceLog.WithField("unit", unitNum)

			// Process inet (IPv4) addresses
			if family, exists := unit.Family["inet"]; exists {
				for _, addrStr := range family.Addresses {
					result.TotalAddresses++
					_, ipNet, err := net.ParseCIDR(addrStr)
					if err != nil {
						unitLog.Error("Invalid CIDR address",
							slog.String("address", addrStr),
							slog.Any("error", err))
						result.FailedAddresses = append(result.FailedAddresses, failedAddress{
							Interface: ifaceName,
							Unit:      unitNum,
							Address:   addrStr,
							Error:     fmt.Sprintf("invalid CIDR: %v", err),
						})
						continue
					}

					if err := vppClient.SetInterfaceAddress(ctx, swIfIndex, ipNet); err != nil {
						unitLog.Error("Failed to set IPv4 address",
							slog.String("address", addrStr),
							slog.Any("error", err))
						result.FailedAddresses = append(result.FailedAddresses, failedAddress{
							Interface: ifaceName,
							Unit:      unitNum,
							Address:   addrStr,
							Error:     err.Error(),
						})
					} else {
						unitLog.Info("IPv4 address set", slog.String("address", addrStr))
						result.AppliedAddresses++
					}
				}
			}

			// Process inet6 (IPv6) addresses
			if family, exists := unit.Family["inet6"]; exists {
				for _, addrStr := range family.Addresses {
					result.TotalAddresses++
					_, ipNet, err := net.ParseCIDR(addrStr)
					if err != nil {
						unitLog.Error("Invalid CIDR address",
							slog.String("address", addrStr),
							slog.Any("error", err))
						result.FailedAddresses = append(result.FailedAddresses, failedAddress{
							Interface: ifaceName,
							Unit:      unitNum,
							Address:   addrStr,
							Error:     fmt.Sprintf("invalid CIDR: %v", err),
						})
						continue
					}

					if err := vppClient.SetInterfaceAddress(ctx, swIfIndex, ipNet); err != nil {
						unitLog.Error("Failed to set IPv6 address",
							slog.String("address", addrStr),
							slog.Any("error", err))
						result.FailedAddresses = append(result.FailedAddresses, failedAddress{
							Interface: ifaceName,
							Unit:      unitNum,
							Address:   addrStr,
							Error:     err.Error(),
						})
					} else {
						unitLog.Info("IPv6 address set", slog.String("address", addrStr))
						result.AppliedAddresses++
					}
				}
			}
		}
	}

	return result
}
