// arca-routerd is the unified daemon for arca-router.
// It combines the router engine, NETCONF server, and gRPC API
// into a single process with shared state.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	nbgrpc "github.com/akam1o/arca-router/internal/northbound/grpc"
	sbfrr "github.com/akam1o/arca-router/internal/southbound/frr"
	sbvpp "github.com/akam1o/arca-router/internal/southbound/vpp"
	internalstore "github.com/akam1o/arca-router/internal/store"
	storesqlite "github.com/akam1o/arca-router/internal/store/sqlite"
	"github.com/akam1o/arca-router/pkg/auth"
	"github.com/akam1o/arca-router/pkg/config"
	"github.com/akam1o/arca-router/pkg/datastore"
	"github.com/akam1o/arca-router/pkg/device"
	pkgfrr "github.com/akam1o/arca-router/pkg/frr"
	"github.com/akam1o/arca-router/pkg/logger"
	"github.com/akam1o/arca-router/pkg/netconf"
	"github.com/akam1o/arca-router/pkg/security"
	pkgvpp "github.com/akam1o/arca-router/pkg/vpp"
	googlegrpc "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

const defaultNETCONFPort = 830

const etcdPasswordFileEnv = "ARCA_ROUTER_ETCD_PASSWORD_FILE"

const (
	secureGRPCSocketDirPerms  os.FileMode = 0750
	secureGRPCSocketFilePerms os.FileMode = 0660
	secureGRPCSocketUmask                 = 0077
)

var grpcSocketUmaskMu sync.Mutex

type daemonFlags struct {
	configPath       string
	hardwarePath     string
	datastorePath    string
	datastoreMode    string
	etcdEndpoints    string
	etcdPrefix       string
	etcdTimeout      time.Duration
	etcdUsername     string
	etcdPassword     string
	etcdPasswordFile string
	etcdCertFile     string
	etcdKeyFile      string
	etcdCAFile       string
	logLevel         string
	version          bool
	mockVPP          bool
	vppAPISocket     string
	vppStatsSocket   string

	// NETCONF settings.
	netconfListen   string
	netconfXPath    bool
	hostKeyPath     string
	userDBPath      string
	grpcSocket      string
	grpcListen      string
	grpcTLSCert     string
	grpcTLSKey      string
	grpcClientCA    string
	grpcClientID    string
	grpcClientRole  string
	metricsListen   string
	webListen       string
	webAPITokenFile string
	snmpListen      string
	snmpCommunity   string
	frrApplyMode    string
}

func main() {
	f := parseFlags()

	if f.version {
		fmt.Printf("arca-routerd version %s\n", Version)
		fmt.Printf("  Commit: %s\n", Commit)
		fmt.Printf("  Built:  %s\n", BuildDate)
		os.Exit(0)
	}

	logLevel := parseLogLevel(f.logLevel)
	log := logger.New("arca-routerd", &logger.Config{
		Level:     logLevel,
		AddSource: true,
	})

	log.Info("Starting unified arca-routerd",
		slog.String("version", Version),
		slog.String("commit", Commit),
		slog.String("build_date", BuildDate),
	)

	ctx, cancel := signal.NotifyContext(context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer cancel()

	if err := run(ctx, f, log); err != nil {
		log.Error("Daemon failed", slog.Any("error", err))
		os.Exit(1)
	}

	log.Info("arca-routerd stopped gracefully")
}

func parseFlags() *daemonFlags {
	f := &daemonFlags{}

	flag.StringVar(&f.configPath, "config", "/etc/arca-router/arca-router.conf",
		"Path to configuration file")
	flag.StringVar(&f.hardwarePath, "hardware", "/etc/arca-router/hardware.yaml",
		"Path to hardware configuration file")
	flag.StringVar(&f.datastorePath, "datastore", "/var/lib/arca-router/config.db",
		"Path to configuration datastore (SQLite)")
	flag.StringVar(&f.datastoreMode, "datastore-backend", string(datastore.BackendSQLite),
		"Configuration datastore backend: sqlite or etcd")
	flag.StringVar(&f.etcdEndpoints, "etcd-endpoints", "",
		"Comma-separated etcd endpoints for --datastore-backend=etcd")
	flag.StringVar(&f.etcdPrefix, "etcd-prefix", "/arca-router/",
		"Key prefix for the etcd datastore")
	flag.DurationVar(&f.etcdTimeout, "etcd-timeout", 5*time.Second,
		"etcd connection and operation timeout")
	flag.StringVar(&f.etcdUsername, "etcd-username", "",
		"etcd username")
	flag.StringVar(&f.etcdPassword, "etcd-password", "",
		"etcd password (discouraged; use --etcd-password-file)")
	flag.StringVar(&f.etcdPasswordFile, "etcd-password-file", "",
		"Path to file containing etcd password (or ARCA_ROUTER_ETCD_PASSWORD_FILE)")
	flag.StringVar(&f.etcdCertFile, "etcd-cert", "",
		"etcd TLS client certificate path")
	flag.StringVar(&f.etcdKeyFile, "etcd-key", "",
		"etcd TLS client key path")
	flag.StringVar(&f.etcdCAFile, "etcd-ca", "",
		"etcd TLS CA certificate path")
	flag.StringVar(&f.logLevel, "log-level", "info",
		"Log level (debug, info, warn, error)")
	flag.BoolVar(&f.version, "version", false,
		"Print version information and exit")
	flag.BoolVar(&f.mockVPP, "mock-vpp", false,
		"Use mock VPP client for testing")
	flag.StringVar(&f.vppAPISocket, "vpp-api-socket", pkgvpp.DefaultAPISocketPath,
		"Path to VPP binary API socket")
	flag.StringVar(&f.vppStatsSocket, "vpp-stats-socket", pkgvpp.DefaultStatsSocketPath(),
		"Path to VPP stats socket")

	// NETCONF flags
	flag.StringVar(&f.netconfListen, "netconf-listen", "",
		"NETCONF/SSH listen address (overrides security netconf ssh listen-address/port and enables NETCONF)")
	flag.BoolVar(&f.netconfXPath, "netconf-standard-xpath", true,
		"Advertise the standard NETCONF :xpath capability (enabled by default; set false to suppress)")
	flag.StringVar(&f.hostKeyPath, "host-key", "/var/lib/arca-router/ssh_host_ed25519_key",
		"Path to SSH host key")
	flag.StringVar(&f.userDBPath, "user-db", "/var/lib/arca-router/users.db",
		"Path to user database")
	flag.StringVar(&f.grpcSocket, "grpc-socket", "/run/arca-router/routerd.sock",
		"Path to internal gRPC Unix socket")
	flag.StringVar(&f.grpcListen, "grpc-listen", "",
		"TCP listen address for the gRPC API (requires --grpc-tls-cert and --grpc-tls-key; Unix socket is used when empty)")
	flag.StringVar(&f.grpcTLSCert, "grpc-tls-cert", "",
		"gRPC server TLS certificate path for --grpc-listen")
	flag.StringVar(&f.grpcTLSKey, "grpc-tls-key", "",
		"gRPC server TLS private key path for --grpc-listen")
	flag.StringVar(&f.grpcClientCA, "grpc-client-ca", "",
		"CA certificate path for verifying gRPC client certificates (enables mTLS)")
	flag.StringVar(&f.grpcClientID, "grpc-client-identity", "",
		"Comma-separated allowed gRPC client certificate identities (URI, CN, DNS, or email)")
	flag.StringVar(&f.grpcClientRole, "grpc-client-role", "",
		"Comma-separated gRPC client certificate identity=role mappings for method-level RBAC (required with --grpc-listen)")
	flag.StringVar(&f.metricsListen, "metrics-listen", "",
		"Prometheus metrics listen address (overrides system services prometheus config; disabled when empty and config disabled)")
	flag.StringVar(&f.webListen, "web-listen", "",
		"Web UI listen address (overrides system services web-ui config; disabled when empty and config disabled)")
	flag.StringVar(&f.webAPITokenFile, "web-api-token-file", "",
		"Path to web API token file (lines: name:role:token or name:role:sha256:<hex>[:not-after=<RFC3339>])")
	flag.StringVar(&f.snmpListen, "snmp-listen", "",
		"SNMPv2c UDP listen address (disabled when empty)")
	flag.StringVar(&f.snmpCommunity, "snmp-community", "",
		"SNMPv2c read-only community (overrides system services snmp config; required when SNMP is enabled)")
	flag.StringVar(&f.frrApplyMode, "frr-apply-mode", string(pkgfrr.BackendModeTransactional),
		"FRR apply backend: transactional or file")

	flag.Parse()
	return f
}

func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func openConfigStore(f *daemonFlags) (*storesqlite.Store, *datastore.ProcessLock, *datastore.Config, error) {
	cfg, err := buildDatastoreConfig(f)
	if err != nil {
		return nil, nil, nil, err
	}

	var processLock *datastore.ProcessLock
	if cfg.Backend == datastore.BackendSQLite {
		processLock, err = datastore.AcquireSQLiteProcessLock(cfg.SQLitePath)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("acquire datastore process lock: %w", err)
		}
	}

	ds, err := datastore.NewDatastore(cfg)
	if err != nil {
		if processLock != nil {
			_ = processLock.Close()
		}
		return nil, nil, nil, err
	}
	return storesqlite.New(ds, storesqlite.WithLegacyTextParser(parseLegacyRouterConfigText)), processLock, cfg, nil
}

func buildDatastoreConfig(f *daemonFlags) (*datastore.Config, error) {
	if f == nil {
		return nil, fmt.Errorf("daemon flags are nil")
	}
	backend := datastore.BackendType(strings.ToLower(strings.TrimSpace(f.datastoreMode)))
	if backend == "" {
		backend = datastore.BackendSQLite
	}

	switch backend {
	case datastore.BackendSQLite:
		path := strings.TrimSpace(f.datastorePath)
		if path == "" {
			path = "/var/lib/arca-router/config.db"
		}
		return &datastore.Config{
			Backend:    datastore.BackendSQLite,
			SQLitePath: path,
		}, nil
	case datastore.BackendEtcd:
		endpoints := parseCommaList(f.etcdEndpoints)
		if len(endpoints) == 0 {
			return nil, fmt.Errorf("etcd datastore requires --etcd-endpoints")
		}
		etcdPassword, err := resolveEtcdPassword(f)
		if err != nil {
			return nil, err
		}
		tlsConfig, err := buildEtcdTLSConfig(f)
		if err != nil {
			return nil, err
		}
		return &datastore.Config{
			Backend:       datastore.BackendEtcd,
			EtcdEndpoints: endpoints,
			EtcdPrefix:    f.etcdPrefix,
			EtcdTimeout:   f.etcdTimeout,
			EtcdUsername:  f.etcdUsername,
			EtcdPassword:  etcdPassword,
			EtcdTLS:       tlsConfig,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported datastore backend: %s", backend)
	}
}

func resolveEtcdPassword(f *daemonFlags) (string, error) {
	filePath := strings.TrimSpace(f.etcdPasswordFile)
	if filePath == "" {
		filePath = strings.TrimSpace(os.Getenv(etcdPasswordFileEnv))
	}
	if filePath == "" {
		return f.etcdPassword, nil
	}
	if f.etcdPassword != "" {
		return "", fmt.Errorf("--etcd-password and --etcd-password-file are mutually exclusive")
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read etcd password file: %w", err)
	}
	return strings.TrimRight(string(data), "\r\n"), nil
}

func parseCommaList(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func buildEtcdTLSConfig(f *daemonFlags) (*datastore.TLSConfig, error) {
	hasTLS := f.etcdCertFile != "" || f.etcdKeyFile != "" || f.etcdCAFile != ""
	if !hasTLS {
		return nil, nil
	}
	if f.etcdCertFile == "" || f.etcdKeyFile == "" || f.etcdCAFile == "" {
		return nil, fmt.Errorf("etcd TLS requires --etcd-cert, --etcd-key, and --etcd-ca")
	}
	return &datastore.TLSConfig{
		CertFile: f.etcdCertFile,
		KeyFile:  f.etcdKeyFile,
		CAFile:   f.etcdCAFile,
	}, nil
}

func vppClientOptionsFromFlags(f *daemonFlags) pkgvpp.GovppClientOptions {
	return pkgvpp.GovppClientOptions{
		SocketPath:      f.vppAPISocket,
		StatsSocketPath: f.vppStatsSocket,
	}
}

func run(ctx context.Context, f *daemonFlags, log *logger.Logger) error {
	installParserHooks()
	logDaemonConfiguration(f, log)

	runtime, err := newDaemonRuntime(ctx, f, log)
	if err != nil {
		return err
	}
	defer runtime.Close(log)

	managementPlane, err := startDaemonManagementPlane(ctx, f, runtime, log)
	if err != nil {
		return err
	}
	defer managementPlane.Stop(log)

	return managementPlane.Wait(ctx, log)
}

func logDaemonConfiguration(f *daemonFlags, log *logger.Logger) {
	log.Info("Configuration",
		slog.String("config_path", f.configPath),
		slog.String("hardware_path", f.hardwarePath),
		slog.String("datastore_backend", f.datastoreMode),
		slog.String("datastore_path", f.datastorePath),
		slog.String("etcd_endpoints", f.etcdEndpoints),
		slog.String("vpp_api_socket", f.vppAPISocket),
		slog.String("vpp_stats_socket", f.vppStatsSocket),
		slog.String("netconf_listen", f.netconfListen),
		slog.String("grpc_socket", f.grpcSocket),
		slog.String("metrics_listen", f.metricsListen),
		slog.String("web_listen", f.webListen),
		slog.String("snmp_listen", f.snmpListen),
		slog.String("frr_apply_mode", f.frrApplyMode),
	)
}

type daemonRuntime struct {
	configStore     *storesqlite.Store
	processLock     *datastore.ProcessLock
	datastoreConfig *datastore.Config
	engine          *engine.Engine
	plugins         []engine.Plugin
	vppPlugin       *sbvpp.VPPPlugin
	frrPlugin       *sbfrr.FRRPlugin
	configSync      configSyncRuntimeSource
}

func newDaemonRuntime(ctx context.Context, f *daemonFlags, log *logger.Logger) (_ *daemonRuntime, err error) {
	log.Info("Loading hardware configuration")
	hwConfig, err := device.LoadHardware(f.hardwarePath, log)
	if err != nil {
		return nil, fmt.Errorf("load hardware config: %w", err)
	}
	log.Info("Hardware loaded", slog.Int("interfaces", len(hwConfig.Interfaces)))

	var vppClient pkgvpp.Client
	if f.mockVPP {
		vppClient = pkgvpp.NewMockClient()
	} else {
		vppClient = pkgvpp.NewGovppClientWithOptions(vppClientOptionsFromFlags(f))
	}

	frrApplyMode, err := pkgfrr.ParseBackendMode(f.frrApplyMode)
	if err != nil {
		return nil, err
	}

	configStore, processLock, datastoreConfig, err := openConfigStore(f)
	if err != nil {
		return nil, fmt.Errorf("open config store: %w", err)
	}
	runtime := &daemonRuntime{
		configStore:     configStore,
		processLock:     processLock,
		datastoreConfig: datastoreConfig,
	}
	defer func() {
		if err != nil {
			runtime.Close(log)
		}
	}()
	if err := configStore.CleanupEphemeralState(ctx); err != nil {
		return nil, fmt.Errorf("cleanup config store ephemeral state: %w", err)
	}

	clusterPlugin := newClusterSyncPlugin(datastoreConfig)
	vppPlugin := sbvpp.NewVPPPlugin(vppClient, hwConfig, slog.Default())
	frrPlugin := sbfrr.NewFRRPluginWithApplyMode(slog.Default(), frrApplyMode)

	plugins := []engine.Plugin{clusterPlugin, vppPlugin, frrPlugin}
	runtime.vppPlugin = vppPlugin
	runtime.frrPlugin = frrPlugin

	eng := engine.NewEngine(plugins, slog.Default())
	runtime.engine = eng

	log.Info("Initializing engine plugins")
	for _, p := range plugins {
		if err := p.Init(ctx); err != nil {
			return nil, fmt.Errorf("init plugin %s: %w", p.Name(), err)
		}
		runtime.plugins = append(runtime.plugins, p)
	}

	log.Info("Loading initial configuration")
	initialSnap, initialSource, err := loadInitialConfig(ctx, f, configStore, log)
	if err != nil {
		return nil, fmt.Errorf("load initial config: %w", err)
	}

	// Apply initial configuration through the engine and keep the legacy
	// datastore in sync for NETCONF running/candidate operations.
	if err := applyInitialConfig(ctx, eng, configStore, initialSnap, initialSource); err != nil {
		return nil, fmt.Errorf("apply initial config: %w", err)
	}
	log.Info("Initial configuration applied", slog.String("source", initialSource))

	if datastoreConfig.Backend == datastore.BackendEtcd {
		etcdStatus, ok := configStore.Legacy().(datastore.EtcdStatusProvider)
		if !ok {
			log.Warn("etcd datastore does not expose config synchronization status")
		} else {
			syncer := newEtcdConfigSynchronizer(configStore, eng, etcdStatus, defaultEtcdConfigSyncInterval, log.Logger)
			syncer.Start(ctx)
			runtime.configSync = syncer
		}
	}

	return runtime, nil
}

func (r *daemonRuntime) Close(log *logger.Logger) {
	if r == nil {
		return
	}
	for i := len(r.plugins) - 1; i >= 0; i-- {
		p := r.plugins[i]
		if closeErr := p.Close(); closeErr != nil {
			log.Error("Failed to close plugin", slog.String("plugin", p.Name()), slog.Any("error", closeErr))
		}
	}
	if r.configStore != nil {
		if closeErr := r.configStore.Close(); closeErr != nil {
			log.Error("Failed to close config store", slog.Any("error", closeErr))
		}
	}
	if r.processLock != nil {
		if closeErr := r.processLock.Close(); closeErr != nil {
			log.Error("Failed to release datastore process lock", slog.Any("error", closeErr))
		}
	}
}

type daemonManagementPlane struct {
	grpcServer    *nbgrpc.Server
	grpcListener  net.Listener
	netconfServer *netconf.SSHServer
	grpcErr       <-chan error
	metricsErr    <-chan error
	webErr        <-chan error
	snmpErr       <-chan error
}

func startDaemonManagementPlane(ctx context.Context, f *daemonFlags, runtime *daemonRuntime, log *logger.Logger) (_ *daemonManagementPlane, err error) {
	plane := &daemonManagementPlane{}
	defer func() {
		if err != nil {
			plane.Stop(log)
		}
	}()

	netconfListen := effectiveNETCONFListen(f.netconfListen, runtime.engine.RunningSnapshot())
	if f.hostKeyPath != "" && netconfListen != "" {
		plane.netconfServer, err = startNETCONFServer(
			ctx,
			f,
			runtime.datastoreConfig,
			runtime.engine,
			newNETCONFOperationalStateProvider(runtime.vppPlugin, runtime.frrPlugin),
			log,
			netconfListen,
		)
		if err != nil {
			return nil, err
		}
	}

	lis, grpcServerOptions, grpcTransport, err := listenGRPCAPI(f)
	if err != nil {
		return nil, err
	}
	plane.grpcListener = lis
	log.Info("Starting gRPC API",
		slog.String("transport", grpcTransport),
		slog.String("address", lis.Addr().String()),
	)

	grpcServer := nbgrpc.NewServer(runtime.engine, runtime.configStore, slog.Default())
	grpcServer.SetInterfaceStateCollector(runtime.vppPlugin)
	grpcServer.SetLCPReconciliationSource(newGRPCLCPReconciliationSource(runtime.vppPlugin))
	grpcServer.SetBFDOperationalSource(runtime.frrPlugin)
	grpcServer.SetQoSCapabilitySource(runtime.vppPlugin)
	plane.grpcServer = grpcServer

	webAPITokens, err := loadWebAPITokens(f.webAPITokenFile)
	if err != nil {
		return nil, fmt.Errorf("load web API tokens: %w", err)
	}

	observabilitySource := metricsSource{
		startedAt:        time.Now(),
		engine:           runtime.engine,
		netconfServer:    plane.netconfServer,
		datastore:        runtime.datastoreConfig,
		configAPI:        grpcServer,
		telemetryAPI:     grpcServer,
		webAPITokens:     webAPITokens,
		webAPITokenFile:  strings.TrimSpace(f.webAPITokenFile),
		webAPITokenCache: newWebAPITokenCache(f.webAPITokenFile, webAPITokens),
		configSync:       runtime.configSync,
		frr:              runtime.frrPlugin,
		vpp:              runtime.vppPlugin,
	}
	grpcServer.SetHAStatusSource(newGRPCHAStatusSource(observabilitySource))

	grpcErr := make(chan error, 1)
	plane.grpcErr = grpcErr
	go func() {
		grpcErr <- grpcServer.ServeWithOptions(lis, grpcServerOptions...)
	}()

	if metricsListen := effectiveMetricsListen(f.metricsListen, runtime.engine.RunningSnapshot()); metricsListen != "" {
		plane.metricsErr, err = startMetricsServer(ctx, metricsListen, observabilitySource, log)
		if err != nil {
			return nil, err
		}
	}

	if webListen := effectiveWebListen(f.webListen, runtime.engine.RunningSnapshot()); webListen != "" {
		plane.webErr, err = startWebServer(ctx, webListen, observabilitySource, log)
		if err != nil {
			return nil, err
		}
	}

	if snmpListen := effectiveSNMPListen(f.snmpListen, runtime.engine.RunningSnapshot()); snmpListen != "" {
		plane.snmpErr, err = startSNMPServer(ctx, snmpListen, effectiveSNMPCommunity(f.snmpCommunity, runtime.engine.RunningSnapshot()), observabilitySource, log)
		if err != nil {
			return nil, err
		}
	}

	return plane, nil
}

func (p *daemonManagementPlane) Stop(log *logger.Logger) {
	if p == nil {
		return
	}
	if p.grpcServer != nil {
		p.grpcServer.Stop()
	}
	if p.grpcListener != nil {
		_ = p.grpcListener.Close()
	}
	if p.netconfServer != nil {
		if err := p.netconfServer.Stop(); err != nil {
			log.Error("Failed to stop NETCONF server", slog.Any("error", err))
		}
	}
}

func (p *daemonManagementPlane) Wait(ctx context.Context, log *logger.Logger) error {
	log.Info("Daemon running, waiting for shutdown signal")
	select {
	case <-ctx.Done():
		log.Info("Shutdown signal received, stopping")
	case err := <-p.grpcErr:
		return fmt.Errorf("gRPC API stopped: %w", err)
	case err := <-p.metricsErr:
		if err != nil {
			return fmt.Errorf("metrics endpoint stopped: %w", err)
		}
	case err := <-p.webErr:
		if err != nil {
			return fmt.Errorf("web endpoint stopped: %w", err)
		}
	case err := <-p.snmpErr:
		if err != nil {
			return fmt.Errorf("SNMP endpoint stopped: %w", err)
		}
	}

	return nil
}

func effectiveNETCONFListen(flagValue string, snapshot *model.ConfigSnapshot) string {
	if listen := strings.TrimSpace(flagValue); listen != "" {
		return listen
	}
	ssh := snapshotNETCONFSSHConfig(snapshot)
	if ssh == nil || !ssh.Enabled {
		return ""
	}
	addr := strings.TrimSpace(ssh.ListenAddress)
	if addr == "" {
		addr = "127.0.0.1"
	}
	port := ssh.Port
	if port == 0 {
		port = defaultNETCONFPort
	}
	return net.JoinHostPort(addr, strconv.Itoa(port))
}

func snapshotNETCONFSSHConfig(snapshot *model.ConfigSnapshot) *model.NETCONFSSHConfig {
	if snapshot == nil || snapshot.Config == nil || snapshot.Config.Security == nil ||
		snapshot.Config.Security.NETCONF == nil || snapshot.Config.Security.NETCONF.SSH == nil {
		return nil
	}
	return snapshot.Config.Security.NETCONF.SSH
}

func startNETCONFServer(
	ctx context.Context,
	f *daemonFlags,
	datastoreConfig *datastore.Config,
	eng *engine.Engine,
	stateProvider netconf.OperationalStateProvider,
	log *logger.Logger,
	listenAddr string,
) (*netconf.SSHServer, error) {
	log.Info("Starting NETCONF server",
		slog.String("listen", listenAddr),
		slog.Bool("standard_xpath", f.netconfXPath),
	)
	ncConfig := netconf.DefaultSSHConfig()
	ncConfig.ListenAddr = listenAddr
	ncConfig.HostKeyPath = f.hostKeyPath
	ncConfig.UserDBPath = f.userDBPath
	ncConfig.DatastorePath = f.datastorePath
	ncConfig.DatastoreConfig = datastoreConfig
	ncConfig.SkipDatastoreStartupCleanup = true
	ncConfig.AdvertiseStandardXPath = f.netconfXPath
	ncConfig.DisableStandardXPath = !f.netconfXPath

	server, err := netconf.NewSSHServer(ncConfig)
	if err != nil {
		return nil, fmt.Errorf("create NETCONF server: %w", err)
	}
	server.SetCommitHook(newNETCONFCommitHook(eng))
	server.SetOperationalStateProvider(stateProvider)
	if err := server.Start(ctx); err != nil {
		_ = server.Stop()
		return nil, fmt.Errorf("start NETCONF server: %w", err)
	}
	return server, nil
}

func listenGRPCAPI(f *daemonFlags) (net.Listener, []googlegrpc.ServerOption, string, error) {
	serverOptions, err := buildGRPCServerOptions(f)
	if err != nil {
		return nil, nil, "", err
	}
	if listenAddr := strings.TrimSpace(f.grpcListen); listenAddr != "" {
		lis, err := net.Listen("tcp", listenAddr)
		if err != nil {
			return nil, nil, "", fmt.Errorf("listen on gRPC address %s: %w", listenAddr, err)
		}
		return lis, serverOptions, "tcp+tls", nil
	}

	if err := prepareGRPCSocketPath(f.grpcSocket); err != nil {
		return nil, nil, "", err
	}
	lis, err := listenSecureGRPCSocket(f.grpcSocket)
	if err != nil {
		return nil, nil, "", fmt.Errorf("listen on gRPC socket: %w", err)
	}
	return lis, serverOptions, "unix", nil
}

func buildGRPCServerOptions(f *daemonFlags) ([]googlegrpc.ServerOption, error) {
	if strings.TrimSpace(f.grpcListen) == "" {
		if f.grpcTLSCert != "" || f.grpcTLSKey != "" || f.grpcClientCA != "" || f.grpcClientID != "" || f.grpcClientRole != "" {
			return nil, fmt.Errorf("gRPC TLS flags require --grpc-listen")
		}
		return nil, nil
	}
	if f.grpcTLSCert == "" || f.grpcTLSKey == "" {
		return nil, fmt.Errorf("--grpc-listen requires --grpc-tls-cert and --grpc-tls-key")
	}
	if f.grpcClientCA == "" {
		return nil, fmt.Errorf("--grpc-listen requires --grpc-client-ca for mutual TLS")
	}
	tlsConfig, err := buildGRPCServerTLSConfig(f)
	if err != nil {
		return nil, err
	}
	opts := []googlegrpc.ServerOption{googlegrpc.Creds(credentials.NewTLS(tlsConfig))}
	clientRoles, err := nbgrpc.ParseTLSClientRoles(f.grpcClientRole)
	if err != nil {
		return nil, fmt.Errorf("parse gRPC client roles: %w", err)
	}
	if len(clientRoles) == 0 {
		return nil, fmt.Errorf("--grpc-listen requires --grpc-client-role for gRPC authorization")
	}
	opts = append(opts,
		googlegrpc.UnaryInterceptor(nbgrpc.NewTLSClientRoleUnaryInterceptor(clientRoles)),
		googlegrpc.StreamInterceptor(nbgrpc.NewTLSClientRoleStreamInterceptor(clientRoles)),
	)
	return opts, nil
}

func buildGRPCServerTLSConfig(f *daemonFlags) (*tls.Config, error) {
	allowedClientIDs, err := parseGRPCClientIdentities(f.grpcClientID)
	if err != nil {
		return nil, err
	}
	cert, err := auth.LoadX509KeyPair(f.grpcTLSCert, f.grpcTLSKey)
	if err != nil {
		return nil, fmt.Errorf("load gRPC server cert/key: %w", err)
	}
	if f.grpcClientCA == "" {
		return nil, fmt.Errorf("gRPC TCP listener requires --grpc-client-ca for mutual TLS")
	}
	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}
	clientCAPEM, err := os.ReadFile(f.grpcClientCA)
	if err != nil {
		return nil, fmt.Errorf("read gRPC client CA: %w", err)
	}
	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(clientCAPEM) {
		return nil, fmt.Errorf("parse gRPC client CA")
	}
	cfg.ClientCAs = clientCAs
	cfg.ClientAuth = tls.RequireAndVerifyClientCert
	if len(allowedClientIDs) > 0 {
		cfg.VerifyConnection = newGRPCClientIdentityVerifier(allowedClientIDs)
	}
	return security.ApplyTLSPolicy(cfg), nil
}

func parseGRPCClientIdentities(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	identities := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		identity := strings.TrimSpace(part)
		if identity == "" {
			return nil, fmt.Errorf("invalid --grpc-client-identity: empty identity")
		}
		if _, ok := seen[identity]; ok {
			continue
		}
		seen[identity] = struct{}{}
		identities = append(identities, identity)
	}
	return identities, nil
}

func newGRPCClientIdentityVerifier(allowed []string) func(tls.ConnectionState) error {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, identity := range allowed {
		allowedSet[identity] = struct{}{}
	}
	return func(state tls.ConnectionState) error {
		for _, identity := range grpcClientCertificateIdentities(state) {
			if _, ok := allowedSet[identity]; ok {
				return nil
			}
		}
		return fmt.Errorf("gRPC client certificate identity is not allowed")
	}
}

func grpcClientCertificateIdentities(state tls.ConnectionState) []string {
	if len(state.VerifiedChains) == 0 || len(state.VerifiedChains[0]) == 0 {
		return nil
	}
	cert := state.VerifiedChains[0][0]
	identities := make([]string, 0, len(cert.URIs)+len(cert.DNSNames)+len(cert.EmailAddresses)+1)
	for _, uri := range cert.URIs {
		if uri != nil && uri.String() != "" {
			identities = append(identities, uri.String())
		}
	}
	if cert.Subject.CommonName != "" {
		identities = append(identities, cert.Subject.CommonName)
	}
	identities = append(identities, cert.DNSNames...)
	identities = append(identities, cert.EmailAddresses...)
	return identities
}

func prepareGRPCSocketPath(socketPath string) error {
	socketDir := filepath.Dir(socketPath)
	if err := os.MkdirAll(socketDir, secureGRPCSocketDirPerms); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}
	if err := validateGRPCSocketDirectory(socketDir); err != nil {
		return err
	}
	if err := removeStaleGRPCSocket(socketPath); err != nil {
		return err
	}
	return nil
}

func validateGRPCSocketDirectory(socketDir string) error {
	info, err := os.Lstat(socketDir)
	if err != nil {
		return fmt.Errorf("stat gRPC socket directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("gRPC socket directory %s must not be a symbolic link", socketDir)
	}
	if !info.IsDir() {
		return fmt.Errorf("gRPC socket parent path is not a directory: %s", socketDir)
	}
	if perms := info.Mode().Perm(); perms&0022 != 0 {
		return fmt.Errorf("insecure permissions on gRPC socket directory %s: mode=%04o", socketDir, perms)
	}
	return nil
}

func removeStaleGRPCSocket(socketPath string) error {
	info, err := os.Lstat(socketPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat stale gRPC socket: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to remove non-socket gRPC path: %s", socketPath)
	}
	if err := os.Remove(socketPath); err != nil {
		return fmt.Errorf("remove stale socket: %w", err)
	}
	return nil
}

func restrictGRPCSocketPermissions(socketPath string) error {
	info, err := os.Lstat(socketPath)
	if err != nil {
		return fmt.Errorf("stat gRPC socket: %w", err)
	}
	if err := validateGRPCSocketPathInfo(socketPath, info); err != nil {
		return err
	}
	if err := os.Chmod(socketPath, secureGRPCSocketFilePerms); err != nil {
		return fmt.Errorf("restrict gRPC socket permissions: %w", err)
	}
	currentInfo, err := os.Lstat(socketPath)
	if err != nil {
		return fmt.Errorf("stat gRPC socket after permission update: %w", err)
	}
	if err := validateGRPCSocketPathInfo(socketPath, currentInfo); err != nil {
		return err
	}
	if !os.SameFile(info, currentInfo) {
		return fmt.Errorf("refusing to restrict gRPC socket %s: file changed during permission update", socketPath)
	}
	return nil
}

func validateGRPCSocketPathInfo(socketPath string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("gRPC socket path %s must not be a symbolic link", socketPath)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("gRPC socket path is not a socket: %s", socketPath)
	}
	return nil
}

func listenSecureGRPCSocket(socketPath string) (net.Listener, error) {
	grpcSocketUmaskMu.Lock()
	defer grpcSocketUmaskMu.Unlock()

	oldUmask := syscall.Umask(secureGRPCSocketUmask)
	defer syscall.Umask(oldUmask)

	lis, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	if err := restrictGRPCSocketPermissions(socketPath); err != nil {
		_ = lis.Close()
		return nil, err
	}
	return lis, nil
}

// loadInitialConfig loads the startup config from the datastore or file.
func loadInitialConfig(ctx context.Context, f *daemonFlags, st internalstore.ConfigStore, log *logger.Logger) (*model.ConfigSnapshot, string, error) {
	if st != nil {
		snap, err := st.GetLatestSnapshot(ctx)
		if err != nil {
			return nil, "", fmt.Errorf("load config from datastore: %w", err)
		}
		if snap != nil && snap.Config != nil {
			log.Info("Loaded initial configuration from datastore")
			return snap.Clone(), "datastore", nil
		}
	}

	file, err := os.Open(f.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Warn("Config file not found, using empty config", slog.String("path", f.configPath))
			return model.NewSnapshot(model.NewRouterConfig(), 1, "system", "initial startup"), "empty", nil
		}
		return nil, "", fmt.Errorf("open config %s: %w", f.configPath, err)
	}
	defer func() { _ = file.Close() }()

	legacyCfg, err := parseLegacyConfig(file)
	if err != nil {
		return nil, "", fmt.Errorf("parse config %s: %w", f.configPath, err)
	}

	// Validate
	if err := legacyCfg.Validate(); err != nil {
		return nil, "", fmt.Errorf("validate config: %w", err)
	}

	// Convert to new model
	return model.NewSnapshot(model.FromLegacyConfig(legacyCfg), 1, "system", "initial startup"), "file", nil
}

func applyInitialConfig(ctx context.Context, eng *engine.Engine, st internalstore.ConfigStore, snap *model.ConfigSnapshot, source string) error {
	if snap == nil || snap.Config == nil {
		return fmt.Errorf("initial configuration is nil")
	}

	var prepared internalstore.PreparedCommit
	if source != "datastore" && st != nil {
		var err error
		prepared, err = st.PrepareCommit(ctx, snap)
		if err != nil {
			return fmt.Errorf("prepare initial config persistence: %w", err)
		}
	}

	beforeSnap := eng.RunningSnapshot()
	if err := eng.Apply(ctx, snap.Config, "system", "initial startup"); err != nil {
		if prepared != nil {
			_ = prepared.Abort(context.Background())
		}
		return err
	}

	if source == "datastore" {
		eng.InitializeRunning(snap.Config, initialSnapshotVersion(snap))
		return nil
	}

	if prepared != nil {
		if _, err := prepared.Commit(ctx); err != nil {
			_ = prepared.Abort(context.Background())
			if rollbackErr := rollbackEngineToSnapshot(context.Background(), eng, beforeSnap, "system", "rollback failed initial config persistence"); rollbackErr != nil {
				return fmt.Errorf("persist initial config after apply: %w (rollback failed: %v)", err, rollbackErr)
			}
			return fmt.Errorf("persist initial config after apply: %w", err)
		}
	}
	if eng.RunningSnapshot() == nil {
		eng.InitializeRunning(snap.Config, initialSnapshotVersion(snap))
	}
	return nil
}

func initialSnapshotVersion(snap *model.ConfigSnapshot) uint64 {
	if snap != nil && snap.Version > 0 {
		return snap.Version
	}
	return 1
}

func parseLegacyConfig(r io.Reader) (*config.Config, error) {
	parser := config.NewParser(r)
	return parser.Parse()
}

func parseLegacyRouterConfigText(text string) (*model.RouterConfig, error) {
	legacyCfg, err := parseLegacyConfig(strings.NewReader(text))
	if err != nil {
		return nil, err
	}
	return model.FromLegacyConfig(legacyCfg), nil
}

func installParserHooks() {
	nbgrpc.ConfigTextParser = parseLegacyRouterConfigText
}

func newNETCONFCommitHook(eng *engine.Engine) netconf.CommitHook {
	return func(ctx context.Context, req *netconf.CommitHookRequest, persist func(context.Context) (string, error)) (string, error) {
		if req == nil {
			return "", fmt.Errorf("commit request is nil")
		}
		legacyCfg, err := parseLegacyConfig(strings.NewReader(req.ConfigText))
		if err != nil {
			return "", fmt.Errorf("parse candidate config: %w", err)
		}
		if err := legacyCfg.Validate(); err != nil {
			return "", fmt.Errorf("validate candidate config: %w", err)
		}
		newCfg := model.FromLegacyConfig(legacyCfg)
		if err := eng.Validate(ctx, newCfg); err != nil {
			return "", err
		}

		beforeSnap := eng.RunningSnapshot()
		if !engine.ComputeDiff(snapshotConfig(beforeSnap), newCfg).HasChanges() {
			return "", fmt.Errorf("no configuration changes to commit")
		}
		if err := eng.Apply(ctx, newCfg, req.User, req.Message); err != nil {
			return "", err
		}

		commitID, err := persist(ctx)
		if err != nil {
			if rollbackErr := rollbackEngineToSnapshot(context.Background(), eng, beforeSnap, req.User, "rollback failed NETCONF commit persistence"); rollbackErr != nil {
				return "", fmt.Errorf("persist NETCONF commit after apply: %w (rollback failed: %v)", err, rollbackErr)
			}
			return "", fmt.Errorf("persist NETCONF commit after apply: %w", err)
		}
		return commitID, nil
	}
}

func snapshotConfig(snap *model.ConfigSnapshot) *model.RouterConfig {
	if snap == nil {
		return nil
	}
	return snap.Config
}

func rollbackEngineToSnapshot(ctx context.Context, eng *engine.Engine, snap *model.ConfigSnapshot, user, message string) error {
	cfg := model.NewRouterConfig()
	if snap != nil && snap.Config != nil {
		cfg = snap.Config
	}
	return eng.Apply(ctx, cfg, user, message)
}
