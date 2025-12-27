// Package cli provides commit and rollback command implementations
package cli

import (
	"context"
	"fmt"

	"github.com/akam1o/arca-router/pkg/datastore"
)

// CommitOptions represents options for commit command
type CommitOptions struct {
	Check   bool   // Validate only, don't commit
	Message string // Optional commit message
	AndQuit bool   // Exit configuration mode after commit
}

// CommitWithOptions commits candidate to running with options
func (s *Session) CommitWithOptions(ctx context.Context, opts CommitOptions) error {
	if s.mode != ModeConfiguration {
		return fmt.Errorf("not in configuration mode")
	}

	// Validation step
	candidate, err := s.ds.GetCandidate(ctx, s.id)
	if err != nil {
		return fmt.Errorf("failed to get candidate: %w", err)
	}

	if candidate.ConfigText == "" {
		return fmt.Errorf("candidate configuration is empty")
	}

	// If check only, return after validation
	if opts.Check {
		fmt.Println("configuration check succeeds")
		return nil
	}

	// Prepare commit message
	message := opts.Message
	if message == "" {
		message = "CLI commit"
	}

	// Perform commit
	req := &datastore.CommitRequest{
		SessionID: s.id,
		User:      s.username,
		Message:   message,
		SourceIP:  "local",
	}
	commitID, err := s.ds.Commit(ctx, req)
	if err != nil {
		return fmt.Errorf("commit failed: %w", err)
	}

	fmt.Printf("commit complete\n")
	if commitID != "" {
		fmt.Printf("  Commit ID: %s\n", commitID)
	}

	return nil
}

// RollbackWithNumber rolls back to a previous commit by number
// rollbackNum=0: discard changes (sync with running)
// rollbackNum=N: rollback to N commits ago
func (s *Session) RollbackWithNumber(ctx context.Context, rollbackNum int) error {
	if s.mode != ModeConfiguration {
		return fmt.Errorf("not in configuration mode")
	}

	if rollbackNum < 0 {
		return fmt.Errorf("invalid rollback number: %d", rollbackNum)
	}

	// Rollback 0 = discard changes
	if rollbackNum == 0 {
		return s.DiscardChangesWithMessage(ctx)
	}

	// Get commit history
	opts := &datastore.HistoryOptions{
		Limit:  rollbackNum + 1,
		Offset: 0,
	}
	history, err := s.ds.ListCommitHistory(ctx, opts)
	if err != nil {
		return fmt.Errorf("failed to get history: %w", err)
	}

	if len(history) <= rollbackNum {
		availableCommits := len(history) - 1
		if availableCommits < 0 {
			availableCommits = 0
		}
		return fmt.Errorf("not enough history for rollback %d (only %d commits available)", rollbackNum, availableCommits)
	}

	// Perform rollback
	targetCommit := history[rollbackNum]
	req := &datastore.RollbackRequest{
		CommitID: targetCommit.CommitID,
		User:     s.username,
		Message:  fmt.Sprintf("CLI rollback %d", rollbackNum),
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

	fmt.Printf("rollback complete\n")
	if len(targetCommit.CommitID) >= 8 {
		fmt.Printf("  Rolled back to: %s\n", targetCommit.CommitID[:8])
	} else {
		fmt.Printf("  Rolled back to: %s\n", targetCommit.CommitID)
	}
	if len(newCommitID) >= 8 {
		fmt.Printf("  New commit ID: %s\n", newCommitID[:8])
	} else if newCommitID != "" {
		fmt.Printf("  New commit ID: %s\n", newCommitID)
	}

	return nil
}

// DiscardChangesWithMessage discards candidate changes (rollback 0)
func (s *Session) DiscardChangesWithMessage(ctx context.Context) error {
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

// ShowCommitHistory displays commit history
func (s *Session) ShowCommitHistory(ctx context.Context, limit int) error {
	if limit <= 0 {
		limit = 10 // Default limit
	}

	opts := &datastore.HistoryOptions{
		Limit:  limit,
		Offset: 0,
	}
	history, err := s.ds.ListCommitHistory(ctx, opts)
	if err != nil {
		return fmt.Errorf("failed to get history: %w", err)
	}

	if len(history) == 0 {
		fmt.Println("No commit history")
		return nil
	}

	fmt.Printf("Commit history (showing last %d commits):\n\n", len(history))
	for i, commit := range history {
		shortID := commit.CommitID
		if len(shortID) >= 8 {
			shortID = shortID[:8]
		}
		fmt.Printf("%2d  %s  %s\n", i, shortID, commit.Timestamp.Format("2006-01-02 15:04:05"))
		if commit.User != "" {
			fmt.Printf("    User: %s\n", commit.User)
		}
		if commit.Message != "" {
			fmt.Printf("    Message: %s\n", commit.Message)
		}
		if commit.IsRollback {
			fmt.Printf("    (Rollback)\n")
		}
		fmt.Println()
	}

	return nil
}
