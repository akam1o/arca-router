package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	sbfrr "github.com/akam1o/arca-router/internal/southbound/frr"
	sbvpp "github.com/akam1o/arca-router/internal/southbound/vpp"
	"github.com/akam1o/arca-router/pkg/datastore"
	"github.com/akam1o/arca-router/pkg/logger"
	"github.com/akam1o/arca-router/pkg/netconf"
)

const defaultPrometheusPort = 9090

const (
	classOfServiceIntentOnlyStatus    = "intent-only"
	classOfServiceNotConfiguredStatus = "not configured"
	observabilityReadHeaderTimeout    = 5 * time.Second
	observabilityReadTimeout          = 15 * time.Second
	observabilityWriteTimeout         = 30 * time.Second
	observabilityIdleTimeout          = 60 * time.Second
)

type metricsSource struct {
	startedAt        time.Time
	engine           *engine.Engine
	netconfServer    *netconf.SSHServer
	datastore        *datastore.Config
	configAPI        webConfigAPI
	telemetryAPI     webTelemetryAPI
	webAPITokens     map[string]webAPIToken
	webAPITokenFile  string
	webAPITokenCache *webAPITokenCache
	webLog           *slog.Logger
	configSync       configSyncRuntimeSource
	frr              frrVRRPSource
	vpp              vppReconciliationSource
}

type frrVRRPSource interface {
	VRRPOperationalStatus() sbfrr.VRRPOperationalStatus
	BFDOperationalStatus() sbfrr.BFDOperationalStatus
}

type vppReconciliationSource interface {
	LCPReconciliationStatus() sbvpp.LCPReconciliationStatus
}

type vppQoSCapabilitySource interface {
	QoSCapabilityStatus() sbvpp.QoSCapabilityStatus
}

type routerMetrics struct {
	UptimeSeconds                          float64
	ConfigVersion                          uint64
	NETCONFActiveSessions                  int
	NETCONFActiveConns                     int32
	NETCONFTotalConns                      uint64
	NETCONFSuccess                         uint64
	NETCONFFailures                        uint64
	NETCONFListening                       bool
	RunningHostname                        string
	DatastoreBackend                       string
	DatastoreEtcdEndpoints                 []string
	ConfigSyncEnabled                      bool
	ConfigSyncHealthy                      bool
	ConfigSyncEtcdRevision                 int64
	ConfigSyncRunningRevision              int64
	ConfigSyncLastCheck                    time.Time
	ConfigSyncLastApply                    time.Time
	ConfigSyncLastError                    string
	ConfigSyncCommitID                     string
	ClusterEnabled                         bool
	ClusterNodeCount                       int
	ClusterEtcdSync                        bool
	ClusterEtcdEndpoints                   []string
	ClusterSyncAligned                     bool
	OverlayEVPNConfigured                  bool
	OverlayEVPNVNIs                        int
	OverlayEVPNL2VNIs                      int
	OverlayEVPNL3VNIs                      int
	OverlayEVPNMulticastVNIs               int
	HAConfigured                           bool
	HAConverged                            bool
	HAVRPGroups                            int
	HAIssues                               []string
	FRRVRRPLastRun                         time.Time
	FRRVRRPConfiguredGroups                int
	FRRVRRPObservedGroups                  int
	FRRVRRPActiveGroups                    int
	FRRVRRPGroups                          []sbfrr.VRRPGroupOperationalStatus
	FRRVRRPIssues                          []string
	FRRVRRPError                           string
	FRRBFDLastRun                          time.Time
	FRRBFDConfiguredPeers                  int
	FRRBFDObservedPeers                    int
	FRRBFDUpPeers                          int
	FRRBFDDownPeers                        int
	FRRBFDSessionDownEvents                int
	FRRBFDRxFailPackets                    int
	FRRBFDPeers                            []sbfrr.BFDPeerOperationalStatus
	FRRBFDIssues                           []string
	FRRBFDError                            string
	VPPLCPReconcileLastRun                 time.Time
	VPPLCPPairs                            int
	VPPLCPInconsistencies                  []string
	VPPLCPReconcileError                   string
	ClassOfServiceConfigured               bool
	ClassOfServiceStatus                   string
	ClassOfServiceClasses                  int
	ClassOfServiceProfiles                 int
	ClassOfServiceBindings                 int
	ClassOfServiceIntentOnly               bool
	ClassOfServiceMetadataBindingSupported bool
	ClassOfServiceQueueSchedulerSupported  bool
	ClassOfServicePolicerSupported         bool
	ClassOfServiceCountersSupported        bool
	ClassOfServiceCapabilityLastCheck      time.Time
	ClassOfServiceCapabilityError          string
	ClassOfServiceCapabilityDiagnostics    []string
}

func (s metricsSource) snapshot(now time.Time) routerMetrics {
	metrics := routerMetrics{
		DatastoreBackend:         string(datastore.BackendSQLite),
		ClusterSyncAligned:       true,
		ClassOfServiceStatus:     classOfServiceNotConfiguredStatus,
		ClassOfServiceIntentOnly: false,
	}
	var runningConfig *model.RouterConfig
	if !s.startedAt.IsZero() {
		metrics.UptimeSeconds = now.Sub(s.startedAt).Seconds()
	}
	if s.datastore != nil {
		if s.datastore.Backend != "" {
			metrics.DatastoreBackend = string(s.datastore.Backend)
		}
		metrics.DatastoreEtcdEndpoints = normalizedEndpoints(s.datastore.EtcdEndpoints)
	}
	if s.configSync != nil {
		status := s.configSync.ConfigSyncStatus()
		metrics.ConfigSyncEnabled = status.Enabled
		metrics.ConfigSyncHealthy = status.Healthy
		metrics.ConfigSyncEtcdRevision = status.EtcdRevision
		metrics.ConfigSyncRunningRevision = status.RunningRevision
		metrics.ConfigSyncLastCheck = status.LastCheck
		metrics.ConfigSyncLastApply = status.LastApply
		metrics.ConfigSyncLastError = status.LastError
		metrics.ConfigSyncCommitID = status.RunningCommitID
	}

	if s.engine != nil {
		if running := s.engine.RunningSnapshot(); running != nil {
			metrics.ConfigVersion = running.Version
			runningConfig = running.Config
			if running.Config != nil && running.Config.System != nil {
				metrics.RunningHostname = running.Config.System.HostName
			}
			applyOverlayStatus(&metrics, running.Config)
			applyClassOfServiceStatus(&metrics, running.Config)
			if running.Config != nil && running.Config.Chassis != nil && running.Config.Chassis.Cluster != nil {
				cluster := running.Config.Chassis.Cluster
				metrics.ClusterEnabled = cluster.Enabled
				metrics.ClusterNodeCount = len(cluster.Nodes)
				metrics.ClusterEtcdEndpoints = normalizedEndpoints(clusterEtcdEndpoints(running.Config))
				metrics.ClusterEtcdSync = len(metrics.ClusterEtcdEndpoints) > 0
				if metrics.ClusterEtcdSync {
					metrics.ClusterSyncAligned = s.datastore != nil &&
						s.datastore.Backend == datastore.BackendEtcd &&
						sameEndpoints(metrics.ClusterEtcdEndpoints, metrics.DatastoreEtcdEndpoints)
				}
			}
		}
	}
	if metrics.RunningHostname == "" {
		metrics.RunningHostname = "arca-router"
	}

	if s.netconfServer != nil {
		nc := s.netconfServer.GetMetrics()
		metrics.NETCONFActiveSessions = nc.ActiveSessions
		metrics.NETCONFActiveConns = nc.ActiveConnections
		metrics.NETCONFTotalConns = nc.TotalConnections
		metrics.NETCONFSuccess = nc.SuccessfulHandshakes
		metrics.NETCONFFailures = nc.FailedHandshakes
		metrics.NETCONFListening = nc.IsListening
	}
	if s.frr != nil {
		vrrp := s.frr.VRRPOperationalStatus()
		metrics.FRRVRRPLastRun = vrrp.LastRun
		metrics.FRRVRRPConfiguredGroups = vrrp.ConfiguredGroups
		metrics.FRRVRRPObservedGroups = vrrp.ObservedGroups
		metrics.FRRVRRPActiveGroups = vrrp.ActiveGroups
		metrics.FRRVRRPGroups = append([]sbfrr.VRRPGroupOperationalStatus(nil), vrrp.Groups...)
		metrics.FRRVRRPIssues = append([]string(nil), vrrp.Issues...)
		metrics.FRRVRRPError = vrrp.LastError
		bfd := s.frr.BFDOperationalStatus()
		metrics.FRRBFDLastRun = bfd.LastRun
		metrics.FRRBFDConfiguredPeers = bfd.ConfiguredPeers
		metrics.FRRBFDObservedPeers = bfd.ObservedPeers
		metrics.FRRBFDUpPeers = bfd.UpPeers
		metrics.FRRBFDDownPeers = bfd.DownPeers
		metrics.FRRBFDSessionDownEvents = bfd.SessionDownEvents
		metrics.FRRBFDRxFailPackets = bfd.RxFailPackets
		metrics.FRRBFDPeers = append([]sbfrr.BFDPeerOperationalStatus(nil), bfd.Peers...)
		metrics.FRRBFDIssues = append([]string(nil), bfd.Issues...)
		metrics.FRRBFDError = bfd.LastError
	}
	if s.vpp != nil {
		lcp := s.vpp.LCPReconciliationStatus()
		metrics.VPPLCPReconcileLastRun = lcp.LastRun
		metrics.VPPLCPPairs = lcp.PairCount
		metrics.VPPLCPInconsistencies = append([]string(nil), lcp.Inconsistencies...)
		metrics.VPPLCPReconcileError = lcp.LastError
		if qosSource, ok := s.vpp.(vppQoSCapabilitySource); ok {
			applyClassOfServiceCapabilities(&metrics, qosSource.QoSCapabilityStatus())
		}
	}
	applyHAConvergenceStatus(&metrics, runningConfig, s.vpp != nil)
	return metrics
}

func applyOverlayStatus(metrics *routerMetrics, cfg *model.RouterConfig) {
	if metrics == nil || cfg == nil || cfg.Protocols == nil || cfg.Protocols.EVPN == nil {
		return
	}
	for _, vni := range cfg.Protocols.EVPN.VNIs {
		if vni == nil {
			continue
		}
		metrics.OverlayEVPNVNIs++
		switch vni.Type {
		case "l2":
			metrics.OverlayEVPNL2VNIs++
		case "l3":
			metrics.OverlayEVPNL3VNIs++
		}
		if vni.MulticastGroup != "" {
			metrics.OverlayEVPNMulticastVNIs++
		}
	}
	metrics.OverlayEVPNConfigured = metrics.OverlayEVPNVNIs > 0
}

func applyClassOfServiceStatus(metrics *routerMetrics, cfg *model.RouterConfig) {
	if metrics == nil {
		return
	}
	metrics.ClassOfServiceStatus = classOfServiceNotConfiguredStatus
	if cfg == nil || cfg.ClassOfService == nil {
		return
	}
	metrics.ClassOfServiceConfigured = true
	metrics.ClassOfServiceStatus = classOfServiceIntentOnlyStatus
	metrics.ClassOfServiceIntentOnly = true
	metrics.ClassOfServiceClasses = countForwardingClasses(cfg.ClassOfService.ForwardingClasses)
	metrics.ClassOfServiceProfiles = countTrafficControlProfiles(cfg.ClassOfService.TrafficControlProfiles)
	metrics.ClassOfServiceBindings = countClassOfServiceBindings(cfg.ClassOfService.Interfaces)
}

func countForwardingClasses(classes map[string]*model.ForwardingClass) int {
	count := 0
	for _, class := range classes {
		if class != nil {
			count++
		}
	}
	return count
}

func countTrafficControlProfiles(profiles map[string]*model.TrafficControlProfile) int {
	count := 0
	for _, profile := range profiles {
		if profile != nil {
			count++
		}
	}
	return count
}

func countClassOfServiceBindings(interfaces map[string]*model.CoSInterface) int {
	count := 0
	for _, iface := range interfaces {
		if iface != nil && iface.OutputTrafficControlProfile != "" {
			count++
		}
	}
	return count
}

func applyClassOfServiceCapabilities(metrics *routerMetrics, status sbvpp.QoSCapabilityStatus) {
	if metrics == nil {
		return
	}
	capabilities := status.Capabilities
	metrics.ClassOfServiceMetadataBindingSupported = capabilities.MetadataBinding
	metrics.ClassOfServiceQueueSchedulerSupported = capabilities.QueueScheduler
	metrics.ClassOfServicePolicerSupported = capabilities.Policer
	metrics.ClassOfServiceCountersSupported = capabilities.OperationalCounters
	metrics.ClassOfServiceCapabilityLastCheck = status.LastCheck
	metrics.ClassOfServiceCapabilityError = status.LastError
	metrics.ClassOfServiceCapabilityDiagnostics = append([]string(nil), capabilities.Diagnostics...)
	if status.LastError != "" {
		metrics.ClassOfServiceCapabilityDiagnostics = append(metrics.ClassOfServiceCapabilityDiagnostics, "capability detection failed: "+status.LastError)
	}
}

func applyHAConvergenceStatus(metrics *routerMetrics, cfg *model.RouterConfig, hasVPP bool) {
	if metrics == nil || cfg == nil || cfg.Chassis == nil || cfg.Chassis.Cluster == nil ||
		!cfg.Chassis.Cluster.Enabled {
		return
	}
	vrrpGroups := vrrpGroupCount(cfg)
	metrics.HAVRPGroups = vrrpGroups
	if vrrpGroups == 0 {
		return
	}
	metrics.HAConfigured = true

	var issues []string
	if metrics.ClusterNodeCount < 2 {
		issues = append(issues, "cluster has fewer than two nodes")
	}
	if !metrics.ClusterEtcdSync {
		issues = append(issues, "etcd cluster sync is not configured")
	} else if !metrics.ClusterSyncAligned {
		issues = append(issues, "cluster sync endpoints are not aligned with the datastore")
	}
	if metrics.ClusterEtcdSync {
		if !metrics.ConfigSyncEnabled {
			issues = append(issues, "etcd config synchronizer is not running")
		} else if !metrics.ConfigSyncHealthy {
			issues = append(issues, "etcd config synchronizer is unhealthy")
		}
	}
	if metrics.FRRVRRPLastRun.IsZero() {
		issues = append(issues, "FRR VRRP status has not run")
	}
	if metrics.FRRVRRPError != "" {
		issues = append(issues, "FRR VRRP status check failed")
	}
	if metrics.FRRVRRPObservedGroups < metrics.HAVRPGroups {
		issues = append(issues, "FRR VRRP status is missing configured groups")
	}
	if metrics.FRRVRRPActiveGroups < metrics.HAVRPGroups {
		issues = append(issues, "FRR VRRP status has inactive groups")
	}
	if len(metrics.FRRVRRPIssues) > 0 {
		issues = append(issues, "FRR VRRP status found convergence issues")
	}
	if metrics.FRRBFDConfiguredPeers > 0 {
		if metrics.FRRBFDLastRun.IsZero() {
			issues = append(issues, "FRR BFD status has not run")
		}
		if metrics.FRRBFDError != "" {
			issues = append(issues, "FRR BFD status check failed")
		}
		if metrics.FRRBFDObservedPeers < metrics.FRRBFDConfiguredPeers {
			issues = append(issues, "FRR BFD status is missing configured peers")
		} else if metrics.FRRBFDDownPeers > 0 || metrics.FRRBFDUpPeers < metrics.FRRBFDConfiguredPeers {
			issues = append(issues, "FRR BFD status has down peers")
		}
		if len(metrics.FRRBFDIssues) > 0 {
			issues = append(issues, "FRR BFD status found convergence issues")
		}
	}
	if !hasVPP {
		issues = append(issues, "VPP LCP reconciliation status is unavailable")
	} else if metrics.VPPLCPReconcileLastRun.IsZero() {
		issues = append(issues, "VPP LCP reconciliation has not run")
	}
	if metrics.VPPLCPReconcileError != "" {
		issues = append(issues, "VPP LCP reconciliation check failed")
	}
	if len(metrics.VPPLCPInconsistencies) > 0 {
		issues = append(issues, "VPP LCP reconciliation found inconsistencies")
	}
	metrics.HAIssues = issues
	metrics.HAConverged = len(issues) == 0
}

func vrrpGroupCount(cfg *model.RouterConfig) int {
	if cfg == nil || cfg.Protocols == nil || cfg.Protocols.VRRP == nil {
		return 0
	}
	return len(cfg.Protocols.VRRP.Groups)
}

func effectiveMetricsListen(flagValue string, snapshot *model.ConfigSnapshot) string {
	if listen := strings.TrimSpace(flagValue); listen != "" {
		return listen
	}
	prometheus := snapshotPrometheusConfig(snapshot)
	if prometheus == nil || !prometheus.Enabled {
		return ""
	}
	addr := strings.TrimSpace(prometheus.ListenAddress)
	if addr == "" {
		addr = "127.0.0.1"
	}
	port := prometheus.Port
	if port == 0 {
		port = defaultPrometheusPort
	}
	return net.JoinHostPort(addr, strconv.Itoa(port))
}

func snapshotPrometheusConfig(snapshot *model.ConfigSnapshot) *model.PrometheusConfig {
	if snapshot == nil || snapshot.Config == nil || snapshot.Config.System == nil ||
		snapshot.Config.System.Services == nil {
		return nil
	}
	return snapshot.Config.System.Services.Prometheus
}

func startMetricsServer(ctx context.Context, listenAddr string, source metricsSource, log *logger.Logger) (<-chan error, error) {
	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen metrics endpoint: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", source.handleMetrics)
	mux.HandleFunc("/healthz", source.handleHealthz)

	srv := newObservabilityHTTPServer(mux)

	errCh := make(chan error, 1)
	go func() {
		log.Info("Metrics endpoint started", slog.String("listen", lis.Addr().String()))
		if err := srv.Serve(lis); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error("Metrics endpoint shutdown failed", slog.Any("error", err))
		}
	}()

	return errCh, nil
}

func newObservabilityHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: observabilityReadHeaderTimeout,
		ReadTimeout:       observabilityReadTimeout,
		WriteTimeout:      observabilityWriteTimeout,
		IdleTimeout:       observabilityIdleTimeout,
	}
}

func (s metricsSource) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write([]byte("ok\n"))
	}
}

func (s metricsSource) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	metrics := s.snapshot(time.Now())

	var b strings.Builder
	writeMetricHelp(&b, "arca_routerd_up", "Whether arca-routerd is serving metrics.")
	writeMetricType(&b, "arca_routerd_up", "gauge")
	writeMetricValue(&b, "arca_routerd_up", 1)

	writeMetricHelp(&b, "arca_routerd_uptime_seconds", "Seconds since arca-routerd started.")
	writeMetricType(&b, "arca_routerd_uptime_seconds", "counter")
	writeMetricValue(&b, "arca_routerd_uptime_seconds", metrics.UptimeSeconds)

	writeMetricHelp(&b, "arca_router_config_version", "Current running configuration version.")
	writeMetricType(&b, "arca_router_config_version", "gauge")
	writeMetricValue(&b, "arca_router_config_version", float64(metrics.ConfigVersion))

	writeMetricHelp(&b, "arca_router_config_sync_etcd_enabled", "Whether etcd-backed running configuration synchronization is enabled.")
	writeMetricType(&b, "arca_router_config_sync_etcd_enabled", "gauge")
	writeMetricHelp(&b, "arca_router_config_sync_etcd_healthy", "Whether the latest etcd config synchronization check succeeded.")
	writeMetricType(&b, "arca_router_config_sync_etcd_healthy", "gauge")
	writeMetricHelp(&b, "arca_router_config_sync_etcd_revision", "Latest etcd cluster revision observed by the config synchronizer.")
	writeMetricType(&b, "arca_router_config_sync_etcd_revision", "gauge")
	writeMetricHelp(&b, "arca_router_config_sync_running_revision", "Latest etcd running configuration key revision observed by the config synchronizer.")
	writeMetricType(&b, "arca_router_config_sync_running_revision", "gauge")
	writeMetricHelp(&b, "arca_router_config_sync_error", "Whether the latest etcd config synchronization check failed.")
	writeMetricType(&b, "arca_router_config_sync_error", "gauge")
	writeMetricHelp(&b, "arca_router_config_sync_last_check_timestamp_seconds", "Unix timestamp of the latest etcd config synchronization check.")
	writeMetricType(&b, "arca_router_config_sync_last_check_timestamp_seconds", "gauge")
	writeMetricHelp(&b, "arca_router_config_sync_last_apply_timestamp_seconds", "Unix timestamp of the latest config applied from etcd.")
	writeMetricType(&b, "arca_router_config_sync_last_apply_timestamp_seconds", "gauge")
	writeMetricBool(&b, "arca_router_config_sync_etcd_enabled", metrics.ConfigSyncEnabled)
	writeMetricBool(&b, "arca_router_config_sync_etcd_healthy", metrics.ConfigSyncHealthy)
	writeMetricValue(&b, "arca_router_config_sync_etcd_revision", float64(metrics.ConfigSyncEtcdRevision))
	writeMetricValue(&b, "arca_router_config_sync_running_revision", float64(metrics.ConfigSyncRunningRevision))
	writeMetricBool(&b, "arca_router_config_sync_error", metrics.ConfigSyncLastError != "")
	writeMetricValue(&b, "arca_router_config_sync_last_check_timestamp_seconds", unixTimestampSeconds(metrics.ConfigSyncLastCheck))
	writeMetricValue(&b, "arca_router_config_sync_last_apply_timestamp_seconds", unixTimestampSeconds(metrics.ConfigSyncLastApply))

	writeMetricHelp(&b, "arca_router_cluster_enabled", "Whether chassis clustering is enabled in the running configuration.")
	writeMetricType(&b, "arca_router_cluster_enabled", "gauge")
	writeMetricHelp(&b, "arca_router_cluster_nodes", "Number of configured chassis cluster nodes.")
	writeMetricType(&b, "arca_router_cluster_nodes", "gauge")
	writeMetricHelp(&b, "arca_router_cluster_sync_etcd_configured", "Whether chassis cluster sync uses etcd endpoints.")
	writeMetricType(&b, "arca_router_cluster_sync_etcd_configured", "gauge")
	writeMetricHelp(&b, "arca_router_cluster_sync_aligned", "Whether cluster sync etcd config matches the daemon datastore backend and endpoints.")
	writeMetricType(&b, "arca_router_cluster_sync_aligned", "gauge")
	writeMetricBool(&b, "arca_router_cluster_enabled", metrics.ClusterEnabled)
	writeMetricValue(&b, "arca_router_cluster_nodes", float64(metrics.ClusterNodeCount))
	writeMetricBool(&b, "arca_router_cluster_sync_etcd_configured", metrics.ClusterEtcdSync)
	writeMetricBool(&b, "arca_router_cluster_sync_aligned", metrics.ClusterSyncAligned)

	writeMetricHelp(&b, "arca_router_overlay_evpn_configured", "Whether EVPN/VXLAN overlay intent is configured.")
	writeMetricType(&b, "arca_router_overlay_evpn_configured", "gauge")
	writeMetricHelp(&b, "arca_router_overlay_evpn_vnis", "Number of configured EVPN/VXLAN VNIs.")
	writeMetricType(&b, "arca_router_overlay_evpn_vnis", "gauge")
	writeMetricHelp(&b, "arca_router_overlay_evpn_l2_vnis", "Number of configured EVPN/VXLAN L2 VNIs.")
	writeMetricType(&b, "arca_router_overlay_evpn_l2_vnis", "gauge")
	writeMetricHelp(&b, "arca_router_overlay_evpn_l3_vnis", "Number of configured EVPN/VXLAN L3 VNIs.")
	writeMetricType(&b, "arca_router_overlay_evpn_l3_vnis", "gauge")
	writeMetricHelp(&b, "arca_router_overlay_evpn_multicast_vnis", "Number of EVPN/VXLAN VNIs configured with a multicast group.")
	writeMetricType(&b, "arca_router_overlay_evpn_multicast_vnis", "gauge")
	writeMetricBool(&b, "arca_router_overlay_evpn_configured", metrics.OverlayEVPNConfigured)
	writeMetricValue(&b, "arca_router_overlay_evpn_vnis", float64(metrics.OverlayEVPNVNIs))
	writeMetricValue(&b, "arca_router_overlay_evpn_l2_vnis", float64(metrics.OverlayEVPNL2VNIs))
	writeMetricValue(&b, "arca_router_overlay_evpn_l3_vnis", float64(metrics.OverlayEVPNL3VNIs))
	writeMetricValue(&b, "arca_router_overlay_evpn_multicast_vnis", float64(metrics.OverlayEVPNMulticastVNIs))

	writeMetricHelp(&b, "arca_router_ha_configured", "Whether control-plane HA is configured with chassis clustering and VRRP groups.")
	writeMetricType(&b, "arca_router_ha_configured", "gauge")
	writeMetricHelp(&b, "arca_router_ha_converged", "Whether configured control-plane HA has no detected config sync, FRR runtime, or VPP LCP convergence issues.")
	writeMetricType(&b, "arca_router_ha_converged", "gauge")
	writeMetricHelp(&b, "arca_router_ha_vrrp_groups", "Number of configured VRRP groups participating in control-plane HA.")
	writeMetricType(&b, "arca_router_ha_vrrp_groups", "gauge")
	writeMetricHelp(&b, "arca_router_ha_convergence_issues", "Number of detected control-plane HA convergence issues.")
	writeMetricType(&b, "arca_router_ha_convergence_issues", "gauge")
	writeMetricBool(&b, "arca_router_ha_configured", metrics.HAConfigured)
	writeMetricBool(&b, "arca_router_ha_converged", metrics.HAConverged)
	writeMetricValue(&b, "arca_router_ha_vrrp_groups", float64(metrics.HAVRPGroups))
	writeMetricValue(&b, "arca_router_ha_convergence_issues", float64(len(metrics.HAIssues)))

	writeMetricHelp(&b, "arca_router_frr_vrrp_configured_groups", "Number of VRRP groups configured for FRR.")
	writeMetricType(&b, "arca_router_frr_vrrp_configured_groups", "gauge")
	writeMetricHelp(&b, "arca_router_frr_vrrp_observed_groups", "Number of configured VRRP groups observed in FRR operational state.")
	writeMetricType(&b, "arca_router_frr_vrrp_observed_groups", "gauge")
	writeMetricHelp(&b, "arca_router_frr_vrrp_active_groups", "Number of configured VRRP groups in an active FRR state.")
	writeMetricType(&b, "arca_router_frr_vrrp_active_groups", "gauge")
	writeMetricHelp(&b, "arca_router_frr_vrrp_issues", "Number of detected FRR VRRP operational convergence issues.")
	writeMetricType(&b, "arca_router_frr_vrrp_issues", "gauge")
	writeMetricHelp(&b, "arca_router_frr_vrrp_error", "Whether the latest FRR VRRP operational status check failed.")
	writeMetricType(&b, "arca_router_frr_vrrp_error", "gauge")
	writeMetricHelp(&b, "arca_router_frr_vrrp_last_check_timestamp_seconds", "Unix timestamp of the latest FRR VRRP operational status check.")
	writeMetricType(&b, "arca_router_frr_vrrp_last_check_timestamp_seconds", "gauge")
	writeMetricValue(&b, "arca_router_frr_vrrp_configured_groups", float64(metrics.FRRVRRPConfiguredGroups))
	writeMetricValue(&b, "arca_router_frr_vrrp_observed_groups", float64(metrics.FRRVRRPObservedGroups))
	writeMetricValue(&b, "arca_router_frr_vrrp_active_groups", float64(metrics.FRRVRRPActiveGroups))
	writeMetricValue(&b, "arca_router_frr_vrrp_issues", float64(len(metrics.FRRVRRPIssues)))
	writeMetricBool(&b, "arca_router_frr_vrrp_error", metrics.FRRVRRPError != "")
	writeMetricValue(&b, "arca_router_frr_vrrp_last_check_timestamp_seconds", unixTimestampSeconds(metrics.FRRVRRPLastRun))

	writeMetricHelp(&b, "arca_router_frr_bfd_configured_peers", "Number of BFD peers configured for FRR.")
	writeMetricType(&b, "arca_router_frr_bfd_configured_peers", "gauge")
	writeMetricHelp(&b, "arca_router_frr_bfd_observed_peers", "Number of BFD peers observed in FRR operational state.")
	writeMetricType(&b, "arca_router_frr_bfd_observed_peers", "gauge")
	writeMetricHelp(&b, "arca_router_frr_bfd_up_peers", "Number of observed FRR BFD peers in the up state.")
	writeMetricType(&b, "arca_router_frr_bfd_up_peers", "gauge")
	writeMetricHelp(&b, "arca_router_frr_bfd_down_peers", "Number of observed FRR BFD peers not in the up state.")
	writeMetricType(&b, "arca_router_frr_bfd_down_peers", "gauge")
	writeMetricHelp(&b, "arca_router_frr_bfd_session_down_events", "Total FRR BFD session-down events observed from peer counters.")
	writeMetricType(&b, "arca_router_frr_bfd_session_down_events", "counter")
	writeMetricHelp(&b, "arca_router_frr_bfd_rx_fail_packets", "Total FRR BFD receive-failure packets observed from peer counters.")
	writeMetricType(&b, "arca_router_frr_bfd_rx_fail_packets", "counter")
	writeMetricHelp(&b, "arca_router_frr_bfd_issues", "Number of detected FRR BFD operational convergence issues.")
	writeMetricType(&b, "arca_router_frr_bfd_issues", "gauge")
	writeMetricHelp(&b, "arca_router_frr_bfd_error", "Whether the latest FRR BFD operational status check failed.")
	writeMetricType(&b, "arca_router_frr_bfd_error", "gauge")
	writeMetricHelp(&b, "arca_router_frr_bfd_last_check_timestamp_seconds", "Unix timestamp of the latest FRR BFD operational status check.")
	writeMetricType(&b, "arca_router_frr_bfd_last_check_timestamp_seconds", "gauge")
	writeMetricValue(&b, "arca_router_frr_bfd_configured_peers", float64(metrics.FRRBFDConfiguredPeers))
	writeMetricValue(&b, "arca_router_frr_bfd_observed_peers", float64(metrics.FRRBFDObservedPeers))
	writeMetricValue(&b, "arca_router_frr_bfd_up_peers", float64(metrics.FRRBFDUpPeers))
	writeMetricValue(&b, "arca_router_frr_bfd_down_peers", float64(metrics.FRRBFDDownPeers))
	writeMetricValue(&b, "arca_router_frr_bfd_session_down_events", float64(metrics.FRRBFDSessionDownEvents))
	writeMetricValue(&b, "arca_router_frr_bfd_rx_fail_packets", float64(metrics.FRRBFDRxFailPackets))
	writeMetricValue(&b, "arca_router_frr_bfd_issues", float64(len(metrics.FRRBFDIssues)))
	writeMetricBool(&b, "arca_router_frr_bfd_error", metrics.FRRBFDError != "")
	writeMetricValue(&b, "arca_router_frr_bfd_last_check_timestamp_seconds", unixTimestampSeconds(metrics.FRRBFDLastRun))

	writeMetricHelp(&b, "arca_router_vpp_lcp_pairs", "Number of VPP LCP pairs known after the latest reconciliation.")
	writeMetricType(&b, "arca_router_vpp_lcp_pairs", "gauge")
	writeMetricHelp(&b, "arca_router_vpp_lcp_inconsistencies", "Number of VPP LCP reconciliation inconsistencies detected.")
	writeMetricType(&b, "arca_router_vpp_lcp_inconsistencies", "gauge")
	writeMetricHelp(&b, "arca_router_vpp_lcp_reconcile_error", "Whether the latest VPP LCP reconciliation check failed.")
	writeMetricType(&b, "arca_router_vpp_lcp_reconcile_error", "gauge")
	writeMetricHelp(&b, "arca_router_vpp_lcp_last_reconcile_timestamp_seconds", "Unix timestamp of the latest VPP LCP reconciliation check.")
	writeMetricType(&b, "arca_router_vpp_lcp_last_reconcile_timestamp_seconds", "gauge")
	writeMetricValue(&b, "arca_router_vpp_lcp_pairs", float64(metrics.VPPLCPPairs))
	writeMetricValue(&b, "arca_router_vpp_lcp_inconsistencies", float64(len(metrics.VPPLCPInconsistencies)))
	writeMetricBool(&b, "arca_router_vpp_lcp_reconcile_error", metrics.VPPLCPReconcileError != "")
	writeMetricValue(&b, "arca_router_vpp_lcp_last_reconcile_timestamp_seconds", unixTimestampSeconds(metrics.VPPLCPReconcileLastRun))

	writeMetricHelp(&b, "arca_router_class_of_service_configured", "Whether class-of-service is configured in the running configuration.")
	writeMetricType(&b, "arca_router_class_of_service_configured", "gauge")
	writeMetricHelp(&b, "arca_router_class_of_service_forwarding_classes", "Number of configured class-of-service forwarding classes.")
	writeMetricType(&b, "arca_router_class_of_service_forwarding_classes", "gauge")
	writeMetricHelp(&b, "arca_router_class_of_service_traffic_control_profiles", "Number of configured class-of-service traffic-control profiles.")
	writeMetricType(&b, "arca_router_class_of_service_traffic_control_profiles", "gauge")
	writeMetricHelp(&b, "arca_router_class_of_service_interface_bindings", "Number of configured class-of-service interface bindings.")
	writeMetricType(&b, "arca_router_class_of_service_interface_bindings", "gauge")
	writeMetricHelp(&b, "arca_router_class_of_service_intent_only", "Whether class-of-service enforcement is currently stored as intent-only VPP interface metadata.")
	writeMetricType(&b, "arca_router_class_of_service_intent_only", "gauge")
	writeMetricHelp(&b, "arca_router_class_of_service_metadata_binding_supported", "Whether VPP supports persisting arca QoS intent as interface metadata.")
	writeMetricType(&b, "arca_router_class_of_service_metadata_binding_supported", "gauge")
	writeMetricHelp(&b, "arca_router_class_of_service_queue_scheduler_supported", "Whether VPP queue scheduler enforcement is available through the bundled binapi.")
	writeMetricType(&b, "arca_router_class_of_service_queue_scheduler_supported", "gauge")
	writeMetricHelp(&b, "arca_router_class_of_service_policer_supported", "Whether VPP policer enforcement is available through the bundled binapi.")
	writeMetricType(&b, "arca_router_class_of_service_policer_supported", "gauge")
	writeMetricHelp(&b, "arca_router_class_of_service_counters_supported", "Whether VPP operational QoS counters are available through the bundled binapi.")
	writeMetricType(&b, "arca_router_class_of_service_counters_supported", "gauge")
	writeMetricHelp(&b, "arca_router_class_of_service_capability_error", "Whether the latest VPP QoS capability detection failed.")
	writeMetricType(&b, "arca_router_class_of_service_capability_error", "gauge")
	writeMetricHelp(&b, "arca_router_class_of_service_capability_last_check_timestamp_seconds", "Unix timestamp of the latest VPP QoS capability detection.")
	writeMetricType(&b, "arca_router_class_of_service_capability_last_check_timestamp_seconds", "gauge")
	writeMetricBool(&b, "arca_router_class_of_service_configured", metrics.ClassOfServiceConfigured)
	writeMetricValue(&b, "arca_router_class_of_service_forwarding_classes", float64(metrics.ClassOfServiceClasses))
	writeMetricValue(&b, "arca_router_class_of_service_traffic_control_profiles", float64(metrics.ClassOfServiceProfiles))
	writeMetricValue(&b, "arca_router_class_of_service_interface_bindings", float64(metrics.ClassOfServiceBindings))
	writeMetricBool(&b, "arca_router_class_of_service_intent_only", metrics.ClassOfServiceIntentOnly)
	writeMetricBool(&b, "arca_router_class_of_service_metadata_binding_supported", metrics.ClassOfServiceMetadataBindingSupported)
	writeMetricBool(&b, "arca_router_class_of_service_queue_scheduler_supported", metrics.ClassOfServiceQueueSchedulerSupported)
	writeMetricBool(&b, "arca_router_class_of_service_policer_supported", metrics.ClassOfServicePolicerSupported)
	writeMetricBool(&b, "arca_router_class_of_service_counters_supported", metrics.ClassOfServiceCountersSupported)
	writeMetricBool(&b, "arca_router_class_of_service_capability_error", metrics.ClassOfServiceCapabilityError != "")
	writeMetricValue(&b, "arca_router_class_of_service_capability_last_check_timestamp_seconds", unixTimestampSeconds(metrics.ClassOfServiceCapabilityLastCheck))

	writeMetricHelp(&b, "arca_router_netconf_active_sessions", "Current active NETCONF sessions.")
	writeMetricType(&b, "arca_router_netconf_active_sessions", "gauge")
	writeMetricHelp(&b, "arca_router_netconf_active_connections", "Current active NETCONF SSH connections.")
	writeMetricType(&b, "arca_router_netconf_active_connections", "gauge")
	writeMetricHelp(&b, "arca_router_netconf_total_connections", "Total NETCONF SSH connections accepted.")
	writeMetricType(&b, "arca_router_netconf_total_connections", "counter")
	writeMetricHelp(&b, "arca_router_netconf_successful_handshakes", "Total successful NETCONF SSH handshakes.")
	writeMetricType(&b, "arca_router_netconf_successful_handshakes", "counter")
	writeMetricHelp(&b, "arca_router_netconf_failed_handshakes", "Total failed NETCONF SSH handshakes.")
	writeMetricType(&b, "arca_router_netconf_failed_handshakes", "counter")
	writeMetricHelp(&b, "arca_router_netconf_listening", "Whether the NETCONF SSH server is listening.")
	writeMetricType(&b, "arca_router_netconf_listening", "gauge")

	writeMetricValue(&b, "arca_router_netconf_active_sessions", float64(metrics.NETCONFActiveSessions))
	writeMetricValue(&b, "arca_router_netconf_active_connections", float64(metrics.NETCONFActiveConns))
	writeMetricValue(&b, "arca_router_netconf_total_connections", float64(metrics.NETCONFTotalConns))
	writeMetricValue(&b, "arca_router_netconf_successful_handshakes", float64(metrics.NETCONFSuccess))
	writeMetricValue(&b, "arca_router_netconf_failed_handshakes", float64(metrics.NETCONFFailures))
	writeMetricBool(&b, "arca_router_netconf_listening", metrics.NETCONFListening)

	_, _ = w.Write([]byte(b.String()))
}

func writeMetricHelp(b *strings.Builder, name, help string) {
	b.WriteString("# HELP ")
	b.WriteString(name)
	b.WriteByte(' ')
	b.WriteString(help)
	b.WriteByte('\n')
}

func writeMetricType(b *strings.Builder, name, metricType string) {
	b.WriteString("# TYPE ")
	b.WriteString(name)
	b.WriteByte(' ')
	b.WriteString(metricType)
	b.WriteByte('\n')
}

func writeMetricValue(b *strings.Builder, name string, value float64) {
	b.WriteString(name)
	b.WriteByte(' ')
	b.WriteString(strconv.FormatFloat(value, 'f', -1, 64))
	b.WriteByte('\n')
}

func writeMetricBool(b *strings.Builder, name string, value bool) {
	if value {
		writeMetricValue(b, name, 1)
		return
	}
	writeMetricValue(b, name, 0)
}

func unixTimestampSeconds(ts time.Time) float64 {
	if ts.IsZero() {
		return 0
	}
	return float64(ts.Unix())
}
