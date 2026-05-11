// Package store defines the configuration persistence interface.
// It abstracts away the storage backend (SQLite, etcd) from the engine
// and northbound services.
package store

import (
	"context"
	"time"

	"github.com/akam1o/arca-router/internal/model"
)

// ConfigStore provides persistence for configuration snapshots,
// commit history, and audit events.
type ConfigStore interface {
	// GetLatestSnapshot returns the most recent committed configuration.
	GetLatestSnapshot(ctx context.Context) (*model.ConfigSnapshot, error)

	// PrepareCommit reserves persistence for a configuration commit before
	// southbound changes are applied. The caller must either Commit or Abort
	// the returned prepared commit.
	PrepareCommit(ctx context.Context, snap *model.ConfigSnapshot) (PreparedCommit, error)

	// SaveCommit persists a new configuration commit and returns its ID.
	SaveCommit(ctx context.Context, snap *model.ConfigSnapshot) (string, error)

	// GetCommit retrieves a specific commit by ID.
	GetCommit(ctx context.Context, commitID string) (*CommitRecord, error)

	// ListCommits returns commit history with pagination.
	ListCommits(ctx context.Context, opts *ListOptions) ([]*CommitRecord, error)

	// AuditLog logs an audit event.
	AuditLog(ctx context.Context, event *AuditEvent) error

	// Close releases resources.
	Close() error
}

// PreparedCommit represents a datastore commit that has been staged but not
// promoted to running yet.
type PreparedCommit interface {
	Commit(ctx context.Context) (string, error)
	Abort(ctx context.Context) error
}

// CommitRecord represents a persisted commit entry.
type CommitRecord struct {
	CommitID   string              `json:"commit_id"`
	Version    uint64              `json:"version"`
	Config     *model.RouterConfig `json:"config"`
	Author     string              `json:"author"`
	Message    string              `json:"message,omitempty"`
	Timestamp  time.Time           `json:"timestamp"`
	IsRollback bool                `json:"is_rollback,omitempty"`
}

// ListOptions controls pagination and filtering for commit history.
type ListOptions struct {
	Limit  int
	Offset int
	User   string
}

// AuditEvent represents a logged audit event.
type AuditEvent struct {
	Timestamp time.Time         `json:"timestamp"`
	User      string            `json:"user"`
	SessionID string            `json:"session_id,omitempty"`
	SourceIP  string            `json:"source_ip,omitempty"`
	Action    string            `json:"action"`
	Result    string            `json:"result"`
	Details   map[string]string `json:"details,omitempty"`
}
