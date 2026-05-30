package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	nbgrpc "github.com/akam1o/arca-router/internal/northbound/grpc"
)

func webSupportedStatus(supported bool) string {
	if supported {
		return "Supported"
	}
	return "Unsupported"
}

func webCoSDiagnosticText(capabilities webCoSCapabilities) string {
	if capabilities.LastError != "" {
		return "Detection failed"
	}
	if capabilities.MetadataBindingSupported && !capabilities.QueueSchedulerSupported &&
		!capabilities.PolicerSupported && !capabilities.CountersSupported {
		return "Metadata only"
	}
	if len(capabilities.Diagnostics) == 0 {
		return "None"
	}
	return fmt.Sprintf("%d diagnostics", len(capabilities.Diagnostics))
}

func newNMSStatusResponse(now time.Time, metrics routerMetrics) nmsStatusResponse {
	return nmsStatusResponse{
		SchemaVersion: nmsOperationalStatusSchemaVersion,
		GeneratedAt:   formatWebOptionalTime(now),
		Resource:      "/api/nms/v1/status",
		Data:          newWebStatus(metrics),
	}
}

func newNMSTelemetryCatalogResponse(now time.Time, filters nmsTelemetryCatalogFilters) nmsTelemetryCatalogResponse {
	catalog := nbgrpc.NewTelemetryCatalog()
	paths := make([]nmsTelemetryPath, 0, len(catalog.Paths))
	if nmsTelemetryCatalogFilterMatches(catalog.Encoding, filters.encodings) {
		for _, info := range catalog.Paths {
			if !nmsTelemetryPathMatchesCatalogFilters(info, filters) {
				continue
			}
			paths = append(paths, nmsTelemetryPath{
				Path:          info.Path,
				Description:   info.Description,
				Cardinality:   info.Cardinality,
				PayloadSchema: info.PayloadSchema,
				Aliases:       append([]string(nil), info.Aliases...),
				Default:       info.Default,
			})
		}
	}
	return nmsTelemetryCatalogResponse{
		SchemaVersion:           nmsTelemetryCatalogSchemaVersion,
		GeneratedAt:             formatWebOptionalTime(now),
		Resource:                "/api/nms/v1/telemetry/paths",
		EventSchemaVersion:      catalog.EventSchemaVersion,
		Encoding:                catalog.Encoding,
		DefaultPaths:            catalog.DefaultPaths,
		DefaultSampleIntervalMs: catalog.DefaultSampleIntervalMs,
		MinSampleIntervalMs:     catalog.MinSampleIntervalMs,
		MaxSampleIntervalMs:     catalog.MaxSampleIntervalMs,
		PathCount:               len(paths),
		Paths:                   paths,
	}
}

func newNMSTelemetrySchemasResponse(now time.Time, filters nmsTelemetryCatalogFilters) nmsTelemetrySchemasResponse {
	baseCatalog := nbgrpc.NewTelemetryCatalog()
	schemaCatalog := nbgrpc.NewFilteredTelemetryPayloadSchemaCatalog(nbgrpc.TelemetryCatalogFilter{
		Paths:          filters.paths,
		Cardinalities:  filters.cardinalities,
		PayloadSchemas: filters.payloadSchemas,
		Encodings:      filters.encodings,
		DefaultOnly:    filters.defaultOnly,
	})
	schemas := make([]nmsTelemetryPayloadSchema, 0, len(schemaCatalog))
	for _, info := range schemaCatalog {
		fields := make([]nmsTelemetryPayloadField, 0, len(info.Fields))
		for _, field := range info.Fields {
			fields = append(fields, nmsTelemetryPayloadField{
				Name:        field.Name,
				Type:        field.Type,
				Description: field.Description,
			})
		}
		schemas = append(schemas, nmsTelemetryPayloadSchema{
			Path:          info.Path,
			Description:   info.Description,
			Cardinality:   info.Cardinality,
			PayloadSchema: info.PayloadSchema,
			Aliases:       append([]string(nil), info.Aliases...),
			Default:       info.Default,
			Fields:        fields,
		})
	}
	return nmsTelemetrySchemasResponse{
		SchemaVersion:           nmsTelemetrySchemasSchemaVersion,
		GeneratedAt:             formatWebOptionalTime(now),
		Resource:                "/api/nms/v1/telemetry/schemas",
		EventSchemaVersion:      baseCatalog.EventSchemaVersion,
		Encoding:                baseCatalog.Encoding,
		DefaultPaths:            append([]string(nil), baseCatalog.DefaultPaths...),
		DefaultSampleIntervalMs: baseCatalog.DefaultSampleIntervalMs,
		MinSampleIntervalMs:     baseCatalog.MinSampleIntervalMs,
		MaxSampleIntervalMs:     baseCatalog.MaxSampleIntervalMs,
		SchemaCount:             len(schemas),
		Schemas:                 schemas,
	}
}

func nmsTelemetryCatalogFiltersFromRequest(r *http.Request) nmsTelemetryCatalogFilters {
	query := r.URL.Query()
	return nmsTelemetryCatalogFilters{
		paths:          nmsTelemetryCatalogFilterValues(query, "path"),
		cardinalities:  nmsTelemetryCatalogFilterValues(query, "cardinality"),
		payloadSchemas: nmsTelemetryCatalogFilterValues(query, "payload_schema", "payload-schema"),
		encodings:      nmsTelemetryCatalogFilterValues(query, "encoding"),
		defaultOnly:    nmsTelemetryCatalogDefaultOnlyFromQuery(query),
	}
}

func nmsTelemetryCatalogFilterValues(query url.Values, keys ...string) []string {
	var values []string
	for _, key := range keys {
		for _, raw := range query[key] {
			for _, part := range strings.Split(raw, ",") {
				value := strings.TrimSpace(part)
				if value != "" {
					values = append(values, value)
				}
			}
		}
	}
	return values
}

func nmsTelemetryPathMatchesCatalogFilters(info nbgrpc.TelemetryPathInfo, filters nmsTelemetryCatalogFilters) bool {
	if filters.defaultOnly && !info.Default {
		return false
	}
	if len(filters.paths) > 0 && !nmsTelemetryCatalogPathMatches(info, filters.paths) {
		return false
	}
	if len(filters.cardinalities) > 0 && !nmsTelemetryCatalogFilterMatches(info.Cardinality, filters.cardinalities) {
		return false
	}
	if len(filters.payloadSchemas) > 0 && !nmsTelemetryCatalogFilterMatches(info.PayloadSchema, filters.payloadSchemas) {
		return false
	}
	return true
}

func nmsTelemetryCatalogDefaultOnlyFromQuery(query url.Values) bool {
	for _, value := range append(append([]string(nil), query["default"]...), query["default_only"]...) {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "", "1", "true", "yes":
			return true
		}
	}
	return false
}

func nmsTelemetryCatalogPathMatches(info nbgrpc.TelemetryPathInfo, filters []string) bool {
	if nmsTelemetryCatalogFilterMatchesPathValue(info.Path, filters) {
		return true
	}
	for _, alias := range info.Aliases {
		if nmsTelemetryCatalogFilterMatchesPathValue(alias, filters) {
			return true
		}
	}
	return false
}

func nmsTelemetryCatalogFilterMatchesPathValue(value string, filters []string) bool {
	value = normalizeNMSTelemetryCatalogPathFilter(value)
	for _, filter := range filters {
		if value == normalizeNMSTelemetryCatalogPathFilter(filter) {
			return true
		}
	}
	return false
}

func normalizeNMSTelemetryCatalogPathFilter(value string) string {
	path := strings.ToLower(strings.TrimSpace(value))
	if path == "" {
		return ""
	}
	return "/" + strings.Trim(path, "/")
}

func nmsTelemetryCatalogFilterMatches(value string, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	value = strings.ToLower(strings.TrimSpace(value))
	for _, filter := range filters {
		if value == strings.ToLower(strings.TrimSpace(filter)) {
			return true
		}
	}
	return false
}

func (s metricsSource) collectNMSTelemetrySnapshot(ctx context.Context, opts nmsTelemetrySnapshotOptions) ([]nbgrpc.TelemetryEvent, int, error) {
	var events []nbgrpc.TelemetryEvent
	payloadBytes := 0
	err := s.telemetryAPI.SubscribeTelemetry(ctx, opts.paths, 0, true, func(event nbgrpc.TelemetryEvent) error {
		if len(events)+1 > opts.maxEvents {
			return fmt.Errorf("%w: %d events exceeds max_events %d", errNMSTelemetrySnapshotTooManyEvents, len(events)+1, opts.maxEvents)
		}
		payloadBytes += telemetryEventPayloadBytes(event)
		if payloadBytes > opts.maxPayloadBytes {
			return fmt.Errorf("%w: %d bytes exceeds max_payload_bytes %d", errNMSTelemetrySnapshotTooLarge, payloadBytes, opts.maxPayloadBytes)
		}
		events = append(events, event)
		return nil
	})
	if err != nil {
		return nil, payloadBytes, err
	}
	return events, payloadBytes, nil
}

func nmsTelemetrySnapshotOptionsFromRequest(r *http.Request) (nmsTelemetrySnapshotOptions, error) {
	opts := nmsTelemetrySnapshotOptions{
		paths:           nmsTelemetrySnapshotPaths(r),
		timeout:         defaultNMSTelemetrySnapshotTimeout,
		maxPayloadBytes: defaultNMSTelemetrySnapshotMaxPayloadBytes,
		maxEvents:       defaultNMSTelemetrySnapshotMaxEvents,
	}
	filters := nmsTelemetryCatalogFiltersFromRequest(r)
	filters.paths = opts.paths
	if nmsTelemetrySnapshotHasCatalogFilters(filters) {
		opts.paths = nmsTelemetrySnapshotPathsFromCatalogFilters(filters)
		if len(opts.paths) == 0 {
			return opts, fmt.Errorf("telemetry snapshot path set is empty after catalog filters")
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("timeout")); raw != "" {
		timeout, err := time.ParseDuration(raw)
		if err != nil || timeout <= 0 {
			return opts, fmt.Errorf("invalid telemetry snapshot timeout %q", raw)
		}
		if timeout > maxNMSTelemetrySnapshotTimeout {
			return opts, fmt.Errorf("telemetry snapshot timeout %s exceeds max %s", timeout, maxNMSTelemetrySnapshotTimeout)
		}
		opts.timeout = timeout
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("max_payload_bytes")); raw != "" {
		maxPayloadBytes, err := strconv.Atoi(raw)
		if err != nil || maxPayloadBytes <= 0 {
			return opts, fmt.Errorf("invalid telemetry snapshot max_payload_bytes %q", raw)
		}
		if maxPayloadBytes > maxNMSTelemetrySnapshotMaxPayloadBytes {
			return opts, fmt.Errorf("telemetry snapshot max_payload_bytes %d exceeds max %d", maxPayloadBytes, maxNMSTelemetrySnapshotMaxPayloadBytes)
		}
		opts.maxPayloadBytes = maxPayloadBytes
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("max_events")); raw != "" {
		maxEvents, err := strconv.Atoi(raw)
		if err != nil || maxEvents <= 0 {
			return opts, fmt.Errorf("invalid telemetry snapshot max_events %q", raw)
		}
		if maxEvents > maxNMSTelemetrySnapshotMaxEvents {
			return opts, fmt.Errorf("telemetry snapshot max_events %d exceeds max %d", maxEvents, maxNMSTelemetrySnapshotMaxEvents)
		}
		opts.maxEvents = maxEvents
	}
	return opts, nil
}

func nmsTelemetrySnapshotHasCatalogFilters(filters nmsTelemetryCatalogFilters) bool {
	return filters.defaultOnly || len(filters.cardinalities) > 0 || len(filters.payloadSchemas) > 0 || len(filters.encodings) > 0
}

func nmsTelemetrySnapshotPathsFromCatalogFilters(filters nmsTelemetryCatalogFilters) []string {
	catalog := nbgrpc.NewFilteredTelemetryCatalog(nbgrpc.TelemetryCatalogFilter{
		Paths:          filters.paths,
		Cardinalities:  filters.cardinalities,
		PayloadSchemas: filters.payloadSchemas,
		Encodings:      filters.encodings,
		DefaultOnly:    filters.defaultOnly,
	})
	paths := make([]string, 0, len(catalog.Paths))
	for _, info := range catalog.Paths {
		paths = append(paths, info.Path)
	}
	return paths
}

func nmsTelemetrySnapshotPaths(r *http.Request) []string {
	rawPaths := r.URL.Query()["path"]
	paths := make([]string, 0, len(rawPaths))
	for _, rawPath := range rawPaths {
		for _, part := range strings.Split(rawPath, ",") {
			if path := strings.TrimSpace(part); path != "" {
				paths = append(paths, path)
			}
		}
	}
	return paths
}

func newNMSTelemetrySnapshotResponse(now time.Time, events []nbgrpc.TelemetryEvent, opts nmsTelemetrySnapshotOptions, payloadBytes int) nmsTelemetrySnapshotResponse {
	catalog := nbgrpc.NewTelemetryCatalog()
	responseEvents := make([]nmsTelemetrySnapshotEvent, 0, len(events))
	paths := make([]string, 0, len(events))
	for _, event := range events {
		responseEvents = append(responseEvents, newNMSTelemetrySnapshotEvent(event))
		paths = append(paths, event.Path)
	}
	return nmsTelemetrySnapshotResponse{
		SchemaVersion:           nmsTelemetrySnapshotSchemaVersion,
		GeneratedAt:             formatWebOptionalTime(now),
		Resource:                "/api/nms/v1/telemetry/snapshot",
		EventSchemaVersion:      catalog.EventSchemaVersion,
		Encoding:                catalog.Encoding,
		DefaultPaths:            append([]string(nil), catalog.DefaultPaths...),
		DefaultSampleIntervalMs: catalog.DefaultSampleIntervalMs,
		MinSampleIntervalMs:     catalog.MinSampleIntervalMs,
		MaxSampleIntervalMs:     catalog.MaxSampleIntervalMs,
		Paths:                   paths,
		EventCount:              len(responseEvents),
		PayloadBytes:            payloadBytes,
		MaxPayloadBytes:         opts.maxPayloadBytes,
		MaxEvents:               opts.maxEvents,
		TimeoutMs:               opts.timeout.Milliseconds(),
		Events:                  responseEvents,
	}
}

func newNMSTelemetrySnapshotEvent(event nbgrpc.TelemetryEvent) nmsTelemetrySnapshotEvent {
	output := nmsTelemetrySnapshotEvent{
		Sequence:      event.Sequence,
		Path:          event.Path,
		Cardinality:   event.Cardinality,
		PayloadSchema: event.PayloadSchema,
		EventType:     event.EventType,
		Encoding:      event.Encoding,
		SchemaVersion: event.SchemaVersion,
		PayloadBytes:  telemetryEventPayloadBytes(event),
		Payload:       telemetrySnapshotPayload(event.JSONPayload),
	}
	if !event.Timestamp.IsZero() {
		output.Timestamp = event.Timestamp.UTC().Format(time.RFC3339Nano)
	}
	return output
}

func telemetryEventPayloadBytes(event nbgrpc.TelemetryEvent) int {
	if event.PayloadBytes > 0 {
		return event.PayloadBytes
	}
	return len(event.JSONPayload)
}

func telemetrySnapshotPayload(payload string) json.RawMessage {
	if payload == "" {
		return json.RawMessage("null")
	}
	if json.Valid([]byte(payload)) {
		return json.RawMessage(payload)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return json.RawMessage("null")
	}
	return json.RawMessage(encoded)
}
