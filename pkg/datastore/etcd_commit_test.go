package datastore

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
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

func TestEtcdCommitHistoryIndexKeysSortNewestFirst(t *testing.T) {
	ds := &etcdDatastore{prefix: "/arca/"}
	base := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)

	keys := []string{
		ds.commitHistoryIndexKey(commitEntry{CommitID: "old", Timestamp: base}),
		ds.commitHistoryIndexKey(commitEntry{CommitID: "new", Timestamp: base.Add(2 * time.Minute)}),
		ds.commitHistoryIndexKey(commitEntry{CommitID: "middle", Timestamp: base.Add(time.Minute)}),
	}
	sort.Strings(keys)

	if !strings.HasSuffix(keys[0], "/new") || !strings.HasSuffix(keys[1], "/middle") || !strings.HasSuffix(keys[2], "/old") {
		t.Fatalf("sorted index keys = %v, want newest to oldest", keys)
	}
}

func TestEtcdCommitHistoryEntryFromIndexedKVAppliesFilters(t *testing.T) {
	base := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	kv := etcdCommitHistoryKV(t, "/arca/commit-history/index/alice", commitEntry{
		CommitID:   "alice-new",
		User:       "alice",
		Timestamp:  base.Add(time.Minute),
		IsRollback: false,
	})

	entry, ok := commitHistoryEntryFromEtcdKV(kv, &HistoryOptions{
		User:             "alice",
		StartTime:        base,
		EndTime:          base.Add(2 * time.Minute),
		ExcludeRollbacks: true,
	})
	if !ok || entry.CommitID != "alice-new" {
		t.Fatalf("commitHistoryEntryFromEtcdKV() = %#v, %v; want alice-new", entry, ok)
	}

	if _, ok := commitHistoryEntryFromEtcdKV(kv, &HistoryOptions{User: "bob"}); ok {
		t.Fatal("commitHistoryEntryFromEtcdKV() matched wrong user")
	}
}

func TestEtcdRollbackTargetCommitLookupErrorPreservesInternalError(t *testing.T) {
	internalErr := NewError(ErrCodeInternal, "failed to get commit", nil)

	err := rollbackTargetCommitLookupError(internalErr)
	if err != internalErr {
		t.Fatalf("rollbackTargetCommitLookupError() = %v, want original internal error", err)
	}
}

func TestEtcdRollbackTargetCommitLookupErrorMapsNotFound(t *testing.T) {
	err := rollbackTargetCommitLookupError(NewError(ErrCodeNotFound, "commit not found", nil))

	var dsErr *Error
	if !errors.As(err, &dsErr) || dsErr.Code != ErrCodeNotFound {
		t.Fatalf("rollbackTargetCommitLookupError() = %v, want ErrCodeNotFound", err)
	}
	if !strings.Contains(err.Error(), "target commit not found") {
		t.Fatalf("rollbackTargetCommitLookupError() = %v, want target context", err)
	}
}

func BenchmarkEtcdCommitHistoryIndexedPageFiltering(b *testing.B) {
	base := time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC)
	kvs := make([]*mvccpb.KeyValue, 1000)
	for i := range kvs {
		user := "alice"
		if i%2 == 0 {
			user = "bob"
		}
		kvs[i] = etcdCommitHistoryKV(b, "/arca/commit-history/index", commitEntry{
			CommitID:  "commit",
			User:      user,
			Timestamp: base.Add(time.Duration(i) * time.Second),
		})
	}
	opts := &HistoryOptions{User: "alice"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matches := 0
		for _, kv := range kvs {
			if _, ok := commitHistoryEntryFromEtcdKV(kv, opts); ok {
				matches++
			}
		}
		if matches == 0 {
			b.Fatal("expected indexed history matches")
		}
	}
}

func etcdCommitHistoryKV(t testing.TB, key string, entry commitEntry) *mvccpb.KeyValue {
	t.Helper()
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return &mvccpb.KeyValue{Key: []byte(key), Value: data}
}
