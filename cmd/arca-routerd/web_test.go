package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akam1o/arca-router/internal/correlation"
	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	nbgrpc "github.com/akam1o/arca-router/internal/northbound/grpc"
	sbfrr "github.com/akam1o/arca-router/internal/southbound/frr"
	sbvpp "github.com/akam1o/arca-router/internal/southbound/vpp"
	pkgconfig "github.com/akam1o/arca-router/pkg/config"
	"github.com/akam1o/arca-router/pkg/datastore"
	pkgvpp "github.com/akam1o/arca-router/pkg/vpp"
)

func TestEffectiveWebListenUsesFlagOverride(t *testing.T) {
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{
		Services: &model.SystemServicesConfig{
			WebUI: &model.WebUIConfig{
				Enabled:       true,
				ListenAddress: "127.0.0.1",
				Port:          8443,
			},
		},
	}

	got := effectiveWebListen(":9000", model.NewSnapshot(cfg, 1, "test", "test"))
	if got != ":9000" {
		t.Fatalf("effectiveWebListen() = %q, want %q", got, ":9000")
	}
}

func TestEffectiveWebListenUsesConfig(t *testing.T) {
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{
		Services: &model.SystemServicesConfig{
			WebUI: &model.WebUIConfig{
				Enabled:       true,
				ListenAddress: "127.0.0.1",
				Port:          8443,
			},
		},
	}

	got := effectiveWebListen("", model.NewSnapshot(cfg, 1, "test", "test"))
	if got != "127.0.0.1:8443" {
		t.Fatalf("effectiveWebListen() = %q, want %q", got, "127.0.0.1:8443")
	}
}

func TestEffectiveWebListenUsesConfigDefaults(t *testing.T) {
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{
		Services: &model.SystemServicesConfig{
			WebUI: &model.WebUIConfig{Enabled: true},
		},
	}

	got := effectiveWebListen("", model.NewSnapshot(cfg, 1, "test", "test"))
	if got != "127.0.0.1:8080" {
		t.Fatalf("effectiveWebListen() = %q, want %q", got, "127.0.0.1:8080")
	}
}

func TestWebPlainHTTPListenAllowed(t *testing.T) {
	tests := []struct {
		listen string
		want   bool
	}{
		{listen: "127.0.0.1:8080", want: true},
		{listen: "localhost:8080", want: true},
		{listen: "[::1]:8080", want: true},
		{listen: ":8080"},
		{listen: "0.0.0.0:8080"},
		{listen: "[::]:8080"},
		{listen: "192.0.2.10:8080"},
		{listen: "not a listen address"},
	}

	for _, tt := range tests {
		t.Run(tt.listen, func(t *testing.T) {
			if got := webPlainHTTPListenAllowed(tt.listen); got != tt.want {
				t.Fatalf("webPlainHTTPListenAllowed(%q) = %v, want %v", tt.listen, got, tt.want)
			}
		})
	}
}

func TestStartWebServerRejectsRemotePlainHTTP(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh, err := startWebServer(ctx, "0.0.0.0:0", metricsSource{}, nil)
	if err == nil {
		cancel()
		<-errCh
		t.Fatal("startWebServer() error = nil, want loopback restriction error")
	}
	if !strings.Contains(err.Error(), "must listen on loopback") {
		t.Fatalf("startWebServer() error = %v, want loopback restriction", err)
	}
}

func TestWebStatusEndpoint(t *testing.T) {
	eng := engine.NewEngine(nil, slog.Default())
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{HostName: "edge01"}
	cfg.Chassis = &model.ChassisConfig{
		Cluster: &model.ClusterConfig{
			Enabled: true,
			Nodes: map[string]*model.ClusterNode{
				"node0": {Address: "192.0.2.10"},
			},
			Sync: &model.ClusterSyncConfig{
				Etcd: &model.EtcdSyncConfig{Endpoints: []string{"https://etcd1:2379"}},
			},
		},
	}
	cfg.Protocols = &model.ProtocolsConfig{
		VRRP: &model.VRRPConfig{Groups: map[string]*model.VRRPGroup{
			"10": {Interface: "ge-0/0/0", VirtualAddress: "192.0.2.1", Priority: 110, Preempt: true},
		}},
		EVPN: &model.EVPNConfig{VNIs: map[int]*model.EVPNVNI{
			10010: {
				VNI:             10010,
				Type:            "l2",
				BridgeDomain:    "BD-10",
				SourceInterface: "ge-0/0/0",
				MulticastGroup:  "239.0.0.10",
			},
			20010: {
				VNI:             20010,
				Type:            "l3",
				RoutingInstance: "BLUE",
			},
		}},
	}
	cfg.ClassOfService = &model.ClassOfServiceConfig{
		ForwardingClasses: map[string]*model.ForwardingClass{
			"best-effort":          {Queue: 0},
			"expedited-forwarding": {Queue: 5},
		},
		TrafficControlProfiles: map[string]*model.TrafficControlProfile{
			"WAN": {ShapingRate: 1000000000, SchedulerMap: "WAN-SCHED"},
		},
		Interfaces: map[string]*model.CoSInterface{
			"ge-0/0/0": {OutputTrafficControlProfile: "WAN"},
		},
	}
	eng.InitializeRunning(cfg, 42)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	metricsSource{
		startedAt: time.Now().Add(-2 * time.Minute),
		engine:    eng,
		datastore: &datastore.Config{
			Backend:       datastore.BackendEtcd,
			EtcdEndpoints: []string{"https://etcd1:2379"},
		},
		configSync: fakeConfigSyncRuntimeSource{status: configSyncStatus{
			Enabled:         true,
			Healthy:         true,
			EtcdRevision:    123,
			RunningRevision: 120,
			RunningCommitID: "commit-120",
			LastCheck:       time.Unix(1700000100, 0),
			LastApply:       time.Unix(1700000200, 0),
		}},
		frr: fakeFRRVRRPSource{
			vrrpStatus: sbfrr.VRRPOperationalStatus{
				LastRun:          time.Unix(1700000300, 0),
				ConfiguredGroups: 1,
				ObservedGroups:   1,
				ActiveGroups:     1,
				Groups: []sbfrr.VRRPGroupOperationalStatus{
					{Interface: "ge0-0-0", ID: 10, VirtualAddress: "192.0.2.1", State: "Master", Observed: true, Active: true},
				},
			},
			bfdStatus: sbfrr.BFDOperationalStatus{
				LastRun:           time.Unix(1700000400, 0),
				ConfiguredPeers:   1,
				ObservedPeers:     1,
				UpPeers:           1,
				SessionDownEvents: 2,
				RxFailPackets:     1,
				Peers: []sbfrr.BFDPeerOperationalStatus{
					{Peer: "192.0.2.2", Status: "up", Observed: true, Up: true, SessionDownEvents: 2, RxFailPackets: 1},
				},
			},
		},
		vpp: fakeVPPReconciliationSource{status: sbvpp.LCPReconciliationStatus{
			LastRun:         time.Unix(1700000000, 0),
			PairCount:       2,
			Inconsistencies: []string{"Interface 7 exists in VPP but not in cache"},
		}, qos: sbvpp.QoSCapabilityStatus{
			LastCheck: time.Unix(1700000500, 0),
			Capabilities: pkgvpp.QoSCapabilities{
				MetadataBinding:     true,
				QueueScheduler:      false,
				Policer:             false,
				OperationalCounters: false,
				Diagnostics:         []string{"scheduler api unavailable"},
			},
		}},
	}.handleWebStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var status webStatus
	if err := json.NewDecoder(rec.Result().Body).Decode(&status); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if status.ConfigVersion != 42 {
		t.Fatalf("ConfigVersion = %d, want 42", status.ConfigVersion)
	}
	if status.RunningHostname != "edge01" {
		t.Fatalf("RunningHostname = %q, want edge01", status.RunningHostname)
	}
	if status.UptimeSeconds <= 0 {
		t.Fatalf("UptimeSeconds = %f, want positive", status.UptimeSeconds)
	}
	if status.Datastore.Backend != "etcd" {
		t.Fatalf("Datastore.Backend = %q, want etcd", status.Datastore.Backend)
	}
	if !status.ConfigSync.Enabled || !status.ConfigSync.Healthy || status.ConfigSync.RunningRevision != 120 ||
		status.ConfigSync.RunningCommitID != "commit-120" {
		t.Fatalf("ConfigSync status = %#v, want healthy revision 120", status.ConfigSync)
	}
	if !status.Cluster.Enabled || status.Cluster.NodeCount != 1 || !status.Cluster.EtcdSyncConfigured || !status.Cluster.SyncAligned {
		t.Fatalf("Cluster status = %#v, want enabled aligned etcd sync", status.Cluster)
	}
	if !status.Overlay.EVPN.Configured || status.Overlay.EVPN.VNIs != 2 ||
		status.Overlay.EVPN.L2VNIs != 1 || status.Overlay.EVPN.L3VNIs != 1 ||
		status.Overlay.EVPN.MulticastVNIs != 1 {
		t.Fatalf("Overlay EVPN status = %#v, want configured L2/L3 multicast VNI counts", status.Overlay.EVPN)
	}
	if !status.HA.Configured || status.HA.Converged || status.HA.VRRPGroups != 1 || status.HA.IssueCount != 2 {
		t.Fatalf("HA status = %#v, want configured with cluster and VPP LCP issues", status.HA)
	}
	if status.FRR.VRRP.ConfiguredGroups != 1 || status.FRR.VRRP.ActiveGroups != 1 ||
		status.FRR.VRRP.LastCheck == "" {
		t.Fatalf("FRR VRRP status = %#v, want active group status", status.FRR.VRRP)
	}
	if len(status.FRR.VRRP.Groups) != 1 || status.FRR.VRRP.Groups[0].State != "Master" ||
		!status.FRR.VRRP.Groups[0].Observed || !status.FRR.VRRP.Groups[0].Active {
		t.Fatalf("FRR VRRP groups = %#v, want active group detail", status.FRR.VRRP.Groups)
	}
	if status.FRR.BFD.ConfiguredPeers != 1 || status.FRR.BFD.UpPeers != 1 ||
		status.FRR.BFD.SessionDownEvents != 2 || status.FRR.BFD.LastCheck == "" {
		t.Fatalf("FRR BFD status = %#v, want active peer status", status.FRR.BFD)
	}
	if len(status.FRR.BFD.Peers) != 1 || status.FRR.BFD.Peers[0].Status != "up" ||
		!status.FRR.BFD.Peers[0].Observed || !status.FRR.BFD.Peers[0].Up {
		t.Fatalf("FRR BFD peers = %#v, want active peer detail", status.FRR.BFD.Peers)
	}
	if status.VPP.LCP.PairCount != 2 || status.VPP.LCP.InconsistencyCount != 1 || status.VPP.LCP.LastReconcile == "" {
		t.Fatalf("VPP LCP status = %#v, want pair count and inconsistency status", status.VPP.LCP)
	}
	if !status.ClassOfService.Configured || status.ClassOfService.EnforcementStatus != "intent-only" ||
		status.ClassOfService.ForwardingClasses != 2 ||
		status.ClassOfService.TrafficControlProfiles != 1 ||
		status.ClassOfService.InterfaceBindings != 1 ||
		!status.ClassOfService.IntentOnly {
		t.Fatalf("ClassOfService status = %#v, want configured intent-only status", status.ClassOfService)
	}
	if !status.ClassOfService.Capabilities.MetadataBindingSupported ||
		status.ClassOfService.Capabilities.QueueSchedulerSupported ||
		status.ClassOfService.Capabilities.PolicerSupported ||
		status.ClassOfService.Capabilities.CountersSupported ||
		len(status.ClassOfService.Capabilities.Diagnostics) != 1 ||
		status.ClassOfService.Capabilities.LastCheck == "" {
		t.Fatalf("ClassOfService capabilities = %#v, want metadata binding with unsupported scheduler/policer", status.ClassOfService.Capabilities)
	}
}

func TestNMSStatusEndpoint(t *testing.T) {
	eng := engine.NewEngine(nil, slog.Default())
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{HostName: "edge-nms"}
	eng.InitializeRunning(cfg, 77)

	req := httptest.NewRequest(http.MethodGet, "/api/nms/v1/status", nil)
	rec := httptest.NewRecorder()
	metricsSource{
		startedAt: time.Now().Add(-2 * time.Minute),
		engine:    eng,
	}.handleNMSStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp nmsStatusResponse
	if err := json.NewDecoder(rec.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if resp.SchemaVersion != nmsOperationalStatusSchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", resp.SchemaVersion, nmsOperationalStatusSchemaVersion)
	}
	if resp.Resource != "/api/nms/v1/status" {
		t.Fatalf("Resource = %q, want /api/nms/v1/status", resp.Resource)
	}
	if _, err := time.Parse(time.RFC3339, resp.GeneratedAt); err != nil {
		t.Fatalf("GeneratedAt = %q, want RFC3339 timestamp: %v", resp.GeneratedAt, err)
	}
	if resp.Data.ConfigVersion != 77 || resp.Data.RunningHostname != "edge-nms" {
		t.Fatalf("Data = %#v, want config version 77 for edge-nms", resp.Data)
	}
}

func TestNMSTelemetryCatalogEndpoint(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/nms/v1/telemetry/paths", nil)
	rec := httptest.NewRecorder()
	metricsSource{}.handleNMSTelemetryCatalog(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp nmsTelemetryCatalogResponse
	if err := json.NewDecoder(rec.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if resp.SchemaVersion != nmsTelemetryCatalogSchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", resp.SchemaVersion, nmsTelemetryCatalogSchemaVersion)
	}
	if resp.Resource != "/api/nms/v1/telemetry/paths" {
		t.Fatalf("Resource = %q, want /api/nms/v1/telemetry/paths", resp.Resource)
	}
	if resp.EventSchemaVersion != nbgrpc.TelemetryEventSchemaVersion() || resp.Encoding != nbgrpc.TelemetryEncoding() {
		t.Fatalf("event schema/encoding = %q/%q, want %q/%q",
			resp.EventSchemaVersion, resp.Encoding, nbgrpc.TelemetryEventSchemaVersion(), nbgrpc.TelemetryEncoding())
	}
	if len(resp.DefaultPaths) != 2 || resp.DefaultPaths[0] != "/system" || resp.DefaultPaths[1] != "/config/running" {
		t.Fatalf("DefaultPaths = %#v, want system and config/running", resp.DefaultPaths)
	}
	catalog := nbgrpc.NewTelemetryCatalog()
	if resp.DefaultSampleIntervalMs != catalog.DefaultSampleIntervalMs ||
		resp.MinSampleIntervalMs != catalog.MinSampleIntervalMs ||
		resp.MaxSampleIntervalMs != catalog.MaxSampleIntervalMs {
		t.Fatalf("sample intervals = %d/%d/%d, want %d/%d/%d",
			resp.DefaultSampleIntervalMs, resp.MinSampleIntervalMs, resp.MaxSampleIntervalMs,
			catalog.DefaultSampleIntervalMs, catalog.MinSampleIntervalMs, catalog.MaxSampleIntervalMs)
	}
	if len(resp.Paths) == 0 {
		t.Fatal("Paths is empty, want telemetry path catalog")
	}
	if resp.PathCount != len(resp.Paths) {
		t.Fatalf("PathCount = %d, want %d", resp.PathCount, len(resp.Paths))
	}
	if resp.Paths[0].Path != "/system" || !resp.Paths[0].Default || resp.Paths[0].Description == "" ||
		resp.Paths[0].Cardinality != "single" || resp.Paths[0].PayloadSchema != "arca.telemetry.system.v1" {
		t.Fatalf("Paths[0] = %#v, want default system path with description, single cardinality, and payload schema", resp.Paths[0])
	}
	var routesPath, evpnPath nmsTelemetryPath
	for _, path := range resp.Paths {
		switch path.Path {
		case "/routes":
			routesPath = path
		case "/overlays/evpn":
			evpnPath = path
		}
	}
	if routesPath.Cardinality != "per-route" {
		t.Fatalf("/routes cardinality = %q, want per-route", routesPath.Cardinality)
	}
	if routesPath.PayloadSchema != "arca.telemetry.routes.v1" {
		t.Fatalf("/routes payload schema = %q, want arca.telemetry.routes.v1", routesPath.PayloadSchema)
	}
	if len(evpnPath.Aliases) != 2 || evpnPath.Aliases[0] != "/evpn" || evpnPath.Aliases[1] != "/overlay/evpn" {
		t.Fatalf("/overlays/evpn aliases = %#v, want EVPN aliases", evpnPath.Aliases)
	}
}

func TestNMSTelemetryCatalogEndpointFilters(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/nms/v1/telemetry/paths?cardinality=per-route&payload_schema=arca.telemetry.routes.v1&encoding=JSON", nil)
	rec := httptest.NewRecorder()
	metricsSource{}.handleNMSTelemetryCatalog(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp nmsTelemetryCatalogResponse
	if err := json.NewDecoder(rec.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(resp.Paths) != 1 || resp.Paths[0].Path != "/routes" {
		t.Fatalf("filtered paths = %#v, want only /routes", resp.Paths)
	}
	if resp.PathCount != 1 {
		t.Fatalf("PathCount = %d, want 1", resp.PathCount)
	}
	if resp.Paths[0].Cardinality != "per-route" || resp.Paths[0].PayloadSchema != "arca.telemetry.routes.v1" {
		t.Fatalf("filtered path = %#v, want route cardinality and schema", resp.Paths[0])
	}
}

func TestNMSTelemetryCatalogEndpointIgnoresEmptyFilters(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/nms/v1/telemetry/paths?path=&cardinality=&payload_schema=&payload-schema=&encoding=", nil)
	rec := httptest.NewRecorder()
	metricsSource{}.handleNMSTelemetryCatalog(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp nmsTelemetryCatalogResponse
	if err := json.NewDecoder(rec.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	catalog := nbgrpc.NewTelemetryCatalog()
	if resp.PathCount != len(catalog.Paths) {
		t.Fatalf("PathCount = %d, want full catalog count %d", resp.PathCount, len(catalog.Paths))
	}
}

func TestNMSTelemetryCatalogEndpointSplitsCommaSeparatedFilters(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/nms/v1/telemetry/paths?path=system,evpn&payload_schema=arca.telemetry.system.v1,arca.telemetry.overlays.evpn.v1&encoding=json,", nil)
	rec := httptest.NewRecorder()
	metricsSource{}.handleNMSTelemetryCatalog(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp nmsTelemetryCatalogResponse
	if err := json.NewDecoder(rec.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(resp.Paths) != 2 || resp.Paths[0].Path != "/system" || resp.Paths[1].Path != "/overlays/evpn" {
		t.Fatalf("filtered paths = %#v, want system and EVPN from comma-separated filters", resp.Paths)
	}
}

func TestNMSTelemetryCatalogEndpointFiltersUnsupportedEncoding(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/nms/v1/telemetry/paths?encoding=protobuf", nil)
	rec := httptest.NewRecorder()
	metricsSource{}.handleNMSTelemetryCatalog(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp nmsTelemetryCatalogResponse
	if err := json.NewDecoder(rec.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if resp.Encoding != nbgrpc.TelemetryEncoding() {
		t.Fatalf("Encoding = %q, want %q", resp.Encoding, nbgrpc.TelemetryEncoding())
	}
	if len(resp.Paths) != 0 {
		t.Fatalf("filtered paths = %#v, want none for unsupported encoding", resp.Paths)
	}
	if resp.PathCount != 0 {
		t.Fatalf("PathCount = %d, want 0", resp.PathCount)
	}
}

func TestNMSTelemetryCatalogEndpointAcceptsPayloadSchemaAlias(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/nms/v1/telemetry/paths?payload-schema=ARCA.TELEMETRY.SYSTEM.V1", nil)
	rec := httptest.NewRecorder()
	metricsSource{}.handleNMSTelemetryCatalog(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp nmsTelemetryCatalogResponse
	if err := json.NewDecoder(rec.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(resp.Paths) != 1 || resp.Paths[0].Path != "/system" {
		t.Fatalf("filtered paths = %#v, want only /system", resp.Paths)
	}
}

func TestNMSTelemetryCatalogEndpointAcceptsPathAliasFilter(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/nms/v1/telemetry/paths?path=evpn", nil)
	rec := httptest.NewRecorder()
	metricsSource{}.handleNMSTelemetryCatalog(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp nmsTelemetryCatalogResponse
	if err := json.NewDecoder(rec.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(resp.Paths) != 1 || resp.Paths[0].Path != "/overlays/evpn" {
		t.Fatalf("filtered paths = %#v, want only /overlays/evpn", resp.Paths)
	}
}

func TestNMSTelemetryCatalogEndpointAcceptsDefaultFilter(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/nms/v1/telemetry/paths?default=true", nil)
	rec := httptest.NewRecorder()
	metricsSource{}.handleNMSTelemetryCatalog(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp nmsTelemetryCatalogResponse
	if err := json.NewDecoder(rec.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(resp.Paths) != len(resp.DefaultPaths) {
		t.Fatalf("filtered paths = %#v, want default path count %d", resp.Paths, len(resp.DefaultPaths))
	}
	for _, path := range resp.Paths {
		if !path.Default {
			t.Fatalf("filtered path = %#v, want only default paths", path)
		}
	}
}

func TestNMSTelemetrySchemasEndpoint(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/nms/v1/telemetry/schemas", nil)
	rec := httptest.NewRecorder()
	metricsSource{}.handleNMSTelemetrySchemas(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp nmsTelemetrySchemasResponse
	if err := json.NewDecoder(rec.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if resp.SchemaVersion != nmsTelemetrySchemasSchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", resp.SchemaVersion, nmsTelemetrySchemasSchemaVersion)
	}
	if resp.Resource != "/api/nms/v1/telemetry/schemas" {
		t.Fatalf("Resource = %q, want /api/nms/v1/telemetry/schemas", resp.Resource)
	}
	if resp.EventSchemaVersion != nbgrpc.TelemetryEventSchemaVersion() || resp.Encoding != nbgrpc.TelemetryEncoding() {
		t.Fatalf("event schema/encoding = %q/%q, want %q/%q",
			resp.EventSchemaVersion, resp.Encoding, nbgrpc.TelemetryEventSchemaVersion(), nbgrpc.TelemetryEncoding())
	}
	catalog := nbgrpc.NewTelemetryCatalog()
	if strings.Join(resp.DefaultPaths, ",") != strings.Join(catalog.DefaultPaths, ",") {
		t.Fatalf("DefaultPaths = %#v, want %#v", resp.DefaultPaths, catalog.DefaultPaths)
	}
	if resp.DefaultSampleIntervalMs != catalog.DefaultSampleIntervalMs ||
		resp.MinSampleIntervalMs != catalog.MinSampleIntervalMs ||
		resp.MaxSampleIntervalMs != catalog.MaxSampleIntervalMs {
		t.Fatalf("sample intervals = %d/%d/%d, want %d/%d/%d",
			resp.DefaultSampleIntervalMs, resp.MinSampleIntervalMs, resp.MaxSampleIntervalMs,
			catalog.DefaultSampleIntervalMs, catalog.MinSampleIntervalMs, catalog.MaxSampleIntervalMs)
	}
	if len(resp.Schemas) == 0 {
		t.Fatal("Schemas is empty, want telemetry payload schemas")
	}
	if resp.SchemaCount != len(resp.Schemas) {
		t.Fatalf("SchemaCount = %d, want %d", resp.SchemaCount, len(resp.Schemas))
	}
	byPath := map[string]nmsTelemetryPayloadSchema{}
	for _, schema := range resp.Schemas {
		byPath[schema.Path] = schema
	}
	routes := byPath["/routes"]
	if routes.PayloadSchema != "arca.telemetry.routes.v1" || routes.Cardinality != "per-route" ||
		len(routes.Fields) != 1 || routes.Fields[0].Name != "routes" || routes.Fields[0].Type != "[]RouteInfo" {
		t.Fatalf("/routes schema = %#v, want route payload field metadata", routes)
	}
	evpn := byPath["/overlays/evpn"]
	if evpn.PayloadSchema != "arca.telemetry.overlays.evpn.v1" ||
		len(evpn.Fields) != 1 || evpn.Fields[0].Name != "vnis" || evpn.Fields[0].Type != "[]EVPNVNI" {
		t.Fatalf("/overlays/evpn schema = %#v, want EVPN VNI field metadata", evpn)
	}
	if cos := byPath["/class-of-service"]; len(cos.Fields) != 1 || cos.Fields[0].Name != "class_of_service" {
		t.Fatalf("/class-of-service schema = %#v, want class_of_service field metadata", cos)
	}
}

func TestNMSTelemetrySchemasEndpointFilters(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/nms/v1/telemetry/schemas?path=evpn&payload_schema=arca.telemetry.overlays.evpn.v1&encoding=json", nil)
	rec := httptest.NewRecorder()
	metricsSource{}.handleNMSTelemetrySchemas(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp nmsTelemetrySchemasResponse
	if err := json.NewDecoder(rec.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(resp.Schemas) != 1 || resp.Schemas[0].Path != "/overlays/evpn" {
		t.Fatalf("filtered schemas = %#v, want only /overlays/evpn", resp.Schemas)
	}
	if resp.SchemaCount != 1 {
		t.Fatalf("SchemaCount = %d, want 1", resp.SchemaCount)
	}
	if len(resp.DefaultPaths) != 2 || resp.DefaultPaths[0] != "/system" || resp.DefaultPaths[1] != "/config/running" {
		t.Fatalf("DefaultPaths = %#v, want system/config defaults", resp.DefaultPaths)
	}
	if len(resp.Schemas[0].Fields) != 1 || resp.Schemas[0].Fields[0].Name != "vnis" {
		t.Fatalf("filtered schema fields = %#v, want EVPN VNI field", resp.Schemas[0].Fields)
	}
}

func TestNMSTelemetrySchemasEndpointFiltersUnsupportedEncoding(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/nms/v1/telemetry/schemas?encoding=protobuf", nil)
	rec := httptest.NewRecorder()
	metricsSource{}.handleNMSTelemetrySchemas(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var resp nmsTelemetrySchemasResponse
	if err := json.NewDecoder(rec.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if resp.Encoding != nbgrpc.TelemetryEncoding() {
		t.Fatalf("Encoding = %q, want %q", resp.Encoding, nbgrpc.TelemetryEncoding())
	}
	if len(resp.Schemas) != 0 {
		t.Fatalf("filtered schemas = %#v, want none for unsupported encoding", resp.Schemas)
	}
	if resp.SchemaCount != 0 {
		t.Fatalf("SchemaCount = %d, want 0", resp.SchemaCount)
	}
	if len(resp.DefaultPaths) != 2 {
		t.Fatalf("DefaultPaths = %#v, want unfiltered defaults with unsupported encoding", resp.DefaultPaths)
	}
}

func TestNMSTelemetrySnapshotEndpoint(t *testing.T) {
	telemetry := &webTelemetryTestAPI{events: []nbgrpc.TelemetryEvent{
		{
			Sequence:      1,
			Timestamp:     time.Unix(1700000600, 123).UTC(),
			Path:          "/system",
			Cardinality:   "single",
			PayloadSchema: "arca.telemetry.system.v1",
			EventType:     "snapshot",
			Encoding:      nbgrpc.TelemetryEncoding(),
			SchemaVersion: nbgrpc.TelemetryEventSchemaVersion(),
			JSONPayload:   `{"hostname":"edge01"}`,
		},
		{
			Sequence:      2,
			Timestamp:     time.Unix(1700000601, 0).UTC(),
			Path:          "/interfaces",
			Cardinality:   "per-interface",
			PayloadSchema: "arca.telemetry.interfaces.v1",
			EventType:     "snapshot",
			Encoding:      nbgrpc.TelemetryEncoding(),
			SchemaVersion: nbgrpc.TelemetryEventSchemaVersion(),
			JSONPayload:   `{"interfaces":[]}`,
		},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/nms/v1/telemetry/snapshot?path=/system&path=/interfaces", nil)
	rec := httptest.NewRecorder()
	metricsSource{telemetryAPI: telemetry}.handleNMSTelemetrySnapshot(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !telemetry.once || len(telemetry.paths) != 2 || telemetry.paths[0] != "/system" || telemetry.paths[1] != "/interfaces" {
		t.Fatalf("telemetry subscription = once %v paths %#v, want one-shot system/interfaces", telemetry.once, telemetry.paths)
	}
	var resp nmsTelemetrySnapshotResponse
	if err := json.NewDecoder(rec.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if resp.SchemaVersion != nmsTelemetrySnapshotSchemaVersion || resp.Resource != "/api/nms/v1/telemetry/snapshot" {
		t.Fatalf("snapshot envelope = %#v", resp)
	}
	if resp.EventSchemaVersion != nbgrpc.TelemetryEventSchemaVersion() || resp.Encoding != nbgrpc.TelemetryEncoding() {
		t.Fatalf("snapshot schema/encoding = %q/%q", resp.EventSchemaVersion, resp.Encoding)
	}
	catalog := nbgrpc.NewTelemetryCatalog()
	if strings.Join(resp.DefaultPaths, ",") != strings.Join(catalog.DefaultPaths, ",") {
		t.Fatalf("DefaultPaths = %#v, want %#v", resp.DefaultPaths, catalog.DefaultPaths)
	}
	if resp.DefaultSampleIntervalMs != catalog.DefaultSampleIntervalMs ||
		resp.MinSampleIntervalMs != catalog.MinSampleIntervalMs ||
		resp.MaxSampleIntervalMs != catalog.MaxSampleIntervalMs {
		t.Fatalf("sample intervals = %d/%d/%d, want %d/%d/%d",
			resp.DefaultSampleIntervalMs, resp.MinSampleIntervalMs, resp.MaxSampleIntervalMs,
			catalog.DefaultSampleIntervalMs, catalog.MinSampleIntervalMs, catalog.MaxSampleIntervalMs)
	}
	if len(resp.Paths) != 2 || resp.Paths[0] != "/system" || resp.Paths[1] != "/interfaces" {
		t.Fatalf("Paths = %#v, want emitted paths", resp.Paths)
	}
	if resp.EventCount != 2 {
		t.Fatalf("EventCount = %d, want 2", resp.EventCount)
	}
	wantPayloadBytes := len(`{"hostname":"edge01"}`) + len(`{"interfaces":[]}`)
	if resp.PayloadBytes != wantPayloadBytes || resp.MaxPayloadBytes != defaultNMSTelemetrySnapshotMaxPayloadBytes {
		t.Fatalf("payload budget = %d/%d, want %d/%d", resp.PayloadBytes, resp.MaxPayloadBytes, wantPayloadBytes, defaultNMSTelemetrySnapshotMaxPayloadBytes)
	}
	if resp.MaxEvents != defaultNMSTelemetrySnapshotMaxEvents {
		t.Fatalf("MaxEvents = %d, want %d", resp.MaxEvents, defaultNMSTelemetrySnapshotMaxEvents)
	}
	if resp.TimeoutMs != defaultNMSTelemetrySnapshotTimeout.Milliseconds() {
		t.Fatalf("TimeoutMs = %d, want %d", resp.TimeoutMs, defaultNMSTelemetrySnapshotTimeout.Milliseconds())
	}
	if len(resp.Events) != 2 || resp.Events[0].Path != "/system" || string(resp.Events[0].Payload) != `{"hostname":"edge01"}` {
		t.Fatalf("Events = %#v, want system payload event", resp.Events)
	}
	if resp.Events[0].Cardinality != "single" || resp.Events[1].Cardinality != "per-interface" {
		t.Fatalf("event cardinalities = %q/%q, want system/interfaces hints",
			resp.Events[0].Cardinality, resp.Events[1].Cardinality)
	}
	if resp.Events[0].PayloadSchema != "arca.telemetry.system.v1" ||
		resp.Events[1].PayloadSchema != "arca.telemetry.interfaces.v1" {
		t.Fatalf("event payload schemas = %q/%q, want system/interfaces schema IDs",
			resp.Events[0].PayloadSchema, resp.Events[1].PayloadSchema)
	}
	if resp.Events[0].PayloadBytes != len(`{"hostname":"edge01"}`) ||
		resp.Events[1].PayloadBytes != len(`{"interfaces":[]}`) {
		t.Fatalf("event payload bytes = %d/%d, want per-event payload lengths",
			resp.Events[0].PayloadBytes, resp.Events[1].PayloadBytes)
	}
}

func TestNMSTelemetrySnapshotEndpointFiltersCatalogMetadata(t *testing.T) {
	telemetry := &webTelemetryTestAPI{events: []nbgrpc.TelemetryEvent{
		{
			Sequence:      1,
			Path:          "/routes",
			Cardinality:   "per-route",
			PayloadSchema: "arca.telemetry.routes.v1",
			EventType:     "snapshot",
			Encoding:      nbgrpc.TelemetryEncoding(),
			SchemaVersion: nbgrpc.TelemetryEventSchemaVersion(),
			JSONPayload:   `{"routes":[]}`,
		},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/nms/v1/telemetry/snapshot?cardinality=per-route&payload_schema=arca.telemetry.routes.v1&encoding=json", nil)
	rec := httptest.NewRecorder()
	metricsSource{telemetryAPI: telemetry}.handleNMSTelemetrySnapshot(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !telemetry.once || len(telemetry.paths) != 1 || telemetry.paths[0] != "/routes" {
		t.Fatalf("telemetry subscription = once %v paths %#v, want one-shot /routes", telemetry.once, telemetry.paths)
	}
	var resp nmsTelemetrySnapshotResponse
	if err := json.NewDecoder(rec.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(resp.Paths) != 1 || resp.Paths[0] != "/routes" ||
		resp.Events[0].Cardinality != "per-route" || resp.Events[0].PayloadSchema != "arca.telemetry.routes.v1" {
		t.Fatalf("snapshot response = %#v, want filtered route event metadata", resp)
	}
}

func TestNMSTelemetrySnapshotEndpointFiltersCatalogPathAlias(t *testing.T) {
	telemetry := &webTelemetryTestAPI{events: []nbgrpc.TelemetryEvent{
		{
			Sequence:      1,
			Path:          "/overlays/evpn",
			Cardinality:   "per-vni",
			PayloadSchema: "arca.telemetry.overlays.evpn.v1",
			EventType:     "snapshot",
			Encoding:      nbgrpc.TelemetryEncoding(),
			SchemaVersion: nbgrpc.TelemetryEventSchemaVersion(),
			JSONPayload:   `{"vnis":[]}`,
		},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/nms/v1/telemetry/snapshot?path=evpn&cardinality=per-vni", nil)
	rec := httptest.NewRecorder()
	metricsSource{telemetryAPI: telemetry}.handleNMSTelemetrySnapshot(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if !telemetry.once || len(telemetry.paths) != 1 || telemetry.paths[0] != "/overlays/evpn" {
		t.Fatalf("telemetry subscription = once %v paths %#v, want canonical EVPN path", telemetry.once, telemetry.paths)
	}
}

func TestNMSTelemetrySnapshotOptionsFiltersDefaultCatalogPaths(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/nms/v1/telemetry/snapshot?default=true&cardinality=single", nil)
	opts, err := nmsTelemetrySnapshotOptionsFromRequest(req)
	if err != nil {
		t.Fatalf("nmsTelemetrySnapshotOptionsFromRequest() error = %v", err)
	}
	if len(opts.paths) != 2 || opts.paths[0] != "/system" || opts.paths[1] != "/config/running" {
		t.Fatalf("paths = %#v, want default single-cardinality snapshot paths", opts.paths)
	}
}

func TestNMSTelemetrySnapshotOptionsNormalizesCatalogFilters(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/nms/v1/telemetry/snapshot?path=system,evpn&payload_schema=arca.telemetry.system.v1,arca.telemetry.overlays.evpn.v1&encoding=json,", nil)
	opts, err := nmsTelemetrySnapshotOptionsFromRequest(req)
	if err != nil {
		t.Fatalf("nmsTelemetrySnapshotOptionsFromRequest() error = %v", err)
	}
	if len(opts.paths) != 2 || opts.paths[0] != "/system" || opts.paths[1] != "/overlays/evpn" {
		t.Fatalf("paths = %#v, want normalized system and EVPN snapshot paths", opts.paths)
	}
}

func TestNMSTelemetrySnapshotEndpointRejectsEmptyCatalogFilter(t *testing.T) {
	telemetry := &webTelemetryTestAPI{}
	req := httptest.NewRequest(http.MethodGet, "/api/nms/v1/telemetry/snapshot?encoding=protobuf", nil)
	rec := httptest.NewRecorder()
	metricsSource{telemetryAPI: telemetry}.handleNMSTelemetrySnapshot(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if telemetry.once || len(telemetry.paths) != 0 {
		t.Fatalf("telemetry subscription = once %v paths %#v, want no subscription", telemetry.once, telemetry.paths)
	}
}

func TestNMSTelemetrySnapshotEndpointRedactsInternalErrors(t *testing.T) {
	source := metricsSource{
		telemetryAPI: &webTelemetryTestAPI{err: errors.New("vpp socket /run/vpp/api.sock failed")},
		webLog:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/nms/v1/telemetry/snapshot", nil)
	rec := httptest.NewRecorder()
	source.handleNMSTelemetrySnapshot(rec, req)

	requireWebJSONInternalError(t, rec, "vpp socket", "/run/vpp/api.sock")
}

func TestNMSTelemetrySnapshotEndpointRejectsOversizedPayload(t *testing.T) {
	telemetry := &webTelemetryTestAPI{events: []nbgrpc.TelemetryEvent{
		{
			Sequence:      1,
			Path:          "/routes",
			EventType:     "snapshot",
			Encoding:      nbgrpc.TelemetryEncoding(),
			SchemaVersion: nbgrpc.TelemetryEventSchemaVersion(),
			JSONPayload:   `{"routes":[1,2,3]}`,
		},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/nms/v1/telemetry/snapshot?path=/routes&max_payload_bytes=4", nil)
	rec := httptest.NewRecorder()
	metricsSource{telemetryAPI: telemetry}.handleNMSTelemetrySnapshot(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
}

func TestNMSTelemetrySnapshotEndpointRejectsTooManyEvents(t *testing.T) {
	telemetry := &webTelemetryTestAPI{events: []nbgrpc.TelemetryEvent{
		{
			Sequence:      1,
			Path:          "/system",
			EventType:     "snapshot",
			Encoding:      nbgrpc.TelemetryEncoding(),
			SchemaVersion: nbgrpc.TelemetryEventSchemaVersion(),
			JSONPayload:   `{"hostname":"edge01"}`,
		},
		{
			Sequence:      2,
			Path:          "/interfaces",
			EventType:     "snapshot",
			Encoding:      nbgrpc.TelemetryEncoding(),
			SchemaVersion: nbgrpc.TelemetryEventSchemaVersion(),
			JSONPayload:   `{"interfaces":[]}`,
		},
		{
			Sequence:      3,
			Path:          "/routes",
			EventType:     "snapshot",
			Encoding:      nbgrpc.TelemetryEncoding(),
			SchemaVersion: nbgrpc.TelemetryEventSchemaVersion(),
			JSONPayload:   `{"routes":[]}`,
		},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/nms/v1/telemetry/snapshot?path=/system&path=/interfaces&path=/routes&max_events=1", nil)
	rec := httptest.NewRecorder()
	metricsSource{telemetryAPI: telemetry}.handleNMSTelemetrySnapshot(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
	if telemetry.sent != 2 {
		t.Fatalf("sent events = %d, want stop on second event", telemetry.sent)
	}
}

func TestNMSTelemetrySnapshotEndpointRejectsInvalidTimeout(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/nms/v1/telemetry/snapshot?timeout=1h", nil)
	rec := httptest.NewRecorder()
	metricsSource{telemetryAPI: &webTelemetryTestAPI{}}.handleNMSTelemetrySnapshot(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestNMSTelemetrySnapshotEndpointRejectsInvalidMaxEvents(t *testing.T) {
	tests := []string{
		"/api/nms/v1/telemetry/snapshot?max_events=0",
		"/api/nms/v1/telemetry/snapshot?max_events=2048",
	}
	for _, target := range tests {
		t.Run(target, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, target, nil)
			rec := httptest.NewRecorder()
			metricsSource{telemetryAPI: &webTelemetryTestAPI{}}.handleNMSTelemetrySnapshot(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}

func TestWebConfigEndpoint(t *testing.T) {
	eng := engine.NewEngine(nil, slog.Default())
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{
		HostName: "edge01",
		Services: &model.SystemServicesConfig{
			SNMP: &model.SNMPConfig{
				Enabled:   true,
				Community: "private-community",
			},
		},
	}
	eng.InitializeRunning(cfg, 42)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rec := httptest.NewRecorder()
	metricsSource{
		startedAt: time.Now().Add(-2 * time.Minute),
		engine:    eng,
	}.handleWebConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var cfgResp webConfig
	if err := json.NewDecoder(rec.Result().Body).Decode(&cfgResp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if cfgResp.Version != 42 {
		t.Fatalf("Version = %d, want 42", cfgResp.Version)
	}
	if !strings.Contains(cfgResp.ConfigText, "set system host-name edge01") {
		t.Fatalf("ConfigText missing hostname:\n%s", cfgResp.ConfigText)
	}
	for _, secret := range []string{"private-community"} {
		if strings.Contains(cfgResp.ConfigText, secret) {
			t.Fatalf("ConfigText leaked %q:\n%s", secret, cfgResp.ConfigText)
		}
	}
	if strings.Count(cfgResp.ConfigText, "<redacted>") != 1 {
		t.Fatalf("ConfigText =\n%s\nwant one redacted marker", cfgResp.ConfigText)
	}
}

func TestWebEndpointRequiresAuthWhenPasswordUsersConfigured(t *testing.T) {
	source := newWebAuthTestSource(t, "monitor", "secret", "read-only")

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	source.handleWebStatus(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got != webAuthRealm {
		t.Fatalf("WWW-Authenticate = %q, want %q", got, webAuthRealm)
	}
}

func TestWebEndpointAcceptsReadOnlyBasicAuth(t *testing.T) {
	source := newWebAuthTestSource(t, "monitor", "secret", "read-only")

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.SetBasicAuth("monitor", "secret")
	rec := httptest.NewRecorder()
	source.handleWebConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var cfgResp webConfig
	if err := json.NewDecoder(rec.Result().Body).Decode(&cfgResp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !strings.Contains(cfgResp.ConfigText, "set system host-name edge01") {
		t.Fatalf("ConfigText missing hostname:\n%s", cfgResp.ConfigText)
	}
	if strings.Contains(cfgResp.ConfigText, "$argon2id$") || !strings.Contains(cfgResp.ConfigText, "<redacted>") {
		t.Fatalf("read-only ConfigText =\n%s\nwant redacted security password", cfgResp.ConfigText)
	}
}

func TestWebConfigEndpointRedactsWriterRole(t *testing.T) {
	source := newWebAuthTestSource(t, "operator", "secret", "operator")

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.SetBasicAuth("operator", "secret")
	rec := httptest.NewRecorder()
	source.handleWebConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var cfgResp webConfig
	if err := json.NewDecoder(rec.Result().Body).Decode(&cfgResp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if strings.Contains(cfgResp.ConfigText, "$argon2id$") || !strings.Contains(cfgResp.ConfigText, webRedactedSecretMarker) {
		t.Fatalf("writer ConfigText =\n%s\nwant redacted security password", cfgResp.ConfigText)
	}
}

func TestWebIndexEndpointRedactsWriterRole(t *testing.T) {
	source := newWebAuthTestSource(t, "operator", "secret", "operator")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.SetBasicAuth("operator", "secret")
	rec := httptest.NewRecorder()
	source.handleWebIndex(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "$argon2id$") || !strings.Contains(body, "redacted") {
		t.Fatalf("writer index body leaked or omitted redaction:\n%s", body)
	}
}

const validWebAPITestToken = "0123456789abcdef0123456789ABCDEF"
const rotatedWebAPITestToken = "fedcba9876543210FEDCBA9876543210"

func hashedWebAPITestToken(token string) string {
	tokenSHA256 := sha256.Sum256([]byte(token))
	return webAPITokenSHA256Prefix + hex.EncodeToString(tokenSHA256[:])
}

func hashedWebAPITestTokenNotAfter(token string, notAfter time.Time) string {
	return hashedWebAPITestToken(token) + webAPITokenNotAfterPrefix + notAfter.UTC().Format(time.RFC3339)
}

func newCachedWebAPITokenTestSource(t *testing.T, tokenFile string) metricsSource {
	t.Helper()
	tokens, err := loadWebAPITokens(tokenFile)
	if err != nil {
		t.Fatalf("loadWebAPITokens() error = %v", err)
	}
	source := newWebAuthTestSource(t, "monitor", "secret", "read-only")
	source.webAPITokens = tokens
	source.webAPITokenFile = tokenFile
	source.webAPITokenCache = newWebAPITokenCache(tokenFile, tokens)
	return source
}

func requestWebStatusWithBearer(source metricsSource, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	source.handleWebStatus(rec, req)
	return rec
}

func requireWebAPITokenUnavailable(t *testing.T, rec *httptest.ResponseRecorder, leakedValues ...string) {
	t.Helper()
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	body := rec.Body.String()
	if !strings.Contains(body, webAPITokenUnavailableMessage) {
		t.Fatalf("response body = %q, want generic token unavailable message", body)
	}
	for _, leaked := range leakedValues {
		if leaked != "" && strings.Contains(body, leaked) {
			t.Fatalf("response body leaked %q: %q", leaked, body)
		}
	}
}

func requireWebJSONInternalError(t *testing.T, rec *httptest.ResponseRecorder, leakedValues ...string) {
	t.Helper()
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Result().Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if body["error"] != webInternalServerErrorMessage {
		t.Fatalf("error = %q, want generic internal server error", body["error"])
	}
	rawBody := rec.Body.String()
	for _, leaked := range leakedValues {
		if leaked != "" && strings.Contains(rawBody, leaked) {
			t.Fatalf("response body leaked %q: %q", leaked, rawBody)
		}
	}
}

func TestLoadWebAPITokensParsesTokenFile(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "tokens")
	if err := os.WriteFile(tokenFile, []byte("# comment\nrobot:operator:"+validWebAPITestToken+"\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tokens, err := loadWebAPITokens(tokenFile)
	if err != nil {
		t.Fatalf("loadWebAPITokens() error = %v", err)
	}
	token := tokens["robot"]
	if token.Name != "robot" || token.Role != "operator" || token.Token != validWebAPITestToken {
		t.Fatalf("token = %#v, want parsed robot operator token", token)
	}
	if len(token.TokenSHA256) != sha256.Size {
		t.Fatalf("len(token.TokenSHA256) = %d, want %d", len(token.TokenSHA256), sha256.Size)
	}
}

func TestLoadWebAPITokensParsesHashedTokenFile(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "tokens")
	if err := os.WriteFile(tokenFile, []byte("robot:operator:"+hashedWebAPITestToken(validWebAPITestToken)+"\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tokens, err := loadWebAPITokens(tokenFile)
	if err != nil {
		t.Fatalf("loadWebAPITokens() error = %v", err)
	}
	token := tokens["robot"]
	if token.Name != "robot" || token.Role != "operator" || token.Token != "" || len(token.TokenSHA256) != sha256.Size {
		t.Fatalf("token = %#v, want parsed robot operator SHA-256 token", token)
	}

	source := newWebAuthTestSource(t, "monitor", "secret", "read-only")
	source.webAPITokens = tokens
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+validWebAPITestToken)
	rec := httptest.NewRecorder()
	source.handleWebStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestWebEndpointReloadsAPITokenFile(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "tokens")
	if err := os.WriteFile(tokenFile, []byte("robot:read-only:"+hashedWebAPITestToken(validWebAPITestToken)+"\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	source := newWebAuthTestSource(t, "monitor", "secret", "read-only")
	source.webAPITokenFile = tokenFile

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+validWebAPITestToken)
	rec := httptest.NewRecorder()
	source.handleWebStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status before rotation = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	if err := os.WriteFile(tokenFile, []byte("robot:read-only:"+hashedWebAPITestToken(rotatedWebAPITestToken)+"\n"), 0600); err != nil {
		t.Fatalf("WriteFile(rotated) error = %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+validWebAPITestToken)
	rec = httptest.NewRecorder()
	source.handleWebStatus(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status for old token = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+rotatedWebAPITestToken)
	rec = httptest.NewRecorder()
	source.handleWebStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status after rotation = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestWebEndpointFailsClosedWhenReloadedAPITokenFileIsInvalid(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "tokens")
	if err := os.WriteFile(tokenFile, []byte("robot:read-only:"+hashedWebAPITestToken(validWebAPITestToken)+"\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	source := newWebAuthTestSource(t, "monitor", "secret", "read-only")
	source.webAPITokenFile = tokenFile

	if err := os.WriteFile(tokenFile, []byte("robot:read-only:sha256:not-hex\n"), 0600); err != nil {
		t.Fatalf("WriteFile(invalid) error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+validWebAPITestToken)
	rec := httptest.NewRecorder()
	source.handleWebStatus(rec, req)
	requireWebAPITokenUnavailable(t, rec, tokenFile, "not-hex", "robot")
}

func TestWebEndpointReloadsCachedAPITokenFileWhenMetadataChanges(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "tokens")
	if err := os.WriteFile(tokenFile, []byte("robot:read-only:"+hashedWebAPITestToken(validWebAPITestToken)+"\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	source := newCachedWebAPITokenTestSource(t, tokenFile)
	rec := requestWebStatusWithBearer(source, validWebAPITestToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status before rotation = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	if err := os.WriteFile(tokenFile, []byte("# rotated\nrobot:read-only:"+hashedWebAPITestToken(rotatedWebAPITestToken)+"\n"), 0600); err != nil {
		t.Fatalf("WriteFile(rotated) error = %v", err)
	}

	rec = requestWebStatusWithBearer(source, validWebAPITestToken)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status for old token = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	rec = requestWebStatusWithBearer(source, rotatedWebAPITestToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status after rotation = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestWebEndpointFailsClosedWhenCachedAPITokenReloadIsInvalid(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "tokens")
	if err := os.WriteFile(tokenFile, []byte("robot:read-only:"+hashedWebAPITestToken(validWebAPITestToken)+"\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	source := newCachedWebAPITokenTestSource(t, tokenFile)
	if err := os.WriteFile(tokenFile, []byte("robot:read-only:sha256:not-hex\n# invalid\n"), 0600); err != nil {
		t.Fatalf("WriteFile(invalid) error = %v", err)
	}

	rec := requestWebStatusWithBearer(source, validWebAPITestToken)
	requireWebAPITokenUnavailable(t, rec, tokenFile, "not-hex", "robot")

	if err := os.WriteFile(tokenFile, []byte("# recovered\nrobot:read-only:"+hashedWebAPITestToken(rotatedWebAPITestToken)+"\n"), 0600); err != nil {
		t.Fatalf("WriteFile(recovered) error = %v", err)
	}

	rec = requestWebStatusWithBearer(source, rotatedWebAPITestToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status after recovery = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestWebEndpointFailsClosedWhenCachedAPITokenFilePermissionsChange(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "tokens")
	if err := os.WriteFile(tokenFile, []byte("robot:read-only:"+hashedWebAPITestToken(validWebAPITestToken)+"\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	source := newCachedWebAPITokenTestSource(t, tokenFile)
	if err := os.Chmod(tokenFile, 0644); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}

	rec := requestWebStatusWithBearer(source, validWebAPITestToken)
	requireWebAPITokenUnavailable(t, rec, tokenFile, "permissions")
}

func TestLoadWebAPITokensParsesHashedTokenNotAfter(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "tokens")
	notAfter := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	if err := os.WriteFile(tokenFile, []byte("robot:operator:"+hashedWebAPITestTokenNotAfter(validWebAPITestToken, notAfter)+"\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	tokens, err := loadWebAPITokens(tokenFile)
	if err != nil {
		t.Fatalf("loadWebAPITokens() error = %v", err)
	}
	token := tokens["robot"]
	if !token.NotAfter.Equal(notAfter) {
		t.Fatalf("token.NotAfter = %s, want %s", token.NotAfter, notAfter)
	}

	source := newWebAuthTestSource(t, "monitor", "secret", "read-only")
	source.webAPITokens = tokens
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+validWebAPITestToken)
	rec := httptest.NewRecorder()
	source.handleWebStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestWebEndpointRejectsExpiredHashedToken(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "tokens")
	notAfter := time.Now().UTC().Add(-time.Hour)
	if err := os.WriteFile(tokenFile, []byte("robot:operator:"+hashedWebAPITestTokenNotAfter(validWebAPITestToken, notAfter)+"\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	tokens, err := loadWebAPITokens(tokenFile)
	if err != nil {
		t.Fatalf("loadWebAPITokens() error = %v", err)
	}

	source := newWebAuthTestSource(t, "monitor", "secret", "read-only")
	source.webAPITokens = tokens
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+validWebAPITestToken)
	rec := httptest.NewRecorder()
	source.handleWebStatus(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestLoadWebAPITokensRejectsDuplicateTokenValue(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "tokens")
	data := []byte("readonly:read-only:" + validWebAPITestToken + "\nadmin:admin:" + validWebAPITestToken + "\n")
	if err := os.WriteFile(tokenFile, data, 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := loadWebAPITokens(tokenFile)
	if err == nil {
		t.Fatal("loadWebAPITokens() error = nil, want duplicate token value error")
	}
	if !strings.Contains(err.Error(), "duplicate web API token value") {
		t.Fatalf("loadWebAPITokens() error = %v, want duplicate token value error", err)
	}
	if strings.Contains(err.Error(), validWebAPITestToken) {
		t.Fatalf("loadWebAPITokens() error leaked token value: %v", err)
	}
}

func TestLoadWebAPITokensRejectsDuplicateHashedTokenValue(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "tokens")
	data := []byte("readonly:read-only:" + validWebAPITestToken + "\nadmin:admin:" + hashedWebAPITestToken(validWebAPITestToken) + "\n")
	if err := os.WriteFile(tokenFile, data, 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := loadWebAPITokens(tokenFile)
	if err == nil {
		t.Fatal("loadWebAPITokens() error = nil, want duplicate token value error")
	}
	if !strings.Contains(err.Error(), "duplicate web API token value") {
		t.Fatalf("loadWebAPITokens() error = %v, want duplicate token value error", err)
	}
	if strings.Contains(err.Error(), validWebAPITestToken) {
		t.Fatalf("loadWebAPITokens() error leaked token value: %v", err)
	}
}

func TestLoadWebAPITokensRejectsWeakTokenValue(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "tokens")
	if err := os.WriteFile(tokenFile, []byte("robot:operator:secret-token\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := loadWebAPITokens(tokenFile)
	if err == nil {
		t.Fatal("loadWebAPITokens() error = nil, want weak token value error")
	}
	if !strings.Contains(err.Error(), "web API token") {
		t.Fatalf("loadWebAPITokens() error = %v, want token validation error", err)
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("loadWebAPITokens() error leaked token value: %v", err)
	}
}

func TestLoadWebAPITokensRejectsMalformedHashedTokenValue(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "tokens")
	if err := os.WriteFile(tokenFile, []byte("robot:operator:sha256:not-hex\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := loadWebAPITokens(tokenFile)
	if err == nil {
		t.Fatal("loadWebAPITokens() error = nil, want malformed hash error")
	}
	if !strings.Contains(err.Error(), "sha256:64 hex characters") {
		t.Fatalf("loadWebAPITokens() error = %v, want malformed hash error", err)
	}
}

func TestLoadWebAPITokensRejectsMalformedHashedTokenNotAfter(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "tokens")
	if err := os.WriteFile(tokenFile, []byte("robot:operator:"+hashedWebAPITestToken(validWebAPITestToken)+webAPITokenNotAfterPrefix+"not-a-time\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := loadWebAPITokens(tokenFile)
	if err == nil {
		t.Fatal("loadWebAPITokens() error = nil, want malformed not-after error")
	}
	if !strings.Contains(err.Error(), "not-after must be RFC3339") {
		t.Fatalf("loadWebAPITokens() error = %v, want malformed not-after error", err)
	}
}

func TestLoadWebAPITokensRejectsUnknownHashedTokenSuffix(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "tokens")
	if err := os.WriteFile(tokenFile, []byte("robot:operator:"+hashedWebAPITestToken(validWebAPITestToken)+":expires=2026-01-01T00:00:00Z\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := loadWebAPITokens(tokenFile)
	if err == nil {
		t.Fatal("loadWebAPITokens() error = nil, want unknown suffix error")
	}
	if !strings.Contains(err.Error(), "hash suffix") {
		t.Fatalf("loadWebAPITokens() error = %v, want unknown suffix error", err)
	}
}

func TestLoadWebAPITokensRejectsInsecurePermissions(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "tokens")
	if err := os.WriteFile(tokenFile, []byte("robot:operator:"+validWebAPITestToken+"\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Chmod(tokenFile, 0644); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}

	_, err := loadWebAPITokens(tokenFile)
	if err == nil {
		t.Fatal("loadWebAPITokens() error = nil, want permission error")
	}
	if !strings.Contains(err.Error(), "validate token file permissions") {
		t.Fatalf("loadWebAPITokens() error = %v, want permission validation error", err)
	}
}

func TestWebEndpointAcceptsBearerToken(t *testing.T) {
	source := newWebAuthTestSource(t, "monitor", "secret", "read-only")
	source.webAPITokens = map[string]webAPIToken{
		"robot": {Name: "robot", Role: "read-only", Token: "secret-token"},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()
	source.handleWebStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestWebEndpointAcceptsAPIKeyHeader(t *testing.T) {
	source := newWebAuthTestSource(t, "monitor", "secret", "read-only")
	source.webAPITokens = map[string]webAPIToken{
		"robot": {Name: "robot", Role: "read-only", Token: "secret-token"},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.Header.Set("X-API-Key", "secret-token")
	rec := httptest.NewRecorder()
	source.handleWebConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestConstantTimeWebTokenEqual(t *testing.T) {
	if !constantTimeWebTokenEqual("secret-token", "secret-token") {
		t.Fatal("constantTimeWebTokenEqual() = false, want true")
	}
	for _, candidate := range []string{"secret-token-extra", "secret", ""} {
		if constantTimeWebTokenEqual("secret-token", candidate) {
			t.Fatalf("constantTimeWebTokenEqual() matched %q, want false", candidate)
		}
	}
}

func TestWebEndpointRequiresAuthWhenOnlyTokensConfigured(t *testing.T) {
	eng := engine.NewEngine(nil, slog.Default())
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{HostName: "edge01"}
	eng.InitializeRunning(cfg, 42)
	source := metricsSource{
		startedAt: time.Now(),
		engine:    eng,
		webAPITokens: map[string]webAPIToken{
			"robot": {Name: "robot", Role: "read-only", Token: "secret-token"},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rec := httptest.NewRecorder()
	source.handleWebStatus(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestWebEndpointRejectsInvalidRole(t *testing.T) {
	source := newWebAuthTestSource(t, "monitor", "secret", "invalid")

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.SetBasicAuth("monitor", "secret")
	rec := httptest.NewRecorder()
	source.handleWebStatus(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestWebConfigValidateEndpointUsesConfigAPI(t *testing.T) {
	source, _ := newWebConfigAPITestSource(t, "operator")

	req := newWebJSONTestRequest(http.MethodPost, "/api/config/validate", `{"config_text":"set system host-name edge02"}`)
	req.SetBasicAuth("operator", "secret")
	rec := httptest.NewRecorder()
	source.handleWebConfigValidate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp webConfigValidateResponse
	if err := json.NewDecoder(rec.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !resp.Valid || !resp.HasChanges {
		t.Fatalf("validate response = %#v, want valid with changes", resp)
	}
	for _, want := range []string{"- set system host-name edge01", "+ set system host-name edge02"} {
		if !strings.Contains(resp.DiffText, want) {
			t.Fatalf("DiffText missing %q:\n%s", want, resp.DiffText)
		}
	}
}

func TestWebConfigCommitEndpointAppliesConfig(t *testing.T) {
	source, eng := newWebConfigAPITestSource(t, "operator")

	req := newWebJSONTestRequest(http.MethodPost, "/api/config/commit", `{"config_text":"set system host-name edge02","message":"web update"}`)
	req.SetBasicAuth("operator", "secret")
	rec := httptest.NewRecorder()
	source.handleWebConfigCommit(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp webConfigCommitResponse
	if err := json.NewDecoder(rec.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if resp.Version != 43 {
		t.Fatalf("Version = %d, want 43", resp.Version)
	}
	if got := eng.Running().System.HostName; got != "edge02" {
		t.Fatalf("running hostname = %q, want edge02", got)
	}
}

func TestWebConfigCommitEndpointPropagatesCorrelationID(t *testing.T) {
	api := &webConfigEditErrorTestAPI{}
	source := newWebAuthTestSource(t, "operator", "secret", "operator")
	source.configAPI = api

	req := newWebJSONTestRequest(http.MethodPost, "/api/config/commit", `{"config_text":"set system host-name edge02"}`)
	req.Header.Set(correlation.HeaderName, "web-request-1")
	req.SetBasicAuth("operator", "secret")
	rec := httptest.NewRecorder()
	source.handleWebConfigCommit(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Result().Header.Get(correlation.HeaderName); got != "web-request-1" {
		t.Fatalf("%s response header = %q, want web-request-1", correlation.HeaderName, got)
	}
	if api.commitCorrelationID != "web-request-1" {
		t.Fatalf("commit correlation ID = %q, want web-request-1", api.commitCorrelationID)
	}
}

func TestWebConfigCommitEndpointReplacesFullConfig(t *testing.T) {
	source, eng := newWebConfigAPITestSource(t, "operator")
	hash, err := pkgconfig.NormalizePasswordForStorage("secret")
	if err != nil {
		t.Fatalf("NormalizePasswordForStorage() error = %v", err)
	}
	eng.InitializeRunning(&model.RouterConfig{
		System: &model.SystemConfig{
			HostName: "edge01",
			Services: &model.SystemServicesConfig{
				SNMP: &model.SNMPConfig{
					Enabled:   true,
					Community: "private-community",
				},
			},
		},
		Security: &model.SecurityConfig{
			Users: map[string]*model.UserConfig{
				"operator": {
					Password: hash,
					Role:     "operator",
				},
			},
		},
	}, 42)

	req := newWebJSONTestRequest(http.MethodPost, "/api/config/commit", `{"config_text":"set system host-name edge02","message":"replace full config"}`)
	req.SetBasicAuth("operator", "secret")
	rec := httptest.NewRecorder()
	source.handleWebConfigCommit(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	running := eng.Running()
	if got := running.System.HostName; got != "edge02" {
		t.Fatalf("running hostname = %q, want edge02", got)
	}
	if running.System.Services != nil && running.System.Services.SNMP != nil {
		t.Fatalf("SNMP config remained after full replacement: %#v", running.System.Services.SNMP)
	}
}

func TestWebConfigWriteEndpointRejectsTrailingJSON(t *testing.T) {
	source, eng := newWebConfigAPITestSource(t, "operator")

	req := newWebJSONTestRequest(http.MethodPost, "/api/config/commit", `{"config_text":"set system host-name edge02"}{"config_text":"set system host-name edge03"}`)
	req.SetBasicAuth("operator", "secret")
	rec := httptest.NewRecorder()
	source.handleWebConfigCommit(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if got := eng.Running().System.HostName; got != "edge01" {
		t.Fatalf("running hostname = %q, want unchanged edge01", got)
	}
	if !strings.Contains(rec.Body.String(), "unexpected trailing JSON value") {
		t.Fatalf("response body = %q, want trailing JSON error", rec.Body.String())
	}
}

func TestWebConfigWriteEndpointRejectsOversizedBody(t *testing.T) {
	source, _ := newWebConfigAPITestSource(t, "operator")
	body := `{"config_text":"` + strings.Repeat("x", webConfigEditBodyLimit) + `"}`

	req := newWebJSONTestRequest(http.MethodPost, "/api/config/validate", body)
	req.SetBasicAuth("operator", "secret")
	rec := httptest.NewRecorder()
	source.handleWebConfigValidate(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
}

func TestWebConfigWriteEndpointRejectsRedactedConfig(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		handle func(metricsSource, http.ResponseWriter, *http.Request)
	}{
		{
			name: "validate",
			path: "/api/config/validate",
			handle: func(source metricsSource, w http.ResponseWriter, r *http.Request) {
				source.handleWebConfigValidate(w, r)
			},
		},
		{
			name: "commit",
			path: "/api/config/commit",
			handle: func(source metricsSource, w http.ResponseWriter, r *http.Request) {
				source.handleWebConfigCommit(w, r)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, eng := newWebConfigAPITestSource(t, "operator")
			body := `{"config_text":"set security users operator password ` + webRedactedSecretMarker + `"}`
			req := newWebJSONTestRequest(http.MethodPost, tt.path, body)
			req.SetBasicAuth("operator", "secret")
			rec := httptest.NewRecorder()
			tt.handle(source, rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "redacted config text cannot be validated or committed") {
				t.Fatalf("response body = %q, want redacted config rejection", rec.Body.String())
			}
			if got := eng.Running().System.HostName; got != "edge01" {
				t.Fatalf("running hostname = %q, want unchanged edge01", got)
			}
		})
	}
}

func TestWebConfigWriteEndpointRequiresJSONContentType(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
	}{
		{name: "missing"},
		{name: "text plain", contentType: "text/plain"},
		{name: "form", contentType: "application/x-www-form-urlencoded"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, eng := newWebConfigAPITestSource(t, "operator")
			req := httptest.NewRequest(http.MethodPost, "/api/config/commit", strings.NewReader(`{"config_text":"set system host-name edge02"}`))
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			req.SetBasicAuth("operator", "secret")
			rec := httptest.NewRecorder()
			source.handleWebConfigCommit(rec, req)

			if rec.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusUnsupportedMediaType, rec.Body.String())
			}
			if got := eng.Running().System.HostName; got != "edge01" {
				t.Fatalf("running hostname = %q, want unchanged edge01", got)
			}
		})
	}
}

func TestWebConfigWriteEndpointRejectsCrossOriginHeaders(t *testing.T) {
	tests := []struct {
		name   string
		header string
		value  string
	}{
		{name: "origin", header: "Origin", value: "https://evil.example"},
		{name: "referer", header: "Referer", value: "https://evil.example/config"},
		{name: "malformed origin", header: "Origin", value: "://bad"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, eng := newWebConfigAPITestSource(t, "operator")
			req := newWebJSONTestRequest(http.MethodPost, "/api/config/commit", `{"config_text":"set system host-name edge02"}`)
			req.Header.Set(tt.header, tt.value)
			req.SetBasicAuth("operator", "secret")
			rec := httptest.NewRecorder()
			source.handleWebConfigCommit(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusForbidden, rec.Body.String())
			}
			if got := eng.Running().System.HostName; got != "edge01" {
				t.Fatalf("running hostname = %q, want unchanged edge01", got)
			}
		})
	}
}

func TestWebConfigWriteEndpointAllowsSameOriginHeader(t *testing.T) {
	source, eng := newWebConfigAPITestSource(t, "operator")
	req := newWebJSONTestRequest(http.MethodPost, "/api/config/commit", `{"config_text":"set system host-name edge02"}`)
	req.Header.Set("Origin", "http://example.com")
	req.SetBasicAuth("operator", "secret")
	rec := httptest.NewRecorder()
	source.handleWebConfigCommit(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := eng.Running().System.HostName; got != "edge02" {
		t.Fatalf("running hostname = %q, want edge02", got)
	}
}

func TestWebConfigWriteEndpointRejectsReadOnlyRole(t *testing.T) {
	source, _ := newWebConfigAPITestSource(t, "read-only")

	req := newWebJSONTestRequest(http.MethodPost, "/api/config/validate", `{"config_text":"set system host-name edge02"}`)
	req.SetBasicAuth("read-only", "secret")
	rec := httptest.NewRecorder()
	source.handleWebConfigValidate(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestWebConfigWriteEndpointRejectsReadOnlyToken(t *testing.T) {
	source, _ := newWebConfigAPITestSource(t, "operator")
	source.webAPITokens = map[string]webAPIToken{
		"robot": {Name: "robot", Role: "read-only", Token: "secret-token"},
	}

	req := newWebJSONTestRequest(http.MethodPost, "/api/config/validate", `{"config_text":"set system host-name edge02"}`)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()
	source.handleWebConfigValidate(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestWebConfigValidateEndpointRedactsInternalErrors(t *testing.T) {
	source := newWebAuthTestSource(t, "operator", "secret", "operator")
	source.configAPI = &webConfigEditErrorTestAPI{
		createErr: errors.New("session store /var/lib/arca/session.db failed"),
	}

	req := newWebJSONTestRequest(http.MethodPost, "/api/config/validate", `{"config_text":"set system host-name edge02"}`)
	req.SetBasicAuth("operator", "secret")
	rec := httptest.NewRecorder()
	source.handleWebConfigValidate(rec, req)

	requireWebJSONInternalError(t, rec, "session store", "/var/lib/arca/session.db")
}

func TestWebConfigCommitEndpointRedactsInternalErrors(t *testing.T) {
	source := newWebAuthTestSource(t, "operator", "secret", "operator")
	source.configAPI = &webConfigEditErrorTestAPI{
		commitErr: errors.New("persist commit /var/lib/arca/config.db failed"),
	}

	req := newWebJSONTestRequest(http.MethodPost, "/api/config/commit", `{"config_text":"set system host-name edge02"}`)
	req.SetBasicAuth("operator", "secret")
	rec := httptest.NewRecorder()
	source.handleWebConfigCommit(rec, req)

	requireWebJSONInternalError(t, rec, "persist commit", "/var/lib/arca/config.db")
}

func TestWebConfigCommitEndpointKeepsBadRequestForNoChanges(t *testing.T) {
	source, _ := newWebConfigAPITestSource(t, "operator")
	cfg, err := source.runningConfig(false)
	if err != nil {
		t.Fatalf("runningConfig() error = %v", err)
	}
	body, err := json.Marshal(webConfigCommitRequest{ConfigText: cfg.ConfigText})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	req := newWebJSONTestRequest(http.MethodPost, "/api/config/commit", string(body))
	req.SetBasicAuth("operator", "secret")
	rec := httptest.NewRecorder()
	source.handleWebConfigCommit(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no configuration changes to commit") {
		t.Fatalf("response body = %q, want no-changes message", rec.Body.String())
	}
}

func TestWebConfigCommitEndpointReportsUnavailableConfigAPI(t *testing.T) {
	source := newWebAuthTestSource(t, "operator", "secret", "operator")

	req := newWebJSONTestRequest(http.MethodPost, "/api/config/commit", `{"config_text":"set system host-name edge02"}`)
	req.SetBasicAuth("operator", "secret")
	rec := httptest.NewRecorder()
	source.handleWebConfigCommit(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), errWebConfigAPIUnavailable.Error()) {
		t.Fatalf("response body = %q, want API unavailable message", rec.Body.String())
	}
}

func TestWebConfigHistoryEndpointUsesConfigAPI(t *testing.T) {
	source := newWebAuthTestSource(t, "monitor", "secret", "read-only")
	source.configAPI = webHistoryTestAPI{history: []nbgrpc.CommitInfo{
		{
			CommitID:  "abcdef1234567890",
			User:      "operator",
			Timestamp: time.Date(2026, 5, 13, 9, 10, 11, 0, time.UTC),
			Message:   "web update",
		},
	}}

	req := httptest.NewRequest(http.MethodGet, "/api/config/history?limit=1", nil)
	req.SetBasicAuth("monitor", "secret")
	rec := httptest.NewRecorder()
	source.handleWebConfigHistory(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var resp webConfigHistoryResponse
	if err := json.NewDecoder(rec.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(resp.Entries))
	}
	entry := resp.Entries[0]
	if entry.ShortCommitID != "abcdef123456" || entry.User != "operator" || entry.Message != "web update" {
		t.Fatalf("entry = %#v, want shortened operator web update", entry)
	}
	if entry.Timestamp != "2026-05-13T09:10:11Z" {
		t.Fatalf("Timestamp = %q, want RFC3339 UTC", entry.Timestamp)
	}
}

func TestWebConfigHistoryEndpointRejectsInvalidPagination(t *testing.T) {
	tests := []string{
		"/api/config/history?limit=abc",
		"/api/config/history?limit=0",
		"/api/config/history?offset=-1",
		"/api/config/history?offset=abc",
	}
	for _, target := range tests {
		t.Run(target, func(t *testing.T) {
			source := newWebAuthTestSource(t, "monitor", "secret", "read-only")
			source.configAPI = webHistoryTestAPI{}

			req := httptest.NewRequest(http.MethodGet, target, nil)
			req.SetBasicAuth("monitor", "secret")
			rec := httptest.NewRecorder()
			source.handleWebConfigHistory(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}

func TestWebConfigHistoryEndpointRedactsInternalErrors(t *testing.T) {
	source := newWebAuthTestSource(t, "monitor", "secret", "read-only")
	source.configAPI = webHistoryTestAPI{err: errors.New("sqlite backend /var/lib/arca/history.db failed")}

	req := httptest.NewRequest(http.MethodGet, "/api/config/history", nil)
	req.SetBasicAuth("monitor", "secret")
	rec := httptest.NewRecorder()
	source.handleWebConfigHistory(rec, req)

	requireWebJSONInternalError(t, rec, "sqlite backend", "/var/lib/arca/history.db")
}

func TestWebAuditEndpointRequiresAdminRole(t *testing.T) {
	source := newWebAuthTestSource(t, "monitor", "secret", "read-only")
	source.configAPI = &webAuditTestAPI{}

	req := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	req.SetBasicAuth("monitor", "secret")
	rec := httptest.NewRecorder()
	source.handleWebAudit(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestWebAuditEndpointExportsFilteredEvents(t *testing.T) {
	source := newWebAuthTestSource(t, "admin", "secret", "admin")
	auditAPI := &webAuditTestAPI{events: []nbgrpc.AuditEventInfo{
		{
			ID:        7,
			Timestamp: time.Date(2026, 5, 17, 10, 11, 12, 0, time.UTC),
			User:      "alice",
			SessionID: "session-1",
			SourceIP:  "192.0.2.10",
			Action:    "access_denied",
			Result:    "denied",
			ErrorCode: "rbac-deny",
			Details:   map[string]any{"operation": "kill-session"},
		},
	}}
	source.configAPI = auditAPI

	req := httptest.NewRequest(http.MethodGet, "/api/audit?limit=1&offset=2&user=alice&action=access_denied&result=denied&since=2026-05-17T00:00:00Z&until=2026-05-18T00:00:00Z", nil)
	req.SetBasicAuth("admin", "secret")
	rec := httptest.NewRecorder()
	source.handleWebAudit(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if auditAPI.opts.Limit != 1 || auditAPI.opts.Offset != 2 || auditAPI.opts.User != "alice" ||
		auditAPI.opts.Action != "access_denied" || auditAPI.opts.Result != "denied" ||
		auditAPI.opts.StartTime.IsZero() || auditAPI.opts.EndTime.IsZero() {
		t.Fatalf("audit options = %#v, want filtered request options", auditAPI.opts)
	}
	var resp webAuditResponse
	if err := json.NewDecoder(rec.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if resp.SchemaVersion != webAuditSchemaVersion || resp.Count != 1 || resp.Limit != 1 || resp.Offset != 2 {
		t.Fatalf("audit response metadata = %#v, want schema/count/limit/offset", resp)
	}
	entry := resp.Entries[0]
	if entry.ID != 7 || entry.User != "alice" || entry.Action != "access_denied" ||
		entry.ErrorCode != "rbac-deny" || entry.Timestamp != "2026-05-17T10:11:12Z" {
		t.Fatalf("audit entry = %#v, want exported RBAC denial", entry)
	}
	if entry.Details["operation"] != "kill-session" {
		t.Fatalf("audit entry details = %#v, want operation detail", entry.Details)
	}
}

func TestWebAuditEndpointRejectsInvalidTimeRange(t *testing.T) {
	source := newWebAuthTestSource(t, "admin", "secret", "admin")
	source.configAPI = &webAuditTestAPI{}

	req := httptest.NewRequest(http.MethodGet, "/api/audit?since=2026-05-18T00:00:00Z&until=2026-05-17T00:00:00Z", nil)
	req.SetBasicAuth("admin", "secret")
	rec := httptest.NewRecorder()
	source.handleWebAudit(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestWebAuditEndpointRedactsInternalErrors(t *testing.T) {
	source := newWebAuthTestSource(t, "admin", "secret", "admin")
	source.configAPI = &webAuditTestAPI{err: errors.New("audit backend secret table failed")}

	req := httptest.NewRequest(http.MethodGet, "/api/audit", nil)
	req.SetBasicAuth("admin", "secret")
	rec := httptest.NewRecorder()
	source.handleWebAudit(rec, req)

	requireWebJSONInternalError(t, rec, "audit backend", "secret table")
}

func TestWebIndexTemplateAssetLoaded(t *testing.T) {
	if !strings.HasPrefix(webIndexHTML, "<!doctype html>") {
		t.Fatalf("webIndexHTML prefix = %q, want doctype", webIndexHTML[:min(32, len(webIndexHTML))])
	}
	for _, want := range []string{
		`id="config-editor"`,
		`id="commit-config"`,
		"/api/config/commit",
	} {
		if !strings.Contains(webIndexHTML, want) {
			t.Fatalf("webIndexHTML missing %q", want)
		}
	}
}

func TestWebIndexEndpoint(t *testing.T) {
	eng := engine.NewEngine(nil, slog.Default())
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{HostName: "edge01"}
	eng.InitializeRunning(cfg, 42)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	metricsSource{
		startedAt: time.Now().Add(-2 * time.Minute),
		engine:    eng,
	}.handleWebIndex(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	text := string(body)
	for _, want := range []string{
		"edge01",
		"Config version",
		"NETCONF",
		"Datastore",
		"Cluster sync",
		"Class of service",
		"VPP LCP",
		"Commit history",
		`id="commit-history"`,
		"Configuration editor",
		"set system host-name edge01",
		"/api/status",
		"/api/nms/v1/status",
		"/api/nms/v1/telemetry/paths",
		"/api/nms/v1/telemetry/schemas",
		"/api/nms/v1/telemetry/snapshot",
		"/api/config",
		"/api/config/history",
		"refreshHistory",
		"/api/config/validate",
		"/api/config/commit",
		"validate-config",
		"commit-config",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("index missing %q:\n%s", want, text)
		}
	}
}

func newWebAuthTestSource(t *testing.T, username, password, role string) metricsSource {
	t.Helper()
	hash, err := pkgconfig.NormalizePasswordForStorage(password)
	if err != nil {
		t.Fatalf("NormalizePasswordForStorage() error = %v", err)
	}
	eng := engine.NewEngine(nil, slog.Default())
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{HostName: "edge01"}
	cfg.Security = &model.SecurityConfig{
		Users: map[string]*model.UserConfig{
			username: {
				Password: hash,
				Role:     role,
			},
		},
	}
	eng.InitializeRunning(cfg, 42)
	return metricsSource{
		startedAt: time.Now().Add(-2 * time.Minute),
		engine:    eng,
		webLog:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func newWebJSONTestRequest(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func newWebConfigAPITestSource(t *testing.T, role string) (metricsSource, *engine.Engine) {
	t.Helper()
	installParserHooks()
	eng := engine.NewEngine(nil, slog.Default())
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{HostName: "edge01"}
	hash, err := pkgconfig.NormalizePasswordForStorage("secret")
	if err != nil {
		t.Fatalf("NormalizePasswordForStorage() error = %v", err)
	}
	cfg.Security = &model.SecurityConfig{
		Users: map[string]*model.UserConfig{
			role: {
				Password: hash,
				Role:     role,
			},
		},
	}
	eng.InitializeRunning(cfg, 42)
	configAPI := nbgrpc.NewServer(eng, nil, slog.Default())
	return metricsSource{
		startedAt: time.Now().Add(-2 * time.Minute),
		engine:    eng,
		configAPI: configAPI,
	}, eng
}

type webConfigEditErrorTestAPI struct {
	webConfigAPI
	createErr           error
	acquireErr          error
	replaceErr          error
	validateErr         error
	diffErr             error
	commitErr           error
	commitCorrelationID string
}

func (a *webConfigEditErrorTestAPI) CreateSession(ctx context.Context, user string) (string, error) {
	if a.createErr != nil {
		return "", a.createErr
	}
	return "session-1", nil
}

func (a *webConfigEditErrorTestAPI) CloseSession(ctx context.Context, sessionID string) error {
	return nil
}

func (a *webConfigEditErrorTestAPI) AcquireLock(ctx context.Context, sessionID, user string) error {
	return a.acquireErr
}

func (a *webConfigEditErrorTestAPI) ReleaseLock(ctx context.Context, sessionID string) error {
	return nil
}

func (a *webConfigEditErrorTestAPI) ReplaceCandidate(ctx context.Context, sessionID, configText string) error {
	return a.replaceErr
}

func (a *webConfigEditErrorTestAPI) ValidateCandidate(ctx context.Context, sessionID string) error {
	return a.validateErr
}

func (a *webConfigEditErrorTestAPI) Diff(ctx context.Context, sessionID string) (string, bool, error) {
	if a.diffErr != nil {
		return "", false, a.diffErr
	}
	return "- set system host-name edge01\n+ set system host-name edge02\n", true, nil
}

func (a *webConfigEditErrorTestAPI) Commit(ctx context.Context, sessionID, user, message string) (string, uint64, error) {
	a.commitCorrelationID = correlation.ID(ctx)
	if a.commitErr != nil {
		return "", 0, a.commitErr
	}
	return "commit-1", 43, nil
}

type webHistoryTestAPI struct {
	webConfigAPI
	history []nbgrpc.CommitInfo
	err     error
}

func (a webHistoryTestAPI) ListHistory(ctx context.Context, limit, offset int) ([]nbgrpc.CommitInfo, error) {
	if a.err != nil {
		return nil, a.err
	}
	if offset >= len(a.history) {
		return nil, nil
	}
	history := a.history[offset:]
	if limit > 0 && limit < len(history) {
		history = history[:limit]
	}
	return history, nil
}

type webAuditTestAPI struct {
	webConfigAPI
	events []nbgrpc.AuditEventInfo
	opts   nbgrpc.AuditLogOptions
	err    error
}

func (a *webAuditTestAPI) ListAuditEvents(ctx context.Context, opts nbgrpc.AuditLogOptions) ([]nbgrpc.AuditEventInfo, error) {
	a.opts = opts
	if a.err != nil {
		return nil, a.err
	}
	return a.events, nil
}

type webTelemetryTestAPI struct {
	events []nbgrpc.TelemetryEvent
	paths  []string
	once   bool
	sent   int
	err    error
}

func (a *webTelemetryTestAPI) SubscribeTelemetry(ctx context.Context, rawPaths []string, interval time.Duration, once bool, send func(nbgrpc.TelemetryEvent) error) error {
	a.paths = append([]string(nil), rawPaths...)
	a.once = once
	if a.err != nil {
		return a.err
	}
	for _, event := range a.events {
		a.sent++
		if err := send(event); err != nil {
			return err
		}
	}
	return nil
}
