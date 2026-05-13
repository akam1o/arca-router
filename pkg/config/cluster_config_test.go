package config

import "testing"

func TestClusterConfigRoundTrip(t *testing.T) {
	cfg := parseSetCommands(t,
		"set chassis cluster enabled true",
		"set chassis cluster node node0 address 192.0.2.10",
		"set chassis cluster node node0 priority 120",
		"set chassis cluster sync etcd endpoint http://127.0.0.1:2379",
	)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if cfg.Chassis == nil || cfg.Chassis.Cluster == nil || !cfg.Chassis.Cluster.Enabled {
		t.Fatalf("chassis cluster not parsed: %#v", cfg.Chassis)
	}
	if got := cfg.Chassis.Cluster.Nodes["node0"].Priority; got != 120 {
		t.Fatalf("cluster node priority = %d", got)
	}
	if got := cfg.Chassis.Cluster.Sync.Etcd.Endpoints; len(got) != 1 || got[0] != "http://127.0.0.1:2379" {
		t.Fatalf("cluster etcd endpoints = %#v, want one endpoint", got)
	}
	assertSetCommandRoundTrip(t, cfg)
}
