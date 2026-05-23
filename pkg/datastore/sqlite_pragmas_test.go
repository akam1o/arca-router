package datastore

import (
	"path/filepath"
	"testing"
)

func TestSQLiteDatastoreUsesFullSynchronousMode(t *testing.T) {
	ds := openSQLiteDatastoreForTest(t, filepath.Join(t.TempDir(), "config.db"))

	var synchronous string
	if err := ds.db.QueryRow("PRAGMA synchronous").Scan(&synchronous); err != nil {
		t.Fatalf("query synchronous pragma: %v", err)
	}
	if synchronous != "2" {
		t.Fatalf("PRAGMA synchronous = %q, want 2 (FULL)", synchronous)
	}
}
