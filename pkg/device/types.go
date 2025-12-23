package device

// HardwareConfig represents the hardware.yaml configuration
type HardwareConfig struct {
	Interfaces []PhysicalInterface `yaml:"interfaces" json:"interfaces"`
}

// PhysicalInterface represents a physical NIC configuration
type PhysicalInterface struct {
	// Name is the logical interface name (e.g., "ge-0/0/0")
	Name string `yaml:"name" json:"name"`

	// PCI is the PCI address (e.g., "0000:03:00.0")
	PCI string `yaml:"pci" json:"pci"`

	// Driver is the driver type: "avf", "rdma", or "dpdk"
	Driver string `yaml:"driver" json:"driver"`

	// Description is a human-readable description
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// Validate checks if the physical interface configuration is valid
func (p *PhysicalInterface) Validate() error {
	if p.Name == "" {
		return &ValidationError{Field: "name", Message: "interface name cannot be empty"}
	}
	if p.PCI == "" {
		return &ValidationError{Field: "pci", Message: "PCI address cannot be empty"}
	}
	if p.Driver == "" {
		return &ValidationError{Field: "driver", Message: "driver type cannot be empty"}
	}

	// Validate driver type
	validDrivers := map[string]bool{
		"avf":  true,
		"rdma": true,
		"dpdk": true,
	}
	if !validDrivers[p.Driver] {
		return &ValidationError{
			Field:   "driver",
			Message: "driver must be one of: avf, rdma, dpdk",
		}
	}

	return nil
}

// ValidationError represents a validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}
