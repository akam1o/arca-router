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
	now := time.Now()
	uptime := now.Sub(s.startedAt).Seconds()
	running := s.engine.RunningSnapshot()

	var b strings.Builder
	writeMetricHelp(&b, "arca_routerd_up", "Whether arca-routerd is serving metrics.")
	writeMetricType(&b, "arca_routerd_up", "gauge")
	writeMetricValue(&b, "arca_routerd_up", 1)

	writeMetricHelp(&b, "arca_routerd_uptime_seconds", "Seconds since arca-routerd started.")
	writeMetricType(&b, "arca_routerd_uptime_seconds", "counter")
	writeMetricValue(&b, "arca_routerd_uptime_seconds", uptime)

	writeMetricHelp(&b, "arca_router_config_version", "Current running configuration version.")
	writeMetricType(&b, "arca_router_config_version", "gauge")
	if running != nil {
		writeMetricValue(&b, "arca_router_config_version", float64(running.Version))
	} else {
		writeMetricValue(&b, "arca_router_config_version", 0)
	}

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

	if s.netconfServer != nil {
		metrics := s.netconfServer.GetMetrics()
		writeMetricValue(&b, "arca_router_netconf_active_sessions", float64(metrics.ActiveSessions))
		writeMetricValue(&b, "arca_router_netconf_active_connections", float64(metrics.ActiveConnections))
		writeMetricValue(&b, "arca_router_netconf_total_connections", float64(metrics.TotalConnections))
		writeMetricValue(&b, "arca_router_netconf_successful_handshakes", float64(metrics.SuccessfulHandshakes))
		writeMetricValue(&b, "arca_router_netconf_failed_handshakes", float64(metrics.FailedHandshakes))
		if metrics.IsListening {
			writeMetricValue(&b, "arca_router_netconf_listening", 1)
		} else {
			writeMetricValue(&b, "arca_router_netconf_listening", 0)
		}
	} else {
		writeMetricValue(&b, "arca_router_netconf_active_sessions", 0)
		writeMetricValue(&b, "arca_router_netconf_active_connections", 0)
		writeMetricValue(&b, "arca_router_netconf_total_connections", 0)
		writeMetricValue(&b, "arca_router_netconf_successful_handshakes", 0)
		writeMetricValue(&b, "arca_router_netconf_failed_handshakes", 0)
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
