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
	sbvpp "github.com/akam1o/arca-router/internal/southbound/vpp"
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
	eng.InitializeRunning(cfg, 42)

	server := newSNMPServer(metricsSource{
		startedAt: time.Now().Add(-1 * time.Second),
		engine:    eng,
		vpp: fakeVPPReconciliationSource{status: sbvpp.LCPReconciliationStatus{
			LastRun:         time.Unix(1700000000, 0),
			PairCount:       2,
			Inconsistencies: []string{"Interface 7 exists in VPP but not in cache"},
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
	})
	if err != nil {
		t.Fatalf("SNMP Get() error = %v", err)
	}
	if len(packet.Variables) != 7 {
		t.Fatalf("SNMP variables = %d, want 7", len(packet.Variables))
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
	if got := snmpUintValue(t, packet.Variables[4].Value); got != 1 {
		t.Fatalf("%s = %d, want 1", snmpOIDVPPLCPMismatch, got)
	}
	if got := snmpUintValue(t, packet.Variables[5].Value); got != 0 {
		t.Fatalf("%s = %d, want 0", snmpOIDVPPLCPError, got)
	}
	if got := snmpUintValue(t, packet.Variables[6].Value); got != 1700000000 {
		t.Fatalf("%s = %d, want 1700000000", snmpOIDVPPLCPLastRun, got)
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
