package config

// Config represents the complete router configuration
type Config struct {
	// System holds system-level configuration
	System *SystemConfig `json:"system,omitempty"`

	// Interfaces holds interface configuration
	Interfaces map[string]*Interface `json:"interfaces,omitempty"`
}

// SystemConfig represents system-level settings
// Note: JSON tags use kebab-case to align with Junos-style naming
type SystemConfig struct {
	// HostName is the router's hostname
	HostName string `json:"host-name,omitempty"`
}

// Interface represents a logical interface configuration
type Interface struct {
	// Description is a human-readable description
	Description string `json:"description,omitempty"`

	// Units holds logical unit configurations (sub-interfaces)
	Units map[int]*Unit `json:"units,omitempty"`
}

// Unit represents a logical unit (sub-interface) configuration
type Unit struct {
	// Family holds address family configurations
	Family map[string]*Family `json:"family,omitempty"`
}

// Family represents an address family (inet, inet6) configuration
type Family struct {
	// Addresses holds IP addresses in CIDR format
	Addresses []string `json:"addresses,omitempty"`
}

// NewConfig creates a new empty configuration
func NewConfig() *Config {
	return &Config{
		Interfaces: make(map[string]*Interface),
	}
}

// GetOrCreateInterface gets or creates an interface configuration
func (c *Config) GetOrCreateInterface(name string) *Interface {
	if c.Interfaces == nil {
		c.Interfaces = make(map[string]*Interface)
	}
	if c.Interfaces[name] == nil {
		c.Interfaces[name] = &Interface{
			Units: make(map[int]*Unit),
		}
	}
	return c.Interfaces[name]
}

// GetOrCreateUnit gets or creates a unit configuration
func (i *Interface) GetOrCreateUnit(unitNum int) *Unit {
	if i.Units == nil {
		i.Units = make(map[int]*Unit)
	}
	if i.Units[unitNum] == nil {
		i.Units[unitNum] = &Unit{
			Family: make(map[string]*Family),
		}
	}
	return i.Units[unitNum]
}

// GetOrCreateFamily gets or creates a family configuration
func (u *Unit) GetOrCreateFamily(familyName string) *Family {
	if u.Family == nil {
		u.Family = make(map[string]*Family)
	}
	if u.Family[familyName] == nil {
		u.Family[familyName] = &Family{
			Addresses: make([]string, 0),
		}
	}
	return u.Family[familyName]
}
