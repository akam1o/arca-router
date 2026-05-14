package frr

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

// GenerateStaticRouteConfig generates FRR static route configuration.
// It returns the configuration as a string and any error encountered.
func GenerateStaticRouteConfig(routes []StaticRoute) (string, error) {
	if len(routes) == 0 {
		return "", nil
	}

	var b strings.Builder

	// Sort routes for deterministic output (test stability)
	sortedRoutes := make([]StaticRoute, len(routes))
	copy(sortedRoutes, routes)
	sort.Slice(sortedRoutes, func(i, j int) bool {
		if sortedRoutes[i].Prefix != sortedRoutes[j].Prefix {
			return sortedRoutes[i].Prefix < sortedRoutes[j].Prefix
		}
		return sortedRoutes[i].NextHop < sortedRoutes[j].NextHop
	})

	b.WriteString("!\n")

	// Generate static route commands
	for _, route := range sortedRoutes {
		if err := validateStaticRoute(&route); err != nil {
			return "", err
		}

		// Determine IPv4 or IPv6
		routeCmd := "ip route"
		if route.IsIPv6 {
			routeCmd = "ipv6 route"
		}

		if route.BFD || route.BFDProfile != "" || route.BFDSource != "" || route.BFDMultihop {
			fmt.Fprintf(&b, "%s %s %s bfd", routeCmd, route.Prefix, route.NextHop)
			if route.BFDMultihop {
				b.WriteString(" multi-hop")
			}
			if route.BFDSource != "" {
				fmt.Fprintf(&b, " source %s", route.BFDSource)
			}
			if route.BFDProfile != "" {
				fmt.Fprintf(&b, " profile %s", route.BFDProfile)
			}
			b.WriteString("\n")
		} else if route.Distance > 0 {
			fmt.Fprintf(&b, "%s %s %s %d\n", routeCmd, route.Prefix, route.NextHop, route.Distance)
		} else {
			fmt.Fprintf(&b, "%s %s %s\n", routeCmd, route.Prefix, route.NextHop)
		}
	}

	b.WriteString("!\n")

	return b.String(), nil
}

// validateStaticRoute validates a static route configuration.
func validateStaticRoute(route *StaticRoute) error {
	if route.Prefix == "" {
		return NewInvalidConfigError("static route prefix is required")
	}

	// Validate CIDR format
	_, _, err := net.ParseCIDR(route.Prefix)
	if err != nil {
		return NewInvalidConfigError(fmt.Sprintf("invalid static route prefix: %s", route.Prefix))
	}

	if route.NextHop == "" {
		return NewInvalidConfigError(fmt.Sprintf("static route %s: next-hop is required", route.Prefix))
	}

	// Validate next-hop IP address format
	if net.ParseIP(route.NextHop) == nil {
		return NewInvalidConfigError(fmt.Sprintf("static route %s: invalid next-hop IP: %s", route.Prefix, route.NextHop))
	}

	// Validate distance range (1-255 in FRR, 0 means default)
	if route.Distance < 0 || route.Distance > 255 {
		return NewInvalidConfigError(fmt.Sprintf("static route %s: invalid distance %d (must be 0-255)", route.Prefix, route.Distance))
	}

	if route.Distance > 0 && (route.BFD || route.BFDProfile != "" || route.BFDSource != "" || route.BFDMultihop) {
		return NewInvalidConfigError(fmt.Sprintf("static route %s: distance is not supported with BFD monitoring", route.Prefix))
	}

	if route.BFDSource != "" {
		sourceIP := net.ParseIP(route.BFDSource)
		if sourceIP == nil {
			return NewInvalidConfigError(fmt.Sprintf("static route %s: invalid BFD source IP: %s", route.Prefix, route.BFDSource))
		}
		nextHopIP := net.ParseIP(route.NextHop)
		if (nextHopIP.To4() == nil) != (sourceIP.To4() == nil) {
			return NewInvalidConfigError(fmt.Sprintf("static route %s: BFD source family does not match next-hop", route.Prefix))
		}
	}

	if (route.BFDProfile != "" || route.BFDSource != "" || route.BFDMultihop) && !route.BFD {
		return NewInvalidConfigError(fmt.Sprintf("static route %s: BFD options require BFD to be enabled", route.Prefix))
	}

	return nil
}
