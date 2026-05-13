package main

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	sbvpp "github.com/akam1o/arca-router/internal/southbound/vpp"
	"github.com/akam1o/arca-router/pkg/datastore"
)

type fakeVPPReconciliationSource struct {
	status sbvpp.LCPReconciliationStatus
}

func (s fakeVPPReconciliationSource) LCPReconciliationStatus() sbvpp.LCPReconciliationStatus {
	return s.status
}

func TestEffectiveMetricsListenUsesFlagOverride(t *testing.T) {
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{
		Services: &model.SystemServicesConfig{
			Prometheus: &model.PrometheusConfig{
				Enabled:       true,
				ListenAddress: "127.0.0.1",
				Port:          9090,
			},
		},
	}

	got := effectiveMetricsListen(":19090", model.NewSnapshot(cfg, 1, "test", "test"))
	if got != ":19090" {
		t.Fatalf("effectiveMetricsListen() = %q, want %q", got, ":19090")
	}
}

func TestEffectiveMetricsListenUsesConfig(t *testing.T) {
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{
		Services: &model.SystemServicesConfig{
			Prometheus: &model.PrometheusConfig{
				Enabled:       true,
				ListenAddress: "127.0.0.1",
				Port:          9090,
			},
		},
	}

	got := effectiveMetricsListen("", model.NewSnapshot(cfg, 1, "test", "test"))
	if got != "127.0.0.1:9090" {
		t.Fatalf("effectiveMetricsListen() = %q, want %q", got, "127.0.0.1:9090")
	}
}

func TestEffectiveMetricsListenUsesConfigDefaults(t *testing.T) {
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{
		Services: &model.SystemServicesConfig{
			Prometheus: &model.PrometheusConfig{Enabled: true},
		},
	}

	got := effectiveMetricsListen("", model.NewSnapshot(cfg, 1, "test", "test"))
	if got != "127.0.0.1:9090" {
		t.Fatalf("effectiveMetricsListen() = %q, want %q", got, "127.0.0.1:9090")
	}
}

func TestMetricsEndpointExportsRouterMetrics(t *testing.T) {
	eng := engine.NewEngine(nil, slog.Default())
	cfg := model.NewRouterConfig()
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
	eng.InitializeRunning(cfg, 42)

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	metricsSource{
		startedAt: time.Now(),
		engine:    eng,
		datastore: &datastore.Config{
			Backend:       datastore.BackendEtcd,
			EtcdEndpoints: []string{"https://etcd1:2379"},
		},
		vpp: fakeVPPReconciliationSource{status: sbvpp.LCPReconciliationStatus{
			LastRun:         time.Unix(1700000000, 0),
			PairCount:       2,
			Inconsistencies: []string{"Interface 7 exists in VPP but not in cache"},
		}},
	}.handleMetrics(rec, req)

	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	text := string(body)
	for _, want := range []string{
		"arca_routerd_up 1",
		"arca_router_config_version 42",
		"arca_router_cluster_enabled 1",
		"arca_router_cluster_nodes 2",
		"arca_router_cluster_sync_etcd_configured 1",
		"arca_router_cluster_sync_aligned 1",
		"arca_router_vpp_lcp_pairs 2",
		"arca_router_vpp_lcp_inconsistencies 1",
		"arca_router_vpp_lcp_reconcile_error 0",
		"arca_router_vpp_lcp_last_reconcile_timestamp_seconds 1700000000",
		"arca_router_netconf_listening 0",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("/metrics missing %q:\n%s", want, text)
		}
	}
}
