package sqlite

import (
	"context"
	"errors"
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

func TestCleanupEphemeralStateRemovesLocksAndCandidates(t *testing.T) {
	st, err := NewFromPath(filepath.Join(t.TempDir(), "config.db"))
	if err != nil {
		t.Fatalf("NewFromPath() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()

	snap := model.NewSnapshot(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router1"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 1, "alice", "test")
	if _, err := st.SaveCommit(ctx, snap); err != nil {
		t.Fatalf("SaveCommit() error = %v", err)
	}
	if err := st.Legacy().AcquireLock(ctx, &datastore.LockRequest{
		Target:    datastore.LockTargetCandidate,
		SessionID: "stale-candidate",
		User:      "alice",
	}); err != nil {
		t.Fatalf("AcquireLock(candidate) error = %v", err)
	}
	if err := st.Legacy().SaveCandidate(ctx, "stale-candidate", "set system host-name stale\n"); err != nil {
		t.Fatalf("SaveCandidate() error = %v", err)
	}
	if err := st.Legacy().AcquireLock(ctx, &datastore.LockRequest{
		Target:    datastore.LockTargetRunning,
		SessionID: "stale-running",
		User:      "alice",
	}); err != nil {
		t.Fatalf("AcquireLock(running) error = %v", err)
	}

	if err := st.CleanupEphemeralState(ctx); err != nil {
		t.Fatalf("CleanupEphemeralState() error = %v", err)
	}
	for _, target := range []string{datastore.LockTargetCandidate, datastore.LockTargetRunning} {
		info, err := st.Legacy().GetLockInfo(ctx, target)
		if err != nil {
			t.Fatalf("GetLockInfo(%s) error = %v", target, err)
		}
		if info.IsLocked {
			t.Fatalf("%s lock still held by %q", target, info.SessionID)
		}
	}
	if _, err := st.Legacy().GetCandidate(ctx, "stale-candidate"); !isDatastoreNotFound(err) {
		t.Fatalf("GetCandidate(stale-candidate) error = %v, want not found", err)
	}
	running, err := st.Legacy().GetRunning(ctx)
	if err != nil {
		t.Fatalf("GetRunning() error = %v", err)
	}
	if !strings.Contains(running.ConfigText, "set system host-name router1") {
		t.Fatalf("running config = %q, want original router1 config", running.ConfigText)
	}

	prepared, err := st.PrepareCommit(ctx, model.NewSnapshot(&model.RouterConfig{
		System:     &model.SystemConfig{HostName: "router2"},
		Interfaces: map[string]*model.InterfaceConfig{},
	}, 2, "alice", "after cleanup"))
	if err != nil {
		t.Fatalf("PrepareCommit() after cleanup error = %v", err)
	}
	if _, err := prepared.Commit(ctx); err != nil {
		t.Fatalf("prepared Commit() after cleanup error = %v", err)
	}
}

func TestSaveCommitStoresSetCommands(t *testing.T) {
	st, err := NewFromPath(filepath.Join(t.TempDir(), "config.db"), testLegacyTextParserOption())
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
	if latest.Version == 0 {
		t.Fatal("latest snapshot version = 0, want initialized version")
	}
	if latest.Hash == ([32]byte{}) {
		t.Fatal("latest snapshot hash is empty")
	}

	commit, err := st.GetCommit(context.Background(), running.CommitID)
	if err != nil {
		t.Fatalf("GetCommit() error = %v", err)
	}
	if commit == nil || commit.Config == nil || commit.Config.System == nil || commit.Config.System.HostName != "router1" {
		t.Fatalf("commit = %#v, want parsed router1 config", commit)
	}
}

func TestGetLatestSnapshotRestoresVersionFromCommitHistory(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "config.db")
	st, err := NewFromPath(dbPath, testLegacyTextParserOption())
	if err != nil {
		t.Fatalf("NewFromPath() error = %v", err)
	}

	saveSnapshot := func(st *Store, version uint64, hostName, message string) {
		t.Helper()
		snap := model.NewSnapshot(&model.RouterConfig{
			System:     &model.SystemConfig{HostName: hostName},
			Interfaces: map[string]*model.InterfaceConfig{},
		}, version, "alice", message)
		if _, err := st.SaveCommit(ctx, snap); err != nil {
			t.Fatalf("SaveCommit(%s) error = %v", hostName, err)
		}
	}

	saveSnapshot(st, 1, "router1", "first")
	saveSnapshot(st, 2, "router2", "second")
	if err := st.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := NewFromPath(dbPath, testLegacyTextParserOption())
	if err != nil {
		t.Fatalf("NewFromPath(reopen) error = %v", err)
	}
	latest, err := reopened.GetLatestSnapshot(ctx)
	if err != nil {
		t.Fatalf("GetLatestSnapshot() after reopen error = %v", err)
	}
	if latest == nil || latest.Config == nil || latest.Config.System == nil {
		t.Fatalf("latest snapshot after reopen = %#v, want config", latest)
	}
	if latest.Version != 2 {
		t.Fatalf("latest snapshot version after reopen = %d, want 2", latest.Version)
	}
	if latest.Config.System.HostName != "router2" {
		t.Fatalf("latest hostname after reopen = %q, want router2", latest.Config.System.HostName)
	}

	saveSnapshot(reopened, latest.Version+1, "router3", "third")
	if err := reopened.Close(); err != nil {
		t.Fatalf("Close(reopened) error = %v", err)
	}

	reopenedAgain, err := NewFromPath(dbPath, testLegacyTextParserOption())
	if err != nil {
		t.Fatalf("NewFromPath(reopen again) error = %v", err)
	}
	t.Cleanup(func() { _ = reopenedAgain.Close() })
	latestAgain, err := reopenedAgain.GetLatestSnapshot(ctx)
	if err != nil {
		t.Fatalf("GetLatestSnapshot() after second reopen error = %v", err)
	}
	if latestAgain == nil || latestAgain.Config == nil || latestAgain.Config.System == nil {
		t.Fatalf("latest snapshot after second reopen = %#v, want config", latestAgain)
	}
	if latestAgain.Version != 3 {
		t.Fatalf("latest snapshot version after second reopen = %d, want 3", latestAgain.Version)
	}
	if latestAgain.Config.System.HostName != "router3" {
		t.Fatalf("latest hostname after second reopen = %q, want router3", latestAgain.Config.System.HostName)
	}
}

func TestStoreUsesInstanceLegacyTextParser(t *testing.T) {
	ctx := context.Background()
	newStore := func(hostName string) *Store {
		t.Helper()
		st, err := NewFromPath(filepath.Join(t.TempDir(), "config.db"), WithLegacyTextParser(
			func(text string) (*model.RouterConfig, error) {
				if text == "" {
					t.Fatal("legacy parser received empty config text")
				}
				return &model.RouterConfig{
					System:     &model.SystemConfig{HostName: hostName},
					Interfaces: map[string]*model.InterfaceConfig{},
				}, nil
			},
		))
		if err != nil {
			t.Fatalf("NewFromPath() error = %v", err)
		}
		t.Cleanup(func() { _ = st.Close() })
		snap := model.NewSnapshot(&model.RouterConfig{
			System:     &model.SystemConfig{HostName: "stored"},
			Interfaces: map[string]*model.InterfaceConfig{},
		}, 1, "alice", "test")
		if _, err := st.SaveCommit(ctx, snap); err != nil {
			t.Fatalf("SaveCommit() error = %v", err)
		}
		return st
	}

	first := newStore("parser-one")
	second := newStore("parser-two")

	firstLatest, err := first.GetLatestSnapshot(ctx)
	if err != nil {
		t.Fatalf("first GetLatestSnapshot() error = %v", err)
	}
	secondLatest, err := second.GetLatestSnapshot(ctx)
	if err != nil {
		t.Fatalf("second GetLatestSnapshot() error = %v", err)
	}
	if firstLatest.Config.System.HostName != "parser-one" {
		t.Fatalf("first hostname = %q, want parser-one", firstLatest.Config.System.HostName)
	}
	if secondLatest.Config.System.HostName != "parser-two" {
		t.Fatalf("second hostname = %q, want parser-two", secondLatest.Config.System.HostName)
	}
}

func isDatastoreNotFound(err error) bool {
	var dsErr *datastore.Error
	return errors.As(err, &dsErr) && dsErr.Code == datastore.ErrCodeNotFound
}

func TestSaveCommitPreservesOSPFPriorityZero(t *testing.T) {
	st, err := NewFromPath(filepath.Join(t.TempDir(), "config.db"), testLegacyTextParserOption())
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
	st, err := NewFromPath(filepath.Join(t.TempDir(), "config.db"), testLegacyTextParserOption())
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
	st, err := NewFromPath(filepath.Join(t.TempDir(), "config.db"), testLegacyTextParserOption())
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

func testLegacyTextParserOption() Option {
	return WithLegacyTextParser(func(text string) (*model.RouterConfig, error) {
		cfg, err := pkgconfig.NewParser(strings.NewReader(text)).Parse()
		if err != nil {
			return nil, err
		}
		return model.FromLegacyConfig(cfg), nil
	})
}
