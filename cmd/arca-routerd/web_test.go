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
	pkgconfig "github.com/akam1o/arca-router/pkg/config"
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

func TestWebConfigEndpoint(t *testing.T) {
	eng := engine.NewEngine(nil, slog.Default())
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{HostName: "edge01"}
	eng.InitializeRunning(cfg, 42)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rec := httptest.NewRecorder()
	metricsSource{
		startedAt: time.Now().Add(-2 * time.Minute),
		engine:    eng,
	}.handleWebConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var cfgResp webConfig
	if err := json.NewDecoder(rec.Result().Body).Decode(&cfgResp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if cfgResp.Version != 42 {
		t.Fatalf("Version = %d, want 42", cfgResp.Version)
	}
	if !strings.Contains(cfgResp.ConfigText, "set system host-name edge01") {
		t.Fatalf("ConfigText missing hostname:\n%s", cfgResp.ConfigText)
	}
}

func TestWebEndpointRequiresAuthWhenPasswordUsersConfigured(t *testing.T) {
	source := newWebAuthTestSource(t, "monitor", "secret", "read-only")

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	source.handleWebStatus(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != webAuthRealm {
		t.Fatalf("WWW-Authenticate = %q, want %q", got, webAuthRealm)
	}
}

func TestWebEndpointAcceptsReadOnlyBasicAuth(t *testing.T) {
	source := newWebAuthTestSource(t, "monitor", "secret", "read-only")

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.SetBasicAuth("monitor", "secret")
	rec := httptest.NewRecorder()
	source.handleWebConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var cfgResp webConfig
	if err := json.NewDecoder(rec.Result().Body).Decode(&cfgResp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !strings.Contains(cfgResp.ConfigText, "set system host-name edge01") {
		t.Fatalf("ConfigText missing hostname:\n%s", cfgResp.ConfigText)
	}
}

func TestWebEndpointRejectsInvalidRole(t *testing.T) {
	source := newWebAuthTestSource(t, "monitor", "secret", "invalid")

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.SetBasicAuth("monitor", "secret")
	rec := httptest.NewRecorder()
	source.handleWebStatus(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
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
	for _, want := range []string{"edge01", "Config version", "NETCONF", "Datastore", "Cluster sync", "Running configuration", "set system host-name edge01", "/api/status", "/api/config"} {
		if !strings.Contains(text, want) {
			t.Fatalf("index missing %q:\n%s", want, text)
		}
	}
}

func newWebAuthTestSource(t *testing.T, username, password, role string) metricsSource {
	t.Helper()
	hash, err := pkgconfig.NormalizePasswordForStorage(password)
	if err != nil {
		t.Fatalf("NormalizePasswordForStorage() error = %v", err)
	}
	eng := engine.NewEngine(nil, slog.Default())
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{HostName: "edge01"}
	cfg.Security = &model.SecurityConfig{
		Users: map[string]*model.UserConfig{
			username: {
				Password: hash,
				Role:     role,
			},
		},
	}
	eng.InitializeRunning(cfg, 42)
	return metricsSource{
		startedAt: time.Now().Add(-2 * time.Minute),
		engine:    eng,
	}
}
