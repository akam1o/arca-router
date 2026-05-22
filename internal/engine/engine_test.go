package engine

import (
	"context"
	"errors"
	"log/slog"
	"strings"
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

func TestValidateClassifiesModelValidationErrors(t *testing.T) {
	eng := NewEngine(nil, slog.Default())
	cfg := model.NewRouterConfig()
	cfg.System = &model.SystemConfig{
		Services: &model.SystemServicesConfig{
			WebUI: &model.WebUIConfig{
				Enabled:       true,
				ListenAddress: "not an address",
			},
		},
	}

	err := eng.Validate(context.Background(), cfg)
	if err == nil {
		t.Fatal("Validate() error = nil, want validation error")
	}
	if !errors.Is(err, ErrConfigValidation) {
		t.Fatalf("Validate() error = %v, want ErrConfigValidation", err)
	}
	if !strings.Contains(err.Error(), "config validation failed:") {
		t.Fatalf("Validate() error = %v, want existing validation prefix", err)
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

func TestApplyErrorReportsRollbackSucceeded(t *testing.T) {
	first := &scriptedPlugin{name: "first"}
	second := &scriptedPlugin{name: "second", applyErr: errors.New("apply boom")}
	eng := NewEngine([]Plugin{first, second}, slog.Default())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 1)
	candidate := &model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router2"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}

	err := eng.Apply(context.Background(), candidate, "alice", "test")
	var applyErr *ApplyError
	if !errors.As(err, &applyErr) {
		t.Fatalf("Apply() error = %T %v, want ApplyError", err, err)
	}
	if applyErr.Plugin != "second" || applyErr.Phase != "apply" || !applyErr.RollbackAttempted || !applyErr.RollbackSucceeded {
		t.Fatalf("ApplyError = %#v, want second apply with successful rollback", applyErr)
	}
	if len(applyErr.RollbackDiagnostics) != 0 {
		t.Fatalf("rollback diagnostics = %#v, want none", applyErr.RollbackDiagnostics)
	}
	if !strings.Contains(err.Error(), "rollback succeeded") || !strings.Contains(err.Error(), "apply boom") {
		t.Fatalf("Apply() error = %v, want rollback success and apply cause", err)
	}
	if first.rollbackCalls != 1 || second.rollbackCalls != 0 {
		t.Fatalf("rollback calls first/second = %d/%d, want 1/0", first.rollbackCalls, second.rollbackCalls)
	}
	if got := eng.Running().System.HostName; got != "router1" {
		t.Fatalf("engine running hostname = %q, want unchanged router1", got)
	}
}

func TestApplyErrorReportsRollbackFailure(t *testing.T) {
	first := &scriptedPlugin{name: "first", rollbackErr: errors.New("undo failed")}
	second := &scriptedPlugin{name: "second", applyErr: errors.New("apply boom")}
	eng := NewEngine([]Plugin{first, second}, slog.Default())
	eng.InitializeRunning(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 1)
	candidate := &model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router2"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}

	err := eng.Apply(context.Background(), candidate, "alice", "test")
	var applyErr *ApplyError
	if !errors.As(err, &applyErr) {
		t.Fatalf("Apply() error = %T %v, want ApplyError", err, err)
	}
	if applyErr.RollbackSucceeded || len(applyErr.RollbackDiagnostics) != 1 {
		t.Fatalf("ApplyError = %#v, want failed rollback diagnostic", applyErr)
	}
	if !strings.Contains(applyErr.RollbackDiagnostics[0], "plugin first rollback failed: undo failed") {
		t.Fatalf("rollback diagnostics = %#v, want plugin failure detail", applyErr.RollbackDiagnostics)
	}
	if !strings.Contains(err.Error(), "rollback failed") || !strings.Contains(err.Error(), "undo failed") {
		t.Fatalf("Apply() error = %v, want rollback failure detail", err)
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

type scriptedPlugin struct {
	name          string
	validateErr   error
	applyErr      error
	rollbackErr   error
	validateCalls int
	applyCalls    int
	rollbackCalls int
}

func (p *scriptedPlugin) Name() string { return p.name }

func (p *scriptedPlugin) Init(context.Context) error { return nil }

func (p *scriptedPlugin) Close() error { return nil }

func (p *scriptedPlugin) HealthCheck(context.Context) error { return nil }

func (p *scriptedPlugin) ValidateChanges(context.Context, *ConfigDiff) error {
	p.validateCalls++
	return p.validateErr
}

func (p *scriptedPlugin) ApplyChanges(context.Context, *ConfigDiff) error {
	p.applyCalls++
	return p.applyErr
}

func (p *scriptedPlugin) RollbackChanges(context.Context, *ConfigDiff) error {
	p.rollbackCalls++
	return p.rollbackErr
}
