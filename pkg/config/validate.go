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
	if c.System != nil {
		if err := c.System.Validate(); err != nil {
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

	return nil
}

// Validate validates system configuration
func (s *SystemConfig) Validate() error {
	if s.HostName == "" {
		return errors.New(
			errors.ErrCodeConfigValidation,
			"System hostname is empty",
			"The system hostname must be specified",
			"Set a valid hostname using 'set system host-name <name>'",
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
