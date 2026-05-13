package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	"github.com/akam1o/arca-router/pkg/datastore"
)

type clusterSyncPlugin struct {
	datastoreConfig *datastore.Config
}

func newClusterSyncPlugin(datastoreConfig *datastore.Config) *clusterSyncPlugin {
	return &clusterSyncPlugin{datastoreConfig: datastoreConfig}
}

func (p *clusterSyncPlugin) Name() string { return "cluster-sync" }

func (p *clusterSyncPlugin) Init(ctx context.Context) error { return nil }

func (p *clusterSyncPlugin) Close() error { return nil }

func (p *clusterSyncPlugin) HealthCheck(ctx context.Context) error { return nil }

func (p *clusterSyncPlugin) ValidateChanges(ctx context.Context, diff *engine.ConfigDiff) error {
	if diff == nil || diff.NewConfig == nil {
		return nil
	}
	endpoints := clusterEtcdEndpoints(diff.NewConfig)
	if len(endpoints) == 0 {
		return nil
	}
	if p.datastoreConfig == nil {
		return fmt.Errorf("chassis cluster sync etcd requires daemon datastore configuration")
	}
	if p.datastoreConfig.Backend != datastore.BackendEtcd {
		return fmt.Errorf("chassis cluster sync etcd requires --datastore-backend=etcd (current: %s)", p.datastoreConfig.Backend)
	}

	configured := normalizedEndpoints(endpoints)
	daemon := normalizedEndpoints(p.datastoreConfig.EtcdEndpoints)
	if !sameEndpoints(configured, daemon) {
		return fmt.Errorf("chassis cluster sync etcd endpoints %s do not match daemon --etcd-endpoints %s",
			strings.Join(configured, ","), strings.Join(daemon, ","))
	}
	return nil
}

func (p *clusterSyncPlugin) ApplyChanges(ctx context.Context, diff *engine.ConfigDiff) error {
	return nil
}

func (p *clusterSyncPlugin) RollbackChanges(ctx context.Context, diff *engine.ConfigDiff) error {
	return nil
}

func clusterEtcdEndpoints(cfg *model.RouterConfig) []string {
	if cfg == nil || cfg.Chassis == nil || cfg.Chassis.Cluster == nil || !cfg.Chassis.Cluster.Enabled ||
		cfg.Chassis.Cluster.Sync == nil || cfg.Chassis.Cluster.Sync.Etcd == nil {
		return nil
	}
	return cfg.Chassis.Cluster.Sync.Etcd.Endpoints
}

func normalizedEndpoints(endpoints []string) []string {
	normalized := make([]string, 0, len(endpoints))
	seen := make(map[string]struct{}, len(endpoints))
	for _, endpoint := range endpoints {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" {
			continue
		}
		if _, ok := seen[endpoint]; ok {
			continue
		}
		seen[endpoint] = struct{}{}
		normalized = append(normalized, endpoint)
	}
	sort.Strings(normalized)
	return normalized
}

func sameEndpoints(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
