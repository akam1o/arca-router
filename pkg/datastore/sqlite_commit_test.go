package datastore

import (
	"context"
	"path/filepath"
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
