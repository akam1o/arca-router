package datastore

import (
	"context"
	"database/sql"
	"time"
)

// LogAuditEvent records an audit event to the audit log.
// This method provides application-level audit logging capability.
func (ds *sqliteDatastore) LogAuditEvent(ctx context.Context, event *AuditEvent) error {
	return ds.withTx(ctx, false, func(tx *sql.Tx) error {
		// Set timestamp if not provided
		if event.Timestamp.IsZero() {
			event.Timestamp = time.Now()
		}

		_, err := tx.ExecContext(ctx, `
			INSERT INTO audit_log (
				timestamp, user, session_id, source_ip, correlation_id,
				action, result, error_code, details
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			event.Timestamp,
			event.User,
			event.SessionID,
			event.SourceIP,
			event.CorrelationID,
			event.Action,
			event.Result,
			event.ErrorCode,
			event.Details,
		)

		if err != nil {
			return NewError(ErrCodeInternal, "failed to log audit event", err)
		}

		return nil
	})
}

// CleanupAuditLog deletes audit log entries older than the specified cutoff time
func (ds *sqliteDatastore) CleanupAuditLog(ctx context.Context, cutoff time.Time) (int64, error) {
	var deletedCount int64

	err := ds.withTx(ctx, false, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `
			DELETE FROM audit_log
			WHERE timestamp < ?
		`, cutoff)

		if err != nil {
			return NewError(ErrCodeInternal, "failed to cleanup audit log", err)
		}

		deletedCount, err = result.RowsAffected()
		if err != nil {
			return NewError(ErrCodeInternal, "failed to get deleted count", err)
		}

		return nil
	})

	if err != nil {
		return 0, err
	}

	return deletedCount, nil
}
