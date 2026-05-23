package datastore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3" // SQLite driver
)

const (
	secureSQLiteFilePerms os.FileMode = 0600
	secureSQLiteDirPerms  os.FileMode = 0750
)

// sqliteDatastore implements the Datastore interface using SQLite.
type sqliteDatastore struct {
	db              *sql.DB
	dbPath          string
	cleanupStopChan chan struct{}
	cleanupDoneChan chan struct{}
	closeOnce       sync.Once
}

// ProcessLock is an OS-level lock that marks a SQLite datastore as owned by one daemon process.
type ProcessLock struct {
	file      *os.File
	closeOnce sync.Once
	closeErr  error
}

// NewSQLiteDatastore creates a new SQLite-backed datastore.
func NewSQLiteDatastore(cfg *Config) (Datastore, error) {
	if cfg == nil {
		return nil, fmt.Errorf("datastore config cannot be nil")
	}
	if cfg.Backend != BackendSQLite {
		return nil, fmt.Errorf("invalid backend type: %s (expected %s)", cfg.Backend, BackendSQLite)
	}

	dbPath := cfg.SQLitePath
	if dbPath == "" {
		dbPath = "/var/lib/arca-router/config.db"
	}
	if err := validateSQLitePath(dbPath); err != nil {
		return nil, err
	}

	// Create directory if it doesn't exist
	if dbPath != ":memory:" {
		if err := prepareSecureSQLiteFile(dbPath); err != nil {
			return nil, err
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
			if closeErr := db.Close(); closeErr != nil {
				_ = closeErr
			}
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
		if closeErr := db.Close(); closeErr != nil {
			_ = closeErr
		}
		return nil, fmt.Errorf("failed to apply migrations: %w", err)
	}
	if err := restrictSQLiteFilePermissions(dbPath); err != nil {
		if closeErr := db.Close(); closeErr != nil {
			_ = closeErr
		}
		return nil, err
	}

	// Start background cleanup goroutine for expired locks
	go ds.cleanupExpiredLocks()

	return ds, nil
}

// AcquireSQLiteProcessLock acquires an exclusive non-blocking lock beside the
// datastore file. It is intended for daemon startup coordination, not per-RPC
// candidate locking.
func AcquireSQLiteProcessLock(dbPath string) (*ProcessLock, error) {
	if dbPath == "" {
		dbPath = "/var/lib/arca-router/config.db"
	}
	if err := validateSQLitePath(dbPath); err != nil {
		return nil, err
	}
	if dbPath == ":memory:" {
		return &ProcessLock{}, nil
	}
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, secureSQLiteDirPerms); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}
	if err := validateSQLiteDirectoryPermissions(dir); err != nil {
		return nil, err
	}

	lockPath := dbPath + ".process.lock"
	file, err := openSecureSQLiteFile(lockPath, "datastore process lock", true)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, NewError(ErrCodeConflict, "datastore is already owned by another process", err)
		}
		return nil, fmt.Errorf("failed to acquire datastore process lock: %w", err)
	}
	if err := file.Truncate(0); err != nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
		return nil, fmt.Errorf("failed to update datastore process lock: %w", err)
	}
	if _, err := fmt.Fprintf(file, "%d\n", os.Getpid()); err != nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
		return nil, fmt.Errorf("failed to write datastore process lock: %w", err)
	}
	return &ProcessLock{file: file}, nil
}

func validateSQLitePath(dbPath string) error {
	if dbPath == "" || dbPath == ":memory:" {
		return nil
	}
	lowerPath := strings.ToLower(dbPath)
	if strings.HasPrefix(lowerPath, "file:") || strings.Contains(dbPath, "?") {
		return fmt.Errorf("sqlite path must be a filesystem path without URI parameters")
	}
	return nil
}

// Close releases the process lock.
func (l *ProcessLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	l.closeOnce.Do(func() {
		if err := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN); err != nil {
			l.closeErr = err
		}
		if err := l.file.Close(); l.closeErr == nil && err != nil {
			l.closeErr = err
		}
	})
	return l.closeErr
}

func prepareSecureSQLiteFile(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, secureSQLiteDirPerms); err != nil {
		return fmt.Errorf("failed to create database directory: %w", err)
	}
	if err := validateSQLiteDirectoryPermissions(dir); err != nil {
		return err
	}
	file, err := openSecureSQLiteFile(dbPath, "database file", true)
	if err != nil {
		return err
	}
	if closeErr := file.Close(); closeErr != nil {
		return fmt.Errorf("failed to close database file: %w", closeErr)
	}
	return nil
}

func validateSQLiteDirectoryPermissions(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("failed to stat database directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("database directory %s must not be a symbolic link", dir)
	}
	if !info.IsDir() {
		return fmt.Errorf("database parent path is not a directory: %s", dir)
	}
	perms := info.Mode().Perm()
	if perms&0022 != 0 {
		return fmt.Errorf("insecure permissions on database directory %s: mode=%04o", dir, perms)
	}
	return nil
}

func validateSQLiteFilePath(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to stat database file %s: %w", path, err)
	}
	return validateSQLiteFileInfo(path, info)
}

func validateSQLiteFileInfo(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("database file %s must not be a symbolic link", path)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("database file %s is not a regular file", path)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Nlink > 1 {
		return fmt.Errorf("database file %s must not have multiple hard links", path)
	}
	return nil
}

func openSecureSQLiteFile(path, label string, create bool) (*os.File, error) {
	if err := validateSQLiteFilePath(path); err != nil {
		return nil, err
	}

	flags := os.O_RDWR | syscall.O_NOFOLLOW
	if create {
		flags |= os.O_CREATE
	}
	file, err := os.OpenFile(path, flags, secureSQLiteFilePerms)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", label, err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = file.Close()
		}
	}()

	openedInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat %s: %w", label, err)
	}
	if err := validateSQLiteFileInfo(path, openedInfo); err != nil {
		return nil, err
	}
	if err := file.Chmod(secureSQLiteFilePerms); err != nil {
		return nil, fmt.Errorf("failed to restrict %s permissions: %w", label, err)
	}
	currentInfo, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat database file %s: %w", path, err)
	}
	if err := validateSQLiteFileInfo(path, currentInfo); err != nil {
		return nil, err
	}
	if !os.SameFile(openedInfo, currentInfo) {
		return nil, fmt.Errorf("refusing to use database file %s: file changed during preparation", path)
	}

	cleanup = false
	return file, nil
}

func restrictSQLiteFilePermissions(dbPath string) error {
	if dbPath == "" || dbPath == ":memory:" {
		return nil
	}
	for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		info, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("failed to stat database file %s: %w", path, err)
		}
		if err := validateSQLiteFileInfo(path, info); err != nil {
			return err
		}
		file, err := os.OpenFile(path, os.O_RDWR|syscall.O_NOFOLLOW, secureSQLiteFilePerms)
		if err != nil {
			return fmt.Errorf("failed to open database file %s: %w", path, err)
		}
		openedInfo, statErr := file.Stat()
		if statErr != nil {
			_ = file.Close()
			return fmt.Errorf("failed to stat database file %s: %w", path, statErr)
		}
		if err := validateSQLiteFileInfo(path, openedInfo); err != nil {
			_ = file.Close()
			return err
		}
		if !os.SameFile(info, openedInfo) {
			_ = file.Close()
			return fmt.Errorf("refusing to restrict database file %s: file changed during permission update", path)
		}
		if err := file.Chmod(secureSQLiteFilePerms); err != nil {
			_ = file.Close()
			return fmt.Errorf("failed to restrict database file permissions for %s: %w", path, err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("failed to close database file %s: %w", path, err)
		}
	}
	return nil
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
		now := time.Now().Unix()

		// Delete expired locks for all targets (candidate, running)
		result, err := tx.ExecContext(ctx, `
				DELETE FROM config_locks
				WHERE COALESCE(
					CASE
						WHEN typeof(expires_at) IN ('integer', 'real') THEN CAST(expires_at AS INTEGER)
					END,
					CASE
						WHEN typeof(expires_at) = 'text' AND expires_at NOT GLOB '*[^0-9]*' THEN CAST(expires_at AS INTEGER)
					END,
					CAST(strftime('%s', expires_at) AS INTEGER),
					0
				) < ?
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

// CleanupEphemeralState removes locks and candidates that cannot survive a
// daemon restart safely. Running configuration and commit history are preserved.
func (ds *sqliteDatastore) CleanupEphemeralState(ctx context.Context) error {
	return ds.withTx(ctx, false, func(tx *sql.Tx) error {
		candidateResult, err := tx.ExecContext(ctx, `DELETE FROM candidate_configs`)
		if err != nil {
			return NewError(ErrCodeInternal, "failed to cleanup candidate configs", err)
		}
		lockResult, err := tx.ExecContext(ctx, `DELETE FROM config_locks`)
		if err != nil {
			return NewError(ErrCodeInternal, "failed to cleanup config locks", err)
		}

		candidatesRemoved, err := candidateResult.RowsAffected()
		if err != nil {
			return NewError(ErrCodeInternal, "failed to check candidate cleanup result", err)
		}
		locksRemoved, err := lockResult.RowsAffected()
		if err != nil {
			return NewError(ErrCodeInternal, "failed to check lock cleanup result", err)
		}
		if candidatesRemoved == 0 && locksRemoved == 0 {
			return nil
		}

		_, err = tx.ExecContext(ctx, `
			INSERT INTO audit_log (user, session_id, action, result, details)
			VALUES ('system', '', 'startup_cleanup', 'success', ?)
		`, fmt.Sprintf("removed %d lock(s), %d candidate config(s)", locksRemoved, candidatesRemoved))
		if err != nil {
			return NewError(ErrCodeInternal, "failed to log startup cleanup audit event", err)
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
			if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
				_ = rollbackErr
			}
			panic(p) // Re-throw panic after rollback
		}
	}()

	if err := fn(tx); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			_ = rollbackErr
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return NewError(ErrCodeInternal, "failed to commit transaction", err)
	}

	return nil
}
