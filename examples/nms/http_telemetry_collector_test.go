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
	if cfg.maxEvents != defaultMaxEvents {
		t.Fatalf("max events = %d, want %d", cfg.maxEvents, defaultMaxEvents)
	}
	if len(cfg.paths) != len(defaultSnapshotPaths) {
		t.Fatalf("default paths = %#v, want %#v", cfg.paths, defaultSnapshotPaths)
	}
}

func TestDecodeTelemetryCatalogResponseIntervalHints(t *testing.T) {
	var catalog telemetryCatalogResponse
	body := []byte(`{"encoding":"json","default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"paths":[]}`)
	if err := json.Unmarshal(body, &catalog); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if catalog.DefaultSampleIntervalMs != 30000 || catalog.MinSampleIntervalMs != 1000 || catalog.MaxSampleIntervalMs != 3600000 {
		t.Fatalf("interval hints = default %d min %d max %d, want 30000 1000 3600000",
			catalog.DefaultSampleIntervalMs,
			catalog.MinSampleIntervalMs,
			catalog.MaxSampleIntervalMs,
		)
	}
}

func TestParseCollectorConfigCatalogFilters(t *testing.T) {
	cfg, err := parseCollectorConfig([]string{
		"-discover-paths",
		"-include-default",
		"-include-path", "evpn",
		"-include-cardinality", "single",
		"-include-payload-schema", "arca.telemetry.system.v1",
		"-include-encoding", "json",
		"-exclude-path", "/routes",
		"-exclude-cardinality", "per-route",
		"-exclude-cardinality", "per-peer",
		"-exclude-payload-schema", "arca.telemetry.bfd.v1",
		"-exclude-encoding", "protobuf",
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
	if len(cfg.includedPath) != 1 || cfg.includedPath[0] != "evpn" {
		t.Fatalf("included paths = %#v, want evpn", cfg.includedPath)
	}
	if !cfg.includedDefault {
		t.Fatal("included default = false, want true")
	}
	if len(cfg.includedCard) != 1 || cfg.includedCard[0] != "single" {
		t.Fatalf("included cardinalities = %#v, want single", cfg.includedCard)
	}
	if len(cfg.includedSchema) != 1 || cfg.includedSchema[0] != "arca.telemetry.system.v1" {
		t.Fatalf("included schemas = %#v, want system payload schema", cfg.includedSchema)
	}
	if len(cfg.includedEncoding) != 1 || cfg.includedEncoding[0] != "json" {
		t.Fatalf("included encodings = %#v, want json", cfg.includedEncoding)
	}
	if len(cfg.excludedPath) != 1 || cfg.excludedPath[0] != "/routes" {
		t.Fatalf("excluded paths = %#v, want /routes", cfg.excludedPath)
	}
	if len(cfg.excludedCard) != 2 || cfg.excludedCard[0] != "per-route" || cfg.excludedCard[1] != "per-peer" {
		t.Fatalf("excluded cardinalities = %#v, want per-route and per-peer", cfg.excludedCard)
	}
	if len(cfg.excludedSchema) != 1 || cfg.excludedSchema[0] != "arca.telemetry.bfd.v1" {
		t.Fatalf("excluded schemas = %#v, want BFD payload schema", cfg.excludedSchema)
	}
	if len(cfg.excludedEncoding) != 1 || cfg.excludedEncoding[0] != "protobuf" {
		t.Fatalf("excluded encodings = %#v, want protobuf", cfg.excludedEncoding)
	}
}

func TestParseCollectorConfigOTLPExporter(t *testing.T) {
	cfg, err := parseCollectorConfig([]string{
		"-otlp-endpoint", "http://otel.example:4318/v1/logs",
		"-otlp-service-name", "arca-edge01",
	})
	if err != nil {
		t.Fatalf("parseCollectorConfig() error = %v", err)
	}
	if cfg.otlpEndpoint != "http://otel.example:4318/v1/logs" {
		t.Fatalf("otlp endpoint = %q, want configured endpoint", cfg.otlpEndpoint)
	}
	if cfg.otlpServiceName != "arca-edge01" {
		t.Fatalf("otlp service name = %q, want arca-edge01", cfg.otlpServiceName)
	}

	_, err = parseCollectorConfig([]string{
		"-mode", "status",
		"-otlp-endpoint", "http://otel.example:4318/v1/logs",
	})
	if err == nil || !strings.Contains(err.Error(), "otlp export requires snapshot mode") {
		t.Fatalf("parseCollectorConfig(status otlp) error = %v, want snapshot-only error", err)
	}

	_, err = parseCollectorConfig([]string{
		"-otlp-endpoint", "http://otel.example:4318/v1/logs",
		"-otlp-service-name", " ",
	})
	if err == nil || !strings.Contains(err.Error(), "otlp-service-name must not be empty") {
		t.Fatalf("parseCollectorConfig(empty otlp service) error = %v, want service-name error", err)
	}
}

func TestParseCollectorConfigIncludeFiltersUseCatalogPaths(t *testing.T) {
	cfg, err := parseCollectorConfig([]string{
		"-include-encoding", "json",
	})
	if err != nil {
		t.Fatalf("parseCollectorConfig() error = %v", err)
	}
	if len(cfg.paths) != 0 {
		t.Fatalf("paths = %#v, want catalog-discovered paths for include encoding filter", cfg.paths)
	}
}

func TestParseCollectorConfigExcludeFiltersKeepDefaultPaths(t *testing.T) {
	cfg, err := parseCollectorConfig([]string{
		"-exclude-path", "/routes",
		"-exclude-cardinality", "per-route",
	})
	if err != nil {
		t.Fatalf("parseCollectorConfig() error = %v", err)
	}
	if strings.Join(cfg.paths, ",") != strings.Join(defaultSnapshotPaths, ",") {
		t.Fatalf("paths = %#v, want default snapshot paths", cfg.paths)
	}
}

func TestCollectorEndpointURLForSnapshot(t *testing.T) {
	cfg, err := parseCollectorConfig([]string{
		"-base-url", "http://router.example:8080/arca",
		"-path", "/system",
		"-path", "/overlays/evpn",
		"-timeout", "7s",
		"-max-payload-bytes", "12345",
		"-max-events", "9",
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
		"max_events=9",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("snapshot URL = %q, missing %s", got, want)
		}
	}
}

func TestCollectorEndpointURLForCatalogFilters(t *testing.T) {
	cfg, err := parseCollectorConfig([]string{
		"-mode", "catalog",
		"-base-url", "http://router.example:8080/arca",
		"-include-default",
		"-include-path", "evpn",
		"-include-cardinality", "per-route",
		"-include-cardinality", "per-vni",
		"-include-payload-schema", "arca.telemetry.routes.v1",
		"-include-encoding", "json",
	})
	if err != nil {
		t.Fatalf("parseCollectorConfig() error = %v", err)
	}
	got, err := collectorEndpointURL(cfg)
	if err != nil {
		t.Fatalf("collectorEndpointURL() error = %v", err)
	}
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("catalog URL is invalid: %v", err)
	}
	if parsed.Scheme != "http" || parsed.Host != "router.example:8080" || parsed.Path != "/arca/api/nms/v1/telemetry/paths" {
		t.Fatalf("catalog URL = %q, want endpoint under /arca", got)
	}
	query := parsed.Query()
	if query.Get("default") != "true" {
		t.Fatalf("default query = %#v, want true", query["default"])
	}
	if query.Get("path") != "evpn" {
		t.Fatalf("path query = %#v, want evpn", query["path"])
	}
	if strings.Join(query["cardinality"], ",") != "per-route,per-vni" {
		t.Fatalf("cardinality query = %#v, want per-route and per-vni", query["cardinality"])
	}
	if query.Get("payload_schema") != "arca.telemetry.routes.v1" {
		t.Fatalf("payload_schema query = %#v, want routes schema", query["payload_schema"])
	}
	if query.Get("encoding") != "json" {
		t.Fatalf("encoding query = %#v, want json", query["encoding"])
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
				`{"path":"/overlays/evpn","cardinality":"per-vni","payload_schema":"arca.telemetry.overlays.evpn.v1","aliases":["/evpn"]},` +
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
		"-exclude-path", "evpn",
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
	wantPaths := []string{"/system", "/interfaces"}
	if strings.Join(gotPaths, ",") != strings.Join(wantPaths, ",") {
		t.Fatalf("snapshot paths = %#v, want %#v", gotPaths, wantPaths)
	}
}

func TestFetchNMSUsesCatalogFiltersForSnapshotPaths(t *testing.T) {
	var catalogQuery url.Values
	var snapshotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/nms/v1/telemetry/paths":
			catalogQuery = r.URL.Query()
			_, _ = w.Write([]byte(`{"paths":[` +
				`{"path":"/overlays/evpn","cardinality":"per-vni","payload_schema":"arca.telemetry.overlays.evpn.v1"}` +
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
		"-include-default",
		"-include-path", "evpn",
		"-include-cardinality", "per-vni",
		"-include-payload-schema", "arca.telemetry.overlays.evpn.v1",
		"-include-encoding", "json",
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
	if catalogQuery.Get("path") != "evpn" {
		t.Fatalf("catalog path query = %#v, want evpn", catalogQuery["path"])
	}
	if catalogQuery.Get("default") != "true" {
		t.Fatalf("catalog default query = %#v, want true", catalogQuery["default"])
	}
	if catalogQuery.Get("cardinality") != "per-vni" {
		t.Fatalf("catalog cardinality query = %#v, want per-vni", catalogQuery["cardinality"])
	}
	if catalogQuery.Get("payload_schema") != "arca.telemetry.overlays.evpn.v1" {
		t.Fatalf("catalog payload_schema query = %#v, want EVPN schema", catalogQuery["payload_schema"])
	}
	if catalogQuery.Get("encoding") != "json" {
		t.Fatalf("catalog encoding query = %#v, want json", catalogQuery["encoding"])
	}
	gotPaths := snapshotQuery["path"]
	wantPaths := []string{"/overlays/evpn"}
	if strings.Join(gotPaths, ",") != strings.Join(wantPaths, ",") {
		t.Fatalf("snapshot paths = %#v, want %#v", gotPaths, wantPaths)
	}
}

func TestFetchNMSExportsSnapshotToOTLP(t *testing.T) {
	var snapshotQuery url.Values
	var otlpContentType string
	var otlpRequest otlpLogsRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/nms/v1/telemetry/snapshot":
			snapshotQuery = r.URL.Query()
			_, _ = w.Write([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","events":[{` +
				`"sequence":7,` +
				`"timestamp":"2026-05-15T12:34:56.000000789Z",` +
				`"path":"/system",` +
				`"event_type":"snapshot",` +
				`"encoding":"json",` +
				`"schema_version":"arca.telemetry.v1",` +
				`"payload_bytes":21,` +
				`"payload":{"hostname":"edge01"}` +
				`}]}`))
		case "/v1/logs":
			if r.Method != http.MethodPost {
				t.Fatalf("OTLP method = %s, want POST", r.Method)
			}
			otlpContentType = r.Header.Get("Content-Type")
			if err := json.NewDecoder(r.Body).Decode(&otlpRequest); err != nil {
				t.Fatalf("decode OTLP request: %v", err)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg, err := parseCollectorConfig([]string{
		"-base-url", server.URL,
		"-path", "/system",
		"-otlp-endpoint", server.URL + "/v1/logs",
		"-otlp-service-name", "arca-edge01",
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
	if snapshotQuery.Get("path") != "/system" {
		t.Fatalf("snapshot path query = %#v, want /system", snapshotQuery["path"])
	}
	if otlpContentType != "application/json" {
		t.Fatalf("OTLP Content-Type = %q, want application/json", otlpContentType)
	}
	if len(otlpRequest.ResourceLogs) != 1 || len(otlpRequest.ResourceLogs[0].ScopeLogs) != 1 {
		t.Fatalf("OTLP request = %#v, want one resource/scope logs entry", otlpRequest)
	}
	if got := otlpAttributeValue(otlpRequest.ResourceLogs[0].Resource.Attributes, "service.name"); got != "arca-edge01" {
		t.Fatalf("OTLP service.name = %q, want arca-edge01", got)
	}
	records := otlpRequest.ResourceLogs[0].ScopeLogs[0].LogRecords
	if len(records) != 1 {
		t.Fatalf("OTLP log records = %d, want 1", len(records))
	}
	if records[0].TimeUnixNano != "1778848496000000789" {
		t.Fatalf("TimeUnixNano = %q, want parsed timestamp", records[0].TimeUnixNano)
	}
	if records[0].Body.StringValue != `{"hostname":"edge01"}` {
		t.Fatalf("OTLP body = %q, want JSON payload string", records[0].Body.StringValue)
	}
	if got := otlpAttributeValue(records[0].Attributes, "arca.telemetry.path"); got != "/system" {
		t.Fatalf("OTLP path attribute = %q, want /system", got)
	}
	if got := otlpAttributeValue(records[0].Attributes, "arca.telemetry.sequence"); got != "7" {
		t.Fatalf("OTLP sequence attribute = %q, want 7", got)
	}
}

func TestFilterSnapshotPathsByCardinality(t *testing.T) {
	catalog := telemetryCatalogResponse{Paths: []telemetryCatalogPath{
		{Path: "/system", Cardinality: "single"},
		{Path: "/routes", Cardinality: "per-route", Aliases: []string{"/route-table"}},
		{Path: "/bfd", Cardinality: "per-peer", Aliases: []string{"/bfd-peer"}},
	}}
	got := filterSnapshotPathsByCardinality(
		repeatedPathFlag{"/system", "route-table", "/bfd-peer"},
		catalog,
		repeatedStringFlag{"per-route", "per-peer"},
	)
	if len(got) != 1 || got[0] != "/system" {
		t.Fatalf("filterSnapshotPathsByCardinality() = %#v, want only /system", got)
	}
}

func TestFilterSnapshotPathsByEncoding(t *testing.T) {
	catalog := telemetryCatalogResponse{Encoding: "json"}
	paths := repeatedPathFlag{"/system", "/interfaces"}
	got := filterSnapshotPathsByEncoding(paths, catalog, repeatedStringFlag{"protobuf"})
	if len(got) != 2 || got[0] != "/system" || got[1] != "/interfaces" {
		t.Fatalf("filterSnapshotPathsByEncoding(protobuf) = %#v, want original paths", got)
	}
	got = filterSnapshotPathsByEncoding(paths, catalog, repeatedStringFlag{" JSON "})
	if len(got) != 0 {
		t.Fatalf("filterSnapshotPathsByEncoding(json) = %#v, want none", got)
	}
}

func TestFilterSnapshotPathsByPath(t *testing.T) {
	catalog := telemetryCatalogResponse{Paths: []telemetryCatalogPath{
		{Path: "/system"},
		{Path: "/routes", Aliases: []string{"/route-table"}},
		{Path: "/overlays/evpn", Aliases: []string{"/evpn"}},
	}}
	got := filterSnapshotPathsByPath(
		repeatedPathFlag{"/system", "route-table", "/evpn"},
		catalog,
		repeatedPathFlag{"/routes", "/overlays/evpn"},
	)
	if len(got) != 1 || got[0] != "/system" {
		t.Fatalf("filterSnapshotPathsByPath() = %#v, want only /system", got)
	}
}

func TestFilterSnapshotPathsByPayloadSchema(t *testing.T) {
	catalog := telemetryCatalogResponse{Paths: []telemetryCatalogPath{
		{Path: "/system", PayloadSchema: "arca.telemetry.system.v1"},
		{Path: "/routes", PayloadSchema: "arca.telemetry.routes.v1", Aliases: []string{"/route-table"}},
		{Path: "/bfd", PayloadSchema: "arca.telemetry.bfd.v1", Aliases: []string{"/bfd-peer"}},
	}}
	got := filterSnapshotPathsByPayloadSchema(
		repeatedPathFlag{"/system", "route-table", "/bfd-peer"},
		catalog,
		repeatedStringFlag{"arca.telemetry.routes.v1", "ARCA.TELEMETRY.BFD.V1"},
	)
	if len(got) != 1 || got[0] != "/system" {
		t.Fatalf("filterSnapshotPathsByPayloadSchema() = %#v, want only /system", got)
	}
}

func TestResolveSnapshotPathsRejectsEmptyFilteredSet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"encoding":"json","paths":[{"path":"/routes","cardinality":"per-route","payload_schema":"arca.telemetry.routes.v1"}]}`))
	}))
	defer server.Close()

	cfg, err := parseCollectorConfig([]string{
		"-base-url", server.URL,
		"-discover-paths",
		"-exclude-encoding", "json",
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
		{"-max-events", "0"},
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

func otlpAttributeValue(attrs []otlpKeyValue, key string) string {
	for _, attr := range attrs {
		if attr.Key == key {
			if attr.Value.StringValue != "" {
				return attr.Value.StringValue
			}
			return attr.Value.IntValue
		}
	}
	return ""
}
