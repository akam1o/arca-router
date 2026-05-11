package model

import "testing"

func TestNewSnapshotClonesConfig(t *testing.T) {
	cfg := &RouterConfig{
		System: &SystemConfig{HostName: "router1"},
		Interfaces: map[string]*InterfaceConfig{
			"ge-0/0/0": {
				Units: map[int]*Unit{
					0: {
						Family: map[string]*AddressFamily{
							"inet": {Addresses: []string{"192.0.2.1/24"}},
						},
					},
				},
			},
		},
	}

	snap := NewSnapshot(cfg, 1, "alice", "test")
	cfg.System.HostName = "router2"
	cfg.Interfaces["ge-0/0/0"].Units[0].Family["inet"].Addresses[0] = "192.0.2.2/24"

	if got := snap.Config.System.HostName; got != "router1" {
		t.Fatalf("snapshot hostname = %q, want router1", got)
	}
	if got := snap.Config.Interfaces["ge-0/0/0"].Units[0].Family["inet"].Addresses[0]; got != "192.0.2.1/24" {
		t.Fatalf("snapshot address = %q, want original address", got)
	}
}

func TestConfigSnapshotCloneIsDeep(t *testing.T) {
	snap := NewSnapshot(&RouterConfig{
		Policy: &PolicyConfig{
			PolicyStatements: map[string]*PolicyStatement{
				"EXPORT": {
					Terms: []*PolicyTerm{
						{
							Name: "t1",
							Then: &PolicyActions{Community: "65000:1"},
						},
					},
				},
			},
		},
	}, 1, "alice", "test")

	clone := snap.Clone()
	clone.Config.Policy.PolicyStatements["EXPORT"].Terms[0].Then.Community = "65000:2"

	if got := snap.Config.Policy.PolicyStatements["EXPORT"].Terms[0].Then.Community; got != "65000:1" {
		t.Fatalf("original snapshot community = %q, want unchanged", got)
	}
}
