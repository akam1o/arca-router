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
	sbvpp "github.com/akam1o/arca-router/internal/southbound/vpp"
	"github.com/akam1o/arca-router/pkg/datastore"
	"github.com/akam1o/arca-router/pkg/logger"
	"github.com/akam1o/arca-router/pkg/netconf"
)

const defaultPrometheusPort = 9090

type metricsSource struct {
	startedAt     time.Time
	engine        *engine.Engine
	netconfServer *netconf.SSHServer
	datastore     *datastore.Config
	configAPI     webConfigAPI
	configSync    configSyncRuntimeSource
	vpp           vppReconciliationSource
}

type vppReconciliationSource interface {
	LCPReconciliationStatus() sbvpp.LCPReconciliationStatus
}

type routerMetrics struct {
	UptimeSeconds             float64
	ConfigVersion             uint64
	NETCONFActiveSessions     int
	NETCONFActiveConns        int32
	NETCONFTotalConns         uint64
	NETCONFSuccess            uint64
	NETCONFFailures           uint64
	NETCONFListening          bool
	RunningHostname           string
	DatastoreBackend          string
	DatastoreEtcdEndpoints    []string
	ConfigSyncEnabled         bool
	ConfigSyncHealthy         bool
	ConfigSyncEtcdRevision    int64
	ConfigSyncRunningRevision int64
	ConfigSyncLastCheck       time.Time
	ConfigSyncLastApply       time.Time
	ConfigSyncLastError       string
	ConfigSyncCommitID        string
	ClusterEnabled            bool
	ClusterNodeCount          int
	ClusterEtcdSync           bool
	ClusterEtcdEndpoints      []string
	ClusterSyncAligned        bool
	HAConfigured              bool
	HAConverged               bool
	HAVRPGroups               int
	HAIssues                  []string
	VPPLCPReconcileLastRun    time.Time
	VPPLCPPairs               int
	VPPLCPInconsistencies     []string
	VPPLCPReconcileError      string
}

func (s metricsSource) snapshot(now time.Time) routerMetrics {
	metrics := routerMetrics{
		DatastoreBackend:   string(datastore.BackendSQLite),
		ClusterSyncAligned: true,
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
	if s.vpp != nil {
		lcp := s.vpp.LCPReconciliationStatus()
		metrics.VPPLCPReconcileLastRun = lcp.LastRun
		metrics.VPPLCPPairs = lcp.PairCount
		metrics.VPPLCPInconsistencies = append([]string(nil), lcp.Inconsistencies...)
		metrics.VPPLCPReconcileError = lcp.LastError
	}
	applyHAConvergenceStatus(&metrics, runningConfig, s.vpp != nil)
	return metrics
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

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

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

	writeMetricHelp(&b, "arca_router_ha_configured", "Whether control-plane HA is configured with chassis clustering and VRRP groups.")
	writeMetricType(&b, "arca_router_ha_configured", "gauge")
	writeMetricHelp(&b, "arca_router_ha_converged", "Whether configured control-plane HA has no detected config sync or VPP LCP convergence issues.")
	writeMetricType(&b, "arca_router_ha_converged", "gauge")
	writeMetricHelp(&b, "arca_router_ha_vrrp_groups", "Number of configured VRRP groups participating in control-plane HA.")
	writeMetricType(&b, "arca_router_ha_vrrp_groups", "gauge")
	writeMetricHelp(&b, "arca_router_ha_convergence_issues", "Number of detected control-plane HA convergence issues.")
	writeMetricType(&b, "arca_router_ha_convergence_issues", "gauge")
	writeMetricBool(&b, "arca_router_ha_configured", metrics.HAConfigured)
	writeMetricBool(&b, "arca_router_ha_converged", metrics.HAConverged)
	writeMetricValue(&b, "arca_router_ha_vrrp_groups", float64(metrics.HAVRPGroups))
	writeMetricValue(&b, "arca_router_ha_convergence_issues", float64(len(metrics.HAIssues)))

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
