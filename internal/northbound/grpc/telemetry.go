package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	telemetrySchemaVersion         = "arca.telemetry.v1"
	telemetryEventTypeSnapshot     = "snapshot"
	telemetryEventTypeError        = "error"
	telemetryEncodingJSON          = "json"
	defaultTelemetrySampleInterval = 30 * time.Second
	minTelemetrySampleInterval     = time.Second
	maxTelemetrySampleInterval     = time.Hour
)

var (
	defaultTelemetryPaths = []string{"/system", "/config/running"}
	telemetryPathOrder    = []string{
		"/system",
		"/config/running",
		"/interfaces",
		"/routes",
		"/routing/bgp/neighbors",
		"/routing/ospf/neighbors",
		"/routing/ospf3/neighbors",
		"/routing-instances",
		"/overlays/evpn",
		"/class-of-service",
		"/bfd",
		"/lcp",
		"/ha",
	}
	telemetryPathDescriptions = map[string]string{
		"/system":                  "daemon system metadata and uptime",
		"/config/running":          "running configuration text and version",
		"/interfaces":              "managed interface operational state, counters, QoS binding, and queue placement",
		"/routes":                  "routing table snapshot",
		"/routing/bgp/neighbors":   "BGP neighbor operational state",
		"/routing/ospf/neighbors":  "OSPFv2 neighbor operational state",
		"/routing/ospf3/neighbors": "OSPFv3 neighbor operational state",
		"/routing-instances":       "routing instance operational summary",
		"/overlays/evpn":           "EVPN/VXLAN VNI overlay intent",
		"/class-of-service":        "class-of-service intent and enforcement status",
		"/bfd":                     "BFD peer operational status",
		"/lcp":                     "VPP LCP reconciliation status",
		"/ha":                      "control-plane HA convergence status",
	}
	telemetryPathCardinality = map[string]string{
		"/system":                  "single",
		"/config/running":          "single",
		"/interfaces":              "per-interface",
		"/routes":                  "per-route",
		"/routing/bgp/neighbors":   "per-neighbor",
		"/routing/ospf/neighbors":  "per-neighbor",
		"/routing/ospf3/neighbors": "per-neighbor",
		"/routing-instances":       "per-instance",
		"/overlays/evpn":           "per-vni",
		"/class-of-service":        "per-intent-object",
		"/bfd":                     "per-peer",
		"/lcp":                     "single",
		"/ha":                      "single",
	}
	telemetryPathPayloadSchemas = map[string]string{
		"/system":                  "arca.telemetry.system.v1",
		"/config/running":          "arca.telemetry.config.running.v1",
		"/interfaces":              "arca.telemetry.interfaces.v1",
		"/routes":                  "arca.telemetry.routes.v1",
		"/routing/bgp/neighbors":   "arca.telemetry.routing.bgp.neighbors.v1",
		"/routing/ospf/neighbors":  "arca.telemetry.routing.ospf.neighbors.v1",
		"/routing/ospf3/neighbors": "arca.telemetry.routing.ospf3.neighbors.v1",
		"/routing-instances":       "arca.telemetry.routing.instances.v1",
		"/overlays/evpn":           "arca.telemetry.overlays.evpn.v1",
		"/class-of-service":        "arca.telemetry.class_of_service.v1",
		"/bfd":                     "arca.telemetry.bfd.v1",
		"/lcp":                     "arca.telemetry.lcp.v1",
		"/ha":                      "arca.telemetry.ha.v1",
	}
	telemetryPathAliases = map[string][]string{
		"/config/running":          {"/running", "/config"},
		"/routing/bgp/neighbors":   {"/bgp", "/bgp/neighbors"},
		"/routing/ospf/neighbors":  {"/ospf", "/ospf/neighbors"},
		"/routing/ospf3/neighbors": {"/ospf3", "/ospf3/neighbors"},
		"/overlays/evpn":           {"/evpn", "/overlay/evpn"},
		"/class-of-service":        {"/cos"},
	}
	telemetryPathSet = buildTelemetryPathSet(telemetryPathOrder)
)

// TelemetryPathInfo describes a supported structured telemetry path.
type TelemetryPathInfo struct {
	Path          string
	Description   string
	Cardinality   string
	PayloadSchema string
	Aliases       []string
	Default       bool
}

// TelemetryCatalog describes the structured telemetry stream inputs.
type TelemetryCatalog struct {
	EventSchemaVersion      string
	Encoding                string
	DefaultPaths            []string
	DefaultSampleIntervalMs uint32
	MinSampleIntervalMs     uint32
	MaxSampleIntervalMs     uint32
	Paths                   []TelemetryPathInfo
}

// TelemetryCatalogFilter selects a subset of the advertised telemetry path catalog.
type TelemetryCatalogFilter struct {
	Paths          []string
	Cardinalities  []string
	PayloadSchemas []string
	Encodings      []string
	DefaultOnly    bool
}

// TelemetryEvent is one structured telemetry update emitted over gRPC.
type TelemetryEvent struct {
	Sequence      uint64
	Timestamp     time.Time
	Path          string
	EventType     string
	Encoding      string
	JSONPayload   string
	SchemaVersion string
	PayloadBytes  int
}

// TelemetryEventSchemaVersion returns the current structured telemetry event schema version.
func TelemetryEventSchemaVersion() string {
	return telemetrySchemaVersion
}

// TelemetryEncoding returns the payload encoding used by structured telemetry events.
func TelemetryEncoding() string {
	return telemetryEncodingJSON
}

// NewTelemetryCatalog returns the supported telemetry catalog with schema metadata.
func NewTelemetryCatalog() TelemetryCatalog {
	return TelemetryCatalog{
		EventSchemaVersion:      telemetrySchemaVersion,
		Encoding:                telemetryEncodingJSON,
		DefaultPaths:            append([]string(nil), defaultTelemetryPaths...),
		DefaultSampleIntervalMs: telemetrySampleIntervalMillis(defaultTelemetrySampleInterval),
		MinSampleIntervalMs:     telemetrySampleIntervalMillis(minTelemetrySampleInterval),
		MaxSampleIntervalMs:     telemetrySampleIntervalMillis(maxTelemetrySampleInterval),
		Paths:                   TelemetryPathCatalog(),
	}
}

// NewFilteredTelemetryCatalog returns the supported telemetry catalog with path filters applied.
func NewFilteredTelemetryCatalog(filter TelemetryCatalogFilter) TelemetryCatalog {
	catalog := NewTelemetryCatalog()
	if !telemetryCatalogEncodingMatches(catalog.Encoding, filter.Encodings) {
		catalog.Paths = nil
		return catalog
	}
	catalog.Paths = filterTelemetryPathCatalog(catalog.Paths, filter)
	return catalog
}

// TelemetryPathCatalog returns the supported structured telemetry paths in canonical emission order.
func TelemetryPathCatalog() []TelemetryPathInfo {
	defaults := buildTelemetryPathSet(defaultTelemetryPaths)
	catalog := make([]TelemetryPathInfo, 0, len(telemetryPathOrder))
	for _, path := range telemetryPathOrder {
		_, isDefault := defaults[path]
		catalog = append(catalog, TelemetryPathInfo{
			Path:          path,
			Description:   telemetryPathDescriptions[path],
			Cardinality:   telemetryPathCardinality[path],
			PayloadSchema: telemetryPathPayloadSchemas[path],
			Aliases:       append([]string(nil), telemetryPathAliases[path]...),
			Default:       isDefault,
		})
	}
	return catalog
}

func filterTelemetryPathCatalog(catalog []TelemetryPathInfo, filter TelemetryCatalogFilter) []TelemetryPathInfo {
	paths := normalizedTelemetryCatalogPathFilterSet(filter.Paths)
	cardinalities := normalizedTelemetryCatalogFilterSet(filter.Cardinalities)
	payloadSchemas := normalizedTelemetryCatalogFilterSet(filter.PayloadSchemas)
	if !filter.DefaultOnly && len(paths) == 0 && len(cardinalities) == 0 && len(payloadSchemas) == 0 {
		return catalog
	}

	filtered := make([]TelemetryPathInfo, 0, len(catalog))
	for _, info := range catalog {
		if filter.DefaultOnly && !info.Default {
			continue
		}
		if len(paths) > 0 && !telemetryCatalogPathMatches(info, paths) {
			continue
		}
		if len(cardinalities) > 0 {
			if _, ok := cardinalities[normalizedTelemetryCatalogFilterValue(info.Cardinality)]; !ok {
				continue
			}
		}
		if len(payloadSchemas) > 0 {
			if _, ok := payloadSchemas[normalizedTelemetryCatalogFilterValue(info.PayloadSchema)]; !ok {
				continue
			}
		}
		filtered = append(filtered, info)
	}
	return filtered
}

func normalizedTelemetryCatalogPathFilterSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := normalizeTelemetryPath(value)
		if normalized != "" {
			set[normalized] = struct{}{}
		}
	}
	return set
}

func telemetryCatalogPathMatches(info TelemetryPathInfo, paths map[string]struct{}) bool {
	if _, ok := paths[normalizeTelemetryPath(info.Path)]; ok {
		return true
	}
	for _, alias := range info.Aliases {
		if _, ok := paths[normalizeTelemetryPath(alias)]; ok {
			return true
		}
	}
	return false
}

func normalizedTelemetryCatalogFilterSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := normalizedTelemetryCatalogFilterValue(value)
		if normalized != "" {
			set[normalized] = struct{}{}
		}
	}
	return set
}

func normalizedTelemetryCatalogFilterValue(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func telemetryCatalogEncodingMatches(encoding string, filters []string) bool {
	encodings := normalizedTelemetryCatalogFilterSet(filters)
	if len(encodings) == 0 {
		return true
	}
	_, ok := encodings[normalizedTelemetryCatalogFilterValue(encoding)]
	return ok
}

func telemetrySampleIntervalMillis(interval time.Duration) uint32 {
	return uint32(interval / time.Millisecond)
}

type telemetryErrorPayload struct {
	Error string `json:"error"`
}

type telemetrySystemPayload struct {
	Hostname   string `json:"hostname,omitempty"`
	Version    string `json:"version,omitempty"`
	UptimeSecs uint64 `json:"uptime_secs,omitempty"`
}

type telemetryConfigPayload struct {
	Version    uint64 `json:"version"`
	ConfigText string `json:"config_text"`
	LineCount  int    `json:"line_count"`
}

type telemetryEVPNPayload struct {
	VNIs []telemetryEVPNVNIPayload `json:"vnis"`
}

type telemetryEVPNVNIPayload struct {
	VNI                int      `json:"vni"`
	Type               string   `json:"type,omitempty"`
	BridgeDomain       string   `json:"bridge_domain,omitempty"`
	VLANID             int      `json:"vlan_id,omitempty"`
	RoutingInstance    string   `json:"routing_instance,omitempty"`
	RouteDistinguisher string   `json:"route_distinguisher,omitempty"`
	VRFTarget          string   `json:"vrf_target,omitempty"`
	VRFTargetImport    []string `json:"vrf_target_import,omitempty"`
	VRFTargetExport    []string `json:"vrf_target_export,omitempty"`
	SourceInterface    string   `json:"source_interface,omitempty"`
	SourceAddress      string   `json:"source_address,omitempty"`
	MulticastGroup     string   `json:"multicast_group,omitempty"`
}

func buildTelemetryPathSet(paths []string) map[string]struct{} {
	set := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		set[path] = struct{}{}
	}
	return set
}

// SubscribeTelemetry streams selected telemetry paths as JSON payload events.
func (s *Server) SubscribeTelemetry(ctx context.Context, rawPaths []string, interval time.Duration, once bool, send func(TelemetryEvent) error) error {
	if send == nil {
		return fmt.Errorf("telemetry send function is nil")
	}
	paths, err := normalizeTelemetryPaths(rawPaths)
	if err != nil {
		return err
	}

	var sequence uint64
	sendBatch := func(now time.Time) error {
		events := s.collectTelemetryEvents(ctx, paths, now.UTC(), &sequence)
		for _, event := range events {
			if err := send(event); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return err
			}
		}
		return nil
	}

	if err := sendBatch(time.Now()); err != nil {
		return err
	}
	if once {
		return nil
	}

	ticker := time.NewTicker(normalizeTelemetryInterval(interval))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			if err := sendBatch(now); err != nil {
				return err
			}
		}
	}
}

func (s *Server) collectTelemetryEvents(ctx context.Context, paths []string, now time.Time, sequence *uint64) []TelemetryEvent {
	events := make([]TelemetryEvent, 0, len(paths))
	for _, path := range paths {
		eventType := telemetryEventTypeSnapshot
		payload, err := s.telemetryPayload(ctx, path)
		if err != nil {
			eventType = telemetryEventTypeError
			payload = telemetryErrorPayload{Error: err.Error()}
		}
		rawPayload, err := json.Marshal(payload)
		if err != nil {
			eventType = telemetryEventTypeError
			rawPayload, _ = json.Marshal(telemetryErrorPayload{Error: err.Error()})
		}

		jsonPayload := string(rawPayload)
		*sequence = *sequence + 1
		events = append(events, TelemetryEvent{
			Sequence:      *sequence,
			Timestamp:     now,
			Path:          path,
			EventType:     eventType,
			Encoding:      telemetryEncodingJSON,
			JSONPayload:   jsonPayload,
			SchemaVersion: telemetrySchemaVersion,
			PayloadBytes:  len(jsonPayload),
		})
	}
	return events
}

func (s *Server) telemetryPayload(ctx context.Context, path string) (any, error) {
	switch path {
	case "/system":
		info, err := s.GetSystemInfo(ctx)
		if err != nil {
			return nil, err
		}
		return telemetrySystemPayload{
			Hostname:   info.Hostname,
			Version:    info.Version,
			UptimeSecs: info.UptimeSecs,
		}, nil
	case "/config/running":
		text, version, err := s.runningText()
		if err != nil {
			return nil, err
		}
		return telemetryConfigPayload{
			Version:    version,
			ConfigText: text,
			LineCount:  countConfigLines(text),
		}, nil
	case "/interfaces":
		interfaces, err := s.GetInterfaces(ctx, "")
		if err != nil {
			return nil, err
		}
		return struct {
			Interfaces []InterfaceInfo `json:"interfaces"`
		}{Interfaces: interfaces}, nil
	case "/routes":
		routes, err := s.GetRoutes(ctx, "", "")
		if err != nil {
			return nil, err
		}
		return struct {
			Routes []RouteInfo `json:"routes"`
		}{Routes: routes}, nil
	case "/routing/bgp/neighbors":
		neighbors, err := s.GetBGPNeighbors(ctx)
		if err != nil {
			return nil, err
		}
		return struct {
			Neighbors []BGPNeighborInfo `json:"neighbors"`
		}{Neighbors: neighbors}, nil
	case "/routing/ospf/neighbors":
		neighbors, err := s.GetOSPFNeighbors(ctx, addressFamilyIPv4)
		if err != nil {
			return nil, err
		}
		return struct {
			Neighbors []OSPFNeighborInfo `json:"neighbors"`
		}{Neighbors: neighbors}, nil
	case "/routing/ospf3/neighbors":
		neighbors, err := s.GetOSPFNeighbors(ctx, addressFamilyIPv6)
		if err != nil {
			return nil, err
		}
		return struct {
			Neighbors []OSPFNeighborInfo `json:"neighbors"`
		}{Neighbors: neighbors}, nil
	case "/routing-instances":
		instances, err := s.GetRoutingInstances(ctx)
		if err != nil {
			return nil, err
		}
		return struct {
			Instances []RoutingInstanceInfo `json:"instances"`
		}{Instances: instances}, nil
	case "/overlays/evpn":
		return s.telemetryEVPNPayload(), nil
	case "/class-of-service":
		info, err := s.GetClassOfService(ctx)
		if err != nil {
			return nil, err
		}
		return struct {
			ClassOfService *ClassOfServiceInfo `json:"class_of_service"`
		}{ClassOfService: info}, nil
	case "/bfd":
		info, err := s.GetBFDStatus(ctx)
		if err != nil {
			return nil, err
		}
		return struct {
			Status *BFDStatusInfo `json:"status"`
		}{Status: info}, nil
	case "/lcp":
		info, err := s.GetLCPReconciliation(ctx)
		if err != nil {
			return nil, err
		}
		return struct {
			Reconciliation *LCPReconciliationInfo `json:"reconciliation"`
		}{Reconciliation: info}, nil
	case "/ha":
		info, err := s.GetHAStatus(ctx)
		if err != nil {
			return nil, err
		}
		return struct {
			Status *HAStatusInfo `json:"status"`
		}{Status: info}, nil
	default:
		return nil, fmt.Errorf("unsupported telemetry path %q", path)
	}
}

func normalizeTelemetryPaths(rawPaths []string) ([]string, error) {
	if len(rawPaths) == 0 {
		return append([]string(nil), defaultTelemetryPaths...), nil
	}

	seen := make(map[string]struct{}, len(rawPaths))
	for _, rawPath := range rawPaths {
		path := normalizeTelemetryPath(rawPath)
		if path == "" {
			continue
		}
		if _, ok := telemetryPathSet[path]; !ok {
			return nil, fmt.Errorf("unsupported telemetry path %q", rawPath)
		}
		seen[path] = struct{}{}
	}
	if len(seen) == 0 {
		return append([]string(nil), defaultTelemetryPaths...), nil
	}

	paths := make([]string, 0, len(seen))
	for _, path := range telemetryPathOrder {
		if _, ok := seen[path]; ok {
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func normalizeTelemetryPath(rawPath string) string {
	path := strings.TrimSpace(strings.ToLower(rawPath))
	if path == "" {
		return ""
	}
	path = "/" + strings.Trim(path, "/")
	switch path {
	case "/running", "/config":
		return "/config/running"
	case "/bgp", "/bgp/neighbors":
		return "/routing/bgp/neighbors"
	case "/ospf", "/ospf/neighbors":
		return "/routing/ospf/neighbors"
	case "/ospf3", "/ospf3/neighbors":
		return "/routing/ospf3/neighbors"
	case "/evpn", "/overlay/evpn", "/overlays/evpn":
		return "/overlays/evpn"
	case "/cos":
		return "/class-of-service"
	}
	return path
}

func normalizeTelemetryInterval(interval time.Duration) time.Duration {
	if interval <= 0 {
		return defaultTelemetrySampleInterval
	}
	if interval < minTelemetrySampleInterval {
		return minTelemetrySampleInterval
	}
	if interval > maxTelemetrySampleInterval {
		return maxTelemetrySampleInterval
	}
	return interval
}

func countConfigLines(text string) int {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}

func (s *Server) telemetryEVPNPayload() telemetryEVPNPayload {
	payload := telemetryEVPNPayload{VNIs: []telemetryEVPNVNIPayload{}}
	if s.engine == nil {
		return payload
	}
	cfg := s.engine.Running()
	if cfg == nil || cfg.Protocols == nil || cfg.Protocols.EVPN == nil || len(cfg.Protocols.EVPN.VNIs) == 0 {
		return payload
	}

	ids := make([]int, 0, len(cfg.Protocols.EVPN.VNIs))
	for id := range cfg.Protocols.EVPN.VNIs {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		vni := cfg.Protocols.EVPN.VNIs[id]
		if vni == nil {
			continue
		}
		vniID := vni.VNI
		if vniID == 0 {
			vniID = id
		}
		payload.VNIs = append(payload.VNIs, telemetryEVPNVNIPayload{
			VNI:                vniID,
			Type:               vni.Type,
			BridgeDomain:       vni.BridgeDomain,
			VLANID:             vni.VLANID,
			RoutingInstance:    vni.RoutingInstance,
			RouteDistinguisher: vni.RouteDistinguisher,
			VRFTarget:          vni.VRFTarget,
			VRFTargetImport:    append([]string(nil), vni.VRFTargetImport...),
			VRFTargetExport:    append([]string(nil), vni.VRFTargetExport...),
			SourceInterface:    vni.SourceInterface,
			SourceAddress:      vni.SourceAddress,
			MulticastGroup:     vni.MulticastGroup,
		})
	}
	return payload
}
