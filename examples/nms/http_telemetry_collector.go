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
	baseURL         string
	username        string
	password        string
	mode            string
	paths           repeatedPathFlag
	timeout         time.Duration
	maxPayloadBytes int
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
		if len(cfg.paths) == 0 {
			cfg.paths = append(repeatedPathFlag(nil), defaultSnapshotPaths...)
		}
	}
	return cfg, nil
}

func requestTimeout(cfg collectorConfig) time.Duration {
	if cfg.mode == "snapshot" && cfg.timeout > 0 {
		return cfg.timeout + 10*time.Second
	}
	return 10 * time.Second
}

func fetchNMS(ctx context.Context, client *http.Client, cfg collectorConfig) ([]byte, error) {
	endpoint, err := collectorEndpointURL(cfg)
	if err != nil {
		return nil, err
	}
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
	if cfg.mode == "snapshot" {
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
