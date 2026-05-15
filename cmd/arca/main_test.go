package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	grpcclient "github.com/akam1o/arca-router/internal/northbound/grpc"
)

type fakeInteractiveClient struct {
	acquireLockErr   error
	discardErr       error
	releaseLockErr   error
	history          []grpcclient.CommitInfo
	routeText        string
	routeProtocol    string
	routeFamily      string
	routePrefix      string
	routeStateProto  string
	routes           []grpcclient.RouteInfo
	routingInstances []grpcclient.RoutingInstanceInfo
	bgpNeighbors     []grpcclient.BGPNeighborInfo
	bgpSummaryText   string
	bgpNeighborText  string
	ospfNeighbors    []grpcclient.OSPFNeighborInfo
	ospfText         string
	ospfFamily       string
	vrrpText         string
	bfdText          string
	bfdInfo          *grpcclient.BFDStatusInfo
	bfdPeerAddress   string
	bfdBrief         bool
	bfdCounters      bool
	lcpInfo          *grpcclient.LCPReconciliationInfo
	haInfo           *grpcclient.HAStatusInfo
	cosInfo          *grpcclient.ClassOfServiceInfo
	telemetryCatalog grpcclient.TelemetryCatalog
	telemetryEvents  []*grpcclient.TelemetryEvent

	acquireLockCalls      int
	discardCalls          int
	releaseLockCalls      int
	commitCalls           int
	routeCalls            int
	bfdStatusCalls        int
	routingCalls          int
	bgpNeighborCalls      int
	ospfNeighborCalls     int
	listHistoryCalls      int
	rollbackCalls         int
	telemetryCatalogCalls int
	telemetryCalls        int
	validateCalls         int
	editTexts             []string
	telemetryPaths        []string
	telemetryInterval     time.Duration
	telemetryOnce         bool
}

func (f *fakeInteractiveClient) GetRunning(ctx context.Context) (string, uint64, error) {
	return "", 0, nil
}

func (f *fakeInteractiveClient) GetCandidate(ctx context.Context, sessionID string) (string, error) {
	return "", nil
}

func (f *fakeInteractiveClient) EditCandidate(ctx context.Context, sessionID, configText string) error {
	f.editTexts = append(f.editTexts, configText)
	return nil
}

func (f *fakeInteractiveClient) Commit(ctx context.Context, sessionID, user, message string) (string, uint64, error) {
	f.commitCalls++
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
	return "", false, nil
}

func (f *fakeInteractiveClient) ListHistory(ctx context.Context, limit, offset int) ([]grpcclient.CommitInfo, error) {
	f.listHistoryCalls++
	return f.history, nil
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
		EventType:     "snapshot",
		Encoding:      "json",
		SchemaVersion: "arca.telemetry.v1",
		JSONPayload: `{"vnis":[` +
			`{"vni":10010,"type":"l2","bridge_domain":"BD-10","vlan_id":10,"route_distinguisher":"65000:10010","vrf_target":"target:65000:10010","source_interface":"ge-0/0/0","source_address":"192.0.2.1","multicast_group":"239.0.0.10"},` +
			`{"vni":20010,"type":"l3","routing_instance":"BLUE","vrf_target_import":["target:65000:20010"],"vrf_target_export":["target:65000:20011"]}` +
			`]}`,
	}
}

func TestCmdConfigureRequiresSession(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{}
	sh := &interactiveShell{
		client:   client,
		hostname: "router",
		mode:     modeOperational,
	}

	err := sh.cmdConfigure(ctx)
	if err == nil || !strings.Contains(err.Error(), "configuration session is not available") {
		t.Fatalf("cmdConfigure() error = %v, want missing session", err)
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
			{Path: "/routes", Description: "routes", Cardinality: "per-route", PayloadSchema: "arca.telemetry.routes.v1"},
		},
	}}
	code := oneShotShow(context.Background(), client, []string{"telemetry", "paths", "live", "payload-schema", "arca.telemetry.system.v1"}, &cliFlags{})
	if code != ExitSuccess {
		t.Fatalf("oneShotShow(telemetry paths live payload-schema) = %d, want %d", code, ExitSuccess)
	}
	if client.telemetryCatalogCalls != 1 {
		t.Fatalf("telemetry catalog calls = %d, want 1 live catalog RPC", client.telemetryCatalogCalls)
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
	if handled, _ := runLocalOneShotCommand([]string{"show", "telemetry", "paths", "live", "cardinality", "single"}); handled {
		t.Fatal("runLocalOneShotCommand(show telemetry paths live cardinality) handled live catalog command locally")
	}
	if handled, _ := runLocalOneShotCommand([]string{"show", "telemetry", "path", "/system"}); handled {
		t.Fatal("runLocalOneShotCommand(show telemetry path /system) handled streaming command locally")
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
		!isTelemetryCatalogCommand([]string{"catalog", "live", "payload-schema", "arca.telemetry.routes.v1"}) {
		t.Fatal("isTelemetryCatalogCommand() did not recognize filtered catalog commands")
	}
	if isTelemetryCatalogCommand([]string{"path", "/system"}) || isTelemetryCatalogCommand(nil) {
		t.Fatal("isTelemetryCatalogCommand() recognized non-catalog telemetry arguments")
	}
	if isTelemetryCatalogCommand([]string{"paths", "cardinality"}) ||
		isTelemetryCatalogCommand([]string{"paths", "unknown"}) {
		t.Fatal("isTelemetryCatalogCommand() recognized invalid catalog arguments")
	}
}

func TestFilterTelemetryPathCatalog(t *testing.T) {
	catalog := []grpcclient.TelemetryPathInfo{
		{Path: "/system", Cardinality: "single", PayloadSchema: "arca.telemetry.system.v1"},
		{Path: "/routes", Cardinality: "per-route", PayloadSchema: "arca.telemetry.routes.v1"},
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
