package datastore

import (
	"context"
	"database/sql"
	"time"
)

// AcquireLock attempts to acquire the exclusive config lock.
func (ds *sqliteDatastore) AcquireLock(ctx context.Context, req *LockRequest) error {
	timeout := req.Timeout
	if timeout == 0 {
		timeout = 30 * time.Minute // Default timeout
	}

	expiresAt := time.Now().Add(timeout)

	return ds.withTx(ctx, false, func(tx *sql.Tx) error {
		now := time.Now()

		// Atomic lock acquisition using INSERT OR REPLACE with conditional check
		// This ensures only one transaction can succeed if lock is currently held and not expired
		result, err := tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO config_locks (
				lock_id, session_id, user, acquired_at, expires_at, last_activity
			)
			SELECT 1, ?, ?, ?, ?, ?
			WHERE NOT EXISTS (
				SELECT 1 FROM config_locks
				WHERE lock_id = 1 AND expires_at > ?
			)
		`, req.SessionID, req.User, now, expiresAt, now, now)

		if err != nil {
			return NewError(ErrCodeInternal, "failed to acquire lock", err)
		}

		// Check if the lock was actually acquired (rows affected > 0)
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return NewError(ErrCodeInternal, "failed to check lock acquisition result", err)
		}

		if rowsAffected == 0 {
			// Lock is held by another session and not expired
			return NewError(ErrCodeConflict,
				"config lock is held by another session",
				nil)
		}

		// Log audit event with structured context
		_, err = tx.ExecContext(ctx, `
			INSERT INTO audit_log (user, session_id, action, result, details)
			VALUES (?, ?, 'lock_acquire', 'success', '')
		`, req.User, req.SessionID)

		if err != nil {
			return NewError(ErrCodeInternal, "failed to log audit event", err)
		}

		return nil
	})
}

// ReleaseLock releases the config lock held by the specified session.
func (ds *sqliteDatastore) ReleaseLock(ctx context.Context, sessionID string) error {
	return ds.withTx(ctx, false, func(tx *sql.Tx) error {
		// Verify the lock is held by this session
		var lockSessionID string
		err := tx.QueryRowContext(ctx, `
			SELECT session_id FROM config_locks WHERE lock_id = 1
		`).Scan(&lockSessionID)

		if err == sql.ErrNoRows {
			// No lock exists, nothing to release
			return nil
		}
		if err != nil {
			return NewError(ErrCodeInternal, "failed to check lock ownership", err)
		}

		if lockSessionID != sessionID {
			return NewError(ErrCodeConflict,
				"cannot release lock held by another session", nil)
		}

		// Release lock
		_, err = tx.ExecContext(ctx, `
			DELETE FROM config_locks WHERE lock_id = 1
		`)
		if err != nil {
			return NewError(ErrCodeInternal, "failed to release lock", err)
		}

		// Log audit event
		_, err = tx.ExecContext(ctx, `
			INSERT INTO audit_log (session_id, action, result, user, details)
			VALUES (?, 'lock_release', 'success', '', '')
		`, sessionID)

		if err != nil {
			return NewError(ErrCodeInternal, "failed to log audit event", err)
		}

		return nil
	})
}

// ExtendLock extends the expiration time of an existing lock.
func (ds *sqliteDatastore) ExtendLock(ctx context.Context, sessionID string, duration time.Duration) error {
	if duration == 0 {
		duration = 30 * time.Minute
	}

	return ds.withTx(ctx, false, func(tx *sql.Tx) error {
		// Verify lock is held by this session
		var lockSessionID string
		err := tx.QueryRowContext(ctx, `
			SELECT session_id FROM config_locks WHERE lock_id = 1
		`).Scan(&lockSessionID)

		if err == sql.ErrNoRows {
			return NewError(ErrCodeNotFound, "no lock to extend", nil)
		}
		if err != nil {
			return NewError(ErrCodeInternal, "failed to check lock", err)
		}

		if lockSessionID != sessionID {
			return NewError(ErrCodeConflict,
				"cannot extend lock held by another session", nil)
		}

		// Extend lock
		newExpiresAt := time.Now().Add(duration)
		_, err = tx.ExecContext(ctx, `
			UPDATE config_locks
			SET expires_at = ?, last_activity = ?
			WHERE lock_id = 1
		`, newExpiresAt, time.Now())

		if err != nil {
			return NewError(ErrCodeInternal, "failed to extend lock", err)
		}

		return nil
	})
}

// StealLock forcibly acquires the lock (admin operation).
func (ds *sqliteDatastore) StealLock(ctx context.Context, req *StealLockRequest) error {
	timeout := 30 * time.Minute
	expiresAt := time.Now().Add(timeout)

	return ds.withTx(ctx, false, func(tx *sql.Tx) error {
		// Get current lock holder for audit
		var oldSessionID, oldUser string
		err := tx.QueryRowContext(ctx, `
			SELECT session_id, user FROM config_locks WHERE lock_id = 1
		`).Scan(&oldSessionID, &oldUser)

		if err != nil && err != sql.ErrNoRows {
			return NewError(ErrCodeInternal, "failed to check existing lock", err)
		}

		// Replace lock
		_, err = tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO config_locks (
				lock_id, session_id, user, acquired_at, expires_at, last_activity
			) VALUES (1, ?, ?, ?, ?, ?)
		`, req.NewSessionID, req.User, time.Now(), expiresAt, time.Now())

		if err != nil {
			return NewError(ErrCodeInternal, "failed to steal lock", err)
		}

		// Log audit event
		details := ""
		if oldSessionID != "" {
			details = "stolen from session: " + oldSessionID + " (user: " + oldUser + "), reason: " + req.Reason
		}

		_, err = tx.ExecContext(ctx, `
			INSERT INTO audit_log (user, session_id, action, result, details)
			VALUES (?, ?, 'lock_steal', 'success', ?)
		`, req.User, req.NewSessionID, details)

		if err != nil {
			return NewError(ErrCodeInternal, "failed to log audit event", err)
		}

		return nil
	})
}

// GetLockInfo retrieves information about the current lock state.
func (ds *sqliteDatastore) GetLockInfo(ctx context.Context) (*LockInfo, error) {
	var sessionID, user string
	var acquiredAt, expiresAt time.Time

	err := ds.db.QueryRowContext(ctx, `
		SELECT session_id, user, acquired_at, expires_at
		FROM config_locks
		WHERE lock_id = 1
	`).Scan(&sessionID, &user, &acquiredAt, &expiresAt)

	if err == sql.ErrNoRows {
		// No lock exists
		return &LockInfo{
			IsLocked: false,
		}, nil
	}
	if err != nil {
		return nil, NewError(ErrCodeInternal, "failed to get lock info", err)
	}

	// Check if lock is expired
	if time.Now().After(expiresAt) {
		// Lock expired but not yet cleaned up
		return &LockInfo{
			IsLocked: false,
		}, nil
	}

	return &LockInfo{
		IsLocked:   true,
		SessionID:  sessionID,
		User:       user,
		AcquiredAt: acquiredAt,
		ExpiresAt:  expiresAt,
	}, nil
}
