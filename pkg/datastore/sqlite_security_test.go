package datastore

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLiteDatastoreProtectsCandidatePasswords(t *testing.T) {
	ds := openSQLiteDatastoreForTest(t, filepath.Join(t.TempDir(), "config.db"))

	const plainPassword = "plain-password-value"
	err := ds.SaveCandidate(context.Background(), "session-1", "set security users user admin password "+plainPassword)
	if err != nil {
		t.Fatalf("SaveCandidate() error = %v", err)
	}

	candidate, err := ds.GetCandidate(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("GetCandidate() error = %v", err)
	}
	if strings.Contains(candidate.ConfigText, plainPassword) {
		t.Fatalf("candidate config contains plain password: %s", candidate.ConfigText)
	}
	if !strings.Contains(candidate.ConfigText, "$argon2id$") {
		t.Fatalf("candidate config = %q, want encoded password hash", candidate.ConfigText)
	}
}
