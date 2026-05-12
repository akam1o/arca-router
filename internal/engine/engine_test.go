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

func TestRunningSnapshotReturnsNilBeforeInitialize(t *testing.T) {
	eng := NewEngine(nil, slog.Default())
	if snap := eng.RunningSnapshot(); snap != nil {
		t.Fatalf("RunningSnapshot() = %#v, want nil", snap)
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

func TestValidateDiffIsIsolatedBetweenPlugins(t *testing.T) {
	recorder := &recordingDiffPlugin{}
	eng := NewEngine([]Plugin{&mutatingDiffPlugin{}, recorder}, slog.Default())
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

	if recorder.validateOldHost != "router1" || recorder.validateNewHost != "router2" {
		t.Fatalf("recorder validate hosts = (%q, %q), want (router1, router2)",
			recorder.validateOldHost, recorder.validateNewHost)
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

func TestApplyDiffIsIsolatedBetweenPlugins(t *testing.T) {
	recorder := &recordingDiffPlugin{}
	eng := NewEngine([]Plugin{&mutatingDiffPlugin{}, recorder}, slog.Default())
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

	if recorder.validateOldHost != "router1" || recorder.validateNewHost != "router2" {
		t.Fatalf("recorder validate hosts = (%q, %q), want (router1, router2)",
			recorder.validateOldHost, recorder.validateNewHost)
	}
	if recorder.applyOldHost != "router1" || recorder.applyNewHost != "router2" {
		t.Fatalf("recorder apply hosts = (%q, %q), want (router1, router2)",
			recorder.applyOldHost, recorder.applyNewHost)
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

type recordingDiffPlugin struct {
	validateOldHost string
	validateNewHost string
	applyOldHost    string
	applyNewHost    string
}

func (p *recordingDiffPlugin) Name() string { return "recording" }

func (p *recordingDiffPlugin) Init(ctx context.Context) error { return nil }

func (p *recordingDiffPlugin) Close() error { return nil }

func (p *recordingDiffPlugin) HealthCheck(ctx context.Context) error { return nil }

func (p *recordingDiffPlugin) ValidateChanges(ctx context.Context, diff *ConfigDiff) error {
	p.validateOldHost, p.validateNewHost = diffHostnames(diff)
	return nil
}

func (p *recordingDiffPlugin) ApplyChanges(ctx context.Context, diff *ConfigDiff) error {
	p.applyOldHost, p.applyNewHost = diffHostnames(diff)
	return nil
}

func (p *recordingDiffPlugin) RollbackChanges(ctx context.Context, diff *ConfigDiff) error {
	return nil
}

func diffHostnames(diff *ConfigDiff) (string, string) {
	var oldHost, newHost string
	if diff.OldConfig != nil && diff.OldConfig.System != nil {
		oldHost = diff.OldConfig.System.HostName
	}
	if diff.NewConfig != nil && diff.NewConfig.System != nil {
		newHost = diff.NewConfig.System.HostName
	}
	return oldHost, newHost
}
