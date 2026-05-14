package vpp

import (
	"context"
	"fmt"
	"net"
	"sort"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	pkgvpp "github.com/akam1o/arca-router/pkg/vpp"
)

type evpnVXLANPlan struct {
	vni             int
	bridgeID        uint32
	bridgeDomain    string
	sourceInterface string
	request         pkgvpp.VXLANRequest
}

func validateEVPNChanges(diff *engine.ConfigDiff) error {
	if diff == nil || !diff.EVPNChanged {
		return nil
	}
	_, err := evpnVXLANPlanMap(diff.NewConfig, nil, false)
	return err
}

func (p *VPPPlugin) applyEVPNChanges(ctx context.Context, diff *engine.ConfigDiff, rollback *[]func(context.Context) error) error {
	oldPlans, err := evpnVXLANPlanMap(diff.OldConfig, p.ifaceIndex, true)
	if err != nil {
		return err
	}
	newPlans, err := evpnVXLANPlanMap(diff.NewConfig, p.ifaceIndex, true)
	if err != nil {
		return err
	}

	for _, plan := range evpnPlansToDelete(oldPlans, newPlans) {
		if err := p.deleteEVPNVXLAN(ctx, plan, rollback); err != nil {
			return err
		}
	}
	for _, plan := range evpnPlansToCreate(oldPlans, newPlans) {
		if err := p.createEVPNVXLAN(ctx, plan, rollback); err != nil {
			return err
		}
	}
	return nil
}

func evpnVXLANPlanMap(cfg *model.RouterConfig, ifaceIndex map[string]uint32, requireIfIndex bool) (map[int]evpnVXLANPlan, error) {
	if cfg == nil || cfg.Protocols == nil || cfg.Protocols.EVPN == nil || len(cfg.Protocols.EVPN.VNIs) == 0 {
		return nil, nil
	}

	ids := make([]int, 0, len(cfg.Protocols.EVPN.VNIs))
	for id := range cfg.Protocols.EVPN.VNIs {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	plans := make(map[int]evpnVXLANPlan, len(ids))
	for _, id := range ids {
		vni := cfg.Protocols.EVPN.VNIs[id]
		if vni == nil {
			return nil, fmt.Errorf("EVPN VNI %d is nil", id)
		}
		if id <= 0 || id > 16777215 {
			return nil, fmt.Errorf("EVPN VNI %d: VXLAN VNI must be between 1 and 16777215", id)
		}
		if vni.Type == "l3" {
			return nil, fmt.Errorf("EVPN VNI %d: VPP VXLAN L3 VNI dataplane apply is not implemented yet", id)
		}
		if vni.Type != "l2" {
			return nil, fmt.Errorf("EVPN VNI %d: VPP VXLAN dataplane supports only L2 VNIs", id)
		}
		if vni.BridgeDomain == "" {
			return nil, fmt.Errorf("EVPN VNI %d: bridge-domain is required for VPP VXLAN L2 dataplane", id)
		}
		if vni.MulticastGroup == "" {
			return nil, fmt.Errorf("EVPN VNI %d: multicast-group is required for VPP VXLAN dataplane until remote VTEP support is implemented", id)
		}
		if vni.SourceInterface == "" {
			return nil, fmt.Errorf("EVPN VNI %d: source-interface is required for VPP VXLAN multicast dataplane", id)
		}
		if cfg.Interfaces == nil || cfg.Interfaces[vni.SourceInterface] == nil {
			return nil, fmt.Errorf("EVPN VNI %d: source-interface %s is not configured", id, vni.SourceInterface)
		}

		dst := net.ParseIP(vni.MulticastGroup)
		if dst == nil || !dst.IsMulticast() {
			return nil, fmt.Errorf("EVPN VNI %d: multicast-group %s is invalid", id, vni.MulticastGroup)
		}
		dst = normalizeIP(dst)
		src, err := evpnSourceAddress(cfg, vni, dst.To4() == nil)
		if err != nil {
			return nil, fmt.Errorf("EVPN VNI %d: %w", id, err)
		}
		if (src.To4() == nil) != (dst.To4() == nil) {
			return nil, fmt.Errorf("EVPN VNI %d: source-address and multicast-group address families must match", id)
		}

		var sourceIfIndex uint32
		if requireIfIndex {
			idx, ok := ifaceIndex[vni.SourceInterface]
			if !ok {
				return nil, fmt.Errorf("EVPN VNI %d: source-interface %s is not present in VPP", id, vni.SourceInterface)
			}
			sourceIfIndex = idx
		}

		plans[id] = evpnVXLANPlan{
			vni:             id,
			bridgeID:        uint32(id),
			bridgeDomain:    vni.BridgeDomain,
			sourceInterface: vni.SourceInterface,
			request: pkgvpp.VXLANRequest{
				VNI:                     uint32(id),
				SourceAddress:           src,
				DestinationAddress:      dst,
				MulticastInterfaceIndex: sourceIfIndex,
			},
		}
	}
	return plans, nil
}

func evpnSourceAddress(cfg *model.RouterConfig, vni *model.EVPNVNI, wantIPv6 bool) (net.IP, error) {
	if vni.SourceAddress != "" {
		src := net.ParseIP(vni.SourceAddress)
		if src == nil {
			return nil, fmt.Errorf("source-address %s is invalid", vni.SourceAddress)
		}
		return normalizeIP(src), nil
	}
	addresses, err := configuredInterfaceAddresses(cfg, vni.SourceInterface)
	if err != nil {
		return nil, err
	}
	for _, address := range addresses {
		if address == nil {
			continue
		}
		ip := normalizeIP(address.IP)
		if (ip.To4() == nil) == wantIPv6 {
			return ip, nil
		}
	}
	return nil, fmt.Errorf("source-address is required because source-interface %s has no configured address matching multicast-group family", vni.SourceInterface)
}

func normalizeIP(ip net.IP) net.IP {
	if ip4 := ip.To4(); ip4 != nil {
		return append(net.IP(nil), ip4...)
	}
	return append(net.IP(nil), ip.To16()...)
}

func evpnPlansToDelete(oldPlans, newPlans map[int]evpnVXLANPlan) []evpnVXLANPlan {
	var plans []evpnVXLANPlan
	for vni, oldPlan := range oldPlans {
		newPlan, exists := newPlans[vni]
		if !exists || !evpnPlansEqual(oldPlan, newPlan) {
			plans = append(plans, oldPlan)
		}
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].vni < plans[j].vni })
	return plans
}

func evpnPlansToCreate(oldPlans, newPlans map[int]evpnVXLANPlan) []evpnVXLANPlan {
	var plans []evpnVXLANPlan
	for vni, newPlan := range newPlans {
		oldPlan, exists := oldPlans[vni]
		if !exists || !evpnPlansEqual(oldPlan, newPlan) {
			plans = append(plans, newPlan)
		}
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].vni < plans[j].vni })
	return plans
}

func evpnPlansEqual(a, b evpnVXLANPlan) bool {
	return a.vni == b.vni &&
		a.bridgeID == b.bridgeID &&
		a.bridgeDomain == b.bridgeDomain &&
		a.sourceInterface == b.sourceInterface &&
		a.request.VNI == b.request.VNI &&
		a.request.SourceAddress.Equal(b.request.SourceAddress) &&
		a.request.DestinationAddress.Equal(b.request.DestinationAddress) &&
		a.request.MulticastInterfaceIndex == b.request.MulticastInterfaceIndex &&
		a.request.EncapsulationTable == b.request.EncapsulationTable &&
		a.request.L3 == b.request.L3
}

func evpnBridgeDomain(plan evpnVXLANPlan) pkgvpp.BridgeDomain {
	return pkgvpp.BridgeDomain{
		ID:      plan.bridgeID,
		Tag:     plan.bridgeDomain,
		Flood:   true,
		UUFlood: true,
		Forward: true,
		Learn:   true,
	}
}

func (p *VPPPlugin) createEVPNVXLAN(ctx context.Context, plan evpnVXLANPlan, rollback *[]func(context.Context) error) error {
	bridge := evpnBridgeDomain(plan)
	if err := p.client.AddBridgeDomain(ctx, bridge); err != nil {
		return fmt.Errorf("create bridge-domain %s/%d: %w", plan.bridgeDomain, plan.bridgeID, err)
	}
	if rollback != nil {
		bridgeID := plan.bridgeID
		*rollback = append(*rollback, func(ctx context.Context) error {
			return p.client.DeleteBridgeDomain(ctx, bridgeID)
		})
	}

	vxlanIface, err := p.client.CreateVXLAN(ctx, plan.request)
	if err != nil {
		return fmt.Errorf("create VXLAN VNI %d: %w", plan.vni, err)
	}
	p.vxlanIfIndex[plan.vni] = vxlanIface.SwIfIndex
	if rollback != nil {
		planCopy := plan
		*rollback = append(*rollback, func(ctx context.Context) error {
			delete(p.vxlanIfIndex, planCopy.vni)
			return p.client.DeleteVXLAN(ctx, planCopy.request)
		})
	}

	if err := p.client.SetInterfaceUp(ctx, vxlanIface.SwIfIndex); err != nil {
		return fmt.Errorf("set VXLAN VNI %d interface up: %w", plan.vni, err)
	}
	if rollback != nil {
		ifIndex := vxlanIface.SwIfIndex
		*rollback = append(*rollback, func(ctx context.Context) error {
			return p.client.SetInterfaceDown(ctx, ifIndex)
		})
	}

	if err := p.client.SetInterfaceL2Bridge(ctx, vxlanIface.SwIfIndex, plan.bridgeID, true); err != nil {
		return fmt.Errorf("attach VXLAN VNI %d to bridge-domain %d: %w", plan.vni, plan.bridgeID, err)
	}
	if rollback != nil {
		ifIndex := vxlanIface.SwIfIndex
		bridgeID := plan.bridgeID
		*rollback = append(*rollback, func(ctx context.Context) error {
			return p.client.SetInterfaceL2Bridge(ctx, ifIndex, bridgeID, false)
		})
	}
	return nil
}

func (p *VPPPlugin) deleteEVPNVXLAN(ctx context.Context, plan evpnVXLANPlan, rollback *[]func(context.Context) error) error {
	var (
		ifIndex       uint32
		hadIfIndex    bool
		detached      bool
		downed        bool
		tunnelDeleted bool
		bridgeDeleted bool
	)
	if rollback != nil {
		planCopy := plan
		*rollback = append(*rollback, func(ctx context.Context) error {
			return p.restoreDeletedEVPNVXLAN(ctx, planCopy, ifIndex, hadIfIndex, detached, downed, tunnelDeleted, bridgeDeleted)
		})
	}

	if idx, ok := p.vxlanIfIndex[plan.vni]; ok {
		ifIndex = idx
		hadIfIndex = true
		if err := p.client.SetInterfaceL2Bridge(ctx, ifIndex, plan.bridgeID, false); err != nil {
			return fmt.Errorf("detach VXLAN VNI %d from bridge-domain %d: %w", plan.vni, plan.bridgeID, err)
		}
		detached = true
		if err := p.client.SetInterfaceDown(ctx, ifIndex); err != nil {
			return fmt.Errorf("set VXLAN VNI %d interface down: %w", plan.vni, err)
		}
		downed = true
	}
	if err := p.client.DeleteVXLAN(ctx, plan.request); err != nil {
		return fmt.Errorf("delete VXLAN VNI %d: %w", plan.vni, err)
	}
	tunnelDeleted = true
	delete(p.vxlanIfIndex, plan.vni)
	if err := p.client.DeleteBridgeDomain(ctx, plan.bridgeID); err != nil {
		return fmt.Errorf("delete bridge-domain %s/%d: %w", plan.bridgeDomain, plan.bridgeID, err)
	}
	bridgeDeleted = true
	return nil
}

func (p *VPPPlugin) restoreDeletedEVPNVXLAN(ctx context.Context, plan evpnVXLANPlan, ifIndex uint32, hadIfIndex bool, detached bool, downed bool, tunnelDeleted bool, bridgeDeleted bool) error {
	if tunnelDeleted {
		if bridgeDeleted {
			if err := p.client.AddBridgeDomain(ctx, evpnBridgeDomain(plan)); err != nil {
				return fmt.Errorf("restore bridge-domain %s/%d: %w", plan.bridgeDomain, plan.bridgeID, err)
			}
		}
		vxlanIface, err := p.client.CreateVXLAN(ctx, plan.request)
		if err != nil {
			return fmt.Errorf("restore VXLAN VNI %d: %w", plan.vni, err)
		}
		p.vxlanIfIndex[plan.vni] = vxlanIface.SwIfIndex
		if err := p.client.SetInterfaceUp(ctx, vxlanIface.SwIfIndex); err != nil {
			return fmt.Errorf("restore VXLAN VNI %d interface up: %w", plan.vni, err)
		}
		if err := p.client.SetInterfaceL2Bridge(ctx, vxlanIface.SwIfIndex, plan.bridgeID, true); err != nil {
			return fmt.Errorf("restore VXLAN VNI %d bridge membership: %w", plan.vni, err)
		}
		return nil
	}
	if hadIfIndex && downed {
		if err := p.client.SetInterfaceUp(ctx, ifIndex); err != nil {
			return fmt.Errorf("restore VXLAN VNI %d interface up: %w", plan.vni, err)
		}
	}
	if hadIfIndex && detached {
		if err := p.client.SetInterfaceL2Bridge(ctx, ifIndex, plan.bridgeID, true); err != nil {
			return fmt.Errorf("restore VXLAN VNI %d bridge membership: %w", plan.vni, err)
		}
	}
	return nil
}

func reverseEVPNDiff(diff *engine.ConfigDiff) *engine.ConfigDiff {
	return &engine.ConfigDiff{
		OldConfig:   diff.NewConfig,
		NewConfig:   diff.OldConfig,
		OldEVPN:     diff.NewEVPN,
		NewEVPN:     diff.OldEVPN,
		EVPNChanged: diff.EVPNChanged,
	}
}
