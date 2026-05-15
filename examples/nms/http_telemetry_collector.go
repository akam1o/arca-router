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
)

var defaultSnapshotPaths = []string{"/system", "/interfaces", "/overlays/evpn"}

type collectorConfig struct {
	baseURL          string
	username         string
	password         string
	mode             string
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
	timeout          time.Duration
	maxPayloadBytes  int
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
	Paths []telemetryCatalogPath `json:"paths"`
}

type telemetryCatalogPath struct {
	Path          string   `json:"path"`
	Cardinality   string   `json:"cardinality"`
	PayloadSchema string   `json:"payload_schema"`
	Aliases       []string `json:"aliases"`
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
		timeout:         defaultSnapshotTimeout,
		maxPayloadBytes: defaultMaxPayloadBytes,
	}
	fs := flag.NewFlagSet("http-telemetry-collector", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.baseURL, "base-url", cfg.baseURL, "Base Web API URL")
	fs.StringVar(&cfg.username, "user", "", "HTTP Basic username")
	fs.StringVar(&cfg.password, "password", "", "HTTP Basic password")
	fs.StringVar(&cfg.mode, "mode", cfg.mode, "Endpoint mode: snapshot, status, or catalog")
	fs.Var(&cfg.paths, "path", "Telemetry path for snapshot mode; repeat for multiple paths")
	fs.BoolVar(&cfg.discoverPaths, "discover-paths", false, "Use telemetry catalog paths as the snapshot path set")
	fs.Var(&cfg.includedPath, "include-path", "Telemetry path or alias to request from catalog discovery; repeat for multiple values")
	fs.BoolVar(&cfg.includedDefault, "include-default", false, "Request only default telemetry paths from catalog discovery")
	fs.Var(&cfg.includedCard, "include-cardinality", "Telemetry cardinality to request from catalog discovery; repeat for multiple values")
	fs.Var(&cfg.includedSchema, "include-payload-schema", "Telemetry payload schema ID to request from catalog discovery; repeat for multiple values")
	fs.Var(&cfg.includedEncoding, "include-encoding", "Telemetry payload encoding to request from catalog discovery; repeat for multiple values")
	fs.Var(&cfg.excludedPath, "exclude-path", "Telemetry path or alias to exclude from snapshot mode; repeat for multiple values")
	fs.Var(&cfg.excludedCard, "exclude-cardinality", "Telemetry cardinality to exclude from snapshot mode; repeat for multiple values")
	fs.Var(&cfg.excludedSchema, "exclude-payload-schema", "Telemetry payload schema ID to exclude from snapshot mode; repeat for multiple values")
	fs.DurationVar(&cfg.timeout, "timeout", cfg.timeout, "Snapshot timeout")
	fs.IntVar(&cfg.maxPayloadBytes, "max-payload-bytes", cfg.maxPayloadBytes, "Snapshot payload byte budget")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	cfg.mode = strings.ToLower(strings.TrimSpace(cfg.mode))
	switch cfg.mode {
	case "snapshot", "status", "catalog":
	default:
		return cfg, fmt.Errorf("unsupported mode %q", cfg.mode)
	}
	if cfg.mode == "snapshot" {
		if cfg.timeout <= 0 {
			return cfg, fmt.Errorf("timeout must be positive")
		}
		if cfg.maxPayloadBytes <= 0 {
			return cfg, fmt.Errorf("max-payload-bytes must be positive")
		}
		if len(cfg.paths) == 0 && !usesCatalogDiscovery(cfg) {
			cfg.paths = append(repeatedPathFlag(nil), defaultSnapshotPaths...)
		}
	}
	return cfg, nil
}

func usesCatalogDiscovery(cfg collectorConfig) bool {
	return cfg.discoverPaths || cfg.includedDefault || len(cfg.includedPath) > 0 || len(cfg.includedCard) > 0 || len(cfg.includedSchema) > 0 || len(cfg.includedEncoding) > 0
}

func needsCatalogResolution(cfg collectorConfig) bool {
	return usesCatalogDiscovery(cfg) || len(cfg.excludedPath) > 0 || len(cfg.excludedCard) > 0 || len(cfg.excludedSchema) > 0
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
	return fetchEndpoint(ctx, client, cfg, endpoint)
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
	filtered := filterSnapshotPathsByPath(paths, catalog, cfg.excludedPath)
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
	catalogByPath := make(map[string]telemetryCatalogPath, len(catalog.Paths))
	for _, path := range catalog.Paths {
		catalogByPath[normalizeCatalogPath(path.Path)] = path
	}
	filtered := make(repeatedPathFlag, 0, len(paths))
	for _, path := range paths {
		normalized := normalizeCatalogPath(path)
		if _, skip := excludedSet[normalized]; skip {
			continue
		}
		if entry, ok := catalogByPath[normalized]; ok && telemetryCatalogPathHasExcludedAlias(entry, excludedSet) {
			continue
		}
		filtered = append(filtered, path)
	}
	return filtered
}

func telemetryCatalogPathHasExcludedAlias(path telemetryCatalogPath, excluded map[string]struct{}) bool {
	for _, alias := range path.Aliases {
		if _, ok := excluded[normalizeCatalogPath(alias)]; ok {
			return true
		}
	}
	return false
}

func filterSnapshotPathsByCardinality(paths repeatedPathFlag, catalog telemetryCatalogResponse, excluded repeatedStringFlag) repeatedPathFlag {
	if len(excluded) == 0 {
		return paths
	}
	excludedSet := make(map[string]struct{}, len(excluded))
	for _, value := range excluded {
		excludedSet[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	cardinalityByPath := make(map[string]string, len(catalog.Paths))
	for _, path := range catalog.Paths {
		cardinalityByPath[path.Path] = strings.ToLower(strings.TrimSpace(path.Cardinality))
	}
	filtered := make(repeatedPathFlag, 0, len(paths))
	for _, path := range paths {
		if _, skip := excludedSet[cardinalityByPath[path]]; skip {
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
	schemaByPath := make(map[string]string, len(catalog.Paths))
	for _, path := range catalog.Paths {
		schemaByPath[path.Path] = strings.ToLower(strings.TrimSpace(path.PayloadSchema))
	}
	filtered := make(repeatedPathFlag, 0, len(paths))
	for _, path := range paths {
		if _, skip := excludedSet[schemaByPath[path]]; skip {
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
	case "catalog":
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
		query.Set("timeout", cfg.timeout.String())
		query.Set("max_payload_bytes", strconv.Itoa(cfg.maxPayloadBytes))
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
