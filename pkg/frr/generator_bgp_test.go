package frr

import (
	"strings"
	"testing"
)

func TestGenerateBGPConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *BGPConfig
		want    []string // Expected substrings in output
		wantErr bool
	}{
		{
			name: "basic BGP with single neighbor",
			cfg: &BGPConfig{
				ASN:         65001,
				RouterID:    "10.0.1.1",
				IPv4Unicast: true,
				Neighbors: []BGPNeighbor{
					{
						IP:       "10.0.1.2",
						RemoteAS: 65001,
					},
				},
			},
			want: []string{
				"router bgp 65001",
				"bgp router-id 10.0.1.1",
				"neighbor 10.0.1.2 remote-as 65001",
				"address-family ipv4 unicast",
				"neighbor 10.0.1.2 activate",
			},
			wantErr: false,
		},
		{
			name: "BGP with description and update-source",
			cfg: &BGPConfig{
				ASN:         65001,
				IPv4Unicast: true,
				Neighbors: []BGPNeighbor{
					{
						IP:           "10.0.2.2",
						RemoteAS:     65002,
						Description:  "External BGP Peer - ISP",
						UpdateSource: "ge0-0-2",
					},
				},
			},
			want: []string{
				"neighbor 10.0.2.2 remote-as 65002",
				"neighbor 10.0.2.2 description \"External BGP Peer - ISP\"",
				"neighbor 10.0.2.2 update-source ge0-0-2",
			},
			wantErr: false,
		},
		{
			name: "BGP with multiple neighbors (sorted)",
			cfg: &BGPConfig{
				ASN:         65001,
				IPv4Unicast: true,
				Neighbors: []BGPNeighbor{
					{IP: "10.0.1.3", RemoteAS: 65001},
					{IP: "10.0.1.1", RemoteAS: 65001},
					{IP: "10.0.1.2", RemoteAS: 65001},
				},
			},
			want: []string{
				"neighbor 10.0.1.1 remote-as 65001",
				"neighbor 10.0.1.2 remote-as 65001",
				"neighbor 10.0.1.3 remote-as 65001",
			},
			wantErr: false,
		},
		{
			name: "BGP with IPv6 neighbor",
			cfg: &BGPConfig{
				ASN:         65001,
				IPv6Unicast: true,
				Neighbors: []BGPNeighbor{
					{
						IP:       "2001:db8::2",
						RemoteAS: 65001,
						IsIPv6:   true,
					},
				},
			},
			want: []string{
				"router bgp 65001",
				"neighbor 2001:db8::2 remote-as 65001",
				"address-family ipv6 unicast",
				"neighbor 2001:db8::2 activate",
			},
			wantErr: false,
		},
		{
			name: "BGP with both IPv4 and IPv6",
			cfg: &BGPConfig{
				ASN:         65001,
				IPv4Unicast: true,
				IPv6Unicast: true,
				Neighbors: []BGPNeighbor{
					{IP: "10.0.1.2", RemoteAS: 65001, IsIPv6: false},
					{IP: "2001:db8::2", RemoteAS: 65001, IsIPv6: true},
				},
			},
			want: []string{
				"address-family ipv4 unicast",
				"neighbor 10.0.1.2 activate",
				"address-family ipv6 unicast",
				"neighbor 2001:db8::2 activate",
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
			name: "missing ASN",
			cfg: &BGPConfig{
				ASN: 0,
			},
			wantErr: true,
		},
		{
			name: "invalid neighbor IP",
			cfg: &BGPConfig{
				ASN: 65001,
				Neighbors: []BGPNeighbor{
					{IP: "invalid-ip", RemoteAS: 65001},
				},
			},
			wantErr: true,
		},
		{
			name: "missing remote-as",
			cfg: &BGPConfig{
				ASN: 65001,
				Neighbors: []BGPNeighbor{
					{IP: "10.0.1.2", RemoteAS: 0},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateBGPConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateBGPConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				for _, want := range tt.want {
					if !strings.Contains(got, want) {
						t.Errorf("GenerateBGPConfig() output missing expected string:\nWant: %s\nGot:\n%s", want, got)
					}
				}
			}
		})
	}
}

func TestValidateBGPNeighbor(t *testing.T) {
	tests := []struct {
		name     string
		neighbor *BGPNeighbor
		wantErr  bool
	}{
		{
			name: "valid IPv4 neighbor",
			neighbor: &BGPNeighbor{
				IP:       "10.0.1.2",
				RemoteAS: 65001,
			},
			wantErr: false,
		},
		{
			name: "valid IPv6 neighbor",
			neighbor: &BGPNeighbor{
				IP:       "2001:db8::2",
				RemoteAS: 65001,
			},
			wantErr: false,
		},
		{
			name: "missing IP",
			neighbor: &BGPNeighbor{
				IP:       "",
				RemoteAS: 65001,
			},
			wantErr: true,
		},
		{
			name: "invalid IP format",
			neighbor: &BGPNeighbor{
				IP:       "999.999.999.999",
				RemoteAS: 65001,
			},
			wantErr: true,
		},
		{
			name: "missing RemoteAS",
			neighbor: &BGPNeighbor{
				IP:       "10.0.1.2",
				RemoteAS: 0,
			},
			wantErr: true,
		},
		{
			name: "valid max RemoteAS",
			neighbor: &BGPNeighbor{
				IP:       "10.0.1.2",
				RemoteAS: 4294967295, // Max valid AS
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBGPNeighbor(tt.neighbor)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateBGPNeighbor() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestEscapeDescription(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no spaces",
			input: "BGP-Peer",
			want:  "BGP-Peer",
		},
		{
			name:  "with spaces",
			input: "External BGP Peer",
			want:  "\"External BGP Peer\"",
		},
		{
			name:  "with quotes",
			input: "Peer \"Main\"",
			want:  "\"Peer \\\"Main\\\"\"",
		},
		{
			name:  "with tabs",
			input: "Peer\tMain",
			want:  "\"Peer\tMain\"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeDescription(tt.input)
			if got != tt.want {
				t.Errorf("escapeDescription() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsIPv6(t *testing.T) {
	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{"IPv4", "10.0.1.1", false},
		{"IPv6", "2001:db8::1", true},
		{"IPv6 localhost", "::1", true},
		{"invalid", "invalid", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isIPv6(tt.ip)
			if got != tt.want {
				t.Errorf("isIPv6(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

// TestGenerateBGPConfigWithRouteMaps tests BGP configuration with route-maps (import/export policies)
func TestGenerateBGPConfigWithRouteMaps(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *BGPConfig
		want    []string // Expected substrings in output
		wantErr bool
	}{
		{
			name: "BGP with import route-map (IPv4)",
			cfg: &BGPConfig{
				ASN:         65001,
				RouterID:    "10.0.1.1",
				IPv4Unicast: true,
				Neighbors: []BGPNeighbor{
					{
						IP:          "10.0.1.2",
						RemoteAS:    65002,
						RouteMapIn:  "IMPORT-POLICY",
						RouteMapOut: "",
					},
				},
			},
			want: []string{
				"router bgp 65001",
				"neighbor 10.0.1.2 remote-as 65002",
				"address-family ipv4 unicast",
				"neighbor 10.0.1.2 activate",
				"neighbor 10.0.1.2 route-map IMPORT-POLICY in",
			},
			wantErr: false,
		},
		{
			name: "BGP with export route-map (IPv4)",
			cfg: &BGPConfig{
				ASN:         65001,
				RouterID:    "10.0.1.1",
				IPv4Unicast: true,
				Neighbors: []BGPNeighbor{
					{
						IP:          "10.0.1.2",
						RemoteAS:    65002,
						RouteMapIn:  "",
						RouteMapOut: "EXPORT-POLICY",
					},
				},
			},
			want: []string{
				"router bgp 65001",
				"neighbor 10.0.1.2 remote-as 65002",
				"address-family ipv4 unicast",
				"neighbor 10.0.1.2 activate",
				"neighbor 10.0.1.2 route-map EXPORT-POLICY out",
			},
			wantErr: false,
		},
		{
			name: "BGP with both import and export route-maps (IPv4)",
			cfg: &BGPConfig{
				ASN:         65001,
				RouterID:    "10.0.1.1",
				IPv4Unicast: true,
				Neighbors: []BGPNeighbor{
					{
						IP:          "10.0.1.2",
						RemoteAS:    65002,
						RouteMapIn:  "IMPORT-POLICY",
						RouteMapOut: "EXPORT-POLICY",
					},
				},
			},
			want: []string{
				"router bgp 65001",
				"neighbor 10.0.1.2 remote-as 65002",
				"address-family ipv4 unicast",
				"neighbor 10.0.1.2 activate",
				"neighbor 10.0.1.2 route-map IMPORT-POLICY in",
				"neighbor 10.0.1.2 route-map EXPORT-POLICY out",
			},
			wantErr: false,
		},
		{
			name: "BGP with route-maps (IPv6)",
			cfg: &BGPConfig{
				ASN:         65001,
				RouterID:    "10.0.1.1",
				IPv6Unicast: true,
				Neighbors: []BGPNeighbor{
					{
						IP:          "2001:db8::2",
						RemoteAS:    65002,
						IsIPv6:      true,
						RouteMapIn:  "IMPORT-V6-POLICY",
						RouteMapOut: "EXPORT-V6-POLICY",
					},
				},
			},
			want: []string{
				"router bgp 65001",
				"neighbor 2001:db8::2 remote-as 65002",
				"address-family ipv6 unicast",
				"neighbor 2001:db8::2 activate",
				"neighbor 2001:db8::2 route-map IMPORT-V6-POLICY in",
				"neighbor 2001:db8::2 route-map EXPORT-V6-POLICY out",
			},
			wantErr: false,
		},
		{
			name: "BGP with mixed neighbors (some with route-maps, some without)",
			cfg: &BGPConfig{
				ASN:         65001,
				RouterID:    "10.0.1.1",
				IPv4Unicast: true,
				Neighbors: []BGPNeighbor{
					{
						IP:          "10.0.1.2",
						RemoteAS:    65001,
						RouteMapIn:  "",
						RouteMapOut: "",
					},
					{
						IP:          "10.0.1.3",
						RemoteAS:    65002,
						RouteMapIn:  "IMPORT-POLICY",
						RouteMapOut: "EXPORT-POLICY",
					},
				},
			},
			want: []string{
				"router bgp 65001",
				"neighbor 10.0.1.2 activate",
				"neighbor 10.0.1.3 activate",
				"neighbor 10.0.1.3 route-map IMPORT-POLICY in",
				"neighbor 10.0.1.3 route-map EXPORT-POLICY out",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateBGPConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateBGPConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil {
				return // Error case, no need to check output
			}

			// Check for expected strings
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("GenerateBGPConfig() output missing expected string:\nwant: %s\ngot:\n%s", want, got)
				}
			}

			// Ensure that neighbor without route-map doesn't have route-map statements
			if tt.name == "BGP with mixed neighbors (some with route-maps, some without)" {
				lines := strings.Split(got, "\n")
				for _, line := range lines {
					if strings.Contains(line, "10.0.1.2") && strings.Contains(line, "route-map") {
						t.Errorf("Neighbor 10.0.1.2 should not have route-map, but got: %s", line)
					}
				}
			}
		})
	}
}
