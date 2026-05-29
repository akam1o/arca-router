package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	grpcclient "github.com/akam1o/arca-router/internal/northbound/grpc"
)

func TestDialGRPCRejectsTLSFlagsWithoutAddress(t *testing.T) {
	_, err := dialGRPC(&cliFlags{grpcCAFile: "/ca.crt"})
	if err == nil {
		t.Fatal("dialGRPC() error = nil, want TLS flag usage error")
	}
	if !strings.Contains(err.Error(), "-grpc-address") {
		t.Fatalf("dialGRPC() error = %v, want -grpc-address", err)
	}
}

type fakeInteractiveClient struct {
	acquireLockErr        error
	createSessionErr      error
	commitErr             error
	discardErr            error
	closeSessionErr       error
	releaseLockErr        error
	replaceErr            error
	createSessionID       string
	history               []grpcclient.CommitInfo
	commitDetails         map[string]grpcclient.CommitInfo
	listHistoryErr        error
	getCommitErr          error
	runningText           string
	runningUnredactedText string
	runningVersion        uint64
	candidateText         string
	routeText             string
	routeProtocol         string
	routeFamily           string
	routePrefix           string
	routeStateProto       string
	routes                []grpcclient.RouteInfo
	routingInstances      []grpcclient.RoutingInstanceInfo
	bgpNeighbors          []grpcclient.BGPNeighborInfo
	bgpSummaryText        string
	bgpNeighborText       string
	ospfNeighbors         []grpcclient.OSPFNeighborInfo
	ospfText              string
	ospfFamily            string
	vrrpText              string
	bfdText               string
	bfdInfo               *grpcclient.BFDStatusInfo
	bfdPeerAddress        string
	bfdBrief              bool
	bfdCounters           bool
	lcpInfo               *grpcclient.LCPReconciliationInfo
	haInfo                *grpcclient.HAStatusInfo
	cosInfo               *grpcclient.ClassOfServiceInfo
	cosErr                error
	telemetryCatalog      grpcclient.TelemetryCatalog
	telemetryEvents       []*grpcclient.TelemetryEvent
	diffText              string
	diffHasChanges        bool
	diffErr               error

	createSessionCalls            int
	closeSessionCalls             int
	acquireLockCalls              int
	discardCalls                  int
	releaseLockCalls              int
	commitCalls                   int
	diffCalls                     int
	routeCalls                    int
	bfdStatusCalls                int
	cosCalls                      int
	routingCalls                  int
	bgpNeighborCalls              int
	ospfNeighborCalls             int
	listHistoryCalls              int
	listHistoryLimit              int
	listHistoryOffset             int
	getCommitCalls                int
	getCommitID                   string
	getRunningCalls               int
	getRunningUnredactedCalls     int
	getCandidateCalls             int
	rollbackCalls                 int
	telemetryCatalogCalls         int
	filteredTelemetryCatalogCalls int
	telemetryCalls                int
	validateCalls                 int
	editTexts                     []string
	replaceTexts                  []string
	telemetryCatalogPaths         []string
	telemetryCatalogCardinalities []string
	telemetryCatalogSchemas       []string
	telemetryCatalogEncodings     []string
	telemetryCatalogDefaultOnly   bool
	telemetryPaths                []string
	telemetryInterval             time.Duration
	telemetryOnce                 bool
}

func (f *fakeInteractiveClient) CreateSession(ctx context.Context, user string) (string, error) {
	f.createSessionCalls++
	if f.createSessionErr != nil {
		return "", f.createSessionErr
	}
	if f.createSessionID != "" {
		return f.createSessionID, nil
	}
	return "session-1", nil
}

func (f *fakeInteractiveClient) CloseSession(ctx context.Context, sessionID string) error {
	f.closeSessionCalls++
	return f.closeSessionErr
}

func (f *fakeInteractiveClient) GetRunning(ctx context.Context) (string, uint64, error) {
	f.getRunningCalls++
	return f.runningText, f.runningVersion, nil
}

func (f *fakeInteractiveClient) GetRunningUnredacted(ctx context.Context) (string, uint64, error) {
	f.getRunningUnredactedCalls++
	if f.runningUnredactedText != "" {
		return f.runningUnredactedText, f.runningVersion, nil
	}
	return f.runningText, f.runningVersion, nil
}

func (f *fakeInteractiveClient) GetCandidate(ctx context.Context, sessionID string) (string, error) {
	f.getCandidateCalls++
	return f.candidateText, nil
}

func (f *fakeInteractiveClient) EditCandidate(ctx context.Context, sessionID, configText string) error {
	f.editTexts = append(f.editTexts, configText)
	return nil
}

func (f *fakeInteractiveClient) ReplaceCandidate(ctx context.Context, sessionID, configText string) error {
	f.replaceTexts = append(f.replaceTexts, configText)
	return f.replaceErr
}

func (f *fakeInteractiveClient) Commit(ctx context.Context, sessionID, user, message string) (string, uint64, error) {
	f.commitCalls++
	if f.commitErr != nil {
		return "", 0, f.commitErr
	}
	return "commit-1234567890", 2, nil
}

func (f *fakeInteractiveClient) ValidateCandidate(ctx context.Context, sessionID string) error {
	f.validateCalls++
	return nil
}

func (f *fakeInteractiveClient) Discard(ctx context.Context, sessionID string) error {
	f.discardCalls++
	return f.discardErr
}

func (f *fakeInteractiveClient) Rollback(ctx context.Context, sessionID, commitID, user, message string) (string, uint64, error) {
	f.rollbackCalls++
	return "rollback-1234567890", 3, nil
}

func (f *fakeInteractiveClient) Diff(ctx context.Context, sessionID string) (string, bool, error) {
	f.diffCalls++
	return f.diffText, f.diffHasChanges, f.diffErr
}

func (f *fakeInteractiveClient) ListHistory(ctx context.Context, limit, offset int) ([]grpcclient.CommitInfo, error) {
	f.listHistoryCalls++
	f.listHistoryLimit = limit
	f.listHistoryOffset = offset
	if f.listHistoryErr != nil {
		return nil, f.listHistoryErr
	}
	return f.history, nil
}

func (f *fakeInteractiveClient) GetCommit(ctx context.Context, commitID string) (grpcclient.CommitInfo, error) {
	f.getCommitCalls++
	f.getCommitID = commitID
	if f.getCommitErr != nil {
		return grpcclient.CommitInfo{}, f.getCommitErr
	}
	if f.commitDetails != nil {
		return f.commitDetails[commitID], nil
	}
	for _, entry := range f.history {
		if entry.CommitID == commitID {
			return entry, nil
		}
	}
	return grpcclient.CommitInfo{CommitID: commitID}, nil
}

func (f *fakeInteractiveClient) AcquireLock(ctx context.Context, sessionID, user string) error {
	f.acquireLockCalls++
	return f.acquireLockErr
}

func (f *fakeInteractiveClient) ReleaseLock(ctx context.Context, sessionID string) error {
	f.releaseLockCalls++
	return f.releaseLockErr
}

func (f *fakeInteractiveClient) GetInterfaces(ctx context.Context, nameFilter string) ([]grpcclient.InterfaceInfo, error) {
	return nil, nil
}

func (f *fakeInteractiveClient) GetRoutes(ctx context.Context, prefixFilter, protoFilter string) ([]grpcclient.RouteInfo, error) {
	f.routeCalls++
	f.routePrefix = prefixFilter
	f.routeStateProto = protoFilter
	return f.routes, nil
}

func (f *fakeInteractiveClient) GetRoutingInstances(ctx context.Context) ([]grpcclient.RoutingInstanceInfo, error) {
	f.routingCalls++
	return f.routingInstances, nil
}

func (f *fakeInteractiveClient) GetBGPNeighbors(ctx context.Context) ([]grpcclient.BGPNeighborInfo, error) {
	f.bgpNeighborCalls++
	return f.bgpNeighbors, nil
}

func (f *fakeInteractiveClient) GetOSPFNeighbors(ctx context.Context, addressFamily string) ([]grpcclient.OSPFNeighborInfo, error) {
	f.ospfNeighborCalls++
	f.ospfFamily = addressFamily
	return f.ospfNeighbors, nil
}

func (f *fakeInteractiveClient) GetRouteText(ctx context.Context, protoFilter, addressFamily string) (string, error) {
	f.routeProtocol = protoFilter
	f.routeFamily = addressFamily
	if f.routeText == "" {
		return "route output\n", nil
	}
	return f.routeText, nil
}

func (f *fakeInteractiveClient) GetBGPSummaryText(ctx context.Context) (string, error) {
	if f.bgpSummaryText == "" {
		return "bgp summary output\n", nil
	}
	return f.bgpSummaryText, nil
}

func (f *fakeInteractiveClient) GetBGPNeighborText(ctx context.Context, peerAddress string) (string, error) {
	if f.bgpNeighborText == "" {
		return "bgp neighbor output\n", nil
	}
	return f.bgpNeighborText, nil
}

func (f *fakeInteractiveClient) GetOSPFNeighborsText(ctx context.Context, addressFamily string) (string, error) {
	f.ospfFamily = addressFamily
	if f.ospfText == "" {
		return "ospf neighbor output\n", nil
	}
	return f.ospfText, nil
}

func (f *fakeInteractiveClient) GetVRRPText(ctx context.Context) (string, error) {
	if f.vrrpText == "" {
		return "vrrp output\n", nil
	}
	return f.vrrpText, nil
}

func (f *fakeInteractiveClient) GetBFDText(ctx context.Context, peerAddress string, brief, counters bool) (string, error) {
	f.bfdPeerAddress = peerAddress
	f.bfdBrief = brief
	f.bfdCounters = counters
	if f.bfdText == "" {
		return "bfd output\n", nil
	}
	return f.bfdText, nil
}

func (f *fakeInteractiveClient) GetBFDStatus(ctx context.Context) (*grpcclient.BFDStatusInfo, error) {
	f.bfdStatusCalls++
	if f.bfdInfo != nil {
		return f.bfdInfo, nil
	}
	return &grpcclient.BFDStatusInfo{}, nil
}

func (f *fakeInteractiveClient) GetLCPReconciliation(ctx context.Context) (*grpcclient.LCPReconciliationInfo, error) {
	if f.lcpInfo != nil {
		return f.lcpInfo, nil
	}
	return &grpcclient.LCPReconciliationInfo{}, nil
}

func (f *fakeInteractiveClient) GetHAStatus(ctx context.Context) (*grpcclient.HAStatusInfo, error) {
	if f.haInfo != nil {
		return f.haInfo, nil
	}
	return &grpcclient.HAStatusInfo{}, nil
}

func (f *fakeInteractiveClient) GetClassOfService(ctx context.Context) (*grpcclient.ClassOfServiceInfo, error) {
	f.cosCalls++
	if f.cosErr != nil {
		return nil, f.cosErr
	}
	if f.cosInfo != nil {
		return f.cosInfo, nil
	}
	return &grpcclient.ClassOfServiceInfo{}, nil
}

func (f *fakeInteractiveClient) GetTelemetryCatalog(ctx context.Context) (grpcclient.TelemetryCatalog, error) {
	f.telemetryCatalogCalls++
	if len(f.telemetryCatalog.Paths) > 0 || len(f.telemetryCatalog.DefaultPaths) > 0 ||
		f.telemetryCatalog.EventSchemaVersion != "" || f.telemetryCatalog.Encoding != "" {
		return f.telemetryCatalog, nil
	}
	return grpcclient.NewTelemetryCatalog(), nil
}

func (f *fakeInteractiveClient) GetFilteredTelemetryCatalog(ctx context.Context, cardinalities []string, payloadSchemas []string) (grpcclient.TelemetryCatalog, error) {
	return f.GetPathFilteredTelemetryCatalog(ctx, nil, cardinalities, payloadSchemas)
}

func (f *fakeInteractiveClient) GetPathFilteredTelemetryCatalog(ctx context.Context, paths []string, cardinalities []string, payloadSchemas []string) (grpcclient.TelemetryCatalog, error) {
	return f.GetTelemetryCatalogWithFilter(ctx, grpcclient.TelemetryCatalogFilter{
		Paths:          paths,
		Cardinalities:  cardinalities,
		PayloadSchemas: payloadSchemas,
	})
}

func (f *fakeInteractiveClient) GetTelemetryCatalogWithFilter(ctx context.Context, filter grpcclient.TelemetryCatalogFilter) (grpcclient.TelemetryCatalog, error) {
	f.filteredTelemetryCatalogCalls++
	f.telemetryCatalogPaths = append([]string(nil), filter.Paths...)
	f.telemetryCatalogCardinalities = append([]string(nil), filter.Cardinalities...)
	f.telemetryCatalogSchemas = append([]string(nil), filter.PayloadSchemas...)
	f.telemetryCatalogEncodings = append([]string(nil), filter.Encodings...)
	f.telemetryCatalogDefaultOnly = filter.DefaultOnly
	if len(f.telemetryCatalog.Paths) > 0 || len(f.telemetryCatalog.DefaultPaths) > 0 ||
		f.telemetryCatalog.EventSchemaVersion != "" || f.telemetryCatalog.Encoding != "" {
		catalog := f.telemetryCatalog
		catalog.Paths = filterTelemetryPathCatalog(catalog.Paths, telemetryCatalogCLIOptions{
			defaultOnly:    filter.DefaultOnly,
			paths:          filter.Paths,
			cardinalities:  filter.Cardinalities,
			payloadSchemas: filter.PayloadSchemas,
			encodings:      filter.Encodings,
		})
		return catalog, nil
	}
	return grpcclient.NewFilteredTelemetryCatalog(filter), nil
}

func (f *fakeInteractiveClient) SubscribeTelemetry(ctx context.Context, paths []string, sampleInterval time.Duration, once bool) (grpcclient.TelemetryReceiver, error) {
	f.telemetryCalls++
	f.telemetryPaths = append([]string(nil), paths...)
	f.telemetryInterval = sampleInterval
	f.telemetryOnce = once
	return &fakeTelemetryStream{events: append([]*grpcclient.TelemetryEvent(nil), f.telemetryEvents...)}, nil
}

type fakeTelemetryStream struct {
	events []*grpcclient.TelemetryEvent
}

func (s *fakeTelemetryStream) Recv() (*grpcclient.TelemetryEvent, error) {
	if len(s.events) == 0 {
		return nil, io.EOF
	}
	event := s.events[0]
	s.events = s.events[1:]
	return event, nil
}

func evpnTelemetryTestEvent() *grpcclient.TelemetryEvent {
	return &grpcclient.TelemetryEvent{
		Sequence:      1,
		Timestamp:     time.Unix(100, 0).UTC(),
		Path:          "/overlays/evpn",
		Cardinality:   "per-vni",
		PayloadSchema: "arca.telemetry.overlays.evpn.v1",
		EventType:     "snapshot",
		Encoding:      "json",
		SchemaVersion: "arca.telemetry.v1",
		JSONPayload: `{"vnis":[` +
			`{"vni":10010,"type":"l2","bridge_domain":"BD-10","vlan_id":10,"route_distinguisher":"65000:10010","vrf_target":"target:65000:10010","source_interface":"ge-0/0/0","source_address":"192.0.2.1","multicast_group":"239.0.0.10"},` +
			`{"vni":20010,"type":"l3","routing_instance":"BLUE","vrf_target_import":["target:65000:20010"],"vrf_target_export":["target:65000:20011"]}` +
			`]}`,
	}
}

func TestCmdConfigureCreatesSessionLazily(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{createSessionID: "session-2"}
	sh := &interactiveShell{
		client:   client,
		hostname: "router",
		mode:     modeOperational,
	}

	err := sh.cmdConfigure(ctx)
	if err != nil {
		t.Fatalf("cmdConfigure() error = %v", err)
	}
	if sh.sessionID != "session-2" {
		t.Fatalf("sessionID = %q, want session-2", sh.sessionID)
	}
	if sh.mode != modeConfiguration {
		t.Fatalf("mode = %v, want configuration", sh.mode)
	}
	if client.createSessionCalls != 1 {
		t.Fatalf("CreateSession calls = %d, want 1", client.createSessionCalls)
	}
	if client.acquireLockCalls != 1 {
		t.Fatalf("AcquireLock calls = %d, want 1", client.acquireLockCalls)
	}
}

func TestCmdConfigureStopsOnSessionCreateFailure(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{createSessionErr: errors.New("permission denied")}
	sh := &interactiveShell{
		client:   client,
		hostname: "router",
		mode:     modeOperational,
	}

	err := sh.cmdConfigure(ctx)
	if err == nil || !strings.Contains(err.Error(), "failed to create configuration session") {
		t.Fatalf("cmdConfigure() error = %v, want session create failure", err)
	}
	if sh.mode != modeOperational {
		t.Fatalf("mode = %v, want operational", sh.mode)
	}
	if client.acquireLockCalls != 0 {
		t.Fatalf("AcquireLock calls = %d, want 0", client.acquireLockCalls)
	}
}

func TestExitConfigurationModeStopsOnDiscardFailure(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{discardErr: errors.New("discard failed")}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeConfiguration,
		sessionID: "session-1",
		hasLock:   true,
		editPath:  []string{"system"},
	}

	err := sh.exitConfigurationMode(ctx)
	if err == nil || !strings.Contains(err.Error(), "discard changes") {
		t.Fatalf("exitConfigurationMode() error = %v, want discard failure", err)
	}
	if client.releaseLockCalls != 0 {
		t.Fatalf("ReleaseLock calls = %d, want 0", client.releaseLockCalls)
	}
	if sh.mode != modeConfiguration || !sh.hasLock || len(sh.editPath) == 0 {
		t.Fatal("configuration state changed after discard failure")
	}
}

func TestExitConfigurationModeKeepsStateOnReleaseFailure(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{releaseLockErr: errors.New("release failed")}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeConfiguration,
		sessionID: "session-1",
		hasLock:   true,
		editPath:  []string{"system"},
	}

	err := sh.exitConfigurationMode(ctx)
	if err == nil || !strings.Contains(err.Error(), "release candidate lock") {
		t.Fatalf("exitConfigurationMode() error = %v, want release failure", err)
	}
	if client.discardCalls != 1 || client.releaseLockCalls != 1 {
		t.Fatalf("discard/release calls = %d/%d, want 1/1", client.discardCalls, client.releaseLockCalls)
	}
	if sh.mode != modeConfiguration || !sh.hasLock || len(sh.editPath) == 0 {
		t.Fatal("configuration state changed after release failure")
	}
}

func TestExitConfigurationModeResetsState(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeConfiguration,
		sessionID: "session-1",
		hasLock:   true,
		editPath:  []string{"system"},
	}

	if err := sh.exitConfigurationMode(ctx); err != nil {
		t.Fatalf("exitConfigurationMode() error = %v", err)
	}
	if sh.mode != modeOperational || sh.hasLock || len(sh.editPath) != 0 {
		t.Fatal("configuration state was not reset after successful exit")
	}
}

func TestCommitAndQuitKeepsConfigurationModeOnReleaseFailure(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{releaseLockErr: errors.New("release failed")}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeConfiguration,
		sessionID: "session-1",
		hasLock:   true,
		editPath:  []string{"system"},
	}

	err := sh.cmdCommit(ctx, []string{"and-quit"})
	if err == nil || !strings.Contains(err.Error(), "commit complete but failed to exit configuration mode") {
		t.Fatalf("cmdCommit() error = %v, want release failure after commit", err)
	}
	if client.commitCalls != 1 || client.releaseLockCalls != 1 {
		t.Fatalf("commit/release calls = %d/%d, want 1/1", client.commitCalls, client.releaseLockCalls)
	}
	if sh.mode != modeConfiguration || !sh.hasLock || len(sh.editPath) == 0 {
		t.Fatal("configuration state changed after commit and-quit release failure")
	}
}

func TestCommitFailureCollectsDiagnostics(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{
		commitErr:      errors.New("apply failed"),
		diffText:       "- set protocols bgp group external neighbor 198.51.100.2 peer-as 65001",
		diffHasChanges: true,
	}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeConfiguration,
		sessionID: "session-1",
		hasLock:   true,
	}

	err := sh.cmdCommit(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "commit failed: apply failed") {
		t.Fatalf("cmdCommit() error = %v, want commit failure", err)
	}
	if client.commitCalls != 1 || client.diffCalls != 1 {
		t.Fatalf("commit/diff calls = %d/%d, want 1/1", client.commitCalls, client.diffCalls)
	}
}

func TestCommitFailureReportsDiagnosticCollectionError(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{
		commitErr: errors.New("apply failed"),
		diffErr:   errors.New("diff unavailable"),
	}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeConfiguration,
		sessionID: "session-1",
		hasLock:   true,
	}

	err := sh.cmdCommit(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "diagnostics unavailable: diff unavailable") {
		t.Fatalf("cmdCommit() error = %v, want diagnostics unavailable detail", err)
	}
	if client.commitCalls != 1 || client.diffCalls != 1 {
		t.Fatalf("commit/diff calls = %d/%d, want 1/1", client.commitCalls, client.diffCalls)
	}
}

func TestCommitRunsClassOfServicePostCommitDiagnostics(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{
		diffText:       "+ set class-of-service interfaces ge-0/0/0 output-traffic-control-profile WAN",
		diffHasChanges: true,
		history: []grpcclient.CommitInfo{
			{CommitID: "1234567890abcdef"},
		},
		commitDetails: map[string]grpcclient.CommitInfo{
			"1234567890abcdef": {CommitID: "1234567890abcdef", ConfigText: "set system host-name router"},
		},
		cosInfo: &grpcclient.ClassOfServiceInfo{
			EnforcementStatus: "intent-only",
			Interfaces: []grpcclient.ClassOfServiceInterfaceInfo{
				{Name: "ge-0/0/0", OutputTrafficControlProfile: "WAN"},
			},
			Capabilities: &grpcclient.ClassOfServiceCapabilitiesInfo{
				MetadataBindingSupported: true,
				Diagnostics:              []string{"scheduler unavailable"},
			},
		},
	}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeConfiguration,
		sessionID: "session-1",
		hasLock:   true,
	}

	if err := sh.cmdCommit(ctx, nil); err != nil {
		t.Fatalf("cmdCommit() error = %v", err)
	}
	if client.commitCalls != 1 || client.diffCalls != 1 || client.listHistoryCalls != 1 || client.getCommitCalls != 1 || client.cosCalls != 1 {
		t.Fatalf("commit/diff/history/archive/cos calls = %d/%d/%d/%d/%d, want 1/1/1/1/1",
			client.commitCalls, client.diffCalls, client.listHistoryCalls, client.getCommitCalls, client.cosCalls)
	}
}

func TestCommitSkipsClassOfServicePostCommitDiagnosticsWithoutCosDiff(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{
		diffText:       "+ set routing-options static route 198.51.100.0/24 next-hop 192.0.2.1",
		diffHasChanges: true,
		history: []grpcclient.CommitInfo{
			{CommitID: "1234567890abcdef"},
		},
		commitDetails: map[string]grpcclient.CommitInfo{
			"1234567890abcdef": {CommitID: "1234567890abcdef", ConfigText: "set system host-name router"},
		},
	}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeConfiguration,
		sessionID: "session-1",
		hasLock:   true,
	}

	if err := sh.cmdCommit(ctx, nil); err != nil {
		t.Fatalf("cmdCommit() error = %v", err)
	}
	if client.commitCalls != 1 || client.diffCalls != 1 || client.listHistoryCalls != 1 || client.cosCalls != 0 {
		t.Fatalf("commit/diff/history/cos calls = %d/%d/%d/%d, want 1/1/1/0",
			client.commitCalls, client.diffCalls, client.listHistoryCalls, client.cosCalls)
	}
}

func TestCommitRollbackArchiveWarnings(t *testing.T) {
	tests := []struct {
		name   string
		client *fakeInteractiveClient
		want   string
	}{
		{
			name: "valid archive",
			client: &fakeInteractiveClient{
				history: []grpcclient.CommitInfo{
					{CommitID: "1234567890abcdef"},
				},
				commitDetails: map[string]grpcclient.CommitInfo{
					"1234567890abcdef": {CommitID: "1234567890abcdef", ConfigText: "set system host-name router"},
				},
			},
			want: "",
		},
		{
			name:   "missing archive",
			client: &fakeInteractiveClient{},
			want:   "no rollback archive entry is available",
		},
		{
			name: "empty config text",
			client: &fakeInteractiveClient{
				history: []grpcclient.CommitInfo{{CommitID: "1234567890abcdef"}},
				commitDetails: map[string]grpcclient.CommitInfo{
					"1234567890abcdef": {CommitID: "1234567890abcdef"},
				},
			},
			want: "latest rollback archive has no config text",
		},
		{
			name: "invalid config text",
			client: &fakeInteractiveClient{
				history: []grpcclient.CommitInfo{
					{CommitID: "1234567890abcdef"},
				},
				commitDetails: map[string]grpcclient.CommitInfo{
					"1234567890abcdef": {CommitID: "1234567890abcdef", ConfigText: "set system host-name bad_name"},
				},
			},
			want: "latest rollback archive validation failed",
		},
		{
			name: "history unavailable",
			client: &fakeInteractiveClient{
				listHistoryErr: errors.New("history unavailable"),
			},
			want: "rollback archive check failed: history unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings := commitRollbackArchiveWarnings(context.Background(), tt.client)
			if tt.want == "" {
				if len(warnings) != 0 {
					t.Fatalf("commitRollbackArchiveWarnings() = %#v, want none", warnings)
				}
				return
			}
			got := strings.Join(warnings, "\n")
			if !strings.Contains(got, tt.want) {
				t.Fatalf("commitRollbackArchiveWarnings() = %#v, want substring %q", warnings, tt.want)
			}
			if tt.client.listHistoryCalls != 1 || tt.client.listHistoryLimit != 1 {
				t.Fatalf("ListHistory calls/limit = %d/%d, want 1/1", tt.client.listHistoryCalls, tt.client.listHistoryLimit)
			}
		})
	}
}

func TestCmdSetQuotesValuesWithSpaces(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeConfiguration,
		sessionID: "session-1",
		hasLock:   true,
	}

	if err := sh.processCommand(ctx, `set interfaces ge-0/0/0 description "WAN Uplink"`); err != nil {
		t.Fatalf("processCommand() error = %v", err)
	}
	if len(client.editTexts) != 1 {
		t.Fatalf("EditCandidate calls = %d, want 1", len(client.editTexts))
	}
	want := `set interfaces ge-0/0/0 description "WAN Uplink"`
	if got := client.editTexts[0]; got != want {
		t.Fatalf("EditCandidate config = %q, want %q", got, want)
	}
}

func TestProcessCommandSplitsTabsLikeSharedTokenizer(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeConfiguration,
		sessionID: "session-1",
		hasLock:   true,
	}

	if err := sh.processCommand(ctx, "set\tinterfaces\tge-0/0/0\tdescription\tWAN"); err != nil {
		t.Fatalf("processCommand() error = %v", err)
	}
	if len(client.editTexts) != 1 {
		t.Fatalf("EditCandidate calls = %d, want 1", len(client.editTexts))
	}
	want := "set interfaces ge-0/0/0 description WAN"
	if got := client.editTexts[0]; got != want {
		t.Fatalf("EditCandidate config = %q, want %q", got, want)
	}
}

func TestProcessCommandRejectsUnmatchedQuote(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeConfiguration,
		sessionID: "session-1",
		hasLock:   true,
	}

	err := sh.processCommand(ctx, `set interfaces ge-0/0/0 description "WAN`)
	if err == nil || !strings.Contains(err.Error(), "unmatched quote") {
		t.Fatalf("processCommand() error = %v, want unmatched quote", err)
	}
	if len(client.editTexts) != 0 {
		t.Fatalf("EditCandidate calls = %d, want 0", len(client.editTexts))
	}
}

func TestCommitCheckRejectsComment(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeConfiguration,
		sessionID: "session-1",
		hasLock:   true,
	}

	err := sh.cmdCommit(ctx, []string{"check", "comment", "dry run"})
	if err == nil || !strings.Contains(err.Error(), "'check' and 'comment' cannot be used together") {
		t.Fatalf("cmdCommit(check comment) error = %v, want invalid option combination", err)
	}
	if client.validateCalls != 0 || client.commitCalls != 0 {
		t.Fatalf("validate/commit calls = %d/%d, want 0/0", client.validateCalls, client.commitCalls)
	}
}

func TestCommitCheckBuildsChangeImpactPreview(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{
		diffText:       "+ set routing-options static route 0.0.0.0/0 next-hop 198.51.100.2",
		diffHasChanges: true,
	}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeConfiguration,
		sessionID: "session-1",
		hasLock:   true,
	}

	if err := sh.cmdCommit(ctx, []string{"check"}); err != nil {
		t.Fatalf("cmdCommit(check) error = %v", err)
	}
	if client.validateCalls != 1 || client.diffCalls != 1 || client.commitCalls != 0 {
		t.Fatalf("validate/diff/commit calls = %d/%d/%d, want 1/1/0", client.validateCalls, client.diffCalls, client.commitCalls)
	}
}

func TestCommitCheckRunsClassOfServicePreflight(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{
		diffText:       "+ set class-of-service interfaces ge-0/0/0 output-traffic-control-profile WAN",
		diffHasChanges: true,
		cosInfo: &grpcclient.ClassOfServiceInfo{
			Capabilities: &grpcclient.ClassOfServiceCapabilitiesInfo{
				MetadataBindingSupported: true,
				Diagnostics:              []string{"scheduler unavailable"},
			},
		},
	}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeConfiguration,
		sessionID: "session-1",
		hasLock:   true,
	}

	if err := sh.cmdCommit(ctx, []string{"check"}); err != nil {
		t.Fatalf("cmdCommit(check) error = %v", err)
	}
	if client.validateCalls != 1 || client.diffCalls != 1 || client.cosCalls != 1 || client.commitCalls != 0 {
		t.Fatalf("validate/diff/cos/commit calls = %d/%d/%d/%d, want 1/1/1/0", client.validateCalls, client.diffCalls, client.cosCalls, client.commitCalls)
	}
}

func TestCommitCheckSkipsClassOfServicePreflightWithoutCosDiff(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{
		diffText:       "+ set routing-options static route 198.51.100.0/24 next-hop 192.0.2.1",
		diffHasChanges: true,
	}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeConfiguration,
		sessionID: "session-1",
		hasLock:   true,
	}

	if err := sh.cmdCommit(ctx, []string{"check"}); err != nil {
		t.Fatalf("cmdCommit(check) error = %v", err)
	}
	if client.cosCalls != 0 {
		t.Fatalf("GetClassOfService calls = %d, want 0", client.cosCalls)
	}
}

func TestFormatChangeImpactPreviewSummarizesRouteAndPolicyDiff(t *testing.T) {
	lines := formatChangeImpactPreview(strings.Join([]string{
		"+ set routing-options static route 0.0.0.0/0 next-hop 198.51.100.2",
		"- set routing-options static route 203.0.113.0/24 next-hop 198.51.100.3",
		"+ set policy-options policy-statement EXPORT term ALLOW then accept",
	}, "\n"), true)
	got := strings.Join(lines, "\n")
	for _, want := range []string{
		"change impact preview:",
		"changed lines: +2 -1",
		"static routes: +1 -1",
		"route diff:",
		"add 0.0.0.0/0 via 198.51.100.2",
		"remove 203.0.113.0/24 via 198.51.100.3",
		"policy-options: +1 -0",
		"policy diff:",
		"add route-map EXPORT term ALLOW",
		"route-policy dry-run:",
		"route-map regeneration planned: 1 policy statement changes",
		"warning: default route changes can affect all unmatched traffic",
		"warning: static route removals can withdraw forwarding entries",
		"warning: policy-options changes can regenerate FRR route-maps",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatChangeImpactPreview() = %q, want substring %q", got, want)
		}
	}
}

func TestFormatChangeImpactPreviewSummarizesInterfaceDiff(t *testing.T) {
	lines := formatChangeImpactPreview(strings.Join([]string{
		"+ set interfaces ge-0/0/0 unit 0 family inet address 198.51.100.1/30",
		"- set interfaces ge-0/0/1 disable",
		"+ set interfaces ge-0/0/2 description \"LAN Interface\"",
	}, "\n"), true)
	got := strings.Join(lines, "\n")
	for _, want := range []string{
		"interfaces: +2 -1",
		"interface diff:",
		"add interface ge-0/0/0 address 198.51.100.1/30",
		"remove interface ge-0/0/1 disable",
		"add interface ge-0/0/2 description LAN Interface",
		"warning: interface changes can affect link state or attached services",
		"warning: interface address changes can alter connected route reachability",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatChangeImpactPreview() = %q, want substring %q", got, want)
		}
	}
}

func TestFormatChangeImpactPreviewLimitsInterfaceDiffDetails(t *testing.T) {
	var diff []string
	for i := 0; i < maxChangeImpactInterfaceDetails+2; i++ {
		diff = append(diff, fmt.Sprintf("+ set interfaces ge-0/0/%d description uplink-%d", i, i))
	}
	lines := formatChangeImpactPreview(strings.Join(diff, "\n"), true)
	got := strings.Join(lines, "\n")
	if !strings.Contains(got, "interfaces: +7 -0") {
		t.Fatalf("formatChangeImpactPreview() = %q, want interface count", got)
	}
	if !strings.Contains(got, "... 2 more interface changes") {
		t.Fatalf("formatChangeImpactPreview() = %q, want capped interface details", got)
	}
	if strings.Contains(got, "ge-0/0/6") {
		t.Fatalf("formatChangeImpactPreview() = %q, want details capped before final interface", got)
	}
}

func TestFormatChangeImpactPreviewSummarizesPolicyAndBGPRouteMapDiff(t *testing.T) {
	lines := formatChangeImpactPreview(strings.Join([]string{
		"+ set policy-options prefix-list CUSTOMER 10.100.0.0/16",
		"+ set policy-options policy-statement EXPORT term ALLOW then accept",
		"+ set protocols bgp group external export EXPORT",
	}, "\n"), true)
	got := strings.Join(lines, "\n")
	for _, want := range []string{
		"policy-options: +2 -0",
		"bgp: +1 -0",
		"policy diff:",
		"add prefix-list CUSTOMER 10.100.0.0/16",
		"add route-map EXPORT term ALLOW",
		"add bgp group external export route-map EXPORT",
		"route-policy dry-run:",
		"prefix-list updates: 1",
		"route-map regeneration planned: 1 policy statement changes",
		"bgp policy bindings updated: 1",
		"validation scope: candidate policy syntax and referenced BGP bindings",
		"warning: policy-options changes can regenerate FRR route-maps",
		"warning: BGP changes can reset sessions or change route advertisements",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatChangeImpactPreview() = %q, want substring %q", got, want)
		}
	}
}

func TestFormatChangeImpactPreviewLimitsPolicyDiffDetails(t *testing.T) {
	var diff []string
	for i := 0; i < maxChangeImpactPolicyDetails+2; i++ {
		diff = append(diff, fmt.Sprintf("+ set policy-options policy-statement EXPORT-%d term ALLOW then accept", i))
	}
	lines := formatChangeImpactPreview(strings.Join(diff, "\n"), true)
	got := strings.Join(lines, "\n")
	if !strings.Contains(got, "policy-options: +7 -0") {
		t.Fatalf("formatChangeImpactPreview() = %q, want policy count", got)
	}
	if !strings.Contains(got, "... 2 more policy changes") {
		t.Fatalf("formatChangeImpactPreview() = %q, want capped policy details", got)
	}
	if strings.Contains(got, "EXPORT-6") {
		t.Fatalf("formatChangeImpactPreview() = %q, want details capped before final policy", got)
	}
}

func TestFormatChangeImpactPreviewSummarizesRoutingInstanceRouteDiff(t *testing.T) {
	lines := formatChangeImpactPreview("- set routing-instances BLUE routing-options static route 0.0.0.0/0 next-hop 10.1.1.1", true)
	got := strings.Join(lines, "\n")
	for _, want := range []string{
		"static routes: +0 -1",
		"routing-instances: +0 -1",
		"remove routing-instance BLUE 0.0.0.0/0 via 10.1.1.1",
		"warning: default route changes can affect all unmatched traffic",
		"warning: routing-instance changes can move interfaces or VRF routing state",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatChangeImpactPreview() = %q, want substring %q", got, want)
		}
	}
}

func TestFormatChangeImpactPreviewLimitsRouteDiffDetails(t *testing.T) {
	var diff []string
	for i := 0; i < maxChangeImpactRouteDetails+2; i++ {
		diff = append(diff, fmt.Sprintf("+ set routing-options static route 198.51.%d.0/24 next-hop 192.0.2.%d", i, i+1))
	}
	lines := formatChangeImpactPreview(strings.Join(diff, "\n"), true)
	got := strings.Join(lines, "\n")
	if !strings.Contains(got, "static routes: +7 -0") {
		t.Fatalf("formatChangeImpactPreview() = %q, want static route count", got)
	}
	if !strings.Contains(got, "... 2 more static route changes") {
		t.Fatalf("formatChangeImpactPreview() = %q, want capped route details", got)
	}
	if strings.Contains(got, "198.51.6.0/24") {
		t.Fatalf("formatChangeImpactPreview() = %q, want details capped before final route", got)
	}
}

func TestFormatChangeImpactPreviewWarnsOnDisruptiveProtocolDiff(t *testing.T) {
	lines := formatChangeImpactPreview(strings.Join([]string{
		"- set protocols bgp group external neighbor 198.51.100.2 peer-as 65001",
		"+ set protocols ospf area 0.0.0.0 interface ge-0/0/1 metric 50",
		"- set protocols bfd peer 192.0.2.2 profile fast",
		"+ set protocols evpn vni 10010 remote-vtep 198.51.100.20",
		"- set routing-instances BLUE interface ge-0/0/0",
		"+ set class-of-service interfaces ge-0/0/0 output-traffic-control-profile WAN",
	}, "\n"), true)
	got := strings.Join(lines, "\n")
	for _, want := range []string{
		"bgp: +0 -1",
		"ospf: +1 -0",
		"bfd: +0 -1",
		"evpn: +1 -0",
		"routing-instances: +0 -1",
		"class-of-service: +1 -0",
		"warning: BGP changes can reset sessions or change route advertisements",
		"warning: OSPF changes can trigger adjacency updates or SPF recalculation",
		"warning: BFD changes can affect fast failure detection",
		"warning: EVPN changes can alter overlay VNI reachability",
		"warning: routing-instance changes can move interfaces or VRF routing state",
		"warning: class-of-service changes can alter traffic treatment",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatChangeImpactPreview() = %q, want substring %q", got, want)
		}
	}
}

func TestFormatChangeImpactPreviewNoChanges(t *testing.T) {
	lines := formatChangeImpactPreview("", false)
	if got, want := strings.Join(lines, "\n"), "change impact preview: no candidate changes"; got != want {
		t.Fatalf("formatChangeImpactPreview(no changes) = %q, want %q", got, want)
	}
}

func TestFormatClassOfServicePreflightWarnsOnCapabilityGaps(t *testing.T) {
	lines := formatClassOfServicePreflight(&grpcclient.ClassOfServiceInfo{
		Capabilities: &grpcclient.ClassOfServiceCapabilitiesInfo{
			MetadataBindingSupported: true,
			QueueSchedulerSupported:  false,
			PolicerSupported:         false,
			CountersSupported:        true,
			LastError:                "capability probe failed",
			Diagnostics:              []string{"scheduler unsupported"},
		},
	})
	got := strings.Join(lines, "\n")
	for _, want := range []string{
		"qos preflight:",
		"metadata binding: yes",
		"queue scheduler: no",
		"policer: no",
		"counters: yes",
		"warning: capability detection error: capability probe failed",
		"warning: queue scheduler is unavailable; output QoS remains intent-only",
		"warning: policer is unavailable; traffic policing remains intent-only",
		"diagnostic: scheduler unsupported",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatClassOfServicePreflight() = %q, want substring %q", got, want)
		}
	}
}

func TestFormatClassOfServicePostCommitSummarizesStatus(t *testing.T) {
	lines := formatClassOfServicePostCommit(&grpcclient.ClassOfServiceInfo{
		EnforcementStatus: "intent-only",
		Interfaces: []grpcclient.ClassOfServiceInterfaceInfo{
			{Name: "ge-0/0/0", OutputTrafficControlProfile: "WAN"},
			{Name: "ge-0/0/1", OutputTrafficControlProfile: "LAN"},
		},
		Capabilities: &grpcclient.ClassOfServiceCapabilitiesInfo{
			MetadataBindingSupported: true,
			QueueSchedulerSupported:  false,
			PolicerSupported:         false,
			CountersSupported:        true,
			LastError:                "capability probe failed",
			Diagnostics:              []string{"scheduler unsupported"},
		},
	})
	got := strings.Join(lines, "\n")
	for _, want := range []string{
		"qos post-commit diagnostics:",
		"enforcement status: intent-only",
		"bound interfaces: 2",
		"metadata binding: yes",
		"queue scheduler: no",
		"policer: no",
		"counters: yes",
		"warning: capability detection error: capability probe failed",
		"diagnostic: scheduler unsupported",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("formatClassOfServicePostCommit() = %q, want substring %q", got, want)
		}
	}
}

func TestUpgradePreflightLinesReportsReadyState(t *testing.T) {
	client := &fakeInteractiveClient{
		runningText:    "set system host-name router\n",
		runningVersion: 7,
		history: []grpcclient.CommitInfo{
			{CommitID: "1234567890abcdef"},
		},
		commitDetails: map[string]grpcclient.CommitInfo{
			"1234567890abcdef": {CommitID: "1234567890abcdef", ConfigText: "set system host-name router"},
		},
		cosInfo: &grpcclient.ClassOfServiceInfo{
			Capabilities: &grpcclient.ClassOfServiceCapabilitiesInfo{
				MetadataBindingSupported: true,
				QueueSchedulerSupported:  true,
				PolicerSupported:         true,
				CountersSupported:        true,
			},
		},
	}

	lines, err := upgradePreflightLines(context.Background(), client)
	if err != nil {
		t.Fatalf("upgradePreflightLines() error = %v", err)
	}
	got := strings.Join(lines, "\n")
	for _, want := range []string{
		"upgrade preflight:",
		"running config: version 7",
		"running validation: ok",
		"supported direct upgrade sources: v0.8.x, v0.9.x",
		"datastore schema guard: SQLite schema 1-2 accepted",
		"rollback archive: latest commit 12345678 available",
		"rollback archive validation: ok",
		"telemetry catalog:",
		"qos metadata binding: yes",
		"package preflight:",
		"release readiness:",
		"rollback guidance:",
		"status: ready for package-specific upgrade checks",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("upgradePreflightLines() = %q, want substring %q", got, want)
		}
	}
	if client.getRunningCalls != 1 || client.listHistoryCalls != 1 || client.getCommitCalls != 1 || client.telemetryCatalogCalls != 1 || client.cosCalls != 1 {
		t.Fatalf("running/history/commit/telemetry/cos calls = %d/%d/%d/%d/%d, want 1/1/1/1/1",
			client.getRunningCalls, client.listHistoryCalls, client.getCommitCalls, client.telemetryCatalogCalls, client.cosCalls)
	}
}

func TestUpgradeCompatibilityPreflightLines(t *testing.T) {
	got := strings.Join(upgradeCompatibilityPreflightLines(), "\n")
	for _, want := range []string{
		"compatibility phase: v0.10.x stabilization and compatibility",
		"supported direct upgrade sources: v0.8.x, v0.9.x",
		"unsupported direct upgrades: v0.7.x and older",
		"API compatibility: arca.router.v1, arca.telemetry.v1",
		"datastore schema guard: SQLite schema 1-2 accepted",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("upgradeCompatibilityPreflightLines() = %q, want substring %q", got, want)
		}
	}
}

func TestUpgradePackagePreflightSkipsSourceCheckout(t *testing.T) {
	lines, warnings := upgradePackagePreflightLinesWithRoot(t.TempDir())
	got := strings.Join(lines, "\n")
	if warnings != 0 {
		t.Fatalf("warnings = %d, want 0", warnings)
	}
	if !strings.Contains(got, "packaged install paths: not detected") {
		t.Fatalf("upgradePackagePreflightLinesWithRoot() = %q, want skipped packaged install paths", got)
	}
}

func TestUpgradePackagePreflightWarnsMissingPackagedPaths(t *testing.T) {
	root := t.TempDir()
	cliPath := filepath.Join(root, "usr/bin/arca")
	if err := os.MkdirAll(filepath.Dir(cliPath), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(cliPath, []byte("arca"), 0755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	lines, warnings := upgradePackagePreflightLinesWithRoot(root)
	got := strings.Join(lines, "\n")
	if warnings == 0 {
		t.Fatal("warnings = 0, want missing packaged path warnings")
	}
	for _, want := range []string{
		"CLI binary: file present",
		"warning: daemon binary missing at /usr/sbin/arca-routerd",
		"warning: systemd unit missing at /usr/lib/systemd/system/arca-routerd.service",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("upgradePackagePreflightLinesWithRoot() = %q, want substring %q", got, want)
		}
	}
}

func TestUpgradeRollbackGuidanceLines(t *testing.T) {
	got := strings.Join(upgradeRollbackGuidanceLines(), "\n")
	for _, want := range []string{
		"rollback guidance:",
		"previous package",
		"configuration backup",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("upgradeRollbackGuidanceLines() = %q, want substring %q", got, want)
		}
	}
}

func TestUpgradeReleaseReadinessLines(t *testing.T) {
	got := strings.Join(upgradeReleaseReadinessLines(), "\n")
	for _, want := range []string{
		"release readiness:",
		"docs/v0.10-operational-runbook",
		"docs/v0.10-release-readiness.md",
		"docs/v0.11-deferred-gates.md",
		"release sign-off must list owner",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("upgradeReleaseReadinessLines() = %q, want substring %q", got, want)
		}
	}
}

func TestUpgradePreflightLinesReportsWarnings(t *testing.T) {
	client := &fakeInteractiveClient{}

	lines, err := upgradePreflightLines(context.Background(), client)
	if err != nil {
		t.Fatalf("upgradePreflightLines() error = %v", err)
	}
	got := strings.Join(lines, "\n")
	for _, want := range []string{
		"warning: running configuration is empty",
		"warning: no rollback archive entries available",
		"warning: qos capability snapshot unavailable",
		"status: 3 warning(s), review before upgrade",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("upgradePreflightLines() = %q, want substring %q", got, want)
		}
	}
}

func TestUpgradePreflightLinesReportsConfigValidationWarnings(t *testing.T) {
	client := &fakeInteractiveClient{
		runningText:    "set system host-name bad_name\n",
		runningVersion: 7,
		history: []grpcclient.CommitInfo{
			{CommitID: "1234567890abcdef"},
		},
		commitDetails: map[string]grpcclient.CommitInfo{
			"1234567890abcdef": {CommitID: "1234567890abcdef", ConfigText: "set system host-name bad_name"},
		},
		cosInfo: &grpcclient.ClassOfServiceInfo{
			Capabilities: &grpcclient.ClassOfServiceCapabilitiesInfo{
				MetadataBindingSupported: true,
				QueueSchedulerSupported:  true,
				PolicerSupported:         true,
				CountersSupported:        true,
			},
		},
	}

	lines, err := upgradePreflightLines(context.Background(), client)
	if err != nil {
		t.Fatalf("upgradePreflightLines() error = %v", err)
	}
	got := strings.Join(lines, "\n")
	for _, want := range []string{
		"warning: running configuration validation failed:",
		"warning: latest rollback archive validation failed:",
		"status: 2 warning(s), review before upgrade",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("upgradePreflightLines() = %q, want substring %q", got, want)
		}
	}
}

func TestUpgradePreflightLinesChecksBackupPath(t *testing.T) {
	backupPath := t.TempDir() + "/running.conf"
	client := &fakeInteractiveClient{
		runningText:    "set system host-name router\n",
		runningVersion: 7,
		history: []grpcclient.CommitInfo{
			{CommitID: "1234567890abcdef"},
		},
		commitDetails: map[string]grpcclient.CommitInfo{
			"1234567890abcdef": {CommitID: "1234567890abcdef", ConfigText: "set system host-name router"},
		},
		cosInfo: &grpcclient.ClassOfServiceInfo{
			Capabilities: &grpcclient.ClassOfServiceCapabilitiesInfo{
				MetadataBindingSupported: true,
				QueueSchedulerSupported:  true,
				PolicerSupported:         true,
				CountersSupported:        true,
			},
		},
	}

	lines, err := upgradePreflightLinesWithOptions(context.Background(), client, upgradePreflightOptions{BackupPath: backupPath})
	if err != nil {
		t.Fatalf("upgradePreflightLinesWithOptions() error = %v", err)
	}
	got := strings.Join(lines, "\n")
	for _, want := range []string{
		"backup path: writable " + backupPath,
		"status: ready for package-specific upgrade checks",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("upgradePreflightLinesWithOptions() = %q, want substring %q", got, want)
		}
	}
	if _, err := os.Stat(backupPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup path stat error = %v, want not created", err)
	}
}

func TestUpgradePreflightLinesWarnsExistingBackupPath(t *testing.T) {
	backupPath := t.TempDir() + "/running.conf"
	if err := os.WriteFile(backupPath, []byte("existing\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", backupPath, err)
	}
	client := &fakeInteractiveClient{
		runningText:    "set system host-name router\n",
		runningVersion: 7,
		history: []grpcclient.CommitInfo{
			{CommitID: "1234567890abcdef"},
		},
		commitDetails: map[string]grpcclient.CommitInfo{
			"1234567890abcdef": {CommitID: "1234567890abcdef", ConfigText: "set system host-name router"},
		},
		cosInfo: &grpcclient.ClassOfServiceInfo{
			Capabilities: &grpcclient.ClassOfServiceCapabilitiesInfo{
				MetadataBindingSupported: true,
				QueueSchedulerSupported:  true,
				PolicerSupported:         true,
				CountersSupported:        true,
			},
		},
	}

	lines, err := upgradePreflightLinesWithOptions(context.Background(), client, upgradePreflightOptions{BackupPath: backupPath})
	if err != nil {
		t.Fatalf("upgradePreflightLinesWithOptions() error = %v", err)
	}
	got := strings.Join(lines, "\n")
	for _, want := range []string{
		"warning: backup path already exists: " + backupPath,
		"status: 1 warning(s), review before upgrade",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("upgradePreflightLinesWithOptions() = %q, want substring %q", got, want)
		}
	}
}

func TestCmdCheckUpgradeRequiresOperationalMode(t *testing.T) {
	sh := &interactiveShell{
		client:    &fakeInteractiveClient{},
		hostname:  "router",
		mode:      modeConfiguration,
		sessionID: "session-1",
		hasLock:   true,
	}

	err := sh.cmdCheck(context.Background(), []string{"upgrade"})
	if err == nil || !strings.Contains(err.Error(), "operational mode") {
		t.Fatalf("cmdCheck(upgrade) error = %v, want operational mode error", err)
	}
}

func TestOneShotCheckUpgradeRejectsInvalidUsage(t *testing.T) {
	code := oneShotCheck(context.Background(), &fakeInteractiveClient{}, []string{"configuration"})
	if code != ExitUsageError {
		t.Fatalf("oneShotCheck(configuration) = %d, want %d", code, ExitUsageError)
	}
}

func TestShowHistoryHandlesShortCommitIDs(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{
		history: []grpcclient.CommitInfo{
			{CommitID: "abc", User: "alice", Message: "short id"},
		},
	}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeOperational,
		sessionID: "session-1",
	}

	if err := sh.cmdShow(ctx, []string{"history"}); err != nil {
		t.Fatalf("cmdShow(history) error = %v", err)
	}
	if client.listHistoryCalls != 1 {
		t.Fatalf("ListHistory calls = %d, want 1", client.listHistoryCalls)
	}
}

func TestShowHistoryRejectsInvalidLimit(t *testing.T) {
	ctx := context.Background()

	for _, arg := range []string{"-1", "0", "1abc"} {
		client := &fakeInteractiveClient{}
		sh := &interactiveShell{
			client:    client,
			hostname:  "router",
			mode:      modeOperational,
			sessionID: "session-1",
		}

		err := sh.cmdShow(ctx, []string{"history", arg})
		if err == nil || !strings.Contains(err.Error(), "invalid limit") {
			t.Fatalf("cmdShow(history %s) error = %v, want invalid limit", arg, err)
		}
		if client.listHistoryCalls != 0 {
			t.Fatalf("ListHistory calls for %q = %d, want 0", arg, client.listHistoryCalls)
		}
	}
}

func TestShowConfigurationRollbackUsesArchivedConfig(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{
		history: []grpcclient.CommitInfo{
			{CommitID: "commit-new"},
			{CommitID: "commit-old"},
		},
		commitDetails: map[string]grpcclient.CommitInfo{
			"commit-old": {CommitID: "commit-old", ConfigText: "set system host-name old"},
		},
	}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeOperational,
		sessionID: "session-1",
	}

	if err := sh.cmdShow(ctx, []string{"configuration", "rollback", "1"}); err != nil {
		t.Fatalf("cmdShow(configuration rollback 1) error = %v", err)
	}
	if client.listHistoryCalls != 1 || client.listHistoryLimit != 2 || client.listHistoryOffset != 0 {
		t.Fatalf("ListHistory calls/limit/offset = %d/%d/%d, want 1/2/0",
			client.listHistoryCalls, client.listHistoryLimit, client.listHistoryOffset)
	}
	if client.getRunningCalls != 0 {
		t.Fatalf("GetRunning calls = %d, want 0 when archived config is available", client.getRunningCalls)
	}
	if client.getCommitCalls != 1 || client.getCommitID != "commit-old" {
		t.Fatalf("GetCommit calls/id = %d/%q, want 1/commit-old", client.getCommitCalls, client.getCommitID)
	}
}

func TestShowConfigurationRollbackZeroFallsBackToRunning(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{runningText: "set system host-name running"}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeOperational,
		sessionID: "session-1",
	}

	if err := sh.cmdShow(ctx, []string{"configuration", "rollback", "0"}); err != nil {
		t.Fatalf("cmdShow(configuration rollback 0) error = %v", err)
	}
	if client.listHistoryCalls != 1 || client.listHistoryLimit != 1 {
		t.Fatalf("ListHistory calls/limit = %d/%d, want 1/1", client.listHistoryCalls, client.listHistoryLimit)
	}
	if client.getRunningCalls != 1 {
		t.Fatalf("GetRunning calls = %d, want 1 fallback", client.getRunningCalls)
	}
}

func TestShowConfigurationRollbackRejectsMissingArchive(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{
		history: []grpcclient.CommitInfo{
			{CommitID: "commit-new"},
			{CommitID: "commit-old"},
		},
		commitDetails: map[string]grpcclient.CommitInfo{
			"commit-old": {CommitID: "commit-old"},
		},
	}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeOperational,
		sessionID: "session-1",
	}

	err := sh.cmdShow(ctx, []string{"configuration", "rollback", "1"})
	if err == nil || !strings.Contains(err.Error(), "archived config text unavailable") {
		t.Fatalf("cmdShow(configuration rollback 1) error = %v, want archive unavailable", err)
	}
}

func TestBackupConfigurationWritesRunningConfig(t *testing.T) {
	ctx := context.Background()
	backupPath := t.TempDir() + "/running.conf"
	client := &fakeInteractiveClient{
		runningText:           "set system host-name running\nset system services snmp community <redacted>",
		runningUnredactedText: "set system host-name running\nset system services snmp community secret-community",
	}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeOperational,
		sessionID: "session-1",
	}

	if err := sh.cmdBackup(ctx, []string{"configuration", backupPath}); err != nil {
		t.Fatalf("cmdBackup(configuration) error = %v", err)
	}
	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "set system host-name running\nset system services snmp community secret-community\n" {
		t.Fatalf("backup content = %q, want unredacted running config with trailing newline", string(data))
	}
	if client.getRunningUnredactedCalls != 1 || client.getRunningCalls != 0 || client.getCandidateCalls != 0 {
		t.Fatalf("unredacted/running/candidate calls = %d/%d/%d, want 1/0/0", client.getRunningUnredactedCalls, client.getRunningCalls, client.getCandidateCalls)
	}
}

func TestBackupConfigurationWritesCandidateConfig(t *testing.T) {
	ctx := context.Background()
	backupPath := t.TempDir() + "/candidate.conf"
	client := &fakeInteractiveClient{candidateText: "set system host-name candidate"}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeConfiguration,
		sessionID: "session-1",
	}

	if err := sh.cmdBackup(ctx, []string{"configuration", backupPath}); err != nil {
		t.Fatalf("cmdBackup(configuration) error = %v", err)
	}
	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "set system host-name candidate\n" {
		t.Fatalf("backup content = %q, want candidate config with trailing newline", string(data))
	}
	if client.getCandidateCalls != 1 || client.getRunningCalls != 0 {
		t.Fatalf("candidate/running calls = %d/%d, want 1/0", client.getCandidateCalls, client.getRunningCalls)
	}
}

func TestBackupConfigurationWritesArchivedRollbackConfig(t *testing.T) {
	ctx := context.Background()
	backupPath := t.TempDir() + "/rollback.conf"
	client := &fakeInteractiveClient{
		history: []grpcclient.CommitInfo{
			{CommitID: "commit-new"},
			{CommitID: "commit-old"},
		},
		commitDetails: map[string]grpcclient.CommitInfo{
			"commit-old": {CommitID: "commit-old", ConfigText: "set system host-name old"},
		},
	}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeOperational,
		sessionID: "session-1",
	}

	if err := sh.cmdBackup(ctx, []string{"configuration", "rollback", "1", backupPath}); err != nil {
		t.Fatalf("cmdBackup(configuration rollback 1) error = %v", err)
	}
	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "set system host-name old\n" {
		t.Fatalf("backup content = %q, want archived rollback config", string(data))
	}
	if client.listHistoryCalls != 1 || client.listHistoryLimit != 2 {
		t.Fatalf("ListHistory calls/limit = %d/%d, want 1/2", client.listHistoryCalls, client.listHistoryLimit)
	}
}

func TestBackupConfigurationRollbackZeroUsesUnredactedRunningConfig(t *testing.T) {
	ctx := context.Background()
	backupPath := t.TempDir() + "/rollback-zero.conf"
	client := &fakeInteractiveClient{
		history:               []grpcclient.CommitInfo{{CommitID: "commit-current"}},
		runningText:           "set system host-name running\nset system services snmp community <redacted>",
		runningUnredactedText: "set system host-name running\nset system services snmp community secret-community",
	}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeOperational,
		sessionID: "session-1",
	}

	if err := sh.cmdBackup(ctx, []string{"configuration", "rollback", "0", backupPath}); err != nil {
		t.Fatalf("cmdBackup(configuration rollback 0) error = %v", err)
	}
	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "set system host-name running\nset system services snmp community secret-community\n" {
		t.Fatalf("backup content = %q, want unredacted running config", string(data))
	}
	if client.getRunningUnredactedCalls != 1 || client.getRunningCalls != 0 {
		t.Fatalf("unredacted/running calls = %d/%d, want 1/0", client.getRunningUnredactedCalls, client.getRunningCalls)
	}
}

func TestBackupConfigurationRefusesOverwrite(t *testing.T) {
	backupPath := t.TempDir() + "/running.conf"
	if err := os.WriteFile(backupPath, []byte("existing\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := writeConfigBackupFile(backupPath, "set system host-name running")
	if err == nil || !strings.Contains(err.Error(), "create backup file") {
		t.Fatalf("writeConfigBackupFile() error = %v, want create backup failure", err)
	}
	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "existing\n" {
		t.Fatalf("backup content = %q, want existing content preserved", string(data))
	}
}

func TestRestoreConfigurationBackupReplacesCandidate(t *testing.T) {
	ctx := context.Background()
	backupPath := t.TempDir() + "/backup.conf"
	if err := os.WriteFile(backupPath, []byte("set system host-name restored\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	client := &fakeInteractiveClient{}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeConfiguration,
		sessionID: "session-1",
	}

	if err := sh.cmdRestore(ctx, []string{"configuration", backupPath}); err != nil {
		t.Fatalf("cmdRestore(configuration) error = %v", err)
	}
	if len(client.replaceTexts) != 1 || client.replaceTexts[0] != "set system host-name restored\n" {
		t.Fatalf("ReplaceCandidate texts = %#v, want backup content", client.replaceTexts)
	}
}

func TestRestoreConfigurationBackupValidatesBeforeReplace(t *testing.T) {
	ctx := context.Background()
	backupPath := t.TempDir() + "/backup.conf"
	if err := os.WriteFile(backupPath, []byte("set system host-name bad_name\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	client := &fakeInteractiveClient{}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeConfiguration,
		sessionID: "session-1",
	}

	err := sh.cmdRestore(ctx, []string{"configuration", backupPath})
	if err == nil || !strings.Contains(err.Error(), "validate configuration backup") {
		t.Fatalf("cmdRestore(configuration) error = %v, want validation failure", err)
	}
	if len(client.replaceTexts) != 0 {
		t.Fatalf("ReplaceCandidate texts = %#v, want none for invalid backup", client.replaceTexts)
	}
}

func TestRestoreConfigurationBackupRejectsRedactedSecrets(t *testing.T) {
	ctx := context.Background()
	backupPath := t.TempDir() + "/backup.conf"
	if err := os.WriteFile(backupPath, []byte("set system host-name restored\nset system services snmp community <redacted>\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	client := &fakeInteractiveClient{}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeConfiguration,
		sessionID: "session-1",
	}

	err := sh.cmdRestore(ctx, []string{"configuration", backupPath})
	if err == nil || !strings.Contains(err.Error(), "redacted configuration text cannot be restored") {
		t.Fatalf("cmdRestore(configuration) error = %v, want redacted config rejection", err)
	}
	if len(client.replaceTexts) != 0 {
		t.Fatalf("ReplaceCandidate texts = %#v, want none for redacted backup", client.replaceTexts)
	}
}

func TestRestoreConfigurationRollbackReplacesCandidate(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{
		history: []grpcclient.CommitInfo{
			{CommitID: "commit-new"},
			{CommitID: "commit-old"},
		},
		commitDetails: map[string]grpcclient.CommitInfo{
			"commit-old": {CommitID: "commit-old", ConfigText: "set system host-name old"},
		},
	}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeConfiguration,
		sessionID: "session-1",
	}

	if err := sh.cmdRestore(ctx, []string{"configuration", "rollback", "1"}); err != nil {
		t.Fatalf("cmdRestore(configuration rollback 1) error = %v", err)
	}
	if len(client.replaceTexts) != 1 || client.replaceTexts[0] != "set system host-name old" {
		t.Fatalf("ReplaceCandidate texts = %#v, want archived config", client.replaceTexts)
	}
	if client.listHistoryCalls != 1 || client.listHistoryLimit != 2 {
		t.Fatalf("ListHistory calls/limit = %d/%d, want 1/2", client.listHistoryCalls, client.listHistoryLimit)
	}
}

func TestRestoreConfigurationRollbackValidatesBeforeReplace(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{
		history: []grpcclient.CommitInfo{
			{CommitID: "commit-new"},
			{CommitID: "commit-old"},
		},
		commitDetails: map[string]grpcclient.CommitInfo{
			"commit-old": {CommitID: "commit-old", ConfigText: "set system host-name bad_name"},
		},
	}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeConfiguration,
		sessionID: "session-1",
	}

	err := sh.cmdRestore(ctx, []string{"configuration", "rollback", "1"})
	if err == nil || !strings.Contains(err.Error(), "validate rollback configuration") {
		t.Fatalf("cmdRestore(configuration rollback 1) error = %v, want validation failure", err)
	}
	if len(client.replaceTexts) != 0 {
		t.Fatalf("ReplaceCandidate texts = %#v, want none for invalid rollback archive", client.replaceTexts)
	}
}

func TestRestoreConfigurationRollbackRejectsRedactedSecrets(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{
		history: []grpcclient.CommitInfo{
			{CommitID: "commit-new"},
			{CommitID: "commit-old"},
		},
		commitDetails: map[string]grpcclient.CommitInfo{
			"commit-old": {
				CommitID:   "commit-old",
				ConfigText: "set system host-name old\nset system services snmp community <redacted>",
			},
		},
	}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeConfiguration,
		sessionID: "session-1",
	}

	err := sh.cmdRestore(ctx, []string{"configuration", "rollback", "1"})
	if err == nil || !strings.Contains(err.Error(), "redacted configuration text cannot be restored") {
		t.Fatalf("cmdRestore(configuration rollback 1) error = %v, want redacted config rejection", err)
	}
	if len(client.replaceTexts) != 0 {
		t.Fatalf("ReplaceCandidate texts = %#v, want none for redacted rollback archive", client.replaceTexts)
	}
}

func TestRestoreConfigurationRequiresConfigurationMode(t *testing.T) {
	ctx := context.Background()
	backupPath := t.TempDir() + "/backup.conf"
	if err := os.WriteFile(backupPath, []byte("set system host-name restored\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	client := &fakeInteractiveClient{}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeOperational,
		sessionID: "session-1",
	}

	err := sh.cmdRestore(ctx, []string{"configuration", backupPath})
	if err == nil || !strings.Contains(err.Error(), "configuration mode") {
		t.Fatalf("cmdRestore(configuration) error = %v, want configuration mode error", err)
	}
	if len(client.replaceTexts) != 0 {
		t.Fatalf("ReplaceCandidate texts = %#v, want none outside configuration mode", client.replaceTexts)
	}
}

func TestRestoreConfigurationRejectsInvalidUsage(t *testing.T) {
	sh := &interactiveShell{
		client:    &fakeInteractiveClient{},
		hostname:  "router",
		mode:      modeConfiguration,
		sessionID: "session-1",
	}

	err := sh.cmdRestore(context.Background(), []string{"configuration"})
	if err == nil || !strings.Contains(err.Error(), "usage: restore configuration") {
		t.Fatalf("cmdRestore(configuration) error = %v, want usage error", err)
	}
}

func TestOneShotBackupConfigurationWritesRunningConfig(t *testing.T) {
	backupPath := t.TempDir() + "/running.conf"
	client := &fakeInteractiveClient{
		runningText:           "set system host-name running\nset system services snmp community <redacted>",
		runningUnredactedText: "set system host-name running\nset system services snmp community secret-community",
	}

	code := oneShotBackup(context.Background(), client, []string{"configuration", backupPath})
	if code != ExitSuccess {
		t.Fatalf("oneShotBackup(configuration) = %d, want %d", code, ExitSuccess)
	}
	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "set system host-name running\nset system services snmp community secret-community\n" {
		t.Fatalf("backup content = %q, want unredacted running config", string(data))
	}
	if client.getRunningUnredactedCalls != 1 || client.getRunningCalls != 0 {
		t.Fatalf("unredacted/running calls = %d/%d, want 1/0", client.getRunningUnredactedCalls, client.getRunningCalls)
	}
}

func TestOneShotBackupConfigurationWritesArchivedRollbackConfig(t *testing.T) {
	backupPath := t.TempDir() + "/rollback.conf"
	client := &fakeInteractiveClient{
		history: []grpcclient.CommitInfo{
			{CommitID: "commit-new"},
			{CommitID: "commit-old"},
		},
		commitDetails: map[string]grpcclient.CommitInfo{
			"commit-old": {CommitID: "commit-old", ConfigText: "set system host-name old"},
		},
	}

	code := oneShotBackup(context.Background(), client, []string{"configuration", "rollback", "1", backupPath})
	if code != ExitSuccess {
		t.Fatalf("oneShotBackup(configuration rollback 1) = %d, want %d", code, ExitSuccess)
	}
	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "set system host-name old\n" {
		t.Fatalf("backup content = %q, want archived rollback config", string(data))
	}
	if client.listHistoryCalls != 1 || client.listHistoryLimit != 2 {
		t.Fatalf("ListHistory calls/limit = %d/%d, want 1/2", client.listHistoryCalls, client.listHistoryLimit)
	}
}

func TestOneShotBackupConfigurationRejectsInvalidRollbackNumber(t *testing.T) {
	client := &fakeInteractiveClient{}

	code := oneShotBackup(context.Background(), client, []string{"configuration", "rollback", "-1", "backup.conf"})
	if code != ExitUsageError {
		t.Fatalf("oneShotBackup(configuration rollback -1) = %d, want %d", code, ExitUsageError)
	}
	if client.listHistoryCalls != 0 {
		t.Fatalf("ListHistory calls = %d, want 0", client.listHistoryCalls)
	}
}

func TestCmdShowOSPFNeighborReturnsOutput(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{ospfNeighbors: []grpcclient.OSPFNeighborInfo{{RouterID: "10.0.0.2", State: "Full"}}}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeOperational,
		sessionID: "session-1",
	}

	err := sh.cmdShow(ctx, []string{"ospf", "neighbor"})
	if err != nil {
		t.Fatalf("cmdShow(ospf neighbor) error = %v", err)
	}
	if client.ospfFamily != routeAddressFamilyIPv4 {
		t.Fatalf("OSPF address family = %q, want %q", client.ospfFamily, routeAddressFamilyIPv4)
	}
	if client.ospfNeighborCalls != 1 {
		t.Fatalf("OSPF neighbor calls = %d, want 1", client.ospfNeighborCalls)
	}
}

func TestCmdShowOSPF3NeighborReturnsOutput(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{ospfNeighbors: []grpcclient.OSPFNeighborInfo{{RouterID: "10.0.0.3", State: "Full"}}}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeOperational,
		sessionID: "session-1",
	}

	err := sh.cmdShow(ctx, []string{"ospf3", "neighbor"})
	if err != nil {
		t.Fatalf("cmdShow(ospf3 neighbor) error = %v", err)
	}
	if client.ospfFamily != routeAddressFamilyIPv6 {
		t.Fatalf("OSPF3 address family = %q, want %q", client.ospfFamily, routeAddressFamilyIPv6)
	}
	if client.ospfNeighborCalls != 1 {
		t.Fatalf("OSPF3 neighbor calls = %d, want 1", client.ospfNeighborCalls)
	}
}

func TestCmdShowVRRPReturnsOutput(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeOperational,
		sessionID: "session-1",
	}

	err := sh.cmdShow(ctx, []string{"vrrp"})
	if err != nil {
		t.Fatalf("cmdShow(vrrp) error = %v", err)
	}
}

func TestCmdShowBFDReturnsOutput(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeOperational,
		sessionID: "session-1",
	}

	err := sh.cmdShow(ctx, []string{"bfd", "peer", "192.0.2.2", "counters"})
	if err != nil {
		t.Fatalf("cmdShow(bfd peer counters) error = %v", err)
	}
	if client.bfdPeerAddress != "192.0.2.2" || client.bfdBrief || !client.bfdCounters {
		t.Fatalf("BFD options = peer %q brief %v counters %v, want peer counters", client.bfdPeerAddress, client.bfdBrief, client.bfdCounters)
	}
}

func TestCmdShowBFDStatusUsesStructuredState(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{bfdInfo: &grpcclient.BFDStatusInfo{
		LastRun:         time.Now(),
		ConfiguredPeers: 1,
		ObservedPeers:   1,
		UpPeers:         1,
		Peers: []grpcclient.BFDPeerInfo{
			{Peer: "192.0.2.2", Interface: "ge-0/0/0", Status: "up", Observed: true, Up: true},
		},
	}}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeOperational,
		sessionID: "session-1",
	}

	err := sh.cmdShow(ctx, []string{"bfd", "status"})
	if err != nil {
		t.Fatalf("cmdShow(bfd status) error = %v", err)
	}
	if client.bfdStatusCalls != 1 {
		t.Fatalf("BFD status calls = %d, want 1", client.bfdStatusCalls)
	}
	if client.bfdPeerAddress != "" || client.bfdBrief || client.bfdCounters {
		t.Fatalf("BFD text options = peer %q brief %v counters %v, want unused", client.bfdPeerAddress, client.bfdBrief, client.bfdCounters)
	}
}

func TestCmdShowLCPReturnsOutput(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{lcpInfo: &grpcclient.LCPReconciliationInfo{
		PairCount: 1,
	}}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeOperational,
		sessionID: "session-1",
	}

	err := sh.cmdShow(ctx, []string{"lcp"})
	if err != nil {
		t.Fatalf("cmdShow(lcp) error = %v", err)
	}
}

func TestCmdShowHAReturnsOutput(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{haInfo: &grpcclient.HAStatusInfo{
		Configured: true,
		Converged:  true,
		VRRPGroups: 1,
	}}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeOperational,
		sessionID: "session-1",
	}

	err := sh.cmdShow(ctx, []string{"ha"})
	if err != nil {
		t.Fatalf("cmdShow(ha) error = %v", err)
	}
}

func TestCmdShowClassOfServiceReturnsOutput(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{cosInfo: &grpcclient.ClassOfServiceInfo{
		EnforcementStatus: "intent-only",
		ForwardingClasses: []grpcclient.ClassOfServiceForwardingClassInfo{
			{Name: "expedited-forwarding", Queue: 5},
		},
	}}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeOperational,
		sessionID: "session-1",
	}

	err := sh.cmdShow(ctx, []string{"class-of-service"})
	if err != nil {
		t.Fatalf("cmdShow(class-of-service) error = %v", err)
	}
}

func TestCmdShowEVPNUsesTelemetrySnapshot(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{telemetryEvents: []*grpcclient.TelemetryEvent{evpnTelemetryTestEvent()}}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeOperational,
		sessionID: "session-1",
	}

	err := sh.cmdShow(ctx, []string{"evpn"})
	if err != nil {
		t.Fatalf("cmdShow(evpn) error = %v", err)
	}
	if client.telemetryCalls != 1 {
		t.Fatalf("telemetry calls = %d, want 1", client.telemetryCalls)
	}
	if len(client.telemetryPaths) != 1 || client.telemetryPaths[0] != "/overlays/evpn" {
		t.Fatalf("telemetry paths = %#v, want /overlays/evpn", client.telemetryPaths)
	}
	if !client.telemetryOnce || client.telemetryInterval != 0 {
		t.Fatalf("telemetry once/interval = %v/%v, want true/0", client.telemetryOnce, client.telemetryInterval)
	}
}

func TestCmdShowRoutingInstancesReturnsOutput(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{routingInstances: []grpcclient.RoutingInstanceInfo{
		{
			Name:               "BLUE",
			InstanceType:       "vrf",
			RouteDistinguisher: "65000:100",
			IPv4TableID:        100,
			IPv6TableID:        100,
			Interfaces:         []string{"ge-0/0/0"},
		},
	}}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeOperational,
		sessionID: "session-1",
	}

	err := sh.cmdShow(ctx, []string{"routing-instances", "BLUE"})
	if err != nil {
		t.Fatalf("cmdShow(routing-instances BLUE) error = %v", err)
	}
	if client.routingCalls != 1 {
		t.Fatalf("routing instance calls = %d, want 1", client.routingCalls)
	}
}

func TestCmdShowBGPNeighborsUsesStructuredState(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{bgpNeighbors: []grpcclient.BGPNeighborInfo{
		{PeerAddress: "2001:db8::2", PeerAS: 65001, State: "Established", UptimeSecs: 3661, PrefixReceived: 10, PrefixSent: 20},
	}}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeOperational,
		sessionID: "session-1",
	}

	err := sh.cmdShow(ctx, []string{"bgp", "neighbors"})
	if err != nil {
		t.Fatalf("cmdShow(bgp neighbors) error = %v", err)
	}
	if client.bgpNeighborCalls != 1 {
		t.Fatalf("BGP neighbor calls = %d, want 1", client.bgpNeighborCalls)
	}
}

func TestCmdShowRoutesUsesStructuredState(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{routes: []grpcclient.RouteInfo{
		{Prefix: "2001:db8::/64", NextHop: "fe80::1", Protocol: "bgp", Metric: 20, Interface: "ge-0/0/0", Active: true},
	}}
	sh := &interactiveShell{
		client:    client,
		hostname:  "router",
		mode:      modeOperational,
		sessionID: "session-1",
	}

	err := sh.cmdShow(ctx, []string{"routes", "prefix", "2001:db8::/64", "protocol", "bgp"})
	if err != nil {
		t.Fatalf("cmdShow(routes) error = %v", err)
	}
	if client.routeCalls != 1 {
		t.Fatalf("route calls = %d, want 1", client.routeCalls)
	}
	if client.routePrefix != "2001:db8::/64" || client.routeStateProto != "bgp" {
		t.Fatalf("route filters = prefix %q proto %q, want prefix/proto", client.routePrefix, client.routeStateProto)
	}
	if client.routeFamily != "" || client.routeProtocol != "" {
		t.Fatalf("raw route filters = family %q proto %q, want unused", client.routeFamily, client.routeProtocol)
	}
}

func TestInterfaceQueueSummary(t *testing.T) {
	got := interfaceQueueSummary(grpcclient.InterfaceInfo{
		RxQueues: []grpcclient.InterfaceRxQueueInfo{
			{QueueID: 0, WorkerID: 1, Mode: "polling"},
		},
		TxQueues: []grpcclient.InterfaceTxQueueInfo{
			{QueueID: 0, Shared: true, Threads: []uint32{0, 2}},
		},
	})
	if got != "rx0:w1/polling tx0:[0,2]*" {
		t.Fatalf("interfaceQueueSummary() = %q", got)
	}
	if got := interfaceQueueSummary(grpcclient.InterfaceInfo{}); got != "-" {
		t.Fatalf("interfaceQueueSummary(empty) = %q, want -", got)
	}
}

func TestInterfaceQoSProfile(t *testing.T) {
	if got := interfaceQoSProfile(grpcclient.InterfaceInfo{QoSProfile: "WAN"}); got != "WAN" {
		t.Fatalf("interfaceQoSProfile() = %q, want WAN", got)
	}
	if got := interfaceQoSProfile(grpcclient.InterfaceInfo{}); got != "-" {
		t.Fatalf("interfaceQoSProfile(empty) = %q, want -", got)
	}
}

func TestInterfaceTableSummary(t *testing.T) {
	if got := interfaceTableSummary(grpcclient.InterfaceInfo{IPv4TableID: 100, IPv6TableID: 100}); got != "v4/v6:100" {
		t.Fatalf("interfaceTableSummary() = %q, want v4/v6:100", got)
	}
	if got := interfaceTableSummary(grpcclient.InterfaceInfo{IPv4TableID: 100, IPv6TableID: 200}); got != "v4:100 v6:200" {
		t.Fatalf("interfaceTableSummary(split) = %q, want v4:100 v6:200", got)
	}
	if got := interfaceTableSummary(grpcclient.InterfaceInfo{}); got != "-" {
		t.Fatalf("interfaceTableSummary(empty) = %q, want -", got)
	}
}

func TestRoutingInstanceTableSummary(t *testing.T) {
	if got := routingInstanceTableSummary(grpcclient.RoutingInstanceInfo{IPv4TableID: 100, IPv6TableID: 100}); got != "v4/v6:100" {
		t.Fatalf("routingInstanceTableSummary() = %q, want v4/v6:100", got)
	}
	if got := routingInstanceTableSummary(grpcclient.RoutingInstanceInfo{IPv4TableID: 100, IPv6TableID: 200}); got != "v4:100 v6:200" {
		t.Fatalf("routingInstanceTableSummary(split) = %q, want v4:100 v6:200", got)
	}
	if got := routingInstanceTableSummary(grpcclient.RoutingInstanceInfo{}); got != "-" {
		t.Fatalf("routingInstanceTableSummary(empty) = %q, want -", got)
	}
}

func TestRoutingInstancesNameFilter(t *testing.T) {
	got, err := routingInstancesNameFilter([]string{"BLUE"})
	if err != nil || got != "BLUE" {
		t.Fatalf("routingInstancesNameFilter(BLUE) = %q, %v; want BLUE, nil", got, err)
	}
	if _, err := routingInstancesNameFilter([]string{"BLUE", "RED"}); err == nil {
		t.Fatal("routingInstancesNameFilter(extra) error = nil, want error")
	}
	instances := []grpcclient.RoutingInstanceInfo{{Name: "BLUE"}, {Name: "RED"}}
	filtered := filterRoutingInstances(instances, "RED")
	if len(filtered) != 1 || filtered[0].Name != "RED" {
		t.Fatalf("filterRoutingInstances() = %#v, want RED", filtered)
	}
}

func TestOneShotShowConfigurationRollbackUsesArchivedConfig(t *testing.T) {
	client := &fakeInteractiveClient{
		history: []grpcclient.CommitInfo{
			{CommitID: "commit-new"},
			{CommitID: "commit-old"},
		},
		commitDetails: map[string]grpcclient.CommitInfo{
			"commit-old": {CommitID: "commit-old", ConfigText: "set system host-name old"},
		},
	}

	code := oneShotShow(context.Background(), client, []string{"configuration", "rollback", "1"}, &cliFlags{})
	if code != ExitSuccess {
		t.Fatalf("oneShotShow(configuration rollback 1) = %d, want %d", code, ExitSuccess)
	}
	if client.listHistoryCalls != 1 || client.listHistoryLimit != 2 || client.listHistoryOffset != 0 {
		t.Fatalf("ListHistory calls/limit/offset = %d/%d/%d, want 1/2/0",
			client.listHistoryCalls, client.listHistoryLimit, client.listHistoryOffset)
	}
	if client.getRunningCalls != 0 {
		t.Fatalf("GetRunning calls = %d, want 0", client.getRunningCalls)
	}
}

func TestOneShotShowConfigurationRollbackRejectsInvalidNumber(t *testing.T) {
	client := &fakeInteractiveClient{}

	code := oneShotShow(context.Background(), client, []string{"configuration", "rollback", "-1"}, &cliFlags{})
	if code != ExitUsageError {
		t.Fatalf("oneShotShow(configuration rollback -1) = %d, want %d", code, ExitUsageError)
	}
	if client.listHistoryCalls != 0 {
		t.Fatalf("ListHistory calls = %d, want 0", client.listHistoryCalls)
	}
}

func TestOneShotShowOSPFNeighborReturnsSuccess(t *testing.T) {
	client := &fakeInteractiveClient{ospfNeighbors: []grpcclient.OSPFNeighborInfo{{RouterID: "10.0.0.2", State: "Full"}}}
	code := oneShotShow(context.Background(), client, []string{"ospf", "neighbor"}, &cliFlags{})
	if code != ExitSuccess {
		t.Fatalf("oneShotShow(ospf neighbor) = %d, want %d", code, ExitSuccess)
	}
	if client.ospfFamily != routeAddressFamilyIPv4 {
		t.Fatalf("OSPF address family = %q, want %q", client.ospfFamily, routeAddressFamilyIPv4)
	}
	if client.ospfNeighborCalls != 1 {
		t.Fatalf("OSPF neighbor calls = %d, want 1", client.ospfNeighborCalls)
	}
}

func TestOneShotShowOSPF3NeighborReturnsSuccess(t *testing.T) {
	client := &fakeInteractiveClient{ospfNeighbors: []grpcclient.OSPFNeighborInfo{{RouterID: "10.0.0.3", State: "Full"}}}
	code := oneShotShow(context.Background(), client, []string{"ospf3", "neighbor"}, &cliFlags{})
	if code != ExitSuccess {
		t.Fatalf("oneShotShow(ospf3 neighbor) = %d, want %d", code, ExitSuccess)
	}
	if client.ospfFamily != routeAddressFamilyIPv6 {
		t.Fatalf("OSPF3 address family = %q, want %q", client.ospfFamily, routeAddressFamilyIPv6)
	}
	if client.ospfNeighborCalls != 1 {
		t.Fatalf("OSPF3 neighbor calls = %d, want 1", client.ospfNeighborCalls)
	}
}

func TestOneShotShowRouteInet6ProtocolReturnsSuccess(t *testing.T) {
	client := &fakeInteractiveClient{}
	code := oneShotShow(context.Background(), client, []string{"route", "inet6", "protocol", "ospf3"}, &cliFlags{})
	if code != ExitSuccess {
		t.Fatalf("oneShotShow(route inet6 protocol ospf3) = %d, want %d", code, ExitSuccess)
	}
	if client.routeFamily != routeAddressFamilyIPv6 || client.routeProtocol != "ospf3" {
		t.Fatalf("route options = family %q protocol %q, want inet6/ospf3", client.routeFamily, client.routeProtocol)
	}
}

func TestOneShotShowVRRPReturnsSuccess(t *testing.T) {
	client := &fakeInteractiveClient{}
	code := oneShotShow(context.Background(), client, []string{"vrrp"}, &cliFlags{})
	if code != ExitSuccess {
		t.Fatalf("oneShotShow(vrrp) = %d, want %d", code, ExitSuccess)
	}
}

func TestOneShotShowBFDReturnsSuccess(t *testing.T) {
	client := &fakeInteractiveClient{}
	code := oneShotShow(context.Background(), client, []string{"bfd", "brief"}, &cliFlags{})
	if code != ExitSuccess {
		t.Fatalf("oneShotShow(bfd brief) = %d, want %d", code, ExitSuccess)
	}
	if !client.bfdBrief || client.bfdCounters || client.bfdPeerAddress != "" {
		t.Fatalf("BFD options = peer %q brief %v counters %v, want brief", client.bfdPeerAddress, client.bfdBrief, client.bfdCounters)
	}
}

func TestOneShotShowBFDStatusReturnsSuccess(t *testing.T) {
	client := &fakeInteractiveClient{bfdInfo: &grpcclient.BFDStatusInfo{LastRun: time.Now(), ConfiguredPeers: 1}}
	code := oneShotShow(context.Background(), client, []string{"bfd", "status"}, &cliFlags{})
	if code != ExitSuccess {
		t.Fatalf("oneShotShow(bfd status) = %d, want %d", code, ExitSuccess)
	}
	if client.bfdStatusCalls != 1 {
		t.Fatalf("BFD status calls = %d, want 1", client.bfdStatusCalls)
	}
}

func TestOneShotShowLCPReturnsSuccess(t *testing.T) {
	client := &fakeInteractiveClient{}
	code := oneShotShow(context.Background(), client, []string{"lcp"}, &cliFlags{})
	if code != ExitSuccess {
		t.Fatalf("oneShotShow(lcp) = %d, want %d", code, ExitSuccess)
	}
}

func TestOneShotShowHAReturnsSuccess(t *testing.T) {
	client := &fakeInteractiveClient{}
	code := oneShotShow(context.Background(), client, []string{"ha"}, &cliFlags{})
	if code != ExitSuccess {
		t.Fatalf("oneShotShow(ha) = %d, want %d", code, ExitSuccess)
	}
}

func TestOneShotShowClassOfServiceReturnsSuccess(t *testing.T) {
	client := &fakeInteractiveClient{}
	code := oneShotShow(context.Background(), client, []string{"class-of-service"}, &cliFlags{})
	if code != ExitSuccess {
		t.Fatalf("oneShotShow(class-of-service) = %d, want %d", code, ExitSuccess)
	}
}

func TestOneShotShowEVPNReturnsSuccess(t *testing.T) {
	client := &fakeInteractiveClient{telemetryEvents: []*grpcclient.TelemetryEvent{evpnTelemetryTestEvent()}}
	code := oneShotShow(context.Background(), client, []string{"evpn"}, &cliFlags{})
	if code != ExitSuccess {
		t.Fatalf("oneShotShow(evpn) = %d, want %d", code, ExitSuccess)
	}
	if client.telemetryCalls != 1 {
		t.Fatalf("telemetry calls = %d, want 1", client.telemetryCalls)
	}
	if len(client.telemetryPaths) != 1 || client.telemetryPaths[0] != "/overlays/evpn" {
		t.Fatalf("telemetry paths = %#v, want /overlays/evpn", client.telemetryPaths)
	}
}

func TestOneShotShowRoutingInstancesReturnsSuccess(t *testing.T) {
	client := &fakeInteractiveClient{routingInstances: []grpcclient.RoutingInstanceInfo{{Name: "BLUE", IPv4TableID: 100, IPv6TableID: 100}}}
	code := oneShotShow(context.Background(), client, []string{"routing-instances"}, &cliFlags{})
	if code != ExitSuccess {
		t.Fatalf("oneShotShow(routing-instances) = %d, want %d", code, ExitSuccess)
	}
	if client.routingCalls != 1 {
		t.Fatalf("routing instance calls = %d, want 1", client.routingCalls)
	}
}

func TestOneShotShowBGPNeighborsReturnsSuccess(t *testing.T) {
	client := &fakeInteractiveClient{bgpNeighbors: []grpcclient.BGPNeighborInfo{{PeerAddress: "192.0.2.2", PeerAS: 65001, State: "Established"}}}
	code := oneShotShow(context.Background(), client, []string{"bgp", "neighbors"}, &cliFlags{})
	if code != ExitSuccess {
		t.Fatalf("oneShotShow(bgp neighbors) = %d, want %d", code, ExitSuccess)
	}
	if client.bgpNeighborCalls != 1 {
		t.Fatalf("BGP neighbor calls = %d, want 1", client.bgpNeighborCalls)
	}
}

func TestOneShotShowRoutesReturnsSuccess(t *testing.T) {
	client := &fakeInteractiveClient{routes: []grpcclient.RouteInfo{{Prefix: "192.0.2.0/24", Protocol: "connected", Active: true}}}
	code := oneShotShow(context.Background(), client, []string{"routes", "protocol", "connected"}, &cliFlags{})
	if code != ExitSuccess {
		t.Fatalf("oneShotShow(routes) = %d, want %d", code, ExitSuccess)
	}
	if client.routeCalls != 1 {
		t.Fatalf("route calls = %d, want 1", client.routeCalls)
	}
	if client.routeStateProto != "connected" {
		t.Fatalf("route protocol filter = %q, want connected", client.routeStateProto)
	}
}

func TestOneShotShowTelemetryReturnsSuccess(t *testing.T) {
	client := &fakeInteractiveClient{telemetryEvents: []*grpcclient.TelemetryEvent{
		{
			Sequence:      1,
			Timestamp:     time.Unix(100, 0).UTC(),
			Path:          "/system",
			Cardinality:   "single",
			PayloadSchema: "arca.telemetry.system.v1",
			EventType:     "snapshot",
			Encoding:      "json",
			SchemaVersion: "arca.telemetry.v1",
			JSONPayload:   `{"hostname":"router1"}`,
		},
	}}
	code := oneShotShow(context.Background(), client, []string{"telemetry", "path", "/system"}, &cliFlags{})
	if code != ExitSuccess {
		t.Fatalf("oneShotShow(telemetry) = %d, want %d", code, ExitSuccess)
	}
	if client.telemetryCalls != 1 {
		t.Fatalf("telemetry calls = %d, want 1", client.telemetryCalls)
	}
	if len(client.telemetryPaths) != 1 || client.telemetryPaths[0] != "/system" {
		t.Fatalf("telemetry paths = %#v, want /system", client.telemetryPaths)
	}
	if !client.telemetryOnce {
		t.Fatal("telemetry once = false, want true")
	}
}

func TestOneShotShowTelemetryPathsDoesNotSubscribe(t *testing.T) {
	client := &fakeInteractiveClient{}
	code := oneShotShow(context.Background(), client, []string{"telemetry", "paths"}, &cliFlags{})
	if code != ExitSuccess {
		t.Fatalf("oneShotShow(telemetry paths) = %d, want %d", code, ExitSuccess)
	}
	if client.telemetryCalls != 0 {
		t.Fatalf("telemetry calls = %d, want local catalog output without subscription", client.telemetryCalls)
	}
	if client.telemetryCatalogCalls != 0 {
		t.Fatalf("telemetry catalog calls = %d, want local catalog output without RPC", client.telemetryCatalogCalls)
	}
}

func TestOneShotShowTelemetryPathsLiveUsesCatalogRPC(t *testing.T) {
	client := &fakeInteractiveClient{telemetryCatalog: grpcclient.TelemetryCatalog{
		EventSchemaVersion: "arca.telemetry.v1",
		Encoding:           "json",
		DefaultPaths:       []string{"/system"},
		Paths: []grpcclient.TelemetryPathInfo{
			{Path: "/system", Description: "system", Cardinality: "single", PayloadSchema: "arca.telemetry.system.v1", Default: true},
			{Path: "/config/running", Description: "config", Cardinality: "single", PayloadSchema: "arca.telemetry.config.running.v1", Default: true},
			{Path: "/routes", Description: "routes", Cardinality: "per-route", PayloadSchema: "arca.telemetry.routes.v1"},
		},
	}}
	code := oneShotShow(context.Background(), client, []string{"telemetry", "paths", "live", "default", "path", "/system", "payload-schema", "arca.telemetry.system.v1", "encoding", "json"}, &cliFlags{})
	if code != ExitSuccess {
		t.Fatalf("oneShotShow(telemetry paths live default path payload-schema encoding) = %d, want %d", code, ExitSuccess)
	}
	if client.filteredTelemetryCatalogCalls != 1 {
		t.Fatalf("filtered telemetry catalog calls = %d, want 1 live catalog RPC", client.filteredTelemetryCatalogCalls)
	}
	if client.telemetryCatalogCalls != 0 {
		t.Fatalf("telemetry catalog calls = %d, want filtered catalog RPC", client.telemetryCatalogCalls)
	}
	if len(client.telemetryCatalogPaths) != 1 || client.telemetryCatalogPaths[0] != "/system" {
		t.Fatalf("telemetry catalog paths = %#v, want system path filter", client.telemetryCatalogPaths)
	}
	if !client.telemetryCatalogDefaultOnly {
		t.Fatal("telemetry catalog default-only = false, want true")
	}
	if len(client.telemetryCatalogSchemas) != 1 || client.telemetryCatalogSchemas[0] != "arca.telemetry.system.v1" {
		t.Fatalf("telemetry catalog schemas = %#v, want system payload schema filter", client.telemetryCatalogSchemas)
	}
	if len(client.telemetryCatalogEncodings) != 1 || client.telemetryCatalogEncodings[0] != "json" {
		t.Fatalf("telemetry catalog encodings = %#v, want json encoding filter", client.telemetryCatalogEncodings)
	}
	if client.telemetryCalls != 0 {
		t.Fatalf("telemetry calls = %d, want catalog lookup without subscription", client.telemetryCalls)
	}
}

func TestRunLocalOneShotTelemetryPaths(t *testing.T) {
	handled, code := runLocalOneShotCommand([]string{"show", "telemetry", "paths"})
	if !handled || code != ExitSuccess {
		t.Fatalf("runLocalOneShotCommand(show telemetry paths) = handled %v code %d, want local success", handled, code)
	}
	if handled, _ := runLocalOneShotCommand([]string{"show", "telemetry", "paths", "live"}); handled {
		t.Fatal("runLocalOneShotCommand(show telemetry paths live) handled live catalog command locally")
	}
	if handled, code := runLocalOneShotCommand([]string{"show", "telemetry", "paths", "cardinality", "per-route"}); !handled || code != ExitSuccess {
		t.Fatalf("runLocalOneShotCommand(show telemetry paths cardinality) = handled %v code %d, want local success", handled, code)
	}
	if handled, code := runLocalOneShotCommand([]string{"show", "telemetry", "paths", "path", "/evpn"}); !handled || code != ExitSuccess {
		t.Fatalf("runLocalOneShotCommand(show telemetry paths path /evpn) = handled %v code %d, want local success", handled, code)
	}
	if handled, code := runLocalOneShotCommand([]string{"show", "telemetry", "paths", "default"}); !handled || code != ExitSuccess {
		t.Fatalf("runLocalOneShotCommand(show telemetry paths default) = handled %v code %d, want local success", handled, code)
	}
	if handled, code := runLocalOneShotCommand([]string{"show", "telemetry", "paths", "encoding", "json"}); !handled || code != ExitSuccess {
		t.Fatalf("runLocalOneShotCommand(show telemetry paths encoding json) = handled %v code %d, want local success", handled, code)
	}
	if handled, _ := runLocalOneShotCommand([]string{"show", "telemetry", "paths", "live", "cardinality", "single"}); handled {
		t.Fatal("runLocalOneShotCommand(show telemetry paths live cardinality) handled live catalog command locally")
	}
	if handled, _ := runLocalOneShotCommand([]string{"show", "telemetry", "path", "/system"}); handled {
		t.Fatal("runLocalOneShotCommand(show telemetry path /system) handled streaming command locally")
	}
}

func TestRunLocalOneShotCompatibility(t *testing.T) {
	handled, code := runLocalOneShotCommand([]string{"show", "compatibility"})
	if !handled || code != ExitSuccess {
		t.Fatalf("runLocalOneShotCommand(show compatibility) = handled %v code %d, want local success", handled, code)
	}
	if handled, code := runLocalOneShotCommand([]string{"show", "compatibility", "extra"}); !handled || code != ExitUsageError {
		t.Fatalf("runLocalOneShotCommand(show compatibility extra) = handled %v code %d, want local usage error", handled, code)
	}
}

func TestTelemetryCatalogCommand(t *testing.T) {
	if !isTelemetryCatalogCommand([]string{"paths"}) || !isTelemetryCatalogCommand([]string{"catalog"}) {
		t.Fatal("isTelemetryCatalogCommand() did not recognize paths/catalog")
	}
	if !isTelemetryCatalogCommand([]string{"paths", "live"}) || !isTelemetryCatalogCommand([]string{"catalog", "live"}) {
		t.Fatal("isTelemetryCatalogCommand() did not recognize live paths/catalog")
	}
	if !isTelemetryCatalogCommand([]string{"paths", "cardinality", "per-route"}) ||
		!isTelemetryCatalogCommand([]string{"paths", "default"}) ||
		!isTelemetryCatalogCommand([]string{"paths", "default-only"}) ||
		!isTelemetryCatalogCommand([]string{"paths", "path", "/evpn"}) ||
		!isTelemetryCatalogCommand([]string{"paths", "encoding", "json"}) ||
		!isTelemetryCatalogCommand([]string{"catalog", "live", "path", "evpn", "payload-schema", "arca.telemetry.routes.v1", "encoding", "json"}) {
		t.Fatal("isTelemetryCatalogCommand() did not recognize filtered catalog commands")
	}
	if isTelemetryCatalogCommand([]string{"path", "/system"}) || isTelemetryCatalogCommand(nil) {
		t.Fatal("isTelemetryCatalogCommand() recognized non-catalog telemetry arguments")
	}
	if isTelemetryCatalogCommand([]string{"paths", "cardinality"}) ||
		isTelemetryCatalogCommand([]string{"paths", "path"}) ||
		isTelemetryCatalogCommand([]string{"paths", "encoding"}) ||
		isTelemetryCatalogCommand([]string{"paths", "unknown"}) {
		t.Fatal("isTelemetryCatalogCommand() recognized invalid catalog arguments")
	}
}

func TestFilterTelemetryPathCatalog(t *testing.T) {
	catalog := []grpcclient.TelemetryPathInfo{
		{Path: "/system", Cardinality: "single", PayloadSchema: "arca.telemetry.system.v1", Default: true},
		{Path: "/config/running", Cardinality: "single", PayloadSchema: "arca.telemetry.config.running.v1", Default: true},
		{Path: "/routes", Cardinality: "per-route", PayloadSchema: "arca.telemetry.routes.v1"},
		{Path: "/overlays/evpn", Cardinality: "per-vni", PayloadSchema: "arca.telemetry.overlays.evpn.v1", Aliases: []string{"/evpn"}},
		{Path: "/bfd", Cardinality: "per-peer", PayloadSchema: "arca.telemetry.bfd.v1"},
	}
	got := filterTelemetryPathCatalog(catalog, telemetryCatalogCLIOptions{
		cardinalities:  []string{"per-route", "per-peer"},
		payloadSchemas: []string{"ARCA.TELEMETRY.ROUTES.V1"},
	})
	if len(got) != 1 || got[0].Path != "/routes" {
		t.Fatalf("filterTelemetryPathCatalog() = %#v, want only /routes", got)
	}

	got = filterTelemetryPathCatalog(catalog, telemetryCatalogCLIOptions{payloadSchemas: []string{"arca.telemetry.system.v1"}})
	if len(got) != 1 || got[0].Path != "/system" {
		t.Fatalf("filterTelemetryPathCatalog(payload schema) = %#v, want only /system", got)
	}

	got = filterTelemetryPathCatalog(catalog, telemetryCatalogCLIOptions{paths: []string{"evpn"}})
	if len(got) != 1 || got[0].Path != "/overlays/evpn" {
		t.Fatalf("filterTelemetryPathCatalog(path alias) = %#v, want only /overlays/evpn", got)
	}

	got = filterTelemetryPathCatalog(catalog, telemetryCatalogCLIOptions{defaultOnly: true})
	if len(got) != 2 || got[0].Path != "/system" || got[1].Path != "/config/running" {
		t.Fatalf("filterTelemetryPathCatalog(default only) = %#v, want default paths", got)
	}

	got = filterTelemetryPathCatalog(catalog, telemetryCatalogCLIOptions{encodings: []string{" JSON "}})
	if len(got) != len(catalog) {
		t.Fatalf("filterTelemetryPathCatalog(encoding) = %#v, want full catalog", got)
	}

	got = filterTelemetryPathCatalog(catalog, telemetryCatalogCLIOptions{encodings: []string{"protobuf"}})
	if len(got) != 0 {
		t.Fatalf("filterTelemetryPathCatalog(unsupported encoding) = %#v, want none", got)
	}
}

func TestFormatTelemetryCatalogIntervalHints(t *testing.T) {
	catalog := grpcclient.TelemetryCatalog{
		DefaultSampleIntervalMs: 30000,
		MinSampleIntervalMs:     1000,
		MaxSampleIntervalMs:     3600000,
	}
	if got, want := formatTelemetryCatalogIntervalHints(catalog), "Sample interval: default=30000ms min=1000ms max=3600000ms"; got != want {
		t.Fatalf("formatTelemetryCatalogIntervalHints() = %q, want %q", got, want)
	}
	if got := formatTelemetryCatalogIntervalHints(grpcclient.TelemetryCatalog{}); got != "" {
		t.Fatalf("formatTelemetryCatalogIntervalHints(empty) = %q, want empty", got)
	}
}

func TestTelemetryPayloadBytes(t *testing.T) {
	event := &grpcclient.TelemetryEvent{
		JSONPayload:  `{"hostname":"router1"}`,
		PayloadBytes: 99,
	}
	if got := telemetryPayloadBytes(event); got != 99 {
		t.Fatalf("telemetryPayloadBytes() = %d, want explicit payload bytes", got)
	}
	event.PayloadBytes = 0
	if got, want := telemetryPayloadBytes(event), len(event.JSONPayload); got != want {
		t.Fatalf("telemetryPayloadBytes() = %d, want fallback payload length %d", got, want)
	}
}

func TestShowEVPNRejectsInvalidPayload(t *testing.T) {
	client := &fakeInteractiveClient{telemetryEvents: []*grpcclient.TelemetryEvent{
		{
			Path:        "/overlays/evpn",
			JSONPayload: `{"vnis":`,
		},
	}}
	err := showEVPN(context.Background(), client)
	if err == nil || !strings.Contains(err.Error(), "decode EVPN telemetry snapshot") {
		t.Fatalf("showEVPN() error = %v, want decode failure", err)
	}
}

func TestCountEVPNVNIs(t *testing.T) {
	counts := countEVPNVNIs([]evpnTelemetryVNI{
		{VNI: 10010, Type: "l2", MulticastGroup: "239.0.0.10"},
		{VNI: 20010, Type: "l3"},
		{VNI: 30010, Type: "l2"},
	})
	if counts.total != 3 || counts.l2 != 2 || counts.l3 != 1 || counts.multicast != 1 {
		t.Fatalf("countEVPNVNIs() = %#v, want total 3 l2 2 l3 1 multicast 1", counts)
	}
}

func TestFormatEVPNEndpoint(t *testing.T) {
	if got := formatEVPNEndpoint(evpnTelemetryVNI{MulticastGroup: "239.0.0.10"}); got != "multicast:239.0.0.10" {
		t.Fatalf("formatEVPNEndpoint(multicast) = %q", got)
	}
	if got := formatEVPNEndpoint(evpnTelemetryVNI{RemoteVTEP: "198.51.100.10"}); got != "remote:198.51.100.10" {
		t.Fatalf("formatEVPNEndpoint(remote) = %q", got)
	}
	if got := formatEVPNEndpoint(evpnTelemetryVNI{}); got != "-" {
		t.Fatalf("formatEVPNEndpoint(empty) = %q", got)
	}
}

func TestTelemetryOptions(t *testing.T) {
	opts, err := telemetryOptions([]string{"path", "/system", "path", "/routes", "interval", "5s", "count", "3"})
	if err != nil {
		t.Fatalf("telemetryOptions() error = %v", err)
	}
	if len(opts.paths) != 2 || opts.paths[0] != "/system" || opts.paths[1] != "/routes" {
		t.Fatalf("paths = %#v, want /system and /routes", opts.paths)
	}
	if opts.interval != 5*time.Second {
		t.Fatalf("interval = %v, want 5s", opts.interval)
	}
	if opts.once || opts.count != 3 {
		t.Fatalf("once/count = %v/%d, want false/3", opts.once, opts.count)
	}

	if _, err := telemetryOptions([]string{"interval"}); !isTelemetryUsageError(err) {
		t.Fatalf("telemetryOptions(interval) error = %v, want usage error", err)
	}
	if _, err := telemetryOptions([]string{"count", "0"}); !isTelemetryUsageError(err) {
		t.Fatalf("telemetryOptions(count 0) error = %v, want usage error", err)
	}
}

func TestRouteTextOptions(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantProto  string
		wantFamily string
		wantErr    bool
	}{
		{name: "default", wantFamily: routeAddressFamilyIPv4},
		{name: "inet", args: []string{"inet"}, wantFamily: routeAddressFamilyIPv4},
		{name: "inet6", args: []string{"inet6"}, wantFamily: routeAddressFamilyIPv6},
		{name: "ipv4 protocol", args: []string{"protocol", "ospf"}, wantProto: "ospf", wantFamily: routeAddressFamilyIPv4},
		{name: "ipv6 protocol", args: []string{"inet6", "protocol", "ospf3"}, wantProto: "ospf3", wantFamily: routeAddressFamilyIPv6},
		{name: "unknown address family", args: []string{"ipv6"}, wantErr: true},
		{name: "missing protocol", args: []string{"protocol"}, wantErr: true},
		{name: "ipv4 ospf3", args: []string{"protocol", "ospf3"}, wantErr: true},
		{name: "ipv6 ospf", args: []string{"inet6", "protocol", "ospf"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotProto, gotFamily, err := routeTextOptions(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("routeTextOptions(%v) error = nil, want error", tt.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("routeTextOptions(%v) error = %v", tt.args, err)
			}
			if gotProto != tt.wantProto || gotFamily != tt.wantFamily {
				t.Fatalf("routeTextOptions(%v) = %q, %q; want %q, %q", tt.args, gotProto, gotFamily, tt.wantProto, tt.wantFamily)
			}
		})
	}
}

func TestRouteStateOptions(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantPrefix string
		wantProto  string
		wantErr    bool
	}{
		{name: "default"},
		{name: "prefix", args: []string{"prefix", "192.0.2.0/24"}, wantPrefix: "192.0.2.0/24"},
		{name: "protocol", args: []string{"protocol", "ospf3"}, wantProto: "ospf3"},
		{name: "prefix protocol", args: []string{"prefix", "2001:db8::/64", "protocol", "bgp"}, wantPrefix: "2001:db8::/64", wantProto: "bgp"},
		{name: "protocol prefix", args: []string{"protocol", "connected", "prefix", "192.0.2.0/24"}, wantPrefix: "192.0.2.0/24", wantProto: "connected"},
		{name: "unknown", args: []string{"inet6"}, wantErr: true},
		{name: "missing prefix", args: []string{"prefix"}, wantErr: true},
		{name: "duplicate prefix", args: []string{"prefix", "192.0.2.0/24", "prefix", "198.51.100.0/24"}, wantErr: true},
		{name: "missing protocol", args: []string{"protocol"}, wantErr: true},
		{name: "invalid protocol", args: []string{"protocol", "rip"}, wantErr: true},
		{name: "duplicate protocol", args: []string{"protocol", "bgp", "protocol", "static"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPrefix, gotProto, err := routeStateOptions(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("routeStateOptions(%v) error = nil, want error", tt.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("routeStateOptions(%v) error = %v", tt.args, err)
			}
			if gotPrefix != tt.wantPrefix || gotProto != tt.wantProto {
				t.Fatalf("routeStateOptions(%v) = %q, %q; want %q, %q", tt.args, gotPrefix, gotProto, tt.wantPrefix, tt.wantProto)
			}
		})
	}
}

func TestBFDTextOptions(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		wantPeer     string
		wantBrief    bool
		wantCounters bool
		wantErr      bool
	}{
		{name: "default"},
		{name: "brief", args: []string{"brief"}, wantBrief: true},
		{name: "counters", args: []string{"counters"}, wantCounters: true},
		{name: "peer", args: []string{"peer", "192.0.2.2"}, wantPeer: "192.0.2.2"},
		{name: "peer counters", args: []string{"peer", "192.0.2.2", "counters"}, wantPeer: "192.0.2.2", wantCounters: true},
		{name: "unknown", args: []string{"detail"}, wantErr: true},
		{name: "peer missing address", args: []string{"peer"}, wantErr: true},
		{name: "peer bad extra", args: []string{"peer", "192.0.2.2", "brief"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPeer, gotBrief, gotCounters, err := bfdTextOptions(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("bfdTextOptions(%v) error = nil, want error", tt.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("bfdTextOptions(%v) error = %v", tt.args, err)
			}
			if gotPeer != tt.wantPeer || gotBrief != tt.wantBrief || gotCounters != tt.wantCounters {
				t.Fatalf("bfdTextOptions(%v) = %q, %v, %v; want %q, %v, %v",
					tt.args, gotPeer, gotBrief, gotCounters, tt.wantPeer, tt.wantBrief, tt.wantCounters)
			}
		})
	}
}

func TestBFDStatusRequested(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    bool
		wantErr bool
	}{
		{name: "default"},
		{name: "status", args: []string{"status"}, want: true},
		{name: "status extra", args: []string{"status", "detail"}, wantErr: true},
		{name: "brief", args: []string{"brief"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := bfdStatusRequested(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("bfdStatusRequested(%v) error = nil, want error", tt.args)
				}
				return
			}
			if err != nil {
				t.Fatalf("bfdStatusRequested(%v) error = %v", tt.args, err)
			}
			if got != tt.want {
				t.Fatalf("bfdStatusRequested(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestLCPReconciliationState(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		info *grpcclient.LCPReconciliationInfo
		want string
	}{
		{name: "nil", info: nil, want: "unknown"},
		{name: "never", info: &grpcclient.LCPReconciliationInfo{}, want: "unknown"},
		{name: "error", info: &grpcclient.LCPReconciliationInfo{LastRun: now, LastError: "failed"}, want: "check failed"},
		{name: "mismatch", info: &grpcclient.LCPReconciliationInfo{LastRun: now, Inconsistencies: []string{"missing pair"}}, want: "mismatch"},
		{name: "consistent", info: &grpcclient.LCPReconciliationInfo{LastRun: now}, want: "consistent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lcpReconciliationState(tt.info); got != tt.want {
				t.Fatalf("lcpReconciliationState() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBFDOperationalState(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		info *grpcclient.BFDStatusInfo
		want string
	}{
		{name: "nil", info: nil, want: "unknown"},
		{name: "empty", info: &grpcclient.BFDStatusInfo{}, want: "unknown"},
		{name: "error", info: &grpcclient.BFDStatusInfo{LastRun: now, LastError: "failed"}, want: "check failed"},
		{name: "issue", info: &grpcclient.BFDStatusInfo{LastRun: now, Issues: []string{"peer missing"}}, want: "issues"},
		{name: "down", info: &grpcclient.BFDStatusInfo{LastRun: now, DownPeers: 1}, want: "issues"},
		{name: "converged", info: &grpcclient.BFDStatusInfo{LastRun: now, ConfiguredPeers: 1, ObservedPeers: 1, UpPeers: 1}, want: "converged"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bfdOperationalState(tt.info); got != tt.want {
				t.Fatalf("bfdOperationalState() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatBGPUptime(t *testing.T) {
	tests := []struct {
		name    string
		seconds uint64
		want    string
	}{
		{name: "zero", want: "-"},
		{name: "seconds", seconds: 7, want: "7s"},
		{name: "minutes", seconds: 67, want: "1m07s"},
		{name: "hours", seconds: 3661, want: "1h01m01s"},
		{name: "days", seconds: 90061, want: "1d01h01m01s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatBGPUptime(tt.seconds); got != tt.want {
				t.Fatalf("formatBGPUptime(%d) = %q, want %q", tt.seconds, got, tt.want)
			}
		})
	}
}

func TestHAState(t *testing.T) {
	tests := []struct {
		name string
		info *grpcclient.HAStatusInfo
		want string
	}{
		{name: "nil", info: nil, want: "not configured"},
		{name: "disabled", info: &grpcclient.HAStatusInfo{}, want: "not configured"},
		{name: "issues", info: &grpcclient.HAStatusInfo{Configured: true}, want: "issues"},
		{name: "converged", info: &grpcclient.HAStatusInfo{Configured: true, Converged: true}, want: "converged"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := haState(tt.info); got != tt.want {
				t.Fatalf("haState() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHABFDState(t *testing.T) {
	now := time.Unix(1700000400, 0)
	tests := []struct {
		name string
		info *grpcclient.HAStatusInfo
		want string
	}{
		{name: "nil", info: nil, want: "not configured"},
		{name: "empty", info: &grpcclient.HAStatusInfo{}, want: "not configured"},
		{
			name: "converged",
			info: &grpcclient.HAStatusInfo{
				FRRBFDLastCheck:       now,
				FRRBFDConfiguredPeers: 2,
				FRRBFDObservedPeers:   2,
				FRRBFDUpPeers:         2,
			},
			want: "2/2 up",
		},
		{
			name: "issues",
			info: &grpcclient.HAStatusInfo{
				FRRBFDLastCheck:       now,
				FRRBFDConfiguredPeers: 2,
				FRRBFDObservedPeers:   2,
				FRRBFDUpPeers:         1,
				FRRBFDDownPeers:       1,
			},
			want: "1/2 up (issues)",
		},
		{
			name: "unknown",
			info: &grpcclient.HAStatusInfo{
				FRRBFDConfiguredPeers: 1,
				FRRBFDObservedPeers:   1,
				FRRBFDUpPeers:         1,
			},
			want: "1/1 up (unknown)",
		},
		{
			name: "observed only",
			info: &grpcclient.HAStatusInfo{
				FRRBFDLastCheck:     now,
				FRRBFDObservedPeers: 1,
				FRRBFDUpPeers:       1,
			},
			want: "1/1 up",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := haBFDState(tt.info); got != tt.want {
				t.Fatalf("haBFDState() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRollbackRejectsInvalidNumber(t *testing.T) {
	ctx := context.Background()

	for _, arg := range []string{"-1", "1abc"} {
		client := &fakeInteractiveClient{}
		sh := &interactiveShell{
			client:    client,
			hostname:  "router",
			mode:      modeConfiguration,
			sessionID: "session-1",
			hasLock:   true,
		}

		err := sh.cmdRollback(ctx, []string{arg})
		if err == nil || !strings.Contains(err.Error(), "invalid rollback number") {
			t.Fatalf("cmdRollback(%s) error = %v, want invalid rollback number", arg, err)
		}
		if client.listHistoryCalls != 0 || client.rollbackCalls != 0 {
			t.Fatalf("list/rollback calls for %q = %d/%d, want 0/0", arg, client.listHistoryCalls, client.rollbackCalls)
		}
	}
}
