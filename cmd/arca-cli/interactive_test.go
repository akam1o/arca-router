package main

import (
	"context"
	"testing"
	"time"

	"github.com/akam1o/arca-router/pkg/cli"
	"github.com/akam1o/arca-router/pkg/datastore"
)

type interactiveTestDatastore struct {
	lockSessionID    string
	lockAcquired     bool
	acquireLockCount int
	history          []*datastore.CommitHistoryEntry
}

func (d *interactiveTestDatastore) GetRunning(ctx context.Context) (*datastore.RunningConfig, error) {
	return &datastore.RunningConfig{
		CommitID:   "running-commit",
		ConfigText: "set system host-name router",
		Timestamp:  time.Now(),
	}, nil
}

func (d *interactiveTestDatastore) GetCandidate(ctx context.Context, sessionID string) (*datastore.CandidateConfig, error) {
	return &datastore.CandidateConfig{
		SessionID:  sessionID,
		ConfigText: "set system host-name router",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}, nil
}

func (d *interactiveTestDatastore) SaveCandidate(ctx context.Context, sessionID string, configText string) error {
	return nil
}

func (d *interactiveTestDatastore) DeleteCandidate(ctx context.Context, sessionID string) error {
	return nil
}

func (d *interactiveTestDatastore) Commit(ctx context.Context, req *datastore.CommitRequest) (string, error) {
	d.lockAcquired = false
	d.lockSessionID = ""
	return "commit-1234567890", nil
}

func (d *interactiveTestDatastore) Rollback(ctx context.Context, req *datastore.RollbackRequest) (string, error) {
	d.lockAcquired = false
	d.lockSessionID = ""
	return "rollback-1234567890", nil
}

func (d *interactiveTestDatastore) CompareCandidateRunning(ctx context.Context, sessionID string) (*datastore.DiffResult, error) {
	return &datastore.DiffResult{}, nil
}

func (d *interactiveTestDatastore) CompareCommits(ctx context.Context, commitID1, commitID2 string) (*datastore.DiffResult, error) {
	return &datastore.DiffResult{}, nil
}

func (d *interactiveTestDatastore) AcquireLock(ctx context.Context, req *datastore.LockRequest) error {
	d.acquireLockCount++
	d.lockSessionID = req.SessionID
	d.lockAcquired = true
	return nil
}

func (d *interactiveTestDatastore) ReleaseLock(ctx context.Context, target string, sessionID string) error {
	d.lockAcquired = false
	d.lockSessionID = ""
	return nil
}

func (d *interactiveTestDatastore) ExtendLock(ctx context.Context, target string, sessionID string, duration time.Duration) error {
	return nil
}

func (d *interactiveTestDatastore) StealLock(ctx context.Context, req *datastore.StealLockRequest) error {
	return nil
}

func (d *interactiveTestDatastore) GetLockInfo(ctx context.Context, target string) (*datastore.LockInfo, error) {
	if !d.lockAcquired {
		return &datastore.LockInfo{IsLocked: false}, nil
	}
	return &datastore.LockInfo{
		IsLocked:   true,
		SessionID:  d.lockSessionID,
		User:       "testuser",
		AcquiredAt: time.Now().Add(-time.Minute),
		ExpiresAt:  time.Now().Add(29 * time.Minute),
	}, nil
}

func (d *interactiveTestDatastore) ListCommitHistory(ctx context.Context, opts *datastore.HistoryOptions) ([]*datastore.CommitHistoryEntry, error) {
	if d.history != nil {
		return d.history, nil
	}
	return []*datastore.CommitHistoryEntry{
		{CommitID: "commit-1", User: "testuser", Timestamp: time.Now(), Message: "test"},
	}, nil
}

func (d *interactiveTestDatastore) GetCommit(ctx context.Context, commitID string) (*datastore.CommitHistoryEntry, error) {
	return &datastore.CommitHistoryEntry{CommitID: commitID}, nil
}

func (d *interactiveTestDatastore) LogAuditEvent(ctx context.Context, event *datastore.AuditEvent) error {
	return nil
}

func (d *interactiveTestDatastore) CleanupAuditLog(ctx context.Context, cutoff time.Time) (int64, error) {
	return 0, nil
}

func (d *interactiveTestDatastore) Close() error {
	return nil
}

func TestInteractiveCommitAndQuitLeavesOperational(t *testing.T) {
	ctx := context.Background()
	ds := &interactiveTestDatastore{}
	session := cli.NewSession("testuser", ds)
	sh := &InteractiveShell{session: session, hostname: "router"}

	if err := session.EnterConfigurationMode(ctx); err != nil {
		t.Fatalf("EnterConfigurationMode() error = %v", err)
	}
	if err := sh.cmdCommit(ctx, []string{"and-quit"}); err != nil {
		t.Fatalf("cmdCommit(and-quit) error = %v", err)
	}
	if session.Mode() != cli.ModeOperational {
		t.Fatalf("mode = %v, want operational", session.Mode())
	}
	if ds.lockAcquired {
		t.Fatal("datastore lock was not released")
	}
}
