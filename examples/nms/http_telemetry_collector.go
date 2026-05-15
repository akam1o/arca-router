package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL         = "http://127.0.0.1:8080"
	defaultSnapshotTimeout = 5 * time.Second
	defaultMaxPayloadBytes = 8 << 20
	defaultMaxEvents       = 64
	nmsOperationalStatusV1 = "arca.nms.operational.v1"
	nmsTelemetryCatalogV1  = "arca.nms.telemetry-catalog.v1"
	nmsTelemetrySchemasV1  = "arca.nms.telemetry-schemas.v1"
	nmsTelemetrySnapshotV1 = "arca.nms.telemetry-snapshot.v1"
	telemetryEventSchemaV1 = "arca.telemetry.v1"
	telemetryEncodingJSON  = "json"
	telemetryEventSnapshot = "snapshot"
	telemetryEventError    = "error"
)

var defaultSnapshotPaths = []string{"/system", "/interfaces", "/overlays/evpn"}

type collectorConfig struct {
	baseURL          string
	username         string
	password         string
	mode             string
	otlpEndpoint     string
	otlpServiceName  string
	paths            repeatedPathFlag
	discoverPaths    bool
	includedPath     repeatedPathFlag
	includedDefault  bool
	includedCard     repeatedStringFlag
	includedSchema   repeatedStringFlag
	includedEncoding repeatedStringFlag
	excludedPath     repeatedPathFlag
	excludedCard     repeatedStringFlag
	excludedSchema   repeatedStringFlag
	excludedEncoding repeatedStringFlag
	timeout          time.Duration
	maxPayloadBytes  int
	maxEvents        int
}

type repeatedPathFlag []string

func (p *repeatedPathFlag) String() string {
	return strings.Join(*p, ",")
}

func (p *repeatedPathFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("path must not be empty")
	}
	*p = append(*p, value)
	return nil
}

type repeatedStringFlag []string

func (f *repeatedStringFlag) String() string {
	return strings.Join(*f, ",")
}

func (f *repeatedStringFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("value must not be empty")
	}
	*f = append(*f, value)
	return nil
}

type nmsStatusResponse struct {
	SchemaVersion string          `json:"schema_version"`
	GeneratedAt   string          `json:"generated_at"`
	Resource      string          `json:"resource"`
	Data          json.RawMessage `json:"data"`
}

type telemetryCatalogResponse struct {
	SchemaVersion           string                 `json:"schema_version"`
	GeneratedAt             string                 `json:"generated_at"`
	Resource                string                 `json:"resource"`
	EventSchemaVersion      string                 `json:"event_schema_version"`
	Encoding                string                 `json:"encoding"`
	DefaultPaths            []string               `json:"default_paths"`
	DefaultSampleIntervalMs uint32                 `json:"default_sample_interval_ms"`
	MinSampleIntervalMs     uint32                 `json:"min_sample_interval_ms"`
	MaxSampleIntervalMs     uint32                 `json:"max_sample_interval_ms"`
	PathCount               int                    `json:"path_count"`
	Paths                   []telemetryCatalogPath `json:"paths"`
}

type telemetryCatalogPath struct {
	Description   string   `json:"description"`
	Path          string   `json:"path"`
	Cardinality   string   `json:"cardinality"`
	PayloadSchema string   `json:"payload_schema"`
	Aliases       []string `json:"aliases"`
	Default       bool     `json:"default"`
}

type telemetrySchemasResponse struct {
	SchemaVersion           string                   `json:"schema_version"`
	GeneratedAt             string                   `json:"generated_at"`
	Resource                string                   `json:"resource"`
	EventSchemaVersion      string                   `json:"event_schema_version"`
	Encoding                string                   `json:"encoding"`
	DefaultPaths            []string                 `json:"default_paths"`
	DefaultSampleIntervalMs uint32                   `json:"default_sample_interval_ms"`
	MinSampleIntervalMs     uint32                   `json:"min_sample_interval_ms"`
	MaxSampleIntervalMs     uint32                   `json:"max_sample_interval_ms"`
	SchemaCount             int                      `json:"schema_count"`
	Schemas                 []telemetryPayloadSchema `json:"schemas"`
}

type telemetryPayloadSchema struct {
	Description   string                  `json:"description"`
	Path          string                  `json:"path"`
	Cardinality   string                  `json:"cardinality"`
	PayloadSchema string                  `json:"payload_schema"`
	Aliases       []string                `json:"aliases"`
	Default       bool                    `json:"default"`
	Fields        []telemetryPayloadField `json:"fields"`
}

type telemetryPayloadField struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

type telemetrySnapshotResponse struct {
	SchemaVersion           string                   `json:"schema_version"`
	GeneratedAt             string                   `json:"generated_at"`
	Resource                string                   `json:"resource"`
	EventSchemaVersion      string                   `json:"event_schema_version"`
	Encoding                string                   `json:"encoding"`
	DefaultPaths            []string                 `json:"default_paths"`
	DefaultSampleIntervalMs uint32                   `json:"default_sample_interval_ms"`
	MinSampleIntervalMs     uint32                   `json:"min_sample_interval_ms"`
	MaxSampleIntervalMs     uint32                   `json:"max_sample_interval_ms"`
	Paths                   []string                 `json:"paths"`
	EventCount              int                      `json:"event_count"`
	PayloadBytes            int                      `json:"payload_bytes"`
	MaxPayloadBytes         int                      `json:"max_payload_bytes"`
	MaxEvents               int                      `json:"max_events"`
	TimeoutMs               int64                    `json:"timeout_ms"`
	Events                  []telemetrySnapshotEvent `json:"events"`
}

type telemetrySnapshotEvent struct {
	Sequence      uint64          `json:"sequence"`
	Timestamp     string          `json:"timestamp,omitempty"`
	Path          string          `json:"path"`
	Cardinality   string          `json:"cardinality"`
	PayloadSchema string          `json:"payload_schema"`
	EventType     string          `json:"event_type"`
	Encoding      string          `json:"encoding"`
	SchemaVersion string          `json:"schema_version"`
	PayloadBytes  int             `json:"payload_bytes"`
	Payload       json.RawMessage `json:"payload"`
}

type otlpLogsRequest struct {
	ResourceLogs []otlpResourceLogs `json:"resourceLogs"`
}

type otlpResourceLogs struct {
	Resource  otlpResource    `json:"resource"`
	ScopeLogs []otlpScopeLogs `json:"scopeLogs"`
}

type otlpResource struct {
	Attributes []otlpKeyValue `json:"attributes,omitempty"`
}

type otlpScopeLogs struct {
	Scope      otlpInstrumentationScope `json:"scope"`
	LogRecords []otlpLogRecord          `json:"logRecords"`
}

type otlpInstrumentationScope struct {
	Name string `json:"name"`
}

type otlpLogRecord struct {
	TimeUnixNano   string         `json:"timeUnixNano,omitempty"`
	SeverityText   string         `json:"severityText,omitempty"`
	SeverityNumber int            `json:"severityNumber,omitempty"`
	Body           otlpAnyValue   `json:"body"`
	Attributes     []otlpKeyValue `json:"attributes,omitempty"`
}

type otlpKeyValue struct {
	Key   string       `json:"key"`
	Value otlpAnyValue `json:"value"`
}

type otlpAnyValue struct {
	StringValue string `json:"stringValue,omitempty"`
	IntValue    string `json:"intValue,omitempty"`
}

func main() {
	cfg, err := parseCollectorConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(2)
	}

	client := &http.Client{Timeout: requestTimeout(cfg)}
	body, err := fetchNMS(context.Background(), client, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if err := writePrettyJSON(os.Stdout, body); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func parseCollectorConfig(args []string) (collectorConfig, error) {
	cfg := collectorConfig{
		baseURL:         defaultBaseURL,
		mode:            "snapshot",
		otlpServiceName: "arca-router-nms-collector",
		timeout:         defaultSnapshotTimeout,
		maxPayloadBytes: defaultMaxPayloadBytes,
		maxEvents:       defaultMaxEvents,
	}
	fs := flag.NewFlagSet("http-telemetry-collector", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.baseURL, "base-url", cfg.baseURL, "Base Web API URL")
	fs.StringVar(&cfg.username, "user", "", "HTTP Basic username")
	fs.StringVar(&cfg.password, "password", "", "HTTP Basic password")
	fs.StringVar(&cfg.mode, "mode", cfg.mode, "Endpoint mode: snapshot, status, catalog, or schemas")
	fs.StringVar(&cfg.otlpEndpoint, "otlp-endpoint", "", "OTLP/HTTP logs endpoint URL for snapshot export, for example http://127.0.0.1:4318/v1/logs")
	fs.StringVar(&cfg.otlpServiceName, "otlp-service-name", cfg.otlpServiceName, "OpenTelemetry service.name resource attribute")
	fs.Var(&cfg.paths, "path", "Telemetry path for snapshot mode; repeat for multiple paths")
	fs.BoolVar(&cfg.discoverPaths, "discover-paths", false, "Use telemetry catalog paths as the snapshot path set")
	fs.Var(&cfg.includedPath, "include-path", "Telemetry path or alias to request from catalog, schema, or snapshot metadata filters; repeat for multiple values")
	fs.BoolVar(&cfg.includedDefault, "include-default", false, "Request only default telemetry paths from catalog, schema, or snapshot metadata filters")
	fs.Var(&cfg.includedCard, "include-cardinality", "Telemetry cardinality to request from catalog, schema, or snapshot metadata filters; repeat for multiple values")
	fs.Var(&cfg.includedSchema, "include-payload-schema", "Telemetry payload schema ID to request from catalog, schema, or snapshot metadata filters; repeat for multiple values")
	fs.Var(&cfg.includedEncoding, "include-encoding", "Telemetry payload encoding to request from catalog, schema, or snapshot metadata filters; repeat for multiple values")
	fs.Var(&cfg.excludedPath, "exclude-path", "Telemetry path or alias to exclude from snapshot mode; repeat for multiple values")
	fs.Var(&cfg.excludedCard, "exclude-cardinality", "Telemetry cardinality to exclude from snapshot mode; repeat for multiple values")
	fs.Var(&cfg.excludedSchema, "exclude-payload-schema", "Telemetry payload schema ID to exclude from snapshot mode; repeat for multiple values")
	fs.Var(&cfg.excludedEncoding, "exclude-encoding", "Telemetry payload encoding to exclude from snapshot mode; repeat for multiple values")
	fs.DurationVar(&cfg.timeout, "timeout", cfg.timeout, "Snapshot timeout")
	fs.IntVar(&cfg.maxPayloadBytes, "max-payload-bytes", cfg.maxPayloadBytes, "Snapshot payload byte budget")
	fs.IntVar(&cfg.maxEvents, "max-events", cfg.maxEvents, "Snapshot event count budget")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	cfg.mode = strings.ToLower(strings.TrimSpace(cfg.mode))
	switch cfg.mode {
	case "snapshot", "status", "catalog", "schemas":
	default:
		return cfg, fmt.Errorf("unsupported mode %q", cfg.mode)
	}
	cfg.otlpEndpoint = strings.TrimSpace(cfg.otlpEndpoint)
	if cfg.mode == "snapshot" {
		if cfg.timeout <= 0 {
			return cfg, fmt.Errorf("timeout must be positive")
		}
		if cfg.maxPayloadBytes <= 0 {
			return cfg, fmt.Errorf("max-payload-bytes must be positive")
		}
		if cfg.maxEvents <= 0 {
			return cfg, fmt.Errorf("max-events must be positive")
		}
		if len(cfg.paths) == 0 && !usesCatalogDiscovery(cfg) {
			cfg.paths = append(repeatedPathFlag(nil), defaultSnapshotPaths...)
		}
	} else if cfg.otlpEndpoint != "" {
		return cfg, fmt.Errorf("otlp export requires snapshot mode")
	}
	cfg.otlpServiceName = strings.TrimSpace(cfg.otlpServiceName)
	if cfg.otlpEndpoint != "" && cfg.otlpServiceName == "" {
		return cfg, fmt.Errorf("otlp-service-name must not be empty")
	}
	return cfg, nil
}

func usesCatalogDiscovery(cfg collectorConfig) bool {
	return cfg.discoverPaths || cfg.includedDefault || len(cfg.includedPath) > 0 || len(cfg.includedCard) > 0 || len(cfg.includedSchema) > 0 || len(cfg.includedEncoding) > 0
}

func needsCatalogResolution(cfg collectorConfig) bool {
	return cfg.discoverPaths || len(cfg.excludedPath) > 0 || len(cfg.excludedCard) > 0 || len(cfg.excludedSchema) > 0 || len(cfg.excludedEncoding) > 0
}

func requestTimeout(cfg collectorConfig) time.Duration {
	if cfg.mode == "snapshot" && cfg.timeout > 0 {
		return cfg.timeout + 10*time.Second
	}
	return 10 * time.Second
}

func fetchNMS(ctx context.Context, client *http.Client, cfg collectorConfig) ([]byte, error) {
	var err error
	if cfg.mode == "snapshot" && needsCatalogResolution(cfg) {
		cfg.paths, err = resolveSnapshotPaths(ctx, client, cfg)
		if err != nil {
			return nil, err
		}
	}
	endpoint, err := collectorEndpointURL(cfg)
	if err != nil {
		return nil, err
	}
	body, err := fetchEndpoint(ctx, client, cfg, endpoint)
	if err != nil {
		return nil, err
	}
	if err := decodeDiscoveryResponse(cfg, body); err != nil {
		return nil, err
	}
	if cfg.mode == "status" {
		if err := decodeStatusResponse(body); err != nil {
			return nil, err
		}
	}
	var snapshot telemetrySnapshotResponse
	if cfg.mode == "snapshot" {
		snapshot, err = decodeSnapshotResponse(body)
		if err != nil {
			return nil, err
		}
	}
	if cfg.mode == "snapshot" && cfg.otlpEndpoint != "" {
		if err := exportSnapshotToOTLP(ctx, client, cfg, snapshot); err != nil {
			return nil, err
		}
	}
	return body, nil
}

func decodeDiscoveryResponse(cfg collectorConfig, body []byte) error {
	switch cfg.mode {
	case "catalog":
		_, err := decodeCatalogResponse(body)
		return err
	case "schemas":
		_, err := decodeSchemasResponse(body)
		return err
	}
	return nil
}

func decodeCatalogResponse(body []byte) (telemetryCatalogResponse, error) {
	var catalog telemetryCatalogResponse
	if err := json.Unmarshal(body, &catalog); err != nil {
		return catalog, fmt.Errorf("decode telemetry catalog response: %w", err)
	}
	if err := validateNMSEnvelope("telemetry catalog", catalog.SchemaVersion, catalog.Resource, nmsTelemetryCatalogV1, "/api/nms/v1/telemetry/paths"); err != nil {
		return catalog, err
	}
	if err := validateNMSTelemetryMetadata("telemetry catalog", catalog.EventSchemaVersion, catalog.Encoding); err != nil {
		return catalog, err
	}
	if err := validateNMSResultCount("telemetry catalog", "path_count", catalog.PathCount, "paths", len(catalog.Paths)); err != nil {
		return catalog, err
	}
	if err := validateNMSTelemetryHints(
		"telemetry catalog",
		catalog.DefaultPaths,
		catalog.DefaultSampleIntervalMs,
		catalog.MinSampleIntervalMs,
		catalog.MaxSampleIntervalMs,
	); err != nil {
		return catalog, err
	}
	if err := validateTelemetryCatalogPaths(catalog.Paths); err != nil {
		return catalog, err
	}
	if err := validateNMSGeneratedAt("telemetry catalog", catalog.GeneratedAt); err != nil {
		return catalog, err
	}
	return catalog, nil
}

func decodeSchemasResponse(body []byte) (telemetrySchemasResponse, error) {
	var schemas telemetrySchemasResponse
	if err := json.Unmarshal(body, &schemas); err != nil {
		return schemas, fmt.Errorf("decode telemetry schemas response: %w", err)
	}
	if err := validateNMSEnvelope("telemetry schemas", schemas.SchemaVersion, schemas.Resource, nmsTelemetrySchemasV1, "/api/nms/v1/telemetry/schemas"); err != nil {
		return schemas, err
	}
	if err := validateNMSTelemetryMetadata("telemetry schemas", schemas.EventSchemaVersion, schemas.Encoding); err != nil {
		return schemas, err
	}
	if err := validateNMSResultCount("telemetry schemas", "schema_count", schemas.SchemaCount, "schemas", len(schemas.Schemas)); err != nil {
		return schemas, err
	}
	if err := validateNMSTelemetryHints(
		"telemetry schemas",
		schemas.DefaultPaths,
		schemas.DefaultSampleIntervalMs,
		schemas.MinSampleIntervalMs,
		schemas.MaxSampleIntervalMs,
	); err != nil {
		return schemas, err
	}
	if err := validateTelemetryPayloadSchemas(schemas.Schemas); err != nil {
		return schemas, err
	}
	if err := validateNMSGeneratedAt("telemetry schemas", schemas.GeneratedAt); err != nil {
		return schemas, err
	}
	return schemas, nil
}

func decodeStatusResponse(body []byte) error {
	var status nmsStatusResponse
	if err := json.Unmarshal(body, &status); err != nil {
		return fmt.Errorf("decode nms status response: %w", err)
	}
	if err := validateNMSEnvelope("nms status", status.SchemaVersion, status.Resource, nmsOperationalStatusV1, "/api/nms/v1/status"); err != nil {
		return err
	}
	if err := validateNMSGeneratedAt("nms status", status.GeneratedAt); err != nil {
		return err
	}
	if err := validateNMSStatusData(status.Data); err != nil {
		return err
	}
	return nil
}

func decodeSnapshotResponse(body []byte) (telemetrySnapshotResponse, error) {
	var snapshot telemetrySnapshotResponse
	if err := json.Unmarshal(body, &snapshot); err != nil {
		return snapshot, fmt.Errorf("decode telemetry snapshot response: %w", err)
	}
	if err := validateNMSEnvelope("telemetry snapshot", snapshot.SchemaVersion, snapshot.Resource, nmsTelemetrySnapshotV1, "/api/nms/v1/telemetry/snapshot"); err != nil {
		return snapshot, err
	}
	if err := validateNMSTelemetryMetadata("telemetry snapshot", snapshot.EventSchemaVersion, snapshot.Encoding); err != nil {
		return snapshot, err
	}
	if err := validateNMSResultCount("telemetry snapshot", "event_count", snapshot.EventCount, "events", len(snapshot.Events)); err != nil {
		return snapshot, err
	}
	if err := validateTelemetrySnapshotEvents(snapshot.Events); err != nil {
		return snapshot, err
	}
	if err := validateTelemetrySnapshotAggregates(snapshot); err != nil {
		return snapshot, err
	}
	if err := validateNMSTelemetryHints(
		"telemetry snapshot",
		snapshot.DefaultPaths,
		snapshot.DefaultSampleIntervalMs,
		snapshot.MinSampleIntervalMs,
		snapshot.MaxSampleIntervalMs,
	); err != nil {
		return snapshot, err
	}
	if err := validateNMSGeneratedAt("telemetry snapshot", snapshot.GeneratedAt); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}

func validateNMSEnvelope(kind, schemaVersion, resource, wantSchemaVersion, wantResource string) error {
	if schemaVersion != wantSchemaVersion {
		return fmt.Errorf("%s schema_version = %q, want %q", kind, schemaVersion, wantSchemaVersion)
	}
	if resource != wantResource {
		return fmt.Errorf("%s resource = %q, want %q", kind, resource, wantResource)
	}
	return nil
}

func validateNMSGeneratedAt(kind, generatedAt string) error {
	if strings.TrimSpace(generatedAt) == "" {
		return fmt.Errorf("%s generated_at is empty", kind)
	}
	if _, err := time.Parse(time.RFC3339, generatedAt); err != nil {
		return fmt.Errorf("%s generated_at = %q, want RFC3339: %w", kind, generatedAt, err)
	}
	return nil
}

func validateNMSStatusData(data json.RawMessage) error {
	if len(data) == 0 {
		return fmt.Errorf("nms status data is empty")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return fmt.Errorf("nms status data must be a JSON object: %w", err)
	}
	if len(object) == 0 {
		return fmt.Errorf("nms status data object is empty")
	}
	if err := validateNMSStatusDataFields(object); err != nil {
		return err
	}
	return nil
}

func validateNMSStatusDataFields(object map[string]json.RawMessage) error {
	for _, field := range []string{"version", "commit", "build_date", "running_hostname"} {
		if err := validateNMSStatusStringField(object, field); err != nil {
			return err
		}
	}
	if err := validateNMSStatusFloatField(object, "uptime_seconds"); err != nil {
		return err
	}
	if err := validateNMSStatusUintField(object, "config_version"); err != nil {
		return err
	}
	return validateNMSStatusSections(object)
}

func validateNMSStatusSections(object map[string]json.RawMessage) error {
	datastore, err := validateNMSStatusObjectField(object, "datastore")
	if err != nil {
		return err
	}
	if err := validateNMSStatusStringFieldPath(datastore, "backend", "datastore.backend"); err != nil {
		return err
	}
	if err := validateNMSStatusStringArrayFieldOptional(datastore, "etcd_endpoints", "datastore.etcd_endpoints"); err != nil {
		return err
	}

	configSync, err := validateNMSStatusObjectField(object, "config_sync")
	if err != nil {
		return err
	}
	if err := validateNMSStatusBoolFields(configSync, "config_sync", "enabled", "healthy"); err != nil {
		return err
	}
	for _, field := range []string{"etcd_revision", "running_revision"} {
		if err := validateNMSStatusIntFieldOptional(configSync, field, nmsStatusDataPath("config_sync", field)); err != nil {
			return err
		}
	}
	for _, field := range []string{"running_commit_id", "last_error"} {
		if err := validateNMSStatusStringFieldOptional(configSync, field, nmsStatusDataPath("config_sync", field)); err != nil {
			return err
		}
	}
	for _, field := range []string{"last_check", "last_apply"} {
		if err := validateNMSStatusRFC3339FieldOptional(configSync, field, nmsStatusDataPath("config_sync", field)); err != nil {
			return err
		}
	}

	cluster, err := validateNMSStatusObjectField(object, "cluster")
	if err != nil {
		return err
	}
	if err := validateNMSStatusBoolFields(cluster, "cluster", "enabled", "etcd_sync_configured", "sync_aligned"); err != nil {
		return err
	}
	if err := validateNMSStatusIntFields(cluster, "cluster", "node_count"); err != nil {
		return err
	}
	if err := validateNMSStatusStringArrayFieldOptional(cluster, "etcd_endpoints", "cluster.etcd_endpoints"); err != nil {
		return err
	}

	overlay, err := validateNMSStatusObjectField(object, "overlay")
	if err != nil {
		return err
	}
	evpn, err := validateNMSStatusObjectFieldPath(overlay, "evpn", "overlay.evpn")
	if err != nil {
		return err
	}
	if err := validateNMSStatusBoolFields(evpn, "overlay.evpn", "configured"); err != nil {
		return err
	}
	if err := validateNMSStatusIntFields(evpn, "overlay.evpn", "vnis", "l2_vnis", "l3_vnis", "multicast_vnis"); err != nil {
		return err
	}
	if err := validateNMSStatusEVPNCounters(evpn); err != nil {
		return err
	}

	ha, err := validateNMSStatusObjectField(object, "ha")
	if err != nil {
		return err
	}
	if err := validateNMSStatusBoolFields(ha, "ha", "configured", "converged"); err != nil {
		return err
	}
	if err := validateNMSStatusIntFields(ha, "ha", "vrrp_groups", "issue_count"); err != nil {
		return err
	}
	if err := validateNMSStatusStringArrayFieldOptional(ha, "issues", "ha.issues"); err != nil {
		return err
	}
	if err := validateNMSStatusStringArrayCount(ha, "ha", "issues", "issue_count"); err != nil {
		return err
	}

	classOfService, err := validateNMSStatusObjectField(object, "class_of_service")
	if err != nil {
		return err
	}
	if err := validateNMSStatusBoolFields(classOfService, "class_of_service", "configured", "intent_only"); err != nil {
		return err
	}
	enforcementStatus, err := nmsStatusStringFieldValuePath(classOfService, "enforcement_status", "class_of_service.enforcement_status")
	if err != nil {
		return err
	}
	if err := validateNMSStatusCoSEnforcementStatus(enforcementStatus); err != nil {
		return err
	}
	if err := validateNMSStatusIntFields(classOfService, "class_of_service", "forwarding_classes", "traffic_control_profiles", "interface_bindings"); err != nil {
		return err
	}
	capabilities, err := validateNMSStatusObjectFieldPath(classOfService, "capabilities", "class_of_service.capabilities")
	if err != nil {
		return err
	}
	if err := validateNMSStatusBoolFields(capabilities, "class_of_service.capabilities", "metadata_binding_supported", "queue_scheduler_supported", "policer_supported", "counters_supported"); err != nil {
		return err
	}
	if err := validateNMSStatusStringArrayFieldOptional(capabilities, "diagnostics", "class_of_service.capabilities.diagnostics"); err != nil {
		return err
	}
	if err := validateNMSStatusRFC3339FieldOptional(capabilities, "last_check", "class_of_service.capabilities.last_check"); err != nil {
		return err
	}
	if err := validateNMSStatusStringFieldOptional(capabilities, "last_error", "class_of_service.capabilities.last_error"); err != nil {
		return err
	}

	frr, err := validateNMSStatusObjectField(object, "frr")
	if err != nil {
		return err
	}
	vrrp, err := validateNMSStatusObjectFieldPath(frr, "vrrp", "frr.vrrp")
	if err != nil {
		return err
	}
	if err := validateNMSStatusIntFields(vrrp, "frr.vrrp", "configured_groups", "observed_groups", "active_groups", "issue_count"); err != nil {
		return err
	}
	if err := validateNMSStatusRFC3339FieldOptional(vrrp, "last_check", "frr.vrrp.last_check"); err != nil {
		return err
	}
	if err := validateNMSStatusStringFieldOptional(vrrp, "last_error", "frr.vrrp.last_error"); err != nil {
		return err
	}
	vrrpGroups, err := nmsStatusObjectArrayFieldOptional(vrrp, "groups", "frr.vrrp.groups", validateNMSStatusVRRPGroup)
	if err != nil {
		return err
	}
	if err := validateNMSStatusStringArrayFieldOptional(vrrp, "issues", "frr.vrrp.issues"); err != nil {
		return err
	}
	if err := validateNMSStatusVRRPAggregates(vrrp, vrrpGroups); err != nil {
		return err
	}
	if err := validateNMSStatusStringArrayCount(vrrp, "frr.vrrp", "issues", "issue_count"); err != nil {
		return err
	}
	bfd, err := validateNMSStatusObjectFieldPath(frr, "bfd", "frr.bfd")
	if err != nil {
		return err
	}
	if err := validateNMSStatusIntFields(bfd, "frr.bfd", "configured_peers", "observed_peers", "up_peers", "down_peers", "session_down_events", "rx_fail_packets", "issue_count"); err != nil {
		return err
	}
	if err := validateNMSStatusRFC3339FieldOptional(bfd, "last_check", "frr.bfd.last_check"); err != nil {
		return err
	}
	if err := validateNMSStatusStringFieldOptional(bfd, "last_error", "frr.bfd.last_error"); err != nil {
		return err
	}
	bfdPeers, err := nmsStatusObjectArrayFieldOptional(bfd, "peers", "frr.bfd.peers", validateNMSStatusBFDPeer)
	if err != nil {
		return err
	}
	if err := validateNMSStatusStringArrayFieldOptional(bfd, "issues", "frr.bfd.issues"); err != nil {
		return err
	}
	if err := validateNMSStatusBFDAggregates(bfd, bfdPeers); err != nil {
		return err
	}
	if err := validateNMSStatusStringArrayCount(bfd, "frr.bfd", "issues", "issue_count"); err != nil {
		return err
	}

	vpp, err := validateNMSStatusObjectField(object, "vpp")
	if err != nil {
		return err
	}
	lcp, err := validateNMSStatusObjectFieldPath(vpp, "lcp", "vpp.lcp")
	if err != nil {
		return err
	}
	if err := validateNMSStatusIntFields(lcp, "vpp.lcp", "pair_count", "inconsistency_count"); err != nil {
		return err
	}
	if err := validateNMSStatusRFC3339FieldOptional(lcp, "last_reconcile", "vpp.lcp.last_reconcile"); err != nil {
		return err
	}
	if err := validateNMSStatusStringFieldOptional(lcp, "last_error", "vpp.lcp.last_error"); err != nil {
		return err
	}
	if err := validateNMSStatusStringArrayFieldOptional(lcp, "inconsistencies", "vpp.lcp.inconsistencies"); err != nil {
		return err
	}
	if err := validateNMSStatusStringArrayCount(lcp, "vpp.lcp", "inconsistencies", "inconsistency_count"); err != nil {
		return err
	}

	netconf, err := validateNMSStatusObjectField(object, "netconf")
	if err != nil {
		return err
	}
	if err := validateNMSStatusBoolFields(netconf, "netconf", "listening"); err != nil {
		return err
	}
	if err := validateNMSStatusIntFields(netconf, "netconf", "active_sessions", "active_connections"); err != nil {
		return err
	}
	for _, field := range []string{"total_connections", "successful_auth", "failed_auth"} {
		if err := validateNMSStatusUintFieldPath(netconf, field, nmsStatusDataPath("netconf", field)); err != nil {
			return err
		}
	}
	if err := validateNMSStatusNETCONFCounters(netconf); err != nil {
		return err
	}
	return nil
}

func validateNMSStatusStringField(object map[string]json.RawMessage, field string) error {
	return validateNMSStatusStringFieldPath(object, field, field)
}

func validateNMSStatusStringFieldPath(object map[string]json.RawMessage, field, path string) error {
	_, err := nmsStatusStringFieldValuePath(object, field, path)
	return err
}

func nmsStatusStringFieldValuePath(object map[string]json.RawMessage, field, path string) (string, error) {
	raw, ok := object[field]
	if !ok {
		return "", fmt.Errorf("nms status data %s is missing", path)
	}
	var value *string
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		if err != nil {
			return "", fmt.Errorf("nms status data %s must be a string: %w", path, err)
		}
		return "", fmt.Errorf("nms status data %s must be a string", path)
	}
	return *value, nil
}

func validateNMSStatusFloatField(object map[string]json.RawMessage, field string) error {
	return validateNMSStatusFloatFieldPath(object, field, field)
}

func validateNMSStatusFloatFieldPath(object map[string]json.RawMessage, field, path string) error {
	raw, ok := object[field]
	if !ok {
		return fmt.Errorf("nms status data %s is missing", path)
	}
	var value *float64
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		if err != nil {
			return fmt.Errorf("nms status data %s must be a number: %w", path, err)
		}
		return fmt.Errorf("nms status data %s must be a number", path)
	}
	if *value < 0 {
		return fmt.Errorf("nms status data %s must be non-negative", path)
	}
	return nil
}

func validateNMSStatusUintField(object map[string]json.RawMessage, field string) error {
	return validateNMSStatusUintFieldPath(object, field, field)
}

func validateNMSStatusUintFieldPath(object map[string]json.RawMessage, field, path string) error {
	_, err := nmsStatusUintFieldValuePath(object, field, path)
	return err
}

func nmsStatusUintFieldValuePath(object map[string]json.RawMessage, field, path string) (uint64, error) {
	raw, ok := object[field]
	if !ok {
		return 0, fmt.Errorf("nms status data %s is missing", path)
	}
	var value *uint64
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		if err != nil {
			return 0, fmt.Errorf("nms status data %s must be an unsigned integer: %w", path, err)
		}
		return 0, fmt.Errorf("nms status data %s must be an unsigned integer", path)
	}
	return *value, nil
}

func validateNMSStatusBoolFields(object map[string]json.RawMessage, parent string, fields ...string) error {
	for _, field := range fields {
		if err := validateNMSStatusBoolFieldPath(object, field, nmsStatusDataPath(parent, field)); err != nil {
			return err
		}
	}
	return nil
}

func validateNMSStatusBoolFieldPath(object map[string]json.RawMessage, field, path string) error {
	_, err := nmsStatusBoolFieldValuePath(object, field, path)
	return err
}

func nmsStatusBoolFieldValuePath(object map[string]json.RawMessage, field, path string) (bool, error) {
	raw, ok := object[field]
	if !ok {
		return false, fmt.Errorf("nms status data %s is missing", path)
	}
	var value *bool
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		if err != nil {
			return false, fmt.Errorf("nms status data %s must be a boolean: %w", path, err)
		}
		return false, fmt.Errorf("nms status data %s must be a boolean", path)
	}
	return *value, nil
}

func validateNMSStatusIntFields(object map[string]json.RawMessage, parent string, fields ...string) error {
	for _, field := range fields {
		if err := validateNMSStatusIntFieldPath(object, field, nmsStatusDataPath(parent, field)); err != nil {
			return err
		}
	}
	return nil
}

func validateNMSStatusIntFieldPath(object map[string]json.RawMessage, field, path string) error {
	_, err := nmsStatusIntFieldValuePath(object, field, path)
	return err
}

func validateNMSStatusIntFieldOptional(object map[string]json.RawMessage, field, path string) error {
	raw, ok := object[field]
	if !ok {
		return nil
	}
	var value *int64
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		if err != nil {
			return fmt.Errorf("nms status data %s must be an integer: %w", path, err)
		}
		return fmt.Errorf("nms status data %s must be an integer", path)
	}
	if *value < 0 {
		return fmt.Errorf("nms status data %s must be non-negative", path)
	}
	return nil
}

func nmsStatusIntFieldValuePath(object map[string]json.RawMessage, field, path string) (int64, error) {
	raw, ok := object[field]
	if !ok {
		return 0, fmt.Errorf("nms status data %s is missing", path)
	}
	var value *int64
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		if err != nil {
			return 0, fmt.Errorf("nms status data %s must be an integer: %w", path, err)
		}
		return 0, fmt.Errorf("nms status data %s must be an integer", path)
	}
	if *value < 0 {
		return 0, fmt.Errorf("nms status data %s must be non-negative", path)
	}
	return *value, nil
}

func validateNMSStatusObjectField(object map[string]json.RawMessage, field string) (map[string]json.RawMessage, error) {
	return validateNMSStatusObjectFieldPath(object, field, field)
}

func validateNMSStatusObjectFieldPath(object map[string]json.RawMessage, field, path string) (map[string]json.RawMessage, error) {
	raw, ok := object[field]
	if !ok {
		return nil, fmt.Errorf("nms status data %s is missing", path)
	}
	var section map[string]json.RawMessage
	if err := json.Unmarshal(raw, &section); err != nil {
		return nil, fmt.Errorf("nms status data %s must be a JSON object: %w", path, err)
	}
	if len(section) == 0 {
		return nil, fmt.Errorf("nms status data %s object is empty", path)
	}
	return section, nil
}

func validateNMSStatusStringArrayFieldOptional(object map[string]json.RawMessage, field, path string) error {
	_, err := nmsStatusStringArrayFieldLengthOptional(object, field, path)
	return err
}

func nmsStatusStringArrayFieldLengthOptional(object map[string]json.RawMessage, field, path string) (int, error) {
	raw, ok := object[field]
	if !ok {
		return 0, nil
	}
	var values []*string
	if err := json.Unmarshal(raw, &values); err != nil {
		return 0, fmt.Errorf("nms status data %s must be a string array: %w", path, err)
	}
	if values == nil {
		return 0, fmt.Errorf("nms status data %s must be a string array", path)
	}
	for i, value := range values {
		if value == nil {
			return 0, fmt.Errorf("nms status data %s[%d] must be a string", path, i)
		}
	}
	return len(values), nil
}

func validateNMSStatusObjectArrayFieldOptional(object map[string]json.RawMessage, field, path string, validate func(int, map[string]json.RawMessage) error) error {
	_, err := nmsStatusObjectArrayFieldOptional(object, field, path, validate)
	return err
}

func nmsStatusObjectArrayFieldOptional(object map[string]json.RawMessage, field, path string, validate func(int, map[string]json.RawMessage) error) ([]map[string]json.RawMessage, error) {
	raw, ok := object[field]
	if !ok {
		return nil, nil
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("nms status data %s must be a JSON object array: %w", path, err)
	}
	if values == nil {
		return nil, fmt.Errorf("nms status data %s must be a JSON object array", path)
	}
	objects := make([]map[string]json.RawMessage, 0, len(values))
	for i, rawValue := range values {
		var value map[string]json.RawMessage
		if err := json.Unmarshal(rawValue, &value); err != nil {
			return nil, fmt.Errorf("nms status data %s[%d] must be a JSON object: %w", path, i, err)
		}
		if len(value) == 0 {
			return nil, fmt.Errorf("nms status data %s[%d] object is empty", path, i)
		}
		if err := validate(i, value); err != nil {
			return nil, err
		}
		objects = append(objects, value)
	}
	return objects, nil
}

func validateNMSStatusVRRPGroup(index int, group map[string]json.RawMessage) error {
	path := fmt.Sprintf("frr.vrrp.groups[%d]", index)
	if err := validateNMSStatusStringFieldPath(group, "interface", nmsStatusDataPath(path, "interface")); err != nil {
		return err
	}
	if err := validateNMSStatusIntFieldPath(group, "id", nmsStatusDataPath(path, "id")); err != nil {
		return err
	}
	if err := validateNMSStatusStringFieldOptional(group, "virtual_address", nmsStatusDataPath(path, "virtual_address")); err != nil {
		return err
	}
	state, err := nmsStatusStringFieldValuePath(group, "state", nmsStatusDataPath(path, "state"))
	if err != nil {
		return err
	}
	observed, err := nmsStatusBoolFieldValuePath(group, "observed", nmsStatusDataPath(path, "observed"))
	if err != nil {
		return err
	}
	active, err := nmsStatusBoolFieldValuePath(group, "active", nmsStatusDataPath(path, "active"))
	if err != nil {
		return err
	}
	return validateNMSStatusVRRPGroupState(path, state, observed, active)
}

func validateNMSStatusBFDPeer(index int, peer map[string]json.RawMessage) error {
	path := fmt.Sprintf("frr.bfd.peers[%d]", index)
	if err := validateNMSStatusStringFieldPath(peer, "peer", nmsStatusDataPath(path, "peer")); err != nil {
		return err
	}
	for _, field := range []string{"local_address", "interface", "vrf", "diagnostic", "remote_diagnostic"} {
		if err := validateNMSStatusStringFieldOptional(peer, field, nmsStatusDataPath(path, field)); err != nil {
			return err
		}
	}
	status, err := nmsStatusStringFieldValuePath(peer, "status", nmsStatusDataPath(path, "status"))
	if err != nil {
		return err
	}
	observed, err := nmsStatusBoolFieldValuePath(peer, "observed", nmsStatusDataPath(path, "observed"))
	if err != nil {
		return err
	}
	up, err := nmsStatusBoolFieldValuePath(peer, "up", nmsStatusDataPath(path, "up"))
	if err != nil {
		return err
	}
	if err := validateNMSStatusBFDPeerStatus(path, status, observed, up); err != nil {
		return err
	}
	if err := validateNMSStatusIntFields(peer, path, "session_down_events", "rx_fail_packets"); err != nil {
		return err
	}
	return nil
}

func validateNMSStatusCoSEnforcementStatus(status string) error {
	switch strings.TrimSpace(status) {
	case "not configured", "intent-only":
		return nil
	default:
		return fmt.Errorf("nms status data class_of_service.enforcement_status = %q, want not configured or intent-only", status)
	}
}

func validateNMSStatusVRRPGroupState(path, state string, observed, active bool) error {
	normalized := strings.ToLower(strings.TrimSpace(state))
	statePath := nmsStatusDataPath(path, "state")
	if normalized == "" {
		return fmt.Errorf("nms status data %s must be non-empty", statePath)
	}
	if !observed {
		if active {
			return fmt.Errorf("nms status data %s must be false when %s is false", nmsStatusDataPath(path, "active"), nmsStatusDataPath(path, "observed"))
		}
		if normalized != "missing" {
			return fmt.Errorf("nms status data %s = %q, want missing when %s is false", statePath, state, nmsStatusDataPath(path, "observed"))
		}
		return nil
	}
	if normalized == "missing" {
		return fmt.Errorf("nms status data %s = %q, want observed VRRP state", statePath, state)
	}
	activeState := normalized == "master" || normalized == "backup"
	if active && !activeState {
		return fmt.Errorf("nms status data %s = %q, want Master or Backup when %s is true", statePath, state, nmsStatusDataPath(path, "active"))
	}
	if !active && activeState {
		return fmt.Errorf("nms status data %s must be true when %s is %q", nmsStatusDataPath(path, "active"), statePath, state)
	}
	return nil
}

func validateNMSStatusBFDPeerStatus(path, status string, observed, up bool) error {
	normalized := strings.ToLower(strings.TrimSpace(status))
	statusPath := nmsStatusDataPath(path, "status")
	if normalized == "" {
		return fmt.Errorf("nms status data %s must be non-empty", statusPath)
	}
	if !observed {
		if up {
			return fmt.Errorf("nms status data %s must be false when %s is false", nmsStatusDataPath(path, "up"), nmsStatusDataPath(path, "observed"))
		}
		if normalized != "missing" {
			return fmt.Errorf("nms status data %s = %q, want missing when %s is false", statusPath, status, nmsStatusDataPath(path, "observed"))
		}
		return nil
	}
	if normalized == "missing" {
		return fmt.Errorf("nms status data %s = %q, want observed BFD status", statusPath, status)
	}
	if up && normalized != "up" {
		return fmt.Errorf("nms status data %s = %q, want up when %s is true", statusPath, status, nmsStatusDataPath(path, "up"))
	}
	if !up && normalized == "up" {
		return fmt.Errorf("nms status data %s must be true when %s is %q", nmsStatusDataPath(path, "up"), statusPath, status)
	}
	return nil
}

func validateNMSStatusEVPNCounters(evpn map[string]json.RawMessage) error {
	configured, err := nmsStatusBoolFieldValuePath(evpn, "configured", "overlay.evpn.configured")
	if err != nil {
		return err
	}
	vnis, err := nmsStatusIntFieldValuePath(evpn, "vnis", "overlay.evpn.vnis")
	if err != nil {
		return err
	}
	l2VNIs, err := nmsStatusIntFieldValuePath(evpn, "l2_vnis", "overlay.evpn.l2_vnis")
	if err != nil {
		return err
	}
	l3VNIs, err := nmsStatusIntFieldValuePath(evpn, "l3_vnis", "overlay.evpn.l3_vnis")
	if err != nil {
		return err
	}
	multicastVNIs, err := nmsStatusIntFieldValuePath(evpn, "multicast_vnis", "overlay.evpn.multicast_vnis")
	if err != nil {
		return err
	}
	if configured != (vnis > 0) {
		return fmt.Errorf("nms status data overlay.evpn.configured = %t, want %t because overlay.evpn.vnis = %d", configured, vnis > 0, vnis)
	}
	for _, counter := range []struct {
		field string
		value int64
	}{
		{field: "l2_vnis", value: l2VNIs},
		{field: "l3_vnis", value: l3VNIs},
		{field: "multicast_vnis", value: multicastVNIs},
	} {
		if counter.value > vnis {
			return fmt.Errorf("nms status data overlay.evpn.%s = %d, want no more than overlay.evpn.vnis %d", counter.field, counter.value, vnis)
		}
	}
	if l2VNIs+l3VNIs > vnis {
		return fmt.Errorf("nms status data overlay.evpn.vnis = %d, want at least l2_vnis+l3_vnis %d", vnis, l2VNIs+l3VNIs)
	}
	return nil
}

func validateNMSStatusNETCONFCounters(netconf map[string]json.RawMessage) error {
	total, err := nmsStatusUintFieldValuePath(netconf, "total_connections", "netconf.total_connections")
	if err != nil {
		return err
	}
	success, err := nmsStatusUintFieldValuePath(netconf, "successful_auth", "netconf.successful_auth")
	if err != nil {
		return err
	}
	failed, err := nmsStatusUintFieldValuePath(netconf, "failed_auth", "netconf.failed_auth")
	if err != nil {
		return err
	}
	if success > total || failed > total || success > total-failed {
		return fmt.Errorf("nms status data netconf.total_connections = %d, want at least netconf.successful_auth %d plus netconf.failed_auth %d", total, success, failed)
	}
	return nil
}

func validateNMSStatusStringFieldOptional(object map[string]json.RawMessage, field, path string) error {
	raw, ok := object[field]
	if !ok {
		return nil
	}
	var value *string
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		if err != nil {
			return fmt.Errorf("nms status data %s must be a string: %w", path, err)
		}
		return fmt.Errorf("nms status data %s must be a string", path)
	}
	return nil
}

func validateNMSStatusRFC3339FieldOptional(object map[string]json.RawMessage, field, path string) error {
	raw, ok := object[field]
	if !ok {
		return nil
	}
	var value *string
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		if err != nil {
			return fmt.Errorf("nms status data %s must be an RFC3339 string: %w", path, err)
		}
		return fmt.Errorf("nms status data %s must be an RFC3339 string", path)
	}
	if strings.TrimSpace(*value) == "" {
		return fmt.Errorf("nms status data %s must be an RFC3339 string", path)
	}
	if _, err := time.Parse(time.RFC3339, *value); err != nil {
		return fmt.Errorf("nms status data %s = %q, want RFC3339: %w", path, *value, err)
	}
	return nil
}

func validateNMSStatusStringArrayCount(object map[string]json.RawMessage, parent, arrayField, countField string) error {
	arrayPath := nmsStatusDataPath(parent, arrayField)
	countPath := nmsStatusDataPath(parent, countField)
	length, err := nmsStatusStringArrayFieldLengthOptional(object, arrayField, arrayPath)
	if err != nil {
		return err
	}
	count, err := nmsStatusIntFieldValuePath(object, countField, countPath)
	if err != nil {
		return err
	}
	if count != int64(length) {
		return fmt.Errorf("nms status data %s = %d, want len(%s) %d", countPath, count, arrayPath, length)
	}
	return nil
}

func validateNMSStatusVRRPAggregates(vrrp map[string]json.RawMessage, groups []map[string]json.RawMessage) error {
	if groups == nil {
		return nil
	}
	configuredGroups, err := nmsStatusIntFieldValuePath(vrrp, "configured_groups", "frr.vrrp.configured_groups")
	if err != nil {
		return err
	}
	if configuredGroups != int64(len(groups)) {
		return fmt.Errorf("nms status data frr.vrrp.configured_groups = %d, want len(frr.vrrp.groups) %d", configuredGroups, len(groups))
	}
	var observedGroups, activeGroups int64
	for i, group := range groups {
		path := fmt.Sprintf("frr.vrrp.groups[%d]", i)
		observed, err := nmsStatusBoolFieldValuePath(group, "observed", nmsStatusDataPath(path, "observed"))
		if err != nil {
			return err
		}
		active, err := nmsStatusBoolFieldValuePath(group, "active", nmsStatusDataPath(path, "active"))
		if err != nil {
			return err
		}
		if observed {
			observedGroups++
		}
		if active {
			activeGroups++
		}
	}
	if err := validateNMSStatusAggregateCount(vrrp, "observed_groups", "frr.vrrp.observed_groups", observedGroups); err != nil {
		return err
	}
	return validateNMSStatusAggregateCount(vrrp, "active_groups", "frr.vrrp.active_groups", activeGroups)
}

func validateNMSStatusBFDAggregates(bfd map[string]json.RawMessage, peers []map[string]json.RawMessage) error {
	if peers == nil {
		return nil
	}
	configuredPeers, err := nmsStatusIntFieldValuePath(bfd, "configured_peers", "frr.bfd.configured_peers")
	if err != nil {
		return err
	}
	if configuredPeers > int64(len(peers)) {
		return fmt.Errorf("nms status data frr.bfd.configured_peers = %d, want no more than len(frr.bfd.peers) %d", configuredPeers, len(peers))
	}
	var observedPeers, upPeers, downPeers, sessionDownEvents, rxFailPackets int64
	for i, peer := range peers {
		path := fmt.Sprintf("frr.bfd.peers[%d]", i)
		observed, err := nmsStatusBoolFieldValuePath(peer, "observed", nmsStatusDataPath(path, "observed"))
		if err != nil {
			return err
		}
		if !observed {
			continue
		}
		observedPeers++
		up, err := nmsStatusBoolFieldValuePath(peer, "up", nmsStatusDataPath(path, "up"))
		if err != nil {
			return err
		}
		if up {
			upPeers++
		} else {
			downPeers++
		}
		sessionDown, err := nmsStatusIntFieldValuePath(peer, "session_down_events", nmsStatusDataPath(path, "session_down_events"))
		if err != nil {
			return err
		}
		rxFail, err := nmsStatusIntFieldValuePath(peer, "rx_fail_packets", nmsStatusDataPath(path, "rx_fail_packets"))
		if err != nil {
			return err
		}
		sessionDownEvents += sessionDown
		rxFailPackets += rxFail
	}
	for _, aggregate := range []struct {
		field string
		want  int64
	}{
		{field: "observed_peers", want: observedPeers},
		{field: "up_peers", want: upPeers},
		{field: "down_peers", want: downPeers},
		{field: "session_down_events", want: sessionDownEvents},
		{field: "rx_fail_packets", want: rxFailPackets},
	} {
		if err := validateNMSStatusAggregateCount(bfd, aggregate.field, nmsStatusDataPath("frr.bfd", aggregate.field), aggregate.want); err != nil {
			return err
		}
	}
	return nil
}

func validateNMSStatusAggregateCount(object map[string]json.RawMessage, field, path string, want int64) error {
	got, err := nmsStatusIntFieldValuePath(object, field, path)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("nms status data %s = %d, want %d", path, got, want)
	}
	return nil
}

func nmsStatusDataPath(parent, field string) string {
	if parent == "" {
		return field
	}
	return parent + "." + field
}

func validateNMSTelemetryMetadata(kind, eventSchemaVersion, encoding string) error {
	if eventSchemaVersion != telemetryEventSchemaV1 {
		return fmt.Errorf("%s event_schema_version = %q, want %q", kind, eventSchemaVersion, telemetryEventSchemaV1)
	}
	if encoding != telemetryEncodingJSON {
		return fmt.Errorf("%s encoding = %q, want %q", kind, encoding, telemetryEncodingJSON)
	}
	return nil
}

func validateNMSTelemetryHints(kind string, defaultPaths []string, defaultSampleIntervalMs, minSampleIntervalMs, maxSampleIntervalMs uint32) error {
	if len(defaultPaths) == 0 {
		return fmt.Errorf("%s default_paths is empty", kind)
	}
	for i, path := range defaultPaths {
		if strings.TrimSpace(path) == "" {
			return fmt.Errorf("%s default_paths[%d] is empty", kind, i)
		}
	}
	if defaultSampleIntervalMs == 0 || minSampleIntervalMs == 0 || maxSampleIntervalMs == 0 {
		return fmt.Errorf("%s sample interval hints must be positive", kind)
	}
	if minSampleIntervalMs > defaultSampleIntervalMs || defaultSampleIntervalMs > maxSampleIntervalMs {
		return fmt.Errorf("%s sample interval hints out of order: min %d default %d max %d",
			kind,
			minSampleIntervalMs,
			defaultSampleIntervalMs,
			maxSampleIntervalMs,
		)
	}
	return nil
}

func validateTelemetryCatalogPaths(paths []telemetryCatalogPath) error {
	seen := map[string]string{}
	for i, path := range paths {
		kind := fmt.Sprintf("telemetry catalog paths[%d]", i)
		if err := validateTelemetryPathMetadata(kind, path.Path, path.Cardinality, path.PayloadSchema, path.Aliases); err != nil {
			return err
		}
		if err := rememberTelemetryDiscoveryPath(seen, kind, "path", path.Path); err != nil {
			return err
		}
		for j, alias := range path.Aliases {
			if err := rememberTelemetryDiscoveryPath(seen, kind, fmt.Sprintf("aliases[%d]", j), alias); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateTelemetryPayloadSchemas(schemas []telemetryPayloadSchema) error {
	seen := map[string]string{}
	for i, schema := range schemas {
		kind := fmt.Sprintf("telemetry schemas[%d]", i)
		if err := validateTelemetryPathMetadata(kind, schema.Path, schema.Cardinality, schema.PayloadSchema, schema.Aliases); err != nil {
			return err
		}
		if err := rememberTelemetryDiscoveryPath(seen, kind, "path", schema.Path); err != nil {
			return err
		}
		for j, alias := range schema.Aliases {
			if err := rememberTelemetryDiscoveryPath(seen, kind, fmt.Sprintf("aliases[%d]", j), alias); err != nil {
				return err
			}
		}
		if len(schema.Fields) == 0 {
			return fmt.Errorf("%s fields is empty", kind)
		}
		for j, field := range schema.Fields {
			fieldKind := fmt.Sprintf("%s fields[%d]", kind, j)
			if strings.TrimSpace(field.Name) == "" {
				return fmt.Errorf("%s name is empty", fieldKind)
			}
			if strings.TrimSpace(field.Type) == "" {
				return fmt.Errorf("%s type is empty", fieldKind)
			}
			if strings.TrimSpace(field.Description) == "" {
				return fmt.Errorf("%s description is empty", fieldKind)
			}
		}
	}
	return nil
}

func validateTelemetryPathMetadata(kind, path, cardinality, payloadSchema string, aliases []string) error {
	if err := validateTelemetryPathValue(kind, "path", path); err != nil {
		return err
	}
	if strings.TrimSpace(cardinality) == "" {
		return fmt.Errorf("%s cardinality is empty", kind)
	}
	if strings.TrimSpace(payloadSchema) == "" {
		return fmt.Errorf("%s payload_schema is empty", kind)
	}
	for i, alias := range aliases {
		if err := validateTelemetryPathValue(kind, fmt.Sprintf("aliases[%d]", i), alias); err != nil {
			return err
		}
	}
	return nil
}

func validateTelemetryPathValue(kind, field, value string) error {
	path := strings.TrimSpace(value)
	if path == "" {
		return fmt.Errorf("%s %s is empty", kind, field)
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("%s %s = %q, want absolute telemetry path", kind, field, value)
	}
	if strings.Trim(path, "/") == "" {
		return fmt.Errorf("%s %s = %q, want non-root telemetry path", kind, field, value)
	}
	return nil
}

func rememberTelemetryDiscoveryPath(seen map[string]string, kind, field, value string) error {
	normalized := normalizeCatalogPath(value)
	if first, ok := seen[normalized]; ok {
		return fmt.Errorf("%s %s = %q duplicates %s", kind, field, value, first)
	}
	seen[normalized] = fmt.Sprintf("%s %s", kind, field)
	return nil
}

func validateTelemetrySnapshotEvents(events []telemetrySnapshotEvent) error {
	var previousSequence uint64
	for i, event := range events {
		kind := fmt.Sprintf("telemetry snapshot events[%d]", i)
		if event.SchemaVersion != telemetryEventSchemaV1 {
			return fmt.Errorf("%s schema_version = %q, want %q", kind, event.SchemaVersion, telemetryEventSchemaV1)
		}
		if event.Encoding != telemetryEncodingJSON {
			return fmt.Errorf("%s encoding = %q, want %q", kind, event.Encoding, telemetryEncodingJSON)
		}
		if event.EventType != telemetryEventSnapshot && event.EventType != telemetryEventError {
			return fmt.Errorf("%s event_type = %q, want %q or %q", kind, event.EventType, telemetryEventSnapshot, telemetryEventError)
		}
		if strings.TrimSpace(event.Path) == "" {
			return fmt.Errorf("%s path is empty", kind)
		}
		if len(event.Payload) == 0 {
			return fmt.Errorf("%s payload is empty", kind)
		}
		if event.PayloadBytes != len(event.Payload) {
			return fmt.Errorf("%s payload_bytes = %d, want len(payload) %d", kind, event.PayloadBytes, len(event.Payload))
		}
		if err := validateTelemetryPathMetadata(kind, event.Path, event.Cardinality, event.PayloadSchema, nil); err != nil {
			return err
		}
		if event.Sequence == 0 {
			return fmt.Errorf("%s sequence is zero", kind)
		}
		if previousSequence != 0 && event.Sequence <= previousSequence {
			return fmt.Errorf("%s sequence = %d, want greater than previous sequence %d", kind, event.Sequence, previousSequence)
		}
		previousSequence = event.Sequence
		if strings.TrimSpace(event.Timestamp) == "" {
			return fmt.Errorf("%s timestamp is empty", kind)
		}
		if _, err := time.Parse(time.RFC3339Nano, event.Timestamp); err != nil {
			return fmt.Errorf("%s timestamp = %q, want RFC3339Nano: %w", kind, event.Timestamp, err)
		}
	}
	return nil
}

func validateTelemetrySnapshotAggregates(snapshot telemetrySnapshotResponse) error {
	if len(snapshot.Paths) != len(snapshot.Events) {
		return fmt.Errorf("telemetry snapshot paths length = %d, want event count %d", len(snapshot.Paths), len(snapshot.Events))
	}
	payloadBytes := 0
	for i, event := range snapshot.Events {
		payloadBytes += event.PayloadBytes
		if snapshot.Paths[i] != event.Path {
			return fmt.Errorf("telemetry snapshot paths[%d] = %q, want event path %q", i, snapshot.Paths[i], event.Path)
		}
	}
	if snapshot.PayloadBytes != payloadBytes {
		return fmt.Errorf("telemetry snapshot payload_bytes = %d, want event payload_bytes total %d", snapshot.PayloadBytes, payloadBytes)
	}
	if snapshot.MaxPayloadBytes > 0 && snapshot.PayloadBytes > snapshot.MaxPayloadBytes {
		return fmt.Errorf("telemetry snapshot payload_bytes = %d exceeds max_payload_bytes %d", snapshot.PayloadBytes, snapshot.MaxPayloadBytes)
	}
	if snapshot.MaxEvents > 0 && snapshot.EventCount > snapshot.MaxEvents {
		return fmt.Errorf("telemetry snapshot event_count = %d exceeds max_events %d", snapshot.EventCount, snapshot.MaxEvents)
	}
	if snapshot.TimeoutMs < 0 {
		return fmt.Errorf("telemetry snapshot timeout_ms = %d, want non-negative value", snapshot.TimeoutMs)
	}
	return nil
}

func validateNMSResultCount(kind, countField string, count int, resultField string, resultLen int) error {
	if count != resultLen {
		return fmt.Errorf("%s %s = %d, want len(%s) %d", kind, countField, count, resultField, resultLen)
	}
	return nil
}

func fetchEndpoint(ctx context.Context, client *http.Client, cfg collectorConfig, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if cfg.username != "" || cfg.password != "" {
		req.SetBasicAuth(cfg.username, cfg.password)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(cfg.maxPayloadBytes)+(1<<20)))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("GET %s returned %s: %s", endpoint, resp.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func exportSnapshotToOTLP(ctx context.Context, client *http.Client, cfg collectorConfig, snapshot telemetrySnapshotResponse) error {
	request := buildOTLPLogsRequest(cfg, snapshot.Events)
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode OTLP logs request: %w", err)
	}
	endpoint, err := url.Parse(strings.TrimSpace(cfg.otlpEndpoint))
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return fmt.Errorf("otlp endpoint must include scheme and host")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("POST %s returned %s: %s", endpoint.String(), resp.Status, strings.TrimSpace(string(respBody)))
	}
	return nil
}

func buildOTLPLogsRequest(cfg collectorConfig, events []telemetrySnapshotEvent) otlpLogsRequest {
	records := make([]otlpLogRecord, 0, len(events))
	for _, event := range events {
		records = append(records, otlpLogRecord{
			TimeUnixNano:   otlpTimeUnixNano(event.Timestamp),
			SeverityText:   "Info",
			SeverityNumber: 9,
			Body:           otlpString(string(event.Payload)),
			Attributes: []otlpKeyValue{
				{Key: "arca.telemetry.path", Value: otlpString(event.Path)},
				{Key: "arca.telemetry.cardinality", Value: otlpString(event.Cardinality)},
				{Key: "arca.telemetry.payload_schema", Value: otlpString(event.PayloadSchema)},
				{Key: "arca.telemetry.event_type", Value: otlpString(event.EventType)},
				{Key: "arca.telemetry.sequence", Value: otlpInt(event.Sequence)},
				{Key: "arca.telemetry.schema_version", Value: otlpString(event.SchemaVersion)},
				{Key: "arca.telemetry.encoding", Value: otlpString(event.Encoding)},
				{Key: "arca.telemetry.payload_bytes", Value: otlpInt(uint64(event.PayloadBytes))},
			},
		})
	}
	return otlpLogsRequest{ResourceLogs: []otlpResourceLogs{{
		Resource: otlpResource{Attributes: []otlpKeyValue{
			{Key: "service.name", Value: otlpString(cfg.otlpServiceName)},
			{Key: "arca.collector.name", Value: otlpString("http-telemetry-collector")},
			{Key: "arca.collector.mode", Value: otlpString("nms-http")},
		}},
		ScopeLogs: []otlpScopeLogs{{
			Scope:      otlpInstrumentationScope{Name: "arca-router.examples.nms"},
			LogRecords: records,
		}},
	}}}
}

func otlpTimeUnixNano(timestamp string) string {
	if strings.TrimSpace(timestamp) == "" {
		return ""
	}
	parsed, err := time.Parse(time.RFC3339Nano, timestamp)
	if err != nil {
		return ""
	}
	return strconv.FormatInt(parsed.UnixNano(), 10)
}

func otlpString(value string) otlpAnyValue {
	return otlpAnyValue{StringValue: value}
}

func otlpInt(value uint64) otlpAnyValue {
	return otlpAnyValue{IntValue: strconv.FormatUint(value, 10)}
}

func resolveSnapshotPaths(ctx context.Context, client *http.Client, cfg collectorConfig) (repeatedPathFlag, error) {
	catalogURL, err := collectorEndpointURL(collectorConfig{
		baseURL:          cfg.baseURL,
		username:         cfg.username,
		password:         cfg.password,
		mode:             "catalog",
		includedPath:     append(repeatedPathFlag(nil), cfg.includedPath...),
		includedDefault:  cfg.includedDefault,
		includedCard:     append(repeatedStringFlag(nil), cfg.includedCard...),
		includedSchema:   append(repeatedStringFlag(nil), cfg.includedSchema...),
		includedEncoding: append(repeatedStringFlag(nil), cfg.includedEncoding...),
	})
	if err != nil {
		return nil, err
	}
	body, err := fetchEndpoint(ctx, client, cfg, catalogURL)
	if err != nil {
		return nil, err
	}
	catalog, err := decodeCatalogResponse(body)
	if err != nil {
		return nil, err
	}
	paths := cfg.paths
	if cfg.discoverPaths || len(paths) == 0 {
		paths = pathsFromCatalog(catalog)
	}
	filtered := filterSnapshotPathsByEncoding(paths, catalog, cfg.excludedEncoding)
	filtered = filterSnapshotPathsByPath(filtered, catalog, cfg.excludedPath)
	filtered = filterSnapshotPathsByCardinality(filtered, catalog, cfg.excludedCard)
	filtered = filterSnapshotPathsByPayloadSchema(filtered, catalog, cfg.excludedSchema)
	if len(filtered) == 0 {
		return nil, fmt.Errorf("snapshot path set is empty after applying catalog filters")
	}
	return filtered, nil
}

func pathsFromCatalog(catalog telemetryCatalogResponse) repeatedPathFlag {
	paths := make(repeatedPathFlag, 0, len(catalog.Paths))
	for _, path := range catalog.Paths {
		if path.Path != "" {
			paths = append(paths, path.Path)
		}
	}
	return paths
}

func filterSnapshotPathsByEncoding(paths repeatedPathFlag, catalog telemetryCatalogResponse, excluded repeatedStringFlag) repeatedPathFlag {
	if len(excluded) == 0 {
		return paths
	}
	encoding := strings.ToLower(strings.TrimSpace(catalog.Encoding))
	if encoding == "" {
		return paths
	}
	for _, value := range excluded {
		if encoding == strings.ToLower(strings.TrimSpace(value)) {
			return nil
		}
	}
	return paths
}

func filterSnapshotPathsByPath(paths repeatedPathFlag, catalog telemetryCatalogResponse, excluded repeatedPathFlag) repeatedPathFlag {
	if len(excluded) == 0 {
		return paths
	}
	excludedSet := make(map[string]struct{}, len(excluded))
	for _, path := range excluded {
		normalized := normalizeCatalogPath(path)
		if normalized != "" {
			excludedSet[normalized] = struct{}{}
		}
	}
	catalogByPath := telemetryCatalogPathLookup(catalog)
	filtered := make(repeatedPathFlag, 0, len(paths))
	for _, path := range paths {
		normalized := normalizeCatalogPath(path)
		if _, skip := excludedSet[normalized]; skip {
			continue
		}
		if entry, ok := catalogByPath[normalized]; ok && telemetryCatalogPathMatchesExcluded(entry, excludedSet) {
			continue
		}
		filtered = append(filtered, path)
	}
	return filtered
}

func telemetryCatalogPathMatchesExcluded(path telemetryCatalogPath, excluded map[string]struct{}) bool {
	if _, ok := excluded[normalizeCatalogPath(path.Path)]; ok {
		return true
	}
	for _, alias := range path.Aliases {
		if _, ok := excluded[normalizeCatalogPath(alias)]; ok {
			return true
		}
	}
	return false
}

func telemetryCatalogPathLookup(catalog telemetryCatalogResponse) map[string]telemetryCatalogPath {
	lookup := make(map[string]telemetryCatalogPath, len(catalog.Paths))
	for _, path := range catalog.Paths {
		for _, value := range append([]string{path.Path}, path.Aliases...) {
			normalized := normalizeCatalogPath(value)
			if normalized != "" {
				lookup[normalized] = path
			}
		}
	}
	return lookup
}

func filterSnapshotPathsByCardinality(paths repeatedPathFlag, catalog telemetryCatalogResponse, excluded repeatedStringFlag) repeatedPathFlag {
	if len(excluded) == 0 {
		return paths
	}
	excludedSet := make(map[string]struct{}, len(excluded))
	for _, value := range excluded {
		excludedSet[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	catalogByPath := telemetryCatalogPathLookup(catalog)
	filtered := make(repeatedPathFlag, 0, len(paths))
	for _, path := range paths {
		entry := catalogByPath[normalizeCatalogPath(path)]
		if _, skip := excludedSet[strings.ToLower(strings.TrimSpace(entry.Cardinality))]; skip {
			continue
		}
		filtered = append(filtered, path)
	}
	return filtered
}

func filterSnapshotPathsByPayloadSchema(paths repeatedPathFlag, catalog telemetryCatalogResponse, excluded repeatedStringFlag) repeatedPathFlag {
	if len(excluded) == 0 {
		return paths
	}
	excludedSet := make(map[string]struct{}, len(excluded))
	for _, value := range excluded {
		excludedSet[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	catalogByPath := telemetryCatalogPathLookup(catalog)
	filtered := make(repeatedPathFlag, 0, len(paths))
	for _, path := range paths {
		entry := catalogByPath[normalizeCatalogPath(path)]
		if _, skip := excludedSet[strings.ToLower(strings.TrimSpace(entry.PayloadSchema))]; skip {
			continue
		}
		filtered = append(filtered, path)
	}
	return filtered
}

func normalizeCatalogPath(value string) string {
	path := strings.ToLower(strings.TrimSpace(value))
	if path == "" {
		return ""
	}
	return "/" + strings.Trim(path, "/")
}

func collectorEndpointURL(cfg collectorConfig) (string, error) {
	var endpoint string
	switch cfg.mode {
	case "status":
		endpoint = "/api/nms/v1/status"
	case "catalog":
		endpoint = "/api/nms/v1/telemetry/paths"
	case "schemas":
		endpoint = "/api/nms/v1/telemetry/schemas"
	case "snapshot":
		endpoint = "/api/nms/v1/telemetry/snapshot"
	default:
		return "", fmt.Errorf("unsupported mode %q", cfg.mode)
	}
	u, err := endpointURL(cfg.baseURL, endpoint)
	if err != nil {
		return "", err
	}
	switch cfg.mode {
	case "catalog", "schemas":
		query := u.Query()
		if cfg.includedDefault {
			query.Set("default", "true")
		}
		for _, path := range cfg.includedPath {
			query.Add("path", path)
		}
		for _, value := range cfg.includedCard {
			query.Add("cardinality", value)
		}
		for _, value := range cfg.includedSchema {
			query.Add("payload_schema", value)
		}
		for _, value := range cfg.includedEncoding {
			query.Add("encoding", value)
		}
		u.RawQuery = query.Encode()
	case "snapshot":
		query := u.Query()
		for _, path := range cfg.paths {
			query.Add("path", path)
		}
		for _, path := range cfg.includedPath {
			query.Add("path", path)
		}
		if cfg.includedDefault {
			query.Set("default", "true")
		}
		for _, value := range cfg.includedCard {
			query.Add("cardinality", value)
		}
		for _, value := range cfg.includedSchema {
			query.Add("payload_schema", value)
		}
		for _, value := range cfg.includedEncoding {
			query.Add("encoding", value)
		}
		query.Set("timeout", cfg.timeout.String())
		query.Set("max_payload_bytes", strconv.Itoa(cfg.maxPayloadBytes))
		query.Set("max_events", strconv.Itoa(cfg.maxEvents))
		u.RawQuery = query.Encode()
	}
	return u.String(), nil
}

func endpointURL(baseURL, endpoint string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, err
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("base URL must include scheme and host")
	}
	u.Path = strings.TrimRight(u.Path, "/") + endpoint
	u.RawQuery = ""
	u.Fragment = ""
	return u, nil
}

func writePrettyJSON(w io.Writer, body []byte) error {
	var out bytes.Buffer
	if err := json.Indent(&out, body, "", "  "); err != nil {
		return fmt.Errorf("decode JSON response: %w", err)
	}
	out.WriteByte('\n')
	_, err := w.Write(out.Bytes())
	return err
}
