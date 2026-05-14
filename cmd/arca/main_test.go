package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	grpcclient "github.com/akam1o/arca-router/internal/northbound/grpc"
)

type fakeInteractiveClient struct {
	acquireLockErr  error
	discardErr      error
	releaseLockErr  error
	history         []grpcclient.CommitInfo
	routeText       string
	routeProtocol   string
	routeFamily     string
	bgpSummaryText  string
	bgpNeighborText string
	ospfText        string
	ospfFamily      string
	vrrpText        string
	bfdText         string
	bfdPeerAddress  string
	bfdBrief        bool
	bfdCounters     bool
	lcpInfo         *grpcclient.LCPReconciliationInfo
	haInfo          *grpcclient.HAStatusInfo
	cosInfo         *grpcclient.ClassOfServiceInfo

	acquireLockCalls int
	discardCalls     int
	releaseLockCalls int
	commitCalls      int
	listHistoryCalls int
	rollbackCalls    int
	validateCalls    int
	editTexts        []string
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
	return nil, nil
}

func (f *fakeInteractiveClient) GetBGPNeighbors(ctx context.Context) ([]grpcclient.BGPNeighborInfo, error) {
	return nil, nil
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
	client := &fakeInteractiveClient{}
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
}

func TestCmdShowOSPF3NeighborReturnsOutput(t *testing.T) {
	ctx := context.Background()
	client := &fakeInteractiveClient{}
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

func TestOneShotShowOSPFNeighborReturnsSuccess(t *testing.T) {
	client := &fakeInteractiveClient{}
	code := oneShotShow(context.Background(), client, []string{"ospf", "neighbor"}, &cliFlags{})
	if code != ExitSuccess {
		t.Fatalf("oneShotShow(ospf neighbor) = %d, want %d", code, ExitSuccess)
	}
	if client.ospfFamily != routeAddressFamilyIPv4 {
		t.Fatalf("OSPF address family = %q, want %q", client.ospfFamily, routeAddressFamilyIPv4)
	}
}

func TestOneShotShowOSPF3NeighborReturnsSuccess(t *testing.T) {
	client := &fakeInteractiveClient{}
	code := oneShotShow(context.Background(), client, []string{"ospf3", "neighbor"}, &cliFlags{})
	if code != ExitSuccess {
		t.Fatalf("oneShotShow(ospf3 neighbor) = %d, want %d", code, ExitSuccess)
	}
	if client.ospfFamily != routeAddressFamilyIPv6 {
		t.Fatalf("OSPF3 address family = %q, want %q", client.ospfFamily, routeAddressFamilyIPv6)
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
