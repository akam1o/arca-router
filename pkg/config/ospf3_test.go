package config

import (
	"strings"
	"testing"
)

func TestParser_OSPF3(t *testing.T) {
	input := strings.Join([]string{
		"set protocols ospf3 router-id 10.0.1.2",
		"set protocols ospf3 area 0.0.0.0 interface ge-0/0/0",
		"set protocols ospf3 area 0.0.0.0 interface ge-0/0/1 passive",
		"set protocols ospf3 area 0.0.0.0 interface ge-0/0/1 metric 100",
		"set protocols ospf3 area 0.0.0.0 interface ge-0/0/1 priority 0",
	}, "\n")

	cfg, err := NewParser(strings.NewReader(input)).Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.Protocols == nil || cfg.Protocols.OSPF3 == nil {
		t.Fatalf("OSPF3 is nil: %#v", cfg.Protocols)
	}
	if cfg.Protocols.OSPF3.RouterID != "10.0.1.2" {
		t.Fatalf("OSPF3 router-id = %q, want 10.0.1.2", cfg.Protocols.OSPF3.RouterID)
	}
	area := cfg.Protocols.OSPF3.Areas["0.0.0.0"]
	if area == nil {
		t.Fatal("OSPF3 area 0.0.0.0 is nil")
	}
	if len(area.Interfaces) != 2 {
		t.Fatalf("OSPF3 interfaces = %d, want 2", len(area.Interfaces))
	}
	iface := area.Interfaces["ge-0/0/1"]
	if iface == nil || !iface.Passive || iface.Metric != 100 || !iface.PrioritySet || iface.Priority != 0 {
		t.Fatalf("OSPF3 interface ge-0/0/1 = %#v, want passive metric 100 priority 0", iface)
	}
}

func TestToSetCommandsWritesOSPF3(t *testing.T) {
	cfg := &Config{
		Protocols: &ProtocolConfig{
			OSPF3: &OSPFConfig{
				RouterID: "10.0.1.2",
				Areas: map[string]*OSPFArea{
					"0.0.0.0": {
						AreaID: "0.0.0.0",
						Interfaces: map[string]*OSPFInterface{
							"ge-0/0/0": {Name: "ge-0/0/0", Metric: 20},
						},
					},
				},
			},
		},
	}

	text := ToSetCommands(cfg)
	for _, want := range []string{
		"set protocols ospf3 router-id 10.0.1.2\n",
		"set protocols ospf3 area 0.0.0.0 interface ge-0/0/0 metric 20\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("ToSetCommands() missing %q in:\n%s", want, text)
		}
	}
}

func TestValidate_OSPF3AllowsMissingRouterID(t *testing.T) {
	cfg := &Config{
		Interfaces: map[string]*Interface{
			"ge-0/0/0": {
				Units: map[int]*Unit{
					0: {
						Family: map[string]*Family{
							"inet6": {Addresses: []string{"2001:db8::1/64"}},
						},
					},
				},
			},
		},
		Protocols: &ProtocolConfig{
			OSPF3: &OSPFConfig{
				Areas: map[string]*OSPFArea{
					"0.0.0.0": {
						AreaID: "0.0.0.0",
						Interfaces: map[string]*OSPFInterface{
							"ge-0/0/0": {Name: "ge-0/0/0"},
						},
					},
				},
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}
