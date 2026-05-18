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
	if strings.Contains(text, " secret") {
		t.Fatalf("ToSetCommands() leaked plain password:\n%s", text)
	}
	if cfg.Security.Users["admin"].Password == "" || cfg.Security.Users["admin"].Password != "secret" {
		t.Fatalf("ToSetCommands() mutated source password: %q", cfg.Security.Users["admin"].Password)
	}
	parsed, err := NewParser(strings.NewReader(text)).Parse()
	if err != nil {
		t.Fatalf("round-trip parse failed:\n%s\nerror: %v", text, err)
	}

	roundTripText := ToSetCommands(parsed)
	if roundTripText != text {
		t.Fatalf("round-trip text mismatch\nwant:\n%s\ngot:\n%s", text, roundTripText)
	}
}

func TestToSetCommandsRedactedWithErrorRedactsCredentials(t *testing.T) {
	cfg := &Config{
		System: &SystemConfig{
			Services: &SystemServicesConfig{
				SNMP: &SNMPConfig{
					Enabled:   true,
					Community: "private-community",
				},
			},
		},
		Security: &SecurityConfig{
			Users: map[string]*UserConfig{
				"admin": {
					Username: "admin",
					Password: "plain-password",
					Role:     "admin",
				},
			},
		},
	}

	text, err := ToSetCommandsRedactedWithError(cfg)
	if err != nil {
		t.Fatalf("ToSetCommandsRedactedWithError() error = %v", err)
	}
	for _, secret := range []string{"plain-password", "private-community", "$argon2id$"} {
		if strings.Contains(text, secret) {
			t.Fatalf("redacted config leaked %q:\n%s", secret, text)
		}
	}
	if strings.Count(text, redactedSecretValue) != 2 {
		t.Fatalf("redacted config =\n%s\nwant two redacted markers", text)
	}
	if cfg.Security.Users["admin"].Password != "plain-password" {
		t.Fatalf("redacted serialization mutated source password: %q", cfg.Security.Users["admin"].Password)
	}
}

func TestEscapeValue(t *testing.T) {
	got := EscapeValue("line \"one\"\nnext")
	want := `"line \"one\"\nnext"`
	if got != want {
		t.Fatalf("EscapeValue() = %q, want %q", got, want)
	}
}

func TestToSetCommandsWritesOSPFAttributesSeparately(t *testing.T) {
	cfg := &Config{
		Interfaces: map[string]*Interface{},
		Protocols: &ProtocolConfig{
			OSPF: &OSPFConfig{
				Areas: map[string]*OSPFArea{
					"0.0.0.0": {
						Interfaces: map[string]*OSPFInterface{
							"ge-0/0/0": {Name: "ge-0/0/0", Passive: true, Metric: 20, Priority: 10},
						},
					},
				},
			},
		},
	}

	text := ToSetCommands(cfg)
	for _, want := range []string{
		"set protocols ospf area 0.0.0.0 interface ge-0/0/0 passive\n",
		"set protocols ospf area 0.0.0.0 interface ge-0/0/0 metric 20\n",
		"set protocols ospf area 0.0.0.0 interface ge-0/0/0 priority 10\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("ToSetCommands() missing %q in:\n%s", want, text)
		}
	}
}

func TestToSetCommandsWritesExplicitOSPFPriorityZero(t *testing.T) {
	cfg := &Config{
		Interfaces: map[string]*Interface{},
		Protocols: &ProtocolConfig{
			OSPF: &OSPFConfig{
				Areas: map[string]*OSPFArea{
					"0.0.0.0": {
						Interfaces: map[string]*OSPFInterface{
							"ge-0/0/0": {Name: "ge-0/0/0", Priority: 0, PrioritySet: true},
						},
					},
				},
			},
		},
	}

	text := ToSetCommands(cfg)
	want := "set protocols ospf area 0.0.0.0 interface ge-0/0/0 priority 0\n"
	if !strings.Contains(text, want) {
		t.Fatalf("ToSetCommands() missing %q in:\n%s", want, text)
	}

	parsed, err := NewParser(strings.NewReader(text)).Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	iface := parsed.Protocols.OSPF.Areas["0.0.0.0"].Interfaces["ge-0/0/0"]
	if !iface.PrioritySet || iface.Priority != 0 {
		t.Fatalf("parsed OSPF interface = %#v, want explicit priority 0", iface)
	}
}
