package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/akam1o/arca-router/pkg/datastore"
)

// mockDatastore implements minimal datastore.Datastore interface for testing
type mockDatastore struct {
	events []*datastore.AuditEvent
}

func (m *mockDatastore) LogAuditEvent(ctx context.Context, event *datastore.AuditEvent) error {
	m.events = append(m.events, event)
	return nil
}
func (m *mockDatastore) CleanupAuditLog(ctx context.Context, cutoff time.Time) (int64, error) {
	return 0, nil
}

// Implement other required interface methods as no-ops
func (m *mockDatastore) GetRunning(ctx context.Context) (*datastore.RunningConfig, error) {
	return nil, nil
}
func (m *mockDatastore) GetCandidate(ctx context.Context, sessionID string) (*datastore.CandidateConfig, error) {
	return nil, nil
}
func (m *mockDatastore) SaveCandidate(ctx context.Context, sessionID string, configText string) error {
	return nil
}
func (m *mockDatastore) DeleteCandidate(ctx context.Context, sessionID string) error { return nil }
func (m *mockDatastore) Commit(ctx context.Context, req *datastore.CommitRequest) (string, error) {
	return "", nil
}
func (m *mockDatastore) Rollback(ctx context.Context, req *datastore.RollbackRequest) (string, error) {
	return "", nil
}
func (m *mockDatastore) CompareCandidateRunning(ctx context.Context, sessionID string) (*datastore.DiffResult, error) {
	return nil, nil
}
func (m *mockDatastore) CompareCommits(ctx context.Context, commitID1, commitID2 string) (*datastore.DiffResult, error) {
	return nil, nil
}
func (m *mockDatastore) AcquireLock(ctx context.Context, req *datastore.LockRequest) error {
	return nil
}
func (m *mockDatastore) ReleaseLock(ctx context.Context, target, sessionID string) error { return nil }
func (m *mockDatastore) ExtendLock(ctx context.Context, target, sessionID string, duration time.Duration) error {
	return nil
}
func (m *mockDatastore) StealLock(ctx context.Context, req *datastore.StealLockRequest) error {
	return nil
}
func (m *mockDatastore) GetLockInfo(ctx context.Context, target string) (*datastore.LockInfo, error) {
	return nil, nil
}
func (m *mockDatastore) ListCommitHistory(ctx context.Context, opts *datastore.HistoryOptions) ([]*datastore.CommitHistoryEntry, error) {
	return nil, nil
}
func (m *mockDatastore) GetCommit(ctx context.Context, commitID string) (*datastore.CommitHistoryEntry, error) {
	return nil, nil
}
func (m *mockDatastore) Close() error { return nil }

func TestNewLogger(t *testing.T) {
	ds := &mockDatastore{}
	logger := NewLogger(ds, nil)

	if logger == nil {
		t.Fatal("NewLogger returned nil")
	}
	if logger.datastore != ds {
		t.Error("Datastore not set correctly")
	}
	if logger.retention != 90*24*time.Hour {
		t.Errorf("Expected default retention of 90 days, got %v", logger.retention)
	}
}

func TestLogAuthSuccess(t *testing.T) {
	ds := &mockDatastore{}
	logger := NewLogger(ds, slog.New(slog.NewTextHandler(os.Stdout, nil)))

	ctx := context.Background()
	err := logger.LogAuthSuccess(ctx, "alice", "192.168.1.100", "password")
	if err != nil {
		t.Fatalf("LogAuthSuccess failed: %v", err)
	}

	if len(ds.events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(ds.events))
	}

	event := ds.events[0]
	if event.User != "alice" {
		t.Errorf("Expected user 'alice', got '%s'", event.User)
	}
	if event.SourceIP != "192.168.1.100" {
		t.Errorf("Expected source IP '192.168.1.100', got '%s'", event.SourceIP)
	}
	if event.Action != string(EventAuthSuccess) {
		t.Errorf("Expected action '%s', got '%s'", EventAuthSuccess, event.Action)
	}
	if event.Result != string(ResultSuccess) {
		t.Errorf("Expected result '%s', got '%s'", ResultSuccess, event.Result)
	}

	// Verify details contain method
	if !strings.Contains(event.Details, "password") {
		t.Error("Details should contain authentication method")
	}
}

func TestLogAuthFailure(t *testing.T) {
	ds := &mockDatastore{}
	logger := NewLogger(ds, slog.New(slog.NewTextHandler(os.Stdout, nil)))

	ctx := context.Background()
	err := logger.LogAuthFailure(ctx, "bob", "192.168.1.101", "password", "invalid_password")
	if err != nil {
		t.Fatalf("LogAuthFailure failed: %v", err)
	}

	if len(ds.events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(ds.events))
	}

	event := ds.events[0]
	if event.Action != string(EventAuthFailure) {
		t.Errorf("Expected action '%s', got '%s'", EventAuthFailure, event.Action)
	}
	if event.Result != string(ResultFailure) {
		t.Errorf("Expected result '%s', got '%s'", ResultFailure, event.Result)
	}

	// Verify details contain reason
	if !strings.Contains(event.Details, "invalid_password") {
		t.Error("Details should contain failure reason")
	}
}

func TestLogAccessDenied(t *testing.T) {
	ds := &mockDatastore{}
	logger := NewLogger(ds, slog.New(slog.NewTextHandler(os.Stdout, nil)))

	ctx := context.Background()
	err := logger.LogAccessDenied(ctx, "charlie", "session-123", "kill-session", "operator", "insufficient privileges")
	if err != nil {
		t.Fatalf("LogAccessDenied failed: %v", err)
	}

	if len(ds.events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(ds.events))
	}

	event := ds.events[0]
	if event.Action != string(EventAccessDenied) {
		t.Errorf("Expected action '%s', got '%s'", EventAccessDenied, event.Action)
	}
	if event.Result != string(ResultDenied) {
		t.Errorf("Expected result '%s', got '%s'", ResultDenied, event.Result)
	}
	if event.SessionID != "session-123" {
		t.Errorf("Expected session ID 'session-123', got '%s'", event.SessionID)
	}
}

func TestLogCommit(t *testing.T) {
	ds := &mockDatastore{}
	logger := NewLogger(ds, slog.New(slog.NewTextHandler(os.Stdout, nil)))

	ctx := context.Background()

	// Test successful commit
	err := logger.LogCommit(ctx, "alice", "session-456", "commit-abc123", true, "")
	if err != nil {
		t.Fatalf("LogCommit (success) failed: %v", err)
	}

	if len(ds.events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(ds.events))
	}

	event := ds.events[0]
	if event.Action != string(EventCommit) {
		t.Errorf("Expected action '%s', got '%s'", EventCommit, event.Action)
	}
	if event.Result != string(ResultSuccess) {
		t.Errorf("Expected result '%s', got '%s'", ResultSuccess, event.Result)
	}
	if event.CorrelationID != "commit-abc123" {
		t.Errorf("Expected correlation ID 'commit-abc123', got '%s'", event.CorrelationID)
	}

	// Test failed commit
	err = logger.LogCommit(ctx, "bob", "session-789", "commit-def456", false, "validation error")
	if err != nil {
		t.Fatalf("LogCommit (failure) failed: %v", err)
	}

	if len(ds.events) != 2 {
		t.Fatalf("Expected 2 events, got %d", len(ds.events))
	}

	event = ds.events[1]
	if event.Action != string(EventCommitFailed) {
		t.Errorf("Expected action '%s', got '%s'", EventCommitFailed, event.Action)
	}
	if event.Result != string(ResultFailure) {
		t.Errorf("Expected result '%s', got '%s'", ResultFailure, event.Result)
	}
}

func TestLogRollback(t *testing.T) {
	ds := &mockDatastore{}
	logger := NewLogger(ds, slog.New(slog.NewTextHandler(os.Stdout, nil)))

	ctx := context.Background()
	err := logger.LogRollback(ctx, "alice", "session-123", "commit-xyz", true, "")
	if err != nil {
		t.Fatalf("LogRollback failed: %v", err)
	}

	if len(ds.events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(ds.events))
	}

	event := ds.events[0]
	if event.Action != string(EventRollback) {
		t.Errorf("Expected action '%s', got '%s'", EventRollback, event.Action)
	}
	if !strings.Contains(event.Details, "commit-xyz") {
		t.Error("Details should contain commit ID")
	}
}

func TestLogEditConfig(t *testing.T) {
	ds := &mockDatastore{}
	logger := NewLogger(ds, slog.New(slog.NewTextHandler(os.Stdout, nil)))

	ctx := context.Background()
	err := logger.LogEditConfig(ctx, "alice", "session-123", "candidate", "merge")
	if err != nil {
		t.Fatalf("LogEditConfig failed: %v", err)
	}

	if len(ds.events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(ds.events))
	}

	event := ds.events[0]
	if event.Action != string(EventEditConfig) {
		t.Errorf("Expected action '%s', got '%s'", EventEditConfig, event.Action)
	}
}

func TestLogLockOperations(t *testing.T) {
	ds := &mockDatastore{}
	logger := NewLogger(ds, slog.New(slog.NewTextHandler(os.Stdout, nil)))

	ctx := context.Background()

	// Test lock acquired
	err := logger.LogLockAcquired(ctx, "alice", "session-123", "candidate")
	if err != nil {
		t.Fatalf("LogLockAcquired failed: %v", err)
	}

	// Test lock released
	err = logger.LogLockReleased(ctx, "alice", "session-123", "candidate")
	if err != nil {
		t.Fatalf("LogLockReleased failed: %v", err)
	}

	// Test lock failed
	err = logger.LogLockFailed(ctx, "bob", "session-456", "candidate", "already locked by alice")
	if err != nil {
		t.Fatalf("LogLockFailed failed: %v", err)
	}

	if len(ds.events) != 3 {
		t.Fatalf("Expected 3 events, got %d", len(ds.events))
	}

	// Verify lock acquired
	if ds.events[0].Action != string(EventLockAcquired) {
		t.Errorf("Expected action '%s', got '%s'", EventLockAcquired, ds.events[0].Action)
	}

	// Verify lock released
	if ds.events[1].Action != string(EventLockReleased) {
		t.Errorf("Expected action '%s', got '%s'", EventLockReleased, ds.events[1].Action)
	}

	// Verify lock failed
	if ds.events[2].Action != string(EventLockFailed) {
		t.Errorf("Expected action '%s', got '%s'", EventLockFailed, ds.events[2].Action)
	}
	if ds.events[2].Result != string(ResultFailure) {
		t.Errorf("Expected result '%s', got '%s'", ResultFailure, ds.events[2].Result)
	}
}

func TestLogSessionEvents(t *testing.T) {
	ds := &mockDatastore{}
	logger := NewLogger(ds, slog.New(slog.NewTextHandler(os.Stdout, nil)))

	ctx := context.Background()

	// Test session created
	err := logger.LogSessionCreated(ctx, "alice", "session-abc", "192.168.1.100")
	if err != nil {
		t.Fatalf("LogSessionCreated failed: %v", err)
	}

	// Test session terminated
	err = logger.LogSessionTerminated(ctx, "alice", "session-abc", "user_logout")
	if err != nil {
		t.Fatalf("LogSessionTerminated failed: %v", err)
	}

	if len(ds.events) != 2 {
		t.Fatalf("Expected 2 events, got %d", len(ds.events))
	}

	if ds.events[0].Action != string(EventSessionCreated) {
		t.Errorf("Expected action '%s', got '%s'", EventSessionCreated, ds.events[0].Action)
	}
	if ds.events[1].Action != string(EventSessionTerminated) {
		t.Errorf("Expected action '%s', got '%s'", EventSessionTerminated, ds.events[1].Action)
	}
}

func TestEventJSONMarshaling(t *testing.T) {
	event := &Event{
		Timestamp: time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		EventType: EventAuthSuccess,
		User:      "alice",
		SessionID: "session-123",
		SourceIP:  "192.168.1.100",
		Action:    "login",
		Result:    ResultSuccess,
		Details: map[string]interface{}{
			"method": "password",
			"count":  1,
		},
		CorrelationID: "correlation-abc",
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("Failed to marshal event: %v", err)
	}

	// Unmarshal to verify
	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal event: %v", err)
	}

	if decoded.EventType != EventAuthSuccess {
		t.Errorf("Expected event type '%s', got '%s'", EventAuthSuccess, decoded.EventType)
	}
	if decoded.User != "alice" {
		t.Errorf("Expected user 'alice', got '%s'", decoded.User)
	}
	if decoded.Result != ResultSuccess {
		t.Errorf("Expected result '%s', got '%s'", ResultSuccess, decoded.Result)
	}
}

func TestSetRetention(t *testing.T) {
	ds := &mockDatastore{}
	logger := NewLogger(ds, nil)

	// Default retention
	if logger.retention != 90*24*time.Hour {
		t.Errorf("Expected default retention of 90 days, got %v", logger.retention)
	}

	// Set custom retention
	logger.SetRetention(30 * 24 * time.Hour)
	if logger.retention != 30*24*time.Hour {
		t.Errorf("Expected retention of 30 days, got %v", logger.retention)
	}
}

func TestLogWithMissingTimestamp(t *testing.T) {
	ds := &mockDatastore{}
	logger := NewLogger(ds, slog.New(slog.NewTextHandler(os.Stdout, nil)))

	ctx := context.Background()
	event := &Event{
		EventType: EventAuthSuccess,
		User:      "alice",
		Result:    ResultSuccess,
		// Timestamp intentionally not set
	}

	beforeLog := time.Now()
	err := logger.Log(ctx, event)
	afterLog := time.Now()

	if err != nil {
		t.Fatalf("Log failed: %v", err)
	}

	if len(ds.events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(ds.events))
	}

	// Verify timestamp was set automatically
	if ds.events[0].Timestamp.IsZero() {
		t.Error("Timestamp should be set automatically")
	}

	// Verify timestamp is reasonable
	if ds.events[0].Timestamp.Before(beforeLog) || ds.events[0].Timestamp.After(afterLog) {
		t.Error("Timestamp should be between before and after log call")
	}
}

func TestLogWithComplexDetails(t *testing.T) {
	ds := &mockDatastore{}
	logger := NewLogger(ds, slog.New(slog.NewTextHandler(os.Stdout, nil)))

	ctx := context.Background()
	event := &Event{
		EventType: EventCommit,
		User:      "alice",
		Result:    ResultSuccess,
		Details: map[string]interface{}{
			"commit_id": "abc123",
			"changes": map[string]interface{}{
				"interfaces": 3,
				"routes":     5,
			},
			"config_size": 12345,
			"validated":   true,
		},
	}

	err := logger.Log(ctx, event)
	if err != nil {
		t.Fatalf("Log failed: %v", err)
	}

	if len(ds.events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(ds.events))
	}

	// Verify details were marshaled to JSON
	if ds.events[0].Details == "" {
		t.Error("Details should be marshaled to JSON")
	}

	// Verify we can unmarshal the details
	var details map[string]interface{}
	if err := json.Unmarshal([]byte(ds.events[0].Details), &details); err != nil {
		t.Fatalf("Failed to unmarshal details: %v", err)
	}

	if details["commit_id"] != "abc123" {
		t.Error("Details should contain commit_id")
	}
}

func TestLogDiscardChanges(t *testing.T) {
	ds := &mockDatastore{}
	logger := NewLogger(ds, slog.New(slog.NewTextHandler(os.Stdout, nil)))

	ctx := context.Background()
	err := logger.LogDiscardChanges(ctx, "alice", "session-123")
	if err != nil {
		t.Fatalf("LogDiscardChanges failed: %v", err)
	}

	if len(ds.events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(ds.events))
	}

	event := ds.events[0]
	if event.Action != string(EventDiscardChanges) {
		t.Errorf("Expected action '%s', got '%s'", EventDiscardChanges, event.Action)
	}
}

func TestLogLockStolen(t *testing.T) {
	ds := &mockDatastore{}
	logger := NewLogger(ds, slog.New(slog.NewTextHandler(os.Stdout, nil)))

	ctx := context.Background()
	err := logger.LogLockStolen(ctx, "admin-user", "session-admin", "candidate", "alice")
	if err != nil {
		t.Fatalf("LogLockStolen failed: %v", err)
	}

	if len(ds.events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(ds.events))
	}

	event := ds.events[0]
	if event.Action != string(EventLockStolen) {
		t.Errorf("Expected action '%s', got '%s'", EventLockStolen, event.Action)
	}
	if !strings.Contains(event.Details, "alice") {
		t.Error("Details should contain previous owner")
	}
}

func TestRollbackFailure(t *testing.T) {
	ds := &mockDatastore{}
	logger := NewLogger(ds, slog.New(slog.NewTextHandler(os.Stdout, nil)))

	ctx := context.Background()
	err := logger.LogRollback(ctx, "alice", "session-123", "commit-xyz", false, "rollback failed: config not found")
	if err != nil {
		t.Fatalf("LogRollback (failure) failed: %v", err)
	}

	if len(ds.events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(ds.events))
	}

	event := ds.events[0]
	if event.Action != string(EventRollbackFailed) {
		t.Errorf("Expected action '%s', got '%s'", EventRollbackFailed, event.Action)
	}
	if event.Result != string(ResultFailure) {
		t.Errorf("Expected result '%s', got '%s'", ResultFailure, event.Result)
	}

	// Verify error message is persisted in details
	if !strings.Contains(event.Details, "rollback failed") {
		t.Error("Details should contain error message")
	}
}

func TestErrorMessagePersistence(t *testing.T) {
	ds := &mockDatastore{}
	logger := NewLogger(ds, slog.New(slog.NewTextHandler(os.Stdout, nil)))

	ctx := context.Background()

	// Test with existing details and error message
	err := logger.LogCommit(ctx, "alice", "session-123", "commit-abc", false, "validation failed: missing required field")
	if err != nil {
		t.Fatalf("LogCommit failed: %v", err)
	}

	if len(ds.events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(ds.events))
	}

	event := ds.events[0]

	// Verify error message is in details JSON
	var details map[string]interface{}
	if err := json.Unmarshal([]byte(event.Details), &details); err != nil {
		t.Fatalf("Failed to unmarshal details: %v", err)
	}

	errorMsg, ok := details["error_message"]
	if !ok {
		t.Error("error_message should be in details")
	}
	if errorMsg != "validation failed: missing required field" {
		t.Errorf("Expected error message, got %v", errorMsg)
	}
}

func TestAttemptedActionPersistence(t *testing.T) {
	ds := &mockDatastore{}
	logger := NewLogger(ds, slog.New(slog.NewTextHandler(os.Stdout, nil)))

	ctx := context.Background()

	// Test access denied with attempted action
	err := logger.LogAccessDenied(ctx, "bob", "session-456", "kill-session", "operator", "insufficient privileges")
	if err != nil {
		t.Fatalf("LogAccessDenied failed: %v", err)
	}

	if len(ds.events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(ds.events))
	}

	event := ds.events[0]

	// Verify attempted action is in details JSON
	var details map[string]interface{}
	if err := json.Unmarshal([]byte(event.Details), &details); err != nil {
		t.Fatalf("Failed to unmarshal details: %v", err)
	}

	attemptedAction, ok := details["attempted_action"]
	if !ok {
		t.Error("attempted_action should be in details")
	}
	if attemptedAction != "kill-session" {
		t.Errorf("Expected attempted_action 'kill-session', got %v", attemptedAction)
	}
}
