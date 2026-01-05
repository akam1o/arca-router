package cli

import (
	"context"
	"testing"
	"time"

	"github.com/akam1o/arca-router/pkg/datastore"
)

// mockDatastore implements datastore.Datastore for testing
type mockDatastore struct {
	lockSessionID string
	lockAcquired  bool
}

func (m *mockDatastore) GetRunning(ctx context.Context) (*datastore.RunningConfig, error) {
	return &datastore.RunningConfig{
		CommitID:   "test-commit",
		ConfigText: "set system host-name test-router",
		Timestamp:  time.Now(),
	}, nil
}

func (m *mockDatastore) GetCandidate(ctx context.Context, sessionID string) (*datastore.CandidateConfig, error) {
	return &datastore.CandidateConfig{
		SessionID:  sessionID,
		ConfigText: "set system host-name test-router",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}, nil
}

func (m *mockDatastore) SaveCandidate(ctx context.Context, sessionID string, configText string) error {
	return nil
}

func (m *mockDatastore) DeleteCandidate(ctx context.Context, sessionID string) error {
	return nil
}

func (m *mockDatastore) Commit(ctx context.Context, req *datastore.CommitRequest) (string, error) {
	return "new-commit-id", nil
}

func (m *mockDatastore) Rollback(ctx context.Context, req *datastore.RollbackRequest) (string, error) {
	return "rollback-commit-id", nil
}

func (m *mockDatastore) CompareCandidateRunning(ctx context.Context, sessionID string) (*datastore.DiffResult, error) {
	return &datastore.DiffResult{
		DiffText:   "No changes",
		HasChanges: false,
	}, nil
}

func (m *mockDatastore) CompareCommits(ctx context.Context, commitID1, commitID2 string) (*datastore.DiffResult, error) {
	return &datastore.DiffResult{}, nil
}

func (m *mockDatastore) AcquireLock(ctx context.Context, req *datastore.LockRequest) error {
	m.lockSessionID = req.SessionID
	m.lockAcquired = true
	return nil
}

func (m *mockDatastore) ReleaseLock(ctx context.Context, target string, sessionID string) error {
	m.lockAcquired = false
	m.lockSessionID = ""
	return nil
}

func (m *mockDatastore) ExtendLock(ctx context.Context, target string, sessionID string, duration time.Duration) error {
	return nil
}

func (m *mockDatastore) StealLock(ctx context.Context, req *datastore.StealLockRequest) error {
	return nil
}

func (m *mockDatastore) GetLockInfo(ctx context.Context, target string) (*datastore.LockInfo, error) {
	if !m.lockAcquired {
		return &datastore.LockInfo{
			IsLocked: false,
		}, nil
	}
	return &datastore.LockInfo{
		IsLocked:   true,
		SessionID:  m.lockSessionID,
		User:       "testuser",
		AcquiredAt: time.Now().Add(-5 * time.Minute),
		ExpiresAt:  time.Now().Add(25 * time.Minute), // 30 minutes lock, 5 minutes elapsed
	}, nil
}

func (m *mockDatastore) ListCommitHistory(ctx context.Context, opts *datastore.HistoryOptions) ([]*datastore.CommitHistoryEntry, error) {
	return []*datastore.CommitHistoryEntry{
		{
			CommitID:   "commit-1",
			User:       "test-user",
			Timestamp:  time.Now(),
			Message:    "Test commit",
			ConfigText: "set system host-name test",
			IsRollback: false,
			SourceIP:   "local",
		},
	}, nil
}

func (m *mockDatastore) GetCommit(ctx context.Context, commitID string) (*datastore.CommitHistoryEntry, error) {
	return &datastore.CommitHistoryEntry{}, nil
}

	func (m *mockDatastore) LogAuditEvent(ctx context.Context, event *datastore.AuditEvent) error {
		return nil
	}

	func (m *mockDatastore) CleanupAuditLog(ctx context.Context, cutoff time.Time) (int64, error) {
		return 0, nil
	}

	func (m *mockDatastore) Close() error {
		return nil
	}

func TestNewSession(t *testing.T) {
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	if session.ID() == "" {
		t.Error("Session ID should not be empty")
	}
	if session.Username() != "testuser" {
		t.Errorf("Expected username 'testuser', got '%s'", session.Username())
	}
	if session.Mode() != ModeOperational {
		t.Errorf("Expected ModeOperational, got %v", session.Mode())
	}
}

func TestModeString(t *testing.T) {
	tests := []struct {
		mode Mode
		want string
	}{
		{ModeOperational, "operational"},
		{ModeConfiguration, "configuration"},
		{Mode(999), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.want {
			t.Errorf("Mode.String() = %v, want %v", got, tt.want)
		}
	}
}

func TestEnterExitConfigurationMode(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Initial state should be operational
	if session.Mode() != ModeOperational {
		t.Errorf("Initial mode should be operational, got %v", session.Mode())
	}

	// Enter configuration mode
	err := session.EnterConfigurationMode(ctx)
	if err != nil {
		t.Fatalf("EnterConfigurationMode() error = %v", err)
	}

	if session.Mode() != ModeConfiguration {
		t.Errorf("After enter, mode should be configuration, got %v", session.Mode())
	}

	// Try entering again (should fail)
	err = session.EnterConfigurationMode(ctx)
	if err == nil {
		t.Error("Expected error when entering configuration mode twice")
	}

	// Exit configuration mode
	err = session.ExitConfigurationMode(ctx)
	if err != nil {
		t.Fatalf("ExitConfigurationMode() error = %v", err)
	}

	if session.Mode() != ModeOperational {
		t.Errorf("After exit, mode should be operational, got %v", session.Mode())
	}

	// Try exiting again (should fail)
	err = session.ExitConfigurationMode(ctx)
	if err == nil {
		t.Error("Expected error when exiting operational mode")
	}
}

func TestSetCommand(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Enter configuration mode first
	if err := session.EnterConfigurationMode(ctx); err != nil {
		t.Fatalf("Failed to enter configuration mode: %v", err)
	}

	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "set system host-name",
			args:    []string{"system", "host-name", "router1"},
			wantErr: false,
		},
		{
			name:    "set with description",
			args:    []string{"interfaces", "ge-0/0/0", "description", "test interface"},
			wantErr: false,
		},
		{
			name:    "set BGP AS number",
			args:    []string{"protocols", "bgp", "as-number", "65001"},
			wantErr: false,
		},
		{
			name:    "set with empty args",
			args:    []string{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := session.SetCommand(ctx, tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetCommand() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSetCommandRequiresConfigMode(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Try set command in operational mode (should fail)
	err := session.SetCommand(ctx, []string{"system", "host-name", "router1"})
	if err == nil {
		t.Error("Expected error when calling SetCommand in operational mode")
	}
}

func TestDeleteCommand(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Enter configuration mode first
	if err := session.EnterConfigurationMode(ctx); err != nil {
		t.Fatalf("Failed to enter configuration mode: %v", err)
	}

	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{
			name:    "delete system host-name",
			args:    []string{"system", "host-name"},
			wantErr: false,
		},
		{
			name:    "delete interface",
			args:    []string{"interfaces", "ge-0/0/0"},
			wantErr: false,
		},
		{
			name:    "delete with empty args",
			args:    []string{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := session.DeleteCommand(ctx, tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteCommand() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDeleteCommandRequiresConfigMode(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Try delete command in operational mode (should fail)
	err := session.DeleteCommand(ctx, []string{"system", "host-name"})
	if err == nil {
		t.Error("Expected error when calling DeleteCommand in operational mode")
	}
}

func TestCommitCommand(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Enter configuration mode first
	if err := session.EnterConfigurationMode(ctx); err != nil {
		t.Fatalf("Failed to enter configuration mode: %v", err)
	}

	// Make a change
	if err := session.SetCommand(ctx, []string{"system", "host-name", "test-router"}); err != nil {
		t.Fatalf("SetCommand failed: %v", err)
	}

	// Commit the change
	err := session.CommitCommand(ctx)
	if err != nil {
		t.Errorf("CommitCommand() error = %v", err)
	}
}

func TestCommitCommandRequiresConfigMode(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Try commit command in operational mode (should fail)
	err := session.CommitCommand(ctx)
	if err == nil {
		t.Error("Expected error when calling CommitCommand in operational mode")
	}
}

func TestCommitCheckCommand(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Enter configuration mode
	if err := session.EnterConfigurationMode(ctx); err != nil {
		t.Fatalf("Failed to enter configuration mode: %v", err)
	}

	// Make a change
	if err := session.SetCommand(ctx, []string{"system", "host-name", "test-router"}); err != nil {
		t.Fatalf("SetCommand failed: %v", err)
	}

	// Check commit (should not actually commit)
	err := session.CommitCheckCommand(ctx)
	if err != nil {
		t.Errorf("CommitCheckCommand() error = %v", err)
	}
}

func TestCommitCheckCommandRequiresConfigMode(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Try commit check in operational mode (should fail)
	err := session.CommitCheckCommand(ctx)
	if err == nil {
		t.Error("Expected error when calling CommitCheckCommand in operational mode")
	}
}

func TestRollbackCommand(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Enter configuration mode first
	if err := session.EnterConfigurationMode(ctx); err != nil {
		t.Fatalf("Failed to enter configuration mode: %v", err)
	}

	tests := []struct {
		name        string
		rollbackNum int
		wantErr     bool
	}{
		{
			name:        "rollback 0 (discard changes)",
			rollbackNum: 0,
			wantErr:     false,
		},
		{
			name:        "rollback 1 (insufficient history)",
			rollbackNum: 1,
			wantErr:     true, // Mock only returns 1 commit, so rollback 1 fails
		},
		{
			name:        "negative rollback number",
			rollbackNum: -1,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := session.RollbackCommand(ctx, tt.rollbackNum)
			if (err != nil) != tt.wantErr {
				t.Errorf("RollbackCommand() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRollbackCommandRequiresConfigMode(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Try rollback command in operational mode (should fail)
	err := session.RollbackCommand(ctx, 0)
	if err == nil {
		t.Error("Expected error when calling RollbackCommand in operational mode")
	}
}

func TestHierarchyNavigation(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Enter configuration mode
	if err := session.EnterConfigurationMode(ctx); err != nil {
		t.Fatalf("Failed to enter configuration mode: %v", err)
	}

	// Initial config path should be empty
	if path := session.ConfigPath(); len(path) != 0 {
		t.Errorf("Initial config path should be empty, got %v", path)
	}

	// Edit to interfaces
	session.EditHierarchy([]string{"interfaces", "ge-0/0/0"})

	configPath := session.ConfigPath()
	if len(configPath) != 2 || configPath[0] != "interfaces" || configPath[1] != "ge-0/0/0" {
		t.Errorf("Config path should be [interfaces, ge-0/0/0], got %v", configPath)
	}

	// Edit deeper (replaces path)
	session.EditHierarchy([]string{"interfaces", "ge-0/0/0", "unit", "0"})

	configPath = session.ConfigPath()
	if len(configPath) != 4 {
		t.Errorf("Config path should have 4 elements, got %v", configPath)
	}

	// Up one level
	session.UpHierarchy()

	configPath = session.ConfigPath()
	if len(configPath) != 3 {
		t.Errorf("After up, config path should have 3 elements, got %v", configPath)
	}

	// Top
	session.TopHierarchy()
	configPath = session.ConfigPath()
	if len(configPath) != 0 {
		t.Errorf("After top, config path should be empty, got %v", configPath)
	}

	// Up at top level (should be no-op)
	session.UpHierarchy()
	configPath = session.ConfigPath()
	if len(configPath) != 0 {
		t.Errorf("After up at top, config path should still be empty, got %v", configPath)
	}
}

func TestHierarchyInConfigMode(t *testing.T) {
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Hierarchy commands work in any mode (they just track state)
	// They don't require configuration mode
	session.EditHierarchy([]string{"interfaces"})
	if len(session.ConfigPath()) != 1 {
		t.Error("EditHierarchy should work in operational mode")
	}

	session.UpHierarchy()
	if len(session.ConfigPath()) != 0 {
		t.Error("UpHierarchy should work in operational mode")
	}

	// Top should always work
	session.TopHierarchy()
}

func TestCompareCommand(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Enter configuration mode
	if err := session.EnterConfigurationMode(ctx); err != nil {
		t.Fatalf("Failed to enter configuration mode: %v", err)
	}

	// Compare should work even without changes
	_, err := session.CompareCommand(ctx)
	if err != nil {
		t.Errorf("CompareCommand() error = %v", err)
	}
}

func TestCompareCommandRequiresConfigMode(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Try compare in operational mode (should fail)
	_, err := session.CompareCommand(ctx)
	if err == nil {
		t.Error("Expected error when calling CompareCommand in operational mode")
	}
}

func TestShowCommand(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Enter configuration mode
	if err := session.EnterConfigurationMode(ctx); err != nil {
		t.Fatalf("Failed to enter configuration mode: %v", err)
	}

	// Show command should work
	_, err := session.ShowCommand(ctx, []string{})
	if err != nil {
		t.Errorf("ShowCommand() error = %v", err)
	}
}

func TestShowCommandInOperationalMode(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Show in operational mode shows running config
	_, err := session.ShowCommand(ctx, []string{})
	if err != nil {
		t.Errorf("ShowCommand() should work in operational mode: %v", err)
	}
}

func TestDiscardChanges(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Enter configuration mode
	if err := session.EnterConfigurationMode(ctx); err != nil {
		t.Fatalf("Failed to enter configuration mode: %v", err)
	}

	// Make a change
	if err := session.SetCommand(ctx, []string{"system", "host-name", "test-router"}); err != nil {
		t.Fatalf("SetCommand failed: %v", err)
	}

	// Discard changes
	err := session.DiscardChanges(ctx)
	if err != nil {
		t.Errorf("DiscardChanges() error = %v", err)
	}
}

func TestDiscardChangesRequiresConfigMode(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Try discard in operational mode (should fail)
	err := session.DiscardChanges(ctx)
	if err == nil {
		t.Error("Expected error when calling DiscardChanges in operational mode")
	}
}

func TestSessionClose(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Close should not fail
	err := session.Close(ctx)
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestSessionCloseInConfigMode(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Enter configuration mode
	if err := session.EnterConfigurationMode(ctx); err != nil {
		t.Fatalf("Failed to enter configuration mode: %v", err)
	}

	// Close should exit configuration mode
	err := session.Close(ctx)
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}

	if session.Mode() != ModeOperational {
		t.Error("Close should exit configuration mode")
	}
}

func TestSetCommandWithHierarchy(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Enter configuration mode
	if err := session.EnterConfigurationMode(ctx); err != nil {
		t.Fatalf("Failed to enter configuration mode: %v", err)
	}

	// Set hierarchy
	session.EditHierarchy([]string{"interfaces", "ge-0/0/0"})

	// Set command with hierarchy context
	err := session.SetCommand(ctx, []string{"unit", "0", "family", "inet"})
	if err != nil {
		t.Errorf("SetCommand() with hierarchy error = %v", err)
	}
}

func TestDeleteCommandWithHierarchy(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Enter configuration mode
	if err := session.EnterConfigurationMode(ctx); err != nil {
		t.Fatalf("Failed to enter configuration mode: %v", err)
	}

	// Set hierarchy to match existing config
	session.EditHierarchy([]string{"system"})

	// Delete command with hierarchy context
	err := session.DeleteCommand(ctx, []string{"host-name"})
	if err != nil {
		t.Errorf("DeleteCommand() with hierarchy error = %v", err)
	}
}

func TestRollbackCommandWithZero(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Enter configuration mode
	if err := session.EnterConfigurationMode(ctx); err != nil {
		t.Fatalf("Failed to enter configuration mode: %v", err)
	}

	// Rollback 0 should discard changes
	err := session.RollbackCommand(ctx, 0)
	if err != nil {
		t.Errorf("RollbackCommand(0) error = %v", err)
	}
}

func TestExitConfigurationModeReleasesLock(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Enter configuration mode
	if err := session.EnterConfigurationMode(ctx); err != nil {
		t.Fatalf("Failed to enter configuration mode: %v", err)
	}

	// Exit should release lock
	err := session.ExitConfigurationMode(ctx)
	if err != nil {
		t.Errorf("ExitConfigurationMode() error = %v", err)
	}

	// Should be able to enter again
	err = session.EnterConfigurationMode(ctx)
	if err != nil {
		t.Errorf("Should be able to re-enter configuration mode: %v", err)
	}
}

func TestSetDeleteMultipleOperations(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Enter configuration mode
	if err := session.EnterConfigurationMode(ctx); err != nil {
		t.Fatalf("Failed to enter configuration mode: %v", err)
	}

	// Multiple set operations
	commands := [][]string{
		{"system", "host-name", "router1"},
		{"system", "domain-name", "example.com"},
		{"interfaces", "ge-0/0/0", "description", "test"},
	}

	for _, cmd := range commands {
		if err := session.SetCommand(ctx, cmd); err != nil {
			t.Errorf("SetCommand(%v) error = %v", cmd, err)
		}
	}

	// Delete operation
	err := session.DeleteCommand(ctx, []string{"system", "domain-name"})
	if err != nil {
		t.Errorf("DeleteCommand() error = %v", err)
	}
}
