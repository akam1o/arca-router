package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	if len(resp.Paths) == 0 {
		t.Fatal("Paths is empty, want telemetry path catalog")
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
	req := httptest.NewRequest(http.MethodGet, "/api/nms/v1/telemetry/paths?cardinality=per-route&payload_schema=arca.telemetry.routes.v1", nil)
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
	if resp.Paths[0].Cardinality != "per-route" || resp.Paths[0].PayloadSchema != "arca.telemetry.routes.v1" {
		t.Fatalf("filtered path = %#v, want route cardinality and schema", resp.Paths[0])
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

func TestNMSTelemetrySnapshotEndpoint(t *testing.T) {
	telemetry := &webTelemetryTestAPI{events: []nbgrpc.TelemetryEvent{
		{
			Sequence:      1,
			Timestamp:     time.Unix(1700000600, 123).UTC(),
			Path:          "/system",
			EventType:     "snapshot",
			Encoding:      nbgrpc.TelemetryEncoding(),
			SchemaVersion: nbgrpc.TelemetryEventSchemaVersion(),
			JSONPayload:   `{"hostname":"edge01"}`,
		},
		{
			Sequence:      2,
			Timestamp:     time.Unix(1700000601, 0).UTC(),
			Path:          "/interfaces",
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
	if len(resp.Paths) != 2 || resp.Paths[0] != "/system" || resp.Paths[1] != "/interfaces" {
		t.Fatalf("Paths = %#v, want emitted paths", resp.Paths)
	}
	wantPayloadBytes := len(`{"hostname":"edge01"}`) + len(`{"interfaces":[]}`)
	if resp.PayloadBytes != wantPayloadBytes || resp.MaxPayloadBytes != defaultNMSTelemetrySnapshotMaxPayloadBytes {
		t.Fatalf("payload budget = %d/%d, want %d/%d", resp.PayloadBytes, resp.MaxPayloadBytes, wantPayloadBytes, defaultNMSTelemetrySnapshotMaxPayloadBytes)
	}
	if resp.TimeoutMs != defaultNMSTelemetrySnapshotTimeout.Milliseconds() {
		t.Fatalf("TimeoutMs = %d, want %d", resp.TimeoutMs, defaultNMSTelemetrySnapshotTimeout.Milliseconds())
	}
	if len(resp.Events) != 2 || resp.Events[0].Path != "/system" || string(resp.Events[0].Payload) != `{"hostname":"edge01"}` {
		t.Fatalf("Events = %#v, want system payload event", resp.Events)
	}
	if resp.Events[0].PayloadBytes != len(`{"hostname":"edge01"}`) ||
		resp.Events[1].PayloadBytes != len(`{"interfaces":[]}`) {
		t.Fatalf("event payload bytes = %d/%d, want per-event payload lengths",
			resp.Events[0].PayloadBytes, resp.Events[1].PayloadBytes)
	}
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

func TestNMSTelemetrySnapshotEndpointRejectsInvalidTimeout(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/nms/v1/telemetry/snapshot?timeout=1h", nil)
	rec := httptest.NewRecorder()
	metricsSource{telemetryAPI: &webTelemetryTestAPI{}}.handleNMSTelemetrySnapshot(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestWebConfigEndpoint(t *testing.T) {
	eng := engine.NewEngine(nil, slog.Default())
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{HostName: "edge01"}
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

	req := httptest.NewRequest(http.MethodPost, "/api/config/validate", strings.NewReader(`{"config_text":"set system host-name edge02"}`))
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

	req := httptest.NewRequest(http.MethodPost, "/api/config/commit", strings.NewReader(`{"config_text":"set system host-name edge02","message":"web update"}`))
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

func TestWebConfigWriteEndpointRejectsReadOnlyRole(t *testing.T) {
	source, _ := newWebConfigAPITestSource(t, "read-only")

	req := httptest.NewRequest(http.MethodPost, "/api/config/validate", strings.NewReader(`{"config_text":"set system host-name edge02"}`))
	req.SetBasicAuth("read-only", "secret")
	rec := httptest.NewRecorder()
	source.handleWebConfigValidate(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
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
	}
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

type webHistoryTestAPI struct {
	webConfigAPI
	history []nbgrpc.CommitInfo
}

func (a webHistoryTestAPI) ListHistory(ctx context.Context, limit, offset int) ([]nbgrpc.CommitInfo, error) {
	if offset >= len(a.history) {
		return nil, nil
	}
	history := a.history[offset:]
	if limit > 0 && limit < len(history) {
		history = history[:limit]
	}
	return history, nil
}

type webTelemetryTestAPI struct {
	events []nbgrpc.TelemetryEvent
	paths  []string
	once   bool
	err    error
}

func (a *webTelemetryTestAPI) SubscribeTelemetry(ctx context.Context, rawPaths []string, interval time.Duration, once bool, send func(nbgrpc.TelemetryEvent) error) error {
	a.paths = append([]string(nil), rawPaths...)
	a.once = once
	if a.err != nil {
		return a.err
	}
	for _, event := range a.events {
		if err := send(event); err != nil {
			return err
		}
	}
	return nil
}
