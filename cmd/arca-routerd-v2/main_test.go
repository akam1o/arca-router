package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/akam1o/arca-router/internal/model"
	"github.com/akam1o/arca-router/internal/store"
	"github.com/akam1o/arca-router/pkg/logger"
)

type initialConfigStore struct {
	snap *model.ConfigSnapshot
	err  error
}

func (s *initialConfigStore) GetLatestSnapshot(ctx context.Context) (*model.ConfigSnapshot, error) {
	return s.snap, s.err
}

func (s *initialConfigStore) PrepareCommit(ctx context.Context, snap *model.ConfigSnapshot) (store.PreparedCommit, error) {
	return nil, nil
}

func (s *initialConfigStore) SaveCommit(ctx context.Context, snap *model.ConfigSnapshot) (string, error) {
	return "", nil
}

func (s *initialConfigStore) GetCommit(ctx context.Context, commitID string) (*store.CommitRecord, error) {
	return nil, nil
}

func (s *initialConfigStore) ListCommits(ctx context.Context, opts *store.ListOptions) ([]*store.CommitRecord, error) {
	return nil, nil
}

func (s *initialConfigStore) AuditLog(ctx context.Context, event *store.AuditEvent) error {
	return nil
}

func (s *initialConfigStore) Close() error {
	return nil
}

func testDaemonLogger() *logger.Logger {
	return logger.New("test", &logger.Config{Level: slog.LevelError})
}

func TestLoadInitialConfigPrefersDatastore(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "arca-router.conf")
	if err := os.WriteFile(configPath, []byte("set system host-name file-router\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	stored := model.NewSnapshot(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "stored-router"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 7, "alice", "stored")

	cfg, source, err := loadInitialConfig(context.Background(), &daemonFlags{configPath: configPath}, &initialConfigStore{snap: stored}, testDaemonLogger())
	if err != nil {
		t.Fatalf("loadInitialConfig() error = %v", err)
	}
	if source != "datastore" {
		t.Fatalf("source = %q, want datastore", source)
	}
	if cfg.System.HostName != "stored-router" {
		t.Fatalf("hostname = %q, want stored-router", cfg.System.HostName)
	}
}

func TestLoadInitialConfigFallsBackToFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "arca-router.conf")
	if err := os.WriteFile(configPath, []byte("set system host-name file-router\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, source, err := loadInitialConfig(context.Background(), &daemonFlags{configPath: configPath}, &initialConfigStore{}, testDaemonLogger())
	if err != nil {
		t.Fatalf("loadInitialConfig() error = %v", err)
	}
	if source != "file" {
		t.Fatalf("source = %q, want file", source)
	}
	if cfg.System.HostName != "file-router" {
		t.Fatalf("hostname = %q, want file-router", cfg.System.HostName)
	}
}
