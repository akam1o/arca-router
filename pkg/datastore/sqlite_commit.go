package datastore

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Commit promotes the candidate configuration to running configuration.
// This operation is atomic within the database, but VPP/FRR application
// happens outside the transaction (see docs/datastore-design.md for details).
func (ds *sqliteDatastore) Commit(ctx context.Context, req *CommitRequest) (string, error) {
	// Generate commit ID
	commitID := uuid.New().String()
	now := time.Now()

	// Load candidate config
	candidate, err := ds.GetCandidate(ctx, req.SessionID)
	if err != nil {
		return "", err // Already wrapped by GetCandidate
	}

	// Execute commit transaction
	err = ds.withTx(ctx, false, func(tx *sql.Tx) error {
		// 0. Verify the session holds a valid (non-expired) config lock (enforces exclusive access)
		var lockSessionID string
		var expiresAt time.Time
		err := tx.QueryRowContext(ctx, `
			SELECT session_id, expires_at FROM config_locks WHERE lock_id = 1
		`).Scan(&lockSessionID, &expiresAt)

		if err == sql.ErrNoRows {
			return NewError(ErrCodeConflict,
				"cannot commit: no config lock held (lock must be acquired before commit)", nil)
		}
		if err != nil {
			return NewError(ErrCodeInternal, "failed to check lock ownership", err)
		}

		// Check if lock has expired
		if now.After(expiresAt) {
			return NewError(ErrCodeConflict,
				"cannot commit: config lock has expired (re-acquire lock before commit)", nil)
		}

		if lockSessionID != req.SessionID {
			return NewError(ErrCodeConflict,
				fmt.Sprintf("cannot commit: config lock is held by another session (%s)", lockSessionID), nil)
		}

		// 1. Update all running_config rows to is_current = 0
		_, err = tx.ExecContext(ctx, `
			UPDATE running_config SET is_current = 0 WHERE is_current = 1
		`)
		if err != nil {
			return NewError(ErrCodeInternal, "failed to update running config flags", err)
		}

		// 2. Insert new running config with is_current = 1
		_, err = tx.ExecContext(ctx, `
			INSERT INTO running_config (commit_id, config_text, timestamp, is_current)
			VALUES (?, ?, ?, 1)
		`, commitID, candidate.ConfigText, now)
		if err != nil {
			return NewError(ErrCodeInternal, "failed to insert new running config", err)
		}

		// 3. Insert commit history
		_, err = tx.ExecContext(ctx, `
			INSERT INTO commit_history (commit_id, user, timestamp, message, config_text, is_rollback, source_ip)
			VALUES (?, ?, ?, ?, ?, 0, ?)
		`, commitID, req.User, now, req.Message, candidate.ConfigText, req.SourceIP)
		if err != nil {
			return NewError(ErrCodeInternal, "failed to insert commit history", err)
		}

		// 4. Delete candidate config
		_, err = tx.ExecContext(ctx, `
			DELETE FROM candidate_configs WHERE session_id = ?
		`, req.SessionID)
		if err != nil {
			return NewError(ErrCodeInternal, "failed to delete candidate config", err)
		}

		// 5. Release lock (if held by this session)
		_, err = tx.ExecContext(ctx, `
			DELETE FROM config_locks WHERE session_id = ?
		`, req.SessionID)
		if err != nil {
			return NewError(ErrCodeInternal, "failed to release lock", err)
		}

		// 6. Log audit event
		_, err = tx.ExecContext(ctx, `
			INSERT INTO audit_log (user, session_id, source_ip, action, result, details)
			VALUES (?, ?, ?, 'commit', 'success', ?)
		`, req.User, req.SessionID, req.SourceIP, fmt.Sprintf("commit_id: %s", commitID))
		if err != nil {
			return NewError(ErrCodeInternal, "failed to log audit event", err)
		}

		return nil
	})

	if err != nil {
		return "", err
	}

	return commitID, nil
}

// Rollback rolls back to a previous commit.
func (ds *sqliteDatastore) Rollback(ctx context.Context, req *RollbackRequest) (string, error) {
	// Generate new commit ID for the rollback commit
	newCommitID := uuid.New().String()
	now := time.Now()

	// Load target commit
	targetCommit, err := ds.GetCommit(ctx, req.CommitID)
	if err != nil {
		return "", err // Already wrapped by GetCommit
	}

	// Execute rollback transaction
	err = ds.withTx(ctx, false, func(tx *sql.Tx) error {
		// 1. Update all running_config rows to is_current = 0
		_, err := tx.ExecContext(ctx, `
			UPDATE running_config SET is_current = 0 WHERE is_current = 1
		`)
		if err != nil {
			return NewError(ErrCodeInternal, "failed to update running config flags", err)
		}

		// 2. Insert new running config with target commit's config_text
		_, err = tx.ExecContext(ctx, `
			INSERT INTO running_config (commit_id, config_text, timestamp, is_current)
			VALUES (?, ?, ?, 1)
		`, newCommitID, targetCommit.ConfigText, now)
		if err != nil {
			return NewError(ErrCodeInternal, "failed to insert rollback running config", err)
		}

		// 3. Insert commit history with is_rollback = 1
		message := req.Message
		if message == "" {
			message = fmt.Sprintf("rollback to commit %s", req.CommitID)
		}

		_, err = tx.ExecContext(ctx, `
			INSERT INTO commit_history (commit_id, user, timestamp, message, config_text, is_rollback, source_ip)
			VALUES (?, ?, ?, ?, ?, 1, ?)
		`, newCommitID, req.User, now, message, targetCommit.ConfigText, req.SourceIP)
		if err != nil {
			return NewError(ErrCodeInternal, "failed to insert rollback history", err)
		}

		// 4. Log audit event
		_, err = tx.ExecContext(ctx, `
			INSERT INTO audit_log (user, source_ip, action, result, details)
			VALUES (?, ?, 'rollback', 'success', ?)
		`, req.User, req.SourceIP, fmt.Sprintf("new_commit_id: %s, target_commit_id: %s", newCommitID, req.CommitID))
		if err != nil {
			return NewError(ErrCodeInternal, "failed to log audit event", err)
		}

		return nil
	})

	if err != nil {
		return "", err
	}

	return newCommitID, nil
}

// ListCommitHistory retrieves commit history with optional filtering.
func (ds *sqliteDatastore) ListCommitHistory(ctx context.Context, opts *HistoryOptions) ([]*CommitHistoryEntry, error) {
	// Handle nil opts (use defaults)
	if opts == nil {
		opts = &HistoryOptions{}
	}

	// Build query with filters
	query := `
		SELECT commit_id, user, timestamp, message, config_text, is_rollback, source_ip
		FROM commit_history
		WHERE 1=1
	`
	args := []interface{}{}

	if !opts.StartTime.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, opts.StartTime)
	}

	if !opts.EndTime.IsZero() {
		query += " AND timestamp <= ?"
		args = append(args, opts.EndTime)
	}

	if opts.User != "" {
		query += " AND user = ?"
		args = append(args, opts.User)
	}

	if opts.ExcludeRollbacks {
		query += " AND is_rollback = 0"
	}

	// Order by timestamp descending (newest first)
	query += " ORDER BY timestamp DESC"

	// Apply limit and offset
	if opts.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, opts.Limit)
	}

	if opts.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, opts.Offset)
	}

	// Execute query
	rows, err := ds.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, NewError(ErrCodeInternal, "failed to query commit history", err)
	}
	defer rows.Close()

	// Scan results
	var entries []*CommitHistoryEntry
	for rows.Next() {
		var entry CommitHistoryEntry
		var message sql.NullString
		var sourceIP sql.NullString

		err := rows.Scan(
			&entry.CommitID,
			&entry.User,
			&entry.Timestamp,
			&message,
			&entry.ConfigText,
			&entry.IsRollback,
			&sourceIP,
		)
		if err != nil {
			return nil, NewError(ErrCodeInternal, "failed to scan commit history row", err)
		}

		if message.Valid {
			entry.Message = message.String
		}
		if sourceIP.Valid {
			entry.SourceIP = sourceIP.String
		}

		entries = append(entries, &entry)
	}

	if err := rows.Err(); err != nil {
		return nil, NewError(ErrCodeInternal, "error iterating commit history", err)
	}

	return entries, nil
}

// GetCommit retrieves a specific commit by ID.
func (ds *sqliteDatastore) GetCommit(ctx context.Context, commitID string) (*CommitHistoryEntry, error) {
	var entry CommitHistoryEntry
	var message sql.NullString
	var sourceIP sql.NullString

	err := ds.db.QueryRowContext(ctx, `
		SELECT commit_id, user, timestamp, message, config_text, is_rollback, source_ip
		FROM commit_history
		WHERE commit_id = ?
	`, commitID).Scan(
		&entry.CommitID,
		&entry.User,
		&entry.Timestamp,
		&message,
		&entry.ConfigText,
		&entry.IsRollback,
		&sourceIP,
	)

	if err == sql.ErrNoRows {
		return nil, NewError(ErrCodeNotFound, "commit not found", nil)
	}
	if err != nil {
		return nil, NewError(ErrCodeInternal, "failed to get commit", err)
	}

	if message.Valid {
		entry.Message = message.String
	}
	if sourceIP.Valid {
		entry.SourceIP = sourceIP.String
	}

	return &entry, nil
}
