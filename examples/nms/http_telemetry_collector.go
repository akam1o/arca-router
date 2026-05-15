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
	nmsTelemetryCatalogV1  = "arca.nms.telemetry-catalog.v1"
	nmsTelemetrySchemasV1  = "arca.nms.telemetry-schemas.v1"
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

type telemetryCatalogResponse struct {
	SchemaVersion           string                 `json:"schema_version"`
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
	Path          string   `json:"path"`
	Cardinality   string   `json:"cardinality"`
	PayloadSchema string   `json:"payload_schema"`
	Aliases       []string `json:"aliases"`
}

type telemetrySchemasResponse struct {
	SchemaVersion           string                   `json:"schema_version"`
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
	Path          string                  `json:"path"`
	Cardinality   string                  `json:"cardinality"`
	PayloadSchema string                  `json:"payload_schema"`
	Aliases       []string                `json:"aliases"`
	Fields        []telemetryPayloadField `json:"fields"`
}

type telemetryPayloadField struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

type telemetrySnapshotResponse struct {
	DefaultPaths            []string                 `json:"default_paths"`
	DefaultSampleIntervalMs uint32                   `json:"default_sample_interval_ms"`
	MinSampleIntervalMs     uint32                   `json:"min_sample_interval_ms"`
	MaxSampleIntervalMs     uint32                   `json:"max_sample_interval_ms"`
	EventCount              int                      `json:"event_count"`
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
	if cfg.mode == "snapshot" && cfg.otlpEndpoint != "" {
		if err := exportSnapshotToOTLP(ctx, client, cfg, body); err != nil {
			return nil, err
		}
	}
	return body, nil
}

func decodeDiscoveryResponse(cfg collectorConfig, body []byte) error {
	switch cfg.mode {
	case "catalog":
		var catalog telemetryCatalogResponse
		if err := json.Unmarshal(body, &catalog); err != nil {
			return fmt.Errorf("decode telemetry catalog response: %w", err)
		}
		if err := validateNMSDiscoveryEnvelope("telemetry catalog", catalog.SchemaVersion, catalog.Resource, nmsTelemetryCatalogV1, "/api/nms/v1/telemetry/paths"); err != nil {
			return err
		}
	case "schemas":
		var schemas telemetrySchemasResponse
		if err := json.Unmarshal(body, &schemas); err != nil {
			return fmt.Errorf("decode telemetry schemas response: %w", err)
		}
		if err := validateNMSDiscoveryEnvelope("telemetry schemas", schemas.SchemaVersion, schemas.Resource, nmsTelemetrySchemasV1, "/api/nms/v1/telemetry/schemas"); err != nil {
			return err
		}
	}
	return nil
}

func validateNMSDiscoveryEnvelope(kind, schemaVersion, resource, wantSchemaVersion, wantResource string) error {
	if schemaVersion != wantSchemaVersion {
		return fmt.Errorf("%s schema_version = %q, want %q", kind, schemaVersion, wantSchemaVersion)
	}
	if resource != wantResource {
		return fmt.Errorf("%s resource = %q, want %q", kind, resource, wantResource)
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

func exportSnapshotToOTLP(ctx context.Context, client *http.Client, cfg collectorConfig, snapshotBody []byte) error {
	var snapshot telemetrySnapshotResponse
	if err := json.Unmarshal(snapshotBody, &snapshot); err != nil {
		return fmt.Errorf("decode telemetry snapshot for OTLP export: %w", err)
	}
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
	var catalog telemetryCatalogResponse
	if err := json.Unmarshal(body, &catalog); err != nil {
		return nil, fmt.Errorf("decode telemetry catalog: %w", err)
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
