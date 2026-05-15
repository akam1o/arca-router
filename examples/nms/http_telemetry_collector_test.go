package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestParseCollectorConfigDefaults(t *testing.T) {
	cfg, err := parseCollectorConfig(nil)
	if err != nil {
		t.Fatalf("parseCollectorConfig() error = %v", err)
	}
	if cfg.baseURL != defaultBaseURL || cfg.mode != "snapshot" {
		t.Fatalf("defaults = base %q mode %q, want %q snapshot", cfg.baseURL, cfg.mode, defaultBaseURL)
	}
	if cfg.timeout != defaultSnapshotTimeout || cfg.maxPayloadBytes != defaultMaxPayloadBytes {
		t.Fatalf("snapshot defaults = timeout %v max %d", cfg.timeout, cfg.maxPayloadBytes)
	}
	if len(cfg.paths) != len(defaultSnapshotPaths) {
		t.Fatalf("default paths = %#v, want %#v", cfg.paths, defaultSnapshotPaths)
	}
}

func TestParseCollectorConfigCardinalityFilters(t *testing.T) {
	cfg, err := parseCollectorConfig([]string{
		"-discover-paths",
		"-exclude-cardinality", "per-route",
		"-exclude-cardinality", "per-peer",
		"-exclude-payload-schema", "arca.telemetry.bfd.v1",
	})
	if err != nil {
		t.Fatalf("parseCollectorConfig() error = %v", err)
	}
	if !cfg.discoverPaths {
		t.Fatal("discoverPaths = false, want true")
	}
	if len(cfg.paths) != 0 {
		t.Fatalf("paths = %#v, want catalog-discovered paths", cfg.paths)
	}
	if len(cfg.excludedCard) != 2 || cfg.excludedCard[0] != "per-route" || cfg.excludedCard[1] != "per-peer" {
		t.Fatalf("excluded cardinalities = %#v, want per-route and per-peer", cfg.excludedCard)
	}
	if len(cfg.excludedSchema) != 1 || cfg.excludedSchema[0] != "arca.telemetry.bfd.v1" {
		t.Fatalf("excluded schemas = %#v, want BFD payload schema", cfg.excludedSchema)
	}
}

func TestCollectorEndpointURLForSnapshot(t *testing.T) {
	cfg, err := parseCollectorConfig([]string{
		"-base-url", "http://router.example:8080/arca",
		"-path", "/system",
		"-path", "/overlays/evpn",
		"-timeout", "7s",
		"-max-payload-bytes", "12345",
	})
	if err != nil {
		t.Fatalf("parseCollectorConfig() error = %v", err)
	}
	got, err := collectorEndpointURL(cfg)
	if err != nil {
		t.Fatalf("collectorEndpointURL() error = %v", err)
	}
	if !strings.HasPrefix(got, "http://router.example:8080/arca/api/nms/v1/telemetry/snapshot?") {
		t.Fatalf("snapshot URL = %q, want endpoint under /arca", got)
	}
	for _, want := range []string{
		"path=%2Fsystem",
		"path=%2Foverlays%2Fevpn",
		"timeout=7s",
		"max_payload_bytes=12345",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("snapshot URL = %q, missing %s", got, want)
		}
	}
}

func TestFetchNMSDiscoversAndFiltersSnapshotPaths(t *testing.T) {
	var snapshotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/nms/v1/telemetry/paths":
			_, _ = w.Write([]byte(`{"paths":[` +
				`{"path":"/system","cardinality":"single","payload_schema":"arca.telemetry.system.v1"},` +
				`{"path":"/interfaces","cardinality":"per-interface","payload_schema":"arca.telemetry.interfaces.v1"},` +
				`{"path":"/routes","cardinality":"per-route","payload_schema":"arca.telemetry.routes.v1"},` +
				`{"path":"/overlays/evpn","cardinality":"per-vni","payload_schema":"arca.telemetry.overlays.evpn.v1"},` +
				`{"path":"/bfd","cardinality":"per-peer","payload_schema":"arca.telemetry.bfd.v1"}` +
				`]}`))
		case "/api/nms/v1/telemetry/snapshot":
			snapshotQuery = r.URL.Query()
			_, _ = w.Write([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","events":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg, err := parseCollectorConfig([]string{
		"-base-url", server.URL,
		"-discover-paths",
		"-exclude-cardinality", "per-route",
		"-exclude-payload-schema", "arca.telemetry.bfd.v1",
	})
	if err != nil {
		t.Fatalf("parseCollectorConfig() error = %v", err)
	}
	body, err := fetchNMS(t.Context(), server.Client(), cfg)
	if err != nil {
		t.Fatalf("fetchNMS() error = %v", err)
	}
	if !json.Valid(body) {
		t.Fatalf("fetchNMS() body is invalid JSON: %s", string(body))
	}
	gotPaths := snapshotQuery["path"]
	wantPaths := []string{"/system", "/interfaces", "/overlays/evpn"}
	if strings.Join(gotPaths, ",") != strings.Join(wantPaths, ",") {
		t.Fatalf("snapshot paths = %#v, want %#v", gotPaths, wantPaths)
	}
}

func TestFilterSnapshotPathsByCardinality(t *testing.T) {
	catalog := telemetryCatalogResponse{Paths: []telemetryCatalogPath{
		{Path: "/system", Cardinality: "single"},
		{Path: "/routes", Cardinality: "per-route"},
		{Path: "/bfd", Cardinality: "per-peer"},
	}}
	got := filterSnapshotPathsByCardinality(
		repeatedPathFlag{"/system", "/routes", "/bfd"},
		catalog,
		repeatedStringFlag{"per-route", "per-peer"},
	)
	if len(got) != 1 || got[0] != "/system" {
		t.Fatalf("filterSnapshotPathsByCardinality() = %#v, want only /system", got)
	}
}

func TestFilterSnapshotPathsByPayloadSchema(t *testing.T) {
	catalog := telemetryCatalogResponse{Paths: []telemetryCatalogPath{
		{Path: "/system", PayloadSchema: "arca.telemetry.system.v1"},
		{Path: "/routes", PayloadSchema: "arca.telemetry.routes.v1"},
		{Path: "/bfd", PayloadSchema: "arca.telemetry.bfd.v1"},
	}}
	got := filterSnapshotPathsByPayloadSchema(
		repeatedPathFlag{"/system", "/routes", "/bfd"},
		catalog,
		repeatedStringFlag{"arca.telemetry.routes.v1", "ARCA.TELEMETRY.BFD.V1"},
	)
	if len(got) != 1 || got[0] != "/system" {
		t.Fatalf("filterSnapshotPathsByPayloadSchema() = %#v, want only /system", got)
	}
}

func TestResolveSnapshotPathsRejectsEmptyFilteredSet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"paths":[{"path":"/routes","cardinality":"per-route","payload_schema":"arca.telemetry.routes.v1"}]}`))
	}))
	defer server.Close()

	cfg, err := parseCollectorConfig([]string{
		"-base-url", server.URL,
		"-discover-paths",
		"-exclude-cardinality", "per-route",
	})
	if err != nil {
		t.Fatalf("parseCollectorConfig() error = %v", err)
	}
	_, err = resolveSnapshotPaths(t.Context(), server.Client(), cfg)
	if err == nil || !strings.Contains(err.Error(), "snapshot path set is empty") {
		t.Fatalf("resolveSnapshotPaths() error = %v, want empty filtered set", err)
	}
}

func TestCollectorEndpointURLForStatusAndCatalog(t *testing.T) {
	tests := []struct {
		mode string
		want string
	}{
		{mode: "status", want: "http://127.0.0.1:8080/api/nms/v1/status"},
		{mode: "catalog", want: "http://127.0.0.1:8080/api/nms/v1/telemetry/paths"},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			cfg, err := parseCollectorConfig([]string{"-mode", tt.mode})
			if err != nil {
				t.Fatalf("parseCollectorConfig() error = %v", err)
			}
			got, err := collectorEndpointURL(cfg)
			if err != nil {
				t.Fatalf("collectorEndpointURL() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("collectorEndpointURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseCollectorConfigRejectsInvalidValues(t *testing.T) {
	tests := [][]string{
		{"-mode", "bad"},
		{"-timeout", "0s"},
		{"-max-payload-bytes", "0"},
		{"-path", ""},
	}
	for _, args := range tests {
		if _, err := parseCollectorConfig(args); err == nil {
			t.Fatalf("parseCollectorConfig(%v) error = nil, want error", args)
		}
	}
}

func TestEndpointURLRejectsMissingHost(t *testing.T) {
	if _, err := endpointURL("127.0.0.1:8080", "/api/nms/v1/status"); err == nil {
		t.Fatal("endpointURL() error = nil, want missing scheme/host error")
	}
}

func TestWritePrettyJSON(t *testing.T) {
	var out bytes.Buffer
	if err := writePrettyJSON(&out, []byte(`{"schema_version":"test","events":[]}`)); err != nil {
		t.Fatalf("writePrettyJSON() error = %v", err)
	}
	if !json.Valid(out.Bytes()) {
		t.Fatalf("writePrettyJSON() output is invalid JSON: %q", out.String())
	}
	if !strings.Contains(out.String(), "\n  \"schema_version\": \"test\"") {
		t.Fatalf("writePrettyJSON() output = %q, want indented schema_version", out.String())
	}
}

func TestRequestTimeoutIncludesSnapshotBudget(t *testing.T) {
	cfg := collectorConfig{mode: "snapshot", timeout: 2 * time.Second}
	if got := requestTimeout(cfg); got != 12*time.Second {
		t.Fatalf("requestTimeout(snapshot) = %v, want 12s", got)
	}
	if got := requestTimeout(collectorConfig{mode: "status"}); got != 10*time.Second {
		t.Fatalf("requestTimeout(status) = %v, want 10s", got)
	}
}
