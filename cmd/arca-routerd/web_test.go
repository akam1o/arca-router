package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	"github.com/akam1o/arca-router/pkg/datastore"
)

func TestEffectiveWebListenUsesFlagOverride(t *testing.T) {
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{
		Services: &model.SystemServicesConfig{
			WebUI: &model.WebUIConfig{
				Enabled:       true,
				ListenAddress: "127.0.0.1",
				Port:          8443,
			},
		},
	}

	got := effectiveWebListen(":9000", model.NewSnapshot(cfg, 1, "test", "test"))
	if got != ":9000" {
		t.Fatalf("effectiveWebListen() = %q, want %q", got, ":9000")
	}
}

func TestEffectiveWebListenUsesConfig(t *testing.T) {
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{
		Services: &model.SystemServicesConfig{
			WebUI: &model.WebUIConfig{
				Enabled:       true,
				ListenAddress: "127.0.0.1",
				Port:          8443,
			},
		},
	}

	got := effectiveWebListen("", model.NewSnapshot(cfg, 1, "test", "test"))
	if got != "127.0.0.1:8443" {
		t.Fatalf("effectiveWebListen() = %q, want %q", got, "127.0.0.1:8443")
	}
}

func TestEffectiveWebListenUsesConfigDefaults(t *testing.T) {
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{
		Services: &model.SystemServicesConfig{
			WebUI: &model.WebUIConfig{Enabled: true},
		},
	}

	got := effectiveWebListen("", model.NewSnapshot(cfg, 1, "test", "test"))
	if got != "127.0.0.1:8080" {
		t.Fatalf("effectiveWebListen() = %q, want %q", got, "127.0.0.1:8080")
	}
}

func TestWebStatusEndpoint(t *testing.T) {
	eng := engine.NewEngine(nil, slog.Default())
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{HostName: "edge01"}
	cfg.Chassis = &model.ChassisConfig{
		Cluster: &model.ClusterConfig{
			Enabled: true,
			Nodes: map[string]*model.ClusterNode{
				"node0": {Address: "192.0.2.10"},
			},
			Sync: &model.ClusterSyncConfig{
				Etcd: &model.EtcdSyncConfig{Endpoints: []string{"https://etcd1:2379"}},
			},
		},
	}
	eng.InitializeRunning(cfg, 42)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	metricsSource{
		startedAt: time.Now().Add(-2 * time.Minute),
		engine:    eng,
		datastore: &datastore.Config{
			Backend:       datastore.BackendEtcd,
			EtcdEndpoints: []string{"https://etcd1:2379"},
		},
	}.handleWebStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var status webStatus
	if err := json.NewDecoder(rec.Result().Body).Decode(&status); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if status.ConfigVersion != 42 {
		t.Fatalf("ConfigVersion = %d, want 42", status.ConfigVersion)
	}
	if status.RunningHostname != "edge01" {
		t.Fatalf("RunningHostname = %q, want edge01", status.RunningHostname)
	}
	if status.UptimeSeconds <= 0 {
		t.Fatalf("UptimeSeconds = %f, want positive", status.UptimeSeconds)
	}
	if status.Datastore.Backend != "etcd" {
		t.Fatalf("Datastore.Backend = %q, want etcd", status.Datastore.Backend)
	}
	if !status.Cluster.Enabled || status.Cluster.NodeCount != 1 || !status.Cluster.EtcdSyncConfigured || !status.Cluster.SyncAligned {
		t.Fatalf("Cluster status = %#v, want enabled aligned etcd sync", status.Cluster)
	}
}

func TestWebIndexEndpoint(t *testing.T) {
	eng := engine.NewEngine(nil, slog.Default())
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{HostName: "edge01"}
	eng.InitializeRunning(cfg, 42)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	metricsSource{
		startedAt: time.Now().Add(-2 * time.Minute),
		engine:    eng,
	}.handleWebIndex(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	text := string(body)
	for _, want := range []string{"edge01", "Config version", "NETCONF", "Datastore", "Cluster sync", "/api/status"} {
		if !strings.Contains(text, want) {
			t.Fatalf("index missing %q:\n%s", want, text)
		}
	}
}
