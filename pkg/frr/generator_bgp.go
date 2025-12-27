package frr

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

// GenerateBGPConfig generates FRR BGP configuration from BGPConfig.
// It returns the configuration as a string and any error encountered.
func GenerateBGPConfig(cfg *BGPConfig) (string, error) {
	if cfg == nil {
		return "", nil
	}

	if cfg.ASN == 0 {
		return "", NewInvalidConfigError("BGP ASN is required")
	}

	var b strings.Builder

	// Router BGP section
	b.WriteString("!\n")
	b.WriteString(fmt.Sprintf("router bgp %d\n", cfg.ASN))

	// BGP router-id
	if cfg.RouterID != "" {
		b.WriteString(fmt.Sprintf(" bgp router-id %s\n", cfg.RouterID))
	}

	// Sort neighbors for deterministic output (test stability)
	neighbors := make([]BGPNeighbor, len(cfg.Neighbors))
	copy(neighbors, cfg.Neighbors)
	sort.Slice(neighbors, func(i, j int) bool {
		return neighbors[i].IP < neighbors[j].IP
	})

	// BGP neighbors
	for _, n := range neighbors {
		if err := validateBGPNeighbor(&n); err != nil {
			return "", err
		}

		b.WriteString(fmt.Sprintf(" neighbor %s remote-as %d\n", n.IP, n.RemoteAS))

		if n.Description != "" {
			b.WriteString(fmt.Sprintf(" neighbor %s description %s\n", n.IP, escapeDescription(n.Description)))
		}

		if n.UpdateSource != "" {
			b.WriteString(fmt.Sprintf(" neighbor %s update-source %s\n", n.IP, n.UpdateSource))
		}
	}

	// Address families
	if cfg.IPv4Unicast {
		b.WriteString(" !\n")
		b.WriteString(" address-family ipv4 unicast\n")

		for _, n := range neighbors {
			if !n.IsIPv6 {
				b.WriteString(fmt.Sprintf("  neighbor %s activate\n", n.IP))

				// Apply route-maps (import/export policies)
				if n.RouteMapIn != "" {
					b.WriteString(fmt.Sprintf("  neighbor %s route-map %s in\n", n.IP, n.RouteMapIn))
				}
				if n.RouteMapOut != "" {
					b.WriteString(fmt.Sprintf("  neighbor %s route-map %s out\n", n.IP, n.RouteMapOut))
				}
			}
		}

		b.WriteString(" exit-address-family\n")
	}

	if cfg.IPv6Unicast {
		b.WriteString(" !\n")
		b.WriteString(" address-family ipv6 unicast\n")

		for _, n := range neighbors {
			if n.IsIPv6 {
				b.WriteString(fmt.Sprintf("  neighbor %s activate\n", n.IP))

				// Apply route-maps (import/export policies)
				if n.RouteMapIn != "" {
					b.WriteString(fmt.Sprintf("  neighbor %s route-map %s in\n", n.IP, n.RouteMapIn))
				}
				if n.RouteMapOut != "" {
					b.WriteString(fmt.Sprintf("  neighbor %s route-map %s out\n", n.IP, n.RouteMapOut))
				}
			}
		}

		b.WriteString(" exit-address-family\n")
	}

	b.WriteString("!\n")

	return b.String(), nil
}

// validateBGPNeighbor validates a BGP neighbor configuration.
func validateBGPNeighbor(n *BGPNeighbor) error {
	if n.IP == "" {
		return NewInvalidConfigError("BGP neighbor IP is required")
	}

	// Validate IP address format
	if net.ParseIP(n.IP) == nil {
		return NewInvalidConfigError(fmt.Sprintf("invalid BGP neighbor IP: %s", n.IP))
	}

	if n.RemoteAS == 0 {
		return NewInvalidConfigError(fmt.Sprintf("BGP neighbor %s: remote-as is required", n.IP))
	}

	// Validate AS number range (1-4294967295)
	if n.RemoteAS > 4294967295 {
		return NewInvalidConfigError(fmt.Sprintf("BGP neighbor %s: invalid AS number %d (must be 1-4294967295)", n.IP, n.RemoteAS))
	}

	return nil
}

// escapeDescription escapes special characters in description strings.
// FRR descriptions should be quoted if they contain spaces.
func escapeDescription(desc string) string {
	if strings.Contains(desc, " ") || strings.Contains(desc, "\t") {
		// Quote the description if it contains whitespace
		return fmt.Sprintf("\"%s\"", strings.ReplaceAll(desc, "\"", "\\\""))
	}
	return desc
}

// isIPv6 checks if an IP address is IPv6.
func isIPv6(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	return ip.To4() == nil
}
