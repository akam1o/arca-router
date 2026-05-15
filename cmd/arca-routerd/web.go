package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/akam1o/arca-router/internal/model"
	nbgrpc "github.com/akam1o/arca-router/internal/northbound/grpc"
	sbfrr "github.com/akam1o/arca-router/internal/southbound/frr"
	"github.com/akam1o/arca-router/pkg/auth"
	pkgconfig "github.com/akam1o/arca-router/pkg/config"
	"github.com/akam1o/arca-router/pkg/logger"
	pkgnetconf "github.com/akam1o/arca-router/pkg/netconf"
)

const defaultWebUIPort = 8080

const webAuthRealm = `Basic realm="arca-router", charset="UTF-8"`

const webDummyPasswordHash = "$argon2id$v=19$m=65536,t=3,p=4$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

const webConfigEditBodyLimit = 1 << 20

const nmsOperationalStatusSchemaVersion = "arca.nms.operational.v1"
const nmsTelemetryCatalogSchemaVersion = "arca.nms.telemetry-catalog.v1"
const nmsTelemetrySnapshotSchemaVersion = "arca.nms.telemetry-snapshot.v1"

const (
	defaultNMSTelemetrySnapshotTimeout         = 5 * time.Second
	maxNMSTelemetrySnapshotTimeout             = 30 * time.Second
	defaultNMSTelemetrySnapshotMaxPayloadBytes = 8 << 20
	maxNMSTelemetrySnapshotMaxPayloadBytes     = 64 << 20
)

var errNMSTelemetrySnapshotTooLarge = errors.New("nms telemetry snapshot payload budget exceeded")

type webConfigAPI interface {
	GetRunning(ctx context.Context) (string, uint64, error)
	CreateSession(ctx context.Context, user string) (string, error)
	CloseSession(ctx context.Context, sessionID string) error
	AcquireLock(ctx context.Context, sessionID, user string) error
	ReleaseLock(ctx context.Context, sessionID string) error
	EditCandidate(ctx context.Context, sessionID, configText string) error
	ValidateCandidate(ctx context.Context, sessionID string) error
	Diff(ctx context.Context, sessionID string) (string, bool, error)
	Commit(ctx context.Context, sessionID, user, message string) (string, uint64, error)
	ListHistory(ctx context.Context, limit, offset int) ([]nbgrpc.CommitInfo, error)
}

type webTelemetryAPI interface {
	SubscribeTelemetry(ctx context.Context, rawPaths []string, interval time.Duration, once bool, send func(nbgrpc.TelemetryEvent) error) error
}

type webStatus struct {
	Version         string          `json:"version"`
	Commit          string          `json:"commit"`
	BuildDate       string          `json:"build_date"`
	UptimeSeconds   float64         `json:"uptime_seconds"`
	ConfigVersion   uint64          `json:"config_version"`
	RunningHostname string          `json:"running_hostname"`
	Datastore       webDatastore    `json:"datastore"`
	ConfigSync      webConfigSync   `json:"config_sync"`
	Cluster         webCluster      `json:"cluster"`
	Overlay         webOverlayStats `json:"overlay"`
	HA              webHAStats      `json:"ha"`
	ClassOfService  webCoSStats     `json:"class_of_service"`
	FRR             webFRRStats     `json:"frr"`
	VPP             webVPPStats     `json:"vpp"`
	NETCONF         webNETCONFStats `json:"netconf"`
}

type nmsStatusResponse struct {
	SchemaVersion string    `json:"schema_version"`
	GeneratedAt   string    `json:"generated_at"`
	Resource      string    `json:"resource"`
	Data          webStatus `json:"data"`
}

type nmsTelemetryCatalogResponse struct {
	SchemaVersion      string             `json:"schema_version"`
	GeneratedAt        string             `json:"generated_at"`
	Resource           string             `json:"resource"`
	EventSchemaVersion string             `json:"event_schema_version"`
	Encoding           string             `json:"encoding"`
	DefaultPaths       []string           `json:"default_paths"`
	Paths              []nmsTelemetryPath `json:"paths"`
}

type nmsTelemetrySnapshotResponse struct {
	SchemaVersion      string                      `json:"schema_version"`
	GeneratedAt        string                      `json:"generated_at"`
	Resource           string                      `json:"resource"`
	EventSchemaVersion string                      `json:"event_schema_version"`
	Encoding           string                      `json:"encoding"`
	Paths              []string                    `json:"paths"`
	PayloadBytes       int                         `json:"payload_bytes"`
	MaxPayloadBytes    int                         `json:"max_payload_bytes"`
	TimeoutMs          int64                       `json:"timeout_ms"`
	Events             []nmsTelemetrySnapshotEvent `json:"events"`
}

type nmsTelemetrySnapshotEvent struct {
	Sequence      uint64          `json:"sequence"`
	Timestamp     string          `json:"timestamp,omitempty"`
	Path          string          `json:"path"`
	EventType     string          `json:"event_type"`
	Encoding      string          `json:"encoding"`
	SchemaVersion string          `json:"schema_version"`
	PayloadBytes  int             `json:"payload_bytes"`
	Payload       json.RawMessage `json:"payload"`
}

type nmsTelemetryPath struct {
	Path          string   `json:"path"`
	Description   string   `json:"description"`
	Cardinality   string   `json:"cardinality"`
	PayloadSchema string   `json:"payload_schema"`
	Aliases       []string `json:"aliases,omitempty"`
	Default       bool     `json:"default"`
}

type nmsTelemetryCatalogFilters struct {
	paths          []string
	cardinalities  []string
	payloadSchemas []string
	defaultOnly    bool
}

type nmsTelemetrySnapshotOptions struct {
	paths           []string
	timeout         time.Duration
	maxPayloadBytes int
}

type webDatastore struct {
	Backend       string   `json:"backend"`
	EtcdEndpoints []string `json:"etcd_endpoints,omitempty"`
}

type webConfigSync struct {
	Enabled         bool   `json:"enabled"`
	Healthy         bool   `json:"healthy"`
	EtcdRevision    int64  `json:"etcd_revision,omitempty"`
	RunningRevision int64  `json:"running_revision,omitempty"`
	RunningCommitID string `json:"running_commit_id,omitempty"`
	LastCheck       string `json:"last_check,omitempty"`
	LastApply       string `json:"last_apply,omitempty"`
	LastError       string `json:"last_error,omitempty"`
}

type webCluster struct {
	Enabled            bool     `json:"enabled"`
	NodeCount          int      `json:"node_count"`
	EtcdSyncConfigured bool     `json:"etcd_sync_configured"`
	EtcdEndpoints      []string `json:"etcd_endpoints,omitempty"`
	SyncAligned        bool     `json:"sync_aligned"`
}

type webOverlayStats struct {
	EVPN webEVPNStats `json:"evpn"`
}

type webEVPNStats struct {
	Configured    bool `json:"configured"`
	VNIs          int  `json:"vnis"`
	L2VNIs        int  `json:"l2_vnis"`
	L3VNIs        int  `json:"l3_vnis"`
	MulticastVNIs int  `json:"multicast_vnis"`
}

type webHAStats struct {
	Configured bool     `json:"configured"`
	Converged  bool     `json:"converged"`
	VRRPGroups int      `json:"vrrp_groups"`
	IssueCount int      `json:"issue_count"`
	Issues     []string `json:"issues,omitempty"`
}

type webCoSStats struct {
	Configured             bool               `json:"configured"`
	EnforcementStatus      string             `json:"enforcement_status"`
	ForwardingClasses      int                `json:"forwarding_classes"`
	TrafficControlProfiles int                `json:"traffic_control_profiles"`
	InterfaceBindings      int                `json:"interface_bindings"`
	IntentOnly             bool               `json:"intent_only"`
	Capabilities           webCoSCapabilities `json:"capabilities"`
}

type webCoSCapabilities struct {
	LastCheck                string   `json:"last_check,omitempty"`
	MetadataBindingSupported bool     `json:"metadata_binding_supported"`
	QueueSchedulerSupported  bool     `json:"queue_scheduler_supported"`
	PolicerSupported         bool     `json:"policer_supported"`
	CountersSupported        bool     `json:"counters_supported"`
	Diagnostics              []string `json:"diagnostics,omitempty"`
	LastError                string   `json:"last_error,omitempty"`
}

type webFRRStats struct {
	VRRP webVRRPStats `json:"vrrp"`
	BFD  webBFDStats  `json:"bfd"`
}

type webVRRPStats struct {
	LastCheck        string              `json:"last_check,omitempty"`
	ConfiguredGroups int                 `json:"configured_groups"`
	ObservedGroups   int                 `json:"observed_groups"`
	ActiveGroups     int                 `json:"active_groups"`
	Groups           []webVRRPGroupStats `json:"groups,omitempty"`
	IssueCount       int                 `json:"issue_count"`
	Issues           []string            `json:"issues,omitempty"`
	LastError        string              `json:"last_error,omitempty"`
}

type webVRRPGroupStats struct {
	Interface      string `json:"interface"`
	ID             int    `json:"id"`
	VirtualAddress string `json:"virtual_address,omitempty"`
	State          string `json:"state"`
	Observed       bool   `json:"observed"`
	Active         bool   `json:"active"`
}

type webBFDStats struct {
	LastCheck         string            `json:"last_check,omitempty"`
	ConfiguredPeers   int               `json:"configured_peers"`
	ObservedPeers     int               `json:"observed_peers"`
	UpPeers           int               `json:"up_peers"`
	DownPeers         int               `json:"down_peers"`
	SessionDownEvents int               `json:"session_down_events"`
	RxFailPackets     int               `json:"rx_fail_packets"`
	Peers             []webBFDPeerStats `json:"peers,omitempty"`
	IssueCount        int               `json:"issue_count"`
	Issues            []string          `json:"issues,omitempty"`
	LastError         string            `json:"last_error,omitempty"`
}

type webBFDPeerStats struct {
	Peer              string `json:"peer"`
	LocalAddress      string `json:"local_address,omitempty"`
	Interface         string `json:"interface,omitempty"`
	VRF               string `json:"vrf,omitempty"`
	Status            string `json:"status"`
	Diagnostic        string `json:"diagnostic,omitempty"`
	RemoteDiagnostic  string `json:"remote_diagnostic,omitempty"`
	Observed          bool   `json:"observed"`
	Up                bool   `json:"up"`
	SessionDownEvents int    `json:"session_down_events"`
	RxFailPackets     int    `json:"rx_fail_packets"`
}

type webVPPStats struct {
	LCP webLCPSyncStats `json:"lcp"`
}

type webLCPSyncStats struct {
	LastReconcile      string   `json:"last_reconcile,omitempty"`
	PairCount          int      `json:"pair_count"`
	InconsistencyCount int      `json:"inconsistency_count"`
	Inconsistencies    []string `json:"inconsistencies,omitempty"`
	LastError          string   `json:"last_error,omitempty"`
}

type webNETCONFStats struct {
	Listening         bool   `json:"listening"`
	ActiveSessions    int    `json:"active_sessions"`
	ActiveConnections int32  `json:"active_connections"`
	TotalConnections  uint64 `json:"total_connections"`
	SuccessfulAuth    uint64 `json:"successful_auth"`
	FailedAuth        uint64 `json:"failed_auth"`
}

type webConfig struct {
	ConfigText string `json:"config_text"`
	Version    uint64 `json:"version"`
}

type webConfigEditRequest struct {
	ConfigText string `json:"config_text"`
}

type webConfigCommitRequest struct {
	ConfigText string `json:"config_text"`
	Message    string `json:"message"`
}

type webConfigValidateResponse struct {
	Valid      bool   `json:"valid"`
	HasChanges bool   `json:"has_changes"`
	DiffText   string `json:"diff_text,omitempty"`
}

type webConfigCommitResponse struct {
	CommitID string `json:"commit_id"`
	Version  uint64 `json:"version"`
}

type webConfigHistoryResponse struct {
	Entries []webCommitEntry `json:"entries"`
}

type webCommitEntry struct {
	CommitID      string `json:"commit_id"`
	ShortCommitID string `json:"short_commit_id"`
	User          string `json:"user"`
	Timestamp     string `json:"timestamp"`
	Message       string `json:"message"`
	IsRollback    bool   `json:"is_rollback"`
}

type webAuthUser struct {
	PasswordHash string
	Role         string
}

type webIndexData struct {
	Status                   webStatus
	Uptime                   string
	NETCONFState             string
	NETCONFStateClass        string
	NETCONFConnections       string
	ClusterState             string
	ClusterStateClass        string
	ClusterSyncState         string
	ClusterSyncAlignment     string
	ClusterNodeCount         string
	ConfigSyncState          string
	ConfigSyncStateClass     string
	ConfigSyncRevision       string
	ConfigSyncLastApply      string
	HAState                  string
	HAStateClass             string
	HAVRPGroups              string
	HAIssues                 string
	ClassOfServiceState      string
	ClassOfServiceClass      string
	ClassOfServiceProfiles   string
	ClassOfServiceBindings   string
	ClassOfServiceClasses    string
	ClassOfServiceScheduler  string
	ClassOfServicePolicer    string
	ClassOfServiceCounters   string
	ClassOfServiceDiagnostic string
	FRRVRRPState             string
	FRRVRRPStateClass        string
	FRRVRRPActiveGroups      string
	FRRVRRPGroups            []webVRRPGroupView
	FRRBFDState              string
	FRRBFDStateClass         string
	FRRBFDUpPeers            string
	FRRBFDSessionDownEvents  string
	FRRBFDRxFailPackets      string
	FRRBFDPeers              []webBFDPeerView
	VPPLCPState              string
	VPPLCPStateClass         string
	VPPLCPPairs              string
	VPPLCPInconsistencies    string
	VPPLCPLastReconcile      string
	DatastoreBackend         string
	GeneratedAt              string
	ConfigVersionString      string
	RunningConfig            string
	History                  []webCommitEntry
}

type webVRRPGroupView struct {
	Label      string
	State      string
	StateClass string
}

type webBFDPeerView struct {
	Label      string
	State      string
	StateClass string
	Counters   string
}

var webIndexTemplate = template.Must(template.New("web-index").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>arca-router</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f6f7f9;
      --panel: #ffffff;
      --line: #d8dde5;
      --text: #17202a;
      --muted: #667085;
      --accent: #0f766e;
      --warn: #b45309;
      --neutral: #475467;
      --ok-bg: #dff5ef;
      --warn-bg: #fff1d6;
      --neutral-bg: #eef2f6;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      background: var(--bg);
      color: var(--text);
      font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    main {
      width: min(1120px, calc(100vw - 32px));
      margin: 0 auto;
      padding: 32px 0;
    }
    header {
      display: flex;
      align-items: flex-end;
      justify-content: space-between;
      gap: 24px;
      padding-bottom: 20px;
      border-bottom: 1px solid var(--line);
    }
    h1, h2, p { margin: 0; }
    h1 { font-size: clamp(28px, 4vw, 44px); font-weight: 650; }
    h2 { font-size: 15px; font-weight: 650; }
    .version { color: var(--muted); font-size: 14px; text-align: right; }
    .grid {
      display: grid;
      grid-template-columns: repeat(4, minmax(0, 1fr));
      gap: 12px;
      margin-top: 20px;
    }
    .panel {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 16px;
      min-height: 116px;
    }
    .span-2 { grid-column: span 2; }
    .span-4 { grid-column: span 4; }
    .metric {
      display: flex;
      flex-direction: column;
      gap: 10px;
    }
    .label { color: var(--muted); font-size: 13px; }
    .value {
      overflow-wrap: anywhere;
      font-size: 28px;
      font-weight: 650;
      line-height: 1.1;
    }
    .small-value { font-size: 18px; line-height: 1.25; }
    .rows {
      display: grid;
      gap: 10px;
      margin-top: 14px;
    }
    .row {
      display: flex;
      justify-content: space-between;
      gap: 16px;
      color: var(--muted);
      font-size: 14px;
    }
    .row strong { color: var(--text); font-weight: 600; }
    .pill {
      display: inline-flex;
      align-items: center;
      width: fit-content;
      min-height: 28px;
      padding: 3px 10px;
      border-radius: 999px;
      font-size: 13px;
      font-weight: 650;
    }
    .pill.ok { background: var(--ok-bg); color: var(--accent); }
    .pill.warn { background: var(--warn-bg); color: var(--warn); }
    .pill.neutral { background: var(--neutral-bg); color: var(--neutral); }
    .config {
      max-height: 300px;
      margin: 14px 0 0;
      overflow: auto;
      overflow-wrap: anywhere;
      white-space: pre-wrap;
      border: 1px solid var(--line);
      border-radius: 6px;
      background: #f8fafc;
      padding: 12px;
      color: var(--text);
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
      font-size: 12px;
      line-height: 1.5;
    }
    .history-message {
      max-width: 70%;
      overflow-wrap: anywhere;
      color: var(--text);
    }
    .config-editor {
      display: block;
      width: 100%;
      min-height: 320px;
      max-height: none;
      resize: vertical;
    }
    .actions {
      display: flex;
      flex-wrap: wrap;
      gap: 10px;
      margin-top: 12px;
    }
    .message-input {
      flex: 1 1 260px;
      min-height: 36px;
      border: 1px solid var(--line);
      border-radius: 6px;
      padding: 8px 10px;
      color: var(--text);
      font: inherit;
    }
    .button {
      min-height: 36px;
      border: 1px solid var(--line);
      border-radius: 6px;
      background: #ffffff;
      color: var(--text);
      padding: 8px 14px;
      font: inherit;
      font-weight: 650;
      cursor: pointer;
    }
    .button.primary {
      border-color: var(--accent);
      background: var(--accent);
      color: #ffffff;
    }
    .button:disabled,
    .message-input:disabled,
    .config-editor:disabled {
      cursor: wait;
      opacity: 0.65;
    }
    .status-line {
      display: block;
      min-height: 20px;
      margin-top: 12px;
      color: var(--muted);
      font-size: 13px;
    }
    .status-line[data-state="ok"] { color: var(--accent); }
    .status-line[data-state="warn"] { color: var(--warn); }
    .diff[hidden] { display: none; }
    footer {
      display: flex;
      justify-content: space-between;
      gap: 16px;
      margin-top: 20px;
      color: var(--muted);
      font-size: 12px;
    }
    @media (max-width: 860px) {
      main { width: min(100vw - 24px, 720px); padding: 20px 0; }
      header { align-items: flex-start; flex-direction: column; }
      .version { text-align: left; }
      .grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    }
    @media (max-width: 560px) {
      .grid { grid-template-columns: 1fr; }
      .span-2 { grid-column: auto; }
      .span-4 { grid-column: auto; }
      footer { flex-direction: column; }
    }
  </style>
</head>
<body>
  <main>
    <header>
      <div>
        <h1>{{.Status.RunningHostname}}</h1>
        <p class="label">arca-router</p>
      </div>
      <p class="version">v{{.Status.Version}} | {{.Status.Commit}}</p>
    </header>

    <section class="grid">
      <article class="panel metric">
        <span class="label">Config version</span>
        <span class="value">{{.ConfigVersionString}}</span>
      </article>
      <article class="panel metric">
        <span class="label">Uptime</span>
        <span class="value">{{.Uptime}}</span>
      </article>
      <article class="panel metric">
        <span class="label">NETCONF</span>
        <span class="pill {{.NETCONFStateClass}}">{{.NETCONFState}}</span>
      </article>
      <article class="panel metric">
        <span class="label">Connections</span>
        <span class="value">{{.NETCONFConnections}}</span>
      </article>
      <article class="panel metric">
        <span class="label">Datastore</span>
        <span class="value small-value">{{.DatastoreBackend}}</span>
      </article>
      <article class="panel metric">
        <span class="label">Cluster</span>
        <span class="pill {{.ClusterStateClass}}">{{.ClusterState}}</span>
      </article>
      <article class="panel metric">
        <span class="label">Class of service</span>
        <span class="pill {{.ClassOfServiceClass}}">{{.ClassOfServiceState}}</span>
      </article>

      <article class="panel span-2">
        <h2>NETCONF sessions</h2>
        <div class="rows">
          <div class="row"><span>Active sessions</span><strong>{{.Status.NETCONF.ActiveSessions}}</strong></div>
          <div class="row"><span>Active SSH connections</span><strong>{{.Status.NETCONF.ActiveConnections}}</strong></div>
          <div class="row"><span>Total SSH connections</span><strong>{{.Status.NETCONF.TotalConnections}}</strong></div>
        </div>
      </article>
      <article class="panel span-2">
        <h2>Authentication</h2>
        <div class="rows">
          <div class="row"><span>Successful handshakes</span><strong>{{.Status.NETCONF.SuccessfulAuth}}</strong></div>
          <div class="row"><span>Failed handshakes</span><strong>{{.Status.NETCONF.FailedAuth}}</strong></div>
          <div class="row"><span>Build date</span><strong>{{.Status.BuildDate}}</strong></div>
        </div>
      </article>
      <article class="panel span-2">
        <h2>Cluster sync</h2>
        <div class="rows">
          <div class="row"><span>Configured nodes</span><strong>{{.ClusterNodeCount}}</strong></div>
          <div class="row"><span>etcd sync</span><strong>{{.ClusterSyncState}}</strong></div>
          <div class="row"><span>Backend alignment</span><strong>{{.ClusterSyncAlignment}}</strong></div>
          <div class="row"><span>Config sync</span><strong><span class="pill {{.ConfigSyncStateClass}}">{{.ConfigSyncState}}</span></strong></div>
          <div class="row"><span>Running revision</span><strong>{{.ConfigSyncRevision}}</strong></div>
          <div class="row"><span>Last apply</span><strong>{{.ConfigSyncLastApply}}</strong></div>
          <div class="row"><span>HA convergence</span><strong><span class="pill {{.HAStateClass}}">{{.HAState}}</span></strong></div>
          <div class="row"><span>VRRP groups</span><strong>{{.HAVRPGroups}}</strong></div>
          <div class="row"><span>HA issues</span><strong>{{.HAIssues}}</strong></div>
        </div>
      </article>
      <article class="panel span-2">
        <h2>Class of service</h2>
        <div class="rows">
          <div class="row"><span>Enforcement</span><strong><span class="pill {{.ClassOfServiceClass}}">{{.ClassOfServiceState}}</span></strong></div>
          <div class="row"><span>Traffic-control profiles</span><strong>{{.ClassOfServiceProfiles}}</strong></div>
          <div class="row"><span>Forwarding classes</span><strong>{{.ClassOfServiceClasses}}</strong></div>
          <div class="row"><span>Interface bindings</span><strong>{{.ClassOfServiceBindings}}</strong></div>
          <div class="row"><span>Queue scheduler</span><strong>{{.ClassOfServiceScheduler}}</strong></div>
          <div class="row"><span>Policer</span><strong>{{.ClassOfServicePolicer}}</strong></div>
          <div class="row"><span>QoS counters</span><strong>{{.ClassOfServiceCounters}}</strong></div>
          <div class="row"><span>Diagnostic</span><strong>{{.ClassOfServiceDiagnostic}}</strong></div>
        </div>
      </article>
      <article class="panel span-2">
        <h2>HA southbound</h2>
        <div class="rows">
          <div class="row"><span>FRR VRRP</span><strong><span class="pill {{.FRRVRRPStateClass}}">{{.FRRVRRPState}}</span></strong></div>
          <div class="row"><span>Active VRRP groups</span><strong>{{.FRRVRRPActiveGroups}}</strong></div>
          {{range .FRRVRRPGroups}}
          <div class="row"><span>{{.Label}}</span><strong><span class="pill {{.StateClass}}">{{.State}}</span></strong></div>
          {{end}}
          <div class="row"><span>FRR BFD</span><strong><span class="pill {{.FRRBFDStateClass}}">{{.FRRBFDState}}</span></strong></div>
          <div class="row"><span>Up BFD peers</span><strong>{{.FRRBFDUpPeers}}</strong></div>
          <div class="row"><span>BFD session-down</span><strong>{{.FRRBFDSessionDownEvents}}</strong></div>
          <div class="row"><span>BFD RX fail</span><strong>{{.FRRBFDRxFailPackets}}</strong></div>
          {{range .FRRBFDPeers}}
          <div class="row"><span>{{.Label}}</span><strong><span class="pill {{.StateClass}}">{{.State}}</span> {{.Counters}}</strong></div>
          {{end}}
          <div class="row"><span>VPP LCP</span><strong><span class="pill {{.VPPLCPStateClass}}">{{.VPPLCPState}}</span></strong></div>
          <div class="row"><span>Pairs</span><strong>{{.VPPLCPPairs}}</strong></div>
          <div class="row"><span>Inconsistencies</span><strong>{{.VPPLCPInconsistencies}}</strong></div>
          <div class="row"><span>Last check</span><strong>{{.VPPLCPLastReconcile}}</strong></div>
        </div>
      </article>
      <article class="panel span-2">
        <h2>Commit history</h2>
        <div id="commit-history" class="rows">
          {{range .History}}
          <div class="row"><span class="history-message">{{.Message}}</span><strong>{{.ShortCommitID}}</strong></div>
          {{else}}
          <div class="row"><span>No commits</span><strong>0</strong></div>
          {{end}}
        </div>
      </article>
      <article class="panel span-4">
        <h2>Configuration editor</h2>
        <textarea id="config-editor" class="config config-editor" spellcheck="false">{{.RunningConfig}}</textarea>
        <div class="actions">
          <input id="commit-message" class="message-input" type="text" placeholder="Commit message" autocomplete="off">
          <button id="validate-config" class="button" type="button">Validate</button>
          <button id="commit-config" class="button primary" type="button">Commit</button>
        </div>
        <output id="config-result" class="status-line"></output>
        <pre id="config-diff" class="config diff" hidden></pre>
      </article>
    </section>

    <footer>
      <span>Generated {{.GeneratedAt}}</span>
      <span>/api/status | /api/nms/v1/status | /api/nms/v1/telemetry/paths | /api/nms/v1/telemetry/snapshot | /api/config | /api/config/history | /api/config/validate | /api/config/commit</span>
    </footer>
  </main>
  <script>
    (() => {
      const editor = document.getElementById("config-editor");
      const message = document.getElementById("commit-message");
      const validateButton = document.getElementById("validate-config");
      const commitButton = document.getElementById("commit-config");
      const result = document.getElementById("config-result");
      const diff = document.getElementById("config-diff");
      const history = document.getElementById("commit-history");
      if (!editor || !message || !validateButton || !commitButton || !result || !diff) {
        return;
      }

      const setBusy = (busy) => {
        editor.disabled = busy;
        message.disabled = busy;
        validateButton.disabled = busy;
        commitButton.disabled = busy;
      };
      const setResult = (text, state) => {
        result.textContent = text;
        result.dataset.state = state || "neutral";
      };
      const requestJSON = async (path, payload) => {
        const response = await fetch(path, {
          method: "POST",
          credentials: "same-origin",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(payload),
        });
        let data = {};
        try {
          data = await response.json();
        } catch (_) {
          data = {};
        }
        if (!response.ok) {
          throw new Error(data.error || "HTTP " + response.status);
        }
        return data;
      };
      const refreshConfig = async () => {
        const response = await fetch("/api/config", { credentials: "same-origin" });
        if (!response.ok) {
          return;
        }
        const data = await response.json();
        if (typeof data.config_text === "string") {
          editor.value = data.config_text;
        }
      };
      const renderHistory = (entries) => {
        if (!history) {
          return;
        }
        history.replaceChildren();
        if (!Array.isArray(entries) || entries.length === 0) {
          const row = document.createElement("div");
          row.className = "row";
          const label = document.createElement("span");
          label.textContent = "No commits";
          const value = document.createElement("strong");
          value.textContent = "0";
          row.append(label, value);
          history.append(row);
          return;
        }
        for (const entry of entries) {
          const row = document.createElement("div");
          row.className = "row";
          const messageText = typeof entry.message === "string" && entry.message.trim() ? entry.message : "(no message)";
          const messageNode = document.createElement("span");
          messageNode.className = "history-message";
          messageNode.textContent = messageText;
          const idNode = document.createElement("strong");
          idNode.textContent = entry.short_commit_id || entry.commit_id || "";
          row.append(messageNode, idNode);
          history.append(row);
        }
      };
      const refreshHistory = async () => {
        if (!history) {
          return;
        }
        const response = await fetch("/api/config/history?limit=5", { credentials: "same-origin" });
        if (!response.ok) {
          return;
        }
        const data = await response.json();
        renderHistory(data.entries);
      };

      validateButton.addEventListener("click", async () => {
        setBusy(true);
        setResult("Validating", "neutral");
        diff.hidden = true;
        diff.textContent = "";
        try {
          const data = await requestJSON("/api/config/validate", { config_text: editor.value });
          diff.textContent = data.diff_text || "";
          diff.hidden = !data.diff_text;
          setResult(data.has_changes ? "Valid" : "No changes", "ok");
        } catch (err) {
          setResult(err.message, "warn");
        } finally {
          setBusy(false);
        }
      });

      commitButton.addEventListener("click", async () => {
        setBusy(true);
        setResult("Committing", "neutral");
        diff.hidden = true;
        diff.textContent = "";
        try {
          const data = await requestJSON("/api/config/commit", {
            config_text: editor.value,
            message: message.value,
          });
          await refreshConfig();
          await refreshHistory();
          setResult("Committed version " + data.version, "ok");
        } catch (err) {
          setResult(err.message, "warn");
        } finally {
          setBusy(false);
        }
      });
    })();
  </script>
</body>
</html>
`))

func effectiveWebListen(flagValue string, snapshot *model.ConfigSnapshot) string {
	if listen := strings.TrimSpace(flagValue); listen != "" {
		return listen
	}
	if snapshot == nil || snapshot.Config == nil || snapshot.Config.System == nil ||
		snapshot.Config.System.Services == nil || snapshot.Config.System.Services.WebUI == nil {
		return ""
	}
	web := snapshot.Config.System.Services.WebUI
	if !web.Enabled {
		return ""
	}
	addr := strings.TrimSpace(web.ListenAddress)
	if addr == "" {
		addr = "127.0.0.1"
	}
	port := web.Port
	if port == 0 {
		port = defaultWebUIPort
	}
	return net.JoinHostPort(addr, strconv.Itoa(port))
}

func startWebServer(ctx context.Context, listenAddr string, source metricsSource, log *logger.Logger) (<-chan error, error) {
	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen web endpoint: %w", err)
	}

	srv := &http.Server{
		Handler:           newWebMux(source),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("Web endpoint started", slog.String("listen", lis.Addr().String()))
		if err := srv.Serve(lis); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error("Web endpoint shutdown failed", slog.Any("error", err))
		}
	}()

	return errCh, nil
}

func newWebMux(source metricsSource) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", source.handleWebIndex)
	mux.HandleFunc("/api/config", source.handleWebConfig)
	mux.HandleFunc("/api/config/commit", source.handleWebConfigCommit)
	mux.HandleFunc("/api/config/history", source.handleWebConfigHistory)
	mux.HandleFunc("/api/status", source.handleWebStatus)
	mux.HandleFunc("/api/nms/v1/status", source.handleNMSStatus)
	mux.HandleFunc("/api/nms/v1/telemetry/paths", source.handleNMSTelemetryCatalog)
	mux.HandleFunc("/api/nms/v1/telemetry/snapshot", source.handleNMSTelemetrySnapshot)
	mux.HandleFunc("/api/config/validate", source.handleWebConfigValidate)
	return mux
}

func (s metricsSource) handleWebStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorizeWebRead(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(newWebStatus(s.snapshot(time.Now()))); err != nil {
		http.Error(w, "encode status: "+err.Error(), http.StatusInternalServerError)
	}
}

func (s metricsSource) handleNMSStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorizeWebRead(w, r) {
		return
	}
	now := time.Now()
	writeWebJSON(w, http.StatusOK, newNMSStatusResponse(now, s.snapshot(now)))
}

func (s metricsSource) handleNMSTelemetryCatalog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorizeWebRead(w, r) {
		return
	}
	writeWebJSON(w, http.StatusOK, newNMSTelemetryCatalogResponse(time.Now(), nmsTelemetryCatalogFiltersFromRequest(r)))
}

func (s metricsSource) handleNMSTelemetrySnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorizeWebRead(w, r) {
		return
	}
	if s.telemetryAPI == nil {
		writeWebJSONError(w, http.StatusServiceUnavailable, "telemetry API is not available")
		return
	}
	opts, err := nmsTelemetrySnapshotOptionsFromRequest(r)
	if err != nil {
		writeWebJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	now := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), opts.timeout)
	defer cancel()
	events, payloadBytes, err := s.collectNMSTelemetrySnapshot(ctx, opts)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case strings.Contains(err.Error(), "unsupported telemetry path"):
			status = http.StatusBadRequest
		case errors.Is(err, errNMSTelemetrySnapshotTooLarge):
			status = http.StatusRequestEntityTooLarge
		case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
			status = http.StatusGatewayTimeout
		}
		writeWebJSONError(w, status, err.Error())
		return
	}
	writeWebJSON(w, http.StatusOK, newNMSTelemetrySnapshotResponse(now, events, opts, payloadBytes))
}

func (s metricsSource) handleWebConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorizeWebRead(w, r) {
		return
	}
	cfg, err := s.runningConfig()
	if err != nil {
		http.Error(w, "render config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(cfg); err != nil {
		http.Error(w, "encode config: "+err.Error(), http.StatusInternalServerError)
	}
}

func (s metricsSource) handleWebConfigHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorizeWebRead(w, r) {
		return
	}
	history, err := s.configHistory(r.Context(), webHistoryLimit(r), webHistoryOffset(r))
	if err != nil {
		writeWebJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeWebJSON(w, http.StatusOK, webConfigHistoryResponse{Entries: history})
}

func (s metricsSource) handleWebConfigValidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	username, ok := s.authorizeWebWrite(w, r)
	if !ok {
		return
	}
	req, ok := decodeWebConfigEditRequest(w, r)
	if !ok {
		return
	}
	diff, hasChanges, err := s.validateWebConfig(r.Context(), username, req.ConfigText)
	if err != nil {
		writeWebJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeWebJSON(w, http.StatusOK, webConfigValidateResponse{
		Valid:      true,
		HasChanges: hasChanges,
		DiffText:   diff,
	})
}

func (s metricsSource) handleWebConfigCommit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	username, ok := s.authorizeWebWrite(w, r)
	if !ok {
		return
	}
	req, ok := decodeWebConfigCommitRequest(w, r)
	if !ok {
		return
	}
	commitID, version, err := s.commitWebConfig(r.Context(), username, req.ConfigText, req.Message)
	if err != nil {
		writeWebJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeWebJSON(w, http.StatusOK, webConfigCommitResponse{
		CommitID: commitID,
		Version:  version,
	})
}

func (s metricsSource) handleWebIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorizeWebRead(w, r) {
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	status := newWebStatus(s.snapshot(time.Now()))
	cfg, err := s.runningConfig()
	if err != nil {
		http.Error(w, "render config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	history, err := s.configHistory(r.Context(), 5, 0)
	if err != nil {
		http.Error(w, "render history: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := webIndexTemplate.Execute(w, newWebIndexData(status, time.Now(), cfg.ConfigText, history)); err != nil {
		http.Error(w, "render index: "+err.Error(), http.StatusInternalServerError)
	}
}

func (s metricsSource) runningConfig() (webConfig, error) {
	if s.configAPI != nil {
		text, version, err := s.configAPI.GetRunning(context.Background())
		if err != nil {
			return webConfig{}, fmt.Errorf("get running config: %w", err)
		}
		return webConfig{
			ConfigText: text,
			Version:    version,
		}, nil
	}
	if s.engine == nil {
		return webConfig{}, nil
	}
	snap := s.engine.RunningSnapshot()
	if snap == nil || snap.Config == nil {
		return webConfig{}, nil
	}
	text, err := pkgconfig.ToSetCommandsWithError(snap.Config.ToLegacyConfig())
	if err != nil {
		return webConfig{}, fmt.Errorf("serialize running config: %w", err)
	}
	return webConfig{
		ConfigText: text,
		Version:    snap.Version,
	}, nil
}

func (s metricsSource) validateWebConfig(ctx context.Context, username, configText string) (string, bool, error) {
	api := s.configAPI
	if api == nil {
		return "", false, fmt.Errorf("configuration API is unavailable")
	}
	if strings.TrimSpace(configText) == "" {
		return "", false, fmt.Errorf("config_text is required")
	}
	sessionID, err := api.CreateSession(ctx, username)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = api.CloseSession(context.Background(), sessionID) }()
	if err := api.AcquireLock(ctx, sessionID, username); err != nil {
		return "", false, err
	}
	defer func() { _ = api.ReleaseLock(context.Background(), sessionID) }()
	if err := api.EditCandidate(ctx, sessionID, configText); err != nil {
		return "", false, err
	}
	if err := api.ValidateCandidate(ctx, sessionID); err != nil {
		return "", false, err
	}
	return api.Diff(ctx, sessionID)
}

func (s metricsSource) commitWebConfig(ctx context.Context, username, configText, message string) (string, uint64, error) {
	api := s.configAPI
	if api == nil {
		return "", 0, fmt.Errorf("configuration API is unavailable")
	}
	if strings.TrimSpace(configText) == "" {
		return "", 0, fmt.Errorf("config_text is required")
	}
	if strings.TrimSpace(message) == "" {
		message = "web config commit"
	}
	sessionID, err := api.CreateSession(ctx, username)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = api.CloseSession(context.Background(), sessionID) }()
	if err := api.AcquireLock(ctx, sessionID, username); err != nil {
		return "", 0, err
	}
	defer func() { _ = api.ReleaseLock(context.Background(), sessionID) }()
	if err := api.EditCandidate(ctx, sessionID, configText); err != nil {
		return "", 0, err
	}
	return api.Commit(ctx, sessionID, username, message)
}

func (s metricsSource) configHistory(ctx context.Context, limit, offset int) ([]webCommitEntry, error) {
	if s.configAPI == nil {
		return nil, nil
	}
	entries, err := s.configAPI.ListHistory(ctx, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list config history: %w", err)
	}
	history := make([]webCommitEntry, 0, len(entries))
	for _, entry := range entries {
		history = append(history, newWebCommitEntry(entry))
	}
	return history, nil
}

func (s metricsSource) authorizeWebRead(w http.ResponseWriter, r *http.Request) bool {
	users := s.webAuthUsers()
	if len(users) == 0 {
		return true
	}
	_, role, ok := authenticateWebUser(w, r, users)
	if !ok {
		return false
	}
	if !webRoleCanRead(role) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func (s metricsSource) authorizeWebWrite(w http.ResponseWriter, r *http.Request) (string, bool) {
	users := s.webAuthUsers()
	if len(users) == 0 {
		http.Error(w, "web configuration writes require password-backed security users", http.StatusForbidden)
		return "", false
	}
	username, role, ok := authenticateWebUser(w, r, users)
	if !ok {
		return "", false
	}
	if !webRoleCanWrite(role) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return "", false
	}
	return username, true
}

func authenticateWebUser(w http.ResponseWriter, r *http.Request, users map[string]webAuthUser) (string, string, bool) {
	username, password, ok := r.BasicAuth()
	if !ok {
		writeWebAuthChallenge(w)
		return "", "", false
	}

	user, found := users[username]
	passwordHash := webDummyPasswordHash
	if found {
		passwordHash = user.PasswordHash
	}
	valid, err := auth.VerifyPassword(password, passwordHash)
	if err != nil || !found || !valid {
		writeWebAuthChallenge(w)
		return "", "", false
	}
	return username, user.Role, true
}

func (s metricsSource) webAuthUsers() map[string]webAuthUser {
	if s.engine == nil {
		return nil
	}
	snap := s.engine.RunningSnapshot()
	if snap == nil || snap.Config == nil || snap.Config.Security == nil {
		return nil
	}
	users := make(map[string]webAuthUser, len(snap.Config.Security.Users))
	for username, user := range snap.Config.Security.Users {
		if user == nil || user.Password == "" {
			continue
		}
		role := strings.TrimSpace(user.Role)
		if role == "" {
			role = pkgnetconf.RoleReadOnly
		}
		users[username] = webAuthUser{
			PasswordHash: user.Password,
			Role:         role,
		}
	}
	if len(users) == 0 {
		return nil
	}
	return users
}

func webRoleCanRead(role string) bool {
	switch role {
	case pkgnetconf.RoleReadOnly, pkgnetconf.RoleOperator, pkgnetconf.RoleAdmin:
		return true
	default:
		return false
	}
}

func webRoleCanWrite(role string) bool {
	switch role {
	case pkgnetconf.RoleOperator, pkgnetconf.RoleAdmin:
		return true
	default:
		return false
	}
}

func writeWebAuthChallenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", webAuthRealm)
	http.Error(w, "authentication required", http.StatusUnauthorized)
}

func decodeWebConfigEditRequest(w http.ResponseWriter, r *http.Request) (webConfigEditRequest, bool) {
	var req webConfigEditRequest
	if !decodeWebJSONRequest(w, r, &req) {
		return req, false
	}
	if strings.TrimSpace(req.ConfigText) == "" {
		writeWebJSONError(w, http.StatusBadRequest, "config_text is required")
		return req, false
	}
	return req, true
}

func decodeWebConfigCommitRequest(w http.ResponseWriter, r *http.Request) (webConfigCommitRequest, bool) {
	var req webConfigCommitRequest
	if !decodeWebJSONRequest(w, r, &req) {
		return req, false
	}
	if strings.TrimSpace(req.ConfigText) == "" {
		writeWebJSONError(w, http.StatusBadRequest, "config_text is required")
		return req, false
	}
	return req, true
}

func decodeWebJSONRequest(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, webConfigEditBodyLimit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeWebJSONError(w, http.StatusBadRequest, "decode request: "+err.Error())
		return false
	}
	return true
}

func writeWebJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeWebJSONError(w http.ResponseWriter, status int, message string) {
	writeWebJSON(w, status, map[string]string{"error": message})
}

func webHistoryLimit(r *http.Request) int {
	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func webHistoryOffset(r *http.Request) int {
	raw := strings.TrimSpace(r.URL.Query().Get("offset"))
	if raw == "" {
		return 0
	}
	offset, err := strconv.Atoi(raw)
	if err != nil || offset < 0 {
		return 0
	}
	return offset
}

func newWebCommitEntry(entry nbgrpc.CommitInfo) webCommitEntry {
	message := entry.Message
	if strings.TrimSpace(message) == "" {
		message = "(no message)"
	}
	return webCommitEntry{
		CommitID:      entry.CommitID,
		ShortCommitID: shortCommitID(entry.CommitID),
		User:          entry.User,
		Timestamp:     formatWebCommitTime(entry.Timestamp),
		Message:       message,
		IsRollback:    entry.IsRollback,
	}
}

func shortCommitID(commitID string) string {
	if len(commitID) <= 12 {
		return commitID
	}
	return commitID[:12]
}

func formatWebCommitTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339)
}

func formatWebOptionalTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339)
}

func formatWebOptionalDisplayTime(value string) string {
	if value == "" {
		return "Never"
	}
	return value
}

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
	return nmsTelemetryCatalogResponse{
		SchemaVersion:      nmsTelemetryCatalogSchemaVersion,
		GeneratedAt:        formatWebOptionalTime(now),
		Resource:           "/api/nms/v1/telemetry/paths",
		EventSchemaVersion: catalog.EventSchemaVersion,
		Encoding:           catalog.Encoding,
		DefaultPaths:       catalog.DefaultPaths,
		Paths:              paths,
	}
}

func nmsTelemetryCatalogFiltersFromRequest(r *http.Request) nmsTelemetryCatalogFilters {
	query := r.URL.Query()
	return nmsTelemetryCatalogFilters{
		paths:          append([]string(nil), query["path"]...),
		cardinalities:  append([]string(nil), query["cardinality"]...),
		payloadSchemas: append(append([]string(nil), query["payload_schema"]...), query["payload-schema"]...),
		defaultOnly:    nmsTelemetryCatalogDefaultOnlyFromQuery(query),
	}
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
	return opts, nil
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
	responseEvents := make([]nmsTelemetrySnapshotEvent, 0, len(events))
	paths := make([]string, 0, len(events))
	for _, event := range events {
		responseEvents = append(responseEvents, newNMSTelemetrySnapshotEvent(event))
		paths = append(paths, event.Path)
	}
	return nmsTelemetrySnapshotResponse{
		SchemaVersion:      nmsTelemetrySnapshotSchemaVersion,
		GeneratedAt:        formatWebOptionalTime(now),
		Resource:           "/api/nms/v1/telemetry/snapshot",
		EventSchemaVersion: nbgrpc.TelemetryEventSchemaVersion(),
		Encoding:           nbgrpc.TelemetryEncoding(),
		Paths:              paths,
		PayloadBytes:       payloadBytes,
		MaxPayloadBytes:    opts.maxPayloadBytes,
		TimeoutMs:          opts.timeout.Milliseconds(),
		Events:             responseEvents,
	}
}

func newNMSTelemetrySnapshotEvent(event nbgrpc.TelemetryEvent) nmsTelemetrySnapshotEvent {
	output := nmsTelemetrySnapshotEvent{
		Sequence:      event.Sequence,
		Path:          event.Path,
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

func newWebStatus(metrics routerMetrics) webStatus {
	return webStatus{
		Version:         Version,
		Commit:          Commit,
		BuildDate:       BuildDate,
		UptimeSeconds:   metrics.UptimeSeconds,
		ConfigVersion:   metrics.ConfigVersion,
		RunningHostname: metrics.RunningHostname,
		Datastore: webDatastore{
			Backend:       metrics.DatastoreBackend,
			EtcdEndpoints: metrics.DatastoreEtcdEndpoints,
		},
		ConfigSync: webConfigSync{
			Enabled:         metrics.ConfigSyncEnabled,
			Healthy:         metrics.ConfigSyncHealthy,
			EtcdRevision:    metrics.ConfigSyncEtcdRevision,
			RunningRevision: metrics.ConfigSyncRunningRevision,
			RunningCommitID: metrics.ConfigSyncCommitID,
			LastCheck:       formatWebOptionalTime(metrics.ConfigSyncLastCheck),
			LastApply:       formatWebOptionalTime(metrics.ConfigSyncLastApply),
			LastError:       metrics.ConfigSyncLastError,
		},
		Cluster: webCluster{
			Enabled:            metrics.ClusterEnabled,
			NodeCount:          metrics.ClusterNodeCount,
			EtcdSyncConfigured: metrics.ClusterEtcdSync,
			EtcdEndpoints:      metrics.ClusterEtcdEndpoints,
			SyncAligned:        metrics.ClusterSyncAligned,
		},
		Overlay: webOverlayStats{
			EVPN: webEVPNStats{
				Configured:    metrics.OverlayEVPNConfigured,
				VNIs:          metrics.OverlayEVPNVNIs,
				L2VNIs:        metrics.OverlayEVPNL2VNIs,
				L3VNIs:        metrics.OverlayEVPNL3VNIs,
				MulticastVNIs: metrics.OverlayEVPNMulticastVNIs,
			},
		},
		HA: webHAStats{
			Configured: metrics.HAConfigured,
			Converged:  metrics.HAConverged,
			VRRPGroups: metrics.HAVRPGroups,
			IssueCount: len(metrics.HAIssues),
			Issues:     append([]string(nil), metrics.HAIssues...),
		},
		ClassOfService: webCoSStats{
			Configured:             metrics.ClassOfServiceConfigured,
			EnforcementStatus:      metrics.ClassOfServiceStatus,
			ForwardingClasses:      metrics.ClassOfServiceClasses,
			TrafficControlProfiles: metrics.ClassOfServiceProfiles,
			InterfaceBindings:      metrics.ClassOfServiceBindings,
			IntentOnly:             metrics.ClassOfServiceIntentOnly,
			Capabilities: webCoSCapabilities{
				LastCheck:                formatWebOptionalTime(metrics.ClassOfServiceCapabilityLastCheck),
				MetadataBindingSupported: metrics.ClassOfServiceMetadataBindingSupported,
				QueueSchedulerSupported:  metrics.ClassOfServiceQueueSchedulerSupported,
				PolicerSupported:         metrics.ClassOfServicePolicerSupported,
				CountersSupported:        metrics.ClassOfServiceCountersSupported,
				Diagnostics:              append([]string(nil), metrics.ClassOfServiceCapabilityDiagnostics...),
				LastError:                metrics.ClassOfServiceCapabilityError,
			},
		},
		FRR: webFRRStats{
			VRRP: webVRRPStats{
				LastCheck:        formatWebOptionalTime(metrics.FRRVRRPLastRun),
				ConfiguredGroups: metrics.FRRVRRPConfiguredGroups,
				ObservedGroups:   metrics.FRRVRRPObservedGroups,
				ActiveGroups:     metrics.FRRVRRPActiveGroups,
				Groups:           webVRRPGroups(metrics.FRRVRRPGroups),
				IssueCount:       len(metrics.FRRVRRPIssues),
				Issues:           append([]string(nil), metrics.FRRVRRPIssues...),
				LastError:        metrics.FRRVRRPError,
			},
			BFD: webBFDStats{
				LastCheck:         formatWebOptionalTime(metrics.FRRBFDLastRun),
				ConfiguredPeers:   metrics.FRRBFDConfiguredPeers,
				ObservedPeers:     metrics.FRRBFDObservedPeers,
				UpPeers:           metrics.FRRBFDUpPeers,
				DownPeers:         metrics.FRRBFDDownPeers,
				SessionDownEvents: metrics.FRRBFDSessionDownEvents,
				RxFailPackets:     metrics.FRRBFDRxFailPackets,
				Peers:             webBFDPeers(metrics.FRRBFDPeers),
				IssueCount:        len(metrics.FRRBFDIssues),
				Issues:            append([]string(nil), metrics.FRRBFDIssues...),
				LastError:         metrics.FRRBFDError,
			},
		},
		VPP: webVPPStats{
			LCP: webLCPSyncStats{
				LastReconcile:      formatWebOptionalTime(metrics.VPPLCPReconcileLastRun),
				PairCount:          metrics.VPPLCPPairs,
				InconsistencyCount: len(metrics.VPPLCPInconsistencies),
				Inconsistencies:    metrics.VPPLCPInconsistencies,
				LastError:          metrics.VPPLCPReconcileError,
			},
		},
		NETCONF: webNETCONFStats{
			Listening:         metrics.NETCONFListening,
			ActiveSessions:    metrics.NETCONFActiveSessions,
			ActiveConnections: metrics.NETCONFActiveConns,
			TotalConnections:  metrics.NETCONFTotalConns,
			SuccessfulAuth:    metrics.NETCONFSuccess,
			FailedAuth:        metrics.NETCONFFailures,
		},
	}
}

func webVRRPGroups(groups []sbfrr.VRRPGroupOperationalStatus) []webVRRPGroupStats {
	result := make([]webVRRPGroupStats, 0, len(groups))
	for _, group := range groups {
		result = append(result, webVRRPGroupStats{
			Interface:      group.Interface,
			ID:             group.ID,
			VirtualAddress: group.VirtualAddress,
			State:          group.State,
			Observed:       group.Observed,
			Active:         group.Active,
		})
	}
	return result
}

func webBFDPeers(peers []sbfrr.BFDPeerOperationalStatus) []webBFDPeerStats {
	result := make([]webBFDPeerStats, 0, len(peers))
	for _, peer := range peers {
		result = append(result, webBFDPeerStats{
			Peer:              peer.Peer,
			LocalAddress:      peer.LocalAddress,
			Interface:         peer.Interface,
			VRF:               peer.VRF,
			Status:            peer.Status,
			Diagnostic:        peer.Diagnostic,
			RemoteDiagnostic:  peer.RemoteDiagnostic,
			Observed:          peer.Observed,
			Up:                peer.Up,
			SessionDownEvents: peer.SessionDownEvents,
			RxFailPackets:     peer.RxFailPackets,
		})
	}
	return result
}

func newWebIndexData(status webStatus, now time.Time, runningConfig string, history []webCommitEntry) webIndexData {
	state := "Stopped"
	stateClass := "warn"
	if status.NETCONF.Listening {
		state = "Listening"
		stateClass = "ok"
	}
	clusterState := "Disabled"
	clusterStateClass := "neutral"
	if status.Cluster.Enabled {
		clusterState = "Enabled"
		clusterStateClass = "ok"
	}
	clusterSyncState := "Not configured"
	clusterSyncAlignment := "Not applicable"
	if status.Cluster.EtcdSyncConfigured {
		clusterSyncState = "etcd"
		clusterSyncAlignment = "Aligned"
		if !status.Cluster.SyncAligned {
			clusterSyncAlignment = "Mismatch"
		}
	}
	configSyncState := "Disabled"
	configSyncStateClass := "neutral"
	if status.ConfigSync.Enabled {
		configSyncState = "Healthy"
		configSyncStateClass = "ok"
		if status.ConfigSync.LastError != "" {
			configSyncState = "Error"
			configSyncStateClass = "warn"
		} else if !status.ConfigSync.Healthy {
			configSyncState = "Unknown"
			configSyncStateClass = "neutral"
		}
	}
	configSyncRevision := "n/a"
	if status.ConfigSync.RunningRevision > 0 {
		configSyncRevision = strconv.FormatInt(status.ConfigSync.RunningRevision, 10)
	}
	haState := "Not configured"
	haStateClass := "neutral"
	if status.HA.Configured {
		haState = "Converged"
		haStateClass = "ok"
		if !status.HA.Converged {
			haState = "Issues"
			haStateClass = "warn"
		}
	}
	cosState := "Not configured"
	cosStateClass := "neutral"
	if status.ClassOfService.Configured {
		cosState = status.ClassOfService.EnforcementStatus
		cosStateClass = "ok"
		if status.ClassOfService.IntentOnly {
			cosStateClass = "neutral"
		}
	}
	frrVRRPState := "Not configured"
	frrVRRPStateClass := "neutral"
	if status.FRR.VRRP.ConfiguredGroups > 0 {
		frrVRRPState = "Converged"
		frrVRRPStateClass = "ok"
		if status.FRR.VRRP.LastError != "" || status.FRR.VRRP.IssueCount > 0 ||
			status.FRR.VRRP.ActiveGroups < status.FRR.VRRP.ConfiguredGroups {
			frrVRRPState = "Issues"
			frrVRRPStateClass = "warn"
		} else if status.FRR.VRRP.LastCheck == "" {
			frrVRRPState = "Unknown"
			frrVRRPStateClass = "neutral"
		}
	}
	frrBFDState := "Not configured"
	frrBFDStateClass := "neutral"
	if status.FRR.BFD.ConfiguredPeers > 0 || status.FRR.BFD.ObservedPeers > 0 {
		frrBFDState = "Converged"
		frrBFDStateClass = "ok"
		if status.FRR.BFD.LastError != "" || status.FRR.BFD.IssueCount > 0 ||
			status.FRR.BFD.DownPeers > 0 || status.FRR.BFD.UpPeers < status.FRR.BFD.ConfiguredPeers {
			frrBFDState = "Issues"
			frrBFDStateClass = "warn"
		} else if status.FRR.BFD.LastCheck == "" {
			frrBFDState = "Unknown"
			frrBFDStateClass = "neutral"
		}
	}
	vppLCPState := "Consistent"
	vppLCPStateClass := "ok"
	if status.VPP.LCP.LastError != "" {
		vppLCPState = "Check failed"
		vppLCPStateClass = "warn"
	} else if status.VPP.LCP.InconsistencyCount > 0 {
		vppLCPState = "Mismatch"
		vppLCPStateClass = "warn"
	} else if status.VPP.LCP.LastReconcile == "" {
		vppLCPState = "Unknown"
		vppLCPStateClass = "neutral"
	}

	return webIndexData{
		Status:                   status,
		Uptime:                   formatWebUptime(status.UptimeSeconds),
		NETCONFState:             state,
		NETCONFStateClass:        stateClass,
		NETCONFConnections:       strconv.FormatUint(status.NETCONF.TotalConnections, 10),
		ClusterState:             clusterState,
		ClusterStateClass:        clusterStateClass,
		ClusterSyncState:         clusterSyncState,
		ClusterSyncAlignment:     clusterSyncAlignment,
		ClusterNodeCount:         strconv.Itoa(status.Cluster.NodeCount),
		ConfigSyncState:          configSyncState,
		ConfigSyncStateClass:     configSyncStateClass,
		ConfigSyncRevision:       configSyncRevision,
		ConfigSyncLastApply:      formatWebOptionalDisplayTime(status.ConfigSync.LastApply),
		HAState:                  haState,
		HAStateClass:             haStateClass,
		HAVRPGroups:              strconv.Itoa(status.HA.VRRPGroups),
		HAIssues:                 strconv.Itoa(status.HA.IssueCount),
		ClassOfServiceState:      cosState,
		ClassOfServiceClass:      cosStateClass,
		ClassOfServiceProfiles:   strconv.Itoa(status.ClassOfService.TrafficControlProfiles),
		ClassOfServiceBindings:   strconv.Itoa(status.ClassOfService.InterfaceBindings),
		ClassOfServiceClasses:    strconv.Itoa(status.ClassOfService.ForwardingClasses),
		ClassOfServiceScheduler:  webSupportedStatus(status.ClassOfService.Capabilities.QueueSchedulerSupported),
		ClassOfServicePolicer:    webSupportedStatus(status.ClassOfService.Capabilities.PolicerSupported),
		ClassOfServiceCounters:   webSupportedStatus(status.ClassOfService.Capabilities.CountersSupported),
		ClassOfServiceDiagnostic: webCoSDiagnosticText(status.ClassOfService.Capabilities),
		FRRVRRPState:             frrVRRPState,
		FRRVRRPStateClass:        frrVRRPStateClass,
		FRRVRRPActiveGroups:      fmt.Sprintf("%d/%d", status.FRR.VRRP.ActiveGroups, status.FRR.VRRP.ConfiguredGroups),
		FRRVRRPGroups:            webVRRPGroupViews(status.FRR.VRRP.Groups),
		FRRBFDState:              frrBFDState,
		FRRBFDStateClass:         frrBFDStateClass,
		FRRBFDUpPeers:            webBFDPeerRatio(status.FRR.BFD),
		FRRBFDSessionDownEvents:  strconv.Itoa(status.FRR.BFD.SessionDownEvents),
		FRRBFDRxFailPackets:      strconv.Itoa(status.FRR.BFD.RxFailPackets),
		FRRBFDPeers:              webBFDPeerViews(status.FRR.BFD.Peers),
		VPPLCPState:              vppLCPState,
		VPPLCPStateClass:         vppLCPStateClass,
		VPPLCPPairs:              strconv.Itoa(status.VPP.LCP.PairCount),
		VPPLCPInconsistencies:    strconv.Itoa(status.VPP.LCP.InconsistencyCount),
		VPPLCPLastReconcile:      formatWebOptionalDisplayTime(status.VPP.LCP.LastReconcile),
		DatastoreBackend:         status.Datastore.Backend,
		GeneratedAt:              now.UTC().Format(time.RFC3339),
		ConfigVersionString:      strconv.FormatUint(status.ConfigVersion, 10),
		RunningConfig:            runningConfig,
		History:                  history,
	}
}

func webVRRPGroupViews(groups []webVRRPGroupStats) []webVRRPGroupView {
	result := make([]webVRRPGroupView, 0, len(groups))
	for _, group := range groups {
		state := group.State
		if state == "" {
			state = "unknown"
		}
		result = append(result, webVRRPGroupView{
			Label:      fmt.Sprintf("%s vrid %d", group.Interface, group.ID),
			State:      state,
			StateClass: webVRRPGroupStateClass(group),
		})
	}
	return result
}

func webVRRPGroupStateClass(group webVRRPGroupStats) string {
	if group.Active {
		return "ok"
	}
	return "warn"
}

func webBFDPeerViews(peers []webBFDPeerStats) []webBFDPeerView {
	result := make([]webBFDPeerView, 0, len(peers))
	for _, peer := range peers {
		state := peer.Status
		if state == "" {
			state = "unknown"
		}
		result = append(result, webBFDPeerView{
			Label:      webBFDPeerLabel(peer),
			State:      state,
			StateClass: webBFDPeerStateClass(peer),
			Counters:   webBFDCounterText(peer),
		})
	}
	return result
}

func webBFDPeerRatio(status webBFDStats) string {
	total := status.ConfiguredPeers
	if total == 0 {
		total = status.ObservedPeers
	}
	return fmt.Sprintf("%d/%d", status.UpPeers, total)
}

func webBFDPeerLabel(peer webBFDPeerStats) string {
	parts := []string{"bfd", peer.Peer}
	if peer.Interface != "" {
		parts = append(parts, peer.Interface)
	}
	if peer.VRF != "" {
		parts = append(parts, "vrf "+peer.VRF)
	}
	return strings.Join(parts, " ")
}

func webBFDPeerStateClass(peer webBFDPeerStats) string {
	if peer.Up {
		return "ok"
	}
	return "warn"
}

func webBFDCounterText(peer webBFDPeerStats) string {
	if peer.SessionDownEvents == 0 && peer.RxFailPackets == 0 {
		return ""
	}
	return fmt.Sprintf("down %d / rx-fail %d", peer.SessionDownEvents, peer.RxFailPackets)
}

func formatWebUptime(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	duration := time.Duration(seconds) * time.Second
	days := duration / (24 * time.Hour)
	duration -= days * 24 * time.Hour
	hours := duration / time.Hour
	duration -= hours * time.Hour
	minutes := duration / time.Minute

	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
