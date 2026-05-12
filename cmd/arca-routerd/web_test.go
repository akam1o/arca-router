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
	eng.InitializeRunning(cfg, 42)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	metricsSource{
		startedAt: time.Now().Add(-2 * time.Minute),
		engine:    eng,
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
	for _, want := range []string{"edge01", "Config version", "NETCONF", "/api/status"} {
		if !strings.Contains(text, want) {
			t.Fatalf("index missing %q:\n%s", want, text)
		}
	}
}
