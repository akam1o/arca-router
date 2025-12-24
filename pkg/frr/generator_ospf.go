package frr

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

// GenerateOSPFConfig generates FRR OSPF configuration from OSPFConfig.
// It returns the configuration as a string and any error encountered.
func GenerateOSPFConfig(cfg *OSPFConfig) (string, error) {
	if cfg == nil {
		return "", nil
	}

	var b strings.Builder

	// Determine if OSPFv2 or OSPFv3
	routerCmd := "router ospf"
	if cfg.IsOSPFv3 {
		routerCmd = "router ospf6"
	}

	b.WriteString("!\n")
	b.WriteString(routerCmd + "\n")

	// OSPF router-id (required for OSPFv2)
	if cfg.RouterID != "" {
		// Validate router-id format (must be IPv4)
		if err := validateRouterID(cfg.RouterID); err != nil {
			return "", err
		}
		b.WriteString(fmt.Sprintf(" ospf router-id %s\n", cfg.RouterID))
	} else if !cfg.IsOSPFv3 {
		// OSPFv2 requires router-id
		return "", NewInvalidConfigError("OSPF router-id is required for OSPFv2")
	}

	// Sort networks for deterministic output
	networks := make([]OSPFNetwork, len(cfg.Networks))
	copy(networks, cfg.Networks)
	sort.Slice(networks, func(i, j int) bool {
		return networks[i].Prefix < networks[j].Prefix
	})

	// Network statements
	for _, n := range networks {
		if err := validateOSPFNetwork(&n); err != nil {
			return "", err
		}
		b.WriteString(fmt.Sprintf(" network %s area %s\n", n.Prefix, n.AreaID))
	}

	b.WriteString("!\n")

	// Interface-specific configurations
	// Sort interfaces for deterministic output
	interfaces := make([]OSPFInterface, len(cfg.Interfaces))
	copy(interfaces, cfg.Interfaces)
	sort.Slice(interfaces, func(i, j int) bool {
		return interfaces[i].Name < interfaces[j].Name
	})

	for _, iface := range interfaces {
		if err := validateOSPFInterface(&iface); err != nil {
			return "", err
		}

		// Only generate interface section if there are non-default settings
		hasConfig := iface.Passive || iface.Metric > 0 || iface.Priority != nil
		if hasConfig {
			b.WriteString(fmt.Sprintf("interface %s\n", iface.Name))

			if cfg.IsOSPFv3 {
				// OSPFv3 interface configuration
				b.WriteString(fmt.Sprintf(" ipv6 ospf6 area %s\n", iface.AreaID))

				if iface.Passive {
					b.WriteString(" ipv6 ospf6 passive\n")
				}
				if iface.Metric > 0 {
					b.WriteString(fmt.Sprintf(" ipv6 ospf6 cost %d\n", iface.Metric))
				}
				if iface.Priority != nil {
					b.WriteString(fmt.Sprintf(" ipv6 ospf6 priority %d\n", *iface.Priority))
				}
			} else {
				// OSPFv2 interface configuration
				if iface.Passive {
					b.WriteString(" ip ospf passive\n")
				}
				if iface.Metric > 0 {
					b.WriteString(fmt.Sprintf(" ip ospf cost %d\n", iface.Metric))
				}
				if iface.Priority != nil {
					b.WriteString(fmt.Sprintf(" ip ospf priority %d\n", *iface.Priority))
				}
			}

			b.WriteString("!\n")
		}
	}

	return b.String(), nil
}

// validateRouterID validates an OSPF router ID (must be IPv4 format).
func validateRouterID(routerID string) error {
	ip := net.ParseIP(routerID)
	if ip == nil {
		return NewInvalidConfigError(fmt.Sprintf("invalid OSPF router-id: %s (must be IPv4 format)", routerID))
	}

	// Router-id must be IPv4 format
	if ip.To4() == nil {
		return NewInvalidConfigError(fmt.Sprintf("OSPF router-id must be IPv4 format: %s", routerID))
	}

	return nil
}

// validateOSPFNetwork validates an OSPF network configuration.
func validateOSPFNetwork(n *OSPFNetwork) error {
	if n.Prefix == "" {
		return NewInvalidConfigError("OSPF network prefix is required")
	}

	// Validate CIDR format
	_, _, err := net.ParseCIDR(n.Prefix)
	if err != nil {
		return NewInvalidConfigError(fmt.Sprintf("invalid OSPF network prefix: %s", n.Prefix))
	}

	if n.AreaID == "" {
		return NewInvalidConfigError(fmt.Sprintf("OSPF network %s: area-id is required", n.Prefix))
	}

	// Validate area ID format (can be "0.0.0.0" or "0")
	if err := validateAreaID(n.AreaID); err != nil {
		return err
	}

	return nil
}

// validateOSPFInterface validates an OSPF interface configuration.
func validateOSPFInterface(iface *OSPFInterface) error {
	if iface.Name == "" {
		return NewInvalidConfigError("OSPF interface name is required")
	}

	if iface.AreaID == "" {
		return NewInvalidConfigError(fmt.Sprintf("OSPF interface %s: area-id is required", iface.Name))
	}

	// Validate area ID format
	if err := validateAreaID(iface.AreaID); err != nil {
		return err
	}

	// Validate metric range (1-65535 in OSPF, 0 = not set)
	if iface.Metric < 0 || iface.Metric > 65535 {
		return NewInvalidConfigError(fmt.Sprintf("OSPF interface %s: invalid metric %d (must be 0-65535)", iface.Name, iface.Metric))
	}

	// Validate priority range (0-255 in OSPF, nil = not set)
	if iface.Priority != nil && (*iface.Priority < 0 || *iface.Priority > 255) {
		return NewInvalidConfigError(fmt.Sprintf("OSPF interface %s: invalid priority %d (must be 0-255)", iface.Name, *iface.Priority))
	}

	return nil
}

// validateAreaID validates an OSPF area ID format.
// Area ID can be in dotted decimal format (e.g., "0.0.0.0") or integer format (e.g., "0").
func validateAreaID(areaID string) error {
	// Reject IPv6 early (contains ":")
	if strings.Contains(areaID, ":") {
		return NewInvalidConfigError(fmt.Sprintf("invalid OSPF area-id: %s (IPv6 addresses not allowed, must be IPv4 format like '0.0.0.0' or integer like '0')", areaID))
	}

	// Try parsing as integer (0-4294967295)
	var areaNum uint32
	n, err := fmt.Sscanf(areaID, "%d", &areaNum)
	if err == nil && n == 1 && fmt.Sprintf("%d", areaNum) == areaID {
		// Successfully parsed as integer and no extra characters
		return nil
	}

	// Try parsing as IPv4 address (dotted decimal format)
	ip := net.ParseIP(areaID)
	if ip != nil && ip.To4() != nil {
		return nil
	}

	return NewInvalidConfigError(fmt.Sprintf("invalid OSPF area-id: %s (must be IPv4 format like '0.0.0.0' or integer like '0')", areaID))
}
