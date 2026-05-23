package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/akam1o/arca-router/internal/compat"
	nbgrpc "github.com/akam1o/arca-router/internal/northbound/grpc"
)

const defaultWebUIPort = 8080

const webAuthRealm = `Basic realm="arca-router", charset="UTF-8"`

const webDummyPasswordHash = "$argon2id$v=19$m=65536,t=3,p=4$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

const webConfigEditBodyLimit = 1 << 20
const webAPITokenSHA256Prefix = "sha256:"
const webAPITokenNotAfterPrefix = ":not-after="
const webInternalServerErrorMessage = "internal server error"
const webAPITokenUnavailableMessage = "web API token authentication unavailable"
const webRedactedSecretMarker = "<redacted>"

const nmsOperationalStatusSchemaVersion = "arca.nms.operational.v1"
const nmsTelemetryCatalogSchemaVersion = "arca.nms.telemetry-catalog.v1"
const nmsTelemetrySchemasSchemaVersion = "arca.nms.telemetry-schemas.v1"
const nmsTelemetrySnapshotSchemaVersion = "arca.nms.telemetry-snapshot.v1"
const webAuditSchemaVersion = compat.AuditSchema

const (
	defaultNMSTelemetrySnapshotTimeout         = 5 * time.Second
	maxNMSTelemetrySnapshotTimeout             = 30 * time.Second
	defaultNMSTelemetrySnapshotMaxPayloadBytes = 8 << 20
	maxNMSTelemetrySnapshotMaxPayloadBytes     = 64 << 20
	defaultNMSTelemetrySnapshotMaxEvents       = 64
	maxNMSTelemetrySnapshotMaxEvents           = 1024
)

var (
	errNMSTelemetrySnapshotTooLarge      = errors.New("nms telemetry snapshot payload budget exceeded")
	errNMSTelemetrySnapshotTooManyEvents = errors.New("nms telemetry snapshot event budget exceeded")
	errWebConfigAPIUnavailable           = errors.New("configuration API is unavailable")
)

type webConfigAPI interface {
	GetRunning(ctx context.Context) (string, uint64, error)
	CreateSession(ctx context.Context, user string) (string, error)
	CloseSession(ctx context.Context, sessionID string) error
	AcquireLock(ctx context.Context, sessionID, user string) error
	ReleaseLock(ctx context.Context, sessionID string) error
	EditCandidate(ctx context.Context, sessionID, configText string) error
	ReplaceCandidate(ctx context.Context, sessionID, configText string) error
	ValidateCandidate(ctx context.Context, sessionID string) error
	Diff(ctx context.Context, sessionID string) (string, bool, error)
	Commit(ctx context.Context, sessionID, user, message string) (string, uint64, error)
	ListHistory(ctx context.Context, limit, offset int) ([]nbgrpc.CommitInfo, error)
	ListAuditEvents(ctx context.Context, opts nbgrpc.AuditLogOptions) ([]nbgrpc.AuditEventInfo, error)
}

type webUnredactedConfigAPI interface {
	GetRunningUnredacted(ctx context.Context) (string, uint64, error)
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
	SchemaVersion           string             `json:"schema_version"`
	GeneratedAt             string             `json:"generated_at"`
	Resource                string             `json:"resource"`
	EventSchemaVersion      string             `json:"event_schema_version"`
	Encoding                string             `json:"encoding"`
	DefaultPaths            []string           `json:"default_paths"`
	DefaultSampleIntervalMs uint32             `json:"default_sample_interval_ms"`
	MinSampleIntervalMs     uint32             `json:"min_sample_interval_ms"`
	MaxSampleIntervalMs     uint32             `json:"max_sample_interval_ms"`
	PathCount               int                `json:"path_count"`
	Paths                   []nmsTelemetryPath `json:"paths"`
}

type nmsTelemetrySchemasResponse struct {
	SchemaVersion           string                      `json:"schema_version"`
	GeneratedAt             string                      `json:"generated_at"`
	Resource                string                      `json:"resource"`
	EventSchemaVersion      string                      `json:"event_schema_version"`
	Encoding                string                      `json:"encoding"`
	DefaultPaths            []string                    `json:"default_paths"`
	DefaultSampleIntervalMs uint32                      `json:"default_sample_interval_ms"`
	MinSampleIntervalMs     uint32                      `json:"min_sample_interval_ms"`
	MaxSampleIntervalMs     uint32                      `json:"max_sample_interval_ms"`
	SchemaCount             int                         `json:"schema_count"`
	Schemas                 []nmsTelemetryPayloadSchema `json:"schemas"`
}

type nmsTelemetrySnapshotResponse struct {
	SchemaVersion           string                      `json:"schema_version"`
	GeneratedAt             string                      `json:"generated_at"`
	Resource                string                      `json:"resource"`
	EventSchemaVersion      string                      `json:"event_schema_version"`
	Encoding                string                      `json:"encoding"`
	DefaultPaths            []string                    `json:"default_paths"`
	DefaultSampleIntervalMs uint32                      `json:"default_sample_interval_ms"`
	MinSampleIntervalMs     uint32                      `json:"min_sample_interval_ms"`
	MaxSampleIntervalMs     uint32                      `json:"max_sample_interval_ms"`
	Paths                   []string                    `json:"paths"`
	EventCount              int                         `json:"event_count"`
	PayloadBytes            int                         `json:"payload_bytes"`
	MaxPayloadBytes         int                         `json:"max_payload_bytes"`
	MaxEvents               int                         `json:"max_events"`
	TimeoutMs               int64                       `json:"timeout_ms"`
	Events                  []nmsTelemetrySnapshotEvent `json:"events"`
}

type nmsTelemetrySnapshotEvent struct {
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

type nmsTelemetryPath struct {
	Path          string   `json:"path"`
	Description   string   `json:"description"`
	Cardinality   string   `json:"cardinality"`
	PayloadSchema string   `json:"payload_schema"`
	Aliases       []string `json:"aliases,omitempty"`
	Default       bool     `json:"default"`
}

type nmsTelemetryPayloadSchema struct {
	Path          string                     `json:"path"`
	Description   string                     `json:"description"`
	Cardinality   string                     `json:"cardinality"`
	PayloadSchema string                     `json:"payload_schema"`
	Aliases       []string                   `json:"aliases,omitempty"`
	Default       bool                       `json:"default"`
	Fields        []nmsTelemetryPayloadField `json:"fields"`
}

type nmsTelemetryPayloadField struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

type nmsTelemetryCatalogFilters struct {
	paths          []string
	cardinalities  []string
	payloadSchemas []string
	encodings      []string
	defaultOnly    bool
}

type nmsTelemetrySnapshotOptions struct {
	paths           []string
	timeout         time.Duration
	maxPayloadBytes int
	maxEvents       int
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

type webAuditResponse struct {
	SchemaVersion string          `json:"schema_version"`
	GeneratedAt   string          `json:"generated_at"`
	Limit         int             `json:"limit"`
	Offset        int             `json:"offset"`
	Count         int             `json:"count"`
	Entries       []webAuditEntry `json:"entries"`
}

type webAuditEntry struct {
	ID            int64          `json:"id,omitempty"`
	Key           string         `json:"key,omitempty"`
	Timestamp     string         `json:"timestamp"`
	User          string         `json:"user"`
	SessionID     string         `json:"session_id,omitempty"`
	SourceIP      string         `json:"source_ip,omitempty"`
	CorrelationID string         `json:"correlation_id,omitempty"`
	Action        string         `json:"action"`
	Result        string         `json:"result"`
	ErrorCode     string         `json:"error_code,omitempty"`
	Details       map[string]any `json:"details,omitempty"`
	RawDetails    string         `json:"raw_details,omitempty"`
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

type webAPIToken struct {
	Name        string
	Token       string
	TokenSHA256 []byte
	NotAfter    time.Time
	Role        string
}

type webAPITokenCache struct {
	mu       sync.Mutex
	path     string
	fileInfo os.FileInfo
	tokens   map[string]webAPIToken
	loadErr  error
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
