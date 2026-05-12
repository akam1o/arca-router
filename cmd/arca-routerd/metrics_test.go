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
)

func TestMetricsEndpointExportsRouterMetrics(t *testing.T) {
	eng := engine.NewEngine(nil, slog.Default())
	eng.InitializeRunning(model.NewRouterConfig(), 42)

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	metricsSource{
		startedAt: time.Now(),
		engine:    eng,
	}.handleMetrics(rec, req)

	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	text := string(body)
	for _, want := range []string{
		"arca_routerd_up 1",
		"arca_router_config_version 42",
		"arca_router_netconf_listening 0",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("/metrics missing %q:\n%s", want, text)
		}
	}
}
