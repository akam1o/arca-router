package datastore

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteListAuditEventsFiltersAndPaginates(t *testing.T) {
	ds := openSQLiteDatastoreForTest(t, filepath.Join(t.TempDir(), "config.db"))
	ctx := context.Background()
	base := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
	events := []*AuditEvent{
		{Timestamp: base, User: "alice", SessionID: "s1", SourceIP: "192.0.2.1", Action: "commit", Result: "success", Details: `{"commit":"one"}`},
		{Timestamp: base.Add(time.Minute), User: "bob", SessionID: "s2", SourceIP: "192.0.2.2", Action: "access_denied", Result: "denied", ErrorCode: "rbac-deny"},
		{Timestamp: base.Add(2 * time.Minute), User: "alice", SessionID: "s3", SourceIP: "192.0.2.3", Action: "rollback", Result: "success"},
	}
	for _, event := range events {
		if err := ds.LogAuditEvent(ctx, event); err != nil {
			t.Fatalf("LogAuditEvent() error = %v", err)
		}
	}

	got, err := ds.ListAuditEvents(ctx, &AuditOptions{User: "alice", Limit: 1})
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	if len(got) != 1 || got[0].Action != "rollback" || got[0].SessionID != "s3" {
		t.Fatalf("ListAuditEvents(user alice limit 1) = %#v, want newest alice rollback", got)
	}

	got, err = ds.ListAuditEvents(ctx, &AuditOptions{Action: "access_denied", Result: "denied"})
	if err != nil {
		t.Fatalf("ListAuditEvents(access_denied) error = %v", err)
	}
	if len(got) != 1 || got[0].User != "bob" || got[0].ErrorCode != "rbac-deny" {
		t.Fatalf("ListAuditEvents(access_denied) = %#v, want bob RBAC denial", got)
	}

	got, err = ds.ListAuditEvents(ctx, &AuditOptions{Offset: 1, Limit: 1})
	if err != nil {
		t.Fatalf("ListAuditEvents(offset) error = %v", err)
	}
	if len(got) != 1 || got[0].Action != "access_denied" {
		t.Fatalf("ListAuditEvents(offset 1 limit 1) = %#v, want second-newest access_denied", got)
	}
}

func TestSQLiteListAuditEventsAppliesDefaultLimit(t *testing.T) {
	ds := openSQLiteDatastoreForTest(t, filepath.Join(t.TempDir(), "config.db"))
	insertAuditLogRows(t, ds, defaultAuditEventsLimit+5)

	events, err := ds.ListAuditEvents(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	if len(events) != defaultAuditEventsLimit {
		t.Fatalf("events length = %d, want %d", len(events), defaultAuditEventsLimit)
	}
}

func TestSQLiteListAuditEventsCapsOversizedLimit(t *testing.T) {
	ds := openSQLiteDatastoreForTest(t, filepath.Join(t.TempDir(), "config.db"))
	insertAuditLogRows(t, ds, maxAuditEventsLimit+5)

	events, err := ds.ListAuditEvents(context.Background(), &AuditOptions{Limit: maxAuditEventsLimit + 50})
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	if len(events) != maxAuditEventsLimit {
		t.Fatalf("events length = %d, want %d", len(events), maxAuditEventsLimit)
	}
}

func insertAuditLogRows(t *testing.T, ds *sqliteDatastore, count int) {
	t.Helper()

	base := time.Unix(1000, 0).UTC()
	for i := 0; i < count; i++ {
		mustExec(t, ds.db, `
			INSERT INTO audit_log (timestamp, user, action, result, details)
			VALUES (?, ?, ?, ?, ?)
		`,
			base.Add(time.Duration(i)*time.Minute),
			"alice",
			fmt.Sprintf("action-%04d", i),
			"success",
			"{}",
		)
	}
}
