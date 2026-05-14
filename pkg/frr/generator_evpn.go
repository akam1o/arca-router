package frr

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"

	"github.com/akam1o/arca-router/pkg/config"
)

var (
	evpnRDPattern     = regexp.MustCompile(`^\d+:\d+$`)
	evpnTargetPattern = regexp.MustCompile(`^\d+:\d+$`)
)

// GenerateEVPNConfig generates the global BGP EVPN address family.
func GenerateEVPNConfig(cfg *EVPNConfig, neighbors []BGPNeighbor) (string, error) {
	if cfg == nil {
		return "", nil
	}
	if err := validateEVPNConfig(cfg); err != nil {
		return "", err
	}

	sortedNeighbors := append([]BGPNeighbor(nil), neighbors...)
	sort.Slice(sortedNeighbors, func(i, j int) bool { return sortedNeighbors[i].IP < sortedNeighbors[j].IP })

	var b strings.Builder
	b.WriteString(" !\n")
	b.WriteString(" address-family l2vpn evpn\n")
	for _, neighbor := range sortedNeighbors {
		fmt.Fprintf(&b, "  neighbor %s activate\n", neighbor.IP)
	}
	b.WriteString("  advertise-all-vni\n")
	for _, vni := range sortedEVPNVNIs(cfg.VNIs) {
		if vni.Type != "l2" || !evpnVNIHasRouteTargets(vni) {
			continue
		}
		fmt.Fprintf(&b, "  vni %d\n", vni.VNI)
		if len(vni.ImportTargets) > 0 {
			fmt.Fprintf(&b, "   route-target import %s\n", strings.Join(vni.ImportTargets, " "))
		}
		if len(vni.ExportTargets) > 0 {
			fmt.Fprintf(&b, "   route-target export %s\n", strings.Join(vni.ExportTargets, " "))
		}
		b.WriteString("  exit-vni\n")
	}
	b.WriteString(" exit-address-family\n")
	return b.String(), nil
}

func convertEVPNConfig(cfg *config.Config, ifaceMapping map[string]string) (*EVPNConfig, error) {
	if cfg == nil || cfg.Protocols == nil || cfg.Protocols.EVPN == nil || len(cfg.Protocols.EVPN.VNIs) == 0 {
		return nil, nil
	}
	if cfg.RoutingOptions == nil || cfg.RoutingOptions.AutonomousSystem == 0 {
		return nil, fmt.Errorf("EVPN requires autonomous-system to be configured in routing-options")
	}

	ids := make([]int, 0, len(cfg.Protocols.EVPN.VNIs))
	for id := range cfg.Protocols.EVPN.VNIs {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	evpnConfig := &EVPNConfig{VNIs: make([]EVPNVNI, 0, len(ids))}
	for _, id := range ids {
		arcaVNI := cfg.Protocols.EVPN.VNIs[id]
		if arcaVNI == nil {
			return nil, fmt.Errorf("EVPN VNI %d is nil", id)
		}
		vniID := id
		if arcaVNI.VNI != 0 {
			if arcaVNI.VNI != id {
				return nil, fmt.Errorf("EVPN VNI %d has mismatched VNI value %d", id, arcaVNI.VNI)
			}
			vniID = arcaVNI.VNI
		}
		importTargets, exportTargets := evpnRouteTargets(arcaVNI)
		sourceInterface := arcaVNI.SourceInterface
		if mapped, ok := ifaceMapping[sourceInterface]; ok {
			sourceInterface = mapped
		}
		evpnConfig.VNIs = append(evpnConfig.VNIs, EVPNVNI{
			VNI:                vniID,
			Type:               arcaVNI.Type,
			BridgeDomain:       arcaVNI.BridgeDomain,
			VLANID:             arcaVNI.VLANID,
			RoutingInstance:    arcaVNI.RoutingInstance,
			RouteDistinguisher: arcaVNI.RouteDistinguisher,
			ImportTargets:      importTargets,
			ExportTargets:      exportTargets,
			SourceInterface:    sourceInterface,
			SourceAddress:      arcaVNI.SourceAddress,
			MulticastGroup:     arcaVNI.MulticastGroup,
		})
	}
	if err := validateEVPNConfig(evpnConfig); err != nil {
		return nil, err
	}
	return evpnConfig, nil
}

func attachEVPNConfig(cfg *config.Config, frrConfig *Config) error {
	evpnConfig, err := convertEVPNConfig(cfg, frrConfig.InterfaceMapping)
	if err != nil {
		return err
	}
	if evpnConfig == nil {
		return nil
	}
	if frrConfig.BGP == nil {
		bgpConfig, err := newEVPNBGPConfig(cfg)
		if err != nil {
			return err
		}
		frrConfig.BGP = bgpConfig
	}
	frrConfig.BGP.EVPN = evpnConfig
	return nil
}

func newEVPNBGPConfig(cfg *config.Config) (*BGPConfig, error) {
	if cfg.RoutingOptions == nil || cfg.RoutingOptions.AutonomousSystem == 0 {
		return nil, fmt.Errorf("EVPN requires autonomous-system to be configured in routing-options")
	}
	return &BGPConfig{
		ASN:      cfg.RoutingOptions.AutonomousSystem,
		RouterID: cfg.RoutingOptions.RouterID,
	}, nil
}

func applyEVPNToVRFs(evpn *EVPNConfig, vrfs []VRFConfig) ([]VRFConfig, error) {
	if evpn == nil {
		return vrfs, nil
	}
	updated := append([]VRFConfig(nil), vrfs...)
	indexByName := make(map[string]int, len(updated))
	for i := range updated {
		indexByName[updated[i].Name] = i
	}

	for _, vni := range sortedEVPNVNIs(evpn.VNIs) {
		if vni.Type != "l3" {
			continue
		}
		idx, ok := indexByName[vni.RoutingInstance]
		if !ok {
			return nil, fmt.Errorf("EVPN VNI %d references unknown routing-instance %s", vni.VNI, vni.RoutingInstance)
		}
		vrf := &updated[idx]
		if vrf.ASN == 0 {
			return nil, fmt.Errorf("EVPN VNI %d requires routing-options autonomous-system for routing-instance %s", vni.VNI, vni.RoutingInstance)
		}
		if vrf.VNI != 0 && vrf.VNI != vni.VNI {
			return nil, fmt.Errorf("routing-instance %s already has L3 VNI %d, cannot also use %d", vni.RoutingInstance, vrf.VNI, vni.VNI)
		}
		vrf.VNI = vni.VNI
		vrf.EVPN = &VRFEVPNConfig{
			ImportTargets:        append([]string(nil), vni.ImportTargets...),
			ExportTargets:        append([]string(nil), vni.ExportTargets...),
			AdvertiseIPv4Unicast: true,
			AdvertiseIPv6Unicast: true,
		}
	}
	return updated, nil
}

func validateEVPNConfig(cfg *EVPNConfig) error {
	if cfg == nil {
		return nil
	}
	seen := make(map[int]struct{}, len(cfg.VNIs))
	for _, vni := range cfg.VNIs {
		if vni.VNI < 1 || vni.VNI > 16777215 {
			return NewInvalidConfigError(fmt.Sprintf("EVPN VNI %d must be between 1 and 16777215", vni.VNI))
		}
		if _, ok := seen[vni.VNI]; ok {
			return NewInvalidConfigError(fmt.Sprintf("EVPN VNI %d is duplicated", vni.VNI))
		}
		seen[vni.VNI] = struct{}{}
		switch vni.Type {
		case "l2":
			if strings.TrimSpace(vni.BridgeDomain) == "" {
				return NewInvalidConfigError(fmt.Sprintf("EVPN VNI %d: bridge-domain is required for L2 VNI", vni.VNI))
			}
			if vni.RoutingInstance != "" {
				return NewInvalidConfigError(fmt.Sprintf("EVPN VNI %d: routing-instance is only valid for L3 VNI", vni.VNI))
			}
		case "l3":
			if strings.TrimSpace(vni.RoutingInstance) == "" {
				return NewInvalidConfigError(fmt.Sprintf("EVPN VNI %d: routing-instance is required for L3 VNI", vni.VNI))
			}
			if vni.BridgeDomain != "" {
				return NewInvalidConfigError(fmt.Sprintf("EVPN VNI %d: bridge-domain is only valid for L2 VNI", vni.VNI))
			}
			if vni.VLANID != 0 {
				return NewInvalidConfigError(fmt.Sprintf("EVPN VNI %d: vlan-id is only valid for L2 VNI", vni.VNI))
			}
		default:
			return NewInvalidConfigError(fmt.Sprintf("EVPN VNI %d: type must be l2 or l3", vni.VNI))
		}
		if vni.VLANID != 0 && (vni.VLANID < 1 || vni.VLANID > 4094) {
			return NewInvalidConfigError(fmt.Sprintf("EVPN VNI %d: vlan-id must be 1-4094", vni.VNI))
		}
		if vni.RouteDistinguisher != "" && !evpnRDPattern.MatchString(vni.RouteDistinguisher) {
			return NewInvalidConfigError(fmt.Sprintf("EVPN VNI %d: invalid route-distinguisher %s", vni.VNI, vni.RouteDistinguisher))
		}
		for _, target := range append(append([]string{}, vni.ImportTargets...), vni.ExportTargets...) {
			if !evpnTargetPattern.MatchString(target) {
				return NewInvalidConfigError(fmt.Sprintf("EVPN VNI %d: invalid route-target %s", vni.VNI, target))
			}
		}
		if vni.SourceAddress != "" && net.ParseIP(vni.SourceAddress) == nil {
			return NewInvalidConfigError(fmt.Sprintf("EVPN VNI %d: invalid source-address %s", vni.VNI, vni.SourceAddress))
		}
		if vni.MulticastGroup != "" {
			groupIP := net.ParseIP(vni.MulticastGroup)
			if groupIP == nil || !groupIP.IsMulticast() {
				return NewInvalidConfigError(fmt.Sprintf("EVPN VNI %d: invalid multicast-group %s", vni.VNI, vni.MulticastGroup))
			}
		}
	}
	return nil
}

func evpnRouteTargets(vni *config.EVPNVNI) ([]string, []string) {
	importSet := make(map[string]bool)
	exportSet := make(map[string]bool)
	if vni.VRFTarget != "" {
		target := stripTargetPrefix(vni.VRFTarget)
		importSet[target] = true
		exportSet[target] = true
	}
	for _, target := range vni.VRFTargetImport {
		importSet[stripTargetPrefix(target)] = true
	}
	for _, target := range vni.VRFTargetExport {
		exportSet[stripTargetPrefix(target)] = true
	}
	return sortedStringSet(importSet), sortedStringSet(exportSet)
}

func sortedEVPNVNIs(vnis []EVPNVNI) []EVPNVNI {
	sorted := append([]EVPNVNI(nil), vnis...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].VNI < sorted[j].VNI })
	return sorted
}

func evpnVNIHasRouteTargets(vni EVPNVNI) bool {
	return len(vni.ImportTargets) > 0 || len(vni.ExportTargets) > 0
}
