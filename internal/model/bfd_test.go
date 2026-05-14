package model

import (
	"strings"
	"testing"

	"github.com/akam1o/arca-router/pkg/config"
)

func TestBFDLegacyConversionRoundTrip(t *testing.T) {
	legacy := config.NewConfig()
	legacy.Protocols = &config.ProtocolConfig{
		BFD: &config.BFDConfig{
			Profiles: map[string]*config.BFDProfile{
				"fast": {Name: "fast", DetectMultiplier: 3, ReceiveInterval: 150, TransmitInterval: 150},
			},
			Peers: map[string]*config.BFDPeer{
				"192.0.2.2": {Address: "192.0.2.2", LocalAddress: "192.0.2.1", Interface: "ge-0/0/0", Profile: "fast"},
			},
		},
	}

	modelCfg := FromLegacyConfig(legacy)
	if modelCfg.Protocols == nil || modelCfg.Protocols.BFD == nil {
		t.Fatalf("FromLegacyConfig() dropped BFD: %#v", modelCfg.Protocols)
	}
	roundTrip := modelCfg.ToLegacyConfig()
	if roundTrip.Protocols == nil || roundTrip.Protocols.BFD == nil || roundTrip.Protocols.BFD.Peers["192.0.2.2"] == nil {
		t.Fatalf("ToLegacyConfig() dropped BFD: %#v", roundTrip.Protocols)
	}
	if got := roundTrip.Protocols.BFD.Peers["192.0.2.2"].Profile; got != "fast" {
		t.Fatalf("BFD peer profile = %q, want fast", got)
	}
}

func TestValidateBFDUnknownInterface(t *testing.T) {
	cfg := NewRouterConfig()
	cfg.Protocols = &ProtocolsConfig{
		BFD: &BFDConfig{
			Peers: map[string]*BFDPeer{
				"192.0.2.2": {Interface: "ge-0/0/0"},
			},
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "interface") {
		t.Fatalf("Validate() error = %v, want interface reference error", err)
	}
}
