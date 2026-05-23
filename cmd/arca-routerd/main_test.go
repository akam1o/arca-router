package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"log/slog"
	"math/big"
	"net"
	"net/url"
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
	pkgvpp "github.com/akam1o/arca-router/pkg/vpp"
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

type lifecycleTestPlugin struct {
	name     string
	closeLog *[]string
}

func (p lifecycleTestPlugin) Name() string {
	return p.name
}

func (p lifecycleTestPlugin) Init(ctx context.Context) error {
	return nil
}

func (p lifecycleTestPlugin) ValidateChanges(ctx context.Context, diff *engine.ConfigDiff) error {
	return nil
}

func (p lifecycleTestPlugin) ApplyChanges(ctx context.Context, diff *engine.ConfigDiff) error {
	return nil
}

func (p lifecycleTestPlugin) RollbackChanges(ctx context.Context, diff *engine.ConfigDiff) error {
	return nil
}

func (p lifecycleTestPlugin) Close() error {
	*p.closeLog = append(*p.closeLog, p.name)
	return nil
}

func (p lifecycleTestPlugin) HealthCheck(ctx context.Context) error {
	return nil
}

func TestDaemonRuntimeCloseClosesPluginsInReverseInitOrder(t *testing.T) {
	var closeLog []string
	runtime := &daemonRuntime{
		plugins: []engine.Plugin{
			lifecycleTestPlugin{name: "first", closeLog: &closeLog},
			lifecycleTestPlugin{name: "second", closeLog: &closeLog},
		},
	}

	runtime.Close(testDaemonLogger())

	if got := strings.Join(closeLog, ","); got != "second,first" {
		t.Fatalf("close order = %q, want second,first", got)
	}
}

func TestDaemonManagementPlaneWaitReturnsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := (&daemonManagementPlane{}).Wait(ctx, testDaemonLogger()); err != nil {
		t.Fatalf("Wait() error = %v, want nil", err)
	}
}

func TestDaemonManagementPlaneWaitPropagatesEndpointError(t *testing.T) {
	metricsErr := make(chan error, 1)
	metricsErr <- errors.New("listen failed")

	err := (&daemonManagementPlane{metricsErr: metricsErr}).Wait(context.Background(), testDaemonLogger())
	if err == nil {
		t.Fatal("Wait() error = nil, want metrics endpoint error")
	}
	if !strings.Contains(err.Error(), "metrics endpoint stopped") {
		t.Fatalf("Wait() error = %v, want metrics endpoint stopped", err)
	}
}

func TestDaemonManagementPlaneStopStopsAuxiliaryEndpoints(t *testing.T) {
	var stopped []string
	recordStop := func(name string) func(context.Context) error {
		return func(context.Context) error {
			stopped = append(stopped, name)
			return nil
		}
	}

	plane := &daemonManagementPlane{
		metricsStop: recordStop("metrics"),
		webStop:     recordStop("web"),
		snmpStop:    recordStop("snmp"),
	}
	plane.Stop(testDaemonLogger())

	if got := strings.Join(stopped, ","); got != "metrics,web,snmp" {
		t.Fatalf("stopped endpoints = %q, want metrics,web,snmp", got)
	}
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

func TestBuildDatastoreConfigEtcdPasswordFile(t *testing.T) {
	passwordFile := filepath.Join(t.TempDir(), "etcd-password")
	if err := os.WriteFile(passwordFile, []byte("secret-from-file\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := buildDatastoreConfig(&daemonFlags{
		datastoreMode:    "etcd",
		etcdEndpoints:    "http://127.0.0.1:2379",
		etcdUsername:     "arca",
		etcdPasswordFile: passwordFile,
	})
	if err != nil {
		t.Fatalf("buildDatastoreConfig() error = %v", err)
	}
	if cfg.EtcdPassword != "secret-from-file" {
		t.Fatalf("EtcdPassword = %q, want secret-from-file", cfg.EtcdPassword)
	}
}

func TestBuildDatastoreConfigEtcdPasswordFileFromEnv(t *testing.T) {
	passwordFile := filepath.Join(t.TempDir(), "etcd-password")
	if err := os.WriteFile(passwordFile, []byte("secret-from-env\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv(etcdPasswordFileEnv, passwordFile)

	cfg, err := buildDatastoreConfig(&daemonFlags{
		datastoreMode: "etcd",
		etcdEndpoints: "http://127.0.0.1:2379",
		etcdUsername:  "arca",
	})
	if err != nil {
		t.Fatalf("buildDatastoreConfig() error = %v", err)
	}
	if cfg.EtcdPassword != "secret-from-env" {
		t.Fatalf("EtcdPassword = %q, want secret-from-env", cfg.EtcdPassword)
	}
}

func TestBuildDatastoreConfigEtcdPasswordFileRequiresSecureFile(t *testing.T) {
	t.Run("rejects insecure mode", func(t *testing.T) {
		passwordFile := filepath.Join(t.TempDir(), "etcd-password")
		if err := os.WriteFile(passwordFile, []byte("secret\n"), 0600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		if err := os.Chmod(passwordFile, 0644); err != nil {
			t.Fatalf("Chmod() error = %v", err)
		}

		_, err := buildDatastoreConfig(&daemonFlags{
			datastoreMode:    "etcd",
			etcdEndpoints:    "http://127.0.0.1:2379",
			etcdPasswordFile: passwordFile,
		})
		if err == nil {
			t.Fatal("buildDatastoreConfig() error = nil, want insecure mode rejection")
		}
		if !strings.Contains(err.Error(), "insecure permissions") {
			t.Fatalf("buildDatastoreConfig() error = %v, want insecure permissions", err)
		}
	})

	t.Run("rejects symlink", func(t *testing.T) {
		dir := t.TempDir()
		passwordFile := filepath.Join(dir, "etcd-password")
		linkFile := filepath.Join(dir, "etcd-password-link")
		if err := os.WriteFile(passwordFile, []byte("secret\n"), 0600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		if err := os.Symlink(passwordFile, linkFile); err != nil {
			t.Skipf("Symlink() not available: %v", err)
		}

		_, err := buildDatastoreConfig(&daemonFlags{
			datastoreMode:    "etcd",
			etcdEndpoints:    "http://127.0.0.1:2379",
			etcdPasswordFile: linkFile,
		})
		if err == nil {
			t.Fatal("buildDatastoreConfig() error = nil, want symlink rejection")
		}
		if !strings.Contains(err.Error(), "symbolic link") {
			t.Fatalf("buildDatastoreConfig() error = %v, want symbolic link rejection", err)
		}
	})

	t.Run("rejects hardlink", func(t *testing.T) {
		dir := t.TempDir()
		passwordFile := filepath.Join(dir, "etcd-password")
		linkFile := filepath.Join(dir, "etcd-password-hardlink")
		if err := os.WriteFile(passwordFile, []byte("secret\n"), 0600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}
		if err := os.Link(passwordFile, linkFile); err != nil {
			t.Skipf("Link() not available: %v", err)
		}

		_, err := buildDatastoreConfig(&daemonFlags{
			datastoreMode:    "etcd",
			etcdEndpoints:    "http://127.0.0.1:2379",
			etcdPasswordFile: linkFile,
		})
		if err == nil {
			t.Fatal("buildDatastoreConfig() error = nil, want hardlink rejection")
		}
		if !strings.Contains(err.Error(), "multiple hard links") {
			t.Fatalf("buildDatastoreConfig() error = %v, want hardlink rejection", err)
		}
	})
}

func TestBuildDatastoreConfigEtcdRejectsPasswordAndPasswordFile(t *testing.T) {
	passwordFile := filepath.Join(t.TempDir(), "etcd-password")
	if err := os.WriteFile(passwordFile, []byte("secret-from-file\n"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := buildDatastoreConfig(&daemonFlags{
		datastoreMode:    "etcd",
		etcdEndpoints:    "http://127.0.0.1:2379",
		etcdPassword:     "secret",
		etcdPasswordFile: passwordFile,
	})
	if err == nil {
		t.Fatal("buildDatastoreConfig() error = nil, want password source conflict")
	}
	if !strings.Contains(err.Error(), "--etcd-password") {
		t.Fatalf("buildDatastoreConfig() error = %v, want password source conflict", err)
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

func TestVPPClientOptionsFromFlags(t *testing.T) {
	opts := vppClientOptionsFromFlags(&daemonFlags{
		vppAPISocket:   "/tmp/daemon-vpp-api.sock",
		vppStatsSocket: "/tmp/daemon-vpp-stats.sock",
	})
	if opts.SocketPath != "/tmp/daemon-vpp-api.sock" {
		t.Fatalf("SocketPath = %q, want /tmp/daemon-vpp-api.sock", opts.SocketPath)
	}
	if opts.StatsSocketPath != "/tmp/daemon-vpp-stats.sock" {
		t.Fatalf("StatsSocketPath = %q, want /tmp/daemon-vpp-stats.sock", opts.StatsSocketPath)
	}
}

func TestVPPClientOptionsFromFlagsAllowsDefaults(t *testing.T) {
	opts := vppClientOptionsFromFlags(&daemonFlags{
		vppAPISocket:   pkgvpp.DefaultAPISocketPath,
		vppStatsSocket: pkgvpp.DefaultStatsSocketPath(),
	})
	if opts.SocketPath != pkgvpp.DefaultAPISocketPath {
		t.Fatalf("SocketPath = %q, want %q", opts.SocketPath, pkgvpp.DefaultAPISocketPath)
	}
	if opts.StatsSocketPath != pkgvpp.DefaultStatsSocketPath() {
		t.Fatalf("StatsSocketPath = %q, want %q", opts.StatsSocketPath, pkgvpp.DefaultStatsSocketPath())
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

func TestBuildGRPCServerOptionsUnixRejectsClientIdentityFlag(t *testing.T) {
	_, err := buildGRPCServerOptions(&daemonFlags{grpcClientID: "spiffe://arca-router/nms"})
	if err == nil {
		t.Fatal("buildGRPCServerOptions() error = nil, want client identity without listen error")
	}
	if !strings.Contains(err.Error(), "--grpc-listen") {
		t.Fatalf("buildGRPCServerOptions() error = %v, want --grpc-listen", err)
	}
}

func TestBuildGRPCServerOptionsUnixRejectsClientRoleFlag(t *testing.T) {
	_, err := buildGRPCServerOptions(&daemonFlags{grpcClientRole: "spiffe://arca-router/nms=read-only"})
	if err == nil {
		t.Fatal("buildGRPCServerOptions() error = nil, want client role without listen error")
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

func TestBuildGRPCServerOptionsTCPRequiresClientCA(t *testing.T) {
	certFile, keyFile, _ := writeTestCertificateFiles(t)
	_, err := buildGRPCServerOptions(&daemonFlags{
		grpcListen:  "127.0.0.1:0",
		grpcTLSCert: certFile,
		grpcTLSKey:  keyFile,
	})
	if err == nil {
		t.Fatal("buildGRPCServerOptions() error = nil, want missing client CA error")
	}
	if !strings.Contains(err.Error(), "--grpc-client-ca") {
		t.Fatalf("buildGRPCServerOptions() error = %v, want client CA error", err)
	}
}

func TestBuildGRPCServerOptionsTCPRequiresClientRole(t *testing.T) {
	certFile, keyFile, caFile := writeTestCertificateFiles(t)
	_, err := buildGRPCServerOptions(&daemonFlags{
		grpcListen:   "127.0.0.1:0",
		grpcTLSCert:  certFile,
		grpcTLSKey:   keyFile,
		grpcClientCA: caFile,
	})
	if err == nil {
		t.Fatal("buildGRPCServerOptions() error = nil, want missing client role error")
	}
	if !strings.Contains(err.Error(), "--grpc-client-role") {
		t.Fatalf("buildGRPCServerOptions() error = %v, want client role error", err)
	}
}

func TestBuildGRPCServerOptionsTCPUsesClientRoleInterceptors(t *testing.T) {
	certFile, keyFile, caFile := writeTestCertificateFiles(t)
	opts, err := buildGRPCServerOptions(&daemonFlags{
		grpcListen:     "127.0.0.1:0",
		grpcTLSCert:    certFile,
		grpcTLSKey:     keyFile,
		grpcClientCA:   caFile,
		grpcClientRole: "spiffe://arca-router/nms=read-only,router-operator=operator",
	})
	if err != nil {
		t.Fatalf("buildGRPCServerOptions() error = %v", err)
	}
	if len(opts) != 3 {
		t.Fatalf("buildGRPCServerOptions() returned %d options, want TLS credentials plus unary and stream interceptors", len(opts))
	}
}

func TestBuildGRPCServerOptionsTCPRejectsInvalidClientRole(t *testing.T) {
	certFile, keyFile, caFile := writeTestCertificateFiles(t)
	_, err := buildGRPCServerOptions(&daemonFlags{
		grpcListen:     "127.0.0.1:0",
		grpcTLSCert:    certFile,
		grpcTLSKey:     keyFile,
		grpcClientCA:   caFile,
		grpcClientRole: "router-operator=superuser",
	})
	if err == nil {
		t.Fatal("buildGRPCServerOptions() error = nil, want invalid client role error")
	}
	if !strings.Contains(err.Error(), "parse gRPC client roles") {
		t.Fatalf("buildGRPCServerOptions() error = %v, want client role parse error", err)
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

func TestBuildGRPCServerTLSConfigRestrictsClientIdentity(t *testing.T) {
	certFile, keyFile, caFile := writeTestCertificateFiles(t)
	cfg, err := buildGRPCServerTLSConfig(&daemonFlags{
		grpcTLSCert:  certFile,
		grpcTLSKey:   keyFile,
		grpcClientCA: caFile,
		grpcClientID: "spiffe://arca-router/nms,router-operator",
	})
	if err != nil {
		t.Fatalf("buildGRPCServerTLSConfig() error = %v", err)
	}
	if cfg.VerifyConnection == nil {
		t.Fatal("VerifyConnection = nil, want client identity verifier")
	}

	allowedURI := mustParseURL(t, "spiffe://arca-router/nms")
	if err := cfg.VerifyConnection(tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{
		{URIs: []*url.URL{allowedURI}},
	}}}); err != nil {
		t.Fatalf("VerifyConnection(allowed URI) error = %v", err)
	}
	if err := cfg.VerifyConnection(tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{
		{Subject: pkix.Name{CommonName: "router-operator"}},
	}}}); err != nil {
		t.Fatalf("VerifyConnection(allowed CN) error = %v", err)
	}
	if err := cfg.VerifyConnection(tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{
		{DNSNames: []string{"untrusted.example.net"}},
	}}}); err == nil {
		t.Fatal("VerifyConnection(untrusted DNS) error = nil, want authorization error")
	}
}

func TestBuildGRPCServerTLSConfigRejectsEmptyClientIdentity(t *testing.T) {
	certFile, keyFile, caFile := writeTestCertificateFiles(t)
	_, err := buildGRPCServerTLSConfig(&daemonFlags{
		grpcTLSCert:  certFile,
		grpcTLSKey:   keyFile,
		grpcClientCA: caFile,
		grpcClientID: "spiffe://arca-router/nms,,router-operator",
	})
	if err == nil {
		t.Fatal("buildGRPCServerTLSConfig() error = nil, want empty client identity error")
	}
	if !strings.Contains(err.Error(), "--grpc-client-identity") {
		t.Fatalf("buildGRPCServerTLSConfig() error = %v, want client identity validation error", err)
	}
}

func TestBuildGRPCServerTLSConfigRejectsInsecureKeyPermissions(t *testing.T) {
	certFile, keyFile, caFile := writeTestCertificateFiles(t)
	if err := os.Chmod(keyFile, 0644); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}

	_, err := buildGRPCServerTLSConfig(&daemonFlags{
		grpcTLSCert:  certFile,
		grpcTLSKey:   keyFile,
		grpcClientCA: caFile,
	})
	if err == nil {
		t.Fatal("buildGRPCServerTLSConfig() error = nil, want key permission error")
	}
	if !strings.Contains(err.Error(), "load gRPC server cert/key") || !strings.Contains(err.Error(), "insecure permissions") {
		t.Fatalf("buildGRPCServerTLSConfig() error = %v, want key permission validation error", err)
	}
}

func TestEffectiveNETCONFListenUsesFlagOverride(t *testing.T) {
	got := effectiveNETCONFListen(":2830", nil)
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

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", raw, err)
	}
	return parsed
}

func TestEffectiveNETCONFListenUsesConfigPort(t *testing.T) {
	cfg := model.NewRouterConfig()
	cfg.Security = &model.SecurityConfig{
		NETCONF: &model.NETCONFSecurityConfig{
			SSH: &model.NETCONFSSHConfig{
				Enabled: true,
				Port:    1830,
			},
		},
	}

	got := effectiveNETCONFListen("", model.NewSnapshot(cfg, 1, "test", "test"))
	if got != "127.0.0.1:1830" {
		t.Fatalf("effectiveNETCONFListen() = %q, want %q", got, "127.0.0.1:1830")
	}
}

func TestEffectiveNETCONFListenUsesConfigListenAddress(t *testing.T) {
	cfg := model.NewRouterConfig()
	cfg.Security = &model.SecurityConfig{
		NETCONF: &model.NETCONFSecurityConfig{
			SSH: &model.NETCONFSSHConfig{
				Enabled:       true,
				ListenAddress: "192.0.2.10",
				Port:          1830,
			},
		},
	}

	got := effectiveNETCONFListen("", model.NewSnapshot(cfg, 1, "test", "test"))
	if got != "192.0.2.10:1830" {
		t.Fatalf("effectiveNETCONFListen() = %q, want %q", got, "192.0.2.10:1830")
	}
}

func TestEffectiveNETCONFListenUsesEnabledDefault(t *testing.T) {
	cfg := model.NewRouterConfig()
	cfg.Security = &model.SecurityConfig{
		NETCONF: &model.NETCONFSecurityConfig{
			SSH: &model.NETCONFSSHConfig{Enabled: true},
		},
	}

	got := effectiveNETCONFListen("", model.NewSnapshot(cfg, 1, "test", "test"))
	if got != "127.0.0.1:830" {
		t.Fatalf("effectiveNETCONFListen() = %q, want %q", got, "127.0.0.1:830")
	}
}

func TestEffectiveNETCONFListenDisabledByDefault(t *testing.T) {
	if got := effectiveNETCONFListen("", nil); got != "" {
		t.Fatalf("effectiveNETCONFListen() = %q, want empty", got)
	}
}

func TestEffectiveNETCONFListenIgnoresPortWhenDisabled(t *testing.T) {
	cfg := model.NewRouterConfig()
	cfg.Security = &model.SecurityConfig{
		NETCONF: &model.NETCONFSecurityConfig{
			SSH: &model.NETCONFSSHConfig{Port: 1830},
		},
	}

	if got := effectiveNETCONFListen("", model.NewSnapshot(cfg, 1, "test", "test")); got != "" {
		t.Fatalf("effectiveNETCONFListen() = %q, want empty", got)
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

func TestPrepareGRPCSocketPathRejectsSymlinkDirectory(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, "target")
	socketDir := filepath.Join(root, "linked")
	if err := os.Mkdir(targetDir, secureGRPCSocketDirPerms); err != nil {
		t.Fatalf("Mkdir(target) error = %v", err)
	}
	if err := os.Symlink(targetDir, socketDir); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	err := prepareGRPCSocketPath(filepath.Join(socketDir, "routerd.sock"))
	if err == nil {
		t.Fatal("prepareGRPCSocketPath() error = nil, want symlink directory rejection")
	}
	if !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("prepareGRPCSocketPath() error = %v, want symbolic link rejection", err)
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

func TestRestrictGRPCSocketPermissionsRejectsSymlinkPath(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "target")
	socketPath := filepath.Join(dir, "routerd.sock")
	if err := os.WriteFile(targetPath, []byte("not a socket"), 0644); err != nil {
		t.Fatalf("WriteFile(target) error = %v", err)
	}
	if err := os.Chmod(targetPath, 0644); err != nil {
		t.Fatalf("Chmod(target) error = %v", err)
	}
	if err := os.Symlink(targetPath, socketPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	err := restrictGRPCSocketPermissions(socketPath)
	if err == nil {
		t.Fatal("restrictGRPCSocketPermissions() error = nil, want symlink rejection")
	}
	if !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("restrictGRPCSocketPermissions() error = %v, want symbolic link rejection", err)
	}
	info, statErr := os.Stat(targetPath)
	if statErr != nil {
		t.Fatalf("Stat(target) error = %v", statErr)
	}
	if got := info.Mode().Perm(); got != 0644 {
		t.Fatalf("target mode = %04o, want unchanged 0644", got)
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
