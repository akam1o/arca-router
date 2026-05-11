package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	grpcclient "github.com/akam1o/arca-router/internal/northbound/grpc"
)

type fakeInteractiveClient struct {
	acquireLockErr error
	discardErr     error
	releaseLockErr error
	history        []grpcclient.CommitInfo

	acquireLockCalls int
	discardCalls     int
	releaseLockCalls int
	commitCalls      int
	listHistoryCalls int
	rollbackCalls    int
}

func (f *fakeInteractiveClient) GetRunning(ctx context.Context) (string, uint64, error) {
	return "", 0, nil
}

func (f *fakeInteractiveClient) GetCandidate(ctx context.Context, sessionID string) (string, error) {
	return "", nil
}

func (f *fakeInteractiveClient) EditCandidate(ctx context.Context, sessionID, configText string) error {
	return nil
}

func (f *fakeInteractiveClient) Commit(ctx context.Context, sessionID, user, message string) (string, uint64, error) {
	f.commitCalls++
	return "commit-1234567890", 2, nil
}

func (f *fakeInteractiveClient) ValidateCandidate(ctx context.Context, sessionID string) error {
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
