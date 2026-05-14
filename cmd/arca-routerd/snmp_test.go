package main

import (
	"log/slog"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	sbfrr "github.com/akam1o/arca-router/internal/southbound/frr"
	sbvpp "github.com/akam1o/arca-router/internal/southbound/vpp"
	"github.com/akam1o/arca-router/pkg/datastore"
)

func TestEffectiveSNMPListenUsesFlagOverride(t *testing.T) {
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{
		Services: &model.SystemServicesConfig{
			SNMP: &model.SNMPConfig{
				Enabled:       true,
				ListenAddress: "127.0.0.1",
				Port:          1161,
			},
		},
	}

	got := effectiveSNMPListen(":2161", model.NewSnapshot(cfg, 1, "test", "test"))
	if got != ":2161" {
		t.Fatalf("effectiveSNMPListen() = %q, want %q", got, ":2161")
	}
}

func TestEffectiveSNMPListenUsesConfig(t *testing.T) {
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{
		Services: &model.SystemServicesConfig{
			SNMP: &model.SNMPConfig{
				Enabled:       true,
				ListenAddress: "127.0.0.1",
				Port:          1161,
			},
		},
	}

	got := effectiveSNMPListen("", model.NewSnapshot(cfg, 1, "test", "test"))
	if got != "127.0.0.1:1161" {
		t.Fatalf("effectiveSNMPListen() = %q, want %q", got, "127.0.0.1:1161")
	}
}

func TestEffectiveSNMPListenUsesConfigDefaults(t *testing.T) {
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{
		Services: &model.SystemServicesConfig{
			SNMP: &model.SNMPConfig{Enabled: true},
		},
	}

	got := effectiveSNMPListen("", model.NewSnapshot(cfg, 1, "test", "test"))
	if got != "127.0.0.1:161" {
		t.Fatalf("effectiveSNMPListen() = %q, want %q", got, "127.0.0.1:161")
	}
}

func TestEffectiveSNMPCommunityUsesConfig(t *testing.T) {
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{
		Services: &model.SystemServicesConfig{
			SNMP: &model.SNMPConfig{
				Enabled:   true,
				Community: "monitoring",
			},
		},
	}

	got := effectiveSNMPCommunity("", model.NewSnapshot(cfg, 1, "test", "test"))
	if got != "monitoring" {
		t.Fatalf("effectiveSNMPCommunity() = %q, want %q", got, "monitoring")
	}
}

func TestEffectiveSNMPCommunityUsesDefault(t *testing.T) {
	if got := effectiveSNMPCommunity("", nil); got != "public" {
		t.Fatalf("effectiveSNMPCommunity() = %q, want public", got)
	}
}

func TestSNMPEndpointExportsRouterMetrics(t *testing.T) {
	oldVersion := Version
	Version = "test-version"
	t.Cleanup(func() { Version = oldVersion })

	eng := engine.NewEngine(nil, slog.Default())
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{HostName: "router-snmp"}
	cfg.Chassis = &model.ChassisConfig{
		Cluster: &model.ClusterConfig{
			Enabled: true,
			Nodes: map[string]*model.ClusterNode{
				"node0": {Address: "192.0.2.10"},
				"node1": {Address: "192.0.2.11"},
			},
			Sync: &model.ClusterSyncConfig{
				Etcd: &model.EtcdSyncConfig{Endpoints: []string{"https://etcd1:2379"}},
			},
		},
	}
	cfg.Protocols = &model.ProtocolsConfig{
		VRRP: &model.VRRPConfig{Groups: map[string]*model.VRRPGroup{
			"10": {Interface: "ge-0/0/0", VirtualAddress: "192.0.2.1", Priority: 110, Preempt: true},
		}},
	}
	cfg.ClassOfService = &model.ClassOfServiceConfig{
		ForwardingClasses: map[string]*model.ForwardingClass{
			"best-effort":          {Queue: 0},
			"expedited-forwarding": {Queue: 5},
		},
		TrafficControlProfiles: map[string]*model.TrafficControlProfile{
			"WAN": {ShapingRate: 1000000000, SchedulerMap: "WAN-SCHED"},
		},
		Interfaces: map[string]*model.CoSInterface{
			"ge-0/0/0": {OutputTrafficControlProfile: "WAN"},
		},
	}
	eng.InitializeRunning(cfg, 42)

	server := newSNMPServer(metricsSource{
		startedAt: time.Now().Add(-1 * time.Second),
		engine:    eng,
		datastore: &datastore.Config{
			Backend:       datastore.BackendEtcd,
			EtcdEndpoints: []string{"https://etcd1:2379"},
		},
		configSync: fakeConfigSyncRuntimeSource{status: configSyncStatus{
			Enabled:         true,
			Healthy:         true,
			EtcdRevision:    123,
			RunningRevision: 120,
			RunningCommitID: "commit-120",
			LastCheck:       time.Unix(1700000100, 0),
			LastApply:       time.Unix(1700000200, 0),
		}},
		frr: fakeFRRVRRPSource{
			vrrpStatus: sbfrr.VRRPOperationalStatus{
				LastRun:          time.Unix(1700000300, 0),
				ConfiguredGroups: 1,
				ObservedGroups:   1,
				ActiveGroups:     1,
			},
			bfdStatus: sbfrr.BFDOperationalStatus{
				LastRun:           time.Unix(1700000400, 0),
				ConfiguredPeers:   1,
				ObservedPeers:     1,
				UpPeers:           1,
				SessionDownEvents: 2,
				RxFailPackets:     1,
			},
		},
		vpp: fakeVPPReconciliationSource{status: sbvpp.LCPReconciliationStatus{
			LastRun:   time.Unix(1700000000, 0),
			PairCount: 2,
		}},
	}, "test-community")
	if err := server.ListenUDP("udp4", "127.0.0.1:0"); err != nil {
		t.Fatalf("ListenUDP() error = %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- server.ServeForever()
	}()
	t.Cleanup(func() {
		server.Shutdown()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("ServeForever() error = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("SNMP server did not stop")
		}
	})

	host, portText, err := net.SplitHostPort(server.Address().String())
	if err != nil {
		t.Fatalf("SplitHostPort() error = %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("Atoi(%q) error = %v", portText, err)
	}

	client := &gosnmp.GoSNMP{
		Target:    host,
		Port:      uint16(port),
		Community: "test-community",
		Version:   gosnmp.Version2c,
		Timeout:   time.Second,
		Retries:   1,
	}
	if err := client.Connect(); err != nil {
		t.Fatalf("SNMP Connect() error = %v", err)
	}
	t.Cleanup(func() {
		if client.Conn != nil {
			_ = client.Conn.Close()
		}
	})

	packet, err := client.Get([]string{
		snmpOIDConfigVersion,
		snmpOIDSysName,
		snmpOIDDaemonVersion,
		snmpOIDVPPLCPPairs,
		snmpOIDVPPLCPMismatch,
		snmpOIDVPPLCPError,
		snmpOIDVPPLCPLastRun,
		snmpOIDHAConfigured,
		snmpOIDHAConverged,
		snmpOIDHAVRPGroups,
		snmpOIDHAIssues,
		snmpOIDFRRVRRPConfigured,
		snmpOIDFRRVRRPObserved,
		snmpOIDFRRVRRPActive,
		snmpOIDFRRVRRPIssues,
		snmpOIDFRRVRRPError,
		snmpOIDFRRVRRPLastRun,
		snmpOIDCoSConfigured,
		snmpOIDCoSClasses,
		snmpOIDCoSProfiles,
		snmpOIDCoSBindings,
		snmpOIDCoSIntentOnly,
		snmpOIDFRRBFDConfigured,
		snmpOIDFRRBFDObserved,
		snmpOIDFRRBFDUp,
		snmpOIDFRRBFDDownPeers,
		snmpOIDFRRBFDSessionDown,
		snmpOIDFRRBFDRxFail,
		snmpOIDFRRBFDIssues,
		snmpOIDFRRBFDError,
		snmpOIDFRRBFDLastRun,
	})
	if err != nil {
		t.Fatalf("SNMP Get() error = %v", err)
	}
	if len(packet.Variables) != 31 {
		t.Fatalf("SNMP variables = %d, want 31", len(packet.Variables))
	}
	if got := snmpUintValue(t, packet.Variables[0].Value); got != 42 {
		t.Fatalf("%s = %d, want 42", snmpOIDConfigVersion, got)
	}
	if got := string(packet.Variables[1].Value.([]byte)); got != "router-snmp" {
		t.Fatalf("%s = %q, want router-snmp", snmpOIDSysName, got)
	}
	if got := string(packet.Variables[2].Value.([]byte)); got != "test-version" {
		t.Fatalf("%s = %q, want test-version", snmpOIDDaemonVersion, got)
	}
	if got := snmpUintValue(t, packet.Variables[3].Value); got != 2 {
		t.Fatalf("%s = %d, want 2", snmpOIDVPPLCPPairs, got)
	}
	if got := snmpUintValue(t, packet.Variables[4].Value); got != 0 {
		t.Fatalf("%s = %d, want 0", snmpOIDVPPLCPMismatch, got)
	}
	if got := snmpUintValue(t, packet.Variables[5].Value); got != 0 {
		t.Fatalf("%s = %d, want 0", snmpOIDVPPLCPError, got)
	}
	if got := snmpUintValue(t, packet.Variables[6].Value); got != 1700000000 {
		t.Fatalf("%s = %d, want 1700000000", snmpOIDVPPLCPLastRun, got)
	}
	if got := snmpUintValue(t, packet.Variables[7].Value); got != 1 {
		t.Fatalf("%s = %d, want 1", snmpOIDHAConfigured, got)
	}
	if got := snmpUintValue(t, packet.Variables[8].Value); got != 1 {
		t.Fatalf("%s = %d, want 1", snmpOIDHAConverged, got)
	}
	if got := snmpUintValue(t, packet.Variables[9].Value); got != 1 {
		t.Fatalf("%s = %d, want 1", snmpOIDHAVRPGroups, got)
	}
	if got := snmpUintValue(t, packet.Variables[10].Value); got != 0 {
		t.Fatalf("%s = %d, want 0", snmpOIDHAIssues, got)
	}
	if got := snmpUintValue(t, packet.Variables[11].Value); got != 1 {
		t.Fatalf("%s = %d, want 1", snmpOIDFRRVRRPConfigured, got)
	}
	if got := snmpUintValue(t, packet.Variables[12].Value); got != 1 {
		t.Fatalf("%s = %d, want 1", snmpOIDFRRVRRPObserved, got)
	}
	if got := snmpUintValue(t, packet.Variables[13].Value); got != 1 {
		t.Fatalf("%s = %d, want 1", snmpOIDFRRVRRPActive, got)
	}
	if got := snmpUintValue(t, packet.Variables[14].Value); got != 0 {
		t.Fatalf("%s = %d, want 0", snmpOIDFRRVRRPIssues, got)
	}
	if got := snmpUintValue(t, packet.Variables[15].Value); got != 0 {
		t.Fatalf("%s = %d, want 0", snmpOIDFRRVRRPError, got)
	}
	if got := snmpUintValue(t, packet.Variables[16].Value); got != 1700000300 {
		t.Fatalf("%s = %d, want 1700000300", snmpOIDFRRVRRPLastRun, got)
	}
	if got := snmpUintValue(t, packet.Variables[17].Value); got != 1 {
		t.Fatalf("%s = %d, want 1", snmpOIDCoSConfigured, got)
	}
	if got := snmpUintValue(t, packet.Variables[18].Value); got != 2 {
		t.Fatalf("%s = %d, want 2", snmpOIDCoSClasses, got)
	}
	if got := snmpUintValue(t, packet.Variables[19].Value); got != 1 {
		t.Fatalf("%s = %d, want 1", snmpOIDCoSProfiles, got)
	}
	if got := snmpUintValue(t, packet.Variables[20].Value); got != 1 {
		t.Fatalf("%s = %d, want 1", snmpOIDCoSBindings, got)
	}
	if got := snmpUintValue(t, packet.Variables[21].Value); got != 1 {
		t.Fatalf("%s = %d, want 1", snmpOIDCoSIntentOnly, got)
	}
	if got := snmpUintValue(t, packet.Variables[22].Value); got != 1 {
		t.Fatalf("%s = %d, want 1", snmpOIDFRRBFDConfigured, got)
	}
	if got := snmpUintValue(t, packet.Variables[23].Value); got != 1 {
		t.Fatalf("%s = %d, want 1", snmpOIDFRRBFDObserved, got)
	}
	if got := snmpUintValue(t, packet.Variables[24].Value); got != 1 {
		t.Fatalf("%s = %d, want 1", snmpOIDFRRBFDUp, got)
	}
	if got := snmpUintValue(t, packet.Variables[25].Value); got != 0 {
		t.Fatalf("%s = %d, want 0", snmpOIDFRRBFDDownPeers, got)
	}
	if got := snmpUintValue(t, packet.Variables[26].Value); got != 2 {
		t.Fatalf("%s = %d, want 2", snmpOIDFRRBFDSessionDown, got)
	}
	if got := snmpUintValue(t, packet.Variables[27].Value); got != 1 {
		t.Fatalf("%s = %d, want 1", snmpOIDFRRBFDRxFail, got)
	}
	if got := snmpUintValue(t, packet.Variables[28].Value); got != 0 {
		t.Fatalf("%s = %d, want 0", snmpOIDFRRBFDIssues, got)
	}
	if got := snmpUintValue(t, packet.Variables[29].Value); got != 0 {
		t.Fatalf("%s = %d, want 0", snmpOIDFRRBFDError, got)
	}
	if got := snmpUintValue(t, packet.Variables[30].Value); got != 1700000400 {
		t.Fatalf("%s = %d, want 1700000400", snmpOIDFRRBFDLastRun, got)
	}
}

func TestStartSNMPServerRejectsEmptyCommunity(t *testing.T) {
	if _, err := startSNMPServer(t.Context(), "127.0.0.1:0", "", metricsSource{}, nil); err == nil {
		t.Fatal("startSNMPServer() error = nil, want empty community error")
	}
}

func snmpUintValue(t *testing.T, value interface{}) uint64 {
	t.Helper()
	switch v := value.(type) {
	case int:
		return uint64(v)
	case uint:
		return uint64(v)
	case uint32:
		return uint64(v)
	case uint64:
		return v
	default:
		t.Fatalf("unexpected SNMP value type %T", value)
		return 0
	}
}
