package netconf

import (
	"context"
	"net"
	"path/filepath"
	"testing"

	"github.com/akam1o/arca-router/pkg/datastore"
)

func TestSSHServerStopBeforeStartReleasesProcessLock(t *testing.T) {
	cfg, dbPath := testSSHServerConfig(t, "127.0.0.1:0")
	server, err := NewSSHServer(cfg)
	if err != nil {
		t.Fatalf("NewSSHServer() error = %v", err)
	}

	if err := server.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if err := server.Stop(); err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}
	assertCanAcquireSQLiteProcessLock(t, dbPath)
}

func TestSSHServerStopAfterStartFailureReleasesProcessLock(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()

	cfg, dbPath := testSSHServerConfig(t, listener.Addr().String())
	server, err := NewSSHServer(cfg)
	if err != nil {
		t.Fatalf("NewSSHServer() error = %v", err)
	}

	if err := server.Start(context.Background()); err == nil {
		_ = server.Stop()
		t.Fatal("Start() error = nil, want listen failure")
	}
	if err := server.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	assertCanAcquireSQLiteProcessLock(t, dbPath)
}

func testSSHServerConfig(t *testing.T, listenAddr string) (*SSHConfig, string) {
	t.Helper()

	dir := t.TempDir()
	cfg := DefaultSSHConfig()
	cfg.ListenAddr = listenAddr
	cfg.HostKeyPath = filepath.Join(dir, "ssh_host_ed25519_key")
	cfg.UserDBPath = filepath.Join(dir, "users.db")
	cfg.DatastorePath = filepath.Join(dir, "config.db")

	return cfg, cfg.DatastorePath
}

func assertCanAcquireSQLiteProcessLock(t *testing.T, dbPath string) {
	t.Helper()

	lock, err := datastore.AcquireSQLiteProcessLock(dbPath)
	if err != nil {
		t.Fatalf("AcquireSQLiteProcessLock() error = %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatalf("ProcessLock Close() error = %v", err)
	}
}
