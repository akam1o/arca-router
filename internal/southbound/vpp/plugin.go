// Package vpp implements the VPP southbound plugin for the config engine.
// It translates ConfigDiff operations into VPP API calls via govpp.
package vpp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	"github.com/akam1o/arca-router/pkg/device"
	pkgvpp "github.com/akam1o/arca-router/pkg/vpp"
)

const (
	vppQoSCapabilityErrorMessage     = "failed to detect VPP QoS capabilities"
	vppLCPReconciliationErrorMessage = "failed to check VPP LCP reconciliation"
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

	// vxlanIfIndex maps EVPN VNI → VPP VXLAN tunnel sw_if_index
	vxlanIfIndex map[int]uint32

	// appliedAddrs tracks addresses applied per interface for rollback
	appliedAddrs map[uint32][]*net.IPNet

	// removedInterfaces tracks interfaces disabled during the last apply.
	removedInterfaces map[string]uint32

	lcpReconciliation LCPReconciliationStatus
	qosCapabilities   QoSCapabilityStatus
}

// LCPReconciliationStatus is the latest VPP LCP cache reconciliation result.
type LCPReconciliationStatus struct {
	LastRun         time.Time
	PairCount       int
	Inconsistencies []string
	LastError       string
}

// QoSCapabilityStatus is the latest VPP class-of-service capability result.
type QoSCapabilityStatus struct {
	LastCheck    time.Time
	Capabilities pkgvpp.QoSCapabilities
	LastError    string
}

// NewVPPPlugin creates a new VPP plugin.
func NewVPPPlugin(client pkgvpp.Client, hwConfig *device.HardwareConfig, log *slog.Logger) *VPPPlugin {
	if hwConfig == nil {
		hwConfig = &device.HardwareConfig{}
	}
	if log == nil {
		log = slog.Default()
	}

	return &VPPPlugin{
		client:            client,
		lcpManager:        pkgvpp.NewLCPStateManager(client),
		hwConfig:          hwConfig,
		log:               log.With("plugin", "vpp"),
		ifaceIndex:        make(map[string]uint32),
		vxlanIfIndex:      make(map[int]uint32),
		appliedAddrs:      make(map[uint32][]*net.IPNet),
		removedInterfaces: make(map[string]uint32),
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
	p.updateQoSCapabilities(ctx)

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

	p.updateLCPReconciliation(ctx)

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
	if diff == nil {
		return nil
	}
	if err := validateEVPNChanges(diff); err != nil {
		return err
	}
	// Validate added interfaces exist in hardware config
	for name := range diff.InterfacesAdded {
		if !p.hasHardwareConfig(name) {
			return fmt.Errorf("interface %s: not found in hardware configuration", name)
		}
	}

	if diff.RoutingInstancesChanged {
		if _, err := routingInstancePlanMap(diff.NewRoutingInstances); err != nil {
			return err
		}
	}
	if diff.ClassOfServiceChanged {
		if _, err := classOfServiceBindingMap(diff.NewClassOfService); err != nil {
			return err
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
	p.removedInterfaces = make(map[string]uint32)

	// 1. Create new interfaces
	for name, ifaceCfg := range diff.InterfacesAdded {
		if err := p.createInterface(ctx, name, ifaceCfg, &rollbackOps); err != nil {
			return withRollbackError(
				fmt.Errorf("create interface %s: %w", name, err),
				p.executeRollback(ctx, rollbackOps),
			)
		}
	}

	tableAddressHandled := make(map[string]bool)
	if diff.RoutingInstancesChanged {
		var err error
		tableAddressHandled, err = p.applyRoutingInstanceChanges(ctx, diff, &rollbackOps, false)
		if err != nil {
			return withRollbackError(
				fmt.Errorf("update routing instance tables: %w", err),
				p.executeRollback(ctx, rollbackOps),
			)
		}
	}

	for name, ifaceCfg := range diff.InterfacesAdded {
		if tableAddressHandled[name] {
			continue
		}
		swIfIndex, ok := p.ifaceIndex[name]
		if !ok {
			return withRollbackError(
				fmt.Errorf("interface %s not found in VPP", name),
				p.executeRollback(ctx, rollbackOps),
			)
		}
		if err := p.applyAddresses(ctx, swIfIndex, ifaceCfg, &rollbackOps); err != nil {
			return withRollbackError(
				fmt.Errorf("apply interface %s addresses: %w", name, err),
				p.executeRollback(ctx, rollbackOps),
			)
		}
	}

	// 2. Apply address changes on existing interfaces
	for _, change := range diff.InterfacesChanged {
		if tableAddressHandled[change.Name] {
			continue
		}
		if err := p.applyInterfaceChanges(ctx, change, &rollbackOps); err != nil {
			return withRollbackError(
				fmt.Errorf("update interface %s: %w", change.Name, err),
				p.executeRollback(ctx, rollbackOps),
			)
		}
	}

	// 3. Apply MPLS forwarding state before interfaces are removed.
	if diff.MPLSChanged {
		if err := p.applyMPLSChanges(ctx, diff.OldMPLS, diff.NewMPLS, &rollbackOps); err != nil {
			return withRollbackError(
				fmt.Errorf("update MPLS interfaces: %w", err),
				p.executeRollback(ctx, rollbackOps),
			)
		}
	}

	// 4. Apply class-of-service profile bindings before interfaces are removed.
	if diff.ClassOfServiceChanged {
		if err := p.applyClassOfServiceChanges(ctx, diff.OldClassOfService, diff.NewClassOfService, &rollbackOps); err != nil {
			return withRollbackError(
				fmt.Errorf("update class-of-service interfaces: %w", err),
				p.executeRollback(ctx, rollbackOps),
			)
		}
	}

	// 5. Apply EVPN/VXLAN overlay state before interfaces are removed.
	if diff.EVPNChanged {
		if err := p.applyEVPNChanges(ctx, diff, &rollbackOps); err != nil {
			return withRollbackError(
				fmt.Errorf("update EVPN/VXLAN dataplane: %w", err),
				p.executeRollback(ctx, rollbackOps),
			)
		}
	}

	if diff.RoutingInstancesChanged {
		if err := p.deleteStaleRoutingInstanceTables(ctx, diff, &rollbackOps); err != nil {
			return withRollbackError(
				fmt.Errorf("delete routing instance tables: %w", err),
				p.executeRollback(ctx, rollbackOps),
			)
		}
	}

	// 6. Remove interfaces (remove addresses, LCP, then disable)
	for _, name := range diff.InterfacesRemoved {
		if err := p.removeInterface(ctx, name, &rollbackOps); err != nil {
			return withRollbackError(
				fmt.Errorf("remove interface %s: %w", name, err),
				p.executeRollback(ctx, rollbackOps),
			)
		}
	}

	p.updateLCPReconciliationLocked(ctx)
	return nil
}

// RollbackChanges undoes previously applied VPP changes.
func (p *VPPPlugin) RollbackChanges(ctx context.Context, diff *engine.ConfigDiff) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var rollbackErr error
	if diff.MPLSChanged {
		for _, name := range mplsAddedInterfaces(diff.OldMPLS, diff.NewMPLS) {
			if err := p.setMPLSInterface(ctx, name, false); err != nil {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("disable MPLS interface %s: %w", name, err))
			}
		}
	}

	for _, name := range diff.InterfacesRemoved {
		swIfIndex, ok := p.removedInterfaces[name]
		if !ok {
			continue
		}
		p.ifaceIndex[name] = swIfIndex
		if err := p.client.SetInterfaceUp(ctx, swIfIndex); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore interface %s up: %w", name, err))
		}
		if linuxName, err := pkgvpp.ConvertJunosToLinuxName(name); err == nil {
			if err := p.lcpManager.Create(ctx, swIfIndex, linuxName, name); err != nil {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore LCP interface %s: %w", name, err))
			}
		}
	}

	if diff.ClassOfServiceChanged {
		if err := p.applyClassOfServiceChanges(ctx, diff.NewClassOfService, diff.OldClassOfService, nil); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore class-of-service interfaces: %w", err))
		}
	}

	if diff.EVPNChanged {
		if err := p.applyEVPNChanges(ctx, reverseEVPNDiff(diff), nil); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore EVPN/VXLAN dataplane: %w", err))
		}
	}

	// Reverse of ApplyChanges: remove added addresses, re-add removed addresses.
	for name, ifaceCfg := range diff.InterfacesAdded {
		swIfIndex, ok := p.ifaceIndex[name]
		if !ok {
			continue
		}
		if err := p.deleteConfiguredAddresses(ctx, swIfIndex, ifaceCfg); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("remove configured addresses for interface %s: %w", name, err))
		}
		if err := p.client.DeleteLCPInterface(ctx, swIfIndex); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("delete LCP interface %s: %w", name, err))
		}
		if err := p.client.SetInterfaceDown(ctx, swIfIndex); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("set interface %s down: %w", name, err))
		}
		delete(p.ifaceIndex, name)
	}

	tableAddressHandled := make(map[string]bool)
	if diff.RoutingInstancesChanged {
		var err error
		tableAddressHandled, err = p.applyRoutingInstanceChanges(ctx, &engine.ConfigDiff{
			OldConfig:           diff.NewConfig,
			NewConfig:           diff.OldConfig,
			OldRoutingInstances: diff.NewRoutingInstances,
			NewRoutingInstances: diff.OldRoutingInstances,
		}, nil, true)
		if err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore routing instance tables: %w", err))
		}
	}

	for _, change := range diff.InterfacesChanged {
		if tableAddressHandled[change.Name] {
			continue
		}
		swIfIndex, ok := p.ifaceIndex[change.Name]
		if !ok {
			continue
		}
		// Remove addresses that were added
		for _, addr := range change.AddressesAdded {
			ipNet, err := pkgvpp.ParseCIDRAddress(addr.Address)
			if err != nil {
				continue
			}
			if err := p.client.DeleteInterfaceAddress(ctx, swIfIndex, ipNet); err != nil {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("remove added address %s from interface %s: %w", addr.Address, change.Name, err))
			}
		}
		// Re-add addresses that were removed
		for _, addr := range change.AddressesRemoved {
			ipNet, err := pkgvpp.ParseCIDRAddress(addr.Address)
			if err != nil {
				continue
			}
			if err := p.client.SetInterfaceAddress(ctx, swIfIndex, ipNet); err != nil {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore removed address %s on interface %s: %w", addr.Address, change.Name, err))
			}
		}
	}

	if diff.MPLSChanged {
		for _, name := range mplsRemovedInterfaces(diff.OldMPLS, diff.NewMPLS) {
			if err := p.setMPLSInterface(ctx, name, true); err != nil {
				rollbackErr = errors.Join(rollbackErr, fmt.Errorf("enable MPLS interface %s: %w", name, err))
			}
		}
	}

	p.updateLCPReconciliationLocked(ctx)
	return rollbackErr
}

// CollectState gathers live interface state from VPP.
func (p *VPPPlugin) CollectState(ctx context.Context) (map[string]*model.InterfaceState, error) {
	interfaces, err := p.client.ListInterfaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("list interfaces: %w", err)
	}
	countersByIndex, err := p.client.ListInterfaceCounters(ctx)
	if err != nil {
		p.log.Warn("Failed to list VPP interface counters", slog.Any("error", err))
	}
	queuesByIndex, err := p.client.ListInterfaceQueuePlacements(ctx)
	if err != nil {
		p.log.Warn("Failed to list VPP interface queue placements", slog.Any("error", err))
	}

	result := make(map[string]*model.InterfaceState)
	for _, iface := range interfaces {
		// Find the Junos name for this interface
		junosName := p.findJunosName(iface.SwIfIndex)
		if junosName == "" {
			continue // Skip unmanaged interfaces
		}

		state := &model.InterfaceState{
			Name:       junosName,
			MAC:        iface.MAC.String(),
			QoSProfile: iface.QoSProfile,
		}
		if tableID, err := p.client.GetInterfaceTable(ctx, iface.SwIfIndex, false); err != nil {
			p.log.Warn("Failed to get VPP interface IPv4 table", slog.String("interface", junosName), slog.Any("error", err))
		} else {
			state.IPv4TableID = tableID
		}
		if tableID, err := p.client.GetInterfaceTable(ctx, iface.SwIfIndex, true); err != nil {
			p.log.Warn("Failed to get VPP interface IPv6 table", slog.String("interface", junosName), slog.Any("error", err))
		} else {
			state.IPv6TableID = tableID
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
		if counters, ok := countersByIndex[iface.SwIfIndex]; ok {
			state.Counters = &model.InterfaceCounters{
				RxPackets: counters.RxPackets,
				TxPackets: counters.TxPackets,
				RxBytes:   counters.RxBytes,
				TxBytes:   counters.TxBytes,
				RxErrors:  counters.RxErrors,
				TxErrors:  counters.TxErrors,
				Drops:     counters.Drops,
			}
		}
		if queues, ok := queuesByIndex[iface.SwIfIndex]; ok {
			state.Queues = modelInterfaceQueues(queues)
		}
		result[junosName] = state
	}

	return result, nil
}

func modelInterfaceQueues(queues pkgvpp.InterfaceQueuePlacements) *model.InterfaceQueues {
	if len(queues.Rx) == 0 && len(queues.Tx) == 0 {
		return nil
	}
	result := &model.InterfaceQueues{
		Rx: make([]model.InterfaceRxQueue, 0, len(queues.Rx)),
		Tx: make([]model.InterfaceTxQueue, 0, len(queues.Tx)),
	}
	for _, queue := range queues.Rx {
		result.Rx = append(result.Rx, model.InterfaceRxQueue{
			QueueID:  queue.QueueID,
			WorkerID: queue.WorkerID,
			Mode:     queue.Mode,
		})
	}
	for _, queue := range queues.Tx {
		result.Tx = append(result.Tx, model.InterfaceTxQueue{
			QueueID: queue.QueueID,
			Shared:  queue.Shared,
			Threads: append([]uint32(nil), queue.Threads...),
		})
	}
	return result
}

// LCPReconciliationStatus returns a copy of the latest LCP reconciliation result.
func (p *VPPPlugin) LCPReconciliationStatus() LCPReconciliationStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return cloneLCPReconciliationStatus(p.lcpReconciliation)
}

// QoSCapabilityStatus returns a copy of the latest class-of-service capability result.
func (p *VPPPlugin) QoSCapabilityStatus() QoSCapabilityStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return cloneQoSCapabilityStatus(p.qosCapabilities)
}

// --- Internal helpers ---

func (p *VPPPlugin) hasHardwareConfig(name string) bool {
	if p == nil || p.hwConfig == nil {
		return false
	}
	for _, hw := range p.hwConfig.Interfaces {
		if hw.Name == name {
			return true
		}
	}
	return false
}

func (p *VPPPlugin) getHardwareConfig(name string) *device.PhysicalInterface {
	if p == nil || p.hwConfig == nil {
		return nil
	}
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
	*rollback = append(*rollback, func(ctx context.Context) error {
		var rollbackErr error
		if err := p.client.DeleteLCPInterface(ctx, vppIface.SwIfIndex); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("delete LCP interface %s: %w", name, err))
		}
		if err := p.client.SetInterfaceDown(ctx, vppIface.SwIfIndex); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("set interface %s down: %w", name, err))
		}
		delete(p.ifaceIndex, name)
		return rollbackErr
	})

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

	return nil
}

func (p *VPPPlugin) applyInterfaceChanges(ctx context.Context, change *engine.InterfaceChange, rollback *[]func(context.Context) error) error {
	swIfIndex, ok := p.ifaceIndex[change.Name]
	if !ok {
		return fmt.Errorf("interface %s not found in VPP", change.Name)
	}

	// Remove old addresses
	for _, addr := range change.AddressesRemoved {
		ipNet, err := pkgvpp.ParseCIDRAddress(addr.Address)
		if err != nil {
			continue
		}
		if err := p.client.DeleteInterfaceAddress(ctx, swIfIndex, ipNet); err != nil {
			p.log.Warn("Failed to remove address", slog.String("interface", change.Name), slog.String("address", addr.Address), slog.Any("error", err))
		} else {
			addrCopy := cloneIPNet(ipNet)
			*rollback = append(*rollback, func(ctx context.Context) error {
				return p.client.SetInterfaceAddress(ctx, swIfIndex, addrCopy)
			})
		}
	}

	// Add new addresses
	for _, addr := range change.AddressesAdded {
		ipNet, err := pkgvpp.ParseCIDRAddress(addr.Address)
		if err != nil {
			return fmt.Errorf("parse CIDR %s: %w", addr.Address, err)
		}
		if err := p.client.SetInterfaceAddress(ctx, swIfIndex, ipNet); err != nil {
			return fmt.Errorf("set address %s: %w", addr.Address, err)
		}
		addrCopy := cloneIPNet(ipNet)
		*rollback = append(*rollback, func(ctx context.Context) error {
			return p.client.DeleteInterfaceAddress(ctx, swIfIndex, addrCopy)
		})
	}

	return nil
}

func (p *VPPPlugin) removeInterface(ctx context.Context, name string, rollback *[]func(context.Context) error) error {
	swIfIndex, ok := p.ifaceIndex[name]
	if !ok {
		return nil // Already gone
	}
	p.removedInterfaces[name] = swIfIndex

	// Set interface down
	if err := p.client.SetInterfaceDown(ctx, swIfIndex); err != nil {
		return fmt.Errorf("set down: %w", err)
	}
	*rollback = append(*rollback, func(ctx context.Context) error {
		p.ifaceIndex[name] = swIfIndex
		if err := p.client.SetInterfaceUp(ctx, swIfIndex); err != nil {
			return fmt.Errorf("restore interface %s up: %w", name, err)
		}
		if linuxName, err := pkgvpp.ConvertJunosToLinuxName(name); err == nil {
			if err := p.lcpManager.Create(ctx, swIfIndex, linuxName, name); err != nil {
				return fmt.Errorf("restore LCP interface %s: %w", name, err)
			}
		}
		return nil
	})

	// Remove LCP pair if it exists. LCP creation is currently best-effort, so
	// absence must not block deleting the owning interface config.
	if err := p.deleteLCPIfPresent(ctx, swIfIndex); err != nil {
		return fmt.Errorf("delete LCP interface: %w", err)
	}

	delete(p.ifaceIndex, name)
	return nil
}

func (p *VPPPlugin) applyMPLSChanges(ctx context.Context, oldMPLS, newMPLS *model.MPLSConfig, rollback *[]func(context.Context) error) error {
	for _, name := range mplsRemovedInterfaces(oldMPLS, newMPLS) {
		if err := p.setMPLSInterface(ctx, name, false); err != nil {
			return fmt.Errorf("disable %s: %w", name, err)
		}
		if rollback != nil {
			ifName := name
			*rollback = append(*rollback, func(ctx context.Context) error {
				return p.setMPLSInterface(ctx, ifName, true)
			})
		}
	}
	for _, name := range mplsAddedInterfaces(oldMPLS, newMPLS) {
		if err := p.setMPLSInterface(ctx, name, true); err != nil {
			return fmt.Errorf("enable %s: %w", name, err)
		}
		if rollback != nil {
			ifName := name
			*rollback = append(*rollback, func(ctx context.Context) error {
				return p.setMPLSInterface(ctx, ifName, false)
			})
		}
	}
	return nil
}

func (p *VPPPlugin) setMPLSInterface(ctx context.Context, name string, enabled bool) error {
	swIfIndex, ok := p.ifaceIndex[name]
	if !ok {
		return fmt.Errorf("interface %s not found in VPP", name)
	}
	if err := p.client.SetMPLSInterface(ctx, swIfIndex, enabled); err != nil {
		return err
	}
	return nil
}

type classOfServiceBindingChange struct {
	name       string
	oldProfile pkgvpp.QoSProfile
	oldSet     bool
	newProfile pkgvpp.QoSProfile
	newSet     bool
}

func (p *VPPPlugin) applyClassOfServiceChanges(ctx context.Context, oldCoS, newCoS *model.ClassOfServiceConfig, rollback *[]func(context.Context) error) error {
	oldBindings, err := classOfServiceBindingMap(oldCoS)
	if err != nil {
		return fmt.Errorf("old class-of-service: %w", err)
	}
	newBindings, err := classOfServiceBindingMap(newCoS)
	if err != nil {
		return fmt.Errorf("new class-of-service: %w", err)
	}

	for _, change := range classOfServiceBindingChanges(oldBindings, newBindings) {
		swIfIndex, ok := p.ifaceIndex[change.name]
		if !ok {
			if change.newSet {
				return fmt.Errorf("interface %s not found in VPP", change.name)
			}
			continue
		}
		if change.newSet {
			if err := p.client.SetQoSProfile(ctx, swIfIndex, change.newProfile); err != nil {
				return fmt.Errorf("set %s QoS profile %s: %w", change.name, change.newProfile.Name, err)
			}
		} else {
			if err := p.client.ClearQoSProfile(ctx, swIfIndex); err != nil {
				return fmt.Errorf("clear %s QoS profile: %w", change.name, err)
			}
		}
		if rollback != nil {
			swIfIndexCopy := swIfIndex
			changeCopy := change
			*rollback = append(*rollback, func(ctx context.Context) error {
				if changeCopy.oldSet {
					return p.client.SetQoSProfile(ctx, swIfIndexCopy, changeCopy.oldProfile)
				}
				return p.client.ClearQoSProfile(ctx, swIfIndexCopy)
			})
		}
	}
	return nil
}

func classOfServiceBindingMap(cfg *model.ClassOfServiceConfig) (map[string]pkgvpp.QoSProfile, error) {
	bindings := make(map[string]pkgvpp.QoSProfile)
	if cfg == nil {
		return bindings, nil
	}
	for name, fc := range cfg.ForwardingClasses {
		if fc == nil {
			return nil, fmt.Errorf("forwarding-class %s is nil", name)
		}
		if fc.Queue < 0 || fc.Queue > 7 {
			return nil, fmt.Errorf("forwarding-class %s: queue must be 0-7, got %d", name, fc.Queue)
		}
	}

	interfaceNames := make([]string, 0, len(cfg.Interfaces))
	for name := range cfg.Interfaces {
		interfaceNames = append(interfaceNames, name)
	}
	sort.Strings(interfaceNames)

	for _, name := range interfaceNames {
		iface := cfg.Interfaces[name]
		if iface == nil {
			return nil, fmt.Errorf("interface %s is nil", name)
		}
		profileName := iface.OutputTrafficControlProfile
		if profileName == "" {
			continue
		}
		profileCfg := cfg.TrafficControlProfiles[profileName]
		if profileCfg == nil {
			return nil, fmt.Errorf("interface %s: output traffic-control-profile %q not found", name, profileName)
		}
		bindings[name] = classOfServiceQoSProfile(profileName, profileCfg, cfg.ForwardingClasses)
	}
	return bindings, nil
}

func classOfServiceQoSProfile(name string, profile *model.TrafficControlProfile, forwardingClasses map[string]*model.ForwardingClass) pkgvpp.QoSProfile {
	result := pkgvpp.QoSProfile{
		Name:         name,
		ShapingRate:  profile.ShapingRate,
		SchedulerMap: profile.SchedulerMap,
	}

	classNames := make([]string, 0, len(forwardingClasses))
	for className := range forwardingClasses {
		classNames = append(classNames, className)
	}
	sort.Strings(classNames)
	for _, className := range classNames {
		fc := forwardingClasses[className]
		if fc == nil {
			continue
		}
		result.Queues = append(result.Queues, pkgvpp.QoSQueue{
			ForwardingClass: className,
			Queue:           uint8(fc.Queue),
		})
	}
	return result
}

func classOfServiceBindingChanges(oldBindings, newBindings map[string]pkgvpp.QoSProfile) []classOfServiceBindingChange {
	namesSet := make(map[string]bool)
	for name := range oldBindings {
		namesSet[name] = true
	}
	for name := range newBindings {
		namesSet[name] = true
	}
	names := make([]string, 0, len(namesSet))
	for name := range namesSet {
		names = append(names, name)
	}
	sort.Strings(names)

	changes := make([]classOfServiceBindingChange, 0, len(names))
	for _, name := range names {
		oldProfile, oldSet := oldBindings[name]
		newProfile, newSet := newBindings[name]
		if oldSet != newSet || !qosProfileEqual(oldProfile, newProfile) {
			changes = append(changes, classOfServiceBindingChange{
				name:       name,
				oldProfile: oldProfile,
				oldSet:     oldSet,
				newProfile: newProfile,
				newSet:     newSet,
			})
		}
	}
	return changes
}

func qosProfileEqual(a, b pkgvpp.QoSProfile) bool {
	if a.Name != b.Name || a.ShapingRate != b.ShapingRate || a.SchedulerMap != b.SchedulerMap || len(a.Queues) != len(b.Queues) {
		return false
	}
	for i := range a.Queues {
		if a.Queues[i] != b.Queues[i] {
			return false
		}
	}
	return true
}

type routingInstancePlan struct {
	name       string
	tableID    uint32
	interfaces []string
}

func (p *VPPPlugin) applyRoutingInstanceChanges(ctx context.Context, diff *engine.ConfigDiff, rollback *[]func(context.Context) error, deleteStaleTables bool) (map[string]bool, error) {
	oldPlans, err := routingInstancePlanMap(diff.OldRoutingInstances)
	if err != nil {
		return nil, err
	}
	newPlans, err := routingInstancePlanMap(diff.NewRoutingInstances)
	if err != nil {
		return nil, err
	}

	for _, plan := range routingTablesToCreate(oldPlans, newPlans) {
		if err := p.addIPTablePair(ctx, plan); err != nil {
			return nil, fmt.Errorf("create routing-instance %s table %d: %w", plan.name, plan.tableID, err)
		}
		if rollback != nil {
			planCopy := plan
			*rollback = append(*rollback, func(ctx context.Context) error {
				return p.deleteIPTablePair(ctx, planCopy)
			})
		}
	}

	handledAddresses := make(map[string]bool)
	oldBindings := routingInterfaceBindings(oldPlans)
	newBindings := routingInterfaceBindings(newPlans)
	for _, binding := range routingBindingChanges(oldBindings, newBindings) {
		if _, ok := p.ifaceIndex[binding.name]; !ok {
			if binding.newTable == 0 {
				handledAddresses[binding.name] = true
				continue
			}
			return nil, fmt.Errorf("interface %s not found in VPP", binding.name)
		}
		if err := p.removeConfiguredAddresses(ctx, diff.OldConfig, binding.name, rollback); err != nil {
			return nil, fmt.Errorf("remove addresses before binding %s to table %d: %w", binding.name, binding.newTable, err)
		}
		if err := p.setInterfaceTablePair(ctx, binding.name, binding.newTable, binding.oldTable); err != nil {
			return nil, fmt.Errorf("bind interface %s to table %d: %w", binding.name, binding.newTable, err)
		}
		if rollback != nil {
			bindingCopy := binding
			*rollback = append(*rollback, func(ctx context.Context) error {
				return p.setInterfaceTablePair(ctx, bindingCopy.name, bindingCopy.oldTable, bindingCopy.newTable)
			})
		}
		if err := p.addConfiguredAddresses(ctx, diff.NewConfig, binding.name, rollback); err != nil {
			return nil, fmt.Errorf("restore addresses after binding %s to table %d: %w", binding.name, binding.newTable, err)
		}
		handledAddresses[binding.name] = true
	}

	if deleteStaleTables {
		if err := p.deleteStaleRoutingInstanceTables(ctx, diff, rollback); err != nil {
			return nil, err
		}
	}

	return handledAddresses, nil
}

func (p *VPPPlugin) deleteStaleRoutingInstanceTables(ctx context.Context, diff *engine.ConfigDiff, rollback *[]func(context.Context) error) error {
	oldPlans, err := routingInstancePlanMap(diff.OldRoutingInstances)
	if err != nil {
		return err
	}
	newPlans, err := routingInstancePlanMap(diff.NewRoutingInstances)
	if err != nil {
		return err
	}
	for _, plan := range routingTablesToDelete(oldPlans, newPlans) {
		if err := p.deleteIPTablePair(ctx, plan); err != nil {
			return fmt.Errorf("delete routing-instance %s table %d: %w", plan.name, plan.tableID, err)
		}
		if rollback != nil {
			planCopy := plan
			*rollback = append(*rollback, func(ctx context.Context) error {
				return p.addIPTablePair(ctx, planCopy)
			})
		}
	}
	return nil
}

func routingInstancePlanMap(instances map[string]*model.RoutingInstance) (map[string]routingInstancePlan, error) {
	modelPlans, err := model.RoutingInstanceTablePlans(instances)
	if err != nil {
		return nil, err
	}
	result := make(map[string]routingInstancePlan, len(modelPlans))
	for name, plan := range modelPlans {
		result[name] = routingInstancePlan{
			name:       plan.Name,
			tableID:    plan.TableID,
			interfaces: append([]string(nil), plan.Interfaces...),
		}
	}
	return result, nil
}

func routingInterfaceBindings(plans map[string]routingInstancePlan) map[string]uint32 {
	bindings := make(map[string]uint32)
	for _, plan := range plans {
		for _, name := range plan.interfaces {
			bindings[name] = plan.tableID
		}
	}
	return bindings
}

type routingBindingChange struct {
	name     string
	oldTable uint32
	newTable uint32
}

func routingBindingChanges(oldBindings, newBindings map[string]uint32) []routingBindingChange {
	namesSet := make(map[string]bool)
	for name := range oldBindings {
		namesSet[name] = true
	}
	for name := range newBindings {
		namesSet[name] = true
	}
	names := make([]string, 0, len(namesSet))
	for name := range namesSet {
		names = append(names, name)
	}
	sort.Strings(names)

	changes := make([]routingBindingChange, 0, len(names))
	for _, name := range names {
		oldTable := oldBindings[name]
		newTable := newBindings[name]
		if oldTable != newTable {
			changes = append(changes, routingBindingChange{name: name, oldTable: oldTable, newTable: newTable})
		}
	}
	return changes
}

func routingTablesToCreate(oldPlans, newPlans map[string]routingInstancePlan) []routingInstancePlan {
	var plans []routingInstancePlan
	for name, newPlan := range newPlans {
		oldPlan, exists := oldPlans[name]
		if !exists || oldPlan.tableID != newPlan.tableID {
			plans = append(plans, newPlan)
		}
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].name < plans[j].name })
	return plans
}

func routingTablesToDelete(oldPlans, newPlans map[string]routingInstancePlan) []routingInstancePlan {
	var plans []routingInstancePlan
	for name, oldPlan := range oldPlans {
		newPlan, exists := newPlans[name]
		if !exists || newPlan.tableID != oldPlan.tableID {
			plans = append(plans, oldPlan)
		}
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].name < plans[j].name })
	return plans
}

func (p *VPPPlugin) addIPTablePair(ctx context.Context, plan routingInstancePlan) error {
	table4 := pkgvpp.IPTable{ID: plan.tableID, Name: plan.name}
	if err := p.client.AddIPTable(ctx, table4); err != nil {
		return fmt.Errorf("add IPv4 table: %w", err)
	}
	table6 := pkgvpp.IPTable{ID: plan.tableID, IsIPv6: true, Name: plan.name}
	if err := p.client.AddIPTable(ctx, table6); err != nil {
		_ = p.client.DeleteIPTable(ctx, table4)
		return fmt.Errorf("add IPv6 table: %w", err)
	}
	return nil
}

func (p *VPPPlugin) deleteIPTablePair(ctx context.Context, plan routingInstancePlan) error {
	table4 := pkgvpp.IPTable{ID: plan.tableID, Name: plan.name}
	table6 := pkgvpp.IPTable{ID: plan.tableID, IsIPv6: true, Name: plan.name}
	var firstErr error
	if err := p.client.DeleteIPTable(ctx, table6); err != nil {
		firstErr = fmt.Errorf("delete IPv6 table: %w", err)
	}
	if err := p.client.DeleteIPTable(ctx, table4); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("delete IPv4 table: %w", err)
	}
	return firstErr
}

func (p *VPPPlugin) setInterfaceTablePair(ctx context.Context, name string, tableID uint32, rollbackTableID uint32) error {
	swIfIndex, ok := p.ifaceIndex[name]
	if !ok {
		return fmt.Errorf("interface %s not found in VPP", name)
	}
	if err := p.client.SetInterfaceTable(ctx, swIfIndex, tableID, false); err != nil {
		return fmt.Errorf("set IPv4 table: %w", err)
	}
	if err := p.client.SetInterfaceTable(ctx, swIfIndex, tableID, true); err != nil {
		_ = p.client.SetInterfaceTable(ctx, swIfIndex, rollbackTableID, false)
		return fmt.Errorf("set IPv6 table: %w", err)
	}
	return nil
}

func (p *VPPPlugin) removeConfiguredAddresses(ctx context.Context, cfg *model.RouterConfig, name string, rollback *[]func(context.Context) error) error {
	swIfIndex, ok := p.ifaceIndex[name]
	if !ok {
		return fmt.Errorf("interface %s not found in VPP", name)
	}
	addresses, err := configuredInterfaceAddresses(cfg, name)
	if err != nil {
		return err
	}
	for _, address := range addresses {
		if err := p.client.DeleteInterfaceAddress(ctx, swIfIndex, address); err != nil {
			return err
		}
		if rollback != nil {
			addressCopy := cloneIPNet(address)
			*rollback = append(*rollback, func(ctx context.Context) error {
				return p.client.SetInterfaceAddress(ctx, swIfIndex, addressCopy)
			})
		}
	}
	return nil
}

func (p *VPPPlugin) addConfiguredAddresses(ctx context.Context, cfg *model.RouterConfig, name string, rollback *[]func(context.Context) error) error {
	swIfIndex, ok := p.ifaceIndex[name]
	if !ok {
		return fmt.Errorf("interface %s not found in VPP", name)
	}
	addresses, err := configuredInterfaceAddresses(cfg, name)
	if err != nil {
		return err
	}
	for _, address := range addresses {
		if err := p.client.SetInterfaceAddress(ctx, swIfIndex, address); err != nil {
			return err
		}
		if rollback != nil {
			addressCopy := cloneIPNet(address)
			*rollback = append(*rollback, func(ctx context.Context) error {
				return p.client.DeleteInterfaceAddress(ctx, swIfIndex, addressCopy)
			})
		}
	}
	return nil
}

func configuredInterfaceAddresses(cfg *model.RouterConfig, name string) ([]*net.IPNet, error) {
	if cfg == nil || cfg.Interfaces == nil {
		return nil, nil
	}
	iface := cfg.Interfaces[name]
	if iface == nil {
		return nil, nil
	}

	type addressEntry struct {
		unit    int
		family  string
		address string
	}
	var entries []addressEntry
	for unitNum, unit := range iface.Units {
		if unit == nil {
			continue
		}
		for familyName, family := range unit.Family {
			if family == nil {
				continue
			}
			for _, address := range family.Addresses {
				entries = append(entries, addressEntry{unit: unitNum, family: familyName, address: address})
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].unit != entries[j].unit {
			return entries[i].unit < entries[j].unit
		}
		if entries[i].family != entries[j].family {
			return entries[i].family < entries[j].family
		}
		return entries[i].address < entries[j].address
	})

	addresses := make([]*net.IPNet, 0, len(entries))
	for _, entry := range entries {
		ipNet, err := pkgvpp.ParseCIDRAddress(entry.address)
		if err != nil {
			return nil, fmt.Errorf("parse CIDR %s: %w", entry.address, err)
		}
		addresses = append(addresses, ipNet)
	}
	return addresses, nil
}

func mplsAddedInterfaces(oldMPLS, newMPLS *model.MPLSConfig) []string {
	return mplsDeltaInterfaces(oldMPLS, newMPLS)
}

func mplsRemovedInterfaces(oldMPLS, newMPLS *model.MPLSConfig) []string {
	return mplsDeltaInterfaces(newMPLS, oldMPLS)
}

func mplsDeltaInterfaces(fromMPLS, toMPLS *model.MPLSConfig) []string {
	fromSet := mplsInterfaceSet(fromMPLS)
	toSet := mplsInterfaceSet(toMPLS)
	var names []string
	for name := range toSet {
		if !fromSet[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func mplsInterfaceSet(cfg *model.MPLSConfig) map[string]bool {
	set := make(map[string]bool)
	if cfg == nil {
		return set
	}
	for _, name := range cfg.Interfaces {
		if name != "" {
			set[name] = true
		}
	}
	return set
}

func (p *VPPPlugin) deleteLCPIfPresent(ctx context.Context, swIfIndex uint32) error {
	if _, err := p.lcpManager.Get(ctx, swIfIndex); err != nil {
		if isLCPNotFound(err) {
			return nil
		}
		return err
	}
	if err := p.lcpManager.Delete(ctx, swIfIndex); err != nil {
		if isLCPNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

func isLCPNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "LCP pair not found")
}

func (p *VPPPlugin) applyAddresses(ctx context.Context, swIfIndex uint32, ifaceCfg *model.InterfaceConfig, rollback *[]func(context.Context) error) error {
	for _, unit := range ifaceCfg.Units {
		for _, family := range unit.Family {
			for _, addrStr := range family.Addresses {
				ipNet, err := pkgvpp.ParseCIDRAddress(addrStr)
				if err != nil {
					return fmt.Errorf("parse CIDR %s: %w", addrStr, err)
				}
				if err := p.client.SetInterfaceAddress(ctx, swIfIndex, ipNet); err != nil {
					return fmt.Errorf("set address %s: %w", addrStr, err)
				}
				addrCopy := cloneIPNet(ipNet)
				*rollback = append(*rollback, func(ctx context.Context) error {
					return p.client.DeleteInterfaceAddress(ctx, swIfIndex, addrCopy)
				})
			}
		}
	}
	return nil
}

func (p *VPPPlugin) deleteConfiguredAddresses(ctx context.Context, swIfIndex uint32, ifaceCfg *model.InterfaceConfig) error {
	if ifaceCfg == nil {
		return nil
	}
	var rollbackErr error
	for _, unit := range ifaceCfg.Units {
		if unit == nil {
			continue
		}
		for _, family := range unit.Family {
			if family == nil {
				continue
			}
			for _, addrStr := range family.Addresses {
				ipNet, err := pkgvpp.ParseCIDRAddress(addrStr)
				if err != nil {
					continue
				}
				if err := p.client.DeleteInterfaceAddress(ctx, swIfIndex, ipNet); err != nil {
					p.log.Warn("Failed to remove address during rollback",
						slog.Uint64("sw_if_index", uint64(swIfIndex)),
						slog.String("address", addrStr),
						slog.Any("error", err))
					rollbackErr = errors.Join(rollbackErr, fmt.Errorf("delete address %s: %w", addrStr, err))
				}
			}
		}
	}
	return rollbackErr
}

func cloneIPNet(ipNet *net.IPNet) *net.IPNet {
	if ipNet == nil {
		return nil
	}
	return &net.IPNet{
		IP:   append(net.IP(nil), ipNet.IP...),
		Mask: append(net.IPMask(nil), ipNet.Mask...),
	}
}

func (p *VPPPlugin) findJunosName(swIfIndex uint32) string {
	for name, idx := range p.ifaceIndex {
		if idx == swIfIndex {
			return name
		}
	}
	return ""
}

func (p *VPPPlugin) executeRollback(ctx context.Context, ops []func(context.Context) error) error {
	var rollbackErr error
	for i := len(ops) - 1; i >= 0; i-- {
		if err := ops[i](ctx); err != nil {
			p.log.Error("Rollback operation failed", slog.Any("error", err))
			rollbackErr = errors.Join(rollbackErr, err)
		}
	}
	return rollbackErr
}

func withRollbackError(operationErr, rollbackErr error) error {
	if rollbackErr == nil {
		return operationErr
	}
	return errors.Join(operationErr, fmt.Errorf("rollback failed: %w", rollbackErr))
}

func (p *VPPPlugin) updateLCPReconciliation(ctx context.Context) {
	status := p.checkLCPReconciliation(ctx)
	p.mu.Lock()
	p.lcpReconciliation = status
	p.mu.Unlock()
	p.logLCPReconciliation(status)
}

func (p *VPPPlugin) updateLCPReconciliationLocked(ctx context.Context) {
	status := p.checkLCPReconciliation(ctx)
	p.lcpReconciliation = status
	p.logLCPReconciliation(status)
}

func (p *VPPPlugin) updateQoSCapabilities(ctx context.Context) {
	status := QoSCapabilityStatus{LastCheck: time.Now()}
	capabilities, err := p.client.GetQoSCapabilities(ctx)
	if err != nil {
		status.LastError = vppQoSCapabilityErrorMessage
		p.log.Warn("VPP QoS capability detection failed", slog.String("error", status.LastError))
	} else {
		status.Capabilities = capabilities
		status.Capabilities.Diagnostics = append([]string(nil), capabilities.Diagnostics...)
	}

	p.mu.Lock()
	p.qosCapabilities = status
	p.mu.Unlock()
}

func (p *VPPPlugin) checkLCPReconciliation(ctx context.Context) LCPReconciliationStatus {
	status := LCPReconciliationStatus{
		LastRun:   time.Now(),
		PairCount: len(p.lcpManager.List()),
	}
	inconsistencies, err := p.lcpManager.CheckConsistency(ctx)
	if err != nil {
		status.LastError = vppLCPReconciliationErrorMessage
		return status
	}
	status.Inconsistencies = append([]string(nil), inconsistencies...)
	return status
}

func (p *VPPPlugin) logLCPReconciliation(status LCPReconciliationStatus) {
	if status.LastError != "" {
		p.log.Warn("LCP reconciliation check failed", slog.String("error", status.LastError))
		return
	}
	if len(status.Inconsistencies) > 0 {
		p.log.Warn("LCP reconciliation found inconsistencies",
			slog.Int("pairs", status.PairCount),
			slog.Int("inconsistencies", len(status.Inconsistencies)))
		return
	}
	p.log.Info("LCP reconciliation complete", slog.Int("pairs", status.PairCount))
}

func cloneLCPReconciliationStatus(status LCPReconciliationStatus) LCPReconciliationStatus {
	status.Inconsistencies = append([]string(nil), status.Inconsistencies...)
	return status
}

func cloneQoSCapabilityStatus(status QoSCapabilityStatus) QoSCapabilityStatus {
	status.Capabilities.Diagnostics = append([]string(nil), status.Capabilities.Diagnostics...)
	return status
}

// GetInterfaceIndex returns the VPP sw_if_index for a Junos interface name.
func (p *VPPPlugin) GetInterfaceIndex(name string) (uint32, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	idx, ok := p.ifaceIndex[name]
	return idx, ok
}
