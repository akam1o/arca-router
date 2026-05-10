// Package sqlite implements the ConfigStore interface using SQLite.
// It bridges to the existing pkg/datastore SQLite implementation during
// the migration period.
package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/akam1o/arca-router/internal/model"
	"github.com/akam1o/arca-router/internal/store"
	"github.com/akam1o/arca-router/pkg/datastore"
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

func (s *Store) GetLatestSnapshot(ctx context.Context) (*model.ConfigSnapshot, error) {
	running, err := s.ds.GetRunning(ctx)
	if err != nil {
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

	return &model.ConfigSnapshot{
		Config:    cfg,
		Author:    "system",
		CreatedAt: running.Timestamp,
	}, nil
}

func (s *Store) SaveCommit(ctx context.Context, snap *model.ConfigSnapshot) (string, error) {
	if snap == nil || snap.Config == nil {
		return "", fmt.Errorf("snapshot is nil")
	}

	// Serialize config to JSON for storage
	configJSON, err := json.Marshal(snap.Config)
	if err != nil {
		return "", fmt.Errorf("marshal config: %w", err)
	}

	// Use the legacy commit mechanism
	// We store JSON in the config text field for the new model
	sessionID := "engine"
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
		return "", fmt.Errorf("acquire commit lock: %w", err)
	}
	defer func() {
		_ = s.ds.ReleaseLock(context.Background(), datastore.LockTargetCandidate, sessionID)
	}()

	if err := s.ds.SaveCandidate(ctx, sessionID, string(configJSON)); err != nil {
		return "", fmt.Errorf("save candidate: %w", err)
	}

	commitID, err := s.ds.Commit(ctx, req)
	if err != nil {
		return "", err
	}
	return commitID, nil
}

func (s *Store) GetCommit(ctx context.Context, commitID string) (*store.CommitRecord, error) {
	entry, err := s.ds.GetCommit(ctx, commitID)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, nil
	}

	return &store.CommitRecord{
		CommitID:   entry.CommitID,
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

// Legacy returns the underlying legacy datastore for components that
// still need it during the migration period.
func (s *Store) Legacy() datastore.Datastore {
	return s.ds
}

// TimeNow is used for testing. In production it returns time.Now().
var TimeNow = time.Now
