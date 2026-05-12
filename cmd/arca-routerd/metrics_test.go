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
	"github.com/akam1o/arca-router/pkg/datastore"
)

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
		"arca_router_netconf_listening 0",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("/metrics missing %q:\n%s", want, text)
		}
	}
}
