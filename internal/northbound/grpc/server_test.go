package grpc

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
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
	commitID  string
	commitErr error
	saved     *model.ConfigSnapshot
	aborted   bool
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
	f.saved = snap
	return &fakePreparedCommit{store: f}, nil
}

func (f *fakeStore) GetCommit(ctx context.Context, commitID string) (*store.CommitRecord, error) {
	return nil, nil
}

func (f *fakeStore) ListCommits(ctx context.Context, opts *store.ListOptions) ([]*store.CommitRecord, error) {
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
