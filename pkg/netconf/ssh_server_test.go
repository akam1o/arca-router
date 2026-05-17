package netconf

import (
	"context"
	"net"
	"path/filepath"
	"slices"
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
	defer func() { _ = listener.Close() }()

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
	defer func() { _ = conn.Close() }()

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

func TestNewSSHServerDefaultsPartialConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := &SSHConfig{
		ListenAddr:    "127.0.0.1:0",
		HostKeyPath:   filepath.Join(dir, "ssh_host_ed25519_key"),
		UserDBPath:    filepath.Join(dir, "users.db"),
		DatastorePath: filepath.Join(dir, "config.db"),
	}

	server, err := NewSSHServer(cfg)
	if err != nil {
		t.Fatalf("NewSSHServer() error = %v", err)
	}
	t.Cleanup(func() {
		if err := server.Stop(); err != nil {
			t.Errorf("Stop() error = %v", err)
		}
	})

	defaults := DefaultSSHConfig()
	if server.config == cfg {
		t.Fatal("NewSSHServer reused caller config, want defensive copy")
	}
	if server.config.ListenAddr != cfg.ListenAddr {
		t.Fatalf("ListenAddr = %q, want %q", server.config.ListenAddr, cfg.ListenAddr)
	}
	if server.config.HostKeyPath != cfg.HostKeyPath {
		t.Fatalf("HostKeyPath = %q, want %q", server.config.HostKeyPath, cfg.HostKeyPath)
	}
	if server.config.UserDBPath != cfg.UserDBPath {
		t.Fatalf("UserDBPath = %q, want %q", server.config.UserDBPath, cfg.UserDBPath)
	}
	if server.config.DatastorePath != cfg.DatastorePath {
		t.Fatalf("DatastorePath = %q, want %q", server.config.DatastorePath, cfg.DatastorePath)
	}
	if server.config.IdleTimeout != defaults.IdleTimeout {
		t.Fatalf("IdleTimeout = %s, want %s", server.config.IdleTimeout, defaults.IdleTimeout)
	}
	if server.config.AbsoluteTimeout != defaults.AbsoluteTimeout {
		t.Fatalf("AbsoluteTimeout = %s, want %s", server.config.AbsoluteTimeout, defaults.AbsoluteTimeout)
	}
	if server.config.MaxSessions != defaults.MaxSessions {
		t.Fatalf("MaxSessions = %d, want %d", server.config.MaxSessions, defaults.MaxSessions)
	}
	if server.config.IPFailureLimit != defaults.IPFailureLimit {
		t.Fatalf("IPFailureLimit = %d, want %d", server.config.IPFailureLimit, defaults.IPFailureLimit)
	}
	if server.config.UserFailureLimit != defaults.UserFailureLimit {
		t.Fatalf("UserFailureLimit = %d, want %d", server.config.UserFailureLimit, defaults.UserFailureLimit)
	}
	if server.config.IPLockoutWindow != defaults.IPLockoutWindow {
		t.Fatalf("IPLockoutWindow = %s, want %s", server.config.IPLockoutWindow, defaults.IPLockoutWindow)
	}
	if server.config.UserLockoutWindow != defaults.UserLockoutWindow {
		t.Fatalf("UserLockoutWindow = %s, want %s", server.config.UserLockoutWindow, defaults.UserLockoutWindow)
	}
	if server.config.LockoutDuration != defaults.LockoutDuration {
		t.Fatalf("LockoutDuration = %s, want %s", server.config.LockoutDuration, defaults.LockoutDuration)
	}
	if !slices.Equal(server.config.SSHCiphers, defaults.SSHCiphers) {
		t.Fatalf("SSHCiphers = %v, want %v", server.config.SSHCiphers, defaults.SSHCiphers)
	}
	if !slices.Equal(server.config.SSHKeyExchanges, defaults.SSHKeyExchanges) {
		t.Fatalf("SSHKeyExchanges = %v, want %v", server.config.SSHKeyExchanges, defaults.SSHKeyExchanges)
	}
	if !slices.Equal(server.config.SSHMACs, defaults.SSHMACs) {
		t.Fatalf("SSHMACs = %v, want %v", server.config.SSHMACs, defaults.SSHMACs)
	}
	if !slices.Equal(server.sshConfig.Ciphers, defaults.SSHCiphers) {
		t.Fatalf("ssh ciphers = %v, want %v", server.sshConfig.Ciphers, defaults.SSHCiphers)
	}
	if !slices.Equal(server.sshConfig.KeyExchanges, defaults.SSHKeyExchanges) {
		t.Fatalf("ssh key exchanges = %v, want %v", server.sshConfig.KeyExchanges, defaults.SSHKeyExchanges)
	}
	if !slices.Equal(server.sshConfig.MACs, defaults.SSHMACs) {
		t.Fatalf("ssh MACs = %v, want %v", server.sshConfig.MACs, defaults.SSHMACs)
	}
	if cfg.IdleTimeout != 0 ||
		cfg.AbsoluteTimeout != 0 ||
		cfg.MaxSessions != 0 ||
		cfg.IPFailureLimit != 0 ||
		cfg.UserFailureLimit != 0 ||
		cfg.IPLockoutWindow != 0 ||
		cfg.UserLockoutWindow != 0 ||
		cfg.LockoutDuration != 0 ||
		len(cfg.SSHCiphers) != 0 ||
		len(cfg.SSHKeyExchanges) != 0 ||
		len(cfg.SSHMACs) != 0 {
		t.Fatalf("caller config was mutated: %+v", cfg)
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
