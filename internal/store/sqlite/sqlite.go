// Package sqlite implements the ConfigStore interface using SQLite.
// It bridges to the existing pkg/datastore SQLite implementation during
// the migration period.
package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/akam1o/arca-router/internal/model"
	"github.com/akam1o/arca-router/internal/store"
	pkgconfig "github.com/akam1o/arca-router/pkg/config"
	"github.com/akam1o/arca-router/pkg/datastore"
	"github.com/google/uuid"
)

// Store implements store.ConfigStore using the legacy datastore.
type Store struct {
	ds datastore.Datastore
}

// New creates a new SQLite store, wrapping the legacy datastore.
func New(ds datastore.Datastore) *Store {
	return &Store{ds: ds}
}

// NewFromPath creates a new SQLite store from a file path.
func NewFromPath(path string) (*Store, error) {
	ds, err := datastore.NewDatastore(&datastore.Config{
		Backend:    datastore.BackendSQLite,
		SQLitePath: path,
	})
	if err != nil {
		return nil, fmt.Errorf("open sqlite datastore: %w", err)
	}
	return &Store{ds: ds}, nil
}

// CleanupEphemeralState removes lock and candidate rows left by a previous
// daemon process before this process starts accepting configuration changes.
func (s *Store) CleanupEphemeralState(ctx context.Context) error {
	cleaner, ok := s.ds.(interface {
		CleanupEphemeralState(context.Context) error
	})
	if !ok {
		return nil
	}
	return cleaner.CleanupEphemeralState(ctx)
}

func (s *Store) GetLatestSnapshot(ctx context.Context) (*model.ConfigSnapshot, error) {
	running, err := s.ds.GetRunning(ctx)
	if err != nil {
		var dsErr *datastore.Error
		if errors.As(err, &dsErr) && dsErr.Code == datastore.ErrCodeNotFound {
			return nil, nil
		}
		return nil, err
	}
	if running == nil {
		return nil, nil
	}

	// Parse the stored config text into the new model
	cfg, err := parseStoredConfig(running.ConfigText)
	if err != nil {
		return nil, fmt.Errorf("parse stored config: %w", err)
	}

	snap := model.NewSnapshot(cfg, 1, "system", "loaded from datastore")
	snap.CreatedAt = running.Timestamp
	return snap, nil
}

func (s *Store) PrepareCommit(ctx context.Context, snap *model.ConfigSnapshot) (store.PreparedCommit, error) {
	if snap == nil || snap.Config == nil {
		return nil, fmt.Errorf("snapshot is nil")
	}

	// Store set-command text so the legacy datastore users, including NETCONF,
	// can continue to read the same running_config rows.
	configText, err := pkgconfig.ToSetCommandsWithError(snap.Config.ToLegacyConfig())
	if err != nil {
		return nil, fmt.Errorf("serialize config: %w", err)
	}

	// Use the legacy commit mechanism
	sessionID := "engine-" + uuid.NewString()
	req := &datastore.CommitRequest{
		SessionID: sessionID,
		User:      snap.Author,
		Message:   snap.Message,
	}

	if err := s.ds.AcquireLock(ctx, &datastore.LockRequest{
		Target:    datastore.LockTargetCandidate,
		SessionID: sessionID,
		User:      snap.Author,
		Timeout:   30 * time.Minute,
	}); err != nil {
		return nil, fmt.Errorf("acquire commit lock: %w", err)
	}

	if err := s.ds.SaveCandidate(ctx, sessionID, configText); err != nil {
		_ = s.ds.ReleaseLock(context.Background(), datastore.LockTargetCandidate, sessionID)
		return nil, fmt.Errorf("save candidate: %w", err)
	}

	return &preparedCommit{
		ds:        s.ds,
		sessionID: sessionID,
		req:       req,
	}, nil
}

func (s *Store) SaveCommit(ctx context.Context, snap *model.ConfigSnapshot) (string, error) {
	prepared, err := s.PrepareCommit(ctx, snap)
	if err != nil {
		return "", err
	}
	commitID, err := prepared.Commit(ctx)
	if err != nil {
		_ = prepared.Abort(context.Background())
		return "", err
	}
	return commitID, nil
}

func (s *Store) PrepareRollback(ctx context.Context, snap *model.ConfigSnapshot, targetCommitID string) (store.PreparedCommit, error) {
	if snap == nil || snap.Config == nil {
		return nil, fmt.Errorf("snapshot is nil")
	}
	if targetCommitID == "" {
		return nil, fmt.Errorf("target commit ID is required")
	}
	if _, err := s.ds.GetCommit(ctx, targetCommitID); err != nil {
		return nil, fmt.Errorf("load rollback target: %w", err)
	}

	sessionID := "engine-" + uuid.NewString()
	if err := s.ds.AcquireLock(ctx, &datastore.LockRequest{
		Target:    datastore.LockTargetCandidate,
		SessionID: sessionID,
		User:      snap.Author,
		Timeout:   30 * time.Minute,
	}); err != nil {
		return nil, fmt.Errorf("acquire rollback lock: %w", err)
	}

	return &preparedRollback{
		ds:        s.ds,
		sessionID: sessionID,
		req: &datastore.RollbackRequest{
			SessionID: sessionID,
			CommitID:  targetCommitID,
			User:      snap.Author,
			Message:   snap.Message,
		},
	}, nil
}

func (s *Store) GetCommit(ctx context.Context, commitID string) (*store.CommitRecord, error) {
	entry, err := s.ds.GetCommit(ctx, commitID)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, nil
	}

	cfg, err := parseStoredConfig(entry.ConfigText)
	if err != nil {
		return nil, fmt.Errorf("parse commit config: %w", err)
	}

	return &store.CommitRecord{
		CommitID:   entry.CommitID,
		Config:     cfg,
		Author:     entry.User,
		Message:    entry.Message,
		Timestamp:  entry.Timestamp,
		IsRollback: entry.IsRollback,
	}, nil
}

func (s *Store) ListCommits(ctx context.Context, opts *store.ListOptions) ([]*store.CommitRecord, error) {
	dsOpts := &datastore.HistoryOptions{}
	if opts != nil {
		dsOpts.Limit = opts.Limit
		dsOpts.Offset = opts.Offset
		dsOpts.User = opts.User
	}

	entries, err := s.ds.ListCommitHistory(ctx, dsOpts)
	if err != nil {
		return nil, err
	}

	records := make([]*store.CommitRecord, 0, len(entries))
	for _, e := range entries {
		records = append(records, &store.CommitRecord{
			CommitID:   e.CommitID,
			Author:     e.User,
			Message:    e.Message,
			Timestamp:  e.Timestamp,
			IsRollback: e.IsRollback,
		})
	}
	return records, nil
}

func (s *Store) AuditLog(ctx context.Context, event *store.AuditEvent) error {
	detailsJSON, _ := json.Marshal(event.Details)

	return s.ds.LogAuditEvent(ctx, &datastore.AuditEvent{
		Timestamp: event.Timestamp,
		User:      event.User,
		SessionID: event.SessionID,
		SourceIP:  event.SourceIP,
		Action:    event.Action,
		Result:    event.Result,
		Details:   string(detailsJSON),
	})
}

func (s *Store) Close() error {
	return s.ds.Close()
}

// parseStoredConfig attempts to parse stored config as JSON (new format)
// or falls back to set-command text (legacy format).
func parseStoredConfig(text string) (*model.RouterConfig, error) {
	// Try JSON first (new format)
	var cfg model.RouterConfig
	if err := json.Unmarshal([]byte(text), &cfg); err == nil {
		return &cfg, nil
	}

	// Fall back to legacy set-command text parsing
	// This uses the existing pkg/config parser via the hook
	return parseLegacyText(text)
}

// LegacyTextParser is a hook for parsing legacy set-command text.
// It is set at initialization to break the circular dependency with pkg/config.
var LegacyTextParser func(text string) (*model.RouterConfig, error)

// parseLegacyText parses set-command format text using the existing parser.
func parseLegacyText(text string) (*model.RouterConfig, error) {
	if LegacyTextParser != nil {
		return LegacyTextParser(text)
	}
	return nil, fmt.Errorf("legacy text parser not initialized")
}

// Verify interface compliance at compile time.
var _ store.ConfigStore = (*Store)(nil)
var _ store.RollbackPreparer = (*Store)(nil)

// Legacy returns the underlying legacy datastore for components that
// still need it during the migration period.
func (s *Store) Legacy() datastore.Datastore {
	return s.ds
}

// TimeNow is used for testing. In production it returns time.Now().
var TimeNow = time.Now

type preparedCommit struct {
	ds        datastore.Datastore
	sessionID string
	req       *datastore.CommitRequest
}

func (p *preparedCommit) Commit(ctx context.Context) (string, error) {
	return p.ds.Commit(ctx, p.req)
}

func (p *preparedCommit) Abort(ctx context.Context) error {
	deleteErr := p.ds.DeleteCandidate(ctx, p.sessionID)
	releaseErr := p.ds.ReleaseLock(ctx, datastore.LockTargetCandidate, p.sessionID)
	if deleteErr != nil {
		return deleteErr
	}
	return releaseErr
}

type preparedRollback struct {
	ds        datastore.Datastore
	sessionID string
	req       *datastore.RollbackRequest
}

func (p *preparedRollback) Commit(ctx context.Context) (string, error) {
	return p.ds.Rollback(ctx, p.req)
}

func (p *preparedRollback) Abort(ctx context.Context) error {
	return p.ds.ReleaseLock(ctx, datastore.LockTargetCandidate, p.sessionID)
}
