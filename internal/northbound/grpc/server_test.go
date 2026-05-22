package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	apiv1 "github.com/akam1o/arca-router/api/v1"
	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	sbfrr "github.com/akam1o/arca-router/internal/southbound/frr"
	sbvpp "github.com/akam1o/arca-router/internal/southbound/vpp"
	"github.com/akam1o/arca-router/internal/store"
	pkgconfig "github.com/akam1o/arca-router/pkg/config"
	pkgvpp "github.com/akam1o/arca-router/pkg/vpp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestConfigEditStatusErrorClassifiesTypedErrors(t *testing.T) {
	inputErr := configEditStatusError(newConfigInputErrorf("no configuration changes to commit"))
	if status.Code(inputErr) != codes.InvalidArgument {
		t.Fatalf("input status = %v, want %v", status.Code(inputErr), codes.InvalidArgument)
	}
	if got := status.Convert(inputErr).Message(); got != "no configuration changes to commit" {
		t.Fatalf("input status message = %q, want no-changes message", got)
	}

	conflictErr := configEditStatusError(newCandidateConflictErrorf("candidate lock held by session session-1"))
	if status.Code(conflictErr) != codes.FailedPrecondition {
		t.Fatalf("conflict status = %v, want %v", status.Code(conflictErr), codes.FailedPrecondition)
	}
	if got := status.Convert(conflictErr).Message(); got != "configuration candidate is unavailable" {
		t.Fatalf("conflict status message = %q, want generic candidate message", got)
	}

	unavailableErr := configEditStatusError(fmt.Errorf("%w: commit history is unavailable", ErrCommitHistoryUnavailable))
	if status.Code(unavailableErr) != codes.Unavailable {
		t.Fatalf("unavailable status = %v, want %v", status.Code(unavailableErr), codes.Unavailable)
	}
	if got := status.Convert(unavailableErr).Message(); got != "commit history is unavailable" {
		t.Fatalf("unavailable status message = %q, want commit history unavailable", got)
	}

	internalErr := configEditStatusError(errors.New("persist commit /var/lib/arca/config.db failed"))
	if status.Code(internalErr) != codes.Internal {
		t.Fatalf("internal status = %v, want %v", status.Code(internalErr), codes.Internal)
	}
	if got := status.Convert(internalErr).Message(); got != "internal server error" {
		t.Fatalf("internal status message = %q, want generic internal error", got)
	}
}

func TestStateAdapterRedactsOperationalErrors(t *testing.T) {
	oldVtysh := runOperationalVtyshCommand
	runOperationalVtyshCommand = func(ctx context.Context, command string) (string, error) {
		return "", errors.New("open /var/lib/frr/zebra.sock: permission denied")
	}
	t.Cleanup(func() { runOperationalVtyshCommand = oldVtysh })

	srv := NewServer(engine.NewEngine(nil, testLogger()), &fakeStore{}, testLogger())
	adapter := &stateServiceAdapter{server: srv}

	_, err := adapter.GetRoutes(context.Background(), &apiv1.GetRoutesRequest{})
	if status.Code(err) != codes.Internal {
		t.Fatalf("GetRoutes() status = %v, want %v", status.Code(err), codes.Internal)
	}
	if got := status.Convert(err).Message(); got != "internal server error" {
		t.Fatalf("GetRoutes() status message = %q, want generic internal error", got)
	}
	if strings.Contains(err.Error(), "/var/lib/frr/zebra.sock") {
		t.Fatalf("GetRoutes() error leaked backend path: %v", err)
	}

	_, err = adapter.GetRoutes(context.Background(), &apiv1.GetRoutesRequest{PrefixFilter: "not-a-prefix"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("GetRoutes(invalid prefix) status = %v, want %v", status.Code(err), codes.InvalidArgument)
	}
	if got := status.Convert(err).Message(); !strings.Contains(got, "invalid route prefix filter") {
		t.Fatalf("GetRoutes(invalid prefix) message = %q, want input validation detail", got)
	}
}

func TestConfigAdapterRedactsReadAndHistoryErrors(t *testing.T) {
	srv := NewServer(engine.NewEngine(nil, testLogger()), &fakeStore{
		listErr: errors.New("open /var/lib/arca-router/config.db: permission denied"),
	}, testLogger())
	adapter := &configServiceAdapter{server: srv}

	_, err := adapter.GetCandidate(context.Background(), &apiv1.GetCandidateRequest{SessionId: "session-secret"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("GetCandidate() status = %v, want %v", status.Code(err), codes.NotFound)
	}
	if got := status.Convert(err).Message(); got != "configuration session not found" {
		t.Fatalf("GetCandidate() status message = %q, want redacted session message", got)
	}
	if strings.Contains(err.Error(), "session-secret") {
		t.Fatalf("GetCandidate() error leaked session ID: %v", err)
	}

	_, err = adapter.ListHistory(context.Background(), &apiv1.ListHistoryRequest{Limit: 10})
	if status.Code(err) != codes.Internal {
		t.Fatalf("ListHistory() status = %v, want %v", status.Code(err), codes.Internal)
	}
	if got := status.Convert(err).Message(); got != "internal server error" {
		t.Fatalf("ListHistory() status message = %q, want generic internal error", got)
	}
	if strings.Contains(err.Error(), "/var/lib/arca-router/config.db") {
		t.Fatalf("ListHistory() error leaked backend path: %v", err)
	}

	_, err = adapter.ListHistory(context.Background(), &apiv1.ListHistoryRequest{Limit: -1})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ListHistory(invalid limit) status = %v, want %v", status.Code(err), codes.InvalidArgument)
	}
	if got := status.Convert(err).Message(); !strings.Contains(got, "invalid history limit") {
		t.Fatalf("ListHistory(invalid limit) message = %q, want validation detail", got)
	}
}

func TestSessionAdapterRedactsSessionErrors(t *testing.T) {
	srv := NewServer(engine.NewEngine(nil, testLogger()), &fakeStore{}, testLogger())
	adapter := &sessionServiceAdapter{server: srv}

	_, err := adapter.CloseSession(context.Background(), &apiv1.CloseSessionRequest{SessionId: "session-secret"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("CloseSession() status = %v, want %v", status.Code(err), codes.NotFound)
	}
	if got := status.Convert(err).Message(); got != "configuration session not found" {
		t.Fatalf("CloseSession() status message = %q, want redacted session message", got)
	}
	if strings.Contains(err.Error(), "session-secret") {
		t.Fatalf("CloseSession() error leaked session ID: %v", err)
	}

	session1, err := srv.CreateSession(context.Background(), "alice")
	if err != nil {
		t.Fatalf("CreateSession(session1) error = %v", err)
	}
	session2, err := srv.CreateSession(context.Background(), "bob")
	if err != nil {
		t.Fatalf("CreateSession(session2) error = %v", err)
	}
	if err := srv.AcquireLock(context.Background(), session1, "alice"); err != nil {
		t.Fatalf("AcquireLock(session1) error = %v", err)
	}

	_, err = adapter.AcquireLock(context.Background(), &apiv1.AcquireLockRequest{SessionId: session2, User: "bob"})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("AcquireLock(conflict) status = %v, want %v", status.Code(err), codes.FailedPrecondition)
	}
	if got := status.Convert(err).Message(); got != "configuration candidate is unavailable" {
		t.Fatalf("AcquireLock(conflict) status message = %q, want generic candidate message", got)
	}
	if strings.Contains(err.Error(), session1) {
		t.Fatalf("AcquireLock(conflict) error leaked lock owner session: %v", err)
	}
}

func listenUnix(path string) (net.Listener, error) {
	return net.Listen("unix", path)
}

type fakeStore struct {
	commitID     string
	prepareErr   error
	prepareFn    func()
	commitErr    error
	getCommitErr error
	saved        *model.ConfigSnapshot
	aborted      bool
	commits      map[string]*store.CommitRecord
	listRecords  []*store.CommitRecord
	listErr      error
	listCalls    int
	listOpts     *store.ListOptions
	auditOpts    *store.AuditOptions
}

type fakeInterfaceStateCollector struct {
	states map[string]*model.InterfaceState
	err    error
	calls  int
}

func (f *fakeInterfaceStateCollector) CollectState(ctx context.Context) (map[string]*model.InterfaceState, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.states, nil
}

type fakeLCPReconciliationSource struct {
	info LCPReconciliationInfo
}

func (f fakeLCPReconciliationSource) LCPReconciliationInfo() LCPReconciliationInfo {
	return f.info
}

type fakeHAStatusSource struct {
	info HAStatusInfo
}

func (f fakeHAStatusSource) HAStatusInfo() HAStatusInfo {
	return f.info
}

type fakeBFDOperationalSource struct {
	status sbfrr.BFDOperationalStatus
}

func (f fakeBFDOperationalSource) BFDOperationalStatus() sbfrr.BFDOperationalStatus {
	return f.status
}

type fakeQoSCapabilitySource struct {
	status sbvpp.QoSCapabilityStatus
}

func (f fakeQoSCapabilitySource) QoSCapabilityStatus() sbvpp.QoSCapabilityStatus {
	return f.status
}

func (f *fakeStore) GetLatestSnapshot(ctx context.Context) (*model.ConfigSnapshot, error) {
	return f.saved, nil
}

func (f *fakeStore) SaveCommit(ctx context.Context, snap *model.ConfigSnapshot) (string, error) {
	prepared, err := f.PrepareCommit(ctx, snap)
	if err != nil {
		return "", err
	}
	return prepared.Commit(ctx)
}

func (f *fakeStore) PrepareCommit(ctx context.Context, snap *model.ConfigSnapshot) (store.PreparedCommit, error) {
	if f.prepareErr != nil {
		return nil, f.prepareErr
	}
	f.saved = snap
	if f.prepareFn != nil {
		f.prepareFn()
	}
	return &fakePreparedCommit{store: f}, nil
}

func (f *fakeStore) GetCommit(ctx context.Context, commitID string) (*store.CommitRecord, error) {
	if f.getCommitErr != nil {
		return nil, f.getCommitErr
	}
	return f.commits[commitID], nil
}

func (f *fakeStore) ListCommits(ctx context.Context, opts *store.ListOptions) ([]*store.CommitRecord, error) {
	f.listCalls++
	f.listOpts = opts
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listRecords, nil
}

func (f *fakeStore) AuditLog(ctx context.Context, event *store.AuditEvent) error {
	return nil
}

func (f *fakeStore) ListAuditEvents(ctx context.Context, opts *store.AuditOptions) ([]*store.AuditEvent, error) {
	f.auditOpts = opts
	return nil, nil
}

func (f *fakeStore) Close() error {
	return nil
}

type fakePreparedCommit struct {
	store *fakeStore
}

func (p *fakePreparedCommit) Commit(ctx context.Context) (string, error) {
	if p.store.commitErr != nil {
		return "", p.store.commitErr
	}
	return p.store.commitID, nil
}

func (p *fakePreparedCommit) Abort(ctx context.Context) error {
	p.store.aborted = true
	return nil
}

func TestClientServerConfigFlow(t *testing.T) {
	oldParser := ConfigTextParser
	ConfigTextParser = func(text string) (*model.RouterConfig, error) {
		cfg, err := pkgconfig.NewParser(strings.NewReader(text)).Parse()
		if err != nil {
			return nil, err
		}
		return model.FromLegacyConfig(cfg), nil
	}
	t.Cleanup(func() { ConfigTextParser = oldParser })

	eng := engine.NewEngine(nil, testLogger())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 1)

	socketPath := t.TempDir() + "/routerd.sock"
	lis, err := listenUnix(socketPath)
	if err != nil {
		t.Fatalf("listenUnix() error = %v", err)
	}

	srv := NewServer(eng, &fakeStore{commitID: "commit-1"}, testLogger())
	srv.SetQoSCapabilitySource(fakeQoSCapabilitySource{status: sbvpp.QoSCapabilityStatus{
		LastCheck: time.Unix(1700000500, 0),
		Capabilities: pkgvpp.QoSCapabilities{
			MetadataBinding:     true,
			QueueScheduler:      true,
			Policer:             false,
			OperationalCounters: true,
			Diagnostics:         []string{"scheduler api available"},
		},
	}})
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(lis)
	}()
	t.Cleanup(func() {
		srv.Stop()
		select {
		case <-errCh:
		case <-time.After(time.Second):
			t.Fatal("server did not stop")
		}
	})

	client, err := Dial(socketPath)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	text, version, err := client.GetRunning(ctx)
	if err != nil {
		t.Fatalf("GetRunning() error = %v", err)
	}
	if version != 1 || !strings.Contains(text, "set system host-name router1") {
		t.Fatalf("GetRunning() = (%q, %d), want router1 version 1", text, version)
	}

	sessionID, err := client.CreateSession(ctx, "alice")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := client.AcquireLock(ctx, sessionID, "alice"); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}
	if err := client.EditCandidate(ctx, sessionID, "set system host-name router2"); err != nil {
		t.Fatalf("EditCandidate() error = %v", err)
	}
	candidate, err := client.GetCandidate(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetCandidate() error = %v", err)
	}
	if strings.Contains(candidate, "set system host-name router1") || !strings.Contains(candidate, "set system host-name router2") {
		t.Fatalf("candidate did not replace scalar hostname: %q", candidate)
	}
	if err := client.ReplaceCandidate(ctx, sessionID, "set system host-name router3"); err != nil {
		t.Fatalf("ReplaceCandidate() error = %v", err)
	}
	candidate, err = client.GetCandidate(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetCandidate() after ReplaceCandidate error = %v", err)
	}
	if strings.Contains(candidate, "set system host-name router2") || !strings.Contains(candidate, "set system host-name router3") {
		t.Fatalf("candidate replacement did not replace previous edit: %q", candidate)
	}
	if err := client.ValidateCandidate(ctx, sessionID); err != nil {
		t.Fatalf("ValidateCandidate() error = %v", err)
	}

	commitID, version, err := client.Commit(ctx, sessionID, "alice", "test")
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if commitID != "commit-1" || version != 2 {
		t.Fatalf("Commit() = (%q, %d), want commit-1 version 2", commitID, version)
	}
	cosInfo, err := client.GetClassOfService(ctx)
	if err != nil {
		t.Fatalf("GetClassOfService() error = %v", err)
	}
	if cosInfo.Capabilities == nil || !cosInfo.Capabilities.MetadataBindingSupported ||
		!cosInfo.Capabilities.QueueSchedulerSupported || !cosInfo.Capabilities.CountersSupported ||
		cosInfo.Capabilities.PolicerSupported || cosInfo.Capabilities.LastCheck.Unix() != 1700000500 ||
		len(cosInfo.Capabilities.Diagnostics) != 1 {
		t.Fatalf("GetClassOfService() capabilities = %#v, want VPP QoS capability diagnostics", cosInfo.Capabilities)
	}
	diffText, hasChanges, err := client.Diff(ctx, sessionID)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if hasChanges {
		t.Fatalf("Diff() has changes after commit: %q", diffText)
	}

	catalog, err := client.GetTelemetryCatalog(ctx)
	if err != nil {
		t.Fatalf("GetTelemetryCatalog() error = %v", err)
	}
	if catalog.EventSchemaVersion != telemetrySchemaVersion || catalog.Encoding != telemetryEncodingJSON {
		t.Fatalf("telemetry catalog schema/encoding = %q/%q, want %q/%q",
			catalog.EventSchemaVersion, catalog.Encoding, telemetrySchemaVersion, telemetryEncodingJSON)
	}
	if len(catalog.DefaultPaths) != len(defaultTelemetryPaths) || catalog.DefaultPaths[0] != "/system" || catalog.DefaultPaths[1] != "/config/running" {
		t.Fatalf("telemetry catalog default paths = %#v, want system/config defaults", catalog.DefaultPaths)
	}
	if catalog.DefaultSampleIntervalMs != telemetrySampleIntervalMillis(defaultTelemetrySampleInterval) ||
		catalog.MinSampleIntervalMs != telemetrySampleIntervalMillis(minTelemetrySampleInterval) ||
		catalog.MaxSampleIntervalMs != telemetrySampleIntervalMillis(maxTelemetrySampleInterval) {
		t.Fatalf("telemetry catalog intervals = %d/%d/%d, want %d/%d/%d",
			catalog.DefaultSampleIntervalMs, catalog.MinSampleIntervalMs, catalog.MaxSampleIntervalMs,
			telemetrySampleIntervalMillis(defaultTelemetrySampleInterval),
			telemetrySampleIntervalMillis(minTelemetrySampleInterval),
			telemetrySampleIntervalMillis(maxTelemetrySampleInterval))
	}
	if len(catalog.Paths) != len(telemetryPathOrder) || catalog.Paths[0].Path != "/system" {
		t.Fatalf("telemetry catalog paths = %#v, want canonical path catalog", catalog.Paths)
	}
	if catalog.Paths[0].PayloadSchema != "arca.telemetry.system.v1" ||
		catalog.Paths[1].PayloadSchema != "arca.telemetry.config.running.v1" {
		t.Fatalf("telemetry catalog payload schemas = %q/%q, want system/config schema hints",
			catalog.Paths[0].PayloadSchema, catalog.Paths[1].PayloadSchema)
	}
	if len(catalog.Paths[1].Aliases) != 2 || catalog.Paths[1].Aliases[0] != "/running" {
		t.Fatalf("telemetry catalog aliases for config/running = %#v, want running aliases", catalog.Paths[1].Aliases)
	}
	filteredCatalog, err := client.GetFilteredTelemetryCatalog(ctx, []string{"per-route"}, []string{"arca.telemetry.routes.v1"})
	if err != nil {
		t.Fatalf("GetFilteredTelemetryCatalog() error = %v", err)
	}
	if len(filteredCatalog.Paths) != 1 || filteredCatalog.Paths[0].Path != "/routes" {
		t.Fatalf("filtered telemetry catalog paths = %#v, want only /routes", filteredCatalog.Paths)
	}
	if len(filteredCatalog.DefaultPaths) != len(defaultTelemetryPaths) {
		t.Fatalf("filtered telemetry catalog default paths = %#v, want unfiltered defaults", filteredCatalog.DefaultPaths)
	}
	filteredCatalog, err = client.GetTelemetryCatalogWithFilter(ctx, TelemetryCatalogFilter{
		Paths:     []string{"/routes"},
		Encodings: []string{"JSON"},
	})
	if err != nil {
		t.Fatalf("GetTelemetryCatalogWithFilter(encoding) error = %v", err)
	}
	if len(filteredCatalog.Paths) != 1 || filteredCatalog.Paths[0].Path != "/routes" {
		t.Fatalf("encoding-filtered telemetry catalog paths = %#v, want only /routes", filteredCatalog.Paths)
	}
	filteredCatalog, err = client.GetTelemetryCatalogWithFilter(ctx, TelemetryCatalogFilter{
		Encodings: []string{"protobuf"},
	})
	if err != nil {
		t.Fatalf("GetTelemetryCatalogWithFilter(unsupported encoding) error = %v", err)
	}
	if len(filteredCatalog.Paths) != 0 {
		t.Fatalf("unsupported encoding telemetry catalog paths = %#v, want none", filteredCatalog.Paths)
	}

	stream, err := client.SubscribeTelemetry(ctx, []string{"/config/running"}, time.Second, true)
	if err != nil {
		t.Fatalf("SubscribeTelemetry() error = %v", err)
	}
	event, err := stream.Recv()
	if err != nil {
		t.Fatalf("TelemetryStream.Recv() error = %v", err)
	}
	if event.Path != "/config/running" || event.EventType != telemetryEventTypeSnapshot ||
		event.Encoding != telemetryEncodingJSON || event.SchemaVersion != telemetrySchemaVersion ||
		event.Cardinality != "single" || event.PayloadSchema != "arca.telemetry.config.running.v1" {
		t.Fatalf("telemetry event = %#v, want config/running JSON snapshot", event)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(event.JSONPayload), &payload); err != nil {
		t.Fatalf("telemetry JSON payload is invalid: %v", err)
	}
	if payload["version"] != float64(2) || !strings.Contains(event.JSONPayload, "router3") {
		t.Fatalf("telemetry payload = %s, want running config version 2 with router3", event.JSONPayload)
	}
	if event.PayloadBytes != len(event.JSONPayload) {
		t.Fatalf("telemetry payload bytes = %d, want %d", event.PayloadBytes, len(event.JSONPayload))
	}
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("TelemetryStream.Recv() after once = %v, want EOF", err)
	}
}

func TestGetRunningRedactsCredentialMaterial(t *testing.T) {
	eng := engine.NewEngine(nil, testLogger())
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
	cfg.Security = &model.SecurityConfig{
		Users: map[string]*model.UserConfig{
			"admin": {
				Password: "plain-password",
				Role:     "admin",
			},
		},
	}
	eng.InitializeRunning(cfg, 1)
	srv := NewServer(eng, nil, testLogger())

	text, version, err := srv.GetRunning(context.Background())
	if err != nil {
		t.Fatalf("GetRunning() error = %v", err)
	}
	if version != 1 || !strings.Contains(text, "set system host-name edge01") {
		t.Fatalf("GetRunning() = (%q, %d), want hostname version 1", text, version)
	}
	for _, secret := range []string{"plain-password", "private-community", "$argon2id$"} {
		if strings.Contains(text, secret) {
			t.Fatalf("GetRunning() leaked %q:\n%s", secret, text)
		}
	}
	if strings.Count(text, "<redacted>") != 2 {
		t.Fatalf("GetRunning() =\n%s\nwant two redacted markers", text)
	}
}

func TestCommitUsesSessionUserForAudit(t *testing.T) {
	oldParser := ConfigTextParser
	ConfigTextParser = func(text string) (*model.RouterConfig, error) {
		cfg, err := pkgconfig.NewParser(strings.NewReader(text)).Parse()
		if err != nil {
			return nil, err
		}
		return model.FromLegacyConfig(cfg), nil
	}
	t.Cleanup(func() { ConfigTextParser = oldParser })

	eng := engine.NewEngine(nil, testLogger())
	eng.InitializeRunning(&model.RouterConfig{
		System: &model.SystemConfig{HostName: "router1"},
	}, 1)
	st := &fakeStore{commitID: "commit-1"}
	srv := NewServer(eng, st, testLogger())
	ctx := context.Background()

	sessionID, err := srv.CreateSession(ctx, "alice")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := srv.AcquireLock(ctx, sessionID, "mallory"); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}
	if err := srv.ReplaceCandidate(ctx, sessionID, "set system host-name router2"); err != nil {
		t.Fatalf("ReplaceCandidate() error = %v", err)
	}
	if _, _, err := srv.Commit(ctx, sessionID, "mallory", "test"); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if st.saved == nil || st.saved.Author != "alice" {
		t.Fatalf("persisted author = %#v, want alice", st.saved)
	}
	if snap := eng.RunningSnapshot(); snap == nil || snap.Author != "alice" {
		t.Fatalf("running snapshot author = %#v, want alice", snap)
	}
}

func TestSubscribeTelemetrySelectedSnapshots(t *testing.T) {
	eng := engine.NewEngine(nil, testLogger())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 7)
	srv := NewServer(eng, &fakeStore{}, testLogger())

	var events []TelemetryEvent
	err := srv.SubscribeTelemetry(context.Background(), []string{"system", "running", "system"}, time.Millisecond, true, func(event TelemetryEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("SubscribeTelemetry() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("SubscribeTelemetry() emitted %d events, want 2", len(events))
	}
	if events[0].Sequence != 1 || events[0].Path != "/system" || events[0].Cardinality != "single" ||
		events[0].PayloadSchema != "arca.telemetry.system.v1" || events[0].EventType != telemetryEventTypeSnapshot {
		t.Fatalf("events[0] = %#v, want system snapshot sequence 1", events[0])
	}
	if events[1].Sequence != 2 || events[1].Path != "/config/running" ||
		events[1].Cardinality != "single" || events[1].PayloadSchema != "arca.telemetry.config.running.v1" ||
		events[1].EventType != telemetryEventTypeSnapshot {
		t.Fatalf("events[1] = %#v, want config snapshot sequence 2", events[1])
	}
	if !strings.Contains(events[0].JSONPayload, "router1") || !strings.Contains(events[1].JSONPayload, "router1") {
		t.Fatalf("telemetry payloads = %#v, want hostname/config content", events)
	}
	if events[0].PayloadBytes != len(events[0].JSONPayload) || events[1].PayloadBytes != len(events[1].JSONPayload) {
		t.Fatalf("payload bytes = %d/%d, want JSON payload lengths", events[0].PayloadBytes, events[1].PayloadBytes)
	}
}

func TestSubscribeTelemetryRouteScaleSnapshot(t *testing.T) {
	srv := NewServer(engine.NewEngine(nil, testLogger()), &fakeStore{}, testLogger())
	const routeCount = 128

	var commands []string
	oldVtysh := runOperationalVtyshCommand
	runOperationalVtyshCommand = func(ctx context.Context, command string) (string, error) {
		commands = append(commands, command)
		switch command {
		case "show ip route json":
			return scaledFRRRouteJSON(routeCount), nil
		case "show ipv6 route json":
			return `{}`, nil
		default:
			t.Fatalf("unexpected vtysh command %q", command)
			return "", nil
		}
	}
	t.Cleanup(func() { runOperationalVtyshCommand = oldVtysh })

	var events []TelemetryEvent
	err := srv.SubscribeTelemetry(context.Background(), []string{"/routes"}, 0, true, func(event TelemetryEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("SubscribeTelemetry(/routes) error = %v", err)
	}
	if len(commands) != 2 || commands[0] != "show ip route json" || commands[1] != "show ipv6 route json" {
		t.Fatalf("vtysh commands = %#v, want IPv4 and IPv6 route JSON", commands)
	}
	if len(events) != 1 || events[0].Sequence != 1 || events[0].Path != "/routes" ||
		events[0].Cardinality != "per-route" || events[0].PayloadSchema != "arca.telemetry.routes.v1" ||
		events[0].EventType != telemetryEventTypeSnapshot {
		t.Fatalf("events = %#v, want one route snapshot", events)
	}
	if events[0].PayloadBytes != len(events[0].JSONPayload) {
		t.Fatalf("payload bytes = %d, want %d", events[0].PayloadBytes, len(events[0].JSONPayload))
	}

	var payload struct {
		Routes []RouteInfo `json:"routes"`
	}
	if err := json.Unmarshal([]byte(events[0].JSONPayload), &payload); err != nil {
		t.Fatalf("route telemetry payload is invalid JSON: %v", err)
	}
	if len(payload.Routes) != routeCount {
		t.Fatalf("route telemetry payload has %d routes, want %d", len(payload.Routes), routeCount)
	}
	got, ok := routeInfoByPrefix(payload.Routes, "10.0.127.0/24")
	if !ok {
		t.Fatalf("route telemetry payload missing highest generated prefix")
	}
	if got.NextHop != "192.0.2.128" || got.Protocol != "bgp" || got.Metric != 127 || got.Interface != "ge0-0-127" || !got.Active {
		t.Fatalf("route telemetry payload route = %#v, want generated BGP route attributes", got)
	}
}

func TestSubscribeTelemetryRedactsPayloadErrors(t *testing.T) {
	oldVtysh := runOperationalVtyshCommand
	runOperationalVtyshCommand = func(ctx context.Context, command string) (string, error) {
		return "", errors.New("open /var/lib/frr/zebra.sock: permission denied")
	}
	t.Cleanup(func() { runOperationalVtyshCommand = oldVtysh })

	srv := NewServer(engine.NewEngine(nil, testLogger()), &fakeStore{}, testLogger())
	var events []TelemetryEvent
	err := srv.SubscribeTelemetry(context.Background(), []string{"/routes"}, 0, true, func(event TelemetryEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("SubscribeTelemetry(/routes) error = %v", err)
	}
	if len(events) != 1 || events[0].EventType != telemetryEventTypeError {
		t.Fatalf("events = %#v, want one redacted error event", events)
	}

	var payload telemetryErrorPayload
	if err := json.Unmarshal([]byte(events[0].JSONPayload), &payload); err != nil {
		t.Fatalf("telemetry error payload is invalid JSON: %v", err)
	}
	if payload.Error != telemetryPayloadErrorMessage {
		t.Fatalf("telemetry error payload = %q, want redacted message", payload.Error)
	}
	if strings.Contains(events[0].JSONPayload, "/var/lib/frr/zebra.sock") {
		t.Fatalf("telemetry error payload leaked backend path: %s", events[0].JSONPayload)
	}
}

func TestNormalizeTelemetryPathsDeduplicatesLargeSelection(t *testing.T) {
	var raw []string
	for i := 0; i < 64; i++ {
		raw = append(raw, "system", "/routes", "evpn", "running", "/routes", "/overlays/evpn")
	}

	paths, err := normalizeTelemetryPaths(raw)
	if err != nil {
		t.Fatalf("normalizeTelemetryPaths() error = %v", err)
	}
	want := []string{"/system", "/config/running", "/routes", "/overlays/evpn"}
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("normalizeTelemetryPaths() = %#v, want %#v", paths, want)
	}
}

func TestSubscribeTelemetryEVPNOverlaySnapshot(t *testing.T) {
	eng := engine.NewEngine(nil, testLogger())
	eng.InitializeRunning(&model.RouterConfig{
		Protocols: &model.ProtocolsConfig{EVPN: &model.EVPNConfig{VNIs: map[int]*model.EVPNVNI{
			20010: {
				VNI:             20010,
				Type:            "l3",
				RoutingInstance: "BLUE",
				VRFTargetImport: []string{"target:65000:20010"},
				RemoteVTEP:      "198.51.100.20",
			},
			10010: {
				VNI:                10010,
				Type:               "l2",
				BridgeDomain:       "BD-10",
				VLANID:             10,
				RouteDistinguisher: "65000:10010",
				VRFTarget:          "target:65000:10010",
				VRFTargetExport:    []string{"target:65000:10011"},
				SourceInterface:    "ge-0/0/0",
				SourceAddress:      "192.0.2.1",
				MulticastGroup:     "239.0.0.10",
			},
		}}},
	}, 8)
	srv := NewServer(eng, &fakeStore{}, testLogger())

	var events []TelemetryEvent
	err := srv.SubscribeTelemetry(context.Background(), []string{"evpn"}, 0, true, func(event TelemetryEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("SubscribeTelemetry(evpn) error = %v", err)
	}
	if len(events) != 1 || events[0].Path != "/overlays/evpn" ||
		events[0].Cardinality != "per-vni" || events[0].PayloadSchema != "arca.telemetry.overlays.evpn.v1" ||
		events[0].EventType != telemetryEventTypeSnapshot {
		t.Fatalf("events = %#v, want one EVPN overlay snapshot", events)
	}
	var payload telemetryEVPNPayload
	if err := json.Unmarshal([]byte(events[0].JSONPayload), &payload); err != nil {
		t.Fatalf("EVPN telemetry payload is invalid JSON: %v", err)
	}
	if len(payload.VNIs) != 2 || payload.VNIs[0].VNI != 10010 || payload.VNIs[1].VNI != 20010 {
		t.Fatalf("EVPN VNIs = %#v, want sorted 10010 and 20010", payload.VNIs)
	}
	if payload.VNIs[0].BridgeDomain != "BD-10" || payload.VNIs[0].MulticastGroup != "239.0.0.10" {
		t.Fatalf("L2 EVPN VNI payload = %#v, want bridge-domain and multicast group", payload.VNIs[0])
	}
	if payload.VNIs[1].RoutingInstance != "BLUE" || payload.VNIs[1].RemoteVTEP != "198.51.100.20" || len(payload.VNIs[1].VRFTargetImport) != 1 {
		t.Fatalf("L3 EVPN VNI payload = %#v, want routing-instance, remote VTEP, and import target", payload.VNIs[1])
	}
}

func TestSubscribeTelemetryClassOfServiceIncludesQoSCapabilities(t *testing.T) {
	eng := engine.NewEngine(nil, testLogger())
	eng.InitializeRunning(&model.RouterConfig{
		ClassOfService: &model.ClassOfServiceConfig{
			TrafficControlProfiles: map[string]*model.TrafficControlProfile{
				"WAN": {ShapingRate: 1000000000},
			},
		},
	}, 3)
	srv := NewServer(eng, &fakeStore{}, testLogger())
	srv.SetQoSCapabilitySource(fakeQoSCapabilitySource{status: sbvpp.QoSCapabilityStatus{
		LastCheck: time.Unix(1700000500, 0),
		Capabilities: pkgvpp.QoSCapabilities{
			MetadataBinding:     true,
			QueueScheduler:      true,
			Policer:             false,
			OperationalCounters: true,
			Diagnostics:         []string{"scheduler api available"},
		},
	}})

	var events []TelemetryEvent
	err := srv.SubscribeTelemetry(context.Background(), []string{"/class-of-service"}, 0, true, func(event TelemetryEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatalf("SubscribeTelemetry(class-of-service) error = %v", err)
	}
	if len(events) != 1 || events[0].Path != "/class-of-service" ||
		events[0].Cardinality != "per-intent-object" || events[0].PayloadSchema != "arca.telemetry.class_of_service.v1" {
		t.Fatalf("events = %#v, want one class-of-service snapshot", events)
	}
	var payload struct {
		ClassOfService *ClassOfServiceInfo `json:"class_of_service"`
	}
	if err := json.Unmarshal([]byte(events[0].JSONPayload), &payload); err != nil {
		t.Fatalf("class-of-service telemetry payload is invalid JSON: %v", err)
	}
	if payload.ClassOfService == nil || payload.ClassOfService.Capabilities == nil {
		t.Fatalf("class-of-service payload = %#v, want QoS capability diagnostics", payload)
	}
	capabilities := payload.ClassOfService.Capabilities
	if !capabilities.MetadataBindingSupported || !capabilities.QueueSchedulerSupported || capabilities.PolicerSupported ||
		!capabilities.CountersSupported || capabilities.LastCheck.Unix() != 1700000500 ||
		len(capabilities.Diagnostics) != 1 {
		t.Fatalf("capabilities = %#v, want VPP QoS capability snapshot", capabilities)
	}
}

func TestSubscribeTelemetryRejectsUnsupportedPath(t *testing.T) {
	srv := NewServer(engine.NewEngine(nil, testLogger()), &fakeStore{}, testLogger())
	err := srv.SubscribeTelemetry(context.Background(), []string{"/unsupported"}, 0, true, func(event TelemetryEvent) error {
		t.Fatalf("unexpected telemetry event: %#v", event)
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported telemetry path") {
		t.Fatalf("SubscribeTelemetry() error = %v, want unsupported path", err)
	}
}

func TestTelemetryPathCatalog(t *testing.T) {
	catalog := TelemetryPathCatalog()
	if len(catalog) != len(telemetryPathOrder) {
		t.Fatalf("TelemetryPathCatalog() length = %d, want %d", len(catalog), len(telemetryPathOrder))
	}
	defaults := map[string]bool{}
	for i, info := range catalog {
		if info.Path != telemetryPathOrder[i] {
			t.Fatalf("catalog[%d].Path = %q, want %q", i, info.Path, telemetryPathOrder[i])
		}
		if info.Description == "" {
			t.Fatalf("catalog[%d].Description is empty for %s", i, info.Path)
		}
		if info.Cardinality == "" {
			t.Fatalf("catalog[%d].Cardinality is empty for %s", i, info.Path)
		}
		if info.PayloadSchema == "" {
			t.Fatalf("catalog[%d].PayloadSchema is empty for %s", i, info.Path)
		}
		for _, alias := range info.Aliases {
			if got := normalizeTelemetryPath(alias); got != info.Path {
				t.Fatalf("alias %q normalizes to %q, want %q", alias, got, info.Path)
			}
		}
		if info.Default {
			defaults[info.Path] = true
		}
	}
	cardinality := map[string]string{}
	payloadSchemas := map[string]string{}
	aliases := map[string][]string{}
	for _, info := range catalog {
		cardinality[info.Path] = info.Cardinality
		payloadSchemas[info.Path] = info.PayloadSchema
		aliases[info.Path] = info.Aliases
	}
	if cardinality["/routes"] != "per-route" || cardinality["/overlays/evpn"] != "per-vni" ||
		cardinality["/interfaces"] != "per-interface" {
		t.Fatalf("cardinality hints = %#v, want route/evpn/interface hints", cardinality)
	}
	if payloadSchemas["/routes"] != "arca.telemetry.routes.v1" ||
		payloadSchemas["/overlays/evpn"] != "arca.telemetry.overlays.evpn.v1" ||
		payloadSchemas["/class-of-service"] != "arca.telemetry.class_of_service.v1" {
		t.Fatalf("payload schema hints = %#v, want stable route/evpn/CoS schema hints", payloadSchemas)
	}
	if len(aliases["/overlays/evpn"]) != 2 || aliases["/overlays/evpn"][0] != "/evpn" ||
		len(aliases["/class-of-service"]) != 1 || aliases["/class-of-service"][0] != "/cos" {
		t.Fatalf("aliases = %#v, want EVPN and CoS path aliases", aliases)
	}
	for _, path := range defaultTelemetryPaths {
		if !defaults[path] {
			t.Fatalf("default path %s is not marked default in catalog %#v", path, catalog)
		}
	}
	if TelemetryEventSchemaVersion() != telemetrySchemaVersion {
		t.Fatalf("TelemetryEventSchemaVersion() = %q, want %q", TelemetryEventSchemaVersion(), telemetrySchemaVersion)
	}
	if TelemetryEncoding() != telemetryEncodingJSON {
		t.Fatalf("TelemetryEncoding() = %q, want %q", TelemetryEncoding(), telemetryEncodingJSON)
	}
	envelope := NewTelemetryCatalog()
	if envelope.EventSchemaVersion != telemetrySchemaVersion || envelope.Encoding != telemetryEncodingJSON {
		t.Fatalf("NewTelemetryCatalog() schema/encoding = %q/%q, want %q/%q",
			envelope.EventSchemaVersion, envelope.Encoding, telemetrySchemaVersion, telemetryEncodingJSON)
	}
	if len(envelope.DefaultPaths) != len(defaultTelemetryPaths) || len(envelope.Paths) != len(telemetryPathOrder) {
		t.Fatalf("NewTelemetryCatalog() = %#v, want default paths and path catalog", envelope)
	}
	if envelope.DefaultSampleIntervalMs != telemetrySampleIntervalMillis(defaultTelemetrySampleInterval) ||
		envelope.MinSampleIntervalMs != telemetrySampleIntervalMillis(minTelemetrySampleInterval) ||
		envelope.MaxSampleIntervalMs != telemetrySampleIntervalMillis(maxTelemetrySampleInterval) {
		t.Fatalf("NewTelemetryCatalog() intervals = %d/%d/%d, want sample interval hints",
			envelope.DefaultSampleIntervalMs, envelope.MinSampleIntervalMs, envelope.MaxSampleIntervalMs)
	}
	filtered := NewFilteredTelemetryCatalog(TelemetryCatalogFilter{
		Cardinalities:  []string{"PER-ROUTE", "per-peer"},
		PayloadSchemas: []string{"arca.telemetry.routes.v1"},
	})
	if len(filtered.Paths) != 1 || filtered.Paths[0].Path != "/routes" {
		t.Fatalf("NewFilteredTelemetryCatalog() paths = %#v, want only /routes", filtered.Paths)
	}
	filtered = NewFilteredTelemetryCatalog(TelemetryCatalogFilter{
		Paths: []string{"/evpn"},
	})
	if len(filtered.Paths) != 1 || filtered.Paths[0].Path != "/overlays/evpn" {
		t.Fatalf("NewFilteredTelemetryCatalog(path alias) paths = %#v, want only /overlays/evpn", filtered.Paths)
	}
	filtered = NewFilteredTelemetryCatalog(TelemetryCatalogFilter{
		DefaultOnly: true,
	})
	if len(filtered.Paths) != len(defaultTelemetryPaths) || filtered.Paths[0].Path != "/system" || filtered.Paths[1].Path != "/config/running" {
		t.Fatalf("NewFilteredTelemetryCatalog(default only) paths = %#v, want default paths", filtered.Paths)
	}
	filtered = NewFilteredTelemetryCatalog(TelemetryCatalogFilter{
		Encodings: []string{" JSON "},
	})
	if len(filtered.Paths) != len(telemetryPathOrder) {
		t.Fatalf("NewFilteredTelemetryCatalog(encoding) paths = %#v, want full path catalog", filtered.Paths)
	}
	filtered = NewFilteredTelemetryCatalog(TelemetryCatalogFilter{
		Encodings: []string{"protobuf"},
	})
	if len(filtered.Paths) != 0 {
		t.Fatalf("NewFilteredTelemetryCatalog(unsupported encoding) paths = %#v, want none", filtered.Paths)
	}
}

func TestTelemetryPayloadSchemaCatalog(t *testing.T) {
	catalog := TelemetryPayloadSchemaCatalog()
	if len(catalog) != len(telemetryPathOrder) {
		t.Fatalf("TelemetryPayloadSchemaCatalog() length = %d, want %d", len(catalog), len(telemetryPathOrder))
	}
	byPath := map[string]TelemetryPayloadSchemaInfo{}
	for i, info := range catalog {
		if info.Path != telemetryPathOrder[i] {
			t.Fatalf("catalog[%d].Path = %q, want %q", i, info.Path, telemetryPathOrder[i])
		}
		if info.PayloadSchema == "" || info.Description == "" || info.Cardinality == "" {
			t.Fatalf("catalog[%d] = %#v, want schema, description, and cardinality", i, info)
		}
		if len(info.Fields) == 0 {
			t.Fatalf("catalog[%d].Fields is empty for %s", i, info.Path)
		}
		byPath[info.Path] = info
	}
	if byPath["/routes"].Fields[0].Name != "routes" || byPath["/routes"].Fields[0].Type != "[]RouteInfo" {
		t.Fatalf("/routes fields = %#v, want routes []RouteInfo", byPath["/routes"].Fields)
	}
	if byPath["/overlays/evpn"].Fields[0].Name != "vnis" || byPath["/overlays/evpn"].Fields[0].Type != "[]EVPNVNI" {
		t.Fatalf("/overlays/evpn fields = %#v, want vnis []EVPNVNI", byPath["/overlays/evpn"].Fields)
	}
	if byPath["/class-of-service"].Fields[0].Name != "class_of_service" {
		t.Fatalf("/class-of-service fields = %#v, want class_of_service", byPath["/class-of-service"].Fields)
	}

	filtered := NewFilteredTelemetryPayloadSchemaCatalog(TelemetryCatalogFilter{
		Paths:          []string{"evpn"},
		PayloadSchemas: []string{"ARCA.TELEMETRY.OVERLAYS.EVPN.V1"},
		Encodings:      []string{" json "},
	})
	if len(filtered) != 1 || filtered[0].Path != "/overlays/evpn" {
		t.Fatalf("NewFilteredTelemetryPayloadSchemaCatalog() = %#v, want only /overlays/evpn", filtered)
	}
	filtered = NewFilteredTelemetryPayloadSchemaCatalog(TelemetryCatalogFilter{
		DefaultOnly: true,
	})
	if len(filtered) != len(defaultTelemetryPaths) || filtered[0].Path != "/system" || filtered[1].Path != "/config/running" {
		t.Fatalf("NewFilteredTelemetryPayloadSchemaCatalog(default only) = %#v, want default schemas", filtered)
	}
	filtered = NewFilteredTelemetryPayloadSchemaCatalog(TelemetryCatalogFilter{
		Encodings: []string{"protobuf"},
	})
	if len(filtered) != 0 {
		t.Fatalf("NewFilteredTelemetryPayloadSchemaCatalog(unsupported encoding) = %#v, want none", filtered)
	}

	catalog[0].Fields[0].Name = "mutated"
	if again := TelemetryPayloadSchemaCatalog(); again[0].Fields[0].Name == "mutated" {
		t.Fatal("TelemetryPayloadSchemaCatalog() returned shared field slices, want defensive copies")
	}
}

func scaledFRRRouteJSON(count int) string {
	var b strings.Builder
	b.WriteString("{")
	for i := 0; i < count; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `
	%q: [
		{
			"protocol": "bgp",
			"metric": %d,
			"selected": true,
			"nexthops": [
				{"ip": %q, "interfaceName": %q, "active": true}
			]
		}
	]`, fmt.Sprintf("10.0.%d.0/24", i), i, fmt.Sprintf("192.0.2.%d", i+1), fmt.Sprintf("ge0-0-%d", i))
	}
	b.WriteString("\n}")
	return b.String()
}

func routeInfoByPrefix(routes []RouteInfo, prefix string) (RouteInfo, bool) {
	for _, route := range routes {
		if route.Prefix == prefix {
			return route, true
		}
	}
	return RouteInfo{}, false
}

func TestOperationalStateEndpointsReadVPPAndFRR(t *testing.T) {
	srv := NewServer(engine.NewEngine(nil, testLogger()), &fakeStore{}, testLogger())
	ctx := context.Background()

	vppClient := pkgvpp.NewMockClient()
	if err := vppClient.Connect(ctx); err != nil {
		t.Fatalf("mock VPP Connect() error = %v", err)
	}
	iface, err := vppClient.CreateInterface(ctx, &pkgvpp.CreateInterfaceRequest{Type: pkgvpp.InterfaceTypeTap})
	if err != nil {
		t.Fatalf("mock VPP CreateInterface() error = %v", err)
	}
	if err := vppClient.SetInterfaceUp(ctx, iface.SwIfIndex); err != nil {
		t.Fatalf("mock VPP SetInterfaceUp() error = %v", err)
	}
	vppClient.SetInterfaceCounters(iface.SwIfIndex, pkgvpp.InterfaceCounters{
		RxPackets: 100,
		TxPackets: 200,
		RxBytes:   1000,
		TxBytes:   2000,
		RxErrors:  1,
		TxErrors:  2,
	})
	vppClient.SetInterfaceQueuePlacements(iface.SwIfIndex, pkgvpp.InterfaceQueuePlacements{
		Rx: []pkgvpp.InterfaceRxQueuePlacement{
			{QueueID: 0, WorkerID: 1, Mode: "polling"},
		},
		Tx: []pkgvpp.InterfaceTxQueuePlacement{
			{QueueID: 0, Shared: true, Threads: []uint32{0, 2}},
		},
	})
	if err := vppClient.SetQoSProfile(ctx, iface.SwIfIndex, pkgvpp.QoSProfile{Name: "WAN"}); err != nil {
		t.Fatalf("mock VPP SetQoSProfile() error = %v", err)
	}
	for _, isIPv6 := range []bool{false, true} {
		if err := vppClient.AddIPTable(ctx, pkgvpp.IPTable{ID: 100, IsIPv6: isIPv6, Name: "BLUE"}); err != nil {
			t.Fatalf("mock VPP AddIPTable(IPv6=%t) error = %v", isIPv6, err)
		}
		if err := vppClient.SetInterfaceTable(ctx, iface.SwIfIndex, 100, isIPv6); err != nil {
			t.Fatalf("mock VPP SetInterfaceTable(IPv6=%t) error = %v", isIPv6, err)
		}
	}
	if err := vppClient.Close(); err != nil {
		t.Fatalf("mock VPP Close() error = %v", err)
	}

	oldVPPClient := newOperationalVPPClient
	newOperationalVPPClient = func() pkgvpp.Client { return vppClient }
	t.Cleanup(func() { newOperationalVPPClient = oldVPPClient })

	ifaces, err := srv.GetInterfaces(ctx, "")
	if err != nil {
		t.Fatalf("GetInterfaces() error = %v", err)
	}
	if len(ifaces) != 1 {
		t.Fatalf("GetInterfaces() returned %d interfaces, want 1", len(ifaces))
	}
	if ifaces[0].Name != iface.Name || ifaces[0].AdminStatus != "up" || ifaces[0].OperStatus != "up" {
		t.Fatalf("GetInterfaces()[0] = %#v, want operational VPP state", ifaces[0])
	}
	if ifaces[0].RxPackets != 100 || ifaces[0].TxPackets != 200 || ifaces[0].RxBytes != 1000 || ifaces[0].TxBytes != 2000 || ifaces[0].RxErrors != 1 || ifaces[0].TxErrors != 2 {
		t.Fatalf("GetInterfaces()[0] counters = %#v, want VPP counters", ifaces[0])
	}
	if ifaces[0].QoSProfile != "WAN" {
		t.Fatalf("GetInterfaces()[0] QoSProfile = %q, want WAN", ifaces[0].QoSProfile)
	}
	if ifaces[0].IPv4TableID != 100 || ifaces[0].IPv6TableID != 100 {
		t.Fatalf("GetInterfaces()[0] table IDs = %d/%d, want 100/100", ifaces[0].IPv4TableID, ifaces[0].IPv6TableID)
	}
	if len(ifaces[0].RxQueues) != 1 || ifaces[0].RxQueues[0].WorkerID != 1 || ifaces[0].RxQueues[0].Mode != "polling" {
		t.Fatalf("GetInterfaces()[0] RX queues = %#v, want VPP queue placement", ifaces[0].RxQueues)
	}
	if len(ifaces[0].TxQueues) != 1 || !ifaces[0].TxQueues[0].Shared || len(ifaces[0].TxQueues[0].Threads) != 2 || ifaces[0].TxQueues[0].Threads[1] != 2 {
		t.Fatalf("GetInterfaces()[0] TX queues = %#v, want VPP queue placement", ifaces[0].TxQueues)
	}

	var commands []string
	oldVtysh := runOperationalVtyshCommand
	runOperationalVtyshCommand = func(ctx context.Context, command string) (string, error) {
		commands = append(commands, command)
		return command + "\n", nil
	}
	t.Cleanup(func() { runOperationalVtyshCommand = oldVtysh })

	if output, err := srv.GetRouteText(ctx, "ospf", ""); err != nil || output != "show ip route ospf\n" {
		t.Fatalf("GetRouteText() = %q, %v", output, err)
	}
	if output, err := srv.GetRouteText(ctx, "ospf3", "inet6"); err != nil || output != "show ipv6 route ospf6\n" {
		t.Fatalf("GetRouteText(inet6) = %q, %v", output, err)
	}
	if output, err := srv.GetBGPSummaryText(ctx); err != nil || output != "show bgp summary\n" {
		t.Fatalf("GetBGPSummaryText() = %q, %v", output, err)
	}
	if output, err := srv.GetBGPNeighborText(ctx, "192.0.2.1"); err != nil || output != "show bgp neighbor 192.0.2.1\n" {
		t.Fatalf("GetBGPNeighborText() = %q, %v", output, err)
	}
	if output, err := srv.GetOSPFNeighborsText(ctx, ""); err != nil || output != "show ip ospf neighbor\n" {
		t.Fatalf("GetOSPFNeighborsText() = %q, %v", output, err)
	}
	if output, err := srv.GetOSPFNeighborsText(ctx, "inet6"); err != nil || output != "show ipv6 ospf6 neighbor\n" {
		t.Fatalf("GetOSPFNeighborsText(inet6) = %q, %v", output, err)
	}
	if output, err := srv.GetVRRPText(ctx); err != nil || output != "show vrrp\n" {
		t.Fatalf("GetVRRPText() = %q, %v", output, err)
	}
	if output, err := srv.GetBFDText(ctx, "", true, false); err != nil || output != "show bfd peers brief\n" {
		t.Fatalf("GetBFDText(brief) = %q, %v", output, err)
	}
	if output, err := srv.GetBFDText(ctx, "192.0.2.2", false, true); err != nil || output != "show bfd peer 192.0.2.2 counters\n" {
		t.Fatalf("GetBFDText(peer counters) = %q, %v", output, err)
	}
	if len(commands) != 9 {
		t.Fatalf("vtysh commands = %v, want 9 commands", commands)
	}
	if _, err := srv.GetRouteText(ctx, "rip", ""); err == nil || !strings.Contains(err.Error(), "invalid route protocol") {
		t.Fatalf("GetRouteText(invalid) error = %v, want invalid protocol", err)
	}
	if _, err := srv.GetRouteText(ctx, "ospf", "inet6"); err == nil || !strings.Contains(err.Error(), "invalid route protocol") {
		t.Fatalf("GetRouteText(invalid inet6) error = %v, want invalid protocol", err)
	}
	if _, err := srv.GetOSPFNeighborsText(ctx, "link-state"); err == nil || !strings.Contains(err.Error(), "invalid address family") {
		t.Fatalf("GetOSPFNeighborsText(invalid family) error = %v, want invalid address family", err)
	}
	if _, err := srv.GetBGPNeighborText(ctx, "not-an-address"); err == nil || !strings.Contains(err.Error(), "invalid BGP neighbor address") {
		t.Fatalf("GetBGPNeighborText(invalid) error = %v, want invalid peer address", err)
	}
	if _, err := srv.GetBFDText(ctx, "not-an-address", false, false); err == nil || !strings.Contains(err.Error(), "invalid BFD peer address") {
		t.Fatalf("GetBFDText(invalid peer) error = %v, want invalid peer address", err)
	}
	if _, err := srv.GetBFDText(ctx, "192.0.2.2", true, false); err == nil || !strings.Contains(err.Error(), "does not support brief") {
		t.Fatalf("GetBFDText(peer brief) error = %v, want brief unsupported", err)
	}
}

func TestGetRoutesUsesFRRJSON(t *testing.T) {
	srv := NewServer(engine.NewEngine(nil, testLogger()), &fakeStore{}, testLogger())
	ctx := context.Background()

	var commands []string
	oldVtysh := runOperationalVtyshCommand
	runOperationalVtyshCommand = func(ctx context.Context, command string) (string, error) {
		commands = append(commands, command)
		switch command {
		case "show ip route json":
			return `{
				"10.0.0.0/24": [
					{
						"protocol": "bgp",
						"metric": 20,
						"selected": true,
						"nexthops": [
							{"ip": "192.0.2.1", "interfaceName": "ge0-0-0", "active": true}
						]
					}
				],
				"192.0.2.0/24": [
					{"protocol": "connected", "selected": true, "interfaceName": "ge0-0-1"}
				]
			}`, nil
		case "show ipv6 route json":
			return `{
				"2001:db8:100::/64": [
					{
						"protocol": "ospf6",
						"selected": true,
						"nexthops": [
							{"ip": "2001:db8::2", "interfaceName": "ge0-0-2", "active": true}
						]
					}
				]
			}`, nil
		default:
			t.Fatalf("unexpected vtysh command %q", command)
			return "", nil
		}
	}
	t.Cleanup(func() { runOperationalVtyshCommand = oldVtysh })

	routes, err := srv.GetRoutes(ctx, "", "")
	if err != nil {
		t.Fatalf("GetRoutes() error = %v", err)
	}
	if len(commands) != 2 || commands[0] != "show ip route json" || commands[1] != "show ipv6 route json" {
		t.Fatalf("vtysh commands = %#v, want IPv4 and IPv6 route JSON", commands)
	}
	if len(routes) != 3 {
		t.Fatalf("GetRoutes() returned %d routes, want 3", len(routes))
	}
	if got := routes[0]; got.Prefix != "10.0.0.0/24" || got.NextHop != "192.0.2.1" ||
		got.Protocol != "bgp" || got.Metric != 20 || got.Interface != "ge0-0-0" || !got.Active {
		t.Fatalf("GetRoutes()[0] = %#v, want parsed BGP route", got)
	}

	routes, err = srv.GetRoutes(ctx, "", "ospf3")
	if err != nil {
		t.Fatalf("GetRoutes(ospf3) error = %v", err)
	}
	if len(routes) != 1 || routes[0].Prefix != "2001:db8:100::/64" || routes[0].Protocol != "ospf6" {
		t.Fatalf("GetRoutes(ospf3) = %#v, want IPv6 OSPF route", routes)
	}

	routes, err = srv.GetRoutes(ctx, "192.0.2.0/24", "")
	if err != nil {
		t.Fatalf("GetRoutes(prefix) error = %v", err)
	}
	if len(routes) != 1 || routes[0].Protocol != "connected" {
		t.Fatalf("GetRoutes(prefix) = %#v, want connected route", routes)
	}

	if _, err := srv.GetRoutes(ctx, "not-a-prefix", ""); err == nil || !strings.Contains(err.Error(), "invalid route prefix filter") {
		t.Fatalf("GetRoutes(invalid prefix) error = %v, want invalid prefix", err)
	}
}

func TestGetBGPNeighborsUsesFRRJSON(t *testing.T) {
	srv := NewServer(engine.NewEngine(nil, testLogger()), &fakeStore{}, testLogger())
	ctx := context.Background()

	var commands []string
	oldVtysh := runOperationalVtyshCommand
	runOperationalVtyshCommand = func(ctx context.Context, command string) (string, error) {
		commands = append(commands, command)
		if command != "show bgp summary json" {
			t.Fatalf("unexpected vtysh command %q", command)
		}
		return `{
			"ipv4Unicast": {
				"peers": {
					"192.0.2.2": {
						"remoteAs": 65001,
						"state": "Established",
						"peerUptime": "00:01:30",
						"pfxRcd": 10,
						"pfxSnt": 3
					}
				}
			},
			"ipv6Unicast": {
				"peers": {
					"2001:db8::2": {
						"remoteAs": 65002,
						"state": "Active",
						"peerUptimeMsec": 42000,
						"pfxRcd": 0,
						"pfxSnt": 1
					}
				}
			}
		}`, nil
	}
	t.Cleanup(func() { runOperationalVtyshCommand = oldVtysh })

	neighbors, err := srv.GetBGPNeighbors(ctx)
	if err != nil {
		t.Fatalf("GetBGPNeighbors() error = %v", err)
	}
	if len(commands) != 1 || commands[0] != "show bgp summary json" {
		t.Fatalf("vtysh commands = %#v, want BGP summary JSON", commands)
	}
	if len(neighbors) != 2 {
		t.Fatalf("GetBGPNeighbors() returned %d neighbors, want 2", len(neighbors))
	}
	if got := neighbors[0]; got.PeerAddress != "192.0.2.2" || got.PeerAS != 65001 ||
		got.State != "Established" || got.UptimeSecs != 90 ||
		got.PrefixReceived != 10 || got.PrefixSent != 3 {
		t.Fatalf("GetBGPNeighbors()[0] = %#v, want IPv4 peer state", got)
	}
	if got := neighbors[1]; got.PeerAddress != "2001:db8::2" || got.PeerAS != 65002 ||
		got.State != "Active" || got.UptimeSecs != 42 ||
		got.PrefixReceived != 0 || got.PrefixSent != 1 {
		t.Fatalf("GetBGPNeighbors()[1] = %#v, want IPv6 peer state", got)
	}
}

func TestGetOSPFNeighborsUsesFRRJSON(t *testing.T) {
	srv := NewServer(engine.NewEngine(nil, testLogger()), &fakeStore{}, testLogger())
	ctx := context.Background()

	var commands []string
	oldVtysh := runOperationalVtyshCommand
	runOperationalVtyshCommand = func(ctx context.Context, command string) (string, error) {
		commands = append(commands, command)
		switch command {
		case "show ip ospf neighbor json":
			return `{
				"neighbors": {
					"10.0.0.2": [
						{
							"ifaceAddress": "192.0.2.2",
							"ifaceName": "ge0-0-0",
							"nbrState": "Full/DROther",
							"priority": 1,
							"deadTime": "00:00:31",
							"upTimeInMsec": 65000
						}
					]
				}
			}`, nil
		case "show ipv6 ospf6 neighbor json":
			return `{
				"neighbors": [
					{
						"neighborId": "10.0.0.3",
						"linkLocalAddress": "fe80::1",
						"interfaceName": "ge0-0-1",
						"state": "Full",
						"role": "Backup",
						"deadTime": "35.000s",
						"duration": "00:01:05"
					}
				]
			}`, nil
		default:
			t.Fatalf("unexpected vtysh command %q", command)
			return "", nil
		}
	}
	t.Cleanup(func() { runOperationalVtyshCommand = oldVtysh })

	neighbors, err := srv.GetOSPFNeighbors(ctx, "")
	if err != nil {
		t.Fatalf("GetOSPFNeighbors() error = %v", err)
	}
	if len(neighbors) != 1 {
		t.Fatalf("GetOSPFNeighbors() returned %d neighbors, want 1", len(neighbors))
	}
	if got := neighbors[0]; got.RouterID != "10.0.0.2" || got.Address != "192.0.2.2" ||
		got.Interface != "ge0-0-0" || got.State != "Full" || got.Role != "DROther" ||
		got.DeadTimeSecs != 31 || got.UptimeSecs != 65 {
		t.Fatalf("GetOSPFNeighbors()[0] = %#v, want IPv4 neighbor state", got)
	}

	neighbors, err = srv.GetOSPFNeighbors(ctx, addressFamilyIPv6)
	if err != nil {
		t.Fatalf("GetOSPFNeighbors(inet6) error = %v", err)
	}
	if len(neighbors) != 1 {
		t.Fatalf("GetOSPFNeighbors(inet6) returned %d neighbors, want 1", len(neighbors))
	}
	if got := neighbors[0]; got.RouterID != "10.0.0.3" || got.Address != "fe80::1" ||
		got.Interface != "ge0-0-1" || got.State != "Full" || got.Role != "Backup" ||
		got.DeadTimeSecs != 35 || got.UptimeSecs != 65 {
		t.Fatalf("GetOSPFNeighbors(inet6)[0] = %#v, want IPv6 neighbor state", got)
	}
	if strings.Join(commands, "\n") != "show ip ospf neighbor json\nshow ipv6 ospf6 neighbor json" {
		t.Fatalf("vtysh commands = %#v, want OSPFv2 then OSPFv3 JSON", commands)
	}
}

func TestGetInterfacesUsesManagedStateCollector(t *testing.T) {
	srv := NewServer(engine.NewEngine(nil, testLogger()), &fakeStore{}, testLogger())
	collector := &fakeInterfaceStateCollector{
		states: map[string]*model.InterfaceState{
			"fallback0": {
				Name:        "ge-0/0/0",
				AdminStatus: "up",
				OperStatus:  "down",
				Speed:       10_000_000_000,
				MTU:         1500,
				MAC:         "02:00:00:00:00:01",
				QoSProfile:  "WAN",
				IPv4TableID: 100,
				IPv6TableID: 100,
				Counters: &model.InterfaceCounters{
					RxPackets: 10,
					TxPackets: 20,
					RxBytes:   100,
					TxBytes:   200,
					RxErrors:  1,
					TxErrors:  2,
				},
				Queues: &model.InterfaceQueues{
					Rx: []model.InterfaceRxQueue{
						{QueueID: 0, WorkerID: 3, Mode: "adaptive"},
					},
					Tx: []model.InterfaceTxQueue{
						{QueueID: 1, Shared: true, Threads: []uint32{0, 3}},
					},
				},
			},
			"ge-0/0/1": {
				AdminStatus: "down",
				OperStatus:  "down",
			},
		},
	}
	srv.SetInterfaceStateCollector(collector)

	ifaces, err := srv.GetInterfaces(context.Background(), "ge-0/0/0")
	if err != nil {
		t.Fatalf("GetInterfaces() error = %v", err)
	}
	if collector.calls != 1 {
		t.Fatalf("collector calls = %d, want 1", collector.calls)
	}
	if len(ifaces) != 1 {
		t.Fatalf("GetInterfaces() returned %d interfaces, want 1", len(ifaces))
	}
	got := ifaces[0]
	if got.Name != "ge-0/0/0" || got.AdminStatus != "up" || got.OperStatus != "down" || got.Speed != 10_000_000_000 || got.MTU != 1500 || got.MAC != "02:00:00:00:00:01" {
		t.Fatalf("GetInterfaces()[0] = %#v, want managed interface state", got)
	}
	if got.QoSProfile != "WAN" || got.IPv4TableID != 100 || got.IPv6TableID != 100 || got.RxPackets != 10 || got.TxPackets != 20 || got.RxBytes != 100 || got.TxBytes != 200 || got.RxErrors != 1 || got.TxErrors != 2 {
		t.Fatalf("GetInterfaces()[0] counters/QoS = %#v, want collector values", got)
	}
	if len(got.RxQueues) != 1 || got.RxQueues[0].WorkerID != 3 || got.RxQueues[0].Mode != "adaptive" {
		t.Fatalf("GetInterfaces()[0] RX queues = %#v, want collector queue placement", got.RxQueues)
	}
	if len(got.TxQueues) != 1 || got.TxQueues[0].QueueID != 1 || !got.TxQueues[0].Shared || len(got.TxQueues[0].Threads) != 2 || got.TxQueues[0].Threads[1] != 3 {
		t.Fatalf("GetInterfaces()[0] TX queues = %#v, want collector queue placement", got.TxQueues)
	}
}

func TestGetLCPReconciliationUsesSource(t *testing.T) {
	srv := NewServer(engine.NewEngine(nil, testLogger()), &fakeStore{}, testLogger())
	lastRun := time.Unix(1700000000, 0).UTC()
	srv.SetLCPReconciliationSource(fakeLCPReconciliationSource{info: LCPReconciliationInfo{
		LastRun:         lastRun,
		PairCount:       2,
		Inconsistencies: []string{"missing pair"},
	}})

	info, err := srv.GetLCPReconciliation(context.Background())
	if err != nil {
		t.Fatalf("GetLCPReconciliation() error = %v", err)
	}
	if info.LastRun != lastRun || info.PairCount != 2 || len(info.Inconsistencies) != 1 || info.Inconsistencies[0] != "missing pair" {
		t.Fatalf("GetLCPReconciliation() = %#v, want source status", info)
	}
}

func TestGetBFDStatusUsesSource(t *testing.T) {
	srv := NewServer(engine.NewEngine(nil, testLogger()), &fakeStore{}, testLogger())
	lastRun := time.Unix(1700000500, 0).UTC()
	srv.SetBFDOperationalSource(fakeBFDOperationalSource{status: sbfrr.BFDOperationalStatus{
		LastRun:           lastRun,
		ConfiguredPeers:   2,
		ObservedPeers:     2,
		UpPeers:           1,
		DownPeers:         1,
		SessionDownEvents: 3,
		RxFailPackets:     4,
		Issues:            []string{"FRR BFD peer 192.0.2.3 is not up: down"},
		Peers: []sbfrr.BFDPeerOperationalStatus{
			{
				Peer:              "192.0.2.3",
				LocalAddress:      "192.0.2.1",
				Interface:         "ge0-0-1",
				Status:            "down",
				Diagnostic:        "control detection time expired",
				RemoteDiagnostic:  "ok",
				Observed:          true,
				Up:                false,
				SessionDownEvents: 3,
				RxFailPackets:     4,
			},
			{
				Peer:              "192.0.2.2",
				LocalAddress:      "192.0.2.1",
				Interface:         "ge0-0-0",
				VRF:               "default",
				Status:            "up",
				Observed:          true,
				Up:                true,
				SessionDownEvents: 0,
				RxFailPackets:     0,
			},
		},
	}})

	info, err := srv.GetBFDStatus(context.Background())
	if err != nil {
		t.Fatalf("GetBFDStatus() error = %v", err)
	}
	if info.LastRun != lastRun || info.ConfiguredPeers != 2 || info.ObservedPeers != 2 ||
		info.UpPeers != 1 || info.DownPeers != 1 || info.SessionDownEvents != 3 || info.RxFailPackets != 4 {
		t.Fatalf("GetBFDStatus() = %#v, want BFD aggregate counters", info)
	}
	if len(info.Peers) != 2 || info.Peers[0].Peer != "192.0.2.2" || !info.Peers[0].Up ||
		info.Peers[1].Peer != "192.0.2.3" || info.Peers[1].Up || info.Peers[1].Diagnostic == "" {
		t.Fatalf("GetBFDStatus().Peers = %#v, want sorted peer detail", info.Peers)
	}
	if len(info.Issues) != 1 || !strings.Contains(info.Issues[0], "not up") {
		t.Fatalf("GetBFDStatus().Issues = %#v, want issue detail", info.Issues)
	}
}

func TestGetHAStatusUsesSource(t *testing.T) {
	srv := NewServer(engine.NewEngine(nil, testLogger()), &fakeStore{}, testLogger())
	lastRun := time.Unix(1700000300, 0).UTC()
	bfdLastRun := time.Unix(1700000400, 0).UTC()
	srv.SetHAStatusSource(fakeHAStatusSource{info: HAStatusInfo{
		Configured:              true,
		Converged:               false,
		VRRPGroups:              1,
		Issues:                  []string{"FRR VRRP status has inactive groups"},
		ClusterEnabled:          true,
		ClusterNodes:            2,
		ClusterEtcdSync:         true,
		ClusterSyncAligned:      true,
		FRRVRRPLastCheck:        lastRun,
		FRRVRRPConfiguredGroups: 1,
		FRRVRRPObservedGroups:   1,
		FRRVRRPActiveGroups:     0,
		FRRBFDLastCheck:         bfdLastRun,
		FRRBFDConfiguredPeers:   1,
		FRRBFDObservedPeers:     1,
		FRRBFDUpPeers:           0,
		FRRBFDDownPeers:         1,
		FRRBFDIssues:            []string{"FRR BFD peer 192.0.2.2 is down"},
	}})

	info, err := srv.GetHAStatus(context.Background())
	if err != nil {
		t.Fatalf("GetHAStatus() error = %v", err)
	}
	if !info.Configured || info.Converged || info.VRRPGroups != 1 || info.FRRVRRPLastCheck != lastRun ||
		info.FRRVRRPActiveGroups != 0 || info.FRRBFDLastCheck != bfdLastRun ||
		info.FRRBFDConfiguredPeers != 1 || info.FRRBFDDownPeers != 1 || len(info.FRRBFDIssues) != 1 ||
		len(info.Issues) != 1 {
		t.Fatalf("GetHAStatus() = %#v, want source status", info)
	}
}

func TestGetClassOfServiceReturnsRunningConfig(t *testing.T) {
	eng := engine.NewEngine(nil, testLogger())
	eng.InitializeRunning(&model.RouterConfig{
		ClassOfService: &model.ClassOfServiceConfig{
			ForwardingClasses: map[string]*model.ForwardingClass{
				"best-effort":          {Queue: 0},
				"expedited-forwarding": {Queue: 5},
			},
			TrafficControlProfiles: map[string]*model.TrafficControlProfile{
				"WAN": {
					ShapingRate:  1000000000,
					SchedulerMap: "WAN-SCHED",
				},
			},
			Interfaces: map[string]*model.CoSInterface{
				"ge-0/0/0": {OutputTrafficControlProfile: "WAN"},
			},
		},
	}, 1)
	srv := NewServer(eng, &fakeStore{}, testLogger())
	srv.SetQoSCapabilitySource(fakeQoSCapabilitySource{status: sbvpp.QoSCapabilityStatus{
		LastCheck: time.Unix(1700000500, 0),
		Capabilities: pkgvpp.QoSCapabilities{
			MetadataBinding:     true,
			QueueScheduler:      false,
			Policer:             false,
			OperationalCounters: false,
			Diagnostics:         []string{"scheduler api unavailable"},
		},
	}})

	info, err := srv.GetClassOfService(context.Background())
	if err != nil {
		t.Fatalf("GetClassOfService() error = %v", err)
	}
	if info.EnforcementStatus != "intent-only" {
		t.Fatalf("EnforcementStatus = %q, want intent-only", info.EnforcementStatus)
	}
	if len(info.ForwardingClasses) != 2 || info.ForwardingClasses[0].Name != "best-effort" || info.ForwardingClasses[1].Name != "expedited-forwarding" {
		t.Fatalf("ForwardingClasses = %#v, want sorted class list", info.ForwardingClasses)
	}
	if len(info.TrafficControlProfiles) != 1 || info.TrafficControlProfiles[0].Name != "WAN" ||
		info.TrafficControlProfiles[0].ShapingRate != 1000000000 ||
		info.TrafficControlProfiles[0].SchedulerMap != "WAN-SCHED" ||
		info.TrafficControlProfiles[0].EnforcementStatus != "intent-only" {
		t.Fatalf("TrafficControlProfiles = %#v, want WAN profile", info.TrafficControlProfiles)
	}
	if len(info.Interfaces) != 1 || info.Interfaces[0].Name != "ge-0/0/0" ||
		info.Interfaces[0].OutputTrafficControlProfile != "WAN" ||
		info.Interfaces[0].EnforcementStatus != "intent-only" {
		t.Fatalf("Interfaces = %#v, want interface binding", info.Interfaces)
	}
	if info.Capabilities == nil || !info.Capabilities.MetadataBindingSupported ||
		info.Capabilities.QueueSchedulerSupported || info.Capabilities.LastCheck.Unix() != 1700000500 ||
		len(info.Capabilities.Diagnostics) != 1 {
		t.Fatalf("Capabilities = %#v, want metadata-only VPP QoS capability status", info.Capabilities)
	}
}

func TestGetRoutingInstancesReturnsRunningConfig(t *testing.T) {
	eng := engine.NewEngine(nil, testLogger())
	eng.InitializeRunning(&model.RouterConfig{
		RoutingInstances: map[string]*model.RoutingInstance{
			"BLUE": {
				InstanceType:       "vrf",
				RouteDistinguisher: "65000:100",
				VRFTarget:          "target:65000:100",
				VRFTargetImport:    []string{"target:65000:101"},
				VRFTargetExport:    []string{"target:65000:102"},
				VRFImport:          []string{"BLUE-IN"},
				VRFExport:          []string{"BLUE-OUT"},
				Interfaces:         []string{"ge-0/0/1", "ge-0/0/0"},
			},
			"RED": {
				Interfaces: []string{"ge-0/0/2"},
			},
		},
	}, 1)
	srv := NewServer(eng, &fakeStore{}, testLogger())

	instances, err := srv.GetRoutingInstances(context.Background())
	if err != nil {
		t.Fatalf("GetRoutingInstances() error = %v", err)
	}
	if len(instances) != 2 {
		t.Fatalf("GetRoutingInstances() returned %d instances, want 2: %#v", len(instances), instances)
	}
	blue := instances[0]
	if blue.Name != "BLUE" || blue.InstanceType != "vrf" || blue.RouteDistinguisher != "65000:100" ||
		blue.IPv4TableID != 100 || blue.IPv6TableID != 100 {
		t.Fatalf("BLUE routing instance = %#v, want RD-derived table", blue)
	}
	if strings.Join(blue.ImportTargets, ",") != "target:65000:100,target:65000:101" ||
		strings.Join(blue.ExportTargets, ",") != "target:65000:100,target:65000:102" ||
		strings.Join(blue.ImportPolicies, ",") != "BLUE-IN" ||
		strings.Join(blue.ExportPolicies, ",") != "BLUE-OUT" ||
		strings.Join(blue.Interfaces, ",") != "ge-0/0/0,ge-0/0/1" {
		t.Fatalf("BLUE routing instance lists = %#v, want targets/policies/interfaces", blue)
	}
	red := instances[1]
	if red.Name != "RED" || red.InstanceType != "vrf" || red.IPv4TableID == 0 || red.IPv6TableID != red.IPv4TableID ||
		strings.Join(red.Interfaces, ",") != "ge-0/0/2" {
		t.Fatalf("RED routing instance = %#v, want derived table", red)
	}
}

func TestReleaseLockWaitsForInFlightCommit(t *testing.T) {
	parserEntered := make(chan struct{})
	unblockParser := make(chan struct{})
	var enteredOnce sync.Once
	var unblockOnce sync.Once
	t.Cleanup(func() {
		unblockOnce.Do(func() { close(unblockParser) })
	})

	oldParser := ConfigTextParser
	ConfigTextParser = func(text string) (*model.RouterConfig, error) {
		enteredOnce.Do(func() { close(parserEntered) })
		<-unblockParser
		cfg, err := pkgconfig.NewParser(strings.NewReader(text)).Parse()
		if err != nil {
			return nil, err
		}
		return model.FromLegacyConfig(cfg), nil
	}
	t.Cleanup(func() {
		ConfigTextParser = oldParser
	})

	eng := engine.NewEngine(nil, testLogger())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 1)
	srv := NewServer(eng, &fakeStore{commitID: "commit-1"}, testLogger())
	ctx := context.Background()

	sessionID, err := srv.CreateSession(ctx, "alice")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := srv.AcquireLock(ctx, sessionID, "alice"); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}
	if err := srv.EditCandidate(ctx, sessionID, "set system host-name router2"); err != nil {
		t.Fatalf("EditCandidate() error = %v", err)
	}

	commitErrCh := make(chan error, 1)
	go func() {
		_, _, err := srv.Commit(ctx, sessionID, "alice", "test")
		commitErrCh <- err
	}()

	select {
	case <-parserEntered:
	case <-time.After(time.Second):
		t.Fatal("Commit() did not enter parser")
	}

	releaseErrCh := make(chan error, 1)
	go func() {
		releaseErrCh <- srv.ReleaseLock(ctx, sessionID)
	}()

	select {
	case err := <-releaseErrCh:
		t.Fatalf("ReleaseLock() returned before in-flight commit finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	unblockOnce.Do(func() { close(unblockParser) })
	if err := <-commitErrCh; err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if err := <-releaseErrCh; err != nil {
		t.Fatalf("ReleaseLock() error = %v", err)
	}
}

func TestCommitRejectsStaleCandidate(t *testing.T) {
	oldParser := ConfigTextParser
	ConfigTextParser = func(text string) (*model.RouterConfig, error) {
		cfg, err := pkgconfig.NewParser(strings.NewReader(text)).Parse()
		if err != nil {
			return nil, err
		}
		return model.FromLegacyConfig(cfg), nil
	}
	t.Cleanup(func() { ConfigTextParser = oldParser })

	ctx := context.Background()
	eng := engine.NewEngine(nil, testLogger())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 1)
	st := &fakeStore{commitID: "commit-1"}
	srv := NewServer(eng, st, testLogger())

	sessionID, err := srv.CreateSession(ctx, "alice")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := srv.AcquireLock(ctx, sessionID, "alice"); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}
	if err := srv.EditCandidate(ctx, sessionID, "set system host-name router2"); err != nil {
		t.Fatalf("EditCandidate() error = %v", err)
	}
	if err := eng.Apply(ctx, &model.RouterConfig{
		System:     &model.SystemConfig{HostName: "netconf-router"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, "bob", "external commit"); err != nil {
		t.Fatalf("Apply() external error = %v", err)
	}

	_, _, err = srv.Commit(ctx, sessionID, "alice", "stale")
	if err == nil || !strings.Contains(err.Error(), "candidate configuration is stale") || !errors.Is(err, ErrCandidateConflict) {
		t.Fatalf("Commit() error = %v, want stale candidate", err)
	}
	if st.saved != nil {
		t.Fatal("Commit() prepared persistence for stale candidate")
	}
	if got := eng.Running().System.HostName; got != "netconf-router" {
		t.Fatalf("running hostname = %q, want netconf-router", got)
	}
}

func TestCommitAbortsWhenCandidateStalesAfterPrepare(t *testing.T) {
	oldParser := ConfigTextParser
	ConfigTextParser = func(text string) (*model.RouterConfig, error) {
		cfg, err := pkgconfig.NewParser(strings.NewReader(text)).Parse()
		if err != nil {
			return nil, err
		}
		return model.FromLegacyConfig(cfg), nil
	}
	t.Cleanup(func() { ConfigTextParser = oldParser })

	ctx := context.Background()
	eng := engine.NewEngine(nil, testLogger())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 1)
	st := &fakeStore{commitID: "commit-1"}
	st.prepareFn = func() {
		if err := eng.Apply(ctx, &model.RouterConfig{
			System:     &model.SystemConfig{HostName: "netconf-router"},
			Interfaces: map[string]*model.InterfaceConfig{},
		}, "bob", "external commit"); err != nil {
			t.Fatalf("external Apply() error = %v", err)
		}
	}
	srv := NewServer(eng, st, testLogger())

	sessionID, err := srv.CreateSession(ctx, "alice")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := srv.AcquireLock(ctx, sessionID, "alice"); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}
	if err := srv.EditCandidate(ctx, sessionID, "set system host-name router2"); err != nil {
		t.Fatalf("EditCandidate() error = %v", err)
	}

	_, _, err = srv.Commit(ctx, sessionID, "alice", "stale")
	if err == nil || !strings.Contains(err.Error(), "candidate configuration is stale") || !errors.Is(err, ErrCandidateConflict) {
		t.Fatalf("Commit() error = %v, want stale candidate", err)
	}
	if !st.aborted {
		t.Fatal("Commit() did not abort prepared persistence after stale recheck")
	}
	if got := eng.Running().System.HostName; got != "netconf-router" {
		t.Fatalf("running hostname = %q, want netconf-router", got)
	}
}

func TestCommitAllowsEmptyCandidate(t *testing.T) {
	oldParser := ConfigTextParser
	ConfigTextParser = func(text string) (*model.RouterConfig, error) {
		cfg, err := pkgconfig.NewParser(strings.NewReader(text)).Parse()
		if err != nil {
			return nil, err
		}
		return model.FromLegacyConfig(cfg), nil
	}
	t.Cleanup(func() { ConfigTextParser = oldParser })

	ctx := context.Background()
	eng := engine.NewEngine(nil, testLogger())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 1)
	st := &fakeStore{commitID: "commit-empty"}
	srv := NewServer(eng, st, testLogger())

	sessionID, err := srv.CreateSession(ctx, "alice")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := srv.AcquireLock(ctx, sessionID, "alice"); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}
	if err := srv.EditCandidate(ctx, sessionID, "delete system host-name"); err != nil {
		t.Fatalf("EditCandidate() error = %v", err)
	}
	if candidate, err := srv.GetCandidate(ctx, sessionID); err != nil {
		t.Fatalf("GetCandidate() error = %v", err)
	} else if candidate != "" {
		t.Fatalf("candidate = %q, want empty config", candidate)
	}
	if err := srv.ValidateCandidate(ctx, sessionID); err != nil {
		t.Fatalf("ValidateCandidate() error = %v", err)
	}

	commitID, version, err := srv.Commit(ctx, sessionID, "alice", "clear config")
	if err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if commitID != "commit-empty" || version != 2 {
		t.Fatalf("Commit() = (%q, %d), want commit-empty version 2", commitID, version)
	}
	if got := eng.Running().System; got != nil && got.HostName != "" {
		t.Fatalf("running system = %#v, want empty hostname", got)
	}
}

func TestCommitRejectsNoopCandidate(t *testing.T) {
	oldParser := ConfigTextParser
	ConfigTextParser = func(text string) (*model.RouterConfig, error) {
		cfg, err := pkgconfig.NewParser(strings.NewReader(text)).Parse()
		if err != nil {
			return nil, err
		}
		return model.FromLegacyConfig(cfg), nil
	}
	t.Cleanup(func() { ConfigTextParser = oldParser })

	ctx := context.Background()
	eng := engine.NewEngine(nil, testLogger())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 1)
	st := &fakeStore{commitID: "commit-1"}
	srv := NewServer(eng, st, testLogger())

	sessionID, err := srv.CreateSession(ctx, "alice")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := srv.AcquireLock(ctx, sessionID, "alice"); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}

	_, _, err = srv.Commit(ctx, sessionID, "alice", "noop")
	if err == nil || !strings.Contains(err.Error(), "no configuration changes to commit") || !errors.Is(err, ErrConfigInput) {
		t.Fatalf("Commit() error = %v, want no changes", err)
	}
	if st.saved != nil {
		t.Fatal("Commit() prepared persistence for unchanged candidate")
	}
	if snap := eng.RunningSnapshot(); snap == nil || snap.Version != 1 {
		t.Fatalf("running snapshot = %#v, want version 1", snap)
	}
}

func TestListHistoryRejectsNegativePagination(t *testing.T) {
	eng := engine.NewEngine(nil, testLogger())
	st := &fakeStore{}
	srv := NewServer(eng, st, testLogger())
	ctx := context.Background()

	tests := []struct {
		name   string
		limit  int
		offset int
	}{
		{name: "negative limit", limit: -1, offset: 0},
		{name: "negative offset", limit: 10, offset: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st.listCalls = 0
			if _, err := srv.ListHistory(ctx, tt.limit, tt.offset); err == nil {
				t.Fatal("ListHistory() error = nil, want invalid pagination")
			}
			if st.listCalls != 0 {
				t.Fatalf("ListCommits calls = %d, want 0", st.listCalls)
			}
		})
	}
}

func TestListHistoryBoundsLimit(t *testing.T) {
	eng := engine.NewEngine(nil, testLogger())
	st := &fakeStore{}
	srv := NewServer(eng, st, testLogger())
	ctx := context.Background()

	tests := []struct {
		name      string
		limit     int
		wantLimit int
	}{
		{name: "default", limit: 0, wantLimit: maxListHistoryLimit},
		{name: "within limit", limit: 25, wantLimit: 25},
		{name: "above limit", limit: maxListHistoryLimit + 1, wantLimit: maxListHistoryLimit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st.listOpts = nil
			if _, err := srv.ListHistory(ctx, tt.limit, 7); err != nil {
				t.Fatalf("ListHistory() error = %v", err)
			}
			if st.listOpts == nil {
				t.Fatal("ListHistory() did not call ListCommits")
			}
			if st.listOpts.Limit != tt.wantLimit || st.listOpts.Offset != 7 {
				t.Fatalf("ListOptions = %+v, want limit %d offset 7", st.listOpts, tt.wantLimit)
			}
		})
	}
}

func TestListAuditEventsBoundsLimit(t *testing.T) {
	eng := engine.NewEngine(nil, testLogger())
	st := &fakeStore{}
	srv := NewServer(eng, st, testLogger())
	ctx := context.Background()

	tests := []struct {
		name      string
		limit     int
		wantLimit int
	}{
		{name: "default", limit: 0, wantLimit: maxListAuditEventsLimit},
		{name: "within limit", limit: 25, wantLimit: 25},
		{name: "above limit", limit: maxListAuditEventsLimit + 1, wantLimit: maxListAuditEventsLimit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st.auditOpts = nil
			if _, err := srv.ListAuditEvents(ctx, AuditLogOptions{Limit: tt.limit, Offset: 7}); err != nil {
				t.Fatalf("ListAuditEvents() error = %v", err)
			}
			if st.auditOpts == nil {
				t.Fatal("ListAuditEvents() did not call store")
			}
			if st.auditOpts.Limit != tt.wantLimit || st.auditOpts.Offset != 7 {
				t.Fatalf("AuditOptions = %+v, want limit %d offset 7", st.auditOpts, tt.wantLimit)
			}
		})
	}
}

func TestListHistoryIncludesConfigText(t *testing.T) {
	eng := engine.NewEngine(nil, testLogger())
	st := &fakeStore{
		listRecords: []*store.CommitRecord{
			{
				CommitID:  "commit-1",
				Author:    "alice",
				Message:   "initial",
				Timestamp: time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC),
				Config: &model.RouterConfig{
					System: &model.SystemConfig{HostName: "router1"},
					Security: &model.SecurityConfig{
						Users: map[string]*model.UserConfig{
							"admin": {
								Password: "plain-password",
								Role:     "admin",
							},
						},
					},
				},
			},
		},
	}
	srv := NewServer(eng, st, testLogger())

	entries, err := srv.ListHistory(context.Background(), 10, 0)
	if err != nil {
		t.Fatalf("ListHistory() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("ListHistory() entries = %d, want 1", len(entries))
	}
	if !strings.Contains(entries[0].ConfigText, "set system host-name router1") {
		t.Fatalf("ListHistory() config text = %q, want archived set commands", entries[0].ConfigText)
	}
	if strings.Contains(entries[0].ConfigText, "plain-password") || strings.Contains(entries[0].ConfigText, "$argon2id$") {
		t.Fatalf("ListHistory() leaked credential material:\n%s", entries[0].ConfigText)
	}
}

func TestCommitInfosFromProtoIncludesConfigText(t *testing.T) {
	infos := commitInfosFromProto([]*apiv1.CommitEntry{
		{
			CommitId:   "commit-1",
			User:       "alice",
			Timestamp:  time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
			Message:    "initial",
			ConfigText: "set system host-name router1",
		},
	})

	if len(infos) != 1 {
		t.Fatalf("commitInfosFromProto() entries = %d, want 1", len(infos))
	}
	if infos[0].ConfigText != "set system host-name router1" {
		t.Fatalf("commitInfosFromProto() config text = %q, want archived set commands", infos[0].ConfigText)
	}
}

func TestValidateCandidateRejectsInvalidConfig(t *testing.T) {
	oldParser := ConfigTextParser
	ConfigTextParser = func(text string) (*model.RouterConfig, error) {
		cfg, err := pkgconfig.NewParser(strings.NewReader(text)).Parse()
		if err != nil {
			return nil, err
		}
		return model.FromLegacyConfig(cfg), nil
	}
	t.Cleanup(func() { ConfigTextParser = oldParser })

	eng := engine.NewEngine(nil, testLogger())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 1)
	srv := NewServer(eng, &fakeStore{commitID: "commit-1"}, testLogger())
	ctx := context.Background()
	sessionID, err := srv.CreateSession(ctx, "alice")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := srv.AcquireLock(ctx, sessionID, "alice"); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}
	if err := srv.EditCandidate(ctx, sessionID, "set unsupported path value"); err != nil {
		t.Fatalf("EditCandidate() error = %v", err)
	}
	if err := srv.ValidateCandidate(ctx, sessionID); err == nil {
		t.Fatal("ValidateCandidate() expected error")
	}
}

func TestReplaceCandidateReplacesExistingCandidate(t *testing.T) {
	oldParser := ConfigTextParser
	ConfigTextParser = func(text string) (*model.RouterConfig, error) {
		cfg, err := pkgconfig.NewParser(strings.NewReader(text)).Parse()
		if err != nil {
			return nil, err
		}
		return model.FromLegacyConfig(cfg), nil
	}
	t.Cleanup(func() { ConfigTextParser = oldParser })

	eng := engine.NewEngine(nil, testLogger())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "running"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 1)
	srv := NewServer(eng, &fakeStore{commitID: "commit-1"}, testLogger())
	ctx := context.Background()
	sessionID, err := srv.CreateSession(ctx, "alice")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := srv.AcquireLock(ctx, sessionID, "alice"); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}
	if err := srv.EditCandidate(ctx, sessionID, "set system host-name dirty"); err != nil {
		t.Fatalf("EditCandidate() error = %v", err)
	}
	if err := srv.ReplaceCandidate(ctx, sessionID, "set system host-name restored"); err != nil {
		t.Fatalf("ReplaceCandidate() error = %v", err)
	}
	got, err := srv.GetCandidate(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetCandidate() error = %v", err)
	}
	if !strings.Contains(got, "set system host-name restored") || strings.Contains(got, "dirty") {
		t.Fatalf("candidate text = %q, want restored replacement without stale edits", got)
	}
}

func TestReplaceCandidateRejectsInvalidConfig(t *testing.T) {
	oldParser := ConfigTextParser
	ConfigTextParser = func(text string) (*model.RouterConfig, error) {
		cfg, err := pkgconfig.NewParser(strings.NewReader(text)).Parse()
		if err != nil {
			return nil, err
		}
		return model.FromLegacyConfig(cfg), nil
	}
	t.Cleanup(func() { ConfigTextParser = oldParser })

	eng := engine.NewEngine(nil, testLogger())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "running"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 1)
	srv := NewServer(eng, &fakeStore{commitID: "commit-1"}, testLogger())
	ctx := context.Background()
	sessionID, err := srv.CreateSession(ctx, "alice")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := srv.AcquireLock(ctx, sessionID, "alice"); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}
	before, err := srv.GetCandidate(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetCandidate() error = %v", err)
	}
	if err := srv.ReplaceCandidate(ctx, sessionID, "set unsupported path value"); err == nil {
		t.Fatal("ReplaceCandidate() error = nil, want parse failure")
	}
	after, err := srv.GetCandidate(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetCandidate() error = %v", err)
	}
	if after != before {
		t.Fatalf("candidate changed after failed replacement: before=%q after=%q", before, after)
	}
}

func TestReplaceCandidateRequiresLock(t *testing.T) {
	eng := engine.NewEngine(nil, testLogger())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "running"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 1)
	srv := NewServer(eng, &fakeStore{commitID: "commit-1"}, testLogger())
	ctx := context.Background()
	sessionID, err := srv.CreateSession(ctx, "alice")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := srv.ReplaceCandidate(ctx, sessionID, "set system host-name restored"); err == nil {
		t.Fatal("ReplaceCandidate() error = nil, want lock failure")
	}
}

func TestAcquireLockReleasesLockWhenRunningSerializationFails(t *testing.T) {
	eng := engine.NewEngine(nil, testLogger())
	eng.InitializeRunning(&model.RouterConfig{
		Security: &model.SecurityConfig{
			Users: map[string]*model.UserConfig{
				"admin": {Password: "$argon2id$v=19$m=8,t=1,p=1$AQ$AQ"},
			},
		},
	}, 1)
	srv := NewServer(eng, &fakeStore{commitID: "commit-1"}, testLogger())
	ctx := context.Background()

	sessionID, err := srv.CreateSession(ctx, "alice")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := srv.AcquireLock(ctx, sessionID, "alice"); err == nil {
		t.Fatal("AcquireLock() error = nil, want serialization error")
	}

	session, err := srv.sessions.Get(sessionID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	session.mu.RLock()
	hasLock := session.HasLock
	session.mu.RUnlock()
	if hasLock {
		t.Fatal("session kept lock after AcquireLock failure")
	}

	srv.sessions.mu.Lock()
	lockHeld := srv.sessions.lockHeld
	srv.sessions.mu.Unlock()
	if lockHeld != "" {
		t.Fatalf("candidate lock held by %q after AcquireLock failure, want none", lockHeld)
	}
}

func TestCommitRollsBackEngineWhenPersistenceFails(t *testing.T) {
	oldParser := ConfigTextParser
	ConfigTextParser = func(text string) (*model.RouterConfig, error) {
		cfg, err := pkgconfig.NewParser(strings.NewReader(text)).Parse()
		if err != nil {
			return nil, err
		}
		return model.FromLegacyConfig(cfg), nil
	}
	t.Cleanup(func() { ConfigTextParser = oldParser })

	eng := engine.NewEngine(nil, testLogger())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 1)
	st := &fakeStore{commitID: "commit-1", commitErr: errors.New("commit failed")}
	srv := NewServer(eng, st, testLogger())
	ctx := context.Background()
	sessionID, err := srv.CreateSession(ctx, "alice")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := srv.AcquireLock(ctx, sessionID, "alice"); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}
	if err := srv.EditCandidate(ctx, sessionID, "set system host-name router2"); err != nil {
		t.Fatalf("EditCandidate() error = %v", err)
	}
	if _, _, err := srv.Commit(ctx, sessionID, "alice", "test"); err == nil {
		t.Fatal("Commit() expected persistence error")
	}
	if !st.aborted {
		t.Fatal("Commit() did not abort prepared persistence after commit failure")
	}
	if got := eng.Running().System.HostName; got != "router1" {
		t.Fatalf("engine running hostname = %q, want rollback to router1", got)
	}
}

func TestRollbackAppliesCommitConfig(t *testing.T) {
	oldParser := ConfigTextParser
	ConfigTextParser = func(text string) (*model.RouterConfig, error) {
		cfg, err := pkgconfig.NewParser(strings.NewReader(text)).Parse()
		if err != nil {
			return nil, err
		}
		return model.FromLegacyConfig(cfg), nil
	}
	t.Cleanup(func() { ConfigTextParser = oldParser })

	eng := engine.NewEngine(nil, testLogger())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router2"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 2)
	targetCfg := &model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}
	st := &fakeStore{
		commitID: "rollback-1",
		commits: map[string]*store.CommitRecord{
			"commit-old": {CommitID: "commit-old", Config: targetCfg},
		},
	}
	srv := NewServer(eng, st, testLogger())
	ctx := context.Background()
	sessionID, err := srv.CreateSession(ctx, "alice")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := srv.AcquireLock(ctx, sessionID, "alice"); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}

	commitID, version, err := srv.Rollback(ctx, sessionID, "commit-old", "alice", "")
	if err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if commitID != "rollback-1" || version != 3 {
		t.Fatalf("Rollback() = (%q, %d), want rollback-1 version 3", commitID, version)
	}
	if got := eng.Running().System.HostName; got != "router1" {
		t.Fatalf("engine running hostname = %q, want router1", got)
	}
	candidate, err := srv.GetCandidate(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetCandidate() error = %v", err)
	}
	if !strings.Contains(candidate, "set system host-name router1") {
		t.Fatalf("candidate was not reset to rolled back config: %q", candidate)
	}
}

func TestRollbackDoesNotApplyEngineWhenPersistencePrepareFails(t *testing.T) {
	eng := engine.NewEngine(nil, testLogger())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router2"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 2)
	targetCfg := &model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}
	st := &fakeStore{
		prepareErr: errors.New("lock held"),
		commits: map[string]*store.CommitRecord{
			"commit-old": {CommitID: "commit-old", Config: targetCfg},
		},
	}
	srv := NewServer(eng, st, testLogger())
	ctx := context.Background()
	sessionID, err := srv.CreateSession(ctx, "alice")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := srv.AcquireLock(ctx, sessionID, "alice"); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}

	if _, _, err := srv.Rollback(ctx, sessionID, "commit-old", "alice", ""); err == nil {
		t.Fatal("Rollback() expected prepare error")
	}
	if got := eng.Running().System.HostName; got != "router2" {
		t.Fatalf("engine running hostname = %q, want unchanged router2", got)
	}
}

func TestRollbackRejectsNoopTarget(t *testing.T) {
	eng := engine.NewEngine(nil, testLogger())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 2)
	targetCfg := &model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}
	st := &fakeStore{
		commitID: "rollback-1",
		commits: map[string]*store.CommitRecord{
			"commit-old": {CommitID: "commit-old", Config: targetCfg},
		},
	}
	srv := NewServer(eng, st, testLogger())
	ctx := context.Background()
	sessionID, err := srv.CreateSession(ctx, "alice")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := srv.AcquireLock(ctx, sessionID, "alice"); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}

	_, _, err = srv.Rollback(ctx, sessionID, "commit-old", "alice", "")
	if err == nil || !strings.Contains(err.Error(), "rollback target matches running configuration") || !errors.Is(err, ErrConfigInput) {
		t.Fatalf("Rollback() error = %v, want no changes", err)
	}
	if st.saved != nil {
		t.Fatal("Rollback() prepared persistence for unchanged target")
	}
	if snap := eng.RunningSnapshot(); snap == nil || snap.Version != 2 {
		t.Fatalf("running snapshot = %#v, want version 2", snap)
	}
}

func TestRollbackAbortsWhenCandidateStalesAfterPrepare(t *testing.T) {
	eng := engine.NewEngine(nil, testLogger())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router2"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 2)
	targetCfg := &model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}
	ctx := context.Background()
	st := &fakeStore{
		commitID: "rollback-1",
		commits: map[string]*store.CommitRecord{
			"commit-old": {CommitID: "commit-old", Config: targetCfg},
		},
	}
	st.prepareFn = func() {
		if err := eng.Apply(ctx, &model.RouterConfig{
			System:     &model.SystemConfig{HostName: "netconf-router"},
			Interfaces: map[string]*model.InterfaceConfig{},
		}, "bob", "external commit"); err != nil {
			t.Fatalf("external Apply() error = %v", err)
		}
	}
	srv := NewServer(eng, st, testLogger())
	sessionID, err := srv.CreateSession(ctx, "alice")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := srv.AcquireLock(ctx, sessionID, "alice"); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}

	_, _, err = srv.Rollback(ctx, sessionID, "commit-old", "alice", "")
	if err == nil || !strings.Contains(err.Error(), "candidate configuration is stale") || !errors.Is(err, ErrCandidateConflict) {
		t.Fatalf("Rollback() error = %v, want stale candidate", err)
	}
	if !st.aborted {
		t.Fatal("Rollback() did not abort prepared persistence after stale recheck")
	}
	if got := eng.Running().System.HostName; got != "netconf-router" {
		t.Fatalf("running hostname = %q, want netconf-router", got)
	}
}

func TestRollbackAdapterRedactsInternalErrors(t *testing.T) {
	eng := engine.NewEngine(nil, testLogger())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router2"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 2)
	srv := NewServer(eng, &fakeStore{getCommitErr: errors.New("sqlite /var/lib/arca/config.db failed")}, testLogger())
	ctx := context.Background()
	sessionID, err := srv.CreateSession(ctx, "alice")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if err := srv.AcquireLock(ctx, sessionID, "alice"); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}

	_, err = (&configServiceAdapter{server: srv}).Rollback(ctx, &apiv1.RollbackRequest{
		SessionId: sessionID,
		CommitId:  "commit-old",
		User:      "alice",
	})
	if status.Code(err) != codes.Internal {
		t.Fatalf("Rollback() status = %v, want %v", status.Code(err), codes.Internal)
	}
	if got := status.Convert(err).Message(); got != "internal server error" {
		t.Fatalf("Rollback() status message = %q, want generic internal error", got)
	}
	if strings.Contains(err.Error(), "/var/lib/arca/config.db") {
		t.Fatalf("Rollback() error leaked backend path: %v", err)
	}
}

func TestApplyCandidateCommandPreservesOSPFInterfaceAttributes(t *testing.T) {
	candidate := strings.Join([]string{
		"set protocols ospf area 0.0.0.0 interface ge-0/0/0 passive",
		"set protocols ospf area 0.0.0.0 interface ge-0/0/0 priority 10",
	}, "\n")

	updated, err := applyCandidateCommand(candidate, "set protocols ospf area 0.0.0.0 interface ge-0/0/0 metric 20")
	if err != nil {
		t.Fatalf("applyCandidateCommand() error = %v", err)
	}
	for _, want := range []string{
		"set protocols ospf area 0.0.0.0 interface ge-0/0/0 passive",
		"set protocols ospf area 0.0.0.0 interface ge-0/0/0 priority 10",
		"set protocols ospf area 0.0.0.0 interface ge-0/0/0 metric 20",
	} {
		if !strings.Contains(updated, want) {
			t.Fatalf("updated candidate missing %q:\n%s", want, updated)
		}
	}
}

func TestApplyCandidateCommandReplacesBFDAttributes(t *testing.T) {
	candidate := strings.Join([]string{
		"set protocols bfd profile fast receive-interval 150",
		"set protocols bfd profile fast transmit-interval 150",
		"set protocols bfd profile fast detect-multiplier 3",
		"set protocols bfd profile fast passive-mode",
		"set protocols bfd peer 192.0.2.2 local-address 192.0.2.1",
		"set protocols bfd peer 192.0.2.2 profile fast",
		"set protocols bfd peer 192.0.2.2 multihop",
		"set protocols bfd peer 192.0.2.2 shutdown",
		"set protocols bgp group EBGP neighbor 192.0.2.2 bfd",
		"set protocols ospf area 0.0.0.0 interface ge-0/0/0 bfd",
		"set protocols ospf3 area 0.0.0.0 interface ge-0/0/0 bfd profile fast",
	}, "\n")

	updated, err := applyCandidateCommand(candidate, strings.Join([]string{
		"set protocols bfd profile fast receive-interval 300",
		"set protocols bfd profile fast detect-multiplier 5",
		"set protocols bfd peer 192.0.2.2 local-address 192.0.2.10",
		"set protocols bfd peer 192.0.2.2 profile slow",
		"set protocols bgp group EBGP neighbor 192.0.2.2 bfd profile slow",
		"set protocols ospf area 0.0.0.0 interface ge-0/0/0 bfd profile slow",
		"set protocols ospf3 area 0.0.0.0 interface ge-0/0/0 bfd",
	}, "\n"))
	if err != nil {
		t.Fatalf("applyCandidateCommand() error = %v", err)
	}
	updatedLines := strings.Split(updated, "\n")
	for _, oldLine := range []string{
		"set protocols bfd profile fast receive-interval 150",
		"set protocols bfd profile fast detect-multiplier 3",
		"set protocols bfd peer 192.0.2.2 local-address 192.0.2.1",
		"set protocols bfd peer 192.0.2.2 profile fast",
		"set protocols bgp group EBGP neighbor 192.0.2.2 bfd",
		"set protocols ospf area 0.0.0.0 interface ge-0/0/0 bfd",
		"set protocols ospf3 area 0.0.0.0 interface ge-0/0/0 bfd profile fast",
	} {
		if containsLine(updatedLines, oldLine) {
			t.Fatalf("updated candidate retained old line %q:\n%s", oldLine, updated)
		}
	}
	for _, want := range []string{
		"set protocols bfd profile fast receive-interval 300",
		"set protocols bfd profile fast transmit-interval 150",
		"set protocols bfd profile fast detect-multiplier 5",
		"set protocols bfd profile fast passive-mode",
		"set protocols bfd peer 192.0.2.2 local-address 192.0.2.10",
		"set protocols bfd peer 192.0.2.2 profile slow",
		"set protocols bfd peer 192.0.2.2 multihop",
		"set protocols bfd peer 192.0.2.2 shutdown",
		"set protocols bgp group EBGP neighbor 192.0.2.2 bfd profile slow",
		"set protocols ospf area 0.0.0.0 interface ge-0/0/0 bfd profile slow",
		"set protocols ospf3 area 0.0.0.0 interface ge-0/0/0 bfd",
	} {
		if !containsLine(updatedLines, want) {
			t.Fatalf("updated candidate missing %q:\n%s", want, updated)
		}
		if got := countCandidateLines(updatedLines, want); got != 1 {
			t.Fatalf("updated candidate contains %q %d times, want 1:\n%s", want, got, updated)
		}
	}
}

func countCandidateLines(lines []string, target string) int {
	count := 0
	for _, line := range lines {
		if line == target {
			count++
		}
	}
	return count
}

func TestApplyCandidateCommandReplacesV06ScalarAttributes(t *testing.T) {
	candidate := strings.Join([]string{
		"set system services web-ui port 8080",
		"set system services prometheus port 9090",
		"set system services snmp community old-monitoring",
		"set security netconf ssh port 830",
		"set protocols vrrp group 10 priority 100",
		"set routing-instances BLUE route-distinguisher 65000:100",
		"set routing-instances BLUE vrf-target target:65000:100",
		"set routing-instances BLUE vrf-target import target:65000:101",
		"set routing-instances BLUE vrf-target export target:65000:102",
		"set class-of-service traffic-control-profile WAN shaping-rate 1000",
	}, "\n")

	updated, err := applyCandidateCommand(candidate, strings.Join([]string{
		"set system services web-ui port 8443",
		"set system services prometheus port 19090",
		"set system services snmp community monitoring",
		"set security netconf ssh port 1830",
		"set protocols vrrp group 10 priority 120",
		"set routing-instances BLUE route-distinguisher 65000:200",
		"set routing-instances BLUE vrf-target target:65000:200",
		"set class-of-service traffic-control-profile WAN shaping-rate 2000",
	}, "\n"))
	if err != nil {
		t.Fatalf("applyCandidateCommand() error = %v", err)
	}
	for _, oldLine := range []string{
		"set system services web-ui port 8080",
		"set system services prometheus port 9090",
		"set system services snmp community old-monitoring",
		"set security netconf ssh port 830",
		"set protocols vrrp group 10 priority 100",
		"set routing-instances BLUE route-distinguisher 65000:100",
		"set routing-instances BLUE vrf-target target:65000:100",
		"set class-of-service traffic-control-profile WAN shaping-rate 1000",
	} {
		if strings.Contains(updated, oldLine) {
			t.Fatalf("updated candidate retained old line %q:\n%s", oldLine, updated)
		}
	}
	for _, want := range []string{
		"set system services web-ui port 8443",
		"set system services prometheus port 19090",
		"set system services snmp community monitoring",
		"set security netconf ssh port 1830",
		"set protocols vrrp group 10 priority 120",
		"set routing-instances BLUE route-distinguisher 65000:200",
		"set routing-instances BLUE vrf-target target:65000:200",
		"set routing-instances BLUE vrf-target import target:65000:101",
		"set routing-instances BLUE vrf-target export target:65000:102",
		"set class-of-service traffic-control-profile WAN shaping-rate 2000",
	} {
		if !strings.Contains(updated, want) {
			t.Fatalf("updated candidate missing %q:\n%s", want, updated)
		}
	}
}

func TestApplyCandidateCommandPreservesRoutingInstancePolicyLists(t *testing.T) {
	candidate := strings.Join([]string{
		"set routing-instances BLUE vrf-target import target:65000:101",
		"set routing-instances BLUE vrf-target export target:65000:102",
		"set routing-instances BLUE vrf-import BLUE-IN",
		"set routing-instances BLUE vrf-import BLUE-EXTRA",
		"set routing-instances BLUE vrf-export BLUE-OUT",
	}, "\n")

	updated, err := applyCandidateCommand(candidate, strings.Join([]string{
		"set routing-instances BLUE vrf-target import target:65000:101",
		"set routing-instances BLUE vrf-target import target:65000:111",
		"set routing-instances BLUE vrf-target export target:65000:102",
		"set routing-instances BLUE vrf-target export target:65000:112",
		"set routing-instances BLUE vrf-import BLUE-IN",
		"set routing-instances BLUE vrf-import BLUE-NEW",
		"set routing-instances BLUE vrf-export BLUE-OUT",
		"set routing-instances BLUE vrf-export BLUE-NEW",
	}, "\n"))
	if err != nil {
		t.Fatalf("applyCandidateCommand() error = %v", err)
	}

	for _, want := range []string{
		"set routing-instances BLUE vrf-target import target:65000:101",
		"set routing-instances BLUE vrf-target import target:65000:111",
		"set routing-instances BLUE vrf-target export target:65000:102",
		"set routing-instances BLUE vrf-target export target:65000:112",
		"set routing-instances BLUE vrf-import BLUE-IN",
		"set routing-instances BLUE vrf-import BLUE-EXTRA",
		"set routing-instances BLUE vrf-import BLUE-NEW",
		"set routing-instances BLUE vrf-export BLUE-OUT",
		"set routing-instances BLUE vrf-export BLUE-NEW",
	} {
		if !strings.Contains(updated, want) {
			t.Fatalf("updated candidate missing %q:\n%s", want, updated)
		}
		if got := strings.Count(updated, want); got != 1 {
			t.Fatalf("updated candidate contains %q %d times, want 1:\n%s", want, got, updated)
		}
	}
}
