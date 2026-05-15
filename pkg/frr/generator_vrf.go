package frr

import (
	"fmt"
	"sort"
	"strings"

	"github.com/akam1o/arca-router/pkg/config"
)

// GenerateVRFConfig generates FRR VRF and VPN route-leaking configuration.
func GenerateVRFConfig(vrfs []VRFConfig) (string, error) {
	if len(vrfs) == 0 {
		return "", nil
	}

	sorted := append([]VRFConfig(nil), vrfs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	var b strings.Builder
	for _, vrf := range sorted {
		if vrf.Name == "" {
			return "", NewInvalidConfigError("VRF name is required")
		}
		if vrf.VNI < 0 || vrf.VNI > 16777215 {
			return "", NewInvalidConfigError(fmt.Sprintf("VRF %s: VNI must be between 1 and 16777215", vrf.Name))
		}
		fmt.Fprintf(&b, "vrf %s\n", vrf.Name)
		if vrf.VNI > 0 {
			fmt.Fprintf(&b, " vni %d\n", vrf.VNI)
		}
		b.WriteString(" exit-vrf\n")
		b.WriteString("!\n")

		if !vrfHasBGPConfig(vrf) {
			continue
		}
		if vrf.ASN == 0 {
			return "", NewInvalidConfigError(fmt.Sprintf("VRF %s: BGP ASN is required for VPN/EVPN import/export", vrf.Name))
		}

		if vrfHasVPNConfig(vrf) {
			for _, family := range []string{"ipv4", "ipv6"} {
				fmt.Fprintf(&b, "router bgp %d vrf %s\n", vrf.ASN, vrf.Name)
				b.WriteString(" !\n")
				fmt.Fprintf(&b, " address-family %s unicast\n", family)
				if len(vrf.ExportTargets) > 0 {
					if vrf.RouteDistinguisher == "" {
						return "", NewInvalidConfigError(fmt.Sprintf("VRF %s: route-distinguisher is required for VPN export", vrf.Name))
					}
					fmt.Fprintf(&b, "  rd vpn export %s\n", vrf.RouteDistinguisher)
					b.WriteString("  export vpn\n")
					b.WriteString("  label vpn export auto\n")
					fmt.Fprintf(&b, "  rt vpn export %s\n", strings.Join(vrf.ExportTargets, " "))
				}
				if len(vrf.ImportTargets) > 0 {
					b.WriteString("  import vpn\n")
					fmt.Fprintf(&b, "  rt vpn import %s\n", strings.Join(vrf.ImportTargets, " "))
				}
				if vrf.ImportRouteMap != "" {
					fmt.Fprintf(&b, "  route-map vpn import %s\n", vrf.ImportRouteMap)
				}
				if vrf.ExportRouteMap != "" {
					fmt.Fprintf(&b, "  route-map vpn export %s\n", vrf.ExportRouteMap)
				}
				b.WriteString(" exit-address-family\n")
				b.WriteString("!\n")
			}
		}

		if vrf.EVPN != nil {
			if err := writeVRFEVPNConfig(&b, vrf); err != nil {
				return "", err
			}
		}
	}
	return b.String(), nil
}

func convertVRFConfig(cfg *config.Config, routeMaps []RouteMap) ([]VRFConfig, error) {
	vrfs, _, err := convertVRFConfigWithPolicyChains(cfg, routeMaps)
	return vrfs, err
}

func convertVRFConfigWithPolicyChains(cfg *config.Config, routeMaps []RouteMap) ([]VRFConfig, []RouteMap, error) {
	if cfg == nil || len(cfg.RoutingInstances) == 0 {
		return nil, nil, nil
	}

	var asn uint32
	if cfg.RoutingOptions != nil {
		asn = cfg.RoutingOptions.AutonomousSystem
	}
	routeMapByName := make(map[string]RouteMap, len(routeMaps))
	for _, routeMap := range routeMaps {
		routeMapByName[routeMap.Name] = routeMap
	}

	names := make([]string, 0, len(cfg.RoutingInstances))
	for name := range cfg.RoutingInstances {
		names = append(names, name)
	}
	sort.Strings(names)

	vrfs := make([]VRFConfig, 0, len(names))
	var extraRouteMaps []RouteMap
	for _, name := range names {
		instance := cfg.RoutingInstances[name]
		if instance == nil {
			continue
		}

		importTargets, exportTargets := routingInstanceTargets(instance)
		importRouteMap, importExtra, err := composeVRFPolicyRouteMap(name, "IMPORT", instance.VRFImport, routeMapByName)
		if err != nil {
			return nil, nil, err
		}
		exportRouteMap, exportExtra, err := composeVRFPolicyRouteMap(name, "EXPORT", instance.VRFExport, routeMapByName)
		if err != nil {
			return nil, nil, err
		}
		if importRouteMap != "" && len(importTargets) == 0 {
			return nil, nil, fmt.Errorf("routing-instance %s: vrf-import requires an import vrf-target", name)
		}
		if exportRouteMap != "" && len(exportTargets) == 0 {
			return nil, nil, fmt.Errorf("routing-instance %s: vrf-export requires an export vrf-target", name)
		}
		if len(exportTargets) > 0 && instance.RouteDistinguisher == "" {
			return nil, nil, fmt.Errorf("routing-instance %s: route-distinguisher is required for VPN export", name)
		}
		if vrfNeedsBGP(importTargets, exportTargets, importRouteMap, exportRouteMap) && asn == 0 {
			return nil, nil, fmt.Errorf("routing-instance %s: routing-options autonomous-system is required for VPN import/export", name)
		}

		extraRouteMaps = append(extraRouteMaps, importExtra...)
		extraRouteMaps = append(extraRouteMaps, exportExtra...)
		vrfs = append(vrfs, VRFConfig{
			Name:               name,
			ASN:                asn,
			RouteDistinguisher: instance.RouteDistinguisher,
			ImportTargets:      importTargets,
			ExportTargets:      exportTargets,
			ImportRouteMap:     importRouteMap,
			ExportRouteMap:     exportRouteMap,
		})
	}
	return vrfs, extraRouteMaps, nil
}

func routingInstanceTargets(instance *config.RoutingInstance) ([]string, []string) {
	importSet := make(map[string]bool)
	exportSet := make(map[string]bool)
	if instance.VRFTarget != "" {
		target := stripTargetPrefix(instance.VRFTarget)
		importSet[target] = true
		exportSet[target] = true
	}
	for _, target := range instance.VRFTargetImport {
		importSet[stripTargetPrefix(target)] = true
	}
	for _, target := range instance.VRFTargetExport {
		exportSet[stripTargetPrefix(target)] = true
	}
	return sortedStringSet(importSet), sortedStringSet(exportSet)
}

func stripTargetPrefix(target string) string {
	return strings.TrimPrefix(target, "target:")
}

func sortedStringSet(set map[string]bool) []string {
	values := make([]string, 0, len(set))
	for value := range set {
		if value != "" {
			values = append(values, value)
		}
	}
	sort.Strings(values)
	return values
}

func composeVRFPolicyRouteMap(vrfName, direction string, policyNames []string, routeMaps map[string]RouteMap) (string, []RouteMap, error) {
	if len(policyNames) == 0 {
		return "", nil, nil
	}
	if len(policyNames) == 1 {
		name := policyNames[0]
		if _, ok := routeMaps[name]; !ok {
			return "", nil, fmt.Errorf("routing-instance %s: policy route-map %s not found", vrfName, name)
		}
		return name, nil, nil
	}

	name := syntheticVRFRouteMapName(vrfName, direction)
	routeMap := RouteMap{Name: name}
	seq := 10
	for _, policyName := range policyNames {
		source, ok := routeMaps[policyName]
		if !ok {
			return "", nil, fmt.Errorf("routing-instance %s: policy route-map %s not found", vrfName, policyName)
		}
		entries := append([]RouteMapEntry(nil), source.Entries...)
		sort.Slice(entries, func(i, j int) bool { return entries[i].Seq < entries[j].Seq })
		for _, entry := range entries {
			entry.Seq = seq
			seq += 10
			routeMap.Entries = append(routeMap.Entries, entry)
		}
	}
	return name, []RouteMap{routeMap}, nil
}

func syntheticVRFRouteMapName(vrfName, direction string) string {
	var b strings.Builder
	b.WriteString("ARCA-")
	for _, r := range strings.ToUpper(vrfName) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	b.WriteString("-VRF-")
	b.WriteString(direction)
	return b.String()
}

func vrfNeedsBGP(importTargets, exportTargets []string, importRouteMap, exportRouteMap string) bool {
	return len(importTargets) > 0 || len(exportTargets) > 0 || importRouteMap != "" || exportRouteMap != ""
}

func vrfHasVPNConfig(vrf VRFConfig) bool {
	return vrfNeedsBGP(vrf.ImportTargets, vrf.ExportTargets, vrf.ImportRouteMap, vrf.ExportRouteMap)
}

func vrfHasBGPConfig(vrf VRFConfig) bool {
	return vrfHasVPNConfig(vrf) || vrf.EVPN != nil
}

func writeVRFEVPNConfig(b *strings.Builder, vrf VRFConfig) error {
	for _, target := range append(append([]string{}, vrf.EVPN.ImportTargets...), vrf.EVPN.ExportTargets...) {
		if !evpnTargetPattern.MatchString(target) {
			return NewInvalidConfigError(fmt.Sprintf("VRF %s: invalid EVPN route-target %s", vrf.Name, target))
		}
	}

	fmt.Fprintf(b, "router bgp %d vrf %s\n", vrf.ASN, vrf.Name)
	b.WriteString(" !\n")
	b.WriteString(" address-family l2vpn evpn\n")
	if vrf.EVPN.AdvertiseIPv4Unicast {
		b.WriteString("  advertise ipv4 unicast\n")
	}
	if vrf.EVPN.AdvertiseIPv6Unicast {
		b.WriteString("  advertise ipv6 unicast\n")
	}
	if len(vrf.EVPN.ImportTargets) > 0 {
		fmt.Fprintf(b, "  route-target import %s\n", strings.Join(vrf.EVPN.ImportTargets, " "))
	}
	if len(vrf.EVPN.ExportTargets) > 0 {
		fmt.Fprintf(b, "  route-target export %s\n", strings.Join(vrf.EVPN.ExportTargets, " "))
	}
	b.WriteString(" exit-address-family\n")
	b.WriteString("!\n")
	return nil
}
