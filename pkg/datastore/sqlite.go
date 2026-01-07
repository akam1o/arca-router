package datastore

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3" // SQLite driver
)

// sqliteDatastore implements the Datastore interface using SQLite.
type sqliteDatastore struct {
	db              *sql.DB
	dbPath          string
	cleanupStopChan chan struct{}
	cleanupDoneChan chan struct{}
	closeOnce       sync.Once
}

// NewSQLiteDatastore creates a new SQLite-backed datastore.
func NewSQLiteDatastore(cfg *Config) (Datastore, error) {
	if cfg.Backend != BackendSQLite {
		return nil, fmt.Errorf("invalid backend type: %s (expected %s)", cfg.Backend, BackendSQLite)
	}

	dbPath := cfg.SQLitePath
	if dbPath == "" {
		dbPath = "/var/lib/arca-router/config.db"
	}

	// Create directory if it doesn't exist
	if dbPath != ":memory:" {
		dir := filepath.Dir(dbPath)
		if err := os.MkdirAll(dir, 0750); err != nil {
			return nil, fmt.Errorf("failed to create database directory: %w", err)
		}
	}

	// Open SQLite database with _txlock=immediate for write transactions
	// This ensures write transactions acquire RESERVED lock immediately, preventing lock upgrade races
	// Read-only transactions (with ReadOnly: true in TxOptions) are unaffected and remain DEFERRED
	// Note: With WAL mode, read/write locks are independent, so this has minimal impact on read concurrency
	dsn := dbPath + "?_txlock=immediate"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure SQLite for production use
	pragmas := []string{
		"PRAGMA journal_mode=WAL",    // Write-Ahead Logging for better concurrency
		"PRAGMA synchronous=NORMAL",  // Balance between safety and performance
		"PRAGMA foreign_keys=ON",     // Enable foreign key constraints
		"PRAGMA busy_timeout=5000",   // Wait up to 5 seconds on lock contention
		"PRAGMA cache_size=-64000",   // Use 64MB cache
		"PRAGMA temp_store=MEMORY",   // Store temp tables in memory
		"PRAGMA mmap_size=268435456", // Memory-map I/O (256MB)
	}

	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to set pragma %q: %w", pragma, err)
		}
	}

	// Set connection pool limits
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetConnMaxIdleTime(1 * time.Minute)

	ds := &sqliteDatastore{
		db:              db,
		dbPath:          dbPath,
		cleanupStopChan: make(chan struct{}),
		cleanupDoneChan: make(chan struct{}),
	}

	// Run migrations
	migrator := NewSQLiteMigrationManager(db, dbPath)
	if err := migrator.ApplyMigrations(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to apply migrations: %w", err)
	}

	// Start background cleanup goroutine for expired locks
	go ds.cleanupExpiredLocks()

	return ds, nil
}

// Close closes the datastore connection.
// This method is idempotent and safe to call multiple times.
func (ds *sqliteDatastore) Close() error {
	var closeErr error

	ds.closeOnce.Do(func() {
		// Signal cleanup goroutine to stop
		close(ds.cleanupStopChan)

		// Wait for cleanup goroutine to finish (with timeout)
		select {
		case <-ds.cleanupDoneChan:
			// Cleanup goroutine finished
		case <-time.After(5 * time.Second):
			// Timeout waiting for cleanup goroutine
		}

		if ds.db != nil {
			closeErr = ds.db.Close()
		}
	})

	return closeErr
}

// cleanupExpiredLocks runs in a background goroutine to periodically remove expired locks.
// This prevents stale lock rows from lingering in the database.
func (ds *sqliteDatastore) cleanupExpiredLocks() {
	defer close(ds.cleanupDoneChan)

	ticker := time.NewTicker(5 * time.Minute) // Cleanup every 5 minutes
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Perform cleanup
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			err := ds.performLockCleanup(ctx)
			cancel()

			if err != nil {
				// Log error but continue (non-critical operation)
				// Store error in audit log for operational visibility
				auditErr := ds.logCleanupError(err)
				if auditErr != nil {
					// Even audit logging failed, but we can't do much more
					// In production, this would be sent to a monitoring system
					_ = auditErr
				}
			}

		case <-ds.cleanupStopChan:
			// Stop signal received
			return
		}
	}
}

// logCleanupError logs a cleanup failure to the audit log for operational visibility.
func (ds *sqliteDatastore) logCleanupError(cleanupErr error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return ds.withTx(ctx, false, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO audit_log (user, session_id, action, result, error_code, details)
			VALUES ('system', '', 'lock_cleanup', 'failure', 'CLEANUP_ERROR', ?)
		`, cleanupErr.Error())

		if err != nil {
			return NewError(ErrCodeInternal, "failed to log cleanup error", err)
		}

		return nil
	})
}

// performLockCleanup removes expired locks from the database (for all targets).
func (ds *sqliteDatastore) performLockCleanup(ctx context.Context) error {
	return ds.withTx(ctx, false, func(tx *sql.Tx) error {
		now := time.Now()

		// Delete expired locks for all targets (candidate, running)
		result, err := tx.ExecContext(ctx, `
			DELETE FROM config_locks
			WHERE expires_at < ?
		`, now)

		if err != nil {
			return NewError(ErrCodeInternal, "failed to cleanup expired locks", err)
		}

		// Check if any locks were deleted
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return NewError(ErrCodeInternal, "failed to check cleanup result", err)
		}

		// Log audit event if locks were cleaned up
		if rowsAffected > 0 {
			_, err = tx.ExecContext(ctx, `
				INSERT INTO audit_log (user, session_id, action, result, details)
				VALUES ('system', '', 'lock_cleanup', 'success', ?)
			`, fmt.Sprintf("cleaned up %d expired lock(s)", rowsAffected))

			if err != nil {
				return NewError(ErrCodeInternal, "failed to log cleanup audit event", err)
			}
		}

		return nil
	})
}

// beginTx starts a new transaction with the specified isolation level.
func (ds *sqliteDatastore) beginTx(ctx context.Context, readOnly bool) (*sql.Tx, error) {
	opts := &sql.TxOptions{}
	if readOnly {
		// Explicitly mark as read-only
		// With _txlock=immediate, this will still use DEFERRED mode for read-only transactions
		opts.ReadOnly = true
	}
	// For write transactions, _txlock=immediate ensures IMMEDIATE mode (RESERVED lock acquired upfront)

	tx, err := ds.db.BeginTx(ctx, opts)
	if err != nil {
		return nil, NewError(ErrCodeInternal, "failed to begin transaction", err)
	}
	return tx, nil
}

// withTx executes a function within a transaction, handling commit/rollback automatically.
func (ds *sqliteDatastore) withTx(ctx context.Context, readOnly bool, fn func(*sql.Tx) error) error {
	tx, err := ds.beginTx(ctx, readOnly)
	if err != nil {
		return err
	}

	defer func() {
		if p := recover(); p != nil {
			tx.Rollback()
			panic(p) // Re-throw panic after rollback
		}
	}()

	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}

	if err := tx.Commit(); err != nil {
		return NewError(ErrCodeInternal, "failed to commit transaction", err)
	}

	return nil
}
