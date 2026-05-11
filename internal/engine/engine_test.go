package engine

import (
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
