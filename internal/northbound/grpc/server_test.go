package grpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	"github.com/akam1o/arca-router/internal/store"
	pkgconfig "github.com/akam1o/arca-router/pkg/config"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func listenUnix(path string) (net.Listener, error) {
	return net.Listen("unix", path)
}

type fakeStore struct {
	commitID   string
	prepareErr error
	commitErr  error
	saved      *model.ConfigSnapshot
	aborted    bool
	commits    map[string]*store.CommitRecord
	listCalls  int
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
	return &fakePreparedCommit{store: f}, nil
}

func (f *fakeStore) GetCommit(ctx context.Context, commitID string) (*store.CommitRecord, error) {
	return f.commits[commitID], nil
}

func (f *fakeStore) ListCommits(ctx context.Context, opts *store.ListOptions) ([]*store.CommitRecord, error) {
	f.listCalls++
	return nil, nil
}

func (f *fakeStore) AuditLog(ctx context.Context, event *store.AuditEvent) error {
	return nil
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
	defer client.Close()

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
	diffText, hasChanges, err := client.Diff(ctx, sessionID)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if hasChanges {
		t.Fatalf("Diff() has changes after commit: %q", diffText)
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
