package main

import (
	"bytes"
	"encoding/json"
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
