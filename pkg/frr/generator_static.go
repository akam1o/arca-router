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

		if route.Distance > 0 {
			b.WriteString(fmt.Sprintf("%s %s %s %d\n", routeCmd, route.Prefix, route.NextHop, route.Distance))
		} else {
			b.WriteString(fmt.Sprintf("%s %s %s\n", routeCmd, route.Prefix, route.NextHop))
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

	return nil
}
