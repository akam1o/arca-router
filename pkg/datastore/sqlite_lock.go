package datastore

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// AcquireLock attempts to acquire the exclusive config lock for a specific target datastore.
func (ds *sqliteDatastore) AcquireLock(ctx context.Context, req *LockRequest) error {
	// Validate target
	if err := ValidateLockTarget(req.Target); err != nil {
		return err
	}

	timeout := req.Timeout
	if timeout == 0 {
		timeout = 30 * time.Minute // Default timeout
	}

	return ds.withTx(ctx, false, func(tx *sql.Tx) error {
		now := time.Now()
		expiresAt := now.Add(timeout)

		// Check existing lock in the same transaction
		var existingSessionID, existingUser string
		var existingExpiresAt time.Time
		err := tx.QueryRowContext(ctx, `
			SELECT session_id, user, expires_at
			FROM config_locks
			WHERE target = ?
		`, req.Target).Scan(&existingSessionID, &existingUser, &existingExpiresAt)

		if err != nil && err != sql.ErrNoRows {
			return NewError(ErrCodeInternal, fmt.Sprintf("failed to check %s lock", req.Target), err)
		}

		if err == nil {
			// Lock exists: decide based on session and expiration
			if now.After(existingExpiresAt) {
				_, err = tx.ExecContext(ctx, `
					DELETE FROM config_locks WHERE target = ?
				`, req.Target)
				if err != nil {
					return NewError(ErrCodeInternal, fmt.Sprintf("failed to delete expired %s lock", req.Target), err)
				}
			} else if existingSessionID == req.SessionID {
				// Same session: extend lock
				_, err = tx.ExecContext(ctx, `
					UPDATE config_locks
					SET expires_at = ?, last_activity = ?
					WHERE target = ?
				`, expiresAt, now, req.Target)
				if err != nil {
					return NewError(ErrCodeInternal, fmt.Sprintf("failed to extend %s lock", req.Target), err)
				}

				details := fmt.Sprintf("target=%s, duration=%v", req.Target, timeout)
				_, err = tx.ExecContext(ctx, `
					INSERT INTO audit_log (session_id, action, result, user, details)
					VALUES (?, 'lock_extend', 'success', ?, ?)
				`, req.SessionID, existingUser, details)
				if err != nil {
					return NewError(ErrCodeInternal, fmt.Sprintf("failed to log %s lock extension audit event", req.Target), err)
				}

				return nil
			} else {
				return NewError(ErrCodeConflict,
					fmt.Sprintf("%s lock already held by session %s (user: %s)",
						req.Target, existingSessionID, existingUser),
					nil)
			}
		}

		// Acquire new lock (no existing lock or expired lock was removed)
		_, err = tx.ExecContext(ctx, `
			INSERT INTO config_locks (
				target, session_id, user, acquired_at, expires_at, last_activity
			) VALUES (?, ?, ?, ?, ?, ?)
		`, req.Target, req.SessionID, req.User, now, expiresAt, now)

		if err != nil {
			return NewError(ErrCodeInternal, fmt.Sprintf("failed to acquire %s lock", req.Target), err)
		}

		// Log audit event with target and timeout in details
		details := fmt.Sprintf("target=%s, timeout=%v", req.Target, timeout)
		_, err = tx.ExecContext(ctx, `
			INSERT INTO audit_log (user, session_id, action, result, details)
			VALUES (?, ?, 'lock_acquire', 'success', ?)
		`, req.User, req.SessionID, details)

		if err != nil {
			return NewError(ErrCodeInternal, fmt.Sprintf("failed to log %s lock acquisition audit event", req.Target), err)
		}

		return nil
	})
}

// ReleaseLock releases the config lock held by the specified session for a specific target.
func (ds *sqliteDatastore) ReleaseLock(ctx context.Context, target string, sessionID string) error {
	// Validate target
	if err := ValidateLockTarget(target); err != nil {
		return err
	}

	return ds.withTx(ctx, false, func(tx *sql.Tx) error {
		// Verify the lock is held by this session
		var lockSessionID string
		var lockUser string
		var expiresAt time.Time
		err := tx.QueryRowContext(ctx, `
			SELECT session_id, user, expires_at FROM config_locks WHERE target = ?
		`, target).Scan(&lockSessionID, &lockUser, &expiresAt)

		if err == sql.ErrNoRows {
			// No lock exists, nothing to release (idempotent)
			return nil
		}
		if err != nil {
			return NewError(ErrCodeInternal, fmt.Sprintf("failed to check %s lock ownership", target), err)
		}

		// Check if lock has expired (treat as if no lock exists)
		if time.Now().After(expiresAt) {
			// Lock is expired - delete it and return success
			_, _ = tx.ExecContext(ctx, `DELETE FROM config_locks WHERE target = ?`, target)
			return nil
		}

		if lockSessionID != sessionID {
			return NewError(ErrCodeConflict,
				fmt.Sprintf("%s lock is held by another session %s", target, lockSessionID), nil)
		}

		// Release lock
		_, err = tx.ExecContext(ctx, `
			DELETE FROM config_locks WHERE target = ?
		`, target)
		if err != nil {
			return NewError(ErrCodeInternal, fmt.Sprintf("failed to release %s lock", target), err)
		}

		// Log audit event with target in details
		details := fmt.Sprintf("target=%s", target)
		_, err = tx.ExecContext(ctx, `
			INSERT INTO audit_log (session_id, action, result, user, details)
			VALUES (?, 'lock_release', 'success', ?, ?)
		`, sessionID, lockUser, details)

		if err != nil {
			return NewError(ErrCodeInternal, fmt.Sprintf("failed to log %s lock release audit event", target), err)
		}

		return nil
	})
}

// ExtendLock extends the expiration time of an existing lock for a specific target.
func (ds *sqliteDatastore) ExtendLock(ctx context.Context, target string, sessionID string, duration time.Duration) error {
	// Validate target
	if err := ValidateLockTarget(target); err != nil {
		return err
	}

	if duration == 0 {
		duration = 30 * time.Minute
	}

	return ds.withTx(ctx, false, func(tx *sql.Tx) error {
		// Verify lock is held by this session and not expired
		var lockSessionID string
		var lockUser string
		var expiresAt time.Time
		err := tx.QueryRowContext(ctx, `
			SELECT session_id, user, expires_at FROM config_locks WHERE target = ?
		`, target).Scan(&lockSessionID, &lockUser, &expiresAt)

		if err == sql.ErrNoRows {
			return NewError(ErrCodeNotFound, fmt.Sprintf("no %s lock to extend", target), nil)
		}
		if err != nil {
			return NewError(ErrCodeInternal, fmt.Sprintf("failed to check %s lock", target), err)
		}

		// Check if lock has expired
		if time.Now().After(expiresAt) {
			return NewError(ErrCodeConflict,
				fmt.Sprintf("%s lock has expired, cannot extend (re-acquire lock instead)", target), nil)
		}

		if lockSessionID != sessionID {
			return NewError(ErrCodeConflict,
				fmt.Sprintf("%s lock is held by another session %s", target, lockSessionID), nil)
		}

		// Extend lock
		newExpiresAt := time.Now().Add(duration)
		now := time.Now()
		_, err = tx.ExecContext(ctx, `
			UPDATE config_locks
			SET expires_at = ?, last_activity = ?
			WHERE target = ?
		`, newExpiresAt, now, target)

		if err != nil {
			return NewError(ErrCodeInternal, fmt.Sprintf("failed to extend %s lock", target), err)
		}

		// Log audit event for lock extension
		details := fmt.Sprintf("target=%s, duration=%v", target, duration)
		_, err = tx.ExecContext(ctx, `
			INSERT INTO audit_log (session_id, action, result, user, details)
			VALUES (?, 'lock_extend', 'success', ?, ?)
		`, sessionID, lockUser, details)

		if err != nil {
			return NewError(ErrCodeInternal, fmt.Sprintf("failed to log %s lock extension audit event", target), err)
		}

		return nil
	})
}

// StealLock forcibly acquires the lock for a specific target (admin operation).
func (ds *sqliteDatastore) StealLock(ctx context.Context, req *StealLockRequest) error {
	// Validate target
	if err := ValidateLockTarget(req.Target); err != nil {
		return err
	}

	timeout := 30 * time.Minute
	expiresAt := time.Now().Add(timeout)

	return ds.withTx(ctx, false, func(tx *sql.Tx) error {
		// Get current lock holder for audit
		var oldSessionID, oldUser string
		err := tx.QueryRowContext(ctx, `
			SELECT session_id, user FROM config_locks WHERE target = ?
		`, req.Target).Scan(&oldSessionID, &oldUser)

		if err != nil && err != sql.ErrNoRows {
			return NewError(ErrCodeInternal, fmt.Sprintf("failed to check existing %s lock", req.Target), err)
		}

		// Replace lock
		_, err = tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO config_locks (
				target, session_id, user, acquired_at, expires_at, last_activity
			) VALUES (?, ?, ?, ?, ?, ?)
		`, req.Target, req.NewSessionID, req.User, time.Now(), expiresAt, time.Now())

		if err != nil {
			return NewError(ErrCodeInternal, fmt.Sprintf("failed to steal %s lock", req.Target), err)
		}

		// Log audit event with target and details
		details := fmt.Sprintf("target=%s", req.Target)
		if oldSessionID != "" {
			details += fmt.Sprintf(", stolen from session=%s (user=%s), reason=%s",
				oldSessionID, oldUser, req.Reason)
		}

		_, err = tx.ExecContext(ctx, `
			INSERT INTO audit_log (user, session_id, action, result, details)
			VALUES (?, ?, 'lock_steal', 'success', ?)
		`, req.User, req.NewSessionID, details)

		if err != nil {
			return NewError(ErrCodeInternal, fmt.Sprintf("failed to log %s lock steal audit event", req.Target), err)
		}

		return nil
	})
}

// GetLockInfo retrieves information about the current lock state for a specific target.
func (ds *sqliteDatastore) GetLockInfo(ctx context.Context, target string) (*LockInfo, error) {
	// Validate target
	if err := ValidateLockTarget(target); err != nil {
		return nil, err
	}

	var sessionID, user string
	var acquiredAt, expiresAt time.Time

	err := ds.db.QueryRowContext(ctx, `
		SELECT session_id, user, acquired_at, expires_at
		FROM config_locks
		WHERE target = ?
	`, target).Scan(&sessionID, &user, &acquiredAt, &expiresAt)

	if err == sql.ErrNoRows {
		// No lock exists
		return &LockInfo{
			IsLocked: false,
		}, nil
	}
	if err != nil {
		return nil, NewError(ErrCodeInternal, fmt.Sprintf("failed to get %s lock info", target), err)
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
