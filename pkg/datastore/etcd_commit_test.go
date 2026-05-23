package datastore

import (
	"encoding/json"
	"testing"
	"time"

	"go.etcd.io/etcd/api/v3/mvccpb"
)

func TestEtcdCommitHistorySortsByTimestampBeforeLimit(t *testing.T) {
	base := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	kvs := []*mvccpb.KeyValue{
		etcdCommitHistoryKV(t, "/arca/commits/zzz-old", commitEntry{
			CommitID:  "old",
			User:      "alice",
			Timestamp: base,
			Message:   "old commit",
		}),
		etcdCommitHistoryKV(t, "/arca/commits/aaa-new", commitEntry{
			CommitID:  "new",
			User:      "bob",
			Timestamp: base.Add(2 * time.Minute),
			Message:   "new commit",
		}),
		etcdCommitHistoryKV(t, "/arca/commits/mmm-middle", commitEntry{
			CommitID:  "middle",
			User:      "alice",
			Timestamp: base.Add(time.Minute),
			Message:   "middle commit",
		}),
	}

	entries := commitHistoryEntriesFromEtcdKVs(kvs, &HistoryOptions{Limit: 2})
	if len(entries) != 2 {
		t.Fatalf("entries length = %d, want 2", len(entries))
	}
	if entries[0].CommitID != "new" || entries[1].CommitID != "middle" {
		t.Fatalf("entries order = [%s, %s], want [new, middle]", entries[0].CommitID, entries[1].CommitID)
	}
}

func TestEtcdCommitHistoryFiltersBeforeOffsetAndLimit(t *testing.T) {
	base := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	kvs := []*mvccpb.KeyValue{
		etcdCommitHistoryKV(t, "/arca/commits/a", commitEntry{
			CommitID:  "alice-new",
			User:      "alice",
			Timestamp: base.Add(3 * time.Minute),
		}),
		etcdCommitHistoryKV(t, "/arca/commits/b", commitEntry{
			CommitID:  "bob-new",
			User:      "bob",
			Timestamp: base.Add(2 * time.Minute),
		}),
		etcdCommitHistoryKV(t, "/arca/commits/c", commitEntry{
			CommitID:  "alice-old",
			User:      "alice",
			Timestamp: base.Add(time.Minute),
		}),
		{Key: []byte("/arca/commits/malformed"), Value: []byte("{")},
	}

	entries := commitHistoryEntriesFromEtcdKVs(kvs, &HistoryOptions{User: "alice", Offset: 1, Limit: 1})
	if len(entries) != 1 {
		t.Fatalf("entries length = %d, want 1", len(entries))
	}
	if entries[0].CommitID != "alice-old" {
		t.Fatalf("entries[0].CommitID = %q, want alice-old", entries[0].CommitID)
	}
}

func etcdCommitHistoryKV(t *testing.T, key string, entry commitEntry) *mvccpb.KeyValue {
	t.Helper()
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return &mvccpb.KeyValue{Key: []byte(key), Value: data}
}
