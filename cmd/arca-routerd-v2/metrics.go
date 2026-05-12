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
	"github.com/akam1o/arca-router/pkg/logger"
	"github.com/akam1o/arca-router/pkg/netconf"
)

type metricsSource struct {
	startedAt     time.Time
	engine        *engine.Engine
	netconfServer *netconf.SSHServer
}

type routerMetrics struct {
	UptimeSeconds         float64
	ConfigVersion         uint64
	NETCONFActiveSessions int
	NETCONFActiveConns    int32
	NETCONFTotalConns     uint64
	NETCONFSuccess        uint64
	NETCONFFailures       uint64
	NETCONFListening      bool
	RunningHostname       string
}

func (s metricsSource) snapshot(now time.Time) routerMetrics {
	metrics := routerMetrics{}
	if !s.startedAt.IsZero() {
		metrics.UptimeSeconds = now.Sub(s.startedAt).Seconds()
	}

	if s.engine != nil {
		if running := s.engine.RunningSnapshot(); running != nil {
			metrics.ConfigVersion = running.Version
			if running.Config != nil && running.Config.System != nil {
				metrics.RunningHostname = running.Config.System.HostName
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
	return metrics
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
	if metrics.NETCONFListening {
		writeMetricValue(&b, "arca_router_netconf_listening", 1)
	} else {
		writeMetricValue(&b, "arca_router_netconf_listening", 0)
	}

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
