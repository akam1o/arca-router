// Package datastore provides configuration datastore management
// for arca-router, including running/candidate config separation,
// commit/rollback transactions, and audit logging.
//
// The datastore supports multiple backend implementations:
//   - SQLite: File-based storage for single-node deployments
//   - etcd: Distributed storage for multi-node clustering (Phase 4)
package datastore

import (
	"context"
	"time"
)

// Datastore is the main interface for configuration management.
// It provides operations for running/candidate configs, commit/rollback,
// locking, and audit trail.
//
// All operations accept context.Context for timeout/cancellation support.
type Datastore interface {
	// Running configuration
	GetRunning(ctx context.Context) (*RunningConfig, error)

	// Candidate configuration
	GetCandidate(ctx context.Context, sessionID string) (*CandidateConfig, error)
	SaveCandidate(ctx context.Context, sessionID string, configText string) error
	DeleteCandidate(ctx context.Context, sessionID string) error

	// Commit/Rollback transactions
	Commit(ctx context.Context, req *CommitRequest) (commitID string, err error)
	Rollback(ctx context.Context, req *RollbackRequest) (newCommitID string, err error)

	// Diff/Compare operations
	CompareCandidateRunning(ctx context.Context, sessionID string) (*DiffResult, error)
	CompareCommits(ctx context.Context, commitID1, commitID2 string) (*DiffResult, error)

	// Lock management
	AcquireLock(ctx context.Context, req *LockRequest) error
	ReleaseLock(ctx context.Context, sessionID string) error
	ExtendLock(ctx context.Context, sessionID string, duration time.Duration) error
	StealLock(ctx context.Context, req *StealLockRequest) error
	GetLockInfo(ctx context.Context) (*LockInfo, error)

	// Commit history
	ListCommitHistory(ctx context.Context, opts *HistoryOptions) ([]*CommitHistoryEntry, error)
	GetCommit(ctx context.Context, commitID string) (*CommitHistoryEntry, error)

	// Audit logging
	LogAuditEvent(ctx context.Context, event *AuditEvent) error

	// Close the datastore
	Close() error
}

// RunningConfig represents the current active configuration.
type RunningConfig struct {
	CommitID   string    // UUID of the commit
	ConfigText string    // Configuration in set-command format
	Timestamp  time.Time // When this became the running config
}

// CandidateConfig represents a session's working configuration.
type CandidateConfig struct {
	SessionID  string    // Session identifier (UUID)
	ConfigText string    // Configuration in set-command format
	CreatedAt  time.Time // When the candidate was created
	UpdatedAt  time.Time // Last modification time
}

// CommitRequest contains parameters for a commit operation.
type CommitRequest struct {
	SessionID string // Session holding the candidate config
	User      string // Username performing the commit
	Message   string // Optional commit message
	SourceIP  string // Source IP address of the user (for audit)
}

// RollbackRequest contains parameters for a rollback operation.
type RollbackRequest struct {
	CommitID string // Target commit ID to rollback to
	User     string // Username performing the rollback
	Message  string // Optional rollback reason
	SourceIP string // Source IP address of the user (for audit)
}

// HistoryOptions contains filtering options for commit history queries.
type HistoryOptions struct {
	Limit            int       // Maximum number of entries to return (0 = no limit)
	Offset           int       // Number of entries to skip (for pagination)
	StartTime        time.Time // Filter commits after this time (zero = no filter)
	EndTime          time.Time // Filter commits before this time (zero = no filter)
	User             string    // Filter by username (empty = no filter)
	ExcludeRollbacks bool      // Whether to exclude rollback commits (default: false, include all)
}

// DiffResult represents the difference between two configurations.
type DiffResult struct {
	DiffText   string // Unified diff format
	HasChanges bool   // Whether there are any differences
}

// LockRequest contains parameters for acquiring a config lock.
type LockRequest struct {
	SessionID string        // Session requesting the lock
	User      string        // Username requesting the lock
	Timeout   time.Duration // Lock timeout duration (default: 30 minutes)
}

// StealLockRequest contains parameters for stealing a lock (admin only).
type StealLockRequest struct {
	NewSessionID    string // New session taking the lock
	User            string // Admin user stealing the lock
	TargetSessionID string // Session currently holding the lock
	Reason          string // Reason for stealing the lock
}

// LockInfo describes the current lock state.
type LockInfo struct {
	IsLocked  bool      // Whether the config is currently locked
	SessionID string    // Session holding the lock (empty if not locked)
	User      string    // User holding the lock (empty if not locked)
	AcquiredAt time.Time // When the lock was acquired (zero if not locked)
	ExpiresAt  time.Time // When the lock will expire (zero if not locked)
}

// CommitHistoryEntry represents a single commit in the history.
type CommitHistoryEntry struct {
	CommitID   string    // UUID of the commit
	User       string    // Username who made the commit
	Timestamp  time.Time // When the commit was made
	Message    string    // Commit message (may be empty)
	ConfigText string    // Configuration at this commit
	IsRollback bool      // Whether this commit was a rollback operation
	SourceIP   string    // Source IP address of the user (for audit tracking)
}

// AuditEvent represents a logged event for audit trail.
type AuditEvent struct {
	ID            int64     // Auto-increment ID for SQLite (set by datastore, 0 for etcd)
	Key           string    // ULID key for etcd backend (empty for SQLite)
	Timestamp     time.Time // When the event occurred
	User          string    // Username associated with the event
	SessionID     string    // Session ID (may be empty)
	SourceIP      string    // Source IP address (for security tracking)
	CorrelationID string    // Correlation ID for tracing related events
	Action        string    // Action type (e.g., "commit", "rollback", "lock_acquire")
	Result        string    // Result (e.g., "success", "failure")
	ErrorCode     string    // Error code for failures (empty for success)
	Details       string    // Additional details (JSON or text)
}

// MigrationManager handles database schema migrations.
type MigrationManager interface {
	// GetCurrentVersion returns the current schema version.
	GetCurrentVersion() (int, error)

	// ApplyMigrations applies all pending migrations.
	ApplyMigrations() error

	// CreateBackup creates a database backup before migrations.
	CreateBackup() (string, error)
}

// BackendType represents the type of datastore backend.
type BackendType string

const (
	// BackendSQLite is a file-based SQLite backend (single-node).
	BackendSQLite BackendType = "sqlite"

	// BackendEtcd is a distributed etcd backend (multi-node clustering).
	BackendEtcd BackendType = "etcd"
)

// Config contains configuration for datastore initialization.
type Config struct {
	// Backend type (sqlite or etcd)
	Backend BackendType

	// SQLite-specific configuration
	SQLitePath string // Path to SQLite database file (default: /var/lib/arca-router/config.db)

	// etcd-specific configuration
	EtcdEndpoints []string // etcd cluster endpoints (e.g., ["localhost:2379"])
	EtcdPrefix    string   // Key prefix for arca-router data (default: /arca-router/)
	EtcdTimeout   time.Duration // Connection timeout (default: 5s)
	EtcdUsername  string   // Optional username for authentication
	EtcdPassword  string   // Optional password for authentication
	EtcdTLS       *TLSConfig // Optional TLS configuration
}

// TLSConfig contains TLS configuration for etcd connections.
type TLSConfig struct {
	CertFile string // Path to client certificate file
	KeyFile  string // Path to client key file
	CAFile   string // Path to CA certificate file
}

// ErrorCode represents a standardized error code for datastore operations.
type ErrorCode string

const (
	// ErrCodeNotFound indicates the requested resource was not found.
	ErrCodeNotFound ErrorCode = "NOT_FOUND"

	// ErrCodeConflict indicates a conflict (e.g., lock already held).
	ErrCodeConflict ErrorCode = "CONFLICT"

	// ErrCodeValidation indicates a validation error.
	ErrCodeValidation ErrorCode = "VALIDATION"

	// ErrCodeTimeout indicates a timeout during operation.
	ErrCodeTimeout ErrorCode = "TIMEOUT"

	// ErrCodeInternal indicates an internal datastore error.
	ErrCodeInternal ErrorCode = "INTERNAL"

	// ErrCodeUnauthorized indicates insufficient permissions.
	ErrCodeUnauthorized ErrorCode = "UNAUTHORIZED"
)

// Error represents a datastore error with structured information.
type Error struct {
	Code    ErrorCode // Error code
	Message string    // Human-readable error message
	Cause   error     // Underlying error (may be nil)
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	return e.Cause
}

// NewError creates a new datastore error.
func NewError(code ErrorCode, message string, cause error) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}
