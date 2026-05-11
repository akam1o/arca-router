package sqlite

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akam1o/arca-router/internal/model"
	pkgconfig "github.com/akam1o/arca-router/pkg/config"
	"github.com/akam1o/arca-router/pkg/datastore"
)

func TestGetLatestSnapshotReturnsNilWhenRunningConfigMissing(t *testing.T) {
	st, err := NewFromPath(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatalf("NewFromPath() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	snap, err := st.GetLatestSnapshot(context.Background())
	if err != nil {
		t.Fatalf("GetLatestSnapshot() error = %v", err)
	}
	if snap != nil {
		t.Fatalf("GetLatestSnapshot() = %#v, want nil", snap)
	}
}

func TestSaveCommitStoresSetCommands(t *testing.T) {
	installLegacyTextParser(t)

	st, err := NewFromPath(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatalf("NewFromPath() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	snap := model.NewSnapshot(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 1, "alice", "test")
	if _, err := st.SaveCommit(context.Background(), snap); err != nil {
		t.Fatalf("SaveCommit() error = %v", err)
	}

	running, err := st.Legacy().GetRunning(context.Background())
	if err != nil {
		t.Fatalf("GetRunning() error = %v", err)
	}
	if strings.HasPrefix(strings.TrimSpace(running.ConfigText), "{") {
		t.Fatalf("running config stored JSON, want set commands: %q", running.ConfigText)
	}
	if !strings.Contains(running.ConfigText, "set system host-name router1") {
		t.Fatalf("running config = %q, want hostname set command", running.ConfigText)
	}
	if _, err := pkgconfig.NewParser(strings.NewReader(running.ConfigText)).Parse(); err != nil {
		t.Fatalf("stored running config is not parseable set commands: %v", err)
	}

	latest, err := st.GetLatestSnapshot(context.Background())
	if err != nil {
		t.Fatalf("GetLatestSnapshot() error = %v", err)
	}
	if latest == nil || latest.Config == nil || latest.Config.System == nil || latest.Config.System.HostName != "router1" {
		t.Fatalf("latest snapshot = %#v, want router1 config", latest)
	}

	commit, err := st.GetCommit(context.Background(), running.CommitID)
	if err != nil {
		t.Fatalf("GetCommit() error = %v", err)
	}
	if commit == nil || commit.Config == nil || commit.Config.System == nil || commit.Config.System.HostName != "router1" {
		t.Fatalf("commit = %#v, want parsed router1 config", commit)
	}
}

func TestSaveCommitPreservesOSPFPriorityZero(t *testing.T) {
	installLegacyTextParser(t)

	st, err := NewFromPath(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatalf("NewFromPath() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	priority := 0
	snap := model.NewSnapshot(&model.RouterConfig{
		Interfaces: map[string]*model.InterfaceConfig{},
		Protocols: &model.ProtocolsConfig{
			OSPF: &model.OSPFConfig{
				Areas: map[string]*model.OSPFArea{
					"0.0.0.0": {
						Interfaces: map[string]*model.OSPFInterface{
							"ge-0/0/0": {Priority: &priority},
						},
					},
				},
			},
		},
	}, 1, "alice", "test")
	if _, err := st.SaveCommit(context.Background(), snap); err != nil {
		t.Fatalf("SaveCommit() error = %v", err)
	}

	running, err := st.Legacy().GetRunning(context.Background())
	if err != nil {
		t.Fatalf("GetRunning() error = %v", err)
	}
	want := "set protocols ospf area 0.0.0.0 interface ge-0/0/0 priority 0"
	if !strings.Contains(running.ConfigText, want) {
		t.Fatalf("running config = %q, want %q", running.ConfigText, want)
	}

	latest, err := st.GetLatestSnapshot(context.Background())
	if err != nil {
		t.Fatalf("GetLatestSnapshot() error = %v", err)
	}
	got := latest.Config.Protocols.OSPF.Areas["0.0.0.0"].Interfaces["ge-0/0/0"].Priority
	if got == nil || *got != 0 {
		t.Fatalf("latest OSPF priority = %v, want explicit 0", got)
	}
}

func TestPrepareRollbackConflictsWithExistingDatastoreLock(t *testing.T) {
	installLegacyTextParser(t)

	st, err := NewFromPath(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatalf("NewFromPath() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	first := model.NewSnapshot(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 1, "alice", "first")
	firstID, err := st.SaveCommit(context.Background(), first)
	if err != nil {
		t.Fatalf("SaveCommit(first) error = %v", err)
	}

	second := model.NewSnapshot(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router2"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 2, "alice", "second")
	if _, err := st.SaveCommit(context.Background(), second); err != nil {
		t.Fatalf("SaveCommit(second) error = %v", err)
	}

	if err := st.Legacy().AcquireLock(context.Background(), &datastore.LockRequest{
		Target:    datastore.LockTargetCandidate,
		SessionID: "netconf-session",
		User:      "bob",
	}); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}

	_, err = st.PrepareRollback(context.Background(), first, firstID)
	if err == nil {
		t.Fatal("PrepareRollback() expected lock conflict")
	}
}

func TestPrepareRollbackPersistsRollbackHistory(t *testing.T) {
	installLegacyTextParser(t)

	st, err := NewFromPath(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatalf("NewFromPath() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	first := model.NewSnapshot(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 1, "alice", "first")
	firstID, err := st.SaveCommit(context.Background(), first)
	if err != nil {
		t.Fatalf("SaveCommit(first) error = %v", err)
	}

	second := model.NewSnapshot(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router2"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 2, "alice", "second")
	if _, err := st.SaveCommit(context.Background(), second); err != nil {
		t.Fatalf("SaveCommit(second) error = %v", err)
	}

	prepared, err := st.PrepareRollback(context.Background(), first, firstID)
	if err != nil {
		t.Fatalf("PrepareRollback() error = %v", err)
	}
	rollbackID, err := prepared.Commit(context.Background())
	if err != nil {
		t.Fatalf("prepared rollback Commit() error = %v", err)
	}

	running, err := st.Legacy().GetRunning(context.Background())
	if err != nil {
		t.Fatalf("GetRunning() error = %v", err)
	}
	if !strings.Contains(running.ConfigText, "set system host-name router1") {
		t.Fatalf("running config = %q, want rolled back router1", running.ConfigText)
	}

	rollback, err := st.Legacy().GetCommit(context.Background(), rollbackID)
	if err != nil {
		t.Fatalf("GetCommit(rollback) error = %v", err)
	}
	if !rollback.IsRollback {
		t.Fatalf("rollback commit IsRollback = false, want true")
	}

	lockInfo, err := st.Legacy().GetLockInfo(context.Background(), datastore.LockTargetCandidate)
	if err != nil {
		t.Fatalf("GetLockInfo() error = %v", err)
	}
	if lockInfo.IsLocked {
		t.Fatalf("candidate lock is still held by %q", lockInfo.SessionID)
	}
}

func installLegacyTextParser(t *testing.T) {
	t.Helper()
	oldParser := LegacyTextParser
	LegacyTextParser = func(text string) (*model.RouterConfig, error) {
		cfg, err := pkgconfig.NewParser(strings.NewReader(text)).Parse()
		if err != nil {
			return nil, err
		}
		return model.FromLegacyConfig(cfg), nil
	}
	t.Cleanup(func() { LegacyTextParser = oldParser })
}
