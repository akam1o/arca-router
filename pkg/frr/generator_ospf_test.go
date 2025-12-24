package frr

import (
	"strings"
	"testing"
)

// intPtr returns a pointer to an int value (helper for tests)
func intPtr(v int) *int {
	return &v
}

func TestGenerateOSPFConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *OSPFConfig
		want    []string
		wantErr bool
	}{
		{
			name: "basic OSPFv2 with network",
			cfg: &OSPFConfig{
				RouterID: "10.0.1.1",
				Networks: []OSPFNetwork{
					{Prefix: "10.0.1.0/24", AreaID: "0.0.0.0"},
				},
			},
			want: []string{
				"router ospf",
				"ospf router-id 10.0.1.1",
				"network 10.0.1.0/24 area 0.0.0.0",
			},
			wantErr: false,
		},
		{
			name: "OSPF with interface configuration",
			cfg: &OSPFConfig{
				RouterID: "10.0.1.1",
				Networks: []OSPFNetwork{
					{Prefix: "192.168.1.0/24", AreaID: "0"},
				},
				Interfaces: []OSPFInterface{
					{
						Name:     "ge0-0-1",
						AreaID:   "0",
						Passive:  true,
						Metric:   100,
						Priority: nil, // Not set
					},
				},
			},
			want: []string{
				"router ospf",
				"network 192.168.1.0/24 area 0",
				"interface ge0-0-1",
				"ip ospf passive",
				"ip ospf cost 100",
			},
			wantErr: false,
		},
		{
			name: "OSPF with multiple networks (sorted)",
			cfg: &OSPFConfig{
				RouterID: "10.0.1.1",
				Networks: []OSPFNetwork{
					{Prefix: "192.168.2.0/24", AreaID: "0"},
					{Prefix: "10.0.1.0/24", AreaID: "0"},
					{Prefix: "192.168.1.0/24", AreaID: "0"},
				},
			},
			want: []string{
				"network 10.0.1.0/24 area 0",
				"network 192.168.1.0/24 area 0",
				"network 192.168.2.0/24 area 0",
			},
			wantErr: false,
		},
		{
			name: "OSPFv3 with IPv6",
			cfg: &OSPFConfig{
				IsOSPFv3: true,
				Interfaces: []OSPFInterface{
					{
						Name:     "ge0-0-0",
						AreaID:   "0.0.0.0",
						Passive:  false,
						Metric:   10,
						Priority: intPtr(1),
					},
				},
			},
			want: []string{
				"router ospf6",
				"interface ge0-0-0",
				"ipv6 ospf6 area 0.0.0.0",
				"ipv6 ospf6 cost 10",
				"ipv6 ospf6 priority 1",
			},
			wantErr: false,
		},
		{
			name: "OSPF with passive interface only",
			cfg: &OSPFConfig{
				RouterID: "10.0.1.1",
				Interfaces: []OSPFInterface{
					{
						Name:    "ge0-0-1",
						AreaID:  "0",
						Passive: true,
					},
				},
			},
			want: []string{
				"interface ge0-0-1",
				"ip ospf passive",
			},
			wantErr: false,
		},
		{
			name:    "nil config",
			cfg:     nil,
			want:    []string{},
			wantErr: false,
		},
		{
			name: "missing router-id for OSPFv2",
			cfg: &OSPFConfig{
				RouterID: "",
				Networks: []OSPFNetwork{
					{Prefix: "10.0.1.0/24", AreaID: "0"},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid router-id (IPv6)",
			cfg: &OSPFConfig{
				RouterID: "2001:db8::1",
				Networks: []OSPFNetwork{
					{Prefix: "10.0.1.0/24", AreaID: "0"},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid network prefix",
			cfg: &OSPFConfig{
				RouterID: "10.0.1.1",
				Networks: []OSPFNetwork{
					{Prefix: "invalid-prefix", AreaID: "0"},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid area ID",
			cfg: &OSPFConfig{
				RouterID: "10.0.1.1",
				Networks: []OSPFNetwork{
					{Prefix: "10.0.1.0/24", AreaID: "invalid"},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateOSPFConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateOSPFConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				for _, want := range tt.want {
					if !strings.Contains(got, want) {
						t.Errorf("GenerateOSPFConfig() output missing expected string:\nWant: %s\nGot:\n%s", want, got)
					}
				}
			}
		})
	}
}

func TestValidateRouterID(t *testing.T) {
	tests := []struct {
		name     string
		routerID string
		wantErr  bool
	}{
		{"valid IPv4", "10.0.1.1", false},
		{"valid IPv4 (0.0.0.0)", "0.0.0.0", false},
		{"invalid IPv6", "2001:db8::1", true},
		{"invalid format", "invalid", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRouterID(tt.routerID)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateRouterID() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateOSPFNetwork(t *testing.T) {
	tests := []struct {
		name    string
		network *OSPFNetwork
		wantErr bool
	}{
		{
			name: "valid network",
			network: &OSPFNetwork{
				Prefix: "10.0.1.0/24",
				AreaID: "0.0.0.0",
			},
			wantErr: false,
		},
		{
			name: "valid network with integer area",
			network: &OSPFNetwork{
				Prefix: "192.168.1.0/24",
				AreaID: "0",
			},
			wantErr: false,
		},
		{
			name: "missing prefix",
			network: &OSPFNetwork{
				Prefix: "",
				AreaID: "0",
			},
			wantErr: true,
		},
		{
			name: "invalid CIDR",
			network: &OSPFNetwork{
				Prefix: "10.0.1.0",
				AreaID: "0",
			},
			wantErr: true,
		},
		{
			name: "missing area ID",
			network: &OSPFNetwork{
				Prefix: "10.0.1.0/24",
				AreaID: "",
			},
			wantErr: true,
		},
		{
			name: "invalid area ID",
			network: &OSPFNetwork{
				Prefix: "10.0.1.0/24",
				AreaID: "invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOSPFNetwork(tt.network)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateOSPFNetwork() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateOSPFInterface(t *testing.T) {
	tests := []struct {
		name    string
		iface   *OSPFInterface
		wantErr bool
	}{
		{
			name: "valid interface",
			iface: &OSPFInterface{
				Name:     "ge0-0-0",
				AreaID:   "0.0.0.0",
				Metric:   100,
				Priority: intPtr(1),
			},
			wantErr: false,
		},
		{
			name: "missing name",
			iface: &OSPFInterface{
				Name:   "",
				AreaID: "0",
			},
			wantErr: true,
		},
		{
			name: "missing area ID",
			iface: &OSPFInterface{
				Name:   "ge0-0-0",
				AreaID: "",
			},
			wantErr: true,
		},
		{
			name: "invalid metric (negative)",
			iface: &OSPFInterface{
				Name:   "ge0-0-0",
				AreaID: "0",
				Metric: -1,
			},
			wantErr: true,
		},
		{
			name: "invalid metric (too large)",
			iface: &OSPFInterface{
				Name:   "ge0-0-0",
				AreaID: "0",
				Metric: 65536,
			},
			wantErr: true,
		},
		{
			name: "invalid priority (negative)",
			iface: &OSPFInterface{
				Name:     "ge0-0-0",
				AreaID:   "0",
				Priority: intPtr(-1),
			},
			wantErr: true,
		},
		{
			name: "invalid priority (too large)",
			iface: &OSPFInterface{
				Name:     "ge0-0-0",
				AreaID:   "0",
				Priority: intPtr(256),
			},
			wantErr: true,
		},
		{
			name: "priority not set (nil)",
			iface: &OSPFInterface{
				Name:     "ge0-0-0",
				AreaID:   "0",
				Priority: nil,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOSPFInterface(tt.iface)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateOSPFInterface() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateAreaID(t *testing.T) {
	tests := []struct {
		name    string
		areaID  string
		wantErr bool
	}{
		{"IPv4 format (0.0.0.0)", "0.0.0.0", false},
		{"IPv4 format (0.0.0.1)", "0.0.0.1", false},
		{"integer format (0)", "0", false},
		{"integer format (123)", "123", false},
		{"invalid format", "invalid", true},
		{"IPv6 (not allowed)", "2001:db8::1", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAreaID(tt.areaID)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateAreaID() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
