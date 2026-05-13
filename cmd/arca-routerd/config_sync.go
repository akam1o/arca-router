package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	internalstore "github.com/akam1o/arca-router/internal/store"
	"github.com/akam1o/arca-router/pkg/datastore"
)

const defaultEtcdConfigSyncInterval = 2 * time.Second

type configSyncRuntimeSource interface {
	ConfigSyncStatus() configSyncStatus
}

type configSyncStatus struct {
	Enabled          bool
	Healthy          bool
	EtcdRevision     int64
	RunningRevision  int64
	RunningCommitID  string
	RunningTimestamp time.Time
	LastCheck        time.Time
	LastApply        time.Time
	LastError        string
}

type etcdConfigSynchronizer struct {
	store    internalstore.ConfigStore
	engine   *engine.Engine
	etcd     datastore.EtcdStatusProvider
	interval time.Duration
	log      *slog.Logger

	mu                  sync.RWMutex
	status              configSyncStatus
	lastRunningRevision int64
}

func newEtcdConfigSynchronizer(
	store internalstore.ConfigStore,
	engine *engine.Engine,
	etcd datastore.EtcdStatusProvider,
	interval time.Duration,
	log *slog.Logger,
) *etcdConfigSynchronizer {
	if interval <= 0 {
		interval = defaultEtcdConfigSyncInterval
	}
	if log == nil {
		log = slog.Default()
	}
	return &etcdConfigSynchronizer{
		store:    store,
		engine:   engine,
		etcd:     etcd,
		interval: interval,
		log:      log,
		status:   configSyncStatus{Enabled: true},
	}
}

func (s *etcdConfigSynchronizer) Start(ctx context.Context) {
	if err := s.reconcile(ctx); err != nil {
		s.log.Warn("Initial etcd config synchronization failed", slog.Any("error", err))
	}
	go s.run(ctx)
}

func (s *etcdConfigSynchronizer) ConfigSyncStatus() configSyncStatus {
	if s == nil {
		return configSyncStatus{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *etcdConfigSynchronizer) run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.reconcile(ctx); err != nil {
				s.log.Warn("etcd config synchronization failed", slog.Any("error", err))
			}
		}
	}
}

func (s *etcdConfigSynchronizer) reconcile(ctx context.Context) error {
	if s == nil || s.store == nil || s.engine == nil || s.etcd == nil {
		return fmt.Errorf("etcd config synchronizer is not fully configured")
	}

	now := time.Now()
	runtime := s.ConfigSyncStatus()
	runtime.Enabled = true
	runtime.LastCheck = now

	status, err := s.etcd.EtcdStatus(ctx)
	if err != nil {
		runtime.Healthy = false
		runtime.LastError = err.Error()
		s.setStatus(runtime, 0, false)
		return err
	}
	runtime.Healthy = true
	runtime.LastError = ""
	runtime.EtcdRevision = status.Revision
	runtime.RunningRevision = status.RunningRevision
	runtime.RunningCommitID = status.RunningCommitID
	runtime.RunningTimestamp = status.RunningTimestamp

	if status.RunningRevision == 0 || status.RunningRevision == s.seenRunningRevision() {
		s.setStatus(runtime, 0, false)
		return nil
	}

	snap, err := s.store.GetLatestSnapshot(ctx)
	if err != nil {
		runtime.Healthy = false
		runtime.LastError = fmt.Sprintf("load latest running config: %v", err)
		s.setStatus(runtime, 0, false)
		return err
	}
	if snap == nil || snap.Config == nil {
		err := fmt.Errorf("running config revision %d has no snapshot", status.RunningRevision)
		runtime.Healthy = false
		runtime.LastError = err.Error()
		s.setStatus(runtime, 0, false)
		return err
	}

	if shouldApplySyncedSnapshot(s.engine.RunningSnapshot(), snap) {
		if err := s.engine.Apply(ctx, snap.Config, "config-sync", "sync running config from etcd"); err != nil {
			runtime.Healthy = false
			runtime.LastError = fmt.Sprintf("apply synced running config: %v", err)
			s.setStatus(runtime, 0, false)
			return err
		}
		runtime.LastApply = now
		s.log.Info("Applied running configuration from etcd",
			slog.Int64("running_revision", status.RunningRevision),
			slog.String("commit_id", status.RunningCommitID),
		)
	}

	s.setStatus(runtime, status.RunningRevision, true)
	return nil
}

func (s *etcdConfigSynchronizer) seenRunningRevision() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastRunningRevision
}

func (s *etcdConfigSynchronizer) setStatus(status configSyncStatus, runningRevision int64, updateRevision bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
	if updateRevision {
		s.lastRunningRevision = runningRevision
	}
}

func shouldApplySyncedSnapshot(current, next *model.ConfigSnapshot) bool {
	if next == nil || next.Config == nil {
		return false
	}
	if current == nil || current.Config == nil {
		return true
	}
	return current.Hash != next.Hash
}
