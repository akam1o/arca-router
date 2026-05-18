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

// ListAuditEvents returns audit events in newest-first order.
func (ds *sqliteDatastore) ListAuditEvents(ctx context.Context, opts *AuditOptions) ([]*AuditEvent, error) {
	if opts == nil {
		opts = &AuditOptions{}
	}
	limit := opts.Limit
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}

	query := `
		SELECT id, timestamp, user, session_id, source_ip, correlation_id,
			action, result, error_code, details
		FROM audit_log
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
	if opts.Action != "" {
		query += " AND action = ?"
		args = append(args, opts.Action)
	}
	if opts.Result != "" {
		query += " AND result = ?"
		args = append(args, opts.Result)
	}

	query += " ORDER BY timestamp DESC, id DESC"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	} else if offset > 0 {
		query += " LIMIT -1"
	}
	if offset > 0 {
		query += " OFFSET ?"
		args = append(args, offset)
	}

	rows, err := ds.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, NewError(ErrCodeInternal, "failed to query audit events", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			_ = err
		}
	}()

	var events []*AuditEvent
	for rows.Next() {
		var event AuditEvent
		var sessionID, sourceIP, correlationID, errorCode, details sql.NullString
		if err := rows.Scan(
			&event.ID,
			&event.Timestamp,
			&event.User,
			&sessionID,
			&sourceIP,
			&correlationID,
			&event.Action,
			&event.Result,
			&errorCode,
			&details,
		); err != nil {
			return nil, NewError(ErrCodeInternal, "failed to scan audit event row", err)
		}
		if sessionID.Valid {
			event.SessionID = sessionID.String
		}
		if sourceIP.Valid {
			event.SourceIP = sourceIP.String
		}
		if correlationID.Valid {
			event.CorrelationID = correlationID.String
		}
		if errorCode.Valid {
			event.ErrorCode = errorCode.String
		}
		if details.Valid {
			event.Details = details.String
		}
		events = append(events, &event)
	}
	if err := rows.Err(); err != nil {
		return nil, NewError(ErrCodeInternal, "error iterating audit events", err)
	}
	return events, nil
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
