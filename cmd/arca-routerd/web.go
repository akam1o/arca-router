package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/akam1o/arca-router/internal/model"
	pkgconfig "github.com/akam1o/arca-router/pkg/config"
	"github.com/akam1o/arca-router/pkg/logger"
)

const defaultWebUIPort = 8080

type webStatus struct {
	Version         string          `json:"version"`
	Commit          string          `json:"commit"`
	BuildDate       string          `json:"build_date"`
	UptimeSeconds   float64         `json:"uptime_seconds"`
	ConfigVersion   uint64          `json:"config_version"`
	RunningHostname string          `json:"running_hostname"`
	Datastore       webDatastore    `json:"datastore"`
	Cluster         webCluster      `json:"cluster"`
	NETCONF         webNETCONFStats `json:"netconf"`
}

type webDatastore struct {
	Backend       string   `json:"backend"`
	EtcdEndpoints []string `json:"etcd_endpoints,omitempty"`
}

type webCluster struct {
	Enabled            bool     `json:"enabled"`
	NodeCount          int      `json:"node_count"`
	EtcdSyncConfigured bool     `json:"etcd_sync_configured"`
	EtcdEndpoints      []string `json:"etcd_endpoints,omitempty"`
	SyncAligned        bool     `json:"sync_aligned"`
}

type webNETCONFStats struct {
	Listening         bool   `json:"listening"`
	ActiveSessions    int    `json:"active_sessions"`
	ActiveConnections int32  `json:"active_connections"`
	TotalConnections  uint64 `json:"total_connections"`
	SuccessfulAuth    uint64 `json:"successful_auth"`
	FailedAuth        uint64 `json:"failed_auth"`
}

type webConfig struct {
	ConfigText string `json:"config_text"`
	Version    uint64 `json:"version"`
}

type webIndexData struct {
	Status               webStatus
	Uptime               string
	NETCONFState         string
	NETCONFStateClass    string
	NETCONFConnections   string
	ClusterState         string
	ClusterStateClass    string
	ClusterSyncState     string
	ClusterSyncAlignment string
	ClusterNodeCount     string
	DatastoreBackend     string
	GeneratedAt          string
	ConfigVersionString  string
	RunningConfig        string
}

var webIndexTemplate = template.Must(template.New("web-index").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>arca-router</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f6f7f9;
      --panel: #ffffff;
      --line: #d8dde5;
      --text: #17202a;
      --muted: #667085;
      --accent: #0f766e;
      --warn: #b45309;
      --neutral: #475467;
      --ok-bg: #dff5ef;
      --warn-bg: #fff1d6;
      --neutral-bg: #eef2f6;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      min-height: 100vh;
      background: var(--bg);
      color: var(--text);
      font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    main {
      width: min(1120px, calc(100vw - 32px));
      margin: 0 auto;
      padding: 32px 0;
    }
    header {
      display: flex;
      align-items: flex-end;
      justify-content: space-between;
      gap: 24px;
      padding-bottom: 20px;
      border-bottom: 1px solid var(--line);
    }
    h1, h2, p { margin: 0; }
    h1 { font-size: clamp(28px, 4vw, 44px); font-weight: 650; }
    h2 { font-size: 15px; font-weight: 650; }
    .version { color: var(--muted); font-size: 14px; text-align: right; }
    .grid {
      display: grid;
      grid-template-columns: repeat(4, minmax(0, 1fr));
      gap: 12px;
      margin-top: 20px;
    }
    .panel {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 16px;
      min-height: 116px;
    }
    .span-2 { grid-column: span 2; }
    .metric {
      display: flex;
      flex-direction: column;
      gap: 10px;
    }
    .label { color: var(--muted); font-size: 13px; }
    .value {
      overflow-wrap: anywhere;
      font-size: 28px;
      font-weight: 650;
      line-height: 1.1;
    }
    .small-value { font-size: 18px; line-height: 1.25; }
    .rows {
      display: grid;
      gap: 10px;
      margin-top: 14px;
    }
    .row {
      display: flex;
      justify-content: space-between;
      gap: 16px;
      color: var(--muted);
      font-size: 14px;
    }
    .row strong { color: var(--text); font-weight: 600; }
    .pill {
      display: inline-flex;
      align-items: center;
      width: fit-content;
      min-height: 28px;
      padding: 3px 10px;
      border-radius: 999px;
      font-size: 13px;
      font-weight: 650;
    }
    .pill.ok { background: var(--ok-bg); color: var(--accent); }
    .pill.warn { background: var(--warn-bg); color: var(--warn); }
    .pill.neutral { background: var(--neutral-bg); color: var(--neutral); }
    .config {
      max-height: 300px;
      margin: 14px 0 0;
      overflow: auto;
      overflow-wrap: anywhere;
      white-space: pre-wrap;
      border: 1px solid var(--line);
      border-radius: 6px;
      background: #f8fafc;
      padding: 12px;
      color: var(--text);
      font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
      font-size: 12px;
      line-height: 1.5;
    }
    footer {
      display: flex;
      justify-content: space-between;
      gap: 16px;
      margin-top: 20px;
      color: var(--muted);
      font-size: 12px;
    }
    @media (max-width: 860px) {
      main { width: min(100vw - 24px, 720px); padding: 20px 0; }
      header { align-items: flex-start; flex-direction: column; }
      .version { text-align: left; }
      .grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    }
    @media (max-width: 560px) {
      .grid { grid-template-columns: 1fr; }
      .span-2 { grid-column: auto; }
      footer { flex-direction: column; }
    }
  </style>
</head>
<body>
  <main>
    <header>
      <div>
        <h1>{{.Status.RunningHostname}}</h1>
        <p class="label">arca-router</p>
      </div>
      <p class="version">v{{.Status.Version}} | {{.Status.Commit}}</p>
    </header>

    <section class="grid">
      <article class="panel metric">
        <span class="label">Config version</span>
        <span class="value">{{.ConfigVersionString}}</span>
      </article>
      <article class="panel metric">
        <span class="label">Uptime</span>
        <span class="value">{{.Uptime}}</span>
      </article>
      <article class="panel metric">
        <span class="label">NETCONF</span>
        <span class="pill {{.NETCONFStateClass}}">{{.NETCONFState}}</span>
      </article>
      <article class="panel metric">
        <span class="label">Connections</span>
        <span class="value">{{.NETCONFConnections}}</span>
      </article>
      <article class="panel metric">
        <span class="label">Datastore</span>
        <span class="value small-value">{{.DatastoreBackend}}</span>
      </article>
      <article class="panel metric">
        <span class="label">Cluster</span>
        <span class="pill {{.ClusterStateClass}}">{{.ClusterState}}</span>
      </article>

      <article class="panel span-2">
        <h2>NETCONF sessions</h2>
        <div class="rows">
          <div class="row"><span>Active sessions</span><strong>{{.Status.NETCONF.ActiveSessions}}</strong></div>
          <div class="row"><span>Active SSH connections</span><strong>{{.Status.NETCONF.ActiveConnections}}</strong></div>
          <div class="row"><span>Total SSH connections</span><strong>{{.Status.NETCONF.TotalConnections}}</strong></div>
        </div>
      </article>
      <article class="panel span-2">
        <h2>Authentication</h2>
        <div class="rows">
          <div class="row"><span>Successful handshakes</span><strong>{{.Status.NETCONF.SuccessfulAuth}}</strong></div>
          <div class="row"><span>Failed handshakes</span><strong>{{.Status.NETCONF.FailedAuth}}</strong></div>
          <div class="row"><span>Build date</span><strong>{{.Status.BuildDate}}</strong></div>
        </div>
      </article>
      <article class="panel span-2">
        <h2>Cluster sync</h2>
        <div class="rows">
          <div class="row"><span>Configured nodes</span><strong>{{.ClusterNodeCount}}</strong></div>
          <div class="row"><span>etcd sync</span><strong>{{.ClusterSyncState}}</strong></div>
          <div class="row"><span>Backend alignment</span><strong>{{.ClusterSyncAlignment}}</strong></div>
        </div>
      </article>
      <article class="panel span-2">
        <h2>Running configuration</h2>
        <pre class="config">{{.RunningConfig}}</pre>
      </article>
    </section>

    <footer>
      <span>Generated {{.GeneratedAt}}</span>
      <span>/api/status | /api/config</span>
    </footer>
  </main>
</body>
</html>
`))

func effectiveWebListen(flagValue string, snapshot *model.ConfigSnapshot) string {
	if listen := strings.TrimSpace(flagValue); listen != "" {
		return listen
	}
	if snapshot == nil || snapshot.Config == nil || snapshot.Config.System == nil ||
		snapshot.Config.System.Services == nil || snapshot.Config.System.Services.WebUI == nil {
		return ""
	}
	web := snapshot.Config.System.Services.WebUI
	if !web.Enabled {
		return ""
	}
	addr := strings.TrimSpace(web.ListenAddress)
	if addr == "" {
		addr = "127.0.0.1"
	}
	port := web.Port
	if port == 0 {
		port = defaultWebUIPort
	}
	return net.JoinHostPort(addr, strconv.Itoa(port))
}

func startWebServer(ctx context.Context, listenAddr string, source metricsSource, log *logger.Logger) (<-chan error, error) {
	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, fmt.Errorf("listen web endpoint: %w", err)
	}

	srv := &http.Server{
		Handler:           newWebMux(source),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("Web endpoint started", slog.String("listen", lis.Addr().String()))
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
			log.Error("Web endpoint shutdown failed", slog.Any("error", err))
		}
	}()

	return errCh, nil
}

func newWebMux(source metricsSource) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", source.handleWebIndex)
	mux.HandleFunc("/api/config", source.handleWebConfig)
	mux.HandleFunc("/api/status", source.handleWebStatus)
	return mux
}

func (s metricsSource) handleWebStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(newWebStatus(s.snapshot(time.Now()))); err != nil {
		http.Error(w, "encode status: "+err.Error(), http.StatusInternalServerError)
	}
}

func (s metricsSource) handleWebConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg, err := s.runningConfig()
	if err != nil {
		http.Error(w, "render config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(cfg); err != nil {
		http.Error(w, "encode config: "+err.Error(), http.StatusInternalServerError)
	}
}

func (s metricsSource) handleWebIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	status := newWebStatus(s.snapshot(time.Now()))
	cfg, err := s.runningConfig()
	if err != nil {
		http.Error(w, "render config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := webIndexTemplate.Execute(w, newWebIndexData(status, time.Now(), cfg.ConfigText)); err != nil {
		http.Error(w, "render index: "+err.Error(), http.StatusInternalServerError)
	}
}

func (s metricsSource) runningConfig() (webConfig, error) {
	if s.engine == nil {
		return webConfig{}, nil
	}
	snap := s.engine.RunningSnapshot()
	if snap == nil || snap.Config == nil {
		return webConfig{}, nil
	}
	text, err := pkgconfig.ToSetCommandsWithError(snap.Config.ToLegacyConfig())
	if err != nil {
		return webConfig{}, fmt.Errorf("serialize running config: %w", err)
	}
	return webConfig{
		ConfigText: text,
		Version:    snap.Version,
	}, nil
}

func newWebStatus(metrics routerMetrics) webStatus {
	return webStatus{
		Version:         Version,
		Commit:          Commit,
		BuildDate:       BuildDate,
		UptimeSeconds:   metrics.UptimeSeconds,
		ConfigVersion:   metrics.ConfigVersion,
		RunningHostname: metrics.RunningHostname,
		Datastore: webDatastore{
			Backend:       metrics.DatastoreBackend,
			EtcdEndpoints: metrics.DatastoreEtcdEndpoints,
		},
		Cluster: webCluster{
			Enabled:            metrics.ClusterEnabled,
			NodeCount:          metrics.ClusterNodeCount,
			EtcdSyncConfigured: metrics.ClusterEtcdSync,
			EtcdEndpoints:      metrics.ClusterEtcdEndpoints,
			SyncAligned:        metrics.ClusterSyncAligned,
		},
		NETCONF: webNETCONFStats{
			Listening:         metrics.NETCONFListening,
			ActiveSessions:    metrics.NETCONFActiveSessions,
			ActiveConnections: metrics.NETCONFActiveConns,
			TotalConnections:  metrics.NETCONFTotalConns,
			SuccessfulAuth:    metrics.NETCONFSuccess,
			FailedAuth:        metrics.NETCONFFailures,
		},
	}
}

func newWebIndexData(status webStatus, now time.Time, runningConfig string) webIndexData {
	state := "Stopped"
	stateClass := "warn"
	if status.NETCONF.Listening {
		state = "Listening"
		stateClass = "ok"
	}
	clusterState := "Disabled"
	clusterStateClass := "neutral"
	if status.Cluster.Enabled {
		clusterState = "Enabled"
		clusterStateClass = "ok"
	}
	clusterSyncState := "Not configured"
	clusterSyncAlignment := "Not applicable"
	if status.Cluster.EtcdSyncConfigured {
		clusterSyncState = "etcd"
		clusterSyncAlignment = "Aligned"
		if !status.Cluster.SyncAligned {
			clusterSyncAlignment = "Mismatch"
		}
	}

	return webIndexData{
		Status:               status,
		Uptime:               formatWebUptime(status.UptimeSeconds),
		NETCONFState:         state,
		NETCONFStateClass:    stateClass,
		NETCONFConnections:   strconv.FormatUint(status.NETCONF.TotalConnections, 10),
		ClusterState:         clusterState,
		ClusterStateClass:    clusterStateClass,
		ClusterSyncState:     clusterSyncState,
		ClusterSyncAlignment: clusterSyncAlignment,
		ClusterNodeCount:     strconv.Itoa(status.Cluster.NodeCount),
		DatastoreBackend:     status.Datastore.Backend,
		GeneratedAt:          now.UTC().Format(time.RFC3339),
		ConfigVersionString:  strconv.FormatUint(status.ConfigVersion, 10),
		RunningConfig:        runningConfig,
	}
}

func formatWebUptime(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	duration := time.Duration(seconds) * time.Second
	days := duration / (24 * time.Hour)
	duration -= days * 24 * time.Hour
	hours := duration / time.Hour
	duration -= hours * time.Hour
	minutes := duration / time.Minute

	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
