// Package vpp implements the VPP southbound plugin for the config engine.
// It translates ConfigDiff operations into VPP API calls via govpp.
package vpp

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	"github.com/akam1o/arca-router/pkg/device"
	pkgvpp "github.com/akam1o/arca-router/pkg/vpp"
)

// VPPPlugin implements engine.Plugin for VPP dataplane operations.
type VPPPlugin struct {
	mu         sync.RWMutex
	client     pkgvpp.Client
	lcpManager *pkgvpp.LCPStateManager
	hwConfig   *device.HardwareConfig
	log        *slog.Logger

	// ifaceIndex maps Junos interface name → VPP sw_if_index
	ifaceIndex map[string]uint32

	// appliedAddrs tracks addresses applied per interface for rollback
	appliedAddrs map[uint32][]*net.IPNet
}

// NewVPPPlugin creates a new VPP plugin.
func NewVPPPlugin(client pkgvpp.Client, hwConfig *device.HardwareConfig, log *slog.Logger) *VPPPlugin {
	return &VPPPlugin{
		client:       client,
		lcpManager:   pkgvpp.NewLCPStateManager(client),
		hwConfig:     hwConfig,
		log:          log.With("plugin", "vpp"),
		ifaceIndex:   make(map[string]uint32),
		appliedAddrs: make(map[uint32][]*net.IPNet),
	}
}

func (p *VPPPlugin) Name() string { return "vpp" }

func (p *VPPPlugin) Init(ctx context.Context) error {
	if err := p.client.Connect(ctx); err != nil {
		return fmt.Errorf("vpp connect: %w", err)
	}

	// Sync LCP state from VPP
	if err := p.lcpManager.Sync(ctx); err != nil {
		p.log.Warn("LCP state sync failed, continuing", slog.Any("error", err))
	}

	// Build interface index from existing VPP interfaces
	existing, err := p.client.ListInterfaces(ctx)
	if err != nil {
		p.log.Warn("Failed to list existing interfaces", slog.Any("error", err))
	} else {
		for _, iface := range existing {
			if iface.PCIAddress != "" {
				// Map PCI back to Junos name via hardware config
				for _, hw := range p.hwConfig.Interfaces {
					if hw.PCI == iface.PCIAddress {
						p.ifaceIndex[hw.Name] = iface.SwIfIndex
						break
					}
				}
			}
		}
	}

	return nil
}

func (p *VPPPlugin) Close() error {
	return p.client.Close()
}

func (p *VPPPlugin) HealthCheck(ctx context.Context) error {
	_, err := p.client.GetVersion(ctx)
	return err
}

// ValidateChanges checks if the proposed interface changes are feasible.
func (p *VPPPlugin) ValidateChanges(ctx context.Context, diff *engine.ConfigDiff) error {
	// Validate added interfaces exist in hardware config
	for name := range diff.InterfacesAdded {
		if !p.hasHardwareConfig(name) {
			return fmt.Errorf("interface %s: not found in hardware configuration", name)
		}
	}

	// Validate addresses on changed interfaces
	for _, change := range diff.InterfacesChanged {
		for _, addr := range change.AddressesAdded {
			if _, _, err := net.ParseCIDR(addr.Address); err != nil {
				return fmt.Errorf("interface %s: invalid address %s: %w", change.Name, addr.Address, err)
			}
		}
	}

	return nil
}

// ApplyChanges applies interface, LCP, and address changes to VPP.
func (p *VPPPlugin) ApplyChanges(ctx context.Context, diff *engine.ConfigDiff) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Track changes for potential rollback
	var rollbackOps []func(context.Context) error

	// 1. Create new interfaces
	for name, ifaceCfg := range diff.InterfacesAdded {
		if err := p.createInterface(ctx, name, ifaceCfg, &rollbackOps); err != nil {
			p.executeRollback(ctx, rollbackOps)
			return fmt.Errorf("create interface %s: %w", name, err)
		}
	}

	// 2. Apply address changes on existing interfaces
	for _, change := range diff.InterfacesChanged {
		if err := p.applyInterfaceChanges(ctx, change, &rollbackOps); err != nil {
			p.executeRollback(ctx, rollbackOps)
			return fmt.Errorf("update interface %s: %w", change.Name, err)
		}
	}

	// 3. Remove interfaces (remove addresses, LCP, then disable)
	for _, name := range diff.InterfacesRemoved {
		if err := p.removeInterface(ctx, name, &rollbackOps); err != nil {
			p.log.Warn("Failed to remove interface (non-fatal)", slog.String("interface", name), slog.Any("error", err))
		}
	}

	return nil
}

// RollbackChanges undoes previously applied VPP changes.
func (p *VPPPlugin) RollbackChanges(ctx context.Context, diff *engine.ConfigDiff) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Reverse of ApplyChanges: remove added addresses, re-add removed addresses
	for _, change := range diff.InterfacesChanged {
		swIfIndex, ok := p.ifaceIndex[change.Name]
		if !ok {
			continue
		}
		// Remove addresses that were added
		for _, addr := range change.AddressesAdded {
			_, ipNet, err := net.ParseCIDR(addr.Address)
			if err != nil {
				continue
			}
			_ = p.client.DeleteInterfaceAddress(ctx, swIfIndex, ipNet)
		}
		// Re-add addresses that were removed
		for _, addr := range change.AddressesRemoved {
			_, ipNet, err := net.ParseCIDR(addr.Address)
			if err != nil {
				continue
			}
			_ = p.client.SetInterfaceAddress(ctx, swIfIndex, ipNet)
		}
	}

	return nil
}

// CollectState gathers live interface state from VPP.
func (p *VPPPlugin) CollectState(ctx context.Context) (map[string]*model.InterfaceState, error) {
	interfaces, err := p.client.ListInterfaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("list interfaces: %w", err)
	}

	result := make(map[string]*model.InterfaceState)
	for _, iface := range interfaces {
		// Find the Junos name for this interface
		junosName := p.findJunosName(iface.SwIfIndex)
		if junosName == "" {
			continue // Skip unmanaged interfaces
		}

		state := &model.InterfaceState{
			Name: junosName,
			MAC:  iface.MAC.String(),
		}
		if iface.AdminUp {
			state.AdminStatus = "up"
		} else {
			state.AdminStatus = "down"
		}
		if iface.LinkUp {
			state.OperStatus = "up"
		} else {
			state.OperStatus = "down"
		}
		result[junosName] = state
	}

	return result, nil
}

// --- Internal helpers ---

func (p *VPPPlugin) hasHardwareConfig(name string) bool {
	for _, hw := range p.hwConfig.Interfaces {
		if hw.Name == name {
			return true
		}
	}
	return false
}

func (p *VPPPlugin) getHardwareConfig(name string) *device.PhysicalInterface {
	for i := range p.hwConfig.Interfaces {
		if p.hwConfig.Interfaces[i].Name == name {
			return &p.hwConfig.Interfaces[i]
		}
	}
	return nil
}

func (p *VPPPlugin) createInterface(ctx context.Context, name string, ifaceCfg *model.InterfaceConfig, rollback *[]func(context.Context) error) error {
	hw := p.getHardwareConfig(name)
	if hw == nil {
		return fmt.Errorf("no hardware config for %s", name)
	}

	// Determine interface type
	var ifaceType pkgvpp.InterfaceType
	var deviceInstance string
	switch hw.Driver {
	case "avf":
		ifaceType = pkgvpp.InterfaceTypeAVF
		deviceInstance = hw.PCI
	case "rdma":
		ifaceType = pkgvpp.InterfaceTypeRDMA
		linuxIfName, err := pkgvpp.GetLinuxIfNameFromPCI(hw.PCI)
		if err != nil {
			return fmt.Errorf("PCI resolve for RDMA: %w", err)
		}
		deviceInstance = linuxIfName
	default:
		return fmt.Errorf("unsupported driver: %s", hw.Driver)
	}

	// Create VPP interface
	vppIface, err := p.client.CreateInterface(ctx, &pkgvpp.CreateInterfaceRequest{
		Type:           ifaceType,
		DeviceInstance: deviceInstance,
		PCIAddress:     hw.PCI,
		Name:           name,
		NumRxQueues:    1,
		NumTxQueues:    1,
	})
	if err != nil {
		return err
	}

	p.ifaceIndex[name] = vppIface.SwIfIndex

	// Set interface up
	if err := p.client.SetInterfaceUp(ctx, vppIface.SwIfIndex); err != nil {
		return fmt.Errorf("set up: %w", err)
	}

	// Create LCP pair
	linuxName, err := pkgvpp.ConvertJunosToLinuxName(name)
	if err != nil {
		p.log.Warn("LCP name conversion failed", slog.String("interface", name), slog.Any("error", err))
	} else {
		if err := p.lcpManager.Create(ctx, vppIface.SwIfIndex, linuxName, name); err != nil {
			p.log.Warn("LCP creation failed", slog.String("interface", name), slog.Any("error", err))
		}
	}

	// Apply addresses
	if ifaceCfg != nil {
		if err := p.applyAddresses(ctx, vppIface.SwIfIndex, ifaceCfg); err != nil {
			return err
		}
	}

	return nil
}

func (p *VPPPlugin) applyInterfaceChanges(ctx context.Context, change *engine.InterfaceChange, rollback *[]func(context.Context) error) error {
	swIfIndex, ok := p.ifaceIndex[change.Name]
	if !ok {
		return fmt.Errorf("interface %s not found in VPP", change.Name)
	}

	// Remove old addresses
	for _, addr := range change.AddressesRemoved {
		_, ipNet, err := net.ParseCIDR(addr.Address)
		if err != nil {
			continue
		}
		if err := p.client.DeleteInterfaceAddress(ctx, swIfIndex, ipNet); err != nil {
			p.log.Warn("Failed to remove address", slog.String("interface", change.Name), slog.String("address", addr.Address), slog.Any("error", err))
		}
	}

	// Add new addresses
	for _, addr := range change.AddressesAdded {
		_, ipNet, err := net.ParseCIDR(addr.Address)
		if err != nil {
			return fmt.Errorf("parse CIDR %s: %w", addr.Address, err)
		}
		if err := p.client.SetInterfaceAddress(ctx, swIfIndex, ipNet); err != nil {
			return fmt.Errorf("set address %s: %w", addr.Address, err)
		}
	}

	return nil
}

func (p *VPPPlugin) removeInterface(ctx context.Context, name string, rollback *[]func(context.Context) error) error {
	swIfIndex, ok := p.ifaceIndex[name]
	if !ok {
		return nil // Already gone
	}

	// Set interface down
	if err := p.client.SetInterfaceDown(ctx, swIfIndex); err != nil {
		return fmt.Errorf("set down: %w", err)
	}

	// Remove LCP pair
	_ = p.client.DeleteLCPInterface(ctx, swIfIndex)

	delete(p.ifaceIndex, name)
	return nil
}

func (p *VPPPlugin) applyAddresses(ctx context.Context, swIfIndex uint32, ifaceCfg *model.InterfaceConfig) error {
	for _, unit := range ifaceCfg.Units {
		for _, family := range unit.Family {
			for _, addrStr := range family.Addresses {
				_, ipNet, err := net.ParseCIDR(addrStr)
				if err != nil {
					return fmt.Errorf("parse CIDR %s: %w", addrStr, err)
				}
				if err := p.client.SetInterfaceAddress(ctx, swIfIndex, ipNet); err != nil {
					return fmt.Errorf("set address %s: %w", addrStr, err)
				}
			}
		}
	}
	return nil
}

func (p *VPPPlugin) findJunosName(swIfIndex uint32) string {
	for name, idx := range p.ifaceIndex {
		if idx == swIfIndex {
			return name
		}
	}
	return ""
}

func (p *VPPPlugin) executeRollback(ctx context.Context, ops []func(context.Context) error) {
	for i := len(ops) - 1; i >= 0; i-- {
		if err := ops[i](ctx); err != nil {
			p.log.Error("Rollback operation failed", slog.Any("error", err))
		}
	}
}

// GetInterfaceIndex returns the VPP sw_if_index for a Junos interface name.
func (p *VPPPlugin) GetInterfaceIndex(name string) (uint32, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	idx, ok := p.ifaceIndex[name]
	return idx, ok
}
