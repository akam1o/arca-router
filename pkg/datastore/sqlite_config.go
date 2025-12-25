package datastore

import (
	"context"
	"database/sql"
	"time"
)

// GetRunning retrieves the current running configuration.
func (ds *sqliteDatastore) GetRunning(ctx context.Context) (*RunningConfig, error) {
	var commitID, configText string
	var timestamp time.Time

	err := ds.db.QueryRowContext(ctx, `
		SELECT commit_id, config_text, timestamp
		FROM running_config
		WHERE is_current = 1
	`).Scan(&commitID, &configText, &timestamp)

	if err == sql.ErrNoRows {
		// No running config exists (first startup)
		return nil, NewError(ErrCodeNotFound, "no running configuration found", nil)
	}
	if err != nil {
		return nil, NewError(ErrCodeInternal, "failed to get running config", err)
	}

	return &RunningConfig{
		CommitID:   commitID,
		ConfigText: configText,
		Timestamp:  timestamp,
	}, nil
}

// GetCandidate retrieves the candidate configuration for a session.
func (ds *sqliteDatastore) GetCandidate(ctx context.Context, sessionID string) (*CandidateConfig, error) {
	var configText string
	var createdAt, updatedAt time.Time

	err := ds.db.QueryRowContext(ctx, `
		SELECT config_text, created_at, updated_at
		FROM candidate_configs
		WHERE session_id = ?
	`, sessionID).Scan(&configText, &createdAt, &updatedAt)

	if err == sql.ErrNoRows {
		// No candidate exists for this session
		return nil, NewError(ErrCodeNotFound, "no candidate configuration found for session", nil)
	}
	if err != nil {
		return nil, NewError(ErrCodeInternal, "failed to get candidate config", err)
	}

	return &CandidateConfig{
		SessionID:  sessionID,
		ConfigText: configText,
		CreatedAt:  createdAt,
		UpdatedAt:  updatedAt,
	}, nil
}

// SaveCandidate saves or updates the candidate configuration for a session.
func (ds *sqliteDatastore) SaveCandidate(ctx context.Context, sessionID string, configText string) error {
	return ds.withTx(ctx, false, func(tx *sql.Tx) error {
		now := time.Now()

		// Use INSERT OR REPLACE to handle both create and update
		_, err := tx.ExecContext(ctx, `
			INSERT INTO candidate_configs (session_id, config_text, created_at, updated_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(session_id) DO UPDATE SET
				config_text = excluded.config_text,
				updated_at = excluded.updated_at
		`, sessionID, configText, now, now)

		if err != nil {
			return NewError(ErrCodeInternal, "failed to save candidate config", err)
		}

		return nil
	})
}

// DeleteCandidate deletes the candidate configuration for a session.
func (ds *sqliteDatastore) DeleteCandidate(ctx context.Context, sessionID string) error {
	return ds.withTx(ctx, false, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			DELETE FROM candidate_configs
			WHERE session_id = ?
		`, sessionID)

		if err != nil {
			return NewError(ErrCodeInternal, "failed to delete candidate config", err)
		}

		// Check if any rows were deleted
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return NewError(ErrCodeInternal, "failed to check delete result", err)
		}

		if rowsAffected == 0 {
			// No candidate existed, but this is not an error (idempotent delete)
			return nil
		}

		return nil
	})
}
