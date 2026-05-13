package main

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	sbfrr "github.com/akam1o/arca-router/internal/southbound/frr"
	sbvpp "github.com/akam1o/arca-router/internal/southbound/vpp"
	"github.com/akam1o/arca-router/pkg/datastore"
)

type fakeVPPReconciliationSource struct {
	status sbvpp.LCPReconciliationStatus
}

func (s fakeVPPReconciliationSource) LCPReconciliationStatus() sbvpp.LCPReconciliationStatus {
	return s.status
}

type fakeConfigSyncRuntimeSource struct {
	status configSyncStatus
}

func (s fakeConfigSyncRuntimeSource) ConfigSyncStatus() configSyncStatus {
	return s.status
}

type fakeFRRVRRPSource struct {
	status sbfrr.VRRPOperationalStatus
}

func (s fakeFRRVRRPSource) VRRPOperationalStatus() sbfrr.VRRPOperationalStatus {
	return s.status
}

func TestEffectiveMetricsListenUsesFlagOverride(t *testing.T) {
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{
		Services: &model.SystemServicesConfig{
			Prometheus: &model.PrometheusConfig{
				Enabled:       true,
				ListenAddress: "127.0.0.1",
				Port:          9090,
			},
		},
	}

	got := effectiveMetricsListen(":19090", model.NewSnapshot(cfg, 1, "test", "test"))
	if got != ":19090" {
		t.Fatalf("effectiveMetricsListen() = %q, want %q", got, ":19090")
	}
}

func TestEffectiveMetricsListenUsesConfig(t *testing.T) {
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{
		Services: &model.SystemServicesConfig{
			Prometheus: &model.PrometheusConfig{
				Enabled:       true,
				ListenAddress: "127.0.0.1",
				Port:          9090,
			},
		},
	}

	got := effectiveMetricsListen("", model.NewSnapshot(cfg, 1, "test", "test"))
	if got != "127.0.0.1:9090" {
		t.Fatalf("effectiveMetricsListen() = %q, want %q", got, "127.0.0.1:9090")
	}
}

func TestEffectiveMetricsListenUsesConfigDefaults(t *testing.T) {
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{
		Services: &model.SystemServicesConfig{
			Prometheus: &model.PrometheusConfig{Enabled: true},
		},
	}

	got := effectiveMetricsListen("", model.NewSnapshot(cfg, 1, "test", "test"))
	if got != "127.0.0.1:9090" {
		t.Fatalf("effectiveMetricsListen() = %q, want %q", got, "127.0.0.1:9090")
	}
}

func TestMetricsEndpointExportsRouterMetrics(t *testing.T) {
	eng := engine.NewEngine(nil, slog.Default())
	cfg := model.NewRouterConfig()
	cfg.Chassis = &model.ChassisConfig{
		Cluster: &model.ClusterConfig{
			Enabled: true,
			Nodes: map[string]*model.ClusterNode{
				"node0": {Address: "192.0.2.10"},
				"node1": {Address: "192.0.2.11"},
			},
			Sync: &model.ClusterSyncConfig{
				Etcd: &model.EtcdSyncConfig{Endpoints: []string{"https://etcd1:2379"}},
			},
		},
	}
	cfg.Protocols = &model.ProtocolsConfig{
		VRRP: &model.VRRPConfig{Groups: map[string]*model.VRRPGroup{
			"10": &model.VRRPGroup{Interface: "ge-0/0/0", VirtualAddress: "192.0.2.1", Priority: 110, Preempt: true},
		}},
	}
	cfg.ClassOfService = &model.ClassOfServiceConfig{
		ForwardingClasses: map[string]*model.ForwardingClass{
			"best-effort":          {Queue: 0},
			"expedited-forwarding": {Queue: 5},
		},
		TrafficControlProfiles: map[string]*model.TrafficControlProfile{
			"WAN": {ShapingRate: 1000000000, SchedulerMap: "WAN-SCHED"},
		},
		Interfaces: map[string]*model.CoSInterface{
			"ge-0/0/0": {OutputTrafficControlProfile: "WAN"},
		},
	}
	eng.InitializeRunning(cfg, 42)

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	metricsSource{
		startedAt: time.Now(),
		engine:    eng,
		datastore: &datastore.Config{
			Backend:       datastore.BackendEtcd,
			EtcdEndpoints: []string{"https://etcd1:2379"},
		},
		configSync: fakeConfigSyncRuntimeSource{status: configSyncStatus{
			Enabled:         true,
			Healthy:         true,
			EtcdRevision:    123,
			RunningRevision: 120,
			RunningCommitID: "commit-120",
			LastCheck:       time.Unix(1700000100, 0),
			LastApply:       time.Unix(1700000200, 0),
		}},
		frr: fakeFRRVRRPSource{status: sbfrr.VRRPOperationalStatus{
			LastRun:          time.Unix(1700000300, 0),
			ConfiguredGroups: 1,
			ObservedGroups:   1,
			ActiveGroups:     1,
		}},
		vpp: fakeVPPReconciliationSource{status: sbvpp.LCPReconciliationStatus{
			LastRun:         time.Unix(1700000000, 0),
			PairCount:       2,
			Inconsistencies: []string{"Interface 7 exists in VPP but not in cache"},
		}},
	}.handleMetrics(rec, req)

	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	text := string(body)
	for _, want := range []string{
		"arca_routerd_up 1",
		"arca_router_config_version 42",
		"arca_router_config_sync_etcd_enabled 1",
		"arca_router_config_sync_etcd_healthy 1",
		"arca_router_config_sync_etcd_revision 123",
		"arca_router_config_sync_running_revision 120",
		"arca_router_config_sync_error 0",
		"arca_router_config_sync_last_check_timestamp_seconds 1700000100",
		"arca_router_config_sync_last_apply_timestamp_seconds 1700000200",
		"arca_router_cluster_enabled 1",
		"arca_router_cluster_nodes 2",
		"arca_router_cluster_sync_etcd_configured 1",
		"arca_router_cluster_sync_aligned 1",
		"arca_router_ha_configured 1",
		"arca_router_ha_converged 0",
		"arca_router_ha_vrrp_groups 1",
		"arca_router_ha_convergence_issues 1",
		"arca_router_frr_vrrp_configured_groups 1",
		"arca_router_frr_vrrp_observed_groups 1",
		"arca_router_frr_vrrp_active_groups 1",
		"arca_router_frr_vrrp_issues 0",
		"arca_router_frr_vrrp_error 0",
		"arca_router_frr_vrrp_last_check_timestamp_seconds 1700000300",
		"arca_router_vpp_lcp_pairs 2",
		"arca_router_vpp_lcp_inconsistencies 1",
		"arca_router_vpp_lcp_reconcile_error 0",
		"arca_router_vpp_lcp_last_reconcile_timestamp_seconds 1700000000",
		"arca_router_class_of_service_configured 1",
		"arca_router_class_of_service_forwarding_classes 2",
		"arca_router_class_of_service_traffic_control_profiles 1",
		"arca_router_class_of_service_interface_bindings 1",
		"arca_router_class_of_service_intent_only 1",
		"arca_router_netconf_listening 0",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("/metrics missing %q:\n%s", want, text)
		}
	}
}
