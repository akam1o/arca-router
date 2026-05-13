package main

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	"github.com/akam1o/arca-router/internal/store"
	"github.com/akam1o/arca-router/pkg/datastore"
)

type configSyncTestStore struct {
	snap *model.ConfigSnapshot
	err  error
	gets int
}

func (s *configSyncTestStore) GetLatestSnapshot(ctx context.Context) (*model.ConfigSnapshot, error) {
	s.gets++
	if s.err != nil {
		return nil, s.err
	}
	if s.snap == nil {
		return nil, nil
	}
	return s.snap.Clone(), nil
}

func (s *configSyncTestStore) PrepareCommit(ctx context.Context, snap *model.ConfigSnapshot) (store.PreparedCommit, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *configSyncTestStore) SaveCommit(ctx context.Context, snap *model.ConfigSnapshot) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (s *configSyncTestStore) GetCommit(ctx context.Context, commitID string) (*store.CommitRecord, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *configSyncTestStore) ListCommits(ctx context.Context, opts *store.ListOptions) ([]*store.CommitRecord, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *configSyncTestStore) AuditLog(ctx context.Context, event *store.AuditEvent) error {
	return nil
}

func (s *configSyncTestStore) Close() error {
	return nil
}

type configSyncTestEtcdStatus struct {
	status *datastore.EtcdStatus
	err    error
}

func (s configSyncTestEtcdStatus) EtcdStatus(ctx context.Context) (*datastore.EtcdStatus, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.status == nil {
		return &datastore.EtcdStatus{}, nil
	}
	copy := *s.status
	copy.Endpoints = append([]string(nil), s.status.Endpoints...)
	return &copy, nil
}

func TestEtcdConfigSynchronizerAppliesNewRunningRevision(t *testing.T) {
	eng := engine.NewEngine(nil, slog.Default())
	eng.InitializeRunning(testSyncConfig("edge-old"), 1)
	st := &configSyncTestStore{
		snap: model.NewSnapshot(testSyncConfig("edge-new"), 2, "alice", "remote commit"),
	}
	syncer := newEtcdConfigSynchronizer(st, eng, configSyncTestEtcdStatus{
		status: &datastore.EtcdStatus{
			Revision:        11,
			RunningRevision: 10,
			RunningCommitID: "commit-10",
		},
	}, 0, slog.Default())

	if err := syncer.reconcile(context.Background()); err != nil {
		t.Fatalf("reconcile() error = %v", err)
	}
	if got := eng.Running().System.HostName; got != "edge-new" {
		t.Fatalf("running hostname = %q, want edge-new", got)
	}
	status := syncer.ConfigSyncStatus()
	if !status.Enabled || !status.Healthy || status.RunningRevision != 10 || status.RunningCommitID != "commit-10" ||
		status.LastApply.IsZero() {
		t.Fatalf("ConfigSyncStatus() = %#v, want healthy applied revision 10", status)
	}
}

func TestEtcdConfigSynchronizerDoesNotReloadUnchangedRevision(t *testing.T) {
	eng := engine.NewEngine(nil, slog.Default())
	eng.InitializeRunning(testSyncConfig("edge-old"), 1)
	st := &configSyncTestStore{
		snap: model.NewSnapshot(testSyncConfig("edge-new"), 2, "alice", "remote commit"),
	}
	syncer := newEtcdConfigSynchronizer(st, eng, configSyncTestEtcdStatus{
		status: &datastore.EtcdStatus{Revision: 11, RunningRevision: 10},
	}, 0, slog.Default())

	if err := syncer.reconcile(context.Background()); err != nil {
		t.Fatalf("first reconcile() error = %v", err)
	}
	st.snap = model.NewSnapshot(testSyncConfig("edge-later"), 3, "alice", "remote commit")
	if err := syncer.reconcile(context.Background()); err != nil {
		t.Fatalf("second reconcile() error = %v", err)
	}
	if got := eng.Running().System.HostName; got != "edge-new" {
		t.Fatalf("running hostname = %q, want edge-new after unchanged revision", got)
	}
	if st.gets != 1 {
		t.Fatalf("GetLatestSnapshot() calls = %d, want 1", st.gets)
	}
}

func TestEtcdConfigSynchronizerKeepsRevisionPendingAfterApplyError(t *testing.T) {
	eng := engine.NewEngine([]engine.Plugin{rejectingSyncPlugin{}}, slog.Default())
	eng.InitializeRunning(testSyncConfig("edge-old"), 1)
	st := &configSyncTestStore{
		snap: model.NewSnapshot(testSyncConfig("edge-new"), 2, "alice", "remote commit"),
	}
	syncer := newEtcdConfigSynchronizer(st, eng, configSyncTestEtcdStatus{
		status: &datastore.EtcdStatus{Revision: 11, RunningRevision: 10},
	}, 0, slog.Default())

	if err := syncer.reconcile(context.Background()); err == nil {
		t.Fatal("reconcile() error = nil, want apply error")
	}
	status := syncer.ConfigSyncStatus()
	if status.Healthy || status.LastError == "" {
		t.Fatalf("ConfigSyncStatus() = %#v, want unhealthy error", status)
	}

	eng = engine.NewEngine(nil, slog.Default())
	eng.InitializeRunning(testSyncConfig("edge-old"), 1)
	syncer.engine = eng
	if err := syncer.reconcile(context.Background()); err != nil {
		t.Fatalf("retry reconcile() error = %v", err)
	}
	if got := eng.Running().System.HostName; got != "edge-new" {
		t.Fatalf("running hostname after retry = %q, want edge-new", got)
	}
}

type rejectingSyncPlugin struct{}

func (rejectingSyncPlugin) Name() string                          { return "rejecting-sync" }
func (rejectingSyncPlugin) Init(ctx context.Context) error        { return nil }
func (rejectingSyncPlugin) Close() error                          { return nil }
func (rejectingSyncPlugin) HealthCheck(ctx context.Context) error { return nil }
func (rejectingSyncPlugin) ValidateChanges(ctx context.Context, diff *engine.ConfigDiff) error {
	return fmt.Errorf("reject synced config")
}
func (rejectingSyncPlugin) ApplyChanges(ctx context.Context, diff *engine.ConfigDiff) error {
	return nil
}
func (rejectingSyncPlugin) RollbackChanges(ctx context.Context, diff *engine.ConfigDiff) error {
	return nil
}

func testSyncConfig(hostname string) *model.RouterConfig {
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{HostName: hostname}
	return cfg
}
