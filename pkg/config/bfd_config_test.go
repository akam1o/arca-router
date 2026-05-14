package config

import (
	"strings"
	"testing"
)

func TestParserBFD(t *testing.T) {
	input := strings.Join([]string{
		"set interfaces ge-0/0/0 unit 0 family inet address 192.0.2.1/24",
		"set protocols bfd profile fast receive-interval 150",
		"set protocols bfd profile fast transmit-interval 150",
		"set protocols bfd profile fast detect-multiplier 3",
		"set protocols bfd profile fast passive-mode",
		"set protocols bfd peer 192.0.2.2 interface ge-0/0/0",
		"set protocols bfd peer 192.0.2.2 local-address 192.0.2.1",
		"set protocols bfd peer 192.0.2.2 profile fast",
		"set protocols bfd peer 192.0.2.2 multihop",
		"set protocols bfd peer 192.0.2.2 shutdown",
	}, "\n")

	cfg, err := NewParser(strings.NewReader(input)).Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Protocols == nil || cfg.Protocols.BFD == nil {
		t.Fatalf("BFD config is nil: %#v", cfg.Protocols)
	}
	profile := cfg.Protocols.BFD.Profiles["fast"]
	if profile == nil || profile.ReceiveInterval != 150 || profile.TransmitInterval != 150 || profile.DetectMultiplier != 3 || !profile.PassiveMode {
		t.Fatalf("BFD profile = %#v, want fast timers", profile)
	}
	peer := cfg.Protocols.BFD.Peers["192.0.2.2"]
	if peer == nil || peer.Interface != "ge-0/0/0" || peer.LocalAddress != "192.0.2.1" || peer.Profile != "fast" || !peer.Multihop || !peer.Shutdown {
		t.Fatalf("BFD peer = %#v, want configured peer", peer)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestToSetCommandsWritesBFD(t *testing.T) {
	cfg := &Config{
		Protocols: &ProtocolConfig{
			BFD: &BFDConfig{
				Profiles: map[string]*BFDProfile{
					"fast": {
						DetectMultiplier: 3,
						ReceiveInterval:  150,
						TransmitInterval: 150,
						PassiveMode:      true,
					},
				},
				Peers: map[string]*BFDPeer{
					"192.0.2.2": {
						Address:      "192.0.2.2",
						LocalAddress: "192.0.2.1",
						Interface:    "ge-0/0/0",
						Profile:      "fast",
					},
				},
			},
		},
	}

	got := ToSetCommands(cfg)
	for _, want := range []string{
		"set protocols bfd profile fast receive-interval 150\n",
		"set protocols bfd profile fast transmit-interval 150\n",
		"set protocols bfd profile fast detect-multiplier 3\n",
		"set protocols bfd profile fast passive-mode\n",
		"set protocols bfd peer 192.0.2.2 local-address 192.0.2.1\n",
		"set protocols bfd peer 192.0.2.2 interface ge-0/0/0\n",
		"set protocols bfd peer 192.0.2.2 profile fast\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("ToSetCommands() missing %q:\n%s", want, got)
		}
	}
}

func TestValidateBFDReferences(t *testing.T) {
	cfg := NewConfig()
	cfg.Interfaces["ge-0/0/0"] = &Interface{Units: map[int]*Unit{0: {Family: map[string]*Family{"inet": {Addresses: []string{"192.0.2.1/24"}}}}}}
	cfg.Protocols = &ProtocolConfig{
		BFD: &BFDConfig{
			Profiles: map[string]*BFDProfile{"fast": {Name: "fast", EchoMode: true}},
			Peers: map[string]*BFDPeer{
				"192.0.2.2": {Address: "192.0.2.2", Interface: "ge-0/0/0", Profile: "fast", Multihop: true},
			},
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "echo-mode") {
		t.Fatalf("Validate() error = %v, want echo-mode multihop error", err)
	}
}
