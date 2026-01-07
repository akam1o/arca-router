package frr

import (
	"strings"
	"testing"

	"github.com/akam1o/arca-router/pkg/config"
)

func TestGenerateFRRConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr bool
		checks  func(*testing.T, *Config)
	}{
		{
			name: "complete configuration with BGP, OSPF, and static routes",
			cfg: &config.Config{
				System: &config.SystemConfig{
					HostName: "router1",
				},
				Interfaces: map[string]*config.Interface{
					"ge-0/0/0": {
						Units: map[int]*config.Unit{
							0: {
								Family: map[string]*config.Family{
									"inet": {
										Addresses: []string{"10.0.1.1/24"},
									},
								},
							},
						},
					},
				},
				RoutingOptions: &config.RoutingOptions{
					AutonomousSystem: 65001,
					RouterID:         "10.0.1.1",
					StaticRoutes: []*config.StaticRoute{
						{Prefix: "0.0.0.0/0", NextHop: "10.0.1.254"},
					},
				},
				Protocols: &config.ProtocolConfig{
					BGP: &config.BGPConfig{
						Groups: map[string]*config.BGPGroup{
							"IBGP": {
								Neighbors: map[string]*config.BGPNeighbor{
									"10.0.1.2": {
										IP:          "10.0.1.2",
										PeerAS:      65001,
										Description: "Internal Peer",
									},
								},
							},
						},
					},
					OSPF: &config.OSPFConfig{
						RouterID: "10.0.1.1",
						Areas: map[string]*config.OSPFArea{
							"0.0.0.0": {
								AreaID: "0.0.0.0",
								Interfaces: map[string]*config.OSPFInterface{
									"ge-0/0/0": {
										Name: "ge-0/0/0",
									},
								},
							},
						},
					},
				},
			},
			wantErr: false,
			checks: func(t *testing.T, frrCfg *Config) {
				if frrCfg.Hostname != "router1" {
					t.Errorf("Hostname = %s, want router1", frrCfg.Hostname)
				}
				if frrCfg.BGP == nil {
					t.Error("BGP config is nil")
				}
				if frrCfg.OSPF == nil {
					t.Error("OSPF config is nil")
				}
				if len(frrCfg.StaticRoutes) != 1 {
					t.Errorf("len(StaticRoutes) = %d, want 1", len(frrCfg.StaticRoutes))
				}
				if len(frrCfg.InterfaceMapping) == 0 {
					t.Error("InterfaceMapping is empty")
				}
			},
		},
		{
			name: "BGP only",
			cfg: &config.Config{
				System: &config.SystemConfig{
					HostName: "bgp-router",
				},
				Interfaces: map[string]*config.Interface{
					"ge-0/0/0": {
						Units: map[int]*config.Unit{
							0: {
								Family: map[string]*config.Family{
									"inet": {
										Addresses: []string{"10.0.1.1/24"},
									},
								},
							},
						},
					},
				},
				RoutingOptions: &config.RoutingOptions{
					AutonomousSystem: 65001,
					RouterID:         "10.0.1.1",
				},
				Protocols: &config.ProtocolConfig{
					BGP: &config.BGPConfig{
						Groups: map[string]*config.BGPGroup{
							"EBGP": {
								Neighbors: map[string]*config.BGPNeighbor{
									"10.0.2.2": {
										IP:     "10.0.2.2",
										PeerAS: 65002,
									},
								},
							},
						},
					},
				},
			},
			wantErr: false,
			checks: func(t *testing.T, frrCfg *Config) {
				if frrCfg.BGP == nil {
					t.Fatal("BGP config is nil")
				}
				if frrCfg.BGP.ASN != 65001 {
					t.Errorf("BGP ASN = %d, want 65001", frrCfg.BGP.ASN)
				}
				if frrCfg.OSPF != nil {
					t.Error("OSPF config should be nil")
				}
			},
		},
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: true,
		},
		{
			name: "BGP without AS number",
			cfg: &config.Config{
				System: &config.SystemConfig{
					HostName: "router",
				},
				Interfaces:     map[string]*config.Interface{},
				RoutingOptions: &config.RoutingOptions{
					// Missing AutonomousSystem
				},
				Protocols: &config.ProtocolConfig{
					BGP: &config.BGPConfig{
						Groups: map[string]*config.BGPGroup{},
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateFRRConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateFRRConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.checks != nil {
				tt.checks(t, got)
			}
		})
	}
}

func TestGenerateFRRConfigFile(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		want    []string
		wantErr bool
	}{
		{
			name: "complete FRR config file",
			cfg: &Config{
				Hostname:     "router1",
				LogFile:      "/var/log/frr/frr.log",
				LogTimestamp: true,
				BGP: &BGPConfig{
					ASN:         65001,
					RouterID:    "10.0.1.1",
					IPv4Unicast: true,
					Neighbors: []BGPNeighbor{
						{IP: "10.0.1.2", RemoteAS: 65001},
					},
				},
				OSPF: &OSPFConfig{
					RouterID: "10.0.1.1",
					Networks: []OSPFNetwork{
						{Prefix: "10.0.1.0/24", AreaID: "0"},
					},
				},
				StaticRoutes: []StaticRoute{
					{Prefix: "0.0.0.0/0", NextHop: "10.0.1.254"},
				},
			},
			want: []string{
				"hostname router1",
				"log file /var/log/frr/frr.log",
				"log timestamp precision 3",
				"ip route 0.0.0.0/0 10.0.1.254",
				"router bgp 65001",
				"router ospf",
				"line vty",
			},
			wantErr: false,
		},
		{
			name: "minimal config",
			cfg: &Config{
				Hostname: "minimal",
			},
			want: []string{
				"hostname minimal",
				"line vty",
			},
			wantErr: false,
		},
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateFRRConfigFile(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateFRRConfigFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				for _, want := range tt.want {
					if !strings.Contains(got, want) {
						t.Errorf("GenerateFRRConfigFile() output missing expected string:\nWant: %s\nGot:\n%s", want, got)
					}
				}
			}
		})
	}
}

func TestBuildInterfaceMapping(t *testing.T) {
	cfg := &config.Config{
		Interfaces: map[string]*config.Interface{
			"ge-0/0/0": {},
			"xe-1/2/3": {},
		},
	}

	frrCfg := &Config{
		InterfaceMapping: make(map[string]string),
	}

	err := buildInterfaceMapping(cfg, frrCfg)
	if err != nil {
		t.Fatalf("buildInterfaceMapping() error = %v", err)
	}

	if len(frrCfg.InterfaceMapping) != 2 {
		t.Errorf("len(InterfaceMapping) = %d, want 2", len(frrCfg.InterfaceMapping))
	}

	// Check specific mappings
	if linux, ok := frrCfg.InterfaceMapping["ge-0/0/0"]; !ok {
		t.Error("ge-0/0/0 not in mapping")
	} else if linux != "ge0-0-0" {
		t.Errorf("ge-0/0/0 mapped to %s, want ge0-0-0", linux)
	}
}

func TestHasIPAddress(t *testing.T) {
	iface := &config.Interface{
		Units: map[int]*config.Unit{
			0: {
				Family: map[string]*config.Family{
					"inet": {
						Addresses: []string{"10.0.1.1/24", "192.168.1.1/24"},
					},
					"inet6": {
						Addresses: []string{"2001:db8::1/64"},
					},
				},
			},
		},
	}

	tests := []struct {
		name   string
		ipAddr string
		want   bool
	}{
		{"IPv4 address present", "10.0.1.1", true},
		{"IPv4 address present (second)", "192.168.1.1", true},
		{"IPv6 address present", "2001:db8::1", true},
		{"IPv4 address not present", "10.0.2.1", false},
		{"IPv6 address not present", "2001:db8::2", false},
		{"invalid IP", "invalid", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasIPAddress(iface, tt.ipAddr)
			if got != tt.want {
				t.Errorf("hasIPAddress(%s) = %v, want %v", tt.ipAddr, got, tt.want)
			}
		})
	}
}

// TestConvertBGPConfigPolicyValidation tests validation of policy references
func TestConvertBGPConfigPolicyValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid policy references",
			cfg: &config.Config{
				RoutingOptions: &config.RoutingOptions{
					AutonomousSystem: 65001,
					RouterID:         "10.0.1.1",
				},
				PolicyOptions: &config.PolicyOptions{
					PolicyStatements: map[string]*config.PolicyStatement{
						"IMPORT-POLICY": {Name: "IMPORT-POLICY"},
						"EXPORT-POLICY": {Name: "EXPORT-POLICY"},
					},
				},
				Protocols: &config.ProtocolConfig{
					BGP: &config.BGPConfig{
						Groups: map[string]*config.BGPGroup{
							"external": {
								Import: "IMPORT-POLICY",
								Export: "EXPORT-POLICY",
								Neighbors: map[string]*config.BGPNeighbor{
									"10.0.1.2": {IP: "10.0.1.2", PeerAS: 65002},
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "missing import policy",
			cfg: &config.Config{
				RoutingOptions: &config.RoutingOptions{
					AutonomousSystem: 65001,
					RouterID:         "10.0.1.1",
				},
				PolicyOptions: &config.PolicyOptions{
					PolicyStatements: map[string]*config.PolicyStatement{
						"EXPORT-POLICY": {Name: "EXPORT-POLICY"},
					},
				},
				Protocols: &config.ProtocolConfig{
					BGP: &config.BGPConfig{
						Groups: map[string]*config.BGPGroup{
							"external": {
								Import: "NONEXISTENT-POLICY",
								Export: "EXPORT-POLICY",
								Neighbors: map[string]*config.BGPNeighbor{
									"10.0.1.2": {IP: "10.0.1.2", PeerAS: 65002},
								},
							},
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "import policy 'NONEXISTENT-POLICY' but policy-statement does not exist",
		},
		{
			name: "missing export policy",
			cfg: &config.Config{
				RoutingOptions: &config.RoutingOptions{
					AutonomousSystem: 65001,
					RouterID:         "10.0.1.1",
				},
				PolicyOptions: &config.PolicyOptions{
					PolicyStatements: map[string]*config.PolicyStatement{
						"IMPORT-POLICY": {Name: "IMPORT-POLICY"},
					},
				},
				Protocols: &config.ProtocolConfig{
					BGP: &config.BGPConfig{
						Groups: map[string]*config.BGPGroup{
							"external": {
								Import: "IMPORT-POLICY",
								Export: "NONEXISTENT-EXPORT",
								Neighbors: map[string]*config.BGPNeighbor{
									"10.0.1.2": {IP: "10.0.1.2", PeerAS: 65002},
								},
							},
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "export policy 'NONEXISTENT-EXPORT' but policy-statement does not exist",
		},
		{
			name: "policy reference with no policy-options configured",
			cfg: &config.Config{
				RoutingOptions: &config.RoutingOptions{
					AutonomousSystem: 65001,
					RouterID:         "10.0.1.1",
				},
				Protocols: &config.ProtocolConfig{
					BGP: &config.BGPConfig{
						Groups: map[string]*config.BGPGroup{
							"external": {
								Import: "IMPORT-POLICY",
								Neighbors: map[string]*config.BGPNeighbor{
									"10.0.1.2": {IP: "10.0.1.2", PeerAS: 65002},
								},
							},
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "import policy 'IMPORT-POLICY' but no policy-options are configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := convertBGPConfig(tt.cfg, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("convertBGPConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil && tt.errMsg != "" {
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("convertBGPConfig() error = %v, expected to contain %q", err, tt.errMsg)
				}
			}
		})
	}
}
