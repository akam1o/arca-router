package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	grpcclient "github.com/akam1o/arca-router/internal/northbound/grpc"
)

type telemetryCLIOptions struct {
	paths    []string
	interval time.Duration
	once     bool
	count    int
}

type telemetryOutputEvent struct {
	Sequence      uint64          `json:"sequence"`
	Timestamp     string          `json:"timestamp,omitempty"`
	Path          string          `json:"path"`
	Cardinality   string          `json:"cardinality,omitempty"`
	PayloadSchema string          `json:"payload_schema,omitempty"`
	EventType     string          `json:"event_type"`
	Encoding      string          `json:"encoding"`
	SchemaVersion string          `json:"schema_version"`
	PayloadBytes  int             `json:"payload_bytes"`
	Payload       json.RawMessage `json:"payload"`
}

func showTelemetry(ctx context.Context, client showClient, args []string) error {
	catalogOpts, isCatalog, err := telemetryCatalogOptions(args)
	if isCatalog {
		if err != nil {
			return err
		}
		catalog := grpcclient.NewTelemetryCatalog()
		if catalogOpts.live {
			liveCatalog, err := client.GetTelemetryCatalogWithFilter(ctx, grpcclient.TelemetryCatalogFilter{
				Paths:          catalogOpts.paths,
				Cardinalities:  catalogOpts.cardinalities,
				PayloadSchemas: catalogOpts.payloadSchemas,
				Encodings:      catalogOpts.encodings,
				DefaultOnly:    catalogOpts.defaultOnly,
			})
			if err != nil {
				return err
			}
			catalog = liveCatalog
			catalogOpts.defaultOnly = false
			catalogOpts.paths = nil
			catalogOpts.cardinalities = nil
			catalogOpts.payloadSchemas = nil
			catalogOpts.encodings = nil
		}
		printTelemetryCatalog(catalog, filterTelemetryPathCatalog(catalog.Paths, catalogOpts))
		return nil
	}
	opts, err := telemetryOptions(args)
	if err != nil {
		return err
	}
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream, err := client.SubscribeTelemetry(streamCtx, opts.paths, opts.interval, opts.once)
	if err != nil {
		return err
	}
	events := 0
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err := printTelemetryEvent(event); err != nil {
			return err
		}
		events++
		if opts.count > 0 && events >= opts.count {
			return nil
		}
	}
}

type telemetryCatalogCLIOptions struct {
	live           bool
	defaultOnly    bool
	paths          []string
	cardinalities  []string
	payloadSchemas []string
	encodings      []string
}

func isTelemetryCatalogCommand(args []string) bool {
	_, ok, err := telemetryCatalogOptions(args)
	return ok && err == nil
}

func telemetryCatalogOptions(args []string) (telemetryCatalogCLIOptions, bool, error) {
	var opts telemetryCatalogCLIOptions
	if len(args) == 0 || (args[0] != "paths" && args[0] != "catalog") {
		return opts, false, nil
	}
	args = args[1:]
	for len(args) > 0 {
		switch args[0] {
		case "live":
			if opts.live {
				return opts, true, telemetryUsageError("'show telemetry paths live' specified more than once")
			}
			opts.live = true
			args = args[1:]
		case "default", "default-only":
			opts.defaultOnly = true
			args = args[1:]
		case "cardinality":
			if len(args) < 2 {
				return opts, true, telemetryUsageError("'show telemetry paths cardinality' requires a cardinality hint")
			}
			opts.cardinalities = append(opts.cardinalities, args[1])
			args = args[2:]
		case "path":
			if len(args) < 2 {
				return opts, true, telemetryUsageError("'show telemetry paths path' requires a telemetry path or alias")
			}
			opts.paths = append(opts.paths, args[1])
			args = args[2:]
		case "payload-schema", "schema":
			if len(args) < 2 {
				return opts, true, telemetryUsageError("'show telemetry paths payload-schema' requires a schema ID")
			}
			opts.payloadSchemas = append(opts.payloadSchemas, args[1])
			args = args[2:]
		case "encoding":
			if len(args) < 2 {
				return opts, true, telemetryUsageError("'show telemetry paths encoding' requires a payload encoding")
			}
			opts.encodings = append(opts.encodings, args[1])
			args = args[2:]
		default:
			return opts, true, telemetryUsageError("unknown telemetry catalog option: %s", args[0])
		}
	}
	return opts, true, nil
}

func filterTelemetryPathCatalog(catalog []grpcclient.TelemetryPathInfo, opts telemetryCatalogCLIOptions) []grpcclient.TelemetryPathInfo {
	paths := normalizedCatalogPathFilterSet(opts.paths)
	cardinalities := normalizedCatalogFilterSet(opts.cardinalities)
	payloadSchemas := normalizedCatalogFilterSet(opts.payloadSchemas)
	encodings := normalizedCatalogFilterSet(opts.encodings)
	if len(encodings) > 0 {
		if _, ok := encodings[normalizedCatalogFilterValue(grpcclient.TelemetryEncoding())]; !ok {
			return nil
		}
	}
	if !opts.defaultOnly && len(paths) == 0 && len(cardinalities) == 0 && len(payloadSchemas) == 0 && len(encodings) == 0 {
		return catalog
	}

	filtered := make([]grpcclient.TelemetryPathInfo, 0, len(catalog))
	for _, info := range catalog {
		if opts.defaultOnly && !info.Default {
			continue
		}
		if len(paths) > 0 && !telemetryCatalogInfoMatchesPath(info, paths) {
			continue
		}
		if len(cardinalities) > 0 {
			if _, ok := cardinalities[normalizedCatalogFilterValue(info.Cardinality)]; !ok {
				continue
			}
		}
		if len(payloadSchemas) > 0 {
			if _, ok := payloadSchemas[normalizedCatalogFilterValue(info.PayloadSchema)]; !ok {
				continue
			}
		}
		filtered = append(filtered, info)
	}
	return filtered
}

func normalizedCatalogPathFilterSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := normalizedCatalogPathFilterValue(value)
		if normalized != "" {
			set[normalized] = struct{}{}
		}
	}
	return set
}

func telemetryCatalogInfoMatchesPath(info grpcclient.TelemetryPathInfo, paths map[string]struct{}) bool {
	if _, ok := paths[normalizedCatalogPathFilterValue(info.Path)]; ok {
		return true
	}
	for _, alias := range info.Aliases {
		if _, ok := paths[normalizedCatalogPathFilterValue(alias)]; ok {
			return true
		}
	}
	return false
}

func normalizedCatalogPathFilterValue(value string) string {
	path := strings.ToLower(strings.TrimSpace(value))
	if path == "" {
		return ""
	}
	return "/" + strings.Trim(path, "/")
}

func normalizedCatalogFilterSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := normalizedCatalogFilterValue(value)
		if normalized != "" {
			set[normalized] = struct{}{}
		}
	}
	return set
}

func normalizedCatalogFilterValue(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func printTelemetryCatalog(catalog grpcclient.TelemetryCatalog, paths []grpcclient.TelemetryPathInfo) {
	if hint := formatTelemetryCatalogIntervalHints(catalog); hint != "" {
		fmt.Println(hint)
	}
	printTelemetryPathCatalog(paths)
}

func formatTelemetryCatalogIntervalHints(catalog grpcclient.TelemetryCatalog) string {
	if catalog.DefaultSampleIntervalMs == 0 && catalog.MinSampleIntervalMs == 0 && catalog.MaxSampleIntervalMs == 0 {
		return ""
	}
	return fmt.Sprintf("Sample interval: default=%dms min=%dms max=%dms",
		catalog.DefaultSampleIntervalMs,
		catalog.MinSampleIntervalMs,
		catalog.MaxSampleIntervalMs)
}

func printTelemetryPathCatalog(catalog []grpcclient.TelemetryPathInfo) {
	if len(catalog) == 0 {
		fmt.Println("No telemetry paths found")
		return
	}
	fmt.Printf("%-28s %-18s %-8s %-28s %-42s %s\n", "Path", "Cardinality", "Default", "Aliases", "Payload schema", "Description")
	fmt.Println(strings.Repeat("-", 168))
	for _, info := range catalog {
		fmt.Printf("%-28s %-18s %-8s %-28s %-42s %s\n",
			formatTelemetryCatalogValue(info.Path),
			formatTelemetryCatalogValue(info.Cardinality),
			yesNo(info.Default),
			formatTelemetryCatalogList(info.Aliases),
			formatTelemetryCatalogValue(info.PayloadSchema),
			formatTelemetryCatalogValue(info.Description),
		)
	}
}

func formatTelemetryCatalogValue(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func formatTelemetryCatalogList(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ",")
}

func telemetryOptions(args []string) (telemetryCLIOptions, error) {
	opts := telemetryCLIOptions{once: true}
	for len(args) > 0 {
		switch args[0] {
		case "path":
			if len(args) < 2 {
				return opts, telemetryUsageError("'show telemetry path' requires a path")
			}
			opts.paths = append(opts.paths, args[1])
			args = args[2:]
		case "interval":
			if len(args) < 2 {
				return opts, telemetryUsageError("'show telemetry interval' requires a duration such as 5s")
			}
			interval, err := time.ParseDuration(args[1])
			if err != nil || interval <= 0 {
				return opts, telemetryUsageError("invalid telemetry interval %q", args[1])
			}
			opts.interval = interval
			args = args[2:]
		case "count":
			if len(args) < 2 {
				return opts, telemetryUsageError("'show telemetry count' requires a positive event count")
			}
			count, err := strconv.Atoi(args[1])
			if err != nil || count <= 0 {
				return opts, telemetryUsageError("invalid telemetry event count %q", args[1])
			}
			opts.count = count
			opts.once = false
			args = args[2:]
		case "once":
			opts.once = true
			opts.count = 0
			args = args[1:]
		default:
			opts.paths = append(opts.paths, args[0])
			args = args[1:]
		}
	}
	return opts, nil
}

func telemetryUsageError(format string, args ...interface{}) error {
	return fmt.Errorf("%w: %s", errTelemetryUsage, fmt.Sprintf(format, args...))
}

func isTelemetryUsageError(err error) bool {
	return errors.Is(err, errTelemetryUsage)
}

func printTelemetryEvent(event *grpcclient.TelemetryEvent) error {
	if event == nil {
		return nil
	}
	payload := json.RawMessage(event.JSONPayload)
	if len(payload) == 0 {
		payload = json.RawMessage("null")
	} else if !json.Valid(payload) {
		encoded, err := json.Marshal(event.JSONPayload)
		if err != nil {
			return err
		}
		payload = json.RawMessage(encoded)
	}

	timestamp := ""
	if !event.Timestamp.IsZero() {
		timestamp = event.Timestamp.UTC().Format(time.RFC3339Nano)
	}
	output := telemetryOutputEvent{
		Sequence:      event.Sequence,
		Timestamp:     timestamp,
		Path:          event.Path,
		Cardinality:   event.Cardinality,
		PayloadSchema: event.PayloadSchema,
		EventType:     event.EventType,
		Encoding:      event.Encoding,
		SchemaVersion: event.SchemaVersion,
		PayloadBytes:  telemetryPayloadBytes(event),
		Payload:       payload,
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(output)
}

func telemetryPayloadBytes(event *grpcclient.TelemetryEvent) int {
	if event.PayloadBytes > 0 {
		return event.PayloadBytes
	}
	return len(event.JSONPayload)
}
