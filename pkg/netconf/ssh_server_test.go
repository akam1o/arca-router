package netconf

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

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

func TestSSHServerStopClosesIdlePreAuthConnection(t *testing.T) {
	cfg, _ := testSSHServerConfig(t, "127.0.0.1:0")
	server, err := NewSSHServer(cfg)
	if err != nil {
		t.Fatalf("NewSSHServer() error = %v", err)
	}
	t.Cleanup(func() { _ = server.Stop() })

	if err := server.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	conn, err := net.Dial("tcp", testSSHServerListenAddr(t, server))
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	waitForCondition(t, time.Second, func() bool {
		return server.GetMetrics().ActiveConnections > 0
	})

	stopped := make(chan error, 1)
	go func() {
		stopped <- server.Stop()
	}()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Stop() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return with idle pre-auth connection")
	}
}

func TestSSHServerStartAfterStopRejected(t *testing.T) {
	cfg, _ := testSSHServerConfig(t, "127.0.0.1:0")
	server, err := NewSSHServer(cfg)
	if err != nil {
		t.Fatalf("NewSSHServer() error = %v", err)
	}
	if err := server.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if err := server.Start(context.Background()); err == nil {
		_ = server.Stop()
		t.Fatal("Start() error = nil after Stop, want rejection")
	}
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

func testSSHServerListenAddr(t *testing.T, server *SSHServer) string {
	t.Helper()

	server.mu.Lock()
	defer server.mu.Unlock()
	if server.listener == nil {
		t.Fatal("server listener is nil")
	}
	return server.listener.Addr().String()
}

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
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
