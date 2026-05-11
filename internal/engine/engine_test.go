package engine

import (
	"context"
	"log/slog"
	"testing"

	"github.com/akam1o/arca-router/internal/model"
)

func TestRunningReturnsCopy(t *testing.T) {
	eng := NewEngine(nil, slog.Default())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 1)

	running := eng.Running()
	running.System.HostName = "router2"

	if got := eng.Running().System.HostName; got != "router1" {
		t.Fatalf("engine running hostname = %q, want router1", got)
	}
}

func TestRunningSnapshotReturnsCopy(t *testing.T) {
	eng := NewEngine(nil, slog.Default())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 1)

	snap := eng.RunningSnapshot()
	snap.Config.System.HostName = "router2"

	if got := eng.RunningSnapshot().Config.System.HostName; got != "router1" {
		t.Fatalf("engine snapshot hostname = %q, want router1", got)
	}
}

func TestValidateDiffDoesNotExposeRunningOrCandidate(t *testing.T) {
	plugin := &mutatingDiffPlugin{}
	eng := NewEngine([]Plugin{plugin}, slog.Default())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 1)
	candidate := &model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router2"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}

	if err := eng.Validate(context.Background(), candidate); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if got := eng.Running().System.HostName; got != "router1" {
		t.Fatalf("engine running hostname = %q, want router1", got)
	}
	if got := candidate.System.HostName; got != "router2" {
		t.Fatalf("candidate hostname = %q, want router2", got)
	}
}

func TestApplyDiffDoesNotAffectCommittedSnapshot(t *testing.T) {
	plugin := &mutatingDiffPlugin{}
	eng := NewEngine([]Plugin{plugin}, slog.Default())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 1)
	candidate := &model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router2"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}

	if err := eng.Apply(context.Background(), candidate, "alice", "test"); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	if got := eng.Running().System.HostName; got != "router2" {
		t.Fatalf("engine running hostname = %q, want router2", got)
	}
	if got := candidate.System.HostName; got != "router2" {
		t.Fatalf("candidate hostname = %q, want router2", got)
	}
}

type mutatingDiffPlugin struct{}

func (p *mutatingDiffPlugin) Name() string { return "mutating" }

func (p *mutatingDiffPlugin) Init(ctx context.Context) error { return nil }

func (p *mutatingDiffPlugin) Close() error { return nil }

func (p *mutatingDiffPlugin) HealthCheck(ctx context.Context) error { return nil }

func (p *mutatingDiffPlugin) ValidateChanges(ctx context.Context, diff *ConfigDiff) error {
	mutateDiffConfig(diff)
	return nil
}

func (p *mutatingDiffPlugin) ApplyChanges(ctx context.Context, diff *ConfigDiff) error {
	mutateDiffConfig(diff)
	return nil
}

func (p *mutatingDiffPlugin) RollbackChanges(ctx context.Context, diff *ConfigDiff) error {
	return nil
}

func mutateDiffConfig(diff *ConfigDiff) {
	if diff.OldConfig != nil && diff.OldConfig.System != nil {
		diff.OldConfig.System.HostName = "mutated-old"
	}
	if diff.NewConfig != nil && diff.NewConfig.System != nil {
		diff.NewConfig.System.HostName = "mutated-new"
	}
}
