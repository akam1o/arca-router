package config

import (
	"testing"
)

func TestValidate_ValidConfig(t *testing.T) {
	config := &Config{
		System: &SystemConfig{
			HostName: "router-01",
		},
		Interfaces: map[string]*Interface{
			"ge-0/0/0": {
				Description: "WAN Interface",
				Units: map[int]*Unit{
					0: {
						Family: map[string]*Family{
							"inet": {
								Addresses: []string{"192.168.1.1/24"},
							},
						},
					},
				},
			},
		},
	}

	if err := config.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}

func TestValidate_InterfaceName(t *testing.T) {
	tests := []struct {
		name    string
		ifName  string
		wantErr bool
	}{
		{"valid ge interface", "ge-0/0/0", false},
		{"valid xe interface", "xe-1/2/3", false},
		{"valid et interface", "et-0/0/0", false},
		{"valid ae interface", "ae0", false},
		{"valid ae interface with number", "ae10", false},
		{"valid loopback", "lo0", false},
		{"valid irb", "irb", false},
		{"valid fxp", "fxp0", false},
		{"empty name", "", true},
		{"invalid format - no slashes", "ge-0-0-0", true},
		{"invalid format - too many parts", "ge-0/0/0/0", true},
		{"invalid format - no prefix", "0/0/0", true},
		{"invalid format - uppercase", "GE-0/0/0", true},
		{"invalid format - spaces", "ge-0/0/0 ", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateInterfaceName(tt.ifName)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateInterfaceName(%q) error = %v, wantErr %v", tt.ifName, err, tt.wantErr)
			}
		})
	}
}

func TestValidate_Hostname(t *testing.T) {
	tests := []struct {
		name     string
		hostname string
		wantErr  bool
	}{
		{"valid simple", "router-01", false},
		{"valid with domain", "router-01.example.com", false},
		{"valid single char", "r", false},
		{"empty hostname", "", true},
		{"too long", string(make([]byte, 254)), true},
		{"invalid start with hyphen", "-router", true},
		{"invalid end with hyphen", "router-", true},
		{"invalid underscore", "router_01", true},
		{"invalid space", "router 01", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &SystemConfig{
				HostName: tt.hostname,
			}
			err := config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("SystemConfig.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidate_Address(t *testing.T) {
	tests := []struct {
		name       string
		address    string
		familyName string
		wantErr    bool
	}{
		{"valid IPv4", "192.168.1.1/24", "inet", false},
		{"valid IPv4 /32", "10.0.0.1/32", "inet", false},
		{"valid IPv4 /8", "10.0.0.0/8", "inet", false},
		{"valid IPv6", "2001:db8::1/64", "inet6", false},
		{"valid IPv6 /128", "2001:db8::1/128", "inet6", false},
		{"empty address", "", "inet", true},
		{"invalid CIDR - no prefix", "192.168.1.1", "inet", true},
		{"invalid CIDR - bad format", "192.168.1.1/", "inet", true},
		{"invalid CIDR - prefix too large", "192.168.1.1/33", "inet", true},
		{"wrong family - IPv4 in inet6", "192.168.1.1/24", "inet6", true},
		{"wrong family - IPv6 in inet", "2001:db8::1/64", "inet", true},
		{"invalid IP", "999.999.999.999/24", "inet", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAddress(tt.address, tt.familyName, "ge-0/0/0", 0)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateAddress(%q, %q) error = %v, wantErr %v", tt.address, tt.familyName, err, tt.wantErr)
			}
		})
	}
}

func TestValidate_UnitNumber(t *testing.T) {
	tests := []struct {
		name    string
		unitNum int
		wantErr bool
	}{
		{"valid 0", 0, false},
		{"valid 100", 100, false},
		{"valid 32767", 32767, false},
		{"invalid negative", -1, true},
		{"invalid too large", 32768, true},
		{"invalid very large", 99999, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unit := &Unit{
				Family: map[string]*Family{
					"inet": {
						Addresses: []string{"192.168.1.1/24"},
					},
				},
			}
			err := unit.Validate("ge-0/0/0", tt.unitNum)
			if (err != nil) != tt.wantErr {
				t.Errorf("Unit.Validate(unitNum=%d) error = %v, wantErr %v", tt.unitNum, err, tt.wantErr)
			}
		})
	}
}

func TestValidate_FamilyName(t *testing.T) {
	tests := []struct {
		name       string
		familyName string
		wantErr    bool
	}{
		{"valid inet", "inet", false},
		{"valid inet6", "inet6", false},
		{"invalid ipv4", "ipv4", true},
		{"invalid ipv6", "ipv6", true},
		{"invalid empty", "", true},
		{"invalid mpls", "mpls", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			family := &Family{
				Addresses: []string{"192.168.1.1/24"},
			}
			// For inet6 test, use valid IPv6 address
			if tt.familyName == "inet6" {
				family.Addresses = []string{"2001:db8::1/64"}
			}
			err := family.Validate("ge-0/0/0", 0, tt.familyName)
			if (err != nil) != tt.wantErr {
				t.Errorf("Family.Validate(familyName=%q) error = %v, wantErr %v", tt.familyName, err, tt.wantErr)
			}
		})
	}
}

func TestValidate_NoAddresses(t *testing.T) {
	family := &Family{
		Addresses: []string{},
	}
	err := family.Validate("ge-0/0/0", 0, "inet")
	if err == nil {
		t.Error("Family.Validate() with no addresses should return error")
	}
}

func TestValidate_Description(t *testing.T) {
	tests := []struct {
		name        string
		description string
		wantErr     bool
	}{
		{"empty", "", false},
		{"normal", "WAN Interface", false},
		{"long but valid", string(make([]byte, 255)), false},
		{"too long", string(make([]byte, 256)), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iface := &Interface{
				Description: tt.description,
				Units: map[int]*Unit{
					0: {
						Family: map[string]*Family{
							"inet": {
								Addresses: []string{"192.168.1.1/24"},
							},
						},
					},
				},
			}
			err := iface.Validate("ge-0/0/0")
			if (err != nil) != tt.wantErr {
				t.Errorf("Interface.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidate_MultipleInterfaces(t *testing.T) {
	config := &Config{
		Interfaces: map[string]*Interface{
			"ge-0/0/0": {
				Units: map[int]*Unit{
					0: {
						Family: map[string]*Family{
							"inet": {
								Addresses: []string{"192.168.1.1/24"},
							},
						},
					},
				},
			},
			"ge-0/0/1": {
				Units: map[int]*Unit{
					0: {
						Family: map[string]*Family{
							"inet": {
								Addresses: []string{"10.0.0.1/8"},
							},
						},
					},
				},
			},
		},
	}

	if err := config.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}

func TestValidate_NilConfig(t *testing.T) {
	var config *Config
	err := config.Validate()
	if err == nil {
		t.Error("Validate() on nil config should return error")
	}
}
