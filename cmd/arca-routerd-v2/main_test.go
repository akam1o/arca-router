package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	"github.com/akam1o/arca-router/internal/store"
	"github.com/akam1o/arca-router/pkg/logger"
	"github.com/akam1o/arca-router/pkg/netconf"
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

func TestLoadInitialConfigRejectsConfigOpenError(t *testing.T) {
	_, _, err := loadInitialConfig(context.Background(), &daemonFlags{configPath: "\x00"}, &initialConfigStore{}, testDaemonLogger())
	if err == nil {
		t.Fatal("loadInitialConfig() error = nil, want open error")
	}
	if !strings.Contains(err.Error(), "open config") {
		t.Fatalf("loadInitialConfig() error = %v, want open config error", err)
	}
}

func TestPrepareGRPCSocketPathRejectsInsecureDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "open")
	if err := os.Mkdir(dir, 0777); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}

	err := prepareGRPCSocketPath(filepath.Join(dir, "routerd.sock"))
	if err == nil {
		t.Fatal("prepareGRPCSocketPath() error = nil, want insecure directory error")
	}
}

func TestPrepareGRPCSocketPathRejectsNonSocketPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "routerd.sock")
	if err := os.WriteFile(path, []byte("not a socket"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := prepareGRPCSocketPath(path)
	if err == nil {
		t.Fatal("prepareGRPCSocketPath() error = nil, want non-socket error")
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("non-socket path was removed: %v", statErr)
	}
}

func TestRestrictGRPCSocketPermissions(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "arca-routerd-")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	path := filepath.Join(dir, "routerd.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
		_ = os.Remove(path)
	})

	if err := restrictGRPCSocketPermissions(path); err != nil {
		t.Fatalf("restrictGRPCSocketPermissions() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != secureGRPCSocketFilePerms {
		t.Fatalf("socket mode = %04o, want %04o", got, secureGRPCSocketFilePerms)
	}
}

func TestListenSecureGRPCSocketCreatesRestrictedSocket(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "arca-routerd-")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	path := filepath.Join(dir, "routerd.sock")
	listener, err := listenSecureGRPCSocket(path)
	if err != nil {
		t.Fatalf("listenSecureGRPCSocket() error = %v", err)
	}
	t.Cleanup(func() {
		_ = listener.Close()
		_ = os.Remove(path)
	})

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got := info.Mode().Perm(); got != secureGRPCSocketFilePerms {
		t.Fatalf("socket mode = %04o, want %04o", got, secureGRPCSocketFilePerms)
	}
}

func TestNETCONFCommitHookAppliesEngineBeforePersist(t *testing.T) {
	eng := engine.NewEngine(nil, slog.Default())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 1)

	hook := newNETCONFCommitHook(eng)
	persistCalled := false
	commitID, err := hook(context.Background(), &netconf.CommitHookRequest{
		User:       "alice",
		Message:    "NETCONF commit by alice",
		ConfigText: "set system host-name router2\n",
	}, func(ctx context.Context) (string, error) {
		persistCalled = true
		if got := eng.Running().System.HostName; got != "router2" {
			t.Fatalf("engine hostname before persist = %q, want router2", got)
		}
		return "commit-1", nil
	})
	if err != nil {
		t.Fatalf("commit hook error = %v", err)
	}
	if !persistCalled {
		t.Fatal("persist callback was not called")
	}
	if commitID != "commit-1" {
		t.Fatalf("commit ID = %q, want commit-1", commitID)
	}
}

func TestNETCONFCommitHookRejectsNoopCandidate(t *testing.T) {
	eng := engine.NewEngine(nil, slog.Default())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 1)

	hook := newNETCONFCommitHook(eng)
	persistCalled := false
	_, err := hook(context.Background(), &netconf.CommitHookRequest{
		User:       "alice",
		Message:    "NETCONF commit by alice",
		ConfigText: "set system host-name router1\n",
	}, func(ctx context.Context) (string, error) {
		persistCalled = true
		return "commit-1", nil
	})
	if err == nil || !strings.Contains(err.Error(), "no configuration changes to commit") {
		t.Fatalf("commit hook error = %v, want no changes", err)
	}
	if persistCalled {
		t.Fatal("persist callback was called for unchanged NETCONF candidate")
	}
	if snap := eng.RunningSnapshot(); snap == nil || snap.Version != 1 {
		t.Fatalf("running snapshot = %#v, want version 1", snap)
	}
}

func TestNETCONFCommitHookRollsBackEngineWhenPersistFails(t *testing.T) {
	eng := engine.NewEngine(nil, slog.Default())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 1)

	hook := newNETCONFCommitHook(eng)
	_, err := hook(context.Background(), &netconf.CommitHookRequest{
		User:       "alice",
		Message:    "NETCONF commit by alice",
		ConfigText: "set system host-name router2\n",
	}, func(ctx context.Context) (string, error) {
		return "", errors.New("persist failed")
	})
	if err == nil {
		t.Fatal("commit hook expected persistence error")
	}
	if got := eng.Running().System.HostName; got != "router1" {
		t.Fatalf("engine hostname after failed persist = %q, want router1", got)
	}
}
