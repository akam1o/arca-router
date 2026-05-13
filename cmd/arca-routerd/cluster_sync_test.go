package main

import (
	"context"
	"strings"
	"testing"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	"github.com/akam1o/arca-router/pkg/datastore"
)

func TestClusterSyncPluginRejectsSQLiteDatastore(t *testing.T) {
	err := newClusterSyncPlugin(&datastore.Config{Backend: datastore.BackendSQLite}).
		ValidateChanges(context.Background(), engine.ComputeDiff(model.NewRouterConfig(), clusterSyncConfig(
			"http://127.0.0.1:2379",
		)))
	if err == nil {
		t.Fatal("ValidateChanges() error = nil, want datastore backend error")
	}
	if !strings.Contains(err.Error(), "--datastore-backend=etcd") {
		t.Fatalf("ValidateChanges() error = %v, want datastore backend hint", err)
	}
}

func TestClusterSyncPluginRejectsEndpointMismatch(t *testing.T) {
	err := newClusterSyncPlugin(&datastore.Config{
		Backend:       datastore.BackendEtcd,
		EtcdEndpoints: []string{"https://etcd1:2379"},
	}).ValidateChanges(context.Background(), engine.ComputeDiff(model.NewRouterConfig(), clusterSyncConfig(
		"https://etcd2:2379",
	)))
	if err == nil {
		t.Fatal("ValidateChanges() error = nil, want endpoint mismatch")
	}
	if !strings.Contains(err.Error(), "do not match") {
		t.Fatalf("ValidateChanges() error = %v, want endpoint mismatch", err)
	}
}

func TestClusterSyncPluginAllowsMatchingEtcdDatastore(t *testing.T) {
	err := newClusterSyncPlugin(&datastore.Config{
		Backend: datastore.BackendEtcd,
		EtcdEndpoints: []string{
			"https://etcd2:2379",
			"https://etcd1:2379",
		},
	}).ValidateChanges(context.Background(), engine.ComputeDiff(model.NewRouterConfig(), clusterSyncConfig(
		"https://etcd1:2379",
		"https://etcd2:2379",
	)))
	if err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}

func TestClusterSyncPluginAllowsClusterWithoutSyncEndpoints(t *testing.T) {
	cfg := model.NewRouterConfig()
	cfg.Chassis = &model.ChassisConfig{
		Cluster: &model.ClusterConfig{Enabled: true},
	}

	err := newClusterSyncPlugin(&datastore.Config{Backend: datastore.BackendSQLite}).
		ValidateChanges(context.Background(), engine.ComputeDiff(model.NewRouterConfig(), cfg))
	if err != nil {
		t.Fatalf("ValidateChanges() error = %v, want nil", err)
	}
}

func clusterSyncConfig(endpoints ...string) *model.RouterConfig {
	cfg := model.NewRouterConfig()
	cfg.Chassis = &model.ChassisConfig{
		Cluster: &model.ClusterConfig{
			Enabled: true,
			Sync: &model.ClusterSyncConfig{
				Etcd: &model.EtcdSyncConfig{Endpoints: endpoints},
			},
		},
	}
	return cfg
}
