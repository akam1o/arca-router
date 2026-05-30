package datastore

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSQLiteReleaseLockReturnsExpiredCleanupError(t *testing.T) {
	ds := openSQLiteDatastoreForTest(t, filepath.Join(t.TempDir(), "config.db"))
	now := time.Now().Unix()
	mustExec(t, ds.db, `
		INSERT INTO config_locks (target, session_id, user, acquired_at, expires_at, last_activity)
		VALUES (?, ?, ?, ?, ?, ?)
	`, LockTargetCandidate, "session-1", "alice", now-120, now-60, now-120)
	mustExec(t, ds.db, `
		CREATE TRIGGER block_config_lock_delete
		BEFORE DELETE ON config_locks
		BEGIN
			SELECT RAISE(FAIL, 'blocked delete');
		END
	`)

	err := ds.ReleaseLock(context.Background(), LockTargetCandidate, "session-1")
	if err == nil {
		t.Fatal("ReleaseLock() error = nil, want cleanup error")
	}
	var dsErr *Error
	if !errors.As(err, &dsErr) {
		t.Fatalf("ReleaseLock() error = %T, want datastore Error", err)
	}
	if dsErr.Code != ErrCodeInternal {
		t.Fatalf("error code = %s, want %s", dsErr.Code, ErrCodeInternal)
	}
	if !strings.Contains(err.Error(), "failed to delete expired candidate lock") {
		t.Fatalf("ReleaseLock() error = %q, want expired cleanup message", err.Error())
	}
}
