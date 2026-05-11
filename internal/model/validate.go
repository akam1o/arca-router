package model

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

// junosIfacePattern matches the legacy config parser's supported Junos-style
// interface names.
var junosIfacePattern = regexp.MustCompile(`^([a-z]{2}-\d+/\d+/\d+|ae\d+|lo\d+|irb|fxp\d+)$`)

// Validate checks the RouterConfig for semantic correctness.
func (c *RouterConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("configuration is nil")
	}

	if err := c.validateInterfaces(); err != nil {
		return err
	}
	if err := c.validateRouting(); err != nil {
		return err
	}
	if err := c.validateProtocols(); err != nil {
		return err
	}
	if err := c.validatePolicy(); err != nil {
		return err
	}
	return nil
}

func (c *RouterConfig) validateInterfaces() error {
	for name, iface := range c.Interfaces {
		if !junosIfacePattern.MatchString(name) {
			return fmt.Errorf("invalid interface name %q: must be Junos format (e.g. ge-0/0/0, ae0, lo0, irb, fxp0)", name)
		}
		for unitNum, unit := range iface.Units {
			if unitNum < 0 {
				return fmt.Errorf("interface %s: unit number must be non-negative, got %d", name, unitNum)
			}
			for familyName, family := range unit.Family {
				if familyName != "inet" && familyName != "inet6" {
					return fmt.Errorf("interface %s unit %d: unsupported family %q", name, unitNum, familyName)
				}
				for _, addr := range family.Addresses {
					if _, _, err := net.ParseCIDR(addr); err != nil {
						return fmt.Errorf("interface %s unit %d family %s: invalid address %q: %w",
							name, unitNum, familyName, addr, err)
					}
				}
			}
		}
	}
	return nil
}

func (c *RouterConfig) validateRouting() error {
	if c.Routing == nil {
		return nil
	}
	if c.Routing.RouterID != "" {
		if net.ParseIP(c.Routing.RouterID) == nil {
			return fmt.Errorf("routing-options: invalid router-id %q", c.Routing.RouterID)
		}
	}
	for _, route := range c.Routing.StaticRoutes {
		if _, _, err := net.ParseCIDR(route.Prefix); err != nil {
			return fmt.Errorf("static route: invalid prefix %q: %w", route.Prefix, err)
		}
		if net.ParseIP(route.NextHop) == nil {
			return fmt.Errorf("static route %s: invalid next-hop %q", route.Prefix, route.NextHop)
		}
	}
	return nil
}

func (c *RouterConfig) validateProtocols() error {
	if c.Protocols == nil {
		return nil
	}
	if bgp := c.Protocols.BGP; bgp != nil {
		if err := c.validateBGP(bgp); err != nil {
			return err
		}
	}
	if ospf := c.Protocols.OSPF; ospf != nil {
		if ospf.RouterID != "" {
			if net.ParseIP(ospf.RouterID) == nil {
				return fmt.Errorf("ospf: invalid router-id %q", ospf.RouterID)
			}
		}
	}
	return nil
}

func (c *RouterConfig) validateBGP(bgp *BGPConfig) error {
	// BGP requires AS number from routing-options
	if c.Routing == nil || c.Routing.AutonomousSystem == 0 {
		return fmt.Errorf("bgp: routing-options autonomous-system is required")
	}
	for groupName, group := range bgp.Groups {
		if group.Type != "" && group.Type != "internal" && group.Type != "external" {
			return fmt.Errorf("bgp group %s: type must be 'internal' or 'external', got %q",
				groupName, group.Type)
		}
		for ip, neighbor := range group.Neighbors {
			if net.ParseIP(ip) == nil {
				return fmt.Errorf("bgp group %s: invalid neighbor IP %q", groupName, ip)
			}
			if neighbor.PeerAS == 0 {
				return fmt.Errorf("bgp group %s neighbor %s: peer-as is required", groupName, ip)
			}
		}
		// Validate import/export policy references
		if group.Import != "" && c.Policy != nil {
			if _, ok := c.Policy.PolicyStatements[group.Import]; !ok {
				return fmt.Errorf("bgp group %s: import policy %q not found in policy-options",
					groupName, group.Import)
			}
		}
		if group.Export != "" && c.Policy != nil {
			if _, ok := c.Policy.PolicyStatements[group.Export]; !ok {
				return fmt.Errorf("bgp group %s: export policy %q not found in policy-options",
					groupName, group.Export)
			}
		}
	}
	return nil
}

func (c *RouterConfig) validatePolicy() error {
	if c.Policy == nil {
		return nil
	}
	for name, pl := range c.Policy.PrefixLists {
		for _, prefix := range pl.Prefixes {
			if _, _, err := net.ParseCIDR(prefix); err != nil {
				return fmt.Errorf("prefix-list %s: invalid prefix %q: %w", name, prefix, err)
			}
		}
	}
	return nil
}

// ResolveRouterID returns the effective router-id for a given protocol,
// applying the Junos-style fallback: protocol-specific → global routing-options.
func (c *RouterConfig) ResolveRouterID(protocol string) string {
	switch strings.ToLower(protocol) {
	case "ospf":
		if c.Protocols != nil && c.Protocols.OSPF != nil && c.Protocols.OSPF.RouterID != "" {
			return c.Protocols.OSPF.RouterID
		}
	}
	if c.Routing != nil && c.Routing.RouterID != "" {
		return c.Routing.RouterID
	}
	return ""
}
