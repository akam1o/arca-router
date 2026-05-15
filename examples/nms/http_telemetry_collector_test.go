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
	body := []byte(`{"event_schema_version":"arca.telemetry.v1","encoding":"json","default_paths":["/system","/config/running"],"default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"path_count":2,"paths":[{"path":"/system"},{"path":"/config/running"}]}`)
	if err := json.Unmarshal(body, &catalog); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if strings.Join(catalog.DefaultPaths, ",") != "/system,/config/running" {
		t.Fatalf("catalog default paths = %#v, want system/config", catalog.DefaultPaths)
	}
	if catalog.DefaultSampleIntervalMs != 30000 || catalog.MinSampleIntervalMs != 1000 || catalog.MaxSampleIntervalMs != 3600000 {
		t.Fatalf("interval hints = default %d min %d max %d, want 30000 1000 3600000",
			catalog.DefaultSampleIntervalMs,
			catalog.MinSampleIntervalMs,
			catalog.MaxSampleIntervalMs,
		)
	}
	if catalog.PathCount != 2 {
		t.Fatalf("path count = %d, want 2", catalog.PathCount)
	}
}

func TestDecodeTelemetrySchemasResponseDefaultHints(t *testing.T) {
	var schemas telemetrySchemasResponse
	body := []byte(`{"event_schema_version":"arca.telemetry.v1","encoding":"json","default_paths":["/system","/config/running"],"default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"schema_count":1,"schemas":[{"path":"/system","cardinality":"single","payload_schema":"arca.telemetry.system.v1","fields":[{"name":"hostname","type":"string","description":"daemon hostname"}]}]}`)
	if err := json.Unmarshal(body, &schemas); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if strings.Join(schemas.DefaultPaths, ",") != "/system,/config/running" {
		t.Fatalf("schema default paths = %#v, want system/config", schemas.DefaultPaths)
	}
	if schemas.DefaultSampleIntervalMs != 30000 || schemas.MinSampleIntervalMs != 1000 || schemas.MaxSampleIntervalMs != 3600000 {
		t.Fatalf("schema interval hints = default %d min %d max %d, want 30000 1000 3600000",
			schemas.DefaultSampleIntervalMs,
			schemas.MinSampleIntervalMs,
			schemas.MaxSampleIntervalMs,
		)
	}
	if schemas.SchemaCount != 1 || len(schemas.Schemas) != 1 {
		t.Fatalf("schema count = %d len %d, want 1/1", schemas.SchemaCount, len(schemas.Schemas))
	}
	if schemas.Schemas[0].PayloadSchema != "arca.telemetry.system.v1" ||
		len(schemas.Schemas[0].Fields) != 1 || schemas.Schemas[0].Fields[0].Name != "hostname" {
		t.Fatalf("schema payload metadata = %#v, want system hostname field", schemas.Schemas[0])
	}
}

func TestDecodeDiscoveryResponseRejectsInvalidSchemaEnvelope(t *testing.T) {
	validCatalog := []byte(`{"schema_version":"arca.nms.telemetry-catalog.v1","generated_at":"2026-05-15T12:34:56Z","resource":"/api/nms/v1/telemetry/paths","event_schema_version":"arca.telemetry.v1","encoding":"json","default_paths":["/system"],"default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"path_count":0,"paths":[]}`)
	if err := decodeDiscoveryResponse(collectorConfig{mode: "catalog"}, validCatalog); err != nil {
		t.Fatalf("decodeDiscoveryResponse(valid catalog) error = %v", err)
	}
	validSchemas := []byte(`{"schema_version":"arca.nms.telemetry-schemas.v1","generated_at":"2026-05-15T12:34:56Z","resource":"/api/nms/v1/telemetry/schemas","event_schema_version":"arca.telemetry.v1","encoding":"json","default_paths":["/system"],"default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"schema_count":0,"schemas":[]}`)
	if err := decodeDiscoveryResponse(collectorConfig{mode: "schemas"}, validSchemas); err != nil {
		t.Fatalf("decodeDiscoveryResponse(valid schemas) error = %v", err)
	}

	err := decodeDiscoveryResponse(collectorConfig{mode: "schemas"}, []byte(`[{"path":"/system"}]`))
	if err == nil || !strings.Contains(err.Error(), "decode telemetry schemas response") {
		t.Fatalf("decodeDiscoveryResponse() error = %v, want telemetry schemas decode error", err)
	}
	err = decodeDiscoveryResponse(collectorConfig{mode: "catalog"}, []byte(`{"schema_version":"wrong","resource":"/api/nms/v1/telemetry/paths","paths":[]}`))
	if err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("decodeDiscoveryResponse() error = %v, want schema_version mismatch", err)
	}
	err = decodeDiscoveryResponse(collectorConfig{mode: "schemas"}, []byte(`{"schema_version":"arca.nms.telemetry-schemas.v1","resource":"/wrong","schemas":[]}`))
	if err == nil || !strings.Contains(err.Error(), "resource") {
		t.Fatalf("decodeDiscoveryResponse() error = %v, want resource mismatch", err)
	}
	err = decodeDiscoveryResponse(collectorConfig{mode: "catalog"}, []byte(`{"schema_version":"arca.nms.telemetry-catalog.v1","resource":"/api/nms/v1/telemetry/paths","event_schema_version":"wrong","encoding":"json","path_count":0,"paths":[]}`))
	if err == nil || !strings.Contains(err.Error(), "event_schema_version") {
		t.Fatalf("decodeDiscoveryResponse() error = %v, want event_schema_version mismatch", err)
	}
	err = decodeDiscoveryResponse(collectorConfig{mode: "schemas"}, []byte(`{"schema_version":"arca.nms.telemetry-schemas.v1","resource":"/api/nms/v1/telemetry/schemas","event_schema_version":"arca.telemetry.v1","encoding":"protobuf","schema_count":0,"schemas":[]}`))
	if err == nil || !strings.Contains(err.Error(), "encoding") {
		t.Fatalf("decodeDiscoveryResponse() error = %v, want encoding mismatch", err)
	}
	err = decodeDiscoveryResponse(collectorConfig{mode: "catalog"}, []byte(`{"schema_version":"arca.nms.telemetry-catalog.v1","resource":"/api/nms/v1/telemetry/paths","event_schema_version":"arca.telemetry.v1","encoding":"json","path_count":1,"paths":[]}`))
	if err == nil || !strings.Contains(err.Error(), "path_count") {
		t.Fatalf("decodeDiscoveryResponse() error = %v, want path_count mismatch", err)
	}
	err = decodeDiscoveryResponse(collectorConfig{mode: "schemas"}, []byte(`{"schema_version":"arca.nms.telemetry-schemas.v1","resource":"/api/nms/v1/telemetry/schemas","event_schema_version":"arca.telemetry.v1","encoding":"json","schema_count":1,"schemas":[]}`))
	if err == nil || !strings.Contains(err.Error(), "schema_count") {
		t.Fatalf("decodeDiscoveryResponse() error = %v, want schema_count mismatch", err)
	}
	err = decodeDiscoveryResponse(collectorConfig{mode: "catalog"}, []byte(`{"schema_version":"arca.nms.telemetry-catalog.v1","resource":"/api/nms/v1/telemetry/paths","event_schema_version":"arca.telemetry.v1","encoding":"json","default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"path_count":0,"paths":[]}`))
	if err == nil || !strings.Contains(err.Error(), "default_paths") {
		t.Fatalf("decodeDiscoveryResponse() error = %v, want default_paths mismatch", err)
	}
	err = decodeDiscoveryResponse(collectorConfig{mode: "catalog"}, []byte(`{"schema_version":"arca.nms.telemetry-catalog.v1","resource":"/api/nms/v1/telemetry/paths","event_schema_version":"arca.telemetry.v1","encoding":"json","default_paths":["system"],"default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"path_count":0,"paths":[]}`))
	if err == nil || !strings.Contains(err.Error(), "default_paths[0]") {
		t.Fatalf("decodeDiscoveryResponse() error = %v, want default_paths absolute path mismatch", err)
	}
	err = decodeDiscoveryResponse(collectorConfig{mode: "schemas"}, []byte(`{"schema_version":"arca.nms.telemetry-schemas.v1","resource":"/api/nms/v1/telemetry/schemas","event_schema_version":"arca.telemetry.v1","encoding":"json","default_paths":["/system","/system/"],"default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"schema_count":0,"schemas":[]}`))
	if err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("decodeDiscoveryResponse() error = %v, want default_paths duplicate mismatch", err)
	}
	err = decodeDiscoveryResponse(collectorConfig{mode: "schemas"}, []byte(`{"schema_version":"arca.nms.telemetry-schemas.v1","resource":"/api/nms/v1/telemetry/schemas","event_schema_version":"arca.telemetry.v1","encoding":"json","default_paths":["/system"],"default_sample_interval_ms":500,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"schema_count":0,"schemas":[]}`))
	if err == nil || !strings.Contains(err.Error(), "sample interval") {
		t.Fatalf("decodeDiscoveryResponse() error = %v, want sample interval mismatch", err)
	}
	err = decodeDiscoveryResponse(collectorConfig{mode: "catalog"}, []byte(`{"schema_version":"arca.nms.telemetry-catalog.v1","resource":"/api/nms/v1/telemetry/paths","event_schema_version":"arca.telemetry.v1","encoding":"json","default_paths":["/system"],"default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"path_count":1,"paths":[{"path":"/system","payload_schema":"arca.telemetry.system.v1"}]}`))
	if err == nil || !strings.Contains(err.Error(), "cardinality") {
		t.Fatalf("decodeDiscoveryResponse() error = %v, want catalog cardinality mismatch", err)
	}
	err = decodeDiscoveryResponse(collectorConfig{mode: "catalog"}, []byte(`{"schema_version":"arca.nms.telemetry-catalog.v1","resource":"/api/nms/v1/telemetry/paths","event_schema_version":"arca.telemetry.v1","encoding":"json","default_paths":["/system"],"default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"path_count":1,"paths":[{"path":"/system","cardinality":"per-device","payload_schema":"arca.telemetry.system.v1"}]}`))
	if err == nil || !strings.Contains(err.Error(), "cardinality") {
		t.Fatalf("decodeDiscoveryResponse() error = %v, want catalog cardinality value mismatch", err)
	}
	err = decodeDiscoveryResponse(collectorConfig{mode: "catalog"}, []byte(`{"schema_version":"arca.nms.telemetry-catalog.v1","resource":"/api/nms/v1/telemetry/paths","event_schema_version":"arca.telemetry.v1","encoding":"json","default_paths":["/system"],"default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"path_count":1,"paths":[{"path":"/system","cardinality":"single","payload_schema":"arca.telemetry.device.v1"}]}`))
	if err == nil || !strings.Contains(err.Error(), "payload_schema") {
		t.Fatalf("decodeDiscoveryResponse() error = %v, want catalog payload_schema value mismatch", err)
	}
	err = decodeDiscoveryResponse(collectorConfig{mode: "catalog"}, []byte(`{"schema_version":"arca.nms.telemetry-catalog.v1","resource":"/api/nms/v1/telemetry/paths","event_schema_version":"arca.telemetry.v1","encoding":"json","default_paths":["/system"],"default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"path_count":1,"paths":[{"path":"/system","cardinality":"single","payload_schema":"arca.telemetry.system.v1","aliases":["/system"]}]}`))
	if err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("decodeDiscoveryResponse() error = %v, want catalog duplicate path mismatch", err)
	}
	err = decodeDiscoveryResponse(collectorConfig{mode: "schemas"}, []byte(`{"schema_version":"arca.nms.telemetry-schemas.v1","resource":"/api/nms/v1/telemetry/schemas","event_schema_version":"arca.telemetry.v1","encoding":"json","default_paths":["/system"],"default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"schema_count":1,"schemas":[{"path":"/system","cardinality":"single","payload_schema":"arca.telemetry.system.v1","fields":[]}]}`))
	if err == nil || !strings.Contains(err.Error(), "fields") {
		t.Fatalf("decodeDiscoveryResponse() error = %v, want schema fields mismatch", err)
	}
	err = decodeDiscoveryResponse(collectorConfig{mode: "schemas"}, []byte(`{"schema_version":"arca.nms.telemetry-schemas.v1","resource":"/api/nms/v1/telemetry/schemas","event_schema_version":"arca.telemetry.v1","encoding":"json","default_paths":["/system"],"default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"schema_count":1,"schemas":[{"path":"/system","cardinality":"single","payload_schema":"arca.telemetry.system.v1","fields":[{"name":"hostname","type":"string"}]}]}`))
	if err == nil || !strings.Contains(err.Error(), "description") {
		t.Fatalf("decodeDiscoveryResponse() error = %v, want schema field description mismatch", err)
	}
	err = decodeDiscoveryResponse(collectorConfig{mode: "catalog"}, []byte(`{"schema_version":"arca.nms.telemetry-catalog.v1","resource":"/api/nms/v1/telemetry/paths","event_schema_version":"arca.telemetry.v1","encoding":"json","default_paths":["/system"],"default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"path_count":0,"paths":[]}`))
	if err == nil || !strings.Contains(err.Error(), "generated_at") {
		t.Fatalf("decodeDiscoveryResponse() error = %v, want catalog generated_at mismatch", err)
	}
	err = decodeDiscoveryResponse(collectorConfig{mode: "schemas"}, []byte(`{"schema_version":"arca.nms.telemetry-schemas.v1","generated_at":"bad","resource":"/api/nms/v1/telemetry/schemas","event_schema_version":"arca.telemetry.v1","encoding":"json","default_paths":["/system"],"default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"schema_count":0,"schemas":[]}`))
	if err == nil || !strings.Contains(err.Error(), "generated_at") {
		t.Fatalf("decodeDiscoveryResponse() error = %v, want schemas generated_at mismatch", err)
	}
}

func TestDecodeStatusResponseRejectsInvalidEnvelope(t *testing.T) {
	validStatusData := func() map[string]any {
		return map[string]any{
			"version":          "0.8.0",
			"commit":           "abc123",
			"build_date":       "2026-05-15T12:00:00Z",
			"uptime_seconds":   120.5,
			"config_version":   uint64(77),
			"running_hostname": "edge01",
			"datastore":        map[string]any{"backend": "sqlite", "etcd_endpoints": []string{}},
			"config_sync": map[string]any{
				"enabled":           false,
				"healthy":           false,
				"etcd_revision":     12,
				"running_revision":  11,
				"running_commit_id": "commit-11",
				"last_error":        "sync delayed",
				"last_check":        "2026-05-15T12:34:56Z",
				"last_apply":        "2026-05-15T12:34:56Z",
			},
			"cluster": map[string]any{"enabled": false, "node_count": 0, "etcd_sync_configured": false, "etcd_endpoints": []string{}, "sync_aligned": false},
			"overlay": map[string]any{"evpn": map[string]any{"configured": false, "vnis": 0, "l2_vnis": 0, "l3_vnis": 0, "multicast_vnis": 0}},
			"ha":      map[string]any{"configured": false, "converged": false, "vrrp_groups": 0, "issue_count": 1, "issues": []string{"cluster disabled"}},
			"class_of_service": map[string]any{
				"configured":               false,
				"enforcement_status":       "not configured",
				"forwarding_classes":       0,
				"traffic_control_profiles": 0,
				"interface_bindings":       0,
				"intent_only":              false,
				"capabilities": map[string]any{
					"metadata_binding_supported": false,
					"queue_scheduler_supported":  false,
					"policer_supported":          false,
					"counters_supported":         false,
					"diagnostics":                []string{"scheduler unsupported"},
					"last_check":                 "2026-05-15T12:34:56Z",
					"last_error":                 "capability partial",
				},
			},
			"frr": map[string]any{
				"vrrp": map[string]any{
					"configured_groups": 1,
					"observed_groups":   1,
					"active_groups":     1,
					"last_check":        "2026-05-15T12:34:56Z",
					"last_error":        "status stale",
					"groups":            []any{map[string]any{"interface": "ge-0/0/0", "id": 10, "virtual_address": "192.0.2.1", "state": "Master", "observed": true, "active": true}},
					"issue_count":       1,
					"issues":            []string{"backup not observed"},
				},
				"bfd": map[string]any{
					"configured_peers":    1,
					"observed_peers":      1,
					"up_peers":            1,
					"down_peers":          0,
					"session_down_events": 0,
					"rx_fail_packets":     0,
					"last_check":          "2026-05-15T12:34:56Z",
					"last_error":          "counter stale",
					"peers": []any{map[string]any{
						"peer":                "192.0.2.2",
						"local_address":       "192.0.2.1",
						"interface":           "ge-0/0/0",
						"vrf":                 "default",
						"status":              "up",
						"diagnostic":          "ok",
						"remote_diagnostic":   "ok",
						"observed":            true,
						"up":                  true,
						"session_down_events": 0,
						"rx_fail_packets":     0,
					}},
					"issue_count": 1,
					"issues":      []string{"remote diagnostic degraded"},
				},
			},
			"vpp":     map[string]any{"lcp": map[string]any{"last_reconcile": "2026-05-15T12:34:56Z", "last_error": "reconcile stale", "pair_count": 0, "inconsistency_count": 1, "inconsistencies": []string{"missing pair"}}},
			"netconf": map[string]any{"listening": false, "active_sessions": 0, "active_connections": 0, "total_connections": uint64(0), "successful_auth": uint64(0), "failed_auth": uint64(0)},
		}
	}
	statusEnvelope := func(data any) []byte {
		t.Helper()
		body, err := json.Marshal(map[string]any{
			"schema_version": "arca.nms.operational.v1",
			"generated_at":   "2026-05-15T12:36:56Z",
			"resource":       "/api/nms/v1/status",
			"data":           data,
		})
		if err != nil {
			t.Fatalf("json.Marshal(status envelope) error = %v", err)
		}
		return body
	}

	validStatus := statusEnvelope(validStatusData())
	if err := decodeStatusResponse(validStatus); err != nil {
		t.Fatalf("decodeStatusResponse(valid) error = %v", err)
	}
	unknownBuildDateData := validStatusData()
	unknownBuildDateData["build_date"] = "unknown"
	if err := decodeStatusResponse(statusEnvelope(unknownBuildDateData)); err != nil {
		t.Fatalf("decodeStatusResponse(unknown build_date) error = %v", err)
	}

	err := decodeStatusResponse([]byte(`[{"data":{}}]`))
	if err == nil || !strings.Contains(err.Error(), "decode nms status response") {
		t.Fatalf("decodeStatusResponse() error = %v, want status decode error", err)
	}
	err = decodeStatusResponse([]byte(`{"schema_version":"wrong","resource":"/api/nms/v1/status","data":{}}`))
	if err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("decodeStatusResponse() error = %v, want schema_version mismatch", err)
	}
	err = decodeStatusResponse([]byte(`{"schema_version":"arca.nms.operational.v1","resource":"/wrong","data":{}}`))
	if err == nil || !strings.Contains(err.Error(), "resource") {
		t.Fatalf("decodeStatusResponse() error = %v, want resource mismatch", err)
	}
	err = decodeStatusResponse([]byte(`{"schema_version":"arca.nms.operational.v1","generated_at":"bad","resource":"/api/nms/v1/status","data":{}}`))
	if err == nil || !strings.Contains(err.Error(), "generated_at") {
		t.Fatalf("decodeStatusResponse() error = %v, want generated_at mismatch", err)
	}
	err = decodeStatusResponse([]byte(`{"schema_version":"arca.nms.operational.v1","generated_at":"2026-05-15T12:34:56Z","resource":"/api/nms/v1/status"}`))
	if err == nil || !strings.Contains(err.Error(), "data") {
		t.Fatalf("decodeStatusResponse() error = %v, want missing data mismatch", err)
	}
	err = decodeStatusResponse([]byte(`{"schema_version":"arca.nms.operational.v1","generated_at":"2026-05-15T12:34:56Z","resource":"/api/nms/v1/status","data":[]}`))
	if err == nil || !strings.Contains(err.Error(), "JSON object") {
		t.Fatalf("decodeStatusResponse() error = %v, want data object mismatch", err)
	}
	err = decodeStatusResponse([]byte(`{"schema_version":"arca.nms.operational.v1","generated_at":"2026-05-15T12:34:56Z","resource":"/api/nms/v1/status","data":{}}`))
	if err == nil || !strings.Contains(err.Error(), "data object") {
		t.Fatalf("decodeStatusResponse() error = %v, want empty data object mismatch", err)
	}
	data := validStatusData()
	delete(data, "config_version")
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "config_version") {
		t.Fatalf("decodeStatusResponse() error = %v, want config_version mismatch", err)
	}
	data = validStatusData()
	data["running_hostname"] = " "
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "running_hostname") {
		t.Fatalf("decodeStatusResponse() error = %v, want running_hostname mismatch", err)
	}
	data = validStatusData()
	data["build_date"] = "yesterday"
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "build_date") {
		t.Fatalf("decodeStatusResponse() error = %v, want build_date format mismatch", err)
	}
	data = validStatusData()
	data["build_date"] = "2026-05-15T12:37:56Z"
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "build_date") {
		t.Fatalf("decodeStatusResponse() error = %v, want build_date timing mismatch", err)
	}
	data = validStatusData()
	data["uptime_seconds"] = -1
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "uptime_seconds") {
		t.Fatalf("decodeStatusResponse() error = %v, want uptime_seconds mismatch", err)
	}
	data = validStatusData()
	data["datastore"] = []string{}
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "datastore") {
		t.Fatalf("decodeStatusResponse() error = %v, want datastore mismatch", err)
	}
	data = validStatusData()
	data["datastore"].(map[string]any)["backend"] = "memory"
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "datastore.backend") {
		t.Fatalf("decodeStatusResponse() error = %v, want datastore.backend value mismatch", err)
	}
	data = validStatusData()
	data["datastore"].(map[string]any)["etcd_endpoints"] = []string{"http://127.0.0.1:2379"}
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "datastore.etcd_endpoints") {
		t.Fatalf("decodeStatusResponse() error = %v, want sqlite datastore endpoint mismatch", err)
	}
	data = validStatusData()
	data["datastore"].(map[string]any)["backend"] = "etcd"
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "datastore.etcd_endpoints") {
		t.Fatalf("decodeStatusResponse() error = %v, want etcd datastore endpoint mismatch", err)
	}
	data = validStatusData()
	data["config_sync"].(map[string]any)["enabled"] = "false"
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "config_sync.enabled") {
		t.Fatalf("decodeStatusResponse() error = %v, want config_sync.enabled mismatch", err)
	}
	data = validStatusData()
	data["config_sync"].(map[string]any)["healthy"] = true
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "config_sync.healthy") {
		t.Fatalf("decodeStatusResponse() error = %v, want config_sync.healthy relationship mismatch", err)
	}
	data = validStatusData()
	data["config_sync"].(map[string]any)["enabled"] = true
	data["config_sync"].(map[string]any)["healthy"] = true
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "config_sync.last_error") {
		t.Fatalf("decodeStatusResponse() error = %v, want config_sync.last_error health mismatch", err)
	}
	data = validStatusData()
	data["config_sync"].(map[string]any)["etcd_revision"] = -1
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "config_sync.etcd_revision") {
		t.Fatalf("decodeStatusResponse() error = %v, want config_sync.etcd_revision mismatch", err)
	}
	data = validStatusData()
	data["config_sync"].(map[string]any)["running_commit_id"] = 11
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "config_sync.running_commit_id") {
		t.Fatalf("decodeStatusResponse() error = %v, want config_sync.running_commit_id mismatch", err)
	}
	data = validStatusData()
	data["config_sync"].(map[string]any)["last_error"] = ""
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "config_sync.last_error") {
		t.Fatalf("decodeStatusResponse() error = %v, want config_sync.last_error mismatch", err)
	}
	data = validStatusData()
	data["overlay"].(map[string]any)["evpn"] = []string{}
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "overlay.evpn") {
		t.Fatalf("decodeStatusResponse() error = %v, want overlay.evpn mismatch", err)
	}
	data = validStatusData()
	data["overlay"].(map[string]any)["evpn"].(map[string]any)["configured"] = true
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "overlay.evpn.configured") {
		t.Fatalf("decodeStatusResponse() error = %v, want overlay.evpn.configured mismatch", err)
	}
	data = validStatusData()
	evpn := data["overlay"].(map[string]any)["evpn"].(map[string]any)
	evpn["configured"] = true
	evpn["vnis"] = 1
	evpn["l2_vnis"] = 1
	evpn["l3_vnis"] = 1
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "overlay.evpn.vnis") {
		t.Fatalf("decodeStatusResponse() error = %v, want overlay.evpn.vnis mismatch", err)
	}
	data = validStatusData()
	data["frr"].(map[string]any)["bfd"].(map[string]any)["rx_fail_packets"] = -1
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "frr.bfd.rx_fail_packets") {
		t.Fatalf("decodeStatusResponse() error = %v, want frr.bfd.rx_fail_packets mismatch", err)
	}
	data = validStatusData()
	data["netconf"].(map[string]any)["total_connections"] = -1
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "netconf.total_connections") {
		t.Fatalf("decodeStatusResponse() error = %v, want netconf.total_connections mismatch", err)
	}
	data = validStatusData()
	netconf := data["netconf"].(map[string]any)
	netconf["total_connections"] = uint64(1)
	netconf["successful_auth"] = uint64(1)
	netconf["failed_auth"] = uint64(1)
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "netconf.total_connections") {
		t.Fatalf("decodeStatusResponse() error = %v, want netconf.total_connections aggregate mismatch", err)
	}
	data = validStatusData()
	data["ha"].(map[string]any)["issues"] = []any{"cluster disabled", nil}
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "ha.issues[1]") {
		t.Fatalf("decodeStatusResponse() error = %v, want ha.issues mismatch", err)
	}
	data = validStatusData()
	data["ha"].(map[string]any)["issues"] = []string{""}
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "ha.issues[0]") {
		t.Fatalf("decodeStatusResponse() error = %v, want ha.issues empty mismatch", err)
	}
	data = validStatusData()
	data["frr"].(map[string]any)["vrrp"].(map[string]any)["groups"] = []any{map[string]any{"interface": "ge-0/0/0", "id": 10, "observed": true, "active": true}}
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "frr.vrrp.groups[0].state") {
		t.Fatalf("decodeStatusResponse() error = %v, want frr.vrrp.groups state mismatch", err)
	}
	data = validStatusData()
	data["frr"].(map[string]any)["bfd"].(map[string]any)["peers"] = []any{map[string]any{
		"peer":                "192.0.2.2",
		"status":              "up",
		"observed":            true,
		"up":                  true,
		"session_down_events": 0,
		"rx_fail_packets":     -1,
	}}
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "frr.bfd.peers[0].rx_fail_packets") {
		t.Fatalf("decodeStatusResponse() error = %v, want frr.bfd.peers rx_fail_packets mismatch", err)
	}
	data = validStatusData()
	data["vpp"].(map[string]any)["lcp"].(map[string]any)["inconsistencies"] = "missing pair"
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "vpp.lcp.inconsistencies") {
		t.Fatalf("decodeStatusResponse() error = %v, want vpp.lcp.inconsistencies mismatch", err)
	}
	data = validStatusData()
	data["class_of_service"].(map[string]any)["enforcement_status"] = "enforced"
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "class_of_service.enforcement_status") {
		t.Fatalf("decodeStatusResponse() error = %v, want class_of_service.enforcement_status mismatch", err)
	}
	data = validStatusData()
	data["ha"].(map[string]any)["configured"] = true
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "ha.configured") {
		t.Fatalf("decodeStatusResponse() error = %v, want ha.configured relationship mismatch", err)
	}
	data = validStatusData()
	data["ha"].(map[string]any)["converged"] = true
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "ha.converged") {
		t.Fatalf("decodeStatusResponse() error = %v, want ha.converged relationship mismatch", err)
	}
	data = validStatusData()
	data["class_of_service"].(map[string]any)["intent_only"] = true
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "class_of_service.intent_only") {
		t.Fatalf("decodeStatusResponse() error = %v, want class_of_service.intent_only relationship mismatch", err)
	}
	data = validStatusData()
	data["class_of_service"].(map[string]any)["configured"] = true
	data["class_of_service"].(map[string]any)["intent_only"] = true
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "class_of_service.enforcement_status") {
		t.Fatalf("decodeStatusResponse() error = %v, want class_of_service.enforcement_status relationship mismatch", err)
	}
	data = validStatusData()
	data["class_of_service"].(map[string]any)["forwarding_classes"] = 1
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "class_of_service.forwarding_classes") {
		t.Fatalf("decodeStatusResponse() error = %v, want class_of_service.forwarding_classes relationship mismatch", err)
	}
	data = validStatusData()
	data["frr"].(map[string]any)["vrrp"].(map[string]any)["groups"] = []any{map[string]any{"interface": "ge-0/0/0", "id": 10, "state": "Idle", "observed": true, "active": true}}
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "frr.vrrp.groups[0].state") {
		t.Fatalf("decodeStatusResponse() error = %v, want frr.vrrp.groups state mismatch", err)
	}
	data = validStatusData()
	data["frr"].(map[string]any)["bfd"].(map[string]any)["peers"] = []any{map[string]any{
		"peer":                "192.0.2.2",
		"status":              "down",
		"observed":            true,
		"up":                  true,
		"session_down_events": 0,
		"rx_fail_packets":     0,
	}}
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "frr.bfd.peers[0].status") {
		t.Fatalf("decodeStatusResponse() error = %v, want frr.bfd.peers status mismatch", err)
	}
	data = validStatusData()
	data["ha"].(map[string]any)["issue_count"] = 2
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "ha.issue_count") {
		t.Fatalf("decodeStatusResponse() error = %v, want ha.issue_count mismatch", err)
	}
	data = validStatusData()
	data["frr"].(map[string]any)["vrrp"].(map[string]any)["observed_groups"] = 0
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "frr.vrrp.observed_groups") {
		t.Fatalf("decodeStatusResponse() error = %v, want frr.vrrp.observed_groups mismatch", err)
	}
	data = validStatusData()
	data["frr"].(map[string]any)["bfd"].(map[string]any)["up_peers"] = 0
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "frr.bfd.up_peers") {
		t.Fatalf("decodeStatusResponse() error = %v, want frr.bfd.up_peers mismatch", err)
	}
	data = validStatusData()
	data["vpp"].(map[string]any)["lcp"].(map[string]any)["inconsistency_count"] = 2
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "vpp.lcp.inconsistency_count") {
		t.Fatalf("decodeStatusResponse() error = %v, want vpp.lcp.inconsistency_count mismatch", err)
	}
	data = validStatusData()
	data["config_sync"].(map[string]any)["last_check"] = "bad"
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "config_sync.last_check") {
		t.Fatalf("decodeStatusResponse() error = %v, want config_sync.last_check mismatch", err)
	}
	data = validStatusData()
	data["config_sync"].(map[string]any)["last_apply"] = "2026-05-15T12:37:56Z"
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "config_sync.last_apply") {
		t.Fatalf("decodeStatusResponse() error = %v, want config_sync.last_apply timing mismatch", err)
	}
	data = validStatusData()
	data["config_sync"].(map[string]any)["last_apply"] = "2026-05-15T12:35:56Z"
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "config_sync.last_apply") {
		t.Fatalf("decodeStatusResponse() error = %v, want config_sync.last_apply order mismatch", err)
	}
	data = validStatusData()
	delete(data["config_sync"].(map[string]any), "last_check")
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "config_sync.last_check") {
		t.Fatalf("decodeStatusResponse() error = %v, want config_sync.last_check relationship mismatch", err)
	}
	data = validStatusData()
	data["cluster"].(map[string]any)["etcd_endpoints"] = []string{"http://127.0.0.1:2379"}
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "cluster.etcd_sync_configured") {
		t.Fatalf("decodeStatusResponse() error = %v, want cluster.etcd_sync_configured relationship mismatch", err)
	}
	data = validStatusData()
	data["cluster"].(map[string]any)["etcd_sync_configured"] = true
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "cluster.etcd_sync_configured") {
		t.Fatalf("decodeStatusResponse() error = %v, want cluster.etcd_sync_configured enabled mismatch", err)
	}
	data = validStatusData()
	data["cluster"].(map[string]any)["enabled"] = true
	data["cluster"].(map[string]any)["etcd_sync_configured"] = true
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "cluster.etcd_endpoints") {
		t.Fatalf("decodeStatusResponse() error = %v, want cluster.etcd_endpoints relationship mismatch", err)
	}
	data = validStatusData()
	data["class_of_service"].(map[string]any)["capabilities"].(map[string]any)["last_check"] = "bad"
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "class_of_service.capabilities.last_check") {
		t.Fatalf("decodeStatusResponse() error = %v, want class_of_service.capabilities.last_check mismatch", err)
	}
	data = validStatusData()
	data["class_of_service"].(map[string]any)["capabilities"].(map[string]any)["last_error"] = []string{"bad"}
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "class_of_service.capabilities.last_error") {
		t.Fatalf("decodeStatusResponse() error = %v, want class_of_service.capabilities.last_error mismatch", err)
	}
	data = validStatusData()
	data["frr"].(map[string]any)["vrrp"].(map[string]any)["last_error"] = []string{"bad"}
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "frr.vrrp.last_error") {
		t.Fatalf("decodeStatusResponse() error = %v, want frr.vrrp.last_error mismatch", err)
	}
	data = validStatusData()
	data["frr"].(map[string]any)["bfd"].(map[string]any)["last_check"] = "bad"
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "frr.bfd.last_check") {
		t.Fatalf("decodeStatusResponse() error = %v, want frr.bfd.last_check mismatch", err)
	}
	data = validStatusData()
	data["vpp"].(map[string]any)["lcp"].(map[string]any)["last_reconcile"] = "bad"
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "vpp.lcp.last_reconcile") {
		t.Fatalf("decodeStatusResponse() error = %v, want vpp.lcp.last_reconcile mismatch", err)
	}
	data = validStatusData()
	data["vpp"].(map[string]any)["lcp"].(map[string]any)["last_error"] = 1
	err = decodeStatusResponse(statusEnvelope(data))
	if err == nil || !strings.Contains(err.Error(), "vpp.lcp.last_error") {
		t.Fatalf("decodeStatusResponse() error = %v, want vpp.lcp.last_error mismatch", err)
	}
}

func TestDecodeTelemetrySnapshotResponseIntervalHints(t *testing.T) {
	var snapshot telemetrySnapshotResponse
	body := []byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","generated_at":"2026-05-15T12:34:56Z","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","default_paths":["/system","/config/running"],"default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"paths":["/system","/config/running"],"event_count":2,"payload_bytes":44,"max_payload_bytes":8388608,"max_events":64,"timeout_ms":5000,"events":[` +
		`{"sequence":1,"timestamp":"2026-05-15T12:34:56Z","path":"/system","cardinality":"single","payload_schema":"arca.telemetry.system.v1","event_type":"snapshot","encoding":"json","schema_version":"arca.telemetry.v1","payload_bytes":21,"payload":{"hostname":"edge01"}},` +
		`{"sequence":2,"timestamp":"2026-05-15T12:34:56Z","path":"/config/running","cardinality":"single","payload_schema":"arca.telemetry.config.running.v1","event_type":"error","encoding":"json","schema_version":"arca.telemetry.v1","payload_bytes":23,"payload":{"error":"unavailable"}}` +
		`]}`)
	if err := json.Unmarshal(body, &snapshot); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if _, err := decodeSnapshotResponse(body); err != nil {
		t.Fatalf("decodeSnapshotResponse() error = %v", err)
	}
	if strings.Join(snapshot.DefaultPaths, ",") != "/system,/config/running" {
		t.Fatalf("snapshot default paths = %#v, want system/config", snapshot.DefaultPaths)
	}
	if snapshot.DefaultSampleIntervalMs != 30000 || snapshot.MinSampleIntervalMs != 1000 || snapshot.MaxSampleIntervalMs != 3600000 {
		t.Fatalf("snapshot interval hints = default %d min %d max %d, want 30000 1000 3600000",
			snapshot.DefaultSampleIntervalMs,
			snapshot.MinSampleIntervalMs,
			snapshot.MaxSampleIntervalMs,
		)
	}
	if snapshot.EventCount != 2 {
		t.Fatalf("snapshot event count = %d, want 2", snapshot.EventCount)
	}
}

func TestDecodeSnapshotResponseRejectsInvalidEnvelope(t *testing.T) {
	_, err := decodeSnapshotResponse([]byte(`[{"path":"/system"}]`))
	if err == nil || !strings.Contains(err.Error(), "decode telemetry snapshot response") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want snapshot decode error", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"wrong","resource":"/api/nms/v1/telemetry/snapshot","events":[]}`))
	if err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want schema_version mismatch", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","resource":"/wrong","events":[]}`))
	if err == nil || !strings.Contains(err.Error(), "resource") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want resource mismatch", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"wrong","encoding":"json","event_count":0,"events":[]}`))
	if err == nil || !strings.Contains(err.Error(), "event_schema_version") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want event_schema_version mismatch", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"protobuf","event_count":0,"events":[]}`))
	if err == nil || !strings.Contains(err.Error(), "encoding") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want encoding mismatch", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","event_count":1,"events":[]}`))
	if err == nil || !strings.Contains(err.Error(), "event_count") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want event_count mismatch", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","event_count":1,"events":[{"path":"/system","event_type":"snapshot","encoding":"json","schema_version":"wrong","payload_bytes":2,"payload":{}}]}`))
	if err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want event schema_version mismatch", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","event_count":1,"events":[{"path":"/system","event_type":"snapshot","encoding":"protobuf","schema_version":"arca.telemetry.v1","payload_bytes":2,"payload":{}}]}`))
	if err == nil || !strings.Contains(err.Error(), "encoding") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want event encoding mismatch", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","event_count":1,"events":[{"path":"/system","event_type":"update","encoding":"json","schema_version":"arca.telemetry.v1","payload_bytes":2,"payload":{}}]}`))
	if err == nil || !strings.Contains(err.Error(), "event_type") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want event_type mismatch", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","event_count":1,"events":[{"event_type":"snapshot","encoding":"json","schema_version":"arca.telemetry.v1","payload_bytes":2,"payload":{}}]}`))
	if err == nil || !strings.Contains(err.Error(), "path") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want empty path mismatch", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","event_count":1,"events":[{"path":"/system","event_type":"snapshot","encoding":"json","schema_version":"arca.telemetry.v1","payload_bytes":1,"payload":{}}]}`))
	if err == nil || !strings.Contains(err.Error(), "payload_bytes") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want event payload_bytes mismatch", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","event_count":1,"events":[{"path":"/system","event_type":"snapshot","encoding":"json","schema_version":"arca.telemetry.v1","payload_bytes":2,"payload":{}}]}`))
	if err == nil || !strings.Contains(err.Error(), "cardinality") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want event cardinality mismatch", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","event_count":1,"events":[{"path":"/system","cardinality":"per-device","payload_schema":"arca.telemetry.system.v1","event_type":"snapshot","encoding":"json","schema_version":"arca.telemetry.v1","payload_bytes":2,"payload":{}}]}`))
	if err == nil || !strings.Contains(err.Error(), "cardinality") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want event cardinality value mismatch", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","event_count":1,"events":[{"path":"/system","cardinality":"single","payload_schema":"arca.telemetry.device.v1","event_type":"snapshot","encoding":"json","schema_version":"arca.telemetry.v1","payload_bytes":2,"payload":{}}]}`))
	if err == nil || !strings.Contains(err.Error(), "payload_schema") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want event payload_schema value mismatch", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","event_count":1,"events":[{"path":"/system","cardinality":"single","event_type":"snapshot","encoding":"json","schema_version":"arca.telemetry.v1","payload_bytes":2,"payload":{}}]}`))
	if err == nil || !strings.Contains(err.Error(), "payload_schema") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want event payload_schema mismatch", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","event_count":1,"events":[{"timestamp":"2026-05-15T12:34:56Z","path":"/system","cardinality":"single","payload_schema":"arca.telemetry.system.v1","event_type":"snapshot","encoding":"json","schema_version":"arca.telemetry.v1","payload_bytes":2,"payload":{}}]}`))
	if err == nil || !strings.Contains(err.Error(), "sequence") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want event sequence mismatch", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","event_count":1,"events":[{"sequence":1,"timestamp":"bad","path":"/system","cardinality":"single","payload_schema":"arca.telemetry.system.v1","event_type":"snapshot","encoding":"json","schema_version":"arca.telemetry.v1","payload_bytes":2,"payload":{}}]}`))
	if err == nil || !strings.Contains(err.Error(), "timestamp") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want event timestamp mismatch", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","paths":["/system","/interfaces"],"event_count":2,"payload_bytes":4,"events":[{"sequence":2,"timestamp":"2026-05-15T12:34:56Z","path":"/system","cardinality":"single","payload_schema":"arca.telemetry.system.v1","event_type":"snapshot","encoding":"json","schema_version":"arca.telemetry.v1","payload_bytes":2,"payload":{}},{"sequence":2,"timestamp":"2026-05-15T12:34:56Z","path":"/interfaces","cardinality":"per-interface","payload_schema":"arca.telemetry.interfaces.v1","event_type":"snapshot","encoding":"json","schema_version":"arca.telemetry.v1","payload_bytes":2,"payload":{}}]}`))
	if err == nil || !strings.Contains(err.Error(), "sequence") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want event sequence order mismatch", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","paths":["/wrong"],"event_count":1,"payload_bytes":2,"events":[{"sequence":1,"timestamp":"2026-05-15T12:34:56Z","path":"/system","cardinality":"single","payload_schema":"arca.telemetry.system.v1","event_type":"snapshot","encoding":"json","schema_version":"arca.telemetry.v1","payload_bytes":2,"payload":{}}]}`))
	if err == nil || !strings.Contains(err.Error(), "paths[0]") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want emitted path mismatch", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","paths":["/system"],"event_count":1,"payload_bytes":3,"events":[{"sequence":1,"timestamp":"2026-05-15T12:34:56Z","path":"/system","cardinality":"single","payload_schema":"arca.telemetry.system.v1","event_type":"snapshot","encoding":"json","schema_version":"arca.telemetry.v1","payload_bytes":2,"payload":{}}]}`))
	if err == nil || !strings.Contains(err.Error(), "payload_bytes") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want aggregate payload_bytes mismatch", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","paths":["/system"],"event_count":1,"payload_bytes":2,"max_payload_bytes":1,"events":[{"sequence":1,"timestamp":"2026-05-15T12:34:56Z","path":"/system","cardinality":"single","payload_schema":"arca.telemetry.system.v1","event_type":"snapshot","encoding":"json","schema_version":"arca.telemetry.v1","payload_bytes":2,"payload":{}}]}`))
	if err == nil || !strings.Contains(err.Error(), "max_payload_bytes") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want max_payload_bytes mismatch", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","paths":["/system","/interfaces"],"event_count":2,"payload_bytes":4,"max_events":1,"events":[{"sequence":1,"timestamp":"2026-05-15T12:34:56Z","path":"/system","cardinality":"single","payload_schema":"arca.telemetry.system.v1","event_type":"snapshot","encoding":"json","schema_version":"arca.telemetry.v1","payload_bytes":2,"payload":{}},{"sequence":2,"timestamp":"2026-05-15T12:34:56Z","path":"/interfaces","cardinality":"per-interface","payload_schema":"arca.telemetry.interfaces.v1","event_type":"snapshot","encoding":"json","schema_version":"arca.telemetry.v1","payload_bytes":2,"payload":{}}]}`))
	if err == nil || !strings.Contains(err.Error(), "max_events") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want max_events mismatch", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","paths":[],"event_count":0,"payload_bytes":0,"timeout_ms":-1,"events":[]}`))
	if err == nil || !strings.Contains(err.Error(), "timeout_ms") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want timeout_ms mismatch", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"paths":[],"event_count":0,"payload_bytes":0,"events":[]}`))
	if err == nil || !strings.Contains(err.Error(), "default_paths") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want snapshot default_paths mismatch", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","default_paths":["system"],"default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"paths":[],"event_count":0,"payload_bytes":0,"events":[]}`))
	if err == nil || !strings.Contains(err.Error(), "default_paths[0]") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want snapshot default_paths absolute path mismatch", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","default_paths":["/system"],"default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":1000,"paths":[],"event_count":0,"payload_bytes":0,"events":[]}`))
	if err == nil || !strings.Contains(err.Error(), "sample interval") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want snapshot sample interval mismatch", err)
	}
	_, err = decodeSnapshotResponse([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","generated_at":"bad","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","default_paths":["/system"],"default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"paths":[],"event_count":0,"payload_bytes":0,"events":[]}`))
	if err == nil || !strings.Contains(err.Error(), "generated_at") {
		t.Fatalf("decodeSnapshotResponse() error = %v, want snapshot generated_at mismatch", err)
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

func TestParseCollectorConfigIncludeFiltersUseSnapshotMetadataFilters(t *testing.T) {
	cfg, err := parseCollectorConfig([]string{
		"-include-encoding", "json",
	})
	if err != nil {
		t.Fatalf("parseCollectorConfig() error = %v", err)
	}
	if len(cfg.paths) != 0 {
		t.Fatalf("paths = %#v, want server-selected paths for include encoding filter", cfg.paths)
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

func TestCollectorEndpointURLForSnapshotMetadataFilters(t *testing.T) {
	cfg, err := parseCollectorConfig([]string{
		"-base-url", "http://router.example:8080/arca",
		"-include-default",
		"-include-cardinality", "single",
		"-include-payload-schema", "arca.telemetry.system.v1",
		"-include-encoding", "json",
		"-timeout", "3s",
		"-max-events", "2",
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
		t.Fatalf("snapshot URL is invalid: %v", err)
	}
	if parsed.Scheme != "http" || parsed.Host != "router.example:8080" || parsed.Path != "/arca/api/nms/v1/telemetry/snapshot" {
		t.Fatalf("snapshot URL = %q, want endpoint under /arca", got)
	}
	query := parsed.Query()
	if query.Get("default") != "true" {
		t.Fatalf("default query = %#v, want true", query["default"])
	}
	if query.Get("cardinality") != "single" {
		t.Fatalf("cardinality query = %#v, want single", query["cardinality"])
	}
	if query.Get("payload_schema") != "arca.telemetry.system.v1" {
		t.Fatalf("payload_schema query = %#v, want system schema", query["payload_schema"])
	}
	if query.Get("encoding") != "json" {
		t.Fatalf("encoding query = %#v, want json", query["encoding"])
	}
	if query.Get("timeout") != "3s" || query.Get("max_events") != "2" {
		t.Fatalf("snapshot guardrail query = %#v, want timeout and max_events", query)
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

func TestFetchNMSValidatesStatusEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/nms/v1/status" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"schema_version":"wrong","resource":"/api/nms/v1/status","data":{}}`))
	}))
	defer server.Close()

	cfg, err := parseCollectorConfig([]string{
		"-base-url", server.URL,
		"-mode", "status",
	})
	if err != nil {
		t.Fatalf("parseCollectorConfig() error = %v", err)
	}
	_, err = fetchNMS(t.Context(), server.Client(), cfg)
	if err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("fetchNMS() error = %v, want status schema_version mismatch", err)
	}
}

func TestFetchNMSDiscoversAndFiltersSnapshotPaths(t *testing.T) {
	var snapshotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/nms/v1/telemetry/paths":
			_, _ = w.Write([]byte(`{"schema_version":"arca.nms.telemetry-catalog.v1","generated_at":"2026-05-15T12:34:56Z","resource":"/api/nms/v1/telemetry/paths","event_schema_version":"arca.telemetry.v1","encoding":"json","default_paths":["/system","/config/running"],"default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"path_count":5,"paths":[` +
				`{"path":"/system","cardinality":"single","payload_schema":"arca.telemetry.system.v1"},` +
				`{"path":"/interfaces","cardinality":"per-interface","payload_schema":"arca.telemetry.interfaces.v1"},` +
				`{"path":"/routes","cardinality":"per-route","payload_schema":"arca.telemetry.routes.v1"},` +
				`{"path":"/overlays/evpn","cardinality":"per-vni","payload_schema":"arca.telemetry.overlays.evpn.v1","aliases":["/evpn"]},` +
				`{"path":"/bfd","cardinality":"per-peer","payload_schema":"arca.telemetry.bfd.v1"}` +
				`]}`))
		case "/api/nms/v1/telemetry/snapshot":
			snapshotQuery = r.URL.Query()
			_, _ = w.Write([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","generated_at":"2026-05-15T12:34:56Z","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","default_paths":["/system","/config/running"],"default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"event_count":0,"events":[]}`))
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

func TestFetchNMSUsesSnapshotMetadataFilters(t *testing.T) {
	var snapshotQuery url.Values
	catalogCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/nms/v1/telemetry/paths":
			catalogCalled = true
			http.Error(w, "unexpected catalog request", http.StatusInternalServerError)
		case "/api/nms/v1/telemetry/snapshot":
			snapshotQuery = r.URL.Query()
			_, _ = w.Write([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","generated_at":"2026-05-15T12:34:56Z","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","default_paths":["/system","/config/running"],"default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"event_count":0,"events":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg, err := parseCollectorConfig([]string{
		"-base-url", server.URL,
		"-include-cardinality", "per-route",
		"-include-payload-schema", "arca.telemetry.routes.v1",
		"-include-encoding", "json",
		"-max-events", "1",
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
	if catalogCalled {
		t.Fatal("catalog endpoint was called, want direct snapshot metadata filters")
	}
	if snapshotQuery.Get("cardinality") != "per-route" {
		t.Fatalf("snapshot cardinality query = %#v, want per-route", snapshotQuery["cardinality"])
	}
	if snapshotQuery.Get("payload_schema") != "arca.telemetry.routes.v1" {
		t.Fatalf("snapshot payload_schema query = %#v, want route schema", snapshotQuery["payload_schema"])
	}
	if snapshotQuery.Get("encoding") != "json" {
		t.Fatalf("snapshot encoding query = %#v, want json", snapshotQuery["encoding"])
	}
	if snapshotQuery.Get("max_events") != "1" {
		t.Fatalf("snapshot max_events query = %#v, want 1", snapshotQuery["max_events"])
	}
	if gotPaths := snapshotQuery["path"]; len(gotPaths) != 0 {
		t.Fatalf("snapshot paths = %#v, want server-selected paths", gotPaths)
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
			_, _ = w.Write([]byte(`{"schema_version":"arca.nms.telemetry-snapshot.v1","generated_at":"2026-05-15T12:34:56Z","resource":"/api/nms/v1/telemetry/snapshot","event_schema_version":"arca.telemetry.v1","encoding":"json","default_paths":["/system","/config/running"],"default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"paths":["/system"],"event_count":1,"payload_bytes":21,"max_payload_bytes":8388608,"max_events":64,"timeout_ms":5000,"events":[{` +
				`"sequence":7,` +
				`"timestamp":"2026-05-15T12:34:56.000000789Z",` +
				`"path":"/system",` +
				`"cardinality":"single",` +
				`"payload_schema":"arca.telemetry.system.v1",` +
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
	if got := otlpAttributeValue(records[0].Attributes, "arca.telemetry.cardinality"); got != "single" {
		t.Fatalf("OTLP cardinality attribute = %q, want single", got)
	}
	if got := otlpAttributeValue(records[0].Attributes, "arca.telemetry.payload_schema"); got != "arca.telemetry.system.v1" {
		t.Fatalf("OTLP payload_schema attribute = %q, want system schema", got)
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
		_, _ = w.Write([]byte(`{"schema_version":"arca.nms.telemetry-catalog.v1","generated_at":"2026-05-15T12:34:56Z","resource":"/api/nms/v1/telemetry/paths","event_schema_version":"arca.telemetry.v1","encoding":"json","default_paths":["/system","/config/running"],"default_sample_interval_ms":30000,"min_sample_interval_ms":1000,"max_sample_interval_ms":3600000,"path_count":1,"paths":[{"path":"/routes","cardinality":"per-route","payload_schema":"arca.telemetry.routes.v1"}]}`))
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

func TestCollectorEndpointURLForDiscoveryModes(t *testing.T) {
	tests := []struct {
		mode string
		want string
	}{
		{mode: "status", want: "http://127.0.0.1:8080/api/nms/v1/status"},
		{mode: "catalog", want: "http://127.0.0.1:8080/api/nms/v1/telemetry/paths"},
		{mode: "schemas", want: "http://127.0.0.1:8080/api/nms/v1/telemetry/schemas"},
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

func TestCollectorEndpointURLForSchemaFilters(t *testing.T) {
	cfg, err := parseCollectorConfig([]string{
		"-mode", "schemas",
		"-base-url", "http://router.example:8080/arca",
		"-include-default",
		"-include-path", "evpn",
		"-include-cardinality", "per-vni",
		"-include-payload-schema", "arca.telemetry.overlays.evpn.v1",
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
		t.Fatalf("schema URL is invalid: %v", err)
	}
	if parsed.Scheme != "http" || parsed.Host != "router.example:8080" || parsed.Path != "/arca/api/nms/v1/telemetry/schemas" {
		t.Fatalf("schema URL = %q, want endpoint under /arca", got)
	}
	query := parsed.Query()
	if query.Get("default") != "true" {
		t.Fatalf("default query = %#v, want true", query["default"])
	}
	if query.Get("path") != "evpn" {
		t.Fatalf("path query = %#v, want evpn", query["path"])
	}
	if query.Get("cardinality") != "per-vni" {
		t.Fatalf("cardinality query = %#v, want per-vni", query["cardinality"])
	}
	if query.Get("payload_schema") != "arca.telemetry.overlays.evpn.v1" {
		t.Fatalf("payload_schema query = %#v, want EVPN schema", query["payload_schema"])
	}
	if query.Get("encoding") != "json" {
		t.Fatalf("encoding query = %#v, want json", query["encoding"])
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
