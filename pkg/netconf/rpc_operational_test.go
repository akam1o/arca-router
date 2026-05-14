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

	data, err := buildOperationalData(cfg, nil, time.Date(2026, 5, 12, 4, 0, 0, 0, time.UTC), nil, nil)
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

func TestBuildOperationalDataUsesLiveInterfaceState(t *testing.T) {
	cfg := config.NewConfig()
	iface := cfg.GetOrCreateInterface("ge-0/0/0")
	unit := iface.GetOrCreateUnit(0)
	unit.GetOrCreateFamily("inet").Addresses = []string{"192.0.2.1/24"}

	data, err := buildOperationalData(cfg, nil, time.Date(2026, 5, 12, 4, 0, 0, 0, time.UTC), map[string]*InterfaceOperationalState{
		"ge-0/0/0": {
			Name:        "ge-0/0/0",
			AdminStatus: "up",
			OperStatus:  "down",
			MAC:         "02:00:00:00:00:01",
			QoSProfile:  "WAN",
			IPv4TableID: 100,
			IPv6TableID: 100,
			Counters: &InterfaceOperationalCounters{
				RxPackets: 10,
				TxPackets: 20,
				RxBytes:   1000,
				TxBytes:   2000,
				RxErrors:  1,
				TxErrors:  2,
				Drops:     3,
			},
			Queues: &InterfaceOperationalQueues{
				Rx: []InterfaceOperationalRxQueue{
					{QueueID: 0, WorkerID: 1, Mode: "polling"},
				},
				Tx: []InterfaceOperationalTxQueue{
					{QueueID: 0, Shared: true, Threads: []uint32{0, 2}},
				},
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("buildOperationalData() error = %v", err)
	}

	for _, want := range []string{
		"<name>ge-0/0/0</name>",
		"<admin-status>up</admin-status>",
		"<oper-status>down</oper-status>",
		"<phys-address>02:00:00:00:00:01</phys-address>",
		"<qos-profile>WAN</qos-profile>",
		"<ipv4-table-id>100</ipv4-table-id>",
		"<ipv6-table-id>100</ipv6-table-id>",
		"<rx-packets>10</rx-packets>",
		"<tx-packets>20</tx-packets>",
		"<rx-bytes>1000</rx-bytes>",
		"<tx-bytes>2000</tx-bytes>",
		"<rx-errors>1</rx-errors>",
		"<tx-errors>2</tx-errors>",
		"<drops>3</drops>",
		"<queue-placements>",
		"<rx-queue>",
		"<worker-id>1</worker-id>",
		"<mode>polling</mode>",
		"<tx-queue>",
		"<shared>true</shared>",
		"<thread>2</thread>",
		"<ip>192.0.2.1/24</ip>",
	} {
		if !bytes.Contains(data, []byte(want)) {
			t.Fatalf("operational data missing %q:\n%s", want, data)
		}
	}
}

func TestBuildOperationalDataWritesBFDState(t *testing.T) {
	cfg := config.NewConfig()
	data, err := buildOperationalData(cfg, nil, time.Date(2026, 5, 12, 4, 0, 0, 0, time.UTC), nil, &BFDOperationalState{
		LastRun:           time.Date(2026, 5, 14, 6, 0, 0, 0, time.UTC),
		ConfiguredPeers:   2,
		ObservedPeers:     2,
		UpPeers:           1,
		DownPeers:         1,
		SessionDownEvents: 3,
		RxFailPackets:     4,
		Issues:            []string{"FRR BFD peer 192.0.2.3 is down"},
		LastError:         "read FRR BFD status failed",
		Peers: []BFDPeerOperationalState{
			{
				Peer:              "192.0.2.3",
				LocalAddress:      "192.0.2.1",
				Interface:         "ge-0/0/1",
				VRF:               "BLUE",
				Status:            "down",
				Diagnostic:        "control detection time expired",
				RemoteDiagnostic:  "none",
				Observed:          true,
				Up:                false,
				SessionDownEvents: 3,
				RxFailPackets:     4,
			},
			{
				Peer:      "192.0.2.2",
				Interface: "ge-0/0/0",
				Status:    "up",
				Observed:  true,
				Up:        true,
			},
		},
	})
	if err != nil {
		t.Fatalf("buildOperationalData() error = %v", err)
	}

	for _, want := range []string{
		`<state xmlns="urn:arca:router:config:1.0">`,
		"<bfd>",
		"<last-run>2026-05-14T06:00:00Z</last-run>",
		"<configured-peers>2</configured-peers>",
		"<observed-peers>2</observed-peers>",
		"<up-peers>1</up-peers>",
		"<down-peers>1</down-peers>",
		"<session-down-events>3</session-down-events>",
		"<rx-fail-packets>4</rx-fail-packets>",
		"<address>192.0.2.2</address>",
		"<address>192.0.2.3</address>",
		"<local-address>192.0.2.1</local-address>",
		"<interface>ge-0/0/1</interface>",
		"<vrf>BLUE</vrf>",
		"<status>down</status>",
		"<diagnostic>control detection time expired</diagnostic>",
		"<remote-diagnostic>none</remote-diagnostic>",
		"<observed>true</observed>",
		"<up>false</up>",
		"<issue>FRR BFD peer 192.0.2.3 is down</issue>",
		"<last-error>read FRR BFD status failed</last-error>",
	} {
		if !bytes.Contains(data, []byte(want)) {
			t.Fatalf("operational data missing %q:\n%s", want, data)
		}
	}
	if bytes.Index(data, []byte("<address>192.0.2.2</address>")) > bytes.Index(data, []byte("<address>192.0.2.3</address>")) {
		t.Fatalf("BFD peers are not sorted:\n%s", data)
	}
}

func TestBuildOperationalDataWritesRoutingInstanceState(t *testing.T) {
	cfg := config.NewConfig()
	cfg.RoutingInstances = map[string]*config.RoutingInstance{}
	cfg.RoutingInstances["BLUE"] = &config.RoutingInstance{
		InstanceType:       "vrf",
		RouteDistinguisher: "65000:100",
		VRFTarget:          "target:65000:100",
		VRFTargetImport:    []string{"target:65000:101"},
		VRFTargetExport:    []string{"target:65000:102"},
		VRFImport:          []string{"BLUE-IN"},
		VRFExport:          []string{"BLUE-OUT"},
		Interfaces:         []string{"ge-0/0/1", "ge-0/0/0", "ge-0/0/1"},
	}

	data, err := buildOperationalData(cfg, nil, time.Date(2026, 5, 12, 4, 0, 0, 0, time.UTC), nil, nil)
	if err != nil {
		t.Fatalf("buildOperationalData() error = %v", err)
	}

	for _, want := range []string{
		`<state xmlns="urn:arca:router:config:1.0">`,
		"<routing-instances>",
		"<instance>",
		"<name>BLUE</name>",
		"<instance-type>vrf</instance-type>",
		"<route-distinguisher>65000:100</route-distinguisher>",
		"<ipv4-table-id>100</ipv4-table-id>",
		"<ipv6-table-id>100</ipv6-table-id>",
		"<import-target>target:65000:100</import-target>",
		"<import-target>target:65000:101</import-target>",
		"<export-target>target:65000:100</export-target>",
		"<export-target>target:65000:102</export-target>",
		"<import-policy>BLUE-IN</import-policy>",
		"<export-policy>BLUE-OUT</export-policy>",
		"<interface>ge-0/0/0</interface>",
		"<interface>ge-0/0/1</interface>",
	} {
		if !bytes.Contains(data, []byte(want)) {
			t.Fatalf("operational data missing %q:\n%s", want, data)
		}
	}
	if bytes.Index(data, []byte("<interface>ge-0/0/0</interface>")) > bytes.Index(data, []byte("<interface>ge-0/0/1</interface>")) {
		t.Fatalf("routing-instance interfaces are not sorted:\n%s", data)
	}
	if bytes.Count(data, []byte("<interface>ge-0/0/1</interface>")) != 1 {
		t.Fatalf("routing-instance interfaces are not deduplicated:\n%s", data)
	}
}
