package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/akam1o/arca-router/pkg/datastore"
)

// EventType represents the type of audit event
type EventType string

const (
	// Authentication events
	EventAuthSuccess EventType = "auth_success"
	EventAuthFailure EventType = "auth_failure"

	// Authorization events
	EventAccessDenied EventType = "access_denied"

	// Configuration events
	EventEditConfig     EventType = "edit_config"
	EventCommit         EventType = "commit"
	EventCommitFailed   EventType = "commit_failed"
	EventRollback       EventType = "rollback"
	EventRollbackFailed EventType = "rollback_failed"
	EventDiscardChanges EventType = "discard_changes"

	// Lock events
	EventLockAcquired EventType = "lock_acquired"
	EventLockReleased EventType = "lock_released"
	EventLockStolen   EventType = "lock_stolen"
	EventLockFailed   EventType = "lock_failed"

	// Session events
	EventSessionCreated    EventType = "session_created"
	EventSessionTerminated EventType = "session_terminated"
)

// Result represents the outcome of an operation
type Result string

const (
	ResultSuccess Result = "success"
	ResultFailure Result = "failure"
	ResultDenied  Result = "denied"
)

// Event represents a structured audit event
type Event struct {
	Timestamp     time.Time              `json:"timestamp"`
	EventType     EventType              `json:"event_type"`
	User          string                 `json:"user,omitempty"`
	SessionID     string                 `json:"session_id,omitempty"`
	SourceIP      string                 `json:"source_ip,omitempty"`
	Action        string                 `json:"action,omitempty"`
	Result        Result                 `json:"result"`
	ErrorCode     string                 `json:"error_code,omitempty"`
	ErrorMessage  string                 `json:"error_message,omitempty"`
	Details       map[string]interface{} `json:"details,omitempty"`
	CorrelationID string                 `json:"correlation_id,omitempty"`
}

// Logger provides structured audit logging
type Logger struct {
	datastore datastore.Datastore
	slogger   *slog.Logger
	retention time.Duration // How long to keep audit logs
}

// NewLogger creates a new audit logger
func NewLogger(ds datastore.Datastore, slogger *slog.Logger) *Logger {
	if slogger == nil {
		slogger = slog.Default()
	}

	return &Logger{
		datastore: ds,
		slogger:   slogger,
		retention: 90 * 24 * time.Hour, // 90 days default
	}
}

// Log records an audit event
func (l *Logger) Log(ctx context.Context, event *Event) error {
	// Set timestamp if not provided
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Ensure Details map exists
	if event.Details == nil {
		event.Details = make(map[string]interface{})
	}

	// Add error message to details if present (before marshaling)
	if event.ErrorMessage != "" {
		event.Details["error_message"] = event.ErrorMessage
	}

	// Add event type and action to details for better audit trail
	event.Details["event_type"] = string(event.EventType)
	if event.Action != "" {
		event.Details["attempted_action"] = event.Action
	}

	// Convert to datastore event format
	dsEvent := &datastore.AuditEvent{
		Timestamp:     event.Timestamp,
		User:          event.User,
		SessionID:     event.SessionID,
		SourceIP:      event.SourceIP,
		CorrelationID: event.CorrelationID,
		Action:        string(event.EventType), // Store event type in action field
		Result:        string(event.Result),
		ErrorCode:     event.ErrorCode,
	}

	// Marshal details to JSON (now includes error_message and attempted_action)
	detailsJSON, err := json.Marshal(event.Details)
	if err != nil {
		l.slogger.WarnContext(ctx, "Failed to marshal audit event details",
			"error", err,
			"event_type", event.EventType)
		// Continue without details rather than failing the audit log
	} else {
		dsEvent.Details = string(detailsJSON)
	}

	// Persist to datastore
	if err := l.datastore.LogAuditEvent(ctx, dsEvent); err != nil {
		l.slogger.ErrorContext(ctx, "Failed to persist audit event",
			"error", err,
			"event_type", event.EventType,
			"user", event.User)
		return fmt.Errorf("failed to persist audit event: %w", err)
	}

	// Log to structured logger for real-time monitoring
	l.logToSlog(ctx, event)

	return nil
}

// logToSlog writes the event to the structured logger
func (l *Logger) logToSlog(ctx context.Context, event *Event) {
	attrs := []any{
		"event_type", event.EventType,
		"result", event.Result,
		"timestamp", event.Timestamp.Format(time.RFC3339),
	}

	if event.User != "" {
		attrs = append(attrs, "user", event.User)
	}
	if event.SessionID != "" {
		attrs = append(attrs, "session_id", event.SessionID)
	}
	if event.SourceIP != "" {
		attrs = append(attrs, "source_ip", event.SourceIP)
	}
	if event.Action != "" {
		attrs = append(attrs, "action", event.Action)
	}
	if event.ErrorCode != "" {
		attrs = append(attrs, "error_code", event.ErrorCode)
	}
	if event.ErrorMessage != "" {
		attrs = append(attrs, "error_message", event.ErrorMessage)
	}
	if event.CorrelationID != "" {
		attrs = append(attrs, "correlation_id", event.CorrelationID)
	}

	// Add details as individual fields
	for k, v := range event.Details {
		attrs = append(attrs, k, v)
	}

	// Choose log level based on result
	switch event.Result {
	case ResultFailure, ResultDenied:
		l.slogger.WarnContext(ctx, fmt.Sprintf("Audit: %s", event.EventType), attrs...)
	default:
		l.slogger.InfoContext(ctx, fmt.Sprintf("Audit: %s", event.EventType), attrs...)
	}
}

// LogAuthSuccess logs a successful authentication
func (l *Logger) LogAuthSuccess(ctx context.Context, username, sourceIP, method string) error {
	return l.Log(ctx, &Event{
		EventType: EventAuthSuccess,
		User:      username,
		SourceIP:  sourceIP,
		Result:    ResultSuccess,
		Details: map[string]interface{}{
			"method": method,
		},
	})
}

// LogAuthFailure logs a failed authentication attempt
func (l *Logger) LogAuthFailure(ctx context.Context, username, sourceIP, method, reason string) error {
	return l.Log(ctx, &Event{
		EventType:    EventAuthFailure,
		User:         username,
		SourceIP:     sourceIP,
		Result:       ResultFailure,
		ErrorMessage: reason,
		Details: map[string]interface{}{
			"method": method,
			"reason": reason,
		},
	})
}

// LogAccessDenied logs an authorization denial (RBAC)
func (l *Logger) LogAccessDenied(ctx context.Context, user, sessionID, action, role, reason string) error {
	return l.Log(ctx, &Event{
		EventType:    EventAccessDenied,
		User:         user,
		SessionID:    sessionID,
		Action:       action,
		Result:       ResultDenied,
		ErrorMessage: reason,
		Details: map[string]interface{}{
			"role":   role,
			"reason": reason,
		},
	})
}

// LogCommit logs a configuration commit
func (l *Logger) LogCommit(ctx context.Context, user, sessionID, commitID string, success bool, errorMsg string) error {
	eventType := EventCommit
	result := ResultSuccess
	if !success {
		eventType = EventCommitFailed
		result = ResultFailure
	}

	return l.Log(ctx, &Event{
		EventType:    eventType,
		User:         user,
		SessionID:    sessionID,
		Result:       result,
		ErrorMessage: errorMsg,
		Details: map[string]interface{}{
			"commit_id": commitID,
		},
		CorrelationID: commitID,
	})
}

// LogRollback logs a configuration rollback
func (l *Logger) LogRollback(ctx context.Context, user, sessionID, commitID string, success bool, errorMsg string) error {
	eventType := EventRollback
	result := ResultSuccess
	if !success {
		eventType = EventRollbackFailed
		result = ResultFailure
	}

	return l.Log(ctx, &Event{
		EventType:    eventType,
		User:         user,
		SessionID:    sessionID,
		Result:       result,
		ErrorMessage: errorMsg,
		Details: map[string]interface{}{
			"commit_id": commitID,
		},
		CorrelationID: commitID,
	})
}

// LogEditConfig logs a configuration edit operation
func (l *Logger) LogEditConfig(ctx context.Context, user, sessionID, target, operation string) error {
	return l.Log(ctx, &Event{
		EventType: EventEditConfig,
		User:      user,
		SessionID: sessionID,
		Action:    operation,
		Result:    ResultSuccess,
		Details: map[string]interface{}{
			"target":    target,
			"operation": operation,
		},
	})
}

// LogDiscardChanges logs discarding uncommitted changes
func (l *Logger) LogDiscardChanges(ctx context.Context, user, sessionID string) error {
	return l.Log(ctx, &Event{
		EventType: EventDiscardChanges,
		User:      user,
		SessionID: sessionID,
		Result:    ResultSuccess,
	})
}

// LogLockAcquired logs successful lock acquisition
func (l *Logger) LogLockAcquired(ctx context.Context, user, sessionID, target string) error {
	return l.Log(ctx, &Event{
		EventType: EventLockAcquired,
		User:      user,
		SessionID: sessionID,
		Result:    ResultSuccess,
		Details: map[string]interface{}{
			"target": target,
		},
	})
}

// LogLockReleased logs successful lock release
func (l *Logger) LogLockReleased(ctx context.Context, user, sessionID, target string) error {
	return l.Log(ctx, &Event{
		EventType: EventLockReleased,
		User:      user,
		SessionID: sessionID,
		Result:    ResultSuccess,
		Details: map[string]interface{}{
			"target": target,
		},
	})
}

// LogLockStolen logs when an admin steals a lock (future feature)
func (l *Logger) LogLockStolen(ctx context.Context, admin, adminSession, target, previousOwner string) error {
	return l.Log(ctx, &Event{
		EventType: EventLockStolen,
		User:      admin,
		SessionID: adminSession,
		Result:    ResultSuccess,
		Details: map[string]interface{}{
			"target":         target,
			"previous_owner": previousOwner,
		},
	})
}

// LogLockFailed logs a failed lock acquisition
func (l *Logger) LogLockFailed(ctx context.Context, user, sessionID, target, reason string) error {
	return l.Log(ctx, &Event{
		EventType:    EventLockFailed,
		User:         user,
		SessionID:    sessionID,
		Result:       ResultFailure,
		ErrorMessage: reason,
		Details: map[string]interface{}{
			"target": target,
			"reason": reason,
		},
	})
}

// LogSessionCreated logs session creation
func (l *Logger) LogSessionCreated(ctx context.Context, user, sessionID, sourceIP string) error {
	return l.Log(ctx, &Event{
		EventType: EventSessionCreated,
		User:      user,
		SessionID: sessionID,
		SourceIP:  sourceIP,
		Result:    ResultSuccess,
	})
}

// LogSessionTerminated logs session termination
func (l *Logger) LogSessionTerminated(ctx context.Context, user, sessionID string, reason string) error {
	return l.Log(ctx, &Event{
		EventType: EventSessionTerminated,
		User:      user,
		SessionID: sessionID,
		Result:    ResultSuccess,
		Details: map[string]interface{}{
			"reason": reason,
		},
	})
}

// Cleanup removes audit logs older than the retention period
// Should be called periodically (e.g., daily)
func (l *Logger) Cleanup(ctx context.Context) error {
	// Calculate cutoff time
	cutoff := time.Now().Add(-l.retention)

	l.slogger.InfoContext(ctx, "Starting audit log cleanup",
		"retention_days", l.retention.Hours()/24,
		"cutoff", cutoff.Format(time.RFC3339))

	// Execute cleanup operation
	// For Phase 3, we implement the cleanup logic with proper structure
	// The actual deletion would require datastore access
	deletedCount, err := l.executeCleanup(ctx, cutoff)
	if err != nil {
		l.slogger.ErrorContext(ctx, "Failed to cleanup audit logs",
			"error", err)
		return err
	}

	l.slogger.InfoContext(ctx, "Audit log cleanup completed",
		"deleted_count", deletedCount)
	return nil
}

// executeCleanup executes the cleanup operation
func (l *Logger) executeCleanup(ctx context.Context, cutoff time.Time) (int64, error) {
	// Use datastore to delete old audit logs
	if l.datastore == nil {
		l.slogger.WarnContext(ctx, "Datastore not available, skipping audit log cleanup")
		return 0, nil
	}

	// Execute cleanup via datastore
	deletedCount, err := l.datastore.CleanupAuditLog(ctx, cutoff)
	if err != nil {
		return 0, err
	}

	return deletedCount, nil
}

// SetRetention sets the retention period for audit logs
func (l *Logger) SetRetention(retention time.Duration) {
	l.retention = retention
}
