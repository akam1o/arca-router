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

	if err := c.validateSystem(); err != nil {
		return err
	}
	if err := c.validateInterfaces(); err != nil {
		return err
	}
	if err := c.validateChassis(); err != nil {
		return err
	}
	if err := c.validateRouting(); err != nil {
		return err
	}
	if err := c.validateRoutingInstances(); err != nil {
		return err
	}
	if err := c.validateProtocols(); err != nil {
		return err
	}
	if err := c.validatePolicy(); err != nil {
		return err
	}
	if err := c.validateClassOfService(); err != nil {
		return err
	}
	return nil
}

func (c *RouterConfig) validateSystem() error {
	if c.System == nil || c.System.Services == nil || c.System.Services.WebUI == nil {
		return nil
	}
	web := c.System.Services.WebUI
	if web.Port < 0 || web.Port > 65535 {
		return fmt.Errorf("system services web-ui: port must be 0-65535, got %d", web.Port)
	}
	if web.ListenAddress != "" && web.ListenAddress != "localhost" && net.ParseIP(web.ListenAddress) == nil {
		return fmt.Errorf("system services web-ui: invalid listen-address %q", web.ListenAddress)
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

func (c *RouterConfig) validateChassis() error {
	if c.Chassis == nil || c.Chassis.Cluster == nil {
		return nil
	}
	for name, node := range c.Chassis.Cluster.Nodes {
		if node == nil {
			return fmt.Errorf("chassis cluster node %s is nil", name)
		}
		if node.Address != "" && net.ParseIP(node.Address) == nil {
			return fmt.Errorf("chassis cluster node %s: invalid address %q", name, node.Address)
		}
		if node.Priority < 0 || node.Priority > 255 {
			return fmt.Errorf("chassis cluster node %s: priority must be 0-255, got %d", name, node.Priority)
		}
	}
	if sync := c.Chassis.Cluster.Sync; sync != nil && sync.Etcd != nil {
		for _, endpoint := range sync.Etcd.Endpoints {
			if strings.TrimSpace(endpoint) == "" {
				return fmt.Errorf("chassis cluster sync etcd endpoint must not be empty")
			}
		}
	}
	return nil
}

func (c *RouterConfig) validateRoutingInstances() error {
	for name, instance := range c.RoutingInstances {
		if instance == nil {
			return fmt.Errorf("routing-instance %s is nil", name)
		}
		if instance.InstanceType != "" && instance.InstanceType != "vrf" {
			return fmt.Errorf("routing-instance %s: unsupported instance-type %q", name, instance.InstanceType)
		}
		if instance.RouteDistinguisher != "" && !regexp.MustCompile(`^\d+:\d+$`).MatchString(instance.RouteDistinguisher) {
			return fmt.Errorf("routing-instance %s: invalid route-distinguisher %q", name, instance.RouteDistinguisher)
		}
		if instance.VRFTarget != "" && !regexp.MustCompile(`^target:\d+:\d+$`).MatchString(instance.VRFTarget) {
			return fmt.Errorf("routing-instance %s: invalid vrf-target %q", name, instance.VRFTarget)
		}
		for _, ifName := range instance.Interfaces {
			if !junosIfacePattern.MatchString(ifName) {
				return fmt.Errorf("routing-instance %s: invalid interface name %q", name, ifName)
			}
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
	if mpls := c.Protocols.MPLS; mpls != nil {
		for _, ifName := range mpls.Interfaces {
			if !junosIfacePattern.MatchString(ifName) {
				return fmt.Errorf("mpls: invalid interface name %q", ifName)
			}
		}
	}
	if vrrp := c.Protocols.VRRP; vrrp != nil {
		for name, group := range vrrp.Groups {
			if group == nil {
				return fmt.Errorf("vrrp group %s is nil", name)
			}
			if group.Interface != "" && !junosIfacePattern.MatchString(group.Interface) {
				return fmt.Errorf("vrrp group %s: invalid interface name %q", name, group.Interface)
			}
			if group.VirtualAddress != "" && net.ParseIP(group.VirtualAddress) == nil {
				return fmt.Errorf("vrrp group %s: invalid virtual-address %q", name, group.VirtualAddress)
			}
			if group.Priority < 0 || group.Priority > 255 {
				return fmt.Errorf("vrrp group %s: priority must be 0-255, got %d", name, group.Priority)
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

func (c *RouterConfig) validateClassOfService() error {
	if c.ClassOfService == nil {
		return nil
	}
	for name, fc := range c.ClassOfService.ForwardingClasses {
		if fc == nil {
			return fmt.Errorf("class-of-service forwarding-class %s is nil", name)
		}
		if fc.Queue < 0 || fc.Queue > 7 {
			return fmt.Errorf("class-of-service forwarding-class %s: queue must be 0-7, got %d", name, fc.Queue)
		}
	}
	for name, iface := range c.ClassOfService.Interfaces {
		if !junosIfacePattern.MatchString(name) {
			return fmt.Errorf("class-of-service interface %s: invalid interface name", name)
		}
		if iface == nil {
			return fmt.Errorf("class-of-service interface %s is nil", name)
		}
		if iface.OutputTrafficControlProfile != "" {
			if _, ok := c.ClassOfService.TrafficControlProfiles[iface.OutputTrafficControlProfile]; !ok {
				return fmt.Errorf("class-of-service interface %s: output traffic-control-profile %q not found", name, iface.OutputTrafficControlProfile)
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
