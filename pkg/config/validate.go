package config

import (
	"fmt"
	"net"
	"regexp"
	"strings"

	"github.com/akam1o/arca-router/pkg/errors"
)

// Interface name patterns
var (
	// interfaceNamePattern matches Junos-style interface names
	// Supports: ge-X/X/X, xe-X/X/X, et-X/X/X (physical)
	//           ae0, ae1, ... (aggregated ethernet)
	//           lo0 (loopback)
	//           irb (integrated routing and bridging)
	//           fxp0 (management)
	interfaceNamePattern = regexp.MustCompile(`^([a-z]{2}-\d+/\d+/\d+|ae\d+|lo\d+|irb|fxp\d+)$`)
)

// Validate performs semantic validation on the configuration
func (c *Config) Validate() error {
	if c == nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			"Configuration is nil",
			"Internal error: configuration object is nil",
			"Report this issue to the maintainers",
		)
	}

	// Validate system configuration
	// If System is not defined, create default with hostname
	if c.System == nil {
		c.System = &SystemConfig{
			HostName: "arca-router", // Default hostname
		}
	}

	// If hostname is not set, use default
	if c.System.HostName == "" {
		c.System.HostName = "arca-router"
	}

	// Validate system configuration
	if err := c.System.Validate(); err != nil {
		return err
	}

	// Validate interfaces
	for name, iface := range c.Interfaces {
		if err := validateInterfaceName(name); err != nil {
			return err
		}
		if err := iface.Validate(name); err != nil {
			return err
		}
	}

	// Validate routing options
	if c.RoutingOptions != nil {
		if err := c.RoutingOptions.Validate(); err != nil {
			return err
		}
	}

	// Validate protocols
	if c.Protocols != nil {
		if err := c.Protocols.Validate(c); err != nil {
			return err
		}
	}

	return nil
}

// Validate validates system configuration
func (s *SystemConfig) Validate() error {
	// Hostname should have been set by Config.Validate() if empty
	// This is a sanity check
	if s.HostName == "" {
		return errors.New(
			errors.ErrCodeConfigValidation,
			"System hostname is empty after default assignment",
			"Internal validation error",
			"Report this issue to the maintainers",
		)
	}

	// Validate hostname format (RFC 1123)
	if len(s.HostName) > 253 {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Hostname too long: %s", s.HostName),
			"Hostname must be 253 characters or less",
			"Use a shorter hostname",
		)
	}

	hostnamePattern := regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)
	if !hostnamePattern.MatchString(s.HostName) {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Invalid hostname format: %s", s.HostName),
			"Hostname must follow RFC 1123 format",
			"Use only alphanumeric characters and hyphens, starting and ending with alphanumeric",
		)
	}

	return nil
}

// Validate validates interface configuration
func (i *Interface) Validate(name string) error {
	if i == nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Interface %s is nil", name),
			"Internal error: interface object is nil",
			"Report this issue to the maintainers",
		)
	}

	// Description is optional, no validation needed if empty
	if len(i.Description) > 255 {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Interface %s description too long", name),
			"Description must be 255 characters or less",
			"Use a shorter description",
		)
	}

	// Validate units
	for unitNum, unit := range i.Units {
		if err := unit.Validate(name, unitNum); err != nil {
			return err
		}
	}

	return nil
}

// Validate validates unit configuration
func (u *Unit) Validate(ifaceName string, unitNum int) error {
	if u == nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Unit %d on interface %s is nil", unitNum, ifaceName),
			"Internal error: unit object is nil",
			"Report this issue to the maintainers",
		)
	}

	// Validate unit number range
	if unitNum < 0 || unitNum > 32767 {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Invalid unit number %d on interface %s", unitNum, ifaceName),
			"Unit number must be between 0 and 32767",
			"Use a valid unit number in the allowed range",
		)
	}

	// Validate families
	for familyName, family := range u.Family {
		if err := family.Validate(ifaceName, unitNum, familyName); err != nil {
			return err
		}
	}

	return nil
}

// Validate validates family configuration
func (f *Family) Validate(ifaceName string, unitNum int, familyName string) error {
	if f == nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Family %s on interface %s unit %d is nil", familyName, ifaceName, unitNum),
			"Internal error: family object is nil",
			"Report this issue to the maintainers",
		)
	}

	// Validate family name
	validFamilies := map[string]bool{
		"inet":  true,
		"inet6": true,
	}
	if !validFamilies[familyName] {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Invalid family %s on interface %s unit %d", familyName, ifaceName, unitNum),
			fmt.Sprintf("Family must be one of: %s", strings.Join(keys(validFamilies), ", ")),
			"Use a valid address family",
		)
	}

	// Validate addresses
	if len(f.Addresses) == 0 {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("No addresses configured for family %s on interface %s unit %d", familyName, ifaceName, unitNum),
			"At least one address must be configured",
			"Add an address using 'set interfaces <name> unit <num> family <family> address <cidr>'",
		)
	}

	for _, addr := range f.Addresses {
		if err := validateAddress(addr, familyName, ifaceName, unitNum); err != nil {
			return err
		}
	}

	return nil
}

// validateInterfaceName validates an interface name
func validateInterfaceName(name string) error {
	if name == "" {
		return errors.New(
			errors.ErrCodeConfigValidation,
			"Interface name is empty",
			"Interface name must be specified",
			"Use a valid interface name like 'ge-0/0/0'",
		)
	}

	if !interfaceNamePattern.MatchString(name) {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Invalid interface name: %s", name),
			"Interface name must be a valid Junos-style name (e.g., ge-0/0/0, xe-1/2/3, ae0, lo0, irb, fxp0)",
			"Use a valid Junos-style interface name",
		)
	}

	return nil
}

// validateAddress validates a CIDR address
func validateAddress(addr, familyName, ifaceName string, unitNum int) error {
	if addr == "" {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Empty address on interface %s unit %d family %s", ifaceName, unitNum, familyName),
			"Address must not be empty",
			"Specify a valid IP address in CIDR format",
		)
	}

	// Parse CIDR
	ip, ipnet, err := net.ParseCIDR(addr)
	if err != nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Invalid CIDR address %s on interface %s unit %d family %s", addr, ifaceName, unitNum, familyName),
			fmt.Sprintf("Failed to parse CIDR: %v", err),
			"Use a valid CIDR format like '192.168.1.1/24' or '2001:db8::1/64'",
		)
	}

	// Validate family matches IP version
	switch familyName {
	case "inet":
		if ip.To4() == nil {
			return errors.New(
				errors.ErrCodeConfigValidation,
				fmt.Sprintf("IPv4 address expected for family inet, got %s on interface %s unit %d", addr, ifaceName, unitNum),
				"Family inet requires IPv4 addresses",
				"Use an IPv4 address or change family to inet6 for IPv6",
			)
		}
	case "inet6":
		if ip.To4() != nil {
			return errors.New(
				errors.ErrCodeConfigValidation,
				fmt.Sprintf("IPv6 address expected for family inet6, got %s on interface %s unit %d", addr, ifaceName, unitNum),
				"Family inet6 requires IPv6 addresses",
				"Use an IPv6 address or change family to inet for IPv4",
			)
		}
	}

	// Validate that the address is the network address (optional, but good practice)
	if !ip.Equal(ipnet.IP) {
		// This is just a warning case, we'll allow it but could add logging in the future
		// For now, we'll accept it as valid
	}

	return nil
}

// keys returns the keys of a map as a slice
func keys(m map[string]bool) []string {
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	return result
}

// Validate validates routing options configuration
func (ro *RoutingOptions) Validate() error {
	if ro == nil {
		return nil
	}

	// Validate router-id format if specified
	if ro.RouterID != "" {
		if net.ParseIP(ro.RouterID) == nil {
			return errors.New(
				errors.ErrCodeConfigValidation,
				fmt.Sprintf("Invalid router-id: %s", ro.RouterID),
				"Router ID must be a valid IPv4 address",
				"Use a valid IPv4 address like '192.168.1.1'",
			)
		}
		// Ensure it's IPv4
		if net.ParseIP(ro.RouterID).To4() == nil {
			return errors.New(
				errors.ErrCodeConfigValidation,
				fmt.Sprintf("Router ID must be IPv4: %s", ro.RouterID),
				"Router ID must be an IPv4 address, not IPv6",
				"Use an IPv4 address",
			)
		}
	}

	// Validate autonomous system number
	if ro.AutonomousSystem != 0 {
		if ro.AutonomousSystem < 1 || ro.AutonomousSystem > 4294967295 {
			return errors.New(
				errors.ErrCodeConfigValidation,
				fmt.Sprintf("AS number out of range: %d", ro.AutonomousSystem),
				"AS number must be between 1 and 4294967295",
				"Use a valid AS number",
			)
		}
	}

	// Validate static routes
	for _, sr := range ro.StaticRoutes {
		if err := validateStaticRoute(sr); err != nil {
			return err
		}
	}

	return nil
}

// validateStaticRoute validates a static route
func validateStaticRoute(sr *StaticRoute) error {
	if sr == nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			"Static route is nil",
			"Internal error: static route object is nil",
			"Report this issue to the maintainers",
		)
	}

	// Validate prefix
	if sr.Prefix == "" {
		return errors.New(
			errors.ErrCodeConfigValidation,
			"Static route prefix is empty",
			"Prefix must be specified",
			"Use a valid CIDR prefix like '0.0.0.0/0' or '192.168.0.0/24'",
		)
	}

	_, _, err := net.ParseCIDR(sr.Prefix)
	if err != nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Invalid static route prefix: %s", sr.Prefix),
			fmt.Sprintf("Failed to parse CIDR: %v", err),
			"Use a valid CIDR format",
		)
	}

	// Validate next-hop
	if sr.NextHop == "" {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Static route %s has empty next-hop", sr.Prefix),
			"Next-hop must be specified",
			"Specify a valid next-hop IP address",
		)
	}

	if net.ParseIP(sr.NextHop) == nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Invalid next-hop for static route %s: %s", sr.Prefix, sr.NextHop),
			"Next-hop must be a valid IP address",
			"Use a valid IPv4 or IPv6 address",
		)
	}

	// Validate distance (optional)
	if sr.Distance < 0 || sr.Distance > 255 {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Invalid distance for static route %s: %d", sr.Prefix, sr.Distance),
			"Distance must be between 0 and 255",
			"Use a valid distance value",
		)
	}

	return nil
}

// Validate validates protocol configuration
func (pc *ProtocolConfig) Validate(cfg *Config) error {
	if pc == nil {
		return nil
	}

	// Validate BGP
	if pc.BGP != nil {
		if err := pc.BGP.Validate(cfg); err != nil {
			return err
		}
	}

	// Validate OSPF
	if pc.OSPF != nil {
		if err := pc.OSPF.Validate(cfg); err != nil {
			return err
		}
	}

	return nil
}

// Validate validates BGP configuration
func (bgp *BGPConfig) Validate(cfg *Config) error {
	if bgp == nil {
		return nil
	}

	// Check if AS number is configured
	if cfg.RoutingOptions == nil || cfg.RoutingOptions.AutonomousSystem == 0 {
		return errors.New(
			errors.ErrCodeConfigValidation,
			"BGP configured but autonomous-system not set",
			"BGP requires an autonomous system number",
			"Set 'routing-options autonomous-system <asn>'",
		)
	}

	// Validate groups
	if len(bgp.Groups) == 0 {
		return errors.New(
			errors.ErrCodeConfigValidation,
			"BGP configured but no groups defined",
			"BGP requires at least one group",
			"Add a BGP group using 'set protocols bgp group <name> ...'",
		)
	}

	for groupName, group := range bgp.Groups {
		if err := validateBGPGroup(groupName, group); err != nil {
			return err
		}
	}

	return nil
}

// validateBGPGroup validates a BGP group
func validateBGPGroup(groupName string, group *BGPGroup) error {
	if group == nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("BGP group %s is nil", groupName),
			"Internal error: BGP group object is nil",
			"Report this issue to the maintainers",
		)
	}

	// Validate type
	if group.Type == "" {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("BGP group %s has no type", groupName),
			"BGP group type must be specified",
			"Set 'set protocols bgp group <name> type internal' or 'type external'",
		)
	}

	if group.Type != "internal" && group.Type != "external" {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Invalid BGP group type for %s: %s", groupName, group.Type),
			"BGP group type must be 'internal' or 'external'",
			"Use 'type internal' or 'type external'",
		)
	}

	// Validate neighbors
	if len(group.Neighbors) == 0 {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("BGP group %s has no neighbors", groupName),
			"BGP group must have at least one neighbor",
			"Add a neighbor using 'set protocols bgp group <name> neighbor <ip> peer-as <asn>'",
		)
	}

	for neighborIP, neighbor := range group.Neighbors {
		if err := validateBGPNeighbor(groupName, neighborIP, neighbor); err != nil {
			return err
		}
	}

	return nil
}

// validateBGPNeighbor validates a BGP neighbor
func validateBGPNeighbor(groupName, neighborIP string, neighbor *BGPNeighbor) error {
	if neighbor == nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("BGP neighbor %s in group %s is nil", neighborIP, groupName),
			"Internal error: BGP neighbor object is nil",
			"Report this issue to the maintainers",
		)
	}

	// Validate IP address
	if net.ParseIP(neighborIP) == nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Invalid BGP neighbor IP in group %s: %s", groupName, neighborIP),
			"Neighbor IP must be a valid IP address",
			"Use a valid IPv4 or IPv6 address",
		)
	}

	// Validate peer AS
	if neighbor.PeerAS == 0 {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("BGP neighbor %s in group %s has no peer-as", neighborIP, groupName),
			"Peer AS number must be specified",
			"Set 'set protocols bgp group <name> neighbor <ip> peer-as <asn>'",
		)
	}

	if neighbor.PeerAS < 1 || neighbor.PeerAS > 4294967295 {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Invalid peer AS for neighbor %s in group %s: %d", neighborIP, groupName, neighbor.PeerAS),
			"Peer AS number must be between 1 and 4294967295",
			"Use a valid AS number",
		)
	}

	// Validate local address if specified
	if neighbor.LocalAddress != "" {
		if net.ParseIP(neighbor.LocalAddress) == nil {
			return errors.New(
				errors.ErrCodeConfigValidation,
				fmt.Sprintf("Invalid local address for neighbor %s in group %s: %s", neighborIP, groupName, neighbor.LocalAddress),
				"Local address must be a valid IP address",
				"Use a valid IPv4 or IPv6 address",
			)
		}
	}

	return nil
}

// Validate validates OSPF configuration
func (ospf *OSPFConfig) Validate(cfg *Config) error {
	if ospf == nil {
		return nil
	}

	// Check for router-id (from OSPF config or routing-options)
	routerID := ospf.RouterID
	if routerID == "" && cfg.RoutingOptions != nil {
		routerID = cfg.RoutingOptions.RouterID
	}

	if routerID == "" {
		return errors.New(
			errors.ErrCodeConfigValidation,
			"OSPF configured but no router-id set",
			"OSPF requires a router ID",
			"Set 'routing-options router-id <ip>' or 'protocols ospf router-id <ip>'",
		)
	}

	// Validate router-id format
	if net.ParseIP(routerID) == nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Invalid OSPF router-id: %s", routerID),
			"Router ID must be a valid IPv4 address",
			"Use a valid IPv4 address",
		)
	}

	if net.ParseIP(routerID).To4() == nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("OSPF router-id must be IPv4: %s", routerID),
			"Router ID must be an IPv4 address, not IPv6",
			"Use an IPv4 address",
		)
	}

	// Validate areas
	if len(ospf.Areas) == 0 {
		return errors.New(
			errors.ErrCodeConfigValidation,
			"OSPF configured but no areas defined",
			"OSPF requires at least one area",
			"Add an area using 'set protocols ospf area <area-id> interface <name>'",
		)
	}

	for areaID, area := range ospf.Areas {
		if err := validateOSPFArea(areaID, area, cfg); err != nil {
			return err
		}
	}

	return nil
}

// validateOSPFArea validates an OSPF area
func validateOSPFArea(areaID string, area *OSPFArea, cfg *Config) error {
	if area == nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("OSPF area %s is nil", areaID),
			"Internal error: OSPF area object is nil",
			"Report this issue to the maintainers",
		)
	}

	// Validate area ID format (can be dotted decimal IPv4 or integer)
	parsedIP := net.ParseIP(areaID)
	if parsedIP != nil {
		// If parsed as IP, must be IPv4
		if parsedIP.To4() == nil {
			return errors.New(
				errors.ErrCodeConfigValidation,
				fmt.Sprintf("Invalid OSPF area ID: %s", areaID),
				"Area ID must be in dotted decimal IPv4 format (e.g., 0.0.0.0), not IPv6",
				"Use an IPv4 address or integer format",
			)
		}
	} else {
		// Try parsing as integer
		// Area ID can be 0, 0.0.0.0, etc.
		if areaID != "0" && !regexp.MustCompile(`^\d+$`).MatchString(areaID) {
			return errors.New(
				errors.ErrCodeConfigValidation,
				fmt.Sprintf("Invalid OSPF area ID: %s", areaID),
				"Area ID must be in dotted decimal format (e.g., 0.0.0.0) or integer (e.g., 0)",
				"Use a valid area ID format",
			)
		}
	}

	// Validate interfaces
	if len(area.Interfaces) == 0 {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("OSPF area %s has no interfaces", areaID),
			"OSPF area must have at least one interface",
			"Add an interface using 'set protocols ospf area <area-id> interface <name>'",
		)
	}

	for ifName, ospfIf := range area.Interfaces {
		if err := validateOSPFInterface(areaID, ifName, ospfIf, cfg); err != nil {
			return err
		}
	}

	return nil
}

// validateOSPFInterface validates an OSPF interface
func validateOSPFInterface(areaID, ifName string, ospfIf *OSPFInterface, cfg *Config) error {
	if ospfIf == nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("OSPF interface %s in area %s is nil", ifName, areaID),
			"Internal error: OSPF interface object is nil",
			"Report this issue to the maintainers",
		)
	}

	// Check if interface exists in configuration
	if cfg.Interfaces != nil {
		if _, exists := cfg.Interfaces[ifName]; !exists {
			return errors.New(
				errors.ErrCodeConfigValidation,
				fmt.Sprintf("OSPF references non-existent interface %s in area %s", ifName, areaID),
				"Interface must be defined before being used in OSPF",
				fmt.Sprintf("Add interface configuration for %s", ifName),
			)
		}
	}

	// Validate metric
	if ospfIf.Metric < 0 || ospfIf.Metric > 65535 {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Invalid OSPF metric for interface %s in area %s: %d", ifName, areaID, ospfIf.Metric),
			"OSPF metric must be between 0 and 65535",
			"Use a valid metric value",
		)
	}

	// Validate priority
	if ospfIf.Priority < 0 || ospfIf.Priority > 255 {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Invalid OSPF priority for interface %s in area %s: %d", ifName, areaID, ospfIf.Priority),
			"OSPF priority must be between 0 and 255",
			"Use a valid priority value",
		)
	}

	return nil
}
