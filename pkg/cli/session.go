// Package cli provides interactive CLI session management for arca-router.
package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/akam1o/arca-router/pkg/datastore"
	"github.com/google/uuid"
)

// Mode represents the current CLI mode
type Mode int

const (
	ModeOperational Mode = iota
	ModeConfiguration
)

func (m Mode) String() string {
	switch m {
	case ModeOperational:
		return "operational"
	case ModeConfiguration:
		return "configuration"
	default:
		return "unknown"
	}
}

// Session represents a CLI session with datastore integration
type Session struct {
	id           string
	username     string
	mode         Mode
	ds           datastore.Datastore
	lockAcquired bool
	timeout      time.Duration
	createdAt    time.Time
	configPath   []string
}

// NewSession creates a new CLI session
func NewSession(username string, ds datastore.Datastore) *Session {
	return &Session{
		id:           uuid.New().String(),
		username:     username,
		mode:         ModeOperational,
		ds:           ds,
		lockAcquired: false,
		timeout:      30 * time.Minute,
		createdAt:    time.Now(),
		configPath:   []string{},
	}
}

func (s *Session) ID() string           { return s.id }
func (s *Session) Username() string     { return s.username }
func (s *Session) Mode() Mode           { return s.mode }
func (s *Session) ConfigPath() []string { return s.configPath }

// verifyLock checks if the session still owns the candidate lock
// Returns error if lock is expired or owned by another session
func (s *Session) verifyLock(ctx context.Context) error {
	if !s.lockAcquired {
		return fmt.Errorf("session does not hold lock")
	}

	lockInfo, err := s.ds.GetLockInfo(ctx, datastore.LockTargetCandidate)
	if err != nil {
		return fmt.Errorf("failed to get lock info: %w", err)
	}

	if !lockInfo.IsLocked {
		return fmt.Errorf("lock has been released")
	}

	if lockInfo.SessionID != s.id {
		return fmt.Errorf("lock is held by another session: %s", lockInfo.SessionID)
	}

	// Check if lock has expired
	if time.Now().After(lockInfo.ExpiresAt) {
		return fmt.Errorf("lock has expired at %s", lockInfo.ExpiresAt.Format(time.RFC3339))
	}

	return nil
}

// EnterConfigurationMode enters configuration mode
func (s *Session) EnterConfigurationMode(ctx context.Context) error {
	if s.mode == ModeConfiguration {
		return fmt.Errorf("already in configuration mode")
	}

	lockReq := &datastore.LockRequest{
		Target:    datastore.LockTargetCandidate,
		SessionID: s.id,
		User:      s.username,
		Timeout:   s.timeout,
	}
	if err := s.ds.AcquireLock(ctx, lockReq); err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}

	s.lockAcquired = true
	s.mode = ModeConfiguration

	// Initialize candidate from running if needed
	_, err := s.ds.GetCandidate(ctx, s.id)
	if err != nil {
		// Only initialize from running if candidate not found
		// Distinguish NOT_FOUND from other errors
		var dsErr *datastore.Error
		if err != nil && (!errors.As(err, &dsErr) || dsErr.Code != datastore.ErrCodeNotFound) {
			// This is not a "not found" error - it's a real failure
			return fmt.Errorf("failed to get candidate: %w", err)
		}

		// Candidate not found - initialize from running
		running, err := s.ds.GetRunning(ctx)
		if err != nil {
			return fmt.Errorf("failed to get running config: %w", err)
		}
		if err := s.ds.SaveCandidate(ctx, s.id, running.ConfigText); err != nil {
			return fmt.Errorf("failed to initialize candidate: %w", err)
		}
	}

	return nil
}

// ExitConfigurationMode exits configuration mode
func (s *Session) ExitConfigurationMode(ctx context.Context) error {
	if s.mode != ModeConfiguration {
		return fmt.Errorf("not in configuration mode")
	}

	if s.lockAcquired {
		if err := s.ds.ReleaseLock(ctx, datastore.LockTargetCandidate, s.id); err != nil {
			return fmt.Errorf("failed to release lock: %w", err)
		}
		s.lockAcquired = false
	}

	s.mode = ModeOperational
	s.configPath = []string{}
	return nil
}

// SetCommand executes a 'set' command
// Deprecated: Use SetCommandWithPath instead for better hierarchy support
func (s *Session) SetCommand(ctx context.Context, args []string) error {
	if s.mode != ModeConfiguration {
		return fmt.Errorf("not in configuration mode")
	}
	if len(args) == 0 {
		return fmt.Errorf("'set' requires arguments")
	}

	candidate, err := s.ds.GetCandidate(ctx, s.id)
	if err != nil {
		return fmt.Errorf("failed to get candidate: %w", err)
	}

	// Simple append for Phase 3
	newLine := "set " + strings.Join(args, " ")
	updatedText := candidate.ConfigText
	if updatedText == "" {
		updatedText = newLine
	} else {
		updatedText += "\n" + newLine
	}

	return s.ds.SaveCandidate(ctx, s.id, updatedText)
}

// DeleteCommand executes a 'delete' command
// Deprecated: Use DeleteCommandWithPath instead for proper token-boundary checking
func (s *Session) DeleteCommand(ctx context.Context, args []string) error {
	if s.mode != ModeConfiguration {
		return fmt.Errorf("not in configuration mode")
	}
	if len(args) == 0 {
		return fmt.Errorf("'delete' requires arguments")
	}

	candidate, err := s.ds.GetCandidate(ctx, s.id)
	if err != nil {
		return fmt.Errorf("failed to get candidate: %w", err)
	}

	// Simple line removal for Phase 3
	deletePattern := "set " + strings.Join(args, " ")
	lines := strings.Split(candidate.ConfigText, "\n")
	var newLines []string
	for _, line := range lines {
		if !strings.HasPrefix(line, deletePattern) {
			newLines = append(newLines, line)
		}
	}

	return s.ds.SaveCandidate(ctx, s.id, strings.Join(newLines, "\n"))
}

// ShowCommand displays configuration
func (s *Session) ShowCommand(ctx context.Context, args []string) (string, error) {
	if s.mode == ModeConfiguration {
		candidate, err := s.ds.GetCandidate(ctx, s.id)
		if err != nil {
			return "", fmt.Errorf("failed to get candidate: %w", err)
		}
		return candidate.ConfigText, nil
	}

	running, err := s.ds.GetRunning(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get running: %w", err)
	}
	return running.ConfigText, nil
}

// CompareCommand shows diff
func (s *Session) CompareCommand(ctx context.Context) (string, error) {
	if s.mode != ModeConfiguration {
		return "", fmt.Errorf("'compare' only available in configuration mode")
	}

	diff, err := s.ds.CompareCandidateRunning(ctx, s.id)
	if err != nil {
		return "", fmt.Errorf("failed to get diff: %w", err)
	}

	if !diff.HasChanges {
		return "No changes\n", nil
	}
	return diff.DiffText, nil
}

// CommitCommand commits candidate to running
// Deprecated: Use CommitWithOptions instead for better control and consistency
func (s *Session) CommitCommand(ctx context.Context) error {
	if s.mode != ModeConfiguration {
		return fmt.Errorf("not in configuration mode")
	}

	// Verify lock before commit
	if err := s.verifyLock(ctx); err != nil {
		return fmt.Errorf("cannot commit: %w", err)
	}

	req := &datastore.CommitRequest{
		SessionID: s.id,
		User:      s.username,
		Message:   "CLI commit",
		SourceIP:  "local",
	}
	commitID, err := s.ds.Commit(ctx, req)
	if err != nil {
		return fmt.Errorf("commit failed: %w", err)
	}

	fmt.Printf("commit complete (commit ID: %s)\n", commitID)
	return nil
}

// CommitCheckCommand validates without committing
func (s *Session) CommitCheckCommand(ctx context.Context) error {
	if s.mode != ModeConfiguration {
		return fmt.Errorf("not in configuration mode")
	}

	candidate, err := s.ds.GetCandidate(ctx, s.id)
	if err != nil {
		return fmt.Errorf("failed to get candidate: %w", err)
	}

	// Phase 3: basic validation
	if candidate.ConfigText == "" {
		return fmt.Errorf("candidate configuration is empty")
	}

	fmt.Println("configuration check succeeds")
	return nil
}

// RollbackCommand rolls back to a previous commit
// Deprecated: Use RollbackWithNumber instead for better consistency
func (s *Session) RollbackCommand(ctx context.Context, rollbackNum int) error {
	if s.mode != ModeConfiguration {
		return fmt.Errorf("not in configuration mode")
	}

	if rollbackNum < 0 {
		return fmt.Errorf("invalid rollback number: %d", rollbackNum)
	}

	if rollbackNum == 0 {
		return s.DiscardChanges(ctx)
	}

	// Verify lock before rollback
	if err := s.verifyLock(ctx); err != nil {
		return fmt.Errorf("cannot rollback: %w", err)
	}

	opts := &datastore.HistoryOptions{
		Limit:  rollbackNum + 1,
		Offset: 0,
	}
	history, err := s.ds.ListCommitHistory(ctx, opts)
	if err != nil {
		return fmt.Errorf("failed to get history: %w", err)
	}

	if len(history) <= rollbackNum {
		return fmt.Errorf("not enough history for rollback %d", rollbackNum)
	}

	req := &datastore.RollbackRequest{
		CommitID: history[rollbackNum].CommitID,
		User:     s.username,
		Message:  fmt.Sprintf("Rollback %d", rollbackNum),
		SourceIP: "local",
	}
	newCommitID, err := s.ds.Rollback(ctx, req)
	if err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}

	// Sync candidate with rolled-back running
	running, err := s.ds.GetRunning(ctx)
	if err != nil {
		return fmt.Errorf("failed to get running after rollback: %w", err)
	}
	if err := s.ds.SaveCandidate(ctx, s.id, running.ConfigText); err != nil {
		return fmt.Errorf("failed to sync candidate: %w", err)
	}

	fmt.Printf("rollback complete (new commit ID: %s)\n", newCommitID)
	return nil
}

// DiscardChanges discards candidate changes
func (s *Session) DiscardChanges(ctx context.Context) error {
	if s.mode != ModeConfiguration {
		return fmt.Errorf("not in configuration mode")
	}

	running, err := s.ds.GetRunning(ctx)
	if err != nil {
		return fmt.Errorf("failed to get running: %w", err)
	}

	if err := s.ds.SaveCandidate(ctx, s.id, running.ConfigText); err != nil {
		return fmt.Errorf("failed to discard changes: %w", err)
	}

	fmt.Println("changes discarded")
	return nil
}

// EditHierarchy enters a config hierarchy level
func (s *Session) EditHierarchy(path []string) {
	s.configPath = append([]string{}, path...)
}

// UpHierarchy moves up one level
func (s *Session) UpHierarchy() {
	if len(s.configPath) > 0 {
		s.configPath = s.configPath[:len(s.configPath)-1]
	}
}

// TopHierarchy moves to top level
func (s *Session) TopHierarchy() {
	s.configPath = []string{}
}

// Close closes the session
func (s *Session) Close(ctx context.Context) error {
	if s.mode == ModeConfiguration {
		return s.ExitConfigurationMode(ctx)
	}
	return nil
}
