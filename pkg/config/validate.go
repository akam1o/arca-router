package config

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
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

	if c.Chassis != nil {
		if err := c.Chassis.Validate(); err != nil {
			return err
		}
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
		if err := c.RoutingOptions.validate(c); err != nil {
			return err
		}
	}

	for name, instance := range c.RoutingInstances {
		if err := validateRoutingInstance(c, name, instance); err != nil {
			return err
		}
	}

	// Validate protocols
	if c.Protocols != nil {
		if err := c.Protocols.Validate(c); err != nil {
			return err
		}
	}

	if c.PolicyOptions != nil {
		if err := c.PolicyOptions.Validate(); err != nil {
			return err
		}
	}

	if c.ClassOfService != nil {
		if err := c.ClassOfService.Validate(); err != nil {
			return err
		}
		if err := c.validateClassOfServiceInterfaceReferences(); err != nil {
			return err
		}
	}

	return nil
}

// Validate validates policy-options configuration.
func (po *PolicyOptions) Validate() error {
	if po == nil {
		return nil
	}
	for name, list := range po.PrefixLists {
		if strings.TrimSpace(name) == "" {
			return errors.New(errors.ErrCodeConfigValidation, "Policy prefix-list name is empty", "Prefix-list names must be specified", "Use a non-empty policy-options prefix-list name")
		}
		if list == nil {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Policy prefix-list %s is nil", name), "Prefix-list configuration is invalid", "Remove or recreate the prefix-list")
		}
		for _, prefix := range list.Prefixes {
			if _, _, err := net.ParseCIDR(prefix); err != nil {
				return errors.New(
					errors.ErrCodeConfigValidation,
					fmt.Sprintf("Policy prefix-list %s has invalid prefix %q", name, prefix),
					"Prefix-list entries must be valid IPv4 or IPv6 CIDR prefixes",
					"Use a value like 192.0.2.0/24 or 2001:db8::/32",
				)
			}
		}
	}
	for name, statement := range po.PolicyStatements {
		if strings.TrimSpace(name) == "" {
			return errors.New(errors.ErrCodeConfigValidation, "Policy statement name is empty", "Policy statement names must be specified", "Use a non-empty policy-options policy-statement name")
		}
		if statement == nil {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Policy statement %s is nil", name), "Policy statement configuration is invalid", "Remove or recreate the policy-statement")
		}
		if err := po.validatePolicyStatement(name, statement); err != nil {
			return err
		}
	}
	return nil
}

func (po *PolicyOptions) validatePolicyStatement(name string, statement *PolicyStatement) error {
	for _, term := range statement.Terms {
		if term == nil {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Policy statement %s has a nil term", name), "Policy terms must be valid", "Remove or recreate the policy term")
		}
		if strings.TrimSpace(term.Name) == "" {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Policy statement %s has an empty term name", name), "Policy terms must have names", "Use a non-empty policy term name")
		}
		if term.From != nil {
			for _, listName := range term.From.PrefixLists {
				if strings.TrimSpace(listName) == "" {
					return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Policy statement %s term %s references an empty prefix-list", name, term.Name), "Prefix-list references must be specified", "Use a configured policy-options prefix-list name")
				}
				if po.PrefixLists == nil || po.PrefixLists[listName] == nil {
					return errors.New(
						errors.ErrCodeConfigValidation,
						fmt.Sprintf("Policy statement %s term %s references unknown prefix-list %s", name, term.Name, listName),
						"Referenced prefix-list must exist before it is used",
						fmt.Sprintf("Create policy-options prefix-list %s", listName),
					)
				}
			}
			if term.From.Protocol != "" {
				if err := validateProtocol(term.From.Protocol); err != nil {
					return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Policy statement %s term %s has invalid protocol %q", name, term.Name, term.From.Protocol), err.Error(), "Use one of bgp, ospf, ospf3, static, connected, direct, kernel, or rip")
				}
			}
			if term.From.Neighbor != "" && net.ParseIP(term.From.Neighbor) == nil {
				return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Policy statement %s term %s has invalid neighbor %q", name, term.Name, term.From.Neighbor), "Neighbor matches must be valid IP addresses", "Use a valid IPv4 or IPv6 address")
			}
			if term.From.ASPath != "" {
				if _, err := regexp.Compile(term.From.ASPath); err != nil {
					return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Policy statement %s term %s has invalid as-path %q", name, term.Name, term.From.ASPath), "AS path match must be a valid regular expression", "Use a valid AS path regular expression")
				}
			}
		}
		if term.Then != nil && term.Then.Community != "" {
			if err := validateCommunity(term.Then.Community); err != nil {
				return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Policy statement %s term %s has invalid community %q", name, term.Name, term.Then.Community), err.Error(), "Use ASN:number or a supported well-known community")
			}
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

	if s.Services != nil && s.Services.WebUI != nil {
		if err := validateWebUI(s.Services.WebUI); err != nil {
			return err
		}
	}
	if s.Services != nil && s.Services.Prometheus != nil {
		if err := validatePrometheus(s.Services.Prometheus); err != nil {
			return err
		}
	}
	if s.Services != nil && s.Services.SNMP != nil {
		if err := validateSNMP(s.Services.SNMP); err != nil {
			return err
		}
	}

	return nil
}

func validateWebUI(web *WebUIConfig) error {
	if web.Port < 0 || web.Port > 65535 {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Invalid web-ui port: %d", web.Port),
			"Web UI port must be between 0 and 65535",
			"Use a valid TCP port",
		)
	}
	if web.ListenAddress != "" && net.ParseIP(web.ListenAddress) == nil && web.ListenAddress != "localhost" {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Invalid web-ui listen-address: %s", web.ListenAddress),
			"Web UI listen-address must be an IP address or localhost",
			"Use a valid listen address",
		)
	}
	return nil
}

func validatePrometheus(prometheus *PrometheusConfig) error {
	if prometheus.Port < 0 || prometheus.Port > 65535 {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Invalid prometheus port: %d", prometheus.Port),
			"Prometheus port must be between 0 and 65535",
			"Use a valid TCP port",
		)
	}
	if prometheus.ListenAddress != "" && net.ParseIP(prometheus.ListenAddress) == nil && prometheus.ListenAddress != "localhost" {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Invalid prometheus listen-address: %s", prometheus.ListenAddress),
			"Prometheus listen-address must be an IP address or localhost",
			"Use a valid listen address",
		)
	}
	return nil
}

func validateSNMP(snmp *SNMPConfig) error {
	if snmp.Port < 0 || snmp.Port > 65535 {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Invalid snmp port: %d", snmp.Port),
			"SNMP port must be between 0 and 65535",
			"Use a valid UDP port",
		)
	}
	if snmp.ListenAddress != "" && net.ParseIP(snmp.ListenAddress) == nil && snmp.ListenAddress != "localhost" {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Invalid snmp listen-address: %s", snmp.ListenAddress),
			"SNMP listen-address must be an IP address or localhost",
			"Use a valid listen address",
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

func validateConfiguredInterfaceReference(cfg *Config, context, ifName string) error {
	if err := validateInterfaceName(ifName); err != nil {
		return err
	}
	if cfg == nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("%s references interface %s without a configuration context", context, ifName),
			"Internal validation error",
			"Report this issue to the maintainers",
		)
	}
	if _, exists := cfg.Interfaces[ifName]; !exists {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("%s references non-existent interface %s", context, ifName),
			"Interface must be defined before it is referenced",
			fmt.Sprintf("Add interface configuration for %s", ifName),
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

	// ParseCIDR validation is enough here (network-address enforcement is optional).
	_ = ipnet

	return nil
}

// Validate validates chassis configuration.
func (c *ChassisConfig) Validate() error {
	if c == nil || c.Cluster == nil {
		return nil
	}
	for name, node := range c.Cluster.Nodes {
		if node == nil {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Cluster node %s is nil", name), "Cluster node is invalid", "Remove or recreate the node")
		}
		if node.Address != "" && net.ParseIP(node.Address) == nil {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Invalid cluster node address for %s: %s", name, node.Address), "Cluster node address must be a valid IP address", "Use a valid IPv4 or IPv6 address")
		}
		if node.Priority < 0 || node.Priority > 255 {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Invalid cluster node priority for %s: %d", name, node.Priority), "Cluster node priority must be between 0 and 255", "Use a valid priority value")
		}
	}
	if c.Cluster.Sync != nil && c.Cluster.Sync.Etcd != nil {
		for _, endpoint := range c.Cluster.Sync.Etcd.Endpoints {
			if strings.TrimSpace(endpoint) == "" {
				return errors.New(errors.ErrCodeConfigValidation, "Empty cluster etcd endpoint", "etcd endpoint must not be empty", "Set a valid endpoint")
			}
		}
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
	return ro.validate(nil)
}

func (ro *RoutingOptions) validate(cfg *Config) error {
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
		// ro.AutonomousSystem is uint32; upper bound is implied by the type.
		if ro.AutonomousSystem < 1 {
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
		if err := validateStaticRoute(cfg, sr); err != nil {
			return err
		}
	}

	return nil
}

// validateStaticRoute validates a static route
func validateStaticRoute(cfg *Config, sr *StaticRoute) error {
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

	_, prefixNet, err := net.ParseCIDR(sr.Prefix)
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

	nextHopIP := net.ParseIP(sr.NextHop)
	if nextHopIP == nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Invalid next-hop for static route %s: %s", sr.Prefix, sr.NextHop),
			"Next-hop must be a valid IP address",
			"Use a valid IPv4 or IPv6 address",
		)
	}

	if prefixNet.IP.To4() == nil && nextHopIP.To4() != nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Static route %s has IPv4 next-hop for IPv6 prefix: %s", sr.Prefix, sr.NextHop),
			"Static route next-hop family must match the prefix family",
			"Use an IPv6 next-hop for IPv6 routes",
		)
	}
	if prefixNet.IP.To4() != nil && nextHopIP.To4() == nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Static route %s has IPv6 next-hop for IPv4 prefix: %s", sr.Prefix, sr.NextHop),
			"Static route next-hop family must match the prefix family",
			"Use an IPv4 next-hop for IPv4 routes",
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

	if sr.BFDProfile != "" {
		if err := validateBFDProfileReference(cfg, fmt.Sprintf("Static route %s", sr.Prefix), sr.BFDProfile); err != nil {
			return err
		}
	}
	if sr.BFDSource != "" {
		sourceIP := net.ParseIP(sr.BFDSource)
		if sourceIP == nil {
			return errors.New(
				errors.ErrCodeConfigValidation,
				fmt.Sprintf("Invalid BFD source for static route %s: %s", sr.Prefix, sr.BFDSource),
				"BFD source must be a valid IP address",
				"Use a valid IPv4 or IPv6 source address",
			)
		}
		if (nextHopIP.To4() == nil) != (sourceIP.To4() == nil) {
			return errors.New(
				errors.ErrCodeConfigValidation,
				fmt.Sprintf("Static route %s has BFD source family mismatch: %s", sr.Prefix, sr.BFDSource),
				"BFD source address family must match the next-hop family",
				"Use a BFD source address with the same IP family as the next-hop",
			)
		}
	}
	if (sr.BFDProfile != "" || sr.BFDSource != "" || sr.BFDMultihop) && !sr.BFD {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Static route %s has BFD options without BFD enabled", sr.Prefix),
			"Static route BFD options require BFD to be enabled",
			"Add 'bfd' before static route BFD options",
		)
	}
	if sr.Distance > 0 && (sr.BFD || sr.BFDProfile != "" || sr.BFDSource != "" || sr.BFDMultihop) {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Static route %s combines distance with BFD", sr.Prefix),
			"FRR static route BFD monitoring does not support administrative distance in the documented command form",
			"Remove distance or BFD from this static route",
		)
	}

	return nil
}

func validateRoutingInstance(cfg *Config, name string, instance *RoutingInstance) error {
	if instance == nil {
		return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Routing instance %s is nil", name), "Routing instance is invalid", "Remove or recreate the routing instance")
	}
	if instance.InstanceType != "" && instance.InstanceType != "vrf" {
		return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Unsupported routing-instance type for %s: %s", name, instance.InstanceType), "Only instance-type vrf is supported in v0.6", "Use 'set routing-instances <name> instance-type vrf'")
	}
	if instance.RouteDistinguisher != "" && !regexp.MustCompile(`^\d+:\d+$`).MatchString(instance.RouteDistinguisher) {
		return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Invalid route-distinguisher for %s: %s", name, instance.RouteDistinguisher), "Route distinguisher must use ASN:number format", "Use a value like 65000:100")
	}
	importTargetCount := 0
	exportTargetCount := 0
	if instance.VRFTarget != "" {
		if err := validateVRFTargetValue(fmt.Sprintf("routing-instance %s vrf-target", name), instance.VRFTarget); err != nil {
			return err
		}
		importTargetCount++
		exportTargetCount++
	}
	for _, ifName := range instance.Interfaces {
		if err := validateConfiguredInterfaceReference(cfg, fmt.Sprintf("Routing instance %s", name), ifName); err != nil {
			return err
		}
	}
	for _, target := range instance.VRFTargetImport {
		if err := validateVRFTargetValue(fmt.Sprintf("routing-instance %s vrf-target import", name), target); err != nil {
			return err
		}
		importTargetCount++
	}
	for _, target := range instance.VRFTargetExport {
		if err := validateVRFTargetValue(fmt.Sprintf("routing-instance %s vrf-target export", name), target); err != nil {
			return err
		}
		exportTargetCount++
	}
	for _, policyName := range instance.VRFImport {
		if err := validatePolicyStatementReference(cfg, fmt.Sprintf("Routing instance %s vrf-import", name), policyName); err != nil {
			return err
		}
	}
	for _, policyName := range instance.VRFExport {
		if err := validatePolicyStatementReference(cfg, fmt.Sprintf("Routing instance %s vrf-export", name), policyName); err != nil {
			return err
		}
	}
	if len(instance.VRFImport) > 0 && importTargetCount == 0 {
		return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Routing instance %s vrf-import requires an import vrf-target", name), "VRF import policy requires at least one import target", "Configure 'vrf-target import target:<asn>:<number>' or a shared 'vrf-target'")
	}
	if len(instance.VRFExport) > 0 && exportTargetCount == 0 {
		return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Routing instance %s vrf-export requires an export vrf-target", name), "VRF export policy requires at least one export target", "Configure 'vrf-target export target:<asn>:<number>' or a shared 'vrf-target'")
	}
	if exportTargetCount > 0 && instance.RouteDistinguisher == "" {
		return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Routing instance %s route-distinguisher is required for VPN export", name), "VPN export requires a route distinguisher", "Configure 'route-distinguisher <asn>:<number>'")
	}
	if (importTargetCount > 0 || exportTargetCount > 0 || len(instance.VRFImport) > 0 || len(instance.VRFExport) > 0) &&
		(cfg.RoutingOptions == nil || cfg.RoutingOptions.AutonomousSystem == 0) {
		return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Routing instance %s routing-options autonomous-system is required for VPN import/export", name), "VPN import/export requires a local autonomous-system", "Configure 'set routing-options autonomous-system <asn>'")
	}
	return nil
}

func validateVRFTargetValue(context, target string) error {
	if !regexp.MustCompile(`^target:\d+:\d+$`).MatchString(target) {
		return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Invalid %s: %s", context, target), "VRF target must use target:ASN:number format", "Use a value like target:65000:100")
	}
	return nil
}

func validatePolicyStatementReference(cfg *Config, context, policyName string) error {
	if strings.TrimSpace(policyName) == "" {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("%s references an empty policy-statement", context),
			"Policy statement name must be specified",
			"Use a configured policy-options policy-statement name",
		)
	}
	if cfg == nil || cfg.PolicyOptions == nil || cfg.PolicyOptions.PolicyStatements == nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("%s references unknown policy-statement %s", context, policyName),
			"Referenced policy-statement must exist before it is used",
			fmt.Sprintf("Create policy-options policy-statement %s", policyName),
		)
	}
	if _, ok := cfg.PolicyOptions.PolicyStatements[policyName]; !ok {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("%s references unknown policy-statement %s", context, policyName),
			"Referenced policy-statement must exist before it is used",
			fmt.Sprintf("Create policy-options policy-statement %s", policyName),
		)
	}
	return nil
}

// Validate validates protocol configuration
func (pc *ProtocolConfig) Validate(cfg *Config) error {
	if pc == nil {
		return nil
	}

	if pc.BFD != nil {
		if err := pc.BFD.Validate(cfg); err != nil {
			return err
		}
	}

	// Validate BGP
	if pc.BGP != nil {
		if err := pc.BGP.Validate(cfg); err != nil {
			return err
		}
	}

	if pc.EVPN != nil {
		if err := pc.EVPN.Validate(cfg); err != nil {
			return err
		}
	}

	// Validate OSPF
	if pc.OSPF != nil {
		if err := pc.OSPF.Validate(cfg); err != nil {
			return err
		}
	}

	// Validate OSPFv3
	if pc.OSPF3 != nil {
		if err := pc.OSPF3.ValidateOSPF3(cfg); err != nil {
			return err
		}
	}

	if pc.MPLS != nil {
		for _, ifName := range pc.MPLS.Interfaces {
			if err := validateConfiguredInterfaceReference(cfg, "MPLS", ifName); err != nil {
				return err
			}
		}
	}

	if pc.VRRP != nil {
		if err := pc.VRRP.Validate(); err != nil {
			return err
		}
		for name, group := range pc.VRRP.Groups {
			if group != nil && group.Interface != "" {
				if err := validateConfiguredInterfaceReference(cfg, fmt.Sprintf("VRRP group %s", name), group.Interface); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// Validate validates EVPN/VXLAN overlay configuration.
func (e *EVPNConfig) Validate(cfg *Config) error {
	if e == nil {
		return nil
	}
	for id, vni := range e.VNIs {
		if err := validateEVPNVNI(cfg, id, vni); err != nil {
			return err
		}
	}
	return nil
}

func validateEVPNVNI(cfg *Config, id int, vni *EVPNVNI) error {
	context := fmt.Sprintf("EVPN VNI %d", id)
	if vni == nil {
		return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("%s is nil", context), "EVPN VNI configuration is invalid", "Remove or recreate the EVPN VNI")
	}
	if id < 1 || id > 16777215 {
		return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Invalid %s", context), "EVPN VNI must be between 1 and 16777215", "Use a valid VXLAN VNI")
	}
	if vni.VNI != 0 && vni.VNI != id {
		return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("%s has mismatched VNI value %d", context, vni.VNI), "EVPN VNI map key and value must match", "Use a consistent EVPN VNI value")
	}
	if vni.Type != "l2" && vni.Type != "l3" {
		return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("%s has invalid type: %s", context, vni.Type), "EVPN VNI type must be l2 or l3", fmt.Sprintf("Set 'protocols evpn vni %d type l2' or 'type l3'", id))
	}
	switch vni.Type {
	case "l2":
		if strings.TrimSpace(vni.BridgeDomain) == "" {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("%s is missing bridge-domain", context), "L2 EVPN VNIs require a bridge-domain", fmt.Sprintf("Set 'protocols evpn vni %d bridge-domain <name>'", id))
		}
		if vni.RoutingInstance != "" {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("%s has routing-instance on an L2 VNI", context), "routing-instance is only valid for L3 EVPN VNIs", "Remove routing-instance or set type l3")
		}
	case "l3":
		if strings.TrimSpace(vni.RoutingInstance) == "" {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("%s is missing routing-instance", context), "L3 EVPN VNIs require a routing-instance", fmt.Sprintf("Set 'protocols evpn vni %d routing-instance <name>'", id))
		}
		if vni.BridgeDomain != "" {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("%s has bridge-domain on an L3 VNI", context), "bridge-domain is only valid for L2 EVPN VNIs", "Remove bridge-domain or set type l2")
		}
		if vni.VLANID != 0 {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("%s has vlan-id on an L3 VNI", context), "vlan-id is only valid for L2 EVPN VNIs", "Remove vlan-id or set type l2")
		}
		if err := validateRoutingInstanceReference(cfg, context, vni.RoutingInstance); err != nil {
			return err
		}
	}
	if vni.VLANID != 0 && (vni.VLANID < 1 || vni.VLANID > 4094) {
		return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("%s has invalid vlan-id: %d", context, vni.VLANID), "EVPN VLAN ID must be between 1 and 4094", "Use a valid VLAN ID")
	}
	if vni.RouteDistinguisher != "" && !regexp.MustCompile(`^\d+:\d+$`).MatchString(vni.RouteDistinguisher) {
		return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Invalid %s route-distinguisher: %s", context, vni.RouteDistinguisher), "EVPN route distinguisher must use ASN:number format", "Use a value like 65000:100")
	}
	if vni.VRFTarget != "" {
		if err := validateVRFTargetValue(fmt.Sprintf("%s vrf-target", context), vni.VRFTarget); err != nil {
			return err
		}
	}
	for _, target := range vni.VRFTargetImport {
		if err := validateVRFTargetValue(fmt.Sprintf("%s vrf-target import", context), target); err != nil {
			return err
		}
	}
	for _, target := range vni.VRFTargetExport {
		if err := validateVRFTargetValue(fmt.Sprintf("%s vrf-target export", context), target); err != nil {
			return err
		}
	}
	if vni.SourceInterface != "" {
		if err := validateConfiguredInterfaceReference(cfg, context, vni.SourceInterface); err != nil {
			return err
		}
	}
	if vni.SourceAddress != "" && net.ParseIP(vni.SourceAddress) == nil {
		return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("%s has invalid source-address: %s", context, vni.SourceAddress), "EVPN source-address must be a valid IP address", "Use a valid IPv4 or IPv6 address")
	}
	if vni.MulticastGroup != "" {
		groupIP := net.ParseIP(vni.MulticastGroup)
		if groupIP == nil || !groupIP.IsMulticast() {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("%s has invalid multicast-group: %s", context, vni.MulticastGroup), "EVPN multicast-group must be a valid multicast IP address", "Use an IPv4 224.0.0.0/4 or IPv6 ff00::/8 group")
		}
	}
	if vni.RemoteVTEP != "" {
		remoteIP := net.ParseIP(vni.RemoteVTEP)
		if remoteIP == nil || remoteIP.IsMulticast() {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("%s has invalid remote-vtep: %s", context, vni.RemoteVTEP), "EVPN remote-vtep must be a valid unicast IP address", "Use the remote VTEP IPv4 or IPv6 address")
		}
	}
	if vni.MulticastGroup != "" && vni.RemoteVTEP != "" {
		return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("%s has both multicast-group and remote-vtep", context), "EVPN VNI dataplane endpoint must be multicast or unicast", "Set either multicast-group or remote-vtep, not both")
	}
	return nil
}

func validateRoutingInstanceReference(cfg *Config, context, name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("%s references an empty routing-instance", context), "Routing instance name must be specified", "Use a configured routing-instance name")
	}
	if cfg == nil || cfg.RoutingInstances == nil {
		return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("%s references unknown routing-instance %s", context, name), "Referenced routing-instance must exist before it is used", fmt.Sprintf("Create routing-instances %s", name))
	}
	if _, ok := cfg.RoutingInstances[name]; !ok {
		return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("%s references unknown routing-instance %s", context, name), "Referenced routing-instance must exist before it is used", fmt.Sprintf("Create routing-instances %s", name))
	}
	return nil
}

// Validate validates BFD configuration.
func (b *BFDConfig) Validate(cfg *Config) error {
	if b == nil {
		return nil
	}
	for name, profile := range b.Profiles {
		if profile == nil {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("BFD profile %s is nil", name), "BFD profile is invalid", "Remove or recreate the profile")
		}
		if err := validateBFDTimers(fmt.Sprintf("BFD profile %s", name), profile.DetectMultiplier, profile.ReceiveInterval, profile.TransmitInterval); err != nil {
			return err
		}
	}
	for address, peer := range b.Peers {
		if peer == nil {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("BFD peer %s is nil", address), "BFD peer is invalid", "Remove or recreate the peer")
		}
		peerAddress := peer.Address
		if peerAddress == "" {
			peerAddress = address
		}
		if net.ParseIP(peerAddress) == nil {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Invalid BFD peer address: %s", peerAddress), "BFD peer address must be a valid IP address", "Use a valid IPv4 or IPv6 address")
		}
		if peer.LocalAddress != "" && net.ParseIP(peer.LocalAddress) == nil {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Invalid BFD local-address for %s: %s", peerAddress, peer.LocalAddress), "BFD local-address must be a valid IP address", "Use a valid IPv4 or IPv6 address")
		}
		if peer.Interface != "" {
			if err := validateConfiguredInterfaceReference(cfg, fmt.Sprintf("BFD peer %s", peerAddress), peer.Interface); err != nil {
				return err
			}
		}
		if peer.VRF != "" && peer.VRF != "default" {
			if cfg == nil || cfg.RoutingInstances == nil || cfg.RoutingInstances[peer.VRF] == nil {
				return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("BFD peer %s references unknown routing-instance %s", peerAddress, peer.VRF), "BFD peer VRF must reference an existing routing instance or default", fmt.Sprintf("Create routing-instances %s or use default", peer.VRF))
			}
		}
		if peer.Profile != "" && b.Profiles[peer.Profile] == nil {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("BFD peer %s references unknown profile %s", peerAddress, peer.Profile), "BFD peer profile must be defined before it is referenced", fmt.Sprintf("Create protocols bfd profile %s", peer.Profile))
		}
		if peer.Multihop && peer.EchoMode {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("BFD peer %s enables echo-mode with multihop", peerAddress), "FRR BFD echo-mode is not supported on multihop sessions", "Remove echo-mode or multihop from the peer")
		}
		if peer.Multihop && peer.Profile != "" && b.Profiles[peer.Profile] != nil && b.Profiles[peer.Profile].EchoMode {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("BFD peer %s uses echo-mode profile %s with multihop", peerAddress, peer.Profile), "FRR BFD echo-mode is not supported on multihop sessions", "Use a profile without echo-mode for multihop peers")
		}
		if err := validateBFDTimers(fmt.Sprintf("BFD peer %s", peerAddress), peer.DetectMultiplier, peer.ReceiveInterval, peer.TransmitInterval); err != nil {
			return err
		}
	}
	return nil
}

func validateBFDTimers(context string, detectMultiplier, receiveInterval, transmitInterval int) error {
	if detectMultiplier < 0 || detectMultiplier > 255 || detectMultiplier == 1 {
		return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("%s has invalid detect-multiplier: %d", context, detectMultiplier), "BFD detect-multiplier must be omitted or between 2 and 255", "Use detect-multiplier 3 for common deployments")
	}
	if receiveInterval < 0 || receiveInterval > 60000 || (receiveInterval > 0 && receiveInterval < 10) {
		return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("%s has invalid receive-interval: %d", context, receiveInterval), "BFD receive-interval must be omitted or between 10 and 60000 milliseconds", "Use receive-interval 300 for common deployments")
	}
	if transmitInterval < 0 || transmitInterval > 60000 || (transmitInterval > 0 && transmitInterval < 10) {
		return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("%s has invalid transmit-interval: %d", context, transmitInterval), "BFD transmit-interval must be omitted or between 10 and 60000 milliseconds", "Use transmit-interval 300 for common deployments")
	}
	return nil
}

// Validate validates VRRP configuration.
func (v *VRRPConfig) Validate() error {
	for name, group := range v.Groups {
		if group == nil {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("VRRP group %s is nil", name), "VRRP group is invalid", "Remove or recreate the group")
		}
		id, err := strconv.Atoi(name)
		if err != nil || id < 1 || id > 255 {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Invalid VRRP group id: %s", name), "VRRP group id must be numeric and between 1 and 255", "Use a valid VRRP group id")
		}
		if group.Interface != "" {
			if err := validateInterfaceName(group.Interface); err != nil {
				return err
			}
		}
		if group.VirtualAddress != "" && net.ParseIP(group.VirtualAddress) == nil {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Invalid VRRP virtual address for %s: %s", name, group.VirtualAddress), "VRRP virtual-address must be a valid IP address", "Use a valid IPv4 or IPv6 address")
		}
		if group.Priority < 0 || group.Priority > 254 {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Invalid VRRP priority for %s: %d", name, group.Priority), "VRRP priority must be between 1 and 254 when configured", "Use a valid priority")
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
		if err := validateBGPGroup(cfg, groupName, group); err != nil {
			return err
		}
	}

	return nil
}

// validateBGPGroup validates a BGP group
func validateBGPGroup(cfg *Config, groupName string, group *BGPGroup) error {
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
		if err := validateBGPNeighbor(cfg, groupName, neighborIP, neighbor); err != nil {
			return err
		}
	}
	if group.Import != "" {
		if err := validatePolicyStatementReference(cfg, fmt.Sprintf("BGP group %s import", groupName), group.Import); err != nil {
			return err
		}
	}
	if group.Export != "" {
		if err := validatePolicyStatementReference(cfg, fmt.Sprintf("BGP group %s export", groupName), group.Export); err != nil {
			return err
		}
	}

	return nil
}

// validateBGPNeighbor validates a BGP neighbor
func validateBGPNeighbor(cfg *Config, groupName, neighborIP string, neighbor *BGPNeighbor) error {
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

	// neighbor.PeerAS is uint32; upper bound is implied by the type.
	if neighbor.PeerAS < 1 {
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

	if neighbor.BFDProfile != "" {
		if err := validateBFDProfileReference(cfg, fmt.Sprintf("BGP neighbor %s in group %s", neighborIP, groupName), neighbor.BFDProfile); err != nil {
			return err
		}
	}

	return nil
}

// Validate validates OSPF configuration
func (ospf *OSPFConfig) Validate(cfg *Config) error {
	return ospf.validate(cfg, "OSPF", "ospf", true)
}

// ValidateOSPF3 validates OSPFv3 configuration.
func (ospf *OSPFConfig) ValidateOSPF3(cfg *Config) error {
	return ospf.validate(cfg, "OSPF3", "ospf3", false)
}

func (ospf *OSPFConfig) validate(cfg *Config, protocolLabel, protocolCommand string, requireRouterID bool) error {
	if ospf == nil {
		return nil
	}

	// Check for router-id (from OSPF config or routing-options)
	routerID := ospf.RouterID
	if routerID == "" && cfg.RoutingOptions != nil {
		routerID = cfg.RoutingOptions.RouterID
	}

	if routerID == "" && requireRouterID {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("%s configured but no router-id set", protocolLabel),
			fmt.Sprintf("%s requires a router ID", protocolLabel),
			fmt.Sprintf("Set 'routing-options router-id <ip>' or 'protocols %s router-id <ip>'", protocolCommand),
		)
	}

	// Validate router-id format
	if routerID != "" && net.ParseIP(routerID) == nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Invalid %s router-id: %s", protocolLabel, routerID),
			"Router ID must be a valid IPv4 address",
			"Use a valid IPv4 address",
		)
	}

	if routerID != "" && net.ParseIP(routerID).To4() == nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("%s router-id must be IPv4: %s", protocolLabel, routerID),
			"Router ID must be an IPv4 address, not IPv6",
			"Use an IPv4 address",
		)
	}

	// Validate areas
	if len(ospf.Areas) == 0 {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("%s configured but no areas defined", protocolLabel),
			fmt.Sprintf("%s requires at least one area", protocolLabel),
			fmt.Sprintf("Add an area using 'set protocols %s area <area-id> interface <name>'", protocolCommand),
		)
	}

	for areaID, area := range ospf.Areas {
		if err := validateOSPFArea(protocolLabel, protocolCommand, areaID, area, cfg); err != nil {
			return err
		}
	}

	return nil
}

// validateOSPFArea validates an OSPF area
func validateOSPFArea(protocolLabel, protocolCommand, areaID string, area *OSPFArea, cfg *Config) error {
	if area == nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("%s area %s is nil", protocolLabel, areaID),
			fmt.Sprintf("Internal error: %s area object is nil", protocolLabel),
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
				fmt.Sprintf("Invalid %s area ID: %s", protocolLabel, areaID),
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
				fmt.Sprintf("Invalid %s area ID: %s", protocolLabel, areaID),
				"Area ID must be in dotted decimal format (e.g., 0.0.0.0) or integer (e.g., 0)",
				"Use a valid area ID format",
			)
		}
	}

	// Validate interfaces
	if len(area.Interfaces) == 0 {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("%s area %s has no interfaces", protocolLabel, areaID),
			fmt.Sprintf("%s area must have at least one interface", protocolLabel),
			fmt.Sprintf("Add an interface using 'set protocols %s area <area-id> interface <name>'", protocolCommand),
		)
	}

	for ifName, ospfIf := range area.Interfaces {
		if err := validateOSPFInterface(protocolLabel, areaID, ifName, ospfIf, cfg); err != nil {
			return err
		}
	}

	return nil
}

// validateOSPFInterface validates an OSPF interface
func validateOSPFInterface(protocolLabel, areaID, ifName string, ospfIf *OSPFInterface, cfg *Config) error {
	if ospfIf == nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("%s interface %s in area %s is nil", protocolLabel, ifName, areaID),
			fmt.Sprintf("Internal error: %s interface object is nil", protocolLabel),
			"Report this issue to the maintainers",
		)
	}

	if err := validateConfiguredInterfaceReference(cfg, fmt.Sprintf("%s area %s", protocolLabel, areaID), ifName); err != nil {
		return err
	}

	// Validate metric
	if ospfIf.Metric < 0 || ospfIf.Metric > 65535 {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Invalid %s metric for interface %s in area %s: %d", protocolLabel, ifName, areaID, ospfIf.Metric),
			fmt.Sprintf("%s metric must be between 0 and 65535", protocolLabel),
			"Use a valid metric value",
		)
	}

	// Validate priority
	if ospfIf.Priority < 0 || ospfIf.Priority > 255 {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("Invalid %s priority for interface %s in area %s: %d", protocolLabel, ifName, areaID, ospfIf.Priority),
			fmt.Sprintf("%s priority must be between 0 and 255", protocolLabel),
			"Use a valid priority value",
		)
	}

	if ospfIf.BFDProfile != "" {
		if err := validateBFDProfileReference(cfg, fmt.Sprintf("%s interface %s in area %s", protocolLabel, ifName, areaID), ospfIf.BFDProfile); err != nil {
			return err
		}
	}

	return nil
}

func validateBFDProfileReference(cfg *Config, context, profileName string) error {
	if strings.TrimSpace(profileName) == "" {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("%s references an empty BFD profile", context),
			"BFD profile reference must not be empty",
			"Use a configured protocols bfd profile name",
		)
	}
	if cfg == nil || cfg.Protocols == nil || cfg.Protocols.BFD == nil || cfg.Protocols.BFD.Profiles == nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("%s references BFD profile %s but no BFD profiles are configured", context, profileName),
			"BFD profile must be defined before it is referenced",
			fmt.Sprintf("Create protocols bfd profile %s", profileName),
		)
	}
	if cfg.Protocols.BFD.Profiles[profileName] == nil {
		return errors.New(
			errors.ErrCodeConfigValidation,
			fmt.Sprintf("%s references unknown BFD profile %s", context, profileName),
			"BFD profile must be defined before it is referenced",
			fmt.Sprintf("Create protocols bfd profile %s", profileName),
		)
	}
	return nil
}

// Validate validates class-of-service configuration.
func (c *ClassOfServiceConfig) Validate() error {
	for name, fc := range c.ForwardingClasses {
		if fc == nil {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Forwarding class %s is nil", name), "Forwarding class is invalid", "Remove or recreate the forwarding class")
		}
		if fc.Queue < 0 || fc.Queue > 7 {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Invalid queue for forwarding class %s: %d", name, fc.Queue), "Forwarding class queue must be between 0 and 7", "Use a valid queue number")
		}
	}
	for name, profile := range c.TrafficControlProfiles {
		if profile == nil {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Traffic control profile %s is nil", name), "Traffic control profile is invalid", "Remove or recreate the profile")
		}
	}
	for ifName, iface := range c.Interfaces {
		if err := validateInterfaceName(ifName); err != nil {
			return err
		}
		if iface == nil {
			return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Class-of-service interface %s is nil", ifName), "Class-of-service interface is invalid", "Remove or recreate the interface binding")
		}
		if iface.OutputTrafficControlProfile != "" {
			if _, ok := c.TrafficControlProfiles[iface.OutputTrafficControlProfile]; !ok {
				return errors.New(errors.ErrCodeConfigValidation, fmt.Sprintf("Class-of-service interface %s references unknown profile %s", ifName, iface.OutputTrafficControlProfile), "Referenced traffic-control-profile must exist", "Create the profile before binding it to an interface")
			}
		}
	}
	return nil
}

func (c *Config) validateClassOfServiceInterfaceReferences() error {
	for ifName := range c.ClassOfService.Interfaces {
		if err := validateConfiguredInterfaceReference(c, "Class-of-service", ifName); err != nil {
			return err
		}
	}
	return nil
}
