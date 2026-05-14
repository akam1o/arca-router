package engine

import (
	"reflect"
	"sort"

	"github.com/akam1o/arca-router/internal/model"
)

// ConfigDiff represents the minimal set of changes between two configurations.
// Instead of re-applying the entire configuration, southbound plugins use this
// to apply only what changed.
type ConfigDiff struct {
	OldConfig *model.RouterConfig
	NewConfig *model.RouterConfig

	// Interface changes
	InterfacesAdded   map[string]*model.InterfaceConfig
	InterfacesRemoved []string
	InterfacesChanged map[string]*InterfaceChange

	// Protocol changes
	BFDChanged   bool
	OldBFD       *model.BFDConfig
	NewBFD       *model.BFDConfig
	BGPChanged   bool
	OldBGP       *model.BGPConfig
	NewBGP       *model.BGPConfig
	OSPFChanged  bool
	OldOSPF      *model.OSPFConfig
	NewOSPF      *model.OSPFConfig
	OSPF3Changed bool
	OldOSPF3     *model.OSPFConfig
	NewOSPF3     *model.OSPFConfig
	MPLSChanged  bool
	OldMPLS      *model.MPLSConfig
	NewMPLS      *model.MPLSConfig
	VRRPChanged  bool
	OldVRRP      *model.VRRPConfig
	NewVRRP      *model.VRRPConfig

	// Routing changes
	StaticRoutesChanged     bool
	OldStaticRoutes         []*model.StaticRoute
	NewStaticRoutes         []*model.StaticRoute
	RoutingChanged          bool
	OldRouting              *model.RoutingConfig
	NewRouting              *model.RoutingConfig
	RoutingInstancesChanged bool
	OldRoutingInstances     map[string]*model.RoutingInstance
	NewRoutingInstances     map[string]*model.RoutingInstance

	// Policy changes
	PolicyChanged bool
	OldPolicy     *model.PolicyConfig
	NewPolicy     *model.PolicyConfig

	// Advanced feature changes
	ChassisChanged        bool
	OldChassis            *model.ChassisConfig
	NewChassis            *model.ChassisConfig
	ClassOfServiceChanged bool
	OldClassOfService     *model.ClassOfServiceConfig
	NewClassOfService     *model.ClassOfServiceConfig

	// System changes
	SystemChanged bool
	OldSystem     *model.SystemConfig
	NewSystem     *model.SystemConfig

	// Security changes
	SecurityChanged bool
	OldSecurity     *model.SecurityConfig
	NewSecurity     *model.SecurityConfig
}

// InterfaceChange describes what changed on a specific interface.
type InterfaceChange struct {
	Name               string
	DescriptionChanged bool
	OldDescription     string
	NewDescription     string
	AddressesAdded     []UnitAddress
	AddressesRemoved   []UnitAddress
}

// UnitAddress identifies an address on a specific unit/family.
type UnitAddress struct {
	UnitNum int
	Family  string
	Address string
}

// HasChanges returns true if any changes exist.
func (d *ConfigDiff) HasChanges() bool {
	return len(d.InterfacesAdded) > 0 ||
		len(d.InterfacesRemoved) > 0 ||
		len(d.InterfacesChanged) > 0 ||
		d.BFDChanged ||
		d.BGPChanged ||
		d.OSPFChanged ||
		d.OSPF3Changed ||
		d.MPLSChanged ||
		d.VRRPChanged ||
		d.StaticRoutesChanged ||
		d.RoutingChanged ||
		d.RoutingInstancesChanged ||
		d.PolicyChanged ||
		d.ChassisChanged ||
		d.ClassOfServiceChanged ||
		d.SystemChanged ||
		d.SecurityChanged
}

// Clone returns an independent diff with cloned old and new configuration trees.
func (d *ConfigDiff) Clone() *ConfigDiff {
	if d == nil {
		return nil
	}
	return ComputeDiff(d.OldConfig.Clone(), d.NewConfig.Clone())
}

// ComputeDiff computes the minimal diff between two RouterConfig snapshots.
func ComputeDiff(old, new *model.RouterConfig) *ConfigDiff {
	diff := &ConfigDiff{
		InterfacesAdded:   make(map[string]*model.InterfaceConfig),
		InterfacesChanged: make(map[string]*InterfaceChange),
	}

	if old == nil {
		old = model.NewRouterConfig()
	}
	if new == nil {
		new = model.NewRouterConfig()
	}
	diff.OldConfig = old
	diff.NewConfig = new

	computeInterfaceDiff(old, new, diff)
	computeProtocolDiff(old, new, diff)
	computeRoutingDiff(old, new, diff)
	computePolicyDiff(old, new, diff)
	computeAdvancedDiff(old, new, diff)
	computeSystemDiff(old, new, diff)
	computeSecurityDiff(old, new, diff)

	return diff
}

func computeInterfaceDiff(old, new *model.RouterConfig, diff *ConfigDiff) {
	// Find removed and changed interfaces
	for name, oldIface := range old.Interfaces {
		newIface, exists := new.Interfaces[name]
		if !exists {
			diff.InterfacesRemoved = append(diff.InterfacesRemoved, name)
			continue
		}
		if change := diffInterface(name, oldIface, newIface); change != nil {
			diff.InterfacesChanged[name] = change
		}
	}
	sort.Strings(diff.InterfacesRemoved)

	// Find added interfaces
	for name, newIface := range new.Interfaces {
		if _, exists := old.Interfaces[name]; !exists {
			diff.InterfacesAdded[name] = newIface
		}
	}
}

func diffInterface(name string, old, new *model.InterfaceConfig) *InterfaceChange {
	change := &InterfaceChange{Name: name}
	hasChange := false

	if old.Description != new.Description {
		change.DescriptionChanged = true
		change.OldDescription = old.Description
		change.NewDescription = new.Description
		hasChange = true
	}

	// Compute address changes
	oldAddrs := collectAddresses(old)
	newAddrs := collectAddresses(new)

	for _, addr := range newAddrs {
		if !containsAddress(oldAddrs, addr) {
			change.AddressesAdded = append(change.AddressesAdded, addr)
			hasChange = true
		}
	}
	for _, addr := range oldAddrs {
		if !containsAddress(newAddrs, addr) {
			change.AddressesRemoved = append(change.AddressesRemoved, addr)
			hasChange = true
		}
	}

	if !hasChange {
		return nil
	}
	return change
}

func collectAddresses(ic *model.InterfaceConfig) []UnitAddress {
	var result []UnitAddress
	for unitNum, unit := range ic.Units {
		for familyName, family := range unit.Family {
			for _, addr := range family.Addresses {
				result = append(result, UnitAddress{
					UnitNum: unitNum,
					Family:  familyName,
					Address: addr,
				})
			}
		}
	}
	return result
}

func containsAddress(addrs []UnitAddress, target UnitAddress) bool {
	for _, a := range addrs {
		if a.UnitNum == target.UnitNum && a.Family == target.Family && a.Address == target.Address {
			return true
		}
	}
	return false
}

func computeProtocolDiff(old, new *model.RouterConfig, diff *ConfigDiff) {
	oldBFD := getBFD(old)
	newBFD := getBFD(new)
	if !reflect.DeepEqual(oldBFD, newBFD) {
		diff.BFDChanged = true
		diff.OldBFD = oldBFD
		diff.NewBFD = newBFD
	}

	oldBGP := getBGP(old)
	newBGP := getBGP(new)
	if !bgpEqual(oldBGP, newBGP) {
		diff.BGPChanged = true
		diff.OldBGP = oldBGP
		diff.NewBGP = newBGP
	}

	oldOSPF := getOSPF(old)
	newOSPF := getOSPF(new)
	if !ospfEqual(oldOSPF, newOSPF) {
		diff.OSPFChanged = true
		diff.OldOSPF = oldOSPF
		diff.NewOSPF = newOSPF
	}

	oldOSPF3 := getOSPF3(old)
	newOSPF3 := getOSPF3(new)
	if !ospfEqual(oldOSPF3, newOSPF3) {
		diff.OSPF3Changed = true
		diff.OldOSPF3 = oldOSPF3
		diff.NewOSPF3 = newOSPF3
	}

	oldMPLS := getMPLS(old)
	newMPLS := getMPLS(new)
	if !reflect.DeepEqual(oldMPLS, newMPLS) {
		diff.MPLSChanged = true
		diff.OldMPLS = oldMPLS
		diff.NewMPLS = newMPLS
	}

	oldVRRP := getVRRP(old)
	newVRRP := getVRRP(new)
	if !reflect.DeepEqual(oldVRRP, newVRRP) {
		diff.VRRPChanged = true
		diff.OldVRRP = oldVRRP
		diff.NewVRRP = newVRRP
	}
}

func computeRoutingDiff(old, new *model.RouterConfig, diff *ConfigDiff) {
	oldRouting := old.Routing
	newRouting := new.Routing
	if !routingEqual(oldRouting, newRouting) {
		diff.RoutingChanged = true
		diff.OldRouting = oldRouting
		diff.NewRouting = newRouting
	}
	if !staticRoutesEqual(getStaticRoutes(old), getStaticRoutes(new)) {
		diff.StaticRoutesChanged = true
		diff.OldStaticRoutes = getStaticRoutes(old)
		diff.NewStaticRoutes = getStaticRoutes(new)
	}
	if !reflect.DeepEqual(old.RoutingInstances, new.RoutingInstances) {
		diff.RoutingInstancesChanged = true
		diff.OldRoutingInstances = old.RoutingInstances
		diff.NewRoutingInstances = new.RoutingInstances
	}
}

func computePolicyDiff(old, new *model.RouterConfig, diff *ConfigDiff) {
	if !policyEqual(old.Policy, new.Policy) {
		diff.PolicyChanged = true
		diff.OldPolicy = old.Policy
		diff.NewPolicy = new.Policy
	}
}

func computeAdvancedDiff(old, new *model.RouterConfig, diff *ConfigDiff) {
	if !reflect.DeepEqual(old.Chassis, new.Chassis) {
		diff.ChassisChanged = true
		diff.OldChassis = old.Chassis
		diff.NewChassis = new.Chassis
	}
	if !reflect.DeepEqual(old.ClassOfService, new.ClassOfService) {
		diff.ClassOfServiceChanged = true
		diff.OldClassOfService = old.ClassOfService
		diff.NewClassOfService = new.ClassOfService
	}
}

func computeSystemDiff(old, new *model.RouterConfig, diff *ConfigDiff) {
	if !systemEqual(old.System, new.System) {
		diff.SystemChanged = true
		diff.OldSystem = old.System
		diff.NewSystem = new.System
	}
}

func computeSecurityDiff(old, new *model.RouterConfig, diff *ConfigDiff) {
	// For security config, use simple nil/non-nil + JSON comparison approach
	if !securityEqual(old.Security, new.Security) {
		diff.SecurityChanged = true
		diff.OldSecurity = old.Security
		diff.NewSecurity = new.Security
	}
}

// Equality helpers — simple deep comparison for each subsection.

func getBFD(c *model.RouterConfig) *model.BFDConfig {
	if c.Protocols == nil {
		return nil
	}
	return c.Protocols.BFD
}

func getBGP(c *model.RouterConfig) *model.BGPConfig {
	if c.Protocols == nil {
		return nil
	}
	return c.Protocols.BGP
}

func getOSPF(c *model.RouterConfig) *model.OSPFConfig {
	if c.Protocols == nil {
		return nil
	}
	return c.Protocols.OSPF
}

func getOSPF3(c *model.RouterConfig) *model.OSPFConfig {
	if c.Protocols == nil {
		return nil
	}
	return c.Protocols.OSPF3
}

func getMPLS(c *model.RouterConfig) *model.MPLSConfig {
	if c.Protocols == nil {
		return nil
	}
	return c.Protocols.MPLS
}

func getVRRP(c *model.RouterConfig) *model.VRRPConfig {
	if c.Protocols == nil {
		return nil
	}
	return c.Protocols.VRRP
}

func getStaticRoutes(c *model.RouterConfig) []*model.StaticRoute {
	if c.Routing == nil {
		return nil
	}
	return c.Routing.StaticRoutes
}

func bgpEqual(a, b *model.BGPConfig) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if len(a.Groups) != len(b.Groups) {
		return false
	}
	for name, ag := range a.Groups {
		bg, ok := b.Groups[name]
		if !ok {
			return false
		}
		if ag.Type != bg.Type || ag.Import != bg.Import || ag.Export != bg.Export {
			return false
		}
		if len(ag.Neighbors) != len(bg.Neighbors) {
			return false
		}
		for ip, an := range ag.Neighbors {
			bn, ok := bg.Neighbors[ip]
			if !ok {
				return false
			}
			if an.PeerAS != bn.PeerAS || an.Description != bn.Description || an.LocalAddress != bn.LocalAddress ||
				an.BFD != bn.BFD || an.BFDProfile != bn.BFDProfile {
				return false
			}
		}
	}
	return true
}

func ospfEqual(a, b *model.OSPFConfig) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.RouterID != b.RouterID {
		return false
	}
	if len(a.Areas) != len(b.Areas) {
		return false
	}
	for aID, aa := range a.Areas {
		ba, ok := b.Areas[aID]
		if !ok {
			return false
		}
		if len(aa.Interfaces) != len(ba.Interfaces) {
			return false
		}
		for iName, ai := range aa.Interfaces {
			bi, ok := ba.Interfaces[iName]
			if !ok {
				return false
			}
			if ai.Passive != bi.Passive || ai.Metric != bi.Metric ||
				ai.BFD != bi.BFD || ai.BFDProfile != bi.BFDProfile {
				return false
			}
			if (ai.Priority == nil) != (bi.Priority == nil) {
				return false
			}
			if ai.Priority != nil && *ai.Priority != *bi.Priority {
				return false
			}
		}
	}
	return true
}

func routingEqual(a, b *model.RoutingConfig) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.AutonomousSystem == b.AutonomousSystem && a.RouterID == b.RouterID
}

func staticRoutesEqual(a, b []*model.StaticRoute) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Prefix != b[i].Prefix || a[i].NextHop != b[i].NextHop || a[i].Distance != b[i].Distance {
			return false
		}
	}
	return true
}

func policyEqual(a, b *model.PolicyConfig) bool {
	return reflect.DeepEqual(a, b)
}

func systemEqual(a, b *model.SystemConfig) bool {
	return reflect.DeepEqual(a, b)
}

func securityEqual(a, b *model.SecurityConfig) bool {
	return reflect.DeepEqual(a, b)
}
