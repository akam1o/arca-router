package datastore

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestListCommitHistoryAllowsOffsetWithoutLimit(t *testing.T) {
	ds := openSQLiteDatastoreForTest(t, filepath.Join(t.TempDir(), "config.db"))
	ctx := context.Background()

	base := time.Unix(1000, 0).UTC()
	commits := []struct {
		id        string
		timestamp time.Time
		message   string
		config    string
	}{
		{id: "commit-1", timestamp: base.Add(time.Minute), message: "first", config: "set system host-name router1"},
		{id: "commit-2", timestamp: base.Add(2 * time.Minute), message: "second", config: "set system host-name router2"},
		{id: "commit-3", timestamp: base.Add(3 * time.Minute), message: "third", config: "set system host-name router3"},
	}
	for _, commit := range commits {
		mustExec(t, ds.db, `
			INSERT INTO commit_history (commit_id, user, timestamp, message, config_text, is_rollback, source_ip)
			VALUES (?, ?, ?, ?, ?, 0, ?)
		`, commit.id, "alice", commit.timestamp, commit.message, commit.config, "")
	}

	history, err := ds.ListCommitHistory(ctx, &HistoryOptions{Offset: 1})
	if err != nil {
		t.Fatalf("ListCommitHistory() error = %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history length = %d, want 2", len(history))
	}
	if history[0].CommitID != "commit-2" || history[1].CommitID != "commit-1" {
		t.Fatalf("history IDs = %q, %q; want commit-2, commit-1", history[0].CommitID, history[1].CommitID)
	}
}

func TestListCommitHistoryAppliesDefaultLimit(t *testing.T) {
	ds := openSQLiteDatastoreForTest(t, filepath.Join(t.TempDir(), "config.db"))
	insertCommitHistoryRows(t, ds, defaultCommitHistoryLimit+5)

	history, err := ds.ListCommitHistory(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListCommitHistory() error = %v", err)
	}
	if len(history) != defaultCommitHistoryLimit {
		t.Fatalf("history length = %d, want %d", len(history), defaultCommitHistoryLimit)
	}
}

func TestListCommitHistoryCapsOversizedLimit(t *testing.T) {
	ds := openSQLiteDatastoreForTest(t, filepath.Join(t.TempDir(), "config.db"))
	insertCommitHistoryRows(t, ds, maxCommitHistoryLimit+5)

	history, err := ds.ListCommitHistory(context.Background(), &HistoryOptions{Limit: maxCommitHistoryLimit + 50})
	if err != nil {
		t.Fatalf("ListCommitHistory() error = %v", err)
	}
	if len(history) != maxCommitHistoryLimit {
		t.Fatalf("history length = %d, want %d", len(history), maxCommitHistoryLimit)
	}
}

func TestSQLiteCommitReadsCandidateInsideTransaction(t *testing.T) {
	ds := openSQLiteDatastoreForTest(t, filepath.Join(t.TempDir(), "config.db"))
	ctx := context.Background()
	sessionID := "session-1"

	if err := ds.AcquireLock(ctx, &LockRequest{
		Target:    LockTargetCandidate,
		SessionID: sessionID,
		User:      "alice",
	}); err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}
	if err := ds.SaveCandidate(ctx, sessionID, "set system host-name old\n"); err != nil {
		t.Fatalf("SaveCandidate(old) error = %v", err)
	}

	tx, err := ds.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE candidate_configs
		SET config_text = ?, updated_at = ?
		WHERE session_id = ?
	`, "set system host-name new\n", time.Now(), sessionID); err != nil {
		_ = tx.Rollback()
		t.Fatalf("update candidate in blocking tx: %v", err)
	}

	type commitResult struct {
		commitID string
		err      error
	}
	done := make(chan commitResult, 1)
	go func() {
		commitID, err := ds.Commit(ctx, &CommitRequest{
			SessionID: sessionID,
			User:      "alice",
			Message:   "commit latest candidate",
		})
		done <- commitResult{commitID: commitID, err: err}
	}()

	time.Sleep(100 * time.Millisecond)
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit blocking tx: %v", err)
	}

	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("Commit() error = %v", result.err)
		}
		if result.commitID == "" {
			t.Fatal("Commit() commitID is empty")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Commit() timed out")
	}

	running, err := ds.GetRunning(ctx)
	if err != nil {
		t.Fatalf("GetRunning() error = %v", err)
	}
	if !strings.Contains(running.ConfigText, "host-name new") {
		t.Fatalf("running config = %q, want latest candidate", running.ConfigText)
	}
}

func insertCommitHistoryRows(t *testing.T, ds *sqliteDatastore, count int) {
	t.Helper()

	base := time.Unix(1000, 0).UTC()
	for i := 0; i < count; i++ {
		mustExec(t, ds.db, `
			INSERT INTO commit_history (commit_id, user, timestamp, message, config_text, is_rollback, source_ip)
			VALUES (?, ?, ?, ?, ?, 0, ?)
		`,
			fmt.Sprintf("commit-%04d", i),
			"alice",
			base.Add(time.Duration(i)*time.Minute),
			fmt.Sprintf("commit %d", i),
			fmt.Sprintf("set system host-name router%d", i),
			"",
		)
	}
}
