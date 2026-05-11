package cli

import (
	"context"
	"testing"

	"github.com/akam1o/arca-router/pkg/datastore"
)

func TestCommitOptions(t *testing.T) {
	tests := []struct {
		name string
		opts CommitOptions
		want string
	}{
		{
			name: "default options",
			opts: CommitOptions{},
			want: "",
		},
		{
			name: "check only",
			opts: CommitOptions{Check: true},
			want: "",
		},
		{
			name: "with message",
			opts: CommitOptions{Message: "test commit"},
			want: "test commit",
		},
		{
			name: "and-quit",
			opts: CommitOptions{AndQuit: true},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.opts.Message != tt.want {
				if tt.want == "" && tt.opts.Message == "" {
					// OK
				} else {
					t.Errorf("CommitOptions.Message = %v, want %v", tt.opts.Message, tt.want)
				}
			}
		})
	}
}

func TestCommitOptionsDefaults(t *testing.T) {
	opts := CommitOptions{}
	if opts.Check {
		t.Error("Expected Check to be false by default")
	}
	if opts.AndQuit {
		t.Error("Expected AndQuit to be false by default")
	}
	if opts.Message != "" {
		t.Error("Expected Message to be empty by default")
	}
}

func TestCommitWithOptions(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Enter configuration mode
	if err := session.EnterConfigurationMode(ctx); err != nil {
		t.Fatalf("Failed to enter configuration mode: %v", err)
	}

	tests := []struct {
		name    string
		opts    CommitOptions
		wantErr bool
	}{
		{
			name:    "commit without options",
			opts:    CommitOptions{},
			wantErr: false,
		},
		{
			name:    "commit with message",
			opts:    CommitOptions{Message: "test commit"},
			wantErr: false,
		},
		{
			name:    "commit check only",
			opts:    CommitOptions{Check: true},
			wantErr: false,
		},
		{
			name:    "commit with and-quit",
			opts:    CommitOptions{AndQuit: true},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := session.CommitWithOptions(ctx, tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("CommitWithOptions() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCommitWithOptionsRefreshesConfigurationSession(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)
	if err := session.EnterConfigurationMode(ctx); err != nil {
		t.Fatalf("EnterConfigurationMode() error = %v", err)
	}

	initialLocks := ds.acquireLockCount
	if err := session.CommitWithOptions(ctx, CommitOptions{}); err != nil {
		t.Fatalf("CommitWithOptions() error = %v", err)
	}

	if session.Mode() != ModeConfiguration {
		t.Fatalf("mode = %v, want configuration", session.Mode())
	}
	if !ds.lockAcquired || ds.lockSessionID != session.ID() {
		t.Fatalf("lock was not reacquired for session %s", session.ID())
	}
	if ds.acquireLockCount != initialLocks+1 {
		t.Fatalf("AcquireLock count = %d, want %d", ds.acquireLockCount, initialLocks+1)
	}
	if ds.saveCandidateText == "" {
		t.Fatal("candidate was not refreshed from running after commit")
	}
	if err := session.SetCommand(ctx, []string{"system", "host-name", "router2"}); err != nil {
		t.Fatalf("SetCommand() after commit error = %v", err)
	}
}

func TestCommitWithOptionsAndQuitLeavesOperational(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)
	if err := session.EnterConfigurationMode(ctx); err != nil {
		t.Fatalf("EnterConfigurationMode() error = %v", err)
	}

	if err := session.CommitWithOptions(ctx, CommitOptions{AndQuit: true}); err != nil {
		t.Fatalf("CommitWithOptions(and-quit) error = %v", err)
	}

	if session.Mode() != ModeOperational {
		t.Fatalf("mode = %v, want operational", session.Mode())
	}
	if session.lockAcquired {
		t.Fatal("session still believes it holds a lock after commit and-quit")
	}
	if ds.acquireLockCount != 1 {
		t.Fatalf("AcquireLock count = %d, want no reacquire after and-quit", ds.acquireLockCount)
	}
}

func TestRollbackWithNumber(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Enter configuration mode
	if err := session.EnterConfigurationMode(ctx); err != nil {
		t.Fatalf("Failed to enter configuration mode: %v", err)
	}

	tests := []struct {
		name        string
		rollbackNum int
		wantErr     bool
	}{
		{
			name:        "rollback 0",
			rollbackNum: 0,
			wantErr:     false,
		},
		{
			name:        "rollback 1 (insufficient history)",
			rollbackNum: 1,
			wantErr:     true, // Mock only returns 1 commit
		},
		{
			name:        "negative rollback",
			rollbackNum: -1,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := session.RollbackWithNumber(ctx, tt.rollbackNum)
			if (err != nil) != tt.wantErr {
				t.Errorf("RollbackWithNumber() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRollbackWithNumberRefreshesConfigurationSession(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{
		history: []*datastore.CommitHistoryEntry{
			{CommitID: "commit-new", ConfigText: "set system host-name new"},
			{CommitID: "commit-old", ConfigText: "set system host-name old"},
		},
	}
	session := NewSession("testuser", ds)
	if err := session.EnterConfigurationMode(ctx); err != nil {
		t.Fatalf("EnterConfigurationMode() error = %v", err)
	}

	initialLocks := ds.acquireLockCount
	if err := session.RollbackWithNumber(ctx, 1); err != nil {
		t.Fatalf("RollbackWithNumber() error = %v", err)
	}

	if session.Mode() != ModeConfiguration {
		t.Fatalf("mode = %v, want configuration", session.Mode())
	}
	if !ds.lockAcquired || ds.lockSessionID != session.ID() {
		t.Fatalf("lock was not reacquired for session %s", session.ID())
	}
	if ds.acquireLockCount != initialLocks+1 {
		t.Fatalf("AcquireLock count = %d, want %d", ds.acquireLockCount, initialLocks+1)
	}
	if err := session.SetCommand(ctx, []string{"system", "host-name", "router2"}); err != nil {
		t.Fatalf("SetCommand() after rollback error = %v", err)
	}
}

func TestShowCommitHistory(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Show history with default limit
	err := session.ShowCommitHistory(ctx, 10)
	if err != nil {
		t.Errorf("ShowCommitHistory() error = %v", err)
	}

	// Show history with small limit
	err = session.ShowCommitHistory(ctx, 1)
	if err != nil {
		t.Errorf("ShowCommitHistory() error = %v", err)
	}

	// Show history with zero limit (should use default)
	err = session.ShowCommitHistory(ctx, 0)
	if err != nil {
		t.Errorf("ShowCommitHistory() error = %v", err)
	}
}

func TestDiscardChangesWithMessage(t *testing.T) {
	ctx := context.Background()
	ds := &mockDatastore{}
	session := NewSession("testuser", ds)

	// Enter configuration mode
	if err := session.EnterConfigurationMode(ctx); err != nil {
		t.Fatalf("Failed to enter configuration mode: %v", err)
	}

	// Discard changes
	err := session.DiscardChangesWithMessage(ctx)
	if err != nil {
		t.Errorf("DiscardChangesWithMessage() error = %v", err)
	}
}
