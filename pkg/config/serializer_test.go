package config

import (
	"strings"
	"testing"
)

func TestToSetCommandsRoundTrip(t *testing.T) {
	accept := true
	localPref := uint32(150)
	cfg := &Config{
		System: &SystemConfig{HostName: "router 1"},
		Interfaces: map[string]*Interface{
			"ge-0/0/0": {
				Description: "WAN link",
				Units: map[int]*Unit{
					0: {
						Family: map[string]*Family{
							"inet": {Addresses: []string{"192.0.2.1/24"}},
						},
					},
				},
			},
		},
		RoutingOptions: &RoutingOptions{
			RouterID:         "192.0.2.1",
			AutonomousSystem: 65000,
			StaticRoutes: []*StaticRoute{
				{Prefix: "0.0.0.0/0", NextHop: "192.0.2.254", Distance: 10},
			},
		},
		Protocols: &ProtocolConfig{
			BGP: &BGPConfig{
				Groups: map[string]*BGPGroup{
					"EBGP": {
						Type:   "external",
						Import: "IMPORT-IN",
						Export: "EXPORT-OUT",
						Neighbors: map[string]*BGPNeighbor{
							"203.0.113.1": {
								IP:           "203.0.113.1",
								PeerAS:       65001,
								Description:  "upstream peer",
								LocalAddress: "192.0.2.1",
							},
						},
					},
				},
			},
			OSPF: &OSPFConfig{
				RouterID: "192.0.2.1",
				Areas: map[string]*OSPFArea{
					"0.0.0.0": {
						AreaID: "0.0.0.0",
						Interfaces: map[string]*OSPFInterface{
							"ge-0/0/0": {
								Name:    "ge-0/0/0",
								Passive: true,
								Metric:  20,
							},
						},
					},
				},
			},
		},
		PolicyOptions: &PolicyOptions{
			PrefixLists: map[string]*PrefixList{
				"PL-IN": {Name: "PL-IN", Prefixes: []string{"10.0.0.0/8"}},
			},
			PolicyStatements: map[string]*PolicyStatement{
				"IMPORT-IN": {
					Name: "IMPORT-IN",
					Terms: []*PolicyTerm{
						{
							Name: "one",
							From: &PolicyMatchConditions{
								PrefixLists: []string{"PL-IN"},
								Protocol:    "bgp",
							},
							Then: &PolicyActions{
								Accept:          &accept,
								LocalPreference: &localPref,
								Community:       "65000:100",
							},
						},
					},
				},
				"EXPORT-OUT": {Name: "EXPORT-OUT", Terms: []*PolicyTerm{{Name: "all", Then: &PolicyActions{Accept: &accept}}}},
			},
		},
		Security: &SecurityConfig{
			NETCONF:   &NETCONFConfig{SSH: &NETCONFSSHConfig{Port: 830}},
			Users:     map[string]*UserConfig{"admin": {Username: "admin", Password: "secret", Role: "admin", SSHKey: "ssh-ed25519 AAAA test"}},
			RateLimit: &RateLimitConfig{PerIP: 5, PerUser: 10},
		},
	}

	text := ToSetCommands(cfg)
	parsed, err := NewParser(strings.NewReader(text)).Parse()
	if err != nil {
		t.Fatalf("round-trip parse failed:\n%s\nerror: %v", text, err)
	}

	roundTripText := ToSetCommands(parsed)
	if roundTripText != text {
		t.Fatalf("round-trip text mismatch\nwant:\n%s\ngot:\n%s", text, roundTripText)
	}
}

func TestEscapeValue(t *testing.T) {
	got := EscapeValue("line \"one\"\nnext")
	want := `"line \"one\"\nnext"`
	if got != want {
		t.Fatalf("EscapeValue() = %q, want %q", got, want)
	}
}
