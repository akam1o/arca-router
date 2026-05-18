package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	"github.com/akam1o/arca-router/internal/store"
	"github.com/akam1o/arca-router/pkg/datastore"
	"github.com/akam1o/arca-router/pkg/logger"
	"github.com/akam1o/arca-router/pkg/netconf"
)

type initialConfigStore struct {
	snap         *model.ConfigSnapshot
	err          error
	prepareErr   error
	prepared     *initialPreparedCommit
	preparedSnap *model.ConfigSnapshot
}

func (s *initialConfigStore) GetLatestSnapshot(ctx context.Context) (*model.ConfigSnapshot, error) {
	return s.snap, s.err
}

func (s *initialConfigStore) PrepareCommit(ctx context.Context, snap *model.ConfigSnapshot) (store.PreparedCommit, error) {
	if s.prepareErr != nil {
		return nil, s.prepareErr
	}
	s.preparedSnap = snap.Clone()
	s.prepared = &initialPreparedCommit{}
	return s.prepared, nil
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

func (s *initialConfigStore) ListAuditEvents(ctx context.Context, opts *store.AuditOptions) ([]*store.AuditEvent, error) {
	return nil, nil
}

func (s *initialConfigStore) Close() error {
	return nil
}

type initialPreparedCommit struct {
	committed bool
	aborted   bool
	commitErr error
	abortErr  error
}

func (p *initialPreparedCommit) Commit(ctx context.Context) (string, error) {
	p.committed = true
	if p.commitErr != nil {
		return "", p.commitErr
	}
	return "initial-commit", nil
}

func (p *initialPreparedCommit) Abort(ctx context.Context) error {
	p.aborted = true
	return p.abortErr
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

	snap, source, err := loadInitialConfig(context.Background(), &daemonFlags{configPath: configPath}, &initialConfigStore{snap: stored}, testDaemonLogger())
	if err != nil {
		t.Fatalf("loadInitialConfig() error = %v", err)
	}
	if source != "datastore" {
		t.Fatalf("source = %q, want datastore", source)
	}
	if snap.Config.System.HostName != "stored-router" {
		t.Fatalf("hostname = %q, want stored-router", snap.Config.System.HostName)
	}
}

func TestLoadInitialConfigFallsBackToFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "arca-router.conf")
	if err := os.WriteFile(configPath, []byte("set system host-name file-router\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	snap, source, err := loadInitialConfig(context.Background(), &daemonFlags{configPath: configPath}, &initialConfigStore{}, testDaemonLogger())
	if err != nil {
		t.Fatalf("loadInitialConfig() error = %v", err)
	}
	if source != "file" {
		t.Fatalf("source = %q, want file", source)
	}
	if snap.Config.System.HostName != "file-router" {
		t.Fatalf("hostname = %q, want file-router", snap.Config.System.HostName)
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

func TestApplyInitialConfigPersistsFileStartupConfig(t *testing.T) {
	eng := engine.NewEngine(nil, slog.Default())
	st := &initialConfigStore{}
	snap := model.NewSnapshot(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "file-router"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 1, "system", "initial startup")

	if err := applyInitialConfig(context.Background(), eng, st, snap, "file"); err != nil {
		t.Fatalf("applyInitialConfig() error = %v", err)
	}
	if st.preparedSnap == nil || st.preparedSnap.Config.System.HostName != "file-router" {
		t.Fatalf("prepared snapshot = %#v, want file-router config", st.preparedSnap)
	}
	if st.prepared == nil || !st.prepared.committed {
		t.Fatal("initial config was not committed to datastore")
	}
	if got := eng.Running().System.HostName; got != "file-router" {
		t.Fatalf("engine hostname = %q, want file-router", got)
	}
}

func TestApplyInitialConfigPersistsEmptyStartupConfigAndCreatesSnapshot(t *testing.T) {
	eng := engine.NewEngine(nil, slog.Default())
	st := &initialConfigStore{}
	snap := model.NewSnapshot(model.NewRouterConfig(), 1, "system", "initial startup")

	if err := applyInitialConfig(context.Background(), eng, st, snap, "empty"); err != nil {
		t.Fatalf("applyInitialConfig() error = %v", err)
	}
	if st.prepared == nil || !st.prepared.committed {
		t.Fatal("empty initial config was not committed to datastore")
	}
	if running := eng.RunningSnapshot(); running == nil || running.Version != 1 {
		t.Fatalf("running snapshot = %#v, want version 1", running)
	}
}

func TestApplyInitialConfigDoesNotPersistDatastoreStartupConfig(t *testing.T) {
	eng := engine.NewEngine(nil, slog.Default())
	st := &initialConfigStore{}
	snap := model.NewSnapshot(model.NewRouterConfig(), 7, "system", "loaded from datastore")

	if err := applyInitialConfig(context.Background(), eng, st, snap, "datastore"); err != nil {
		t.Fatalf("applyInitialConfig() error = %v", err)
	}
	if st.prepared != nil {
		t.Fatal("datastore initial config was prepared for persistence again")
	}
	if running := eng.RunningSnapshot(); running == nil || running.Version != 7 {
		t.Fatalf("running snapshot = %#v, want datastore version 7", running)
	}
}

func TestBuildDatastoreConfigDefaultsToSQLite(t *testing.T) {
	cfg, err := buildDatastoreConfig(&daemonFlags{datastorePath: "/tmp/config.db"})
	if err != nil {
		t.Fatalf("buildDatastoreConfig() error = %v", err)
	}
	if cfg.Backend != datastore.BackendSQLite {
		t.Fatalf("Backend = %s, want sqlite", cfg.Backend)
	}
	if cfg.SQLitePath != "/tmp/config.db" {
		t.Fatalf("SQLitePath = %q, want /tmp/config.db", cfg.SQLitePath)
	}
}

func TestBuildDatastoreConfigEtcd(t *testing.T) {
	cfg, err := buildDatastoreConfig(&daemonFlags{
		datastoreMode: "etcd",
		etcdEndpoints: "http://127.0.0.1:2379, http://127.0.0.2:2379",
		etcdPrefix:    "/arca-test/",
		etcdTimeout:   3,
		etcdUsername:  "arca",
		etcdPassword:  "secret",
	})
	if err != nil {
		t.Fatalf("buildDatastoreConfig() error = %v", err)
	}
	if cfg.Backend != datastore.BackendEtcd {
		t.Fatalf("Backend = %s, want etcd", cfg.Backend)
	}
	if got := strings.Join(cfg.EtcdEndpoints, ","); got != "http://127.0.0.1:2379,http://127.0.0.2:2379" {
		t.Fatalf("EtcdEndpoints = %q", got)
	}
	if cfg.EtcdPrefix != "/arca-test/" {
		t.Fatalf("EtcdPrefix = %q, want /arca-test/", cfg.EtcdPrefix)
	}
	if cfg.EtcdUsername != "arca" || cfg.EtcdPassword != "secret" {
		t.Fatalf("etcd credentials not propagated")
	}
}

func TestBuildDatastoreConfigEtcdRequiresEndpoints(t *testing.T) {
	_, err := buildDatastoreConfig(&daemonFlags{datastoreMode: "etcd"})
	if err == nil {
		t.Fatal("buildDatastoreConfig() error = nil, want missing endpoint error")
	}
}

func TestBuildDatastoreConfigEtcdRejectsPartialTLS(t *testing.T) {
	_, err := buildDatastoreConfig(&daemonFlags{
		datastoreMode: "etcd",
		etcdEndpoints: "http://127.0.0.1:2379",
		etcdCertFile:  "/cert.pem",
	})
	if err == nil {
		t.Fatal("buildDatastoreConfig() error = nil, want partial TLS error")
	}
}

func TestBuildGRPCServerOptionsUnixRejectsTLSFlags(t *testing.T) {
	_, err := buildGRPCServerOptions(&daemonFlags{grpcTLSCert: "/cert.pem"})
	if err == nil {
		t.Fatal("buildGRPCServerOptions() error = nil, want TLS flags without listen error")
	}
	if !strings.Contains(err.Error(), "--grpc-listen") {
		t.Fatalf("buildGRPCServerOptions() error = %v, want --grpc-listen", err)
	}
}

func TestBuildGRPCServerOptionsTCPRequiresTLSKeyPair(t *testing.T) {
	_, err := buildGRPCServerOptions(&daemonFlags{grpcListen: "127.0.0.1:0"})
	if err == nil {
		t.Fatal("buildGRPCServerOptions() error = nil, want missing TLS key pair error")
	}
	if !strings.Contains(err.Error(), "--grpc-tls-cert") {
		t.Fatalf("buildGRPCServerOptions() error = %v, want TLS key pair error", err)
	}
}

func TestBuildGRPCServerOptionsTCPUsesTLSCredentials(t *testing.T) {
	certFile, keyFile, _ := writeTestCertificateFiles(t)
	opts, err := buildGRPCServerOptions(&daemonFlags{
		grpcListen:  "127.0.0.1:0",
		grpcTLSCert: certFile,
		grpcTLSKey:  keyFile,
	})
	if err != nil {
		t.Fatalf("buildGRPCServerOptions() error = %v", err)
	}
	if len(opts) != 1 {
		t.Fatalf("buildGRPCServerOptions() returned %d options, want 1", len(opts))
	}
}

func TestBuildGRPCServerTLSConfigEnablesMTLS(t *testing.T) {
	certFile, keyFile, caFile := writeTestCertificateFiles(t)
	cfg, err := buildGRPCServerTLSConfig(&daemonFlags{
		grpcTLSCert:  certFile,
		grpcTLSKey:   keyFile,
		grpcClientCA: caFile,
	})
	if err != nil {
		t.Fatalf("buildGRPCServerTLSConfig() error = %v", err)
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Fatalf("MinVersion = %x, want TLS 1.2", cfg.MinVersion)
	}
	if cfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("ClientAuth = %v, want RequireAndVerifyClientCert", cfg.ClientAuth)
	}
	if cfg.ClientCAs == nil {
		t.Fatal("ClientCAs = nil, want configured pool")
	}
}

func TestEffectiveNETCONFListenUsesFlagOverride(t *testing.T) {
	cfg := model.NewRouterConfig()
	cfg.Security = &model.SecurityConfig{
		NETCONF: &model.NETCONFSecurityConfig{
			SSH: &model.NETCONFSSHConfig{Port: 1830},
		},
	}

	got := effectiveNETCONFListen(":2830", model.NewSnapshot(cfg, 1, "test", "test"))
	if got != ":2830" {
		t.Fatalf("effectiveNETCONFListen() = %q, want %q", got, ":2830")
	}
}

func writeTestCertificateFiles(t *testing.T) (certFile, keyFile, caFile string) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("CreateCertificate() error = %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(privateKey)})

	dir := t.TempDir()
	certFile = filepath.Join(dir, "server.crt")
	keyFile = filepath.Join(dir, "server.key")
	caFile = filepath.Join(dir, "ca.crt")
	if err := os.WriteFile(certFile, certPEM, 0600); err != nil {
		t.Fatalf("WriteFile(cert) error = %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0600); err != nil {
		t.Fatalf("WriteFile(key) error = %v", err)
	}
	if err := os.WriteFile(caFile, certPEM, 0600); err != nil {
		t.Fatalf("WriteFile(ca) error = %v", err)
	}
	return certFile, keyFile, caFile
}

func TestEffectiveNETCONFListenUsesConfigPort(t *testing.T) {
	cfg := model.NewRouterConfig()
	cfg.Security = &model.SecurityConfig{
		NETCONF: &model.NETCONFSecurityConfig{
			SSH: &model.NETCONFSSHConfig{Port: 1830},
		},
	}

	got := effectiveNETCONFListen("", model.NewSnapshot(cfg, 1, "test", "test"))
	if got != ":1830" {
		t.Fatalf("effectiveNETCONFListen() = %q, want %q", got, ":1830")
	}
}

func TestEffectiveNETCONFListenUsesDefault(t *testing.T) {
	if got := effectiveNETCONFListen("", nil); got != ":830" {
		t.Fatalf("effectiveNETCONFListen() = %q, want :830", got)
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
