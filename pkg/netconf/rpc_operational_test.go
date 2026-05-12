package netconf

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/akam1o/arca-router/pkg/config"
)

func TestBuildAllOperationalDataDoesNotReturnFabricatedCounters(t *testing.T) {
	data := buildAllOperationalData()
	for _, unexpected := range []string{
		"GigabitEthernet0/0/0",
		"1234567890",
		"9876543210",
		"2025-12-28T00:00:00Z",
	} {
		if strings.Contains(data, unexpected) {
			t.Fatalf("operational data contains fabricated value %q:\n%s", unexpected, data)
		}
	}
	if !strings.Contains(data, "<current-datetime>") {
		t.Fatalf("operational data missing current datetime:\n%s", data)
	}
}

func TestBuildOperationalDataUsesRunningConfig(t *testing.T) {
	cfg := config.NewConfig()
	cfg.System = &config.SystemConfig{HostName: "router1"}
	iface := cfg.GetOrCreateInterface("ge-0/0/0")
	unit := iface.GetOrCreateUnit(0)
	unit.GetOrCreateFamily("inet").Addresses = []string{"192.0.2.1/24"}
	cfg.RoutingOptions = &config.RoutingOptions{
		AutonomousSystem: 65000,
		StaticRoutes: []*config.StaticRoute{
			{Prefix: "0.0.0.0/0", NextHop: "192.0.2.254", Distance: 5},
		},
	}
	cfg.Protocols = &config.ProtocolConfig{BGP: &config.BGPConfig{}}

	data, err := buildOperationalData(cfg, nil, time.Date(2026, 5, 12, 4, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("buildOperationalData() error = %v", err)
	}
	for _, want := range []string{
		"<hostname>router1</hostname>",
		"<name>ge-0/0/0</name>",
		"<ip>192.0.2.1/24</ip>",
		"<destination-prefix>0.0.0.0/0</destination-prefix>",
		"<next-hop>192.0.2.254</next-hop>",
		"<name>BGP-65000</name>",
	} {
		if !bytes.Contains(data, []byte(want)) {
			t.Fatalf("operational data missing %q:\n%s", want, data)
		}
	}
}
