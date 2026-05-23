package datastore

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
	"go.etcd.io/etcd/api/v3/mvccpb"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// commitEntry represents a commit stored in etcd.
type commitEntry struct {
	CommitID   string    `json:"commit_id"`
	User       string    `json:"user"`
	Timestamp  time.Time `json:"timestamp"`
	Message    string    `json:"message"`
	ConfigText string    `json:"config_text"`
	IsRollback bool      `json:"is_rollback"`
	SourceIP   string    `json:"source_ip"`
}

const commitHistoryIndexPageSize = 1000

const (
	commitHistoryIndexBatchSize = 128
	commitHistoryIndexVersion   = "1"
)

// Commit promotes a candidate configuration to running configuration.
func (ds *etcdDatastore) Commit(ctx context.Context, req *CommitRequest) (string, error) {
	if err := validateCommitRequest(req); err != nil {
		return "", err
	}

	ctx, cancel := ds.withTimeout(ctx)
	defer cancel()

	// Generate commit ID
	commitID := uuid.New().String()
	now := time.Now()

	// Get candidate config and its ModRevision for CAS
	candidateKey := ds.key("candidates", req.SessionID)
	getCandidateResp, err := ds.client.Get(ctx, candidateKey)
	if err != nil {
		return "", NewError(ErrCodeInternal, "failed to get candidate config", err)
	}

	if len(getCandidateResp.Kvs) == 0 {
		return "", NewError(ErrCodeNotFound, "candidate config not found", nil)
	}

	candidateValue := string(getCandidateResp.Kvs[0].Value)
	candidateModRevision := getCandidateResp.Kvs[0].ModRevision

	// Parse candidate to get config text
	var candidateData struct {
		ConfigText string `json:"config_text"`
	}
	if err := json.Unmarshal(getCandidateResp.Kvs[0].Value, &candidateData); err != nil {
		return "", NewError(ErrCodeInternal, "failed to parse candidate config", err)
	}

	// Prepare commit entry
	entry := commitEntry{
		CommitID:   commitID,
		User:       req.User,
		Timestamp:  now,
		Message:    req.Message,
		ConfigText: candidateData.ConfigText,
		IsRollback: false,
		SourceIP:   req.SourceIP,
	}

	entryJSON, err := json.Marshal(entry)
	if err != nil {
		return "", NewError(ErrCodeInternal, "failed to marshal commit entry", err)
	}

	// Prepare running metadata
	metadata := runningMetadata{
		CommitID:  commitID,
		Timestamp: now,
	}

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return "", NewError(ErrCodeInternal, "failed to marshal running metadata", err)
	}

	// Prepare audit event
	auditULID := generateULID()
	auditEvent := &AuditEvent{
		Key:           auditULID, // Set Key field for consistency
		Timestamp:     now,
		User:          req.User,
		SessionID:     req.SessionID,
		SourceIP:      req.SourceIP,
		CorrelationID: req.CorrelationID,
		Action:        "commit",
		Result:        "success",
		Details:       fmt.Sprintf("commit_id=%s message=%q", commitID, req.Message),
	}

	auditJSON, err := json.Marshal(auditEvent)
	if err != nil {
		return "", NewError(ErrCodeInternal, "failed to marshal audit event", err)
	}

	// Get candidate lock key for transaction (only candidate lock is required for commit)
	lockKey := ds.lockKeyForTarget(LockTargetCandidate)
	runningMetadataKey := ds.key("running", "current")
	runningConfigKey := ds.key("running", "config")
	commitKey := ds.key("commits", commitID)
	commitIndexKey := ds.commitHistoryIndexKey(entry)
	auditKey := ds.key("audit", auditULID)

	// Check for legacy lock (fail-closed)
	if err := ds.checkLegacyLock(ctx); err != nil {
		return "", err
	}

	// Get current candidate lock to verify ownership
	getLockResp, err := ds.client.Get(ctx, lockKey)
	if err != nil {
		return "", NewError(ErrCodeInternal, "failed to get lock", err)
	}

	if len(getLockResp.Kvs) == 0 {
		return "", NewError(ErrCodeConflict, "lock not held", nil)
	}

	var currentLock lockData
	if err := json.Unmarshal(getLockResp.Kvs[0].Value, &currentLock); err != nil {
		return "", NewError(ErrCodeInternal, "failed to parse lock data", err)
	}

	if currentLock.SessionID != req.SessionID {
		return "", NewError(ErrCodeConflict,
			fmt.Sprintf("lock is held by another session %s", currentLock.SessionID),
			nil)
	}

	// Check lock expiration using server-side lease TTL (avoids time skew)
	if currentLock.LeaseID > 0 {
		leaseTTLResp, leaseErr := ds.client.TimeToLive(ctx, clientv3.LeaseID(currentLock.LeaseID))
		if leaseErr != nil {
			// Fail closed on TTL check errors
			return "", NewError(ErrCodeConflict, "lock status cannot be verified", leaseErr)
		}
		if leaseTTLResp.TTL <= 0 {
			return "", NewError(ErrCodeConflict, "lock has expired", nil)
		}
	}

	// Atomic commit transaction:
	// - Condition: Lock still held by this session AND candidate exists
	// - Success: Update running config, add commit history, delete candidate, release lock, log audit
	// - Failure: Return conflict error

	lockValue := string(getLockResp.Kvs[0].Value)

	txnResp, err := ds.client.Txn(ctx).
		If(
			clientv3.Compare(clientv3.Value(lockKey), "=", lockValue),
			clientv3.Compare(clientv3.Value(candidateKey), "=", candidateValue),             // Candidate unchanged
			clientv3.Compare(clientv3.ModRevision(candidateKey), "=", candidateModRevision), // No concurrent modification
		).
		Then(
			clientv3.OpPut(runningMetadataKey, string(metadataJSON)),
			clientv3.OpPut(runningConfigKey, candidateData.ConfigText),
			clientv3.OpPut(commitKey, string(entryJSON)),
			clientv3.OpPut(commitIndexKey, string(entryJSON)),
			clientv3.OpDelete(candidateKey),
			clientv3.OpDelete(lockKey),
			clientv3.OpPut(auditKey, string(auditJSON)),
		).
		Commit()

	if err != nil {
		return "", NewError(ErrCodeInternal, "failed to commit transaction", err)
	}

	if !txnResp.Succeeded {
		return "", NewError(ErrCodeConflict, "commit failed: lock was lost or candidate was deleted", nil)
	}

	// Revoke lease
	if currentLock.LeaseID > 0 {
		if _, err := ds.client.Revoke(ctx, clientv3.LeaseID(currentLock.LeaseID)); err != nil {
			// Best-effort cleanup; the commit already succeeded.
			_ = err
		}
	}

	return commitID, nil
}

// Rollback reverts to a previous commit.
func (ds *etcdDatastore) Rollback(ctx context.Context, req *RollbackRequest) (string, error) {
	ctx, cancel := ds.withTimeout(ctx)
	defer cancel()

	if req.SessionID == "" {
		return "", NewError(ErrCodeConflict,
			"cannot rollback: no config lock held (lock must be acquired before rollback)", nil)
	}

	// Get target commit
	targetCommit, err := ds.GetCommit(ctx, req.CommitID)
	if err != nil {
		return "", NewError(ErrCodeNotFound, "target commit not found", err)
	}

	// Generate new commit ID for the rollback
	newCommitID := uuid.New().String()
	now := time.Now()

	// Prepare rollback commit entry
	entry := commitEntry{
		CommitID:   newCommitID,
		User:       req.User,
		Timestamp:  now,
		Message:    fmt.Sprintf("Rollback to commit %s: %s", req.CommitID, req.Message),
		ConfigText: targetCommit.ConfigText,
		IsRollback: true,
		SourceIP:   req.SourceIP,
	}

	entryJSON, err := json.Marshal(entry)
	if err != nil {
		return "", NewError(ErrCodeInternal, "failed to marshal rollback entry", err)
	}

	// Prepare running metadata
	metadata := runningMetadata{
		CommitID:  newCommitID,
		Timestamp: now,
	}

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return "", NewError(ErrCodeInternal, "failed to marshal running metadata", err)
	}

	// Prepare audit event
	auditULID := generateULID()
	auditEvent := &AuditEvent{
		Key:           auditULID, // Set Key field for consistency
		Timestamp:     now,
		User:          req.User,
		SessionID:     req.SessionID,
		SourceIP:      req.SourceIP,
		CorrelationID: req.CorrelationID,
		Action:        "rollback",
		Result:        "success",
		Details:       fmt.Sprintf("rollback_commit_id=%s target_commit_id=%s message=%q", newCommitID, req.CommitID, req.Message),
	}

	auditJSON, err := json.Marshal(auditEvent)
	if err != nil {
		return "", NewError(ErrCodeInternal, "failed to marshal audit event", err)
	}

	// Keys
	lockKey := ds.lockKeyForTarget(LockTargetCandidate)
	runningMetadataKey := ds.key("running", "current")
	runningConfigKey := ds.key("running", "config")
	commitKey := ds.key("commits", newCommitID)
	commitIndexKey := ds.commitHistoryIndexKey(entry)
	auditKey := ds.key("audit", auditULID)

	// Check for legacy lock (fail-closed)
	if err := ds.checkLegacyLock(ctx); err != nil {
		return "", err
	}

	// Get current candidate lock to verify ownership
	getLockResp, err := ds.client.Get(ctx, lockKey)
	if err != nil {
		return "", NewError(ErrCodeInternal, "failed to get lock", err)
	}
	if len(getLockResp.Kvs) == 0 {
		return "", NewError(ErrCodeConflict, "lock not held", nil)
	}

	var currentLock lockData
	if err := json.Unmarshal(getLockResp.Kvs[0].Value, &currentLock); err != nil {
		return "", NewError(ErrCodeInternal, "failed to parse lock data", err)
	}
	if currentLock.SessionID != req.SessionID {
		return "", NewError(ErrCodeConflict,
			fmt.Sprintf("lock is held by another session %s", currentLock.SessionID),
			nil)
	}
	if currentLock.LeaseID > 0 {
		leaseTTLResp, leaseErr := ds.client.TimeToLive(ctx, clientv3.LeaseID(currentLock.LeaseID))
		if leaseErr != nil {
			return "", NewError(ErrCodeConflict, "lock status cannot be verified", leaseErr)
		}
		if leaseTTLResp.TTL <= 0 {
			return "", NewError(ErrCodeConflict, "lock has expired", nil)
		}
	}

	lockValue := string(getLockResp.Kvs[0].Value)

	// Atomic rollback transaction
	txnResp, err := ds.client.Txn(ctx).
		If(clientv3.Compare(clientv3.Value(lockKey), "=", lockValue)).
		Then(
			clientv3.OpPut(runningMetadataKey, string(metadataJSON)),
			clientv3.OpPut(runningConfigKey, targetCommit.ConfigText),
			clientv3.OpPut(commitKey, string(entryJSON)),
			clientv3.OpPut(commitIndexKey, string(entryJSON)),
			clientv3.OpDelete(lockKey),
			clientv3.OpPut(auditKey, string(auditJSON)),
		).
		Commit()

	if err != nil {
		return "", NewError(ErrCodeInternal, "failed to rollback transaction", err)
	}

	if !txnResp.Succeeded {
		return "", NewError(ErrCodeConflict, "rollback failed: lock was lost", nil)
	}

	if currentLock.LeaseID > 0 {
		if _, err := ds.client.Revoke(ctx, clientv3.LeaseID(currentLock.LeaseID)); err != nil {
			_ = err
		}
	}

	return newCommitID, nil
}

// ListCommitHistory retrieves commit history with optional filtering.
func (ds *etcdDatastore) ListCommitHistory(ctx context.Context, opts *HistoryOptions) ([]*CommitHistoryEntry, error) {
	ctx, cancel := ds.withTimeout(ctx)
	defer cancel()

	normalizedOpts := normalizeHistoryOptions(opts)
	opts = &normalizedOpts

	entries, indexed, err := ds.listCommitHistoryFromIndex(ctx, opts)
	if err != nil {
		return nil, err
	}
	if indexed {
		return entries, nil
	}

	// Legacy fallback for stores created before the timestamp index existed.
	commitsPrefix := ds.key("commits") + "/"
	resp, err := ds.client.Get(ctx, commitsPrefix, clientv3.WithPrefix())
	if err != nil {
		return nil, NewError(ErrCodeInternal, "failed to list commits", err)
	}

	return commitHistoryEntriesFromEtcdKVs(resp.Kvs, opts), nil
}

func (ds *etcdDatastore) listCommitHistoryFromIndex(ctx context.Context, opts *HistoryOptions) ([]*CommitHistoryEntry, bool, error) {
	prefix := ds.commitHistoryIndexPrefix()
	rangeEnd := clientv3.GetPrefixRangeEnd(prefix)
	startKey := prefix
	remainingOffset := opts.Offset
	entries := make([]*CommitHistoryEntry, 0, opts.Limit)
	indexed := false

	for len(entries) < opts.Limit {
		resp, err := ds.client.Get(ctx, startKey,
			clientv3.WithRange(rangeEnd),
			clientv3.WithLimit(commitHistoryIndexPageSize),
			clientv3.WithSort(clientv3.SortByKey, clientv3.SortAscend),
		)
		if err != nil {
			return nil, false, NewError(ErrCodeInternal, "failed to list commit history index", err)
		}
		if len(resp.Kvs) == 0 {
			break
		}
		indexed = true

		for _, kv := range resp.Kvs {
			entry, ok := commitHistoryEntryFromEtcdKV(kv, opts)
			if !ok {
				continue
			}
			if remainingOffset > 0 {
				remainingOffset--
				continue
			}
			entries = append(entries, entry)
			if len(entries) >= opts.Limit {
				break
			}
		}
		if !resp.More {
			break
		}
		startKey = nextEtcdRangeKey(resp.Kvs[len(resp.Kvs)-1].Key)
	}

	return entries, indexed, nil
}

func commitHistoryEntriesFromEtcdKVs(kvs []*mvccpb.KeyValue, opts *HistoryOptions) []*CommitHistoryEntry {
	var entries []*CommitHistoryEntry

	for _, kv := range kvs {
		entry, ok := commitHistoryEntryFromEtcdKV(kv, opts)
		if ok {
			entries = append(entries, entry)
		}
	}

	// Sort by timestamp descending (newest first)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp.After(entries[j].Timestamp)
	})

	// Apply offset and limit
	if opts.Offset > 0 {
		if opts.Offset >= len(entries) {
			return []*CommitHistoryEntry{}
		}
		entries = entries[opts.Offset:]
	}
	if opts.Limit > 0 && opts.Limit < len(entries) {
		entries = entries[:opts.Limit]
	}

	return entries
}

func commitHistoryEntryFromEtcdKV(kv *mvccpb.KeyValue, opts *HistoryOptions) (*CommitHistoryEntry, bool) {
	var entry commitEntry
	if err := json.Unmarshal(kv.Value, &entry); err != nil {
		return nil, false
	}

	if opts.ExcludeRollbacks && entry.IsRollback {
		return nil, false
	}
	if opts.User != "" && entry.User != opts.User {
		return nil, false
	}
	if !opts.StartTime.IsZero() && entry.Timestamp.Before(opts.StartTime) {
		return nil, false
	}
	if !opts.EndTime.IsZero() && entry.Timestamp.After(opts.EndTime) {
		return nil, false
	}

	return &CommitHistoryEntry{
		CommitID:   entry.CommitID,
		User:       entry.User,
		Timestamp:  entry.Timestamp,
		Message:    entry.Message,
		ConfigText: entry.ConfigText,
		IsRollback: entry.IsRollback,
		SourceIP:   entry.SourceIP,
	}, true
}

func (ds *etcdDatastore) commitHistoryIndexPrefix() string {
	return ds.key("commit-history") + "/"
}

func (ds *etcdDatastore) commitHistoryIndexKey(entry commitEntry) string {
	return ds.key("commit-history", commitHistoryIndexTimestampSegment(entry.Timestamp), entry.CommitID)
}

func commitHistoryIndexTimestampSegment(ts time.Time) string {
	ns := ts.UnixNano()
	if ns < 0 {
		ns = 0
	}
	return fmt.Sprintf("%019d", int64(math.MaxInt64)-ns)
}

func nextEtcdRangeKey(key []byte) string {
	next := append(append([]byte(nil), key...), 0)
	return string(next)
}

func (ds *etcdDatastore) ensureCommitHistoryIndex(ctx context.Context) error {
	versionKey := ds.key("metadata", "commit-history-index-version")
	versionResp, err := ds.client.Get(ctx, versionKey)
	if err != nil {
		return NewError(ErrCodeInternal, "failed to get commit history index version", err)
	}
	if len(versionResp.Kvs) > 0 && string(versionResp.Kvs[0].Value) == commitHistoryIndexVersion {
		return nil
	}

	commitsPrefix := ds.key("commits") + "/"
	resp, err := ds.client.Get(ctx, commitsPrefix, clientv3.WithPrefix())
	if err != nil {
		return NewError(ErrCodeInternal, "failed to scan commits for history index", err)
	}

	ops := make([]clientv3.Op, 0, commitHistoryIndexBatchSize)
	flush := func() error {
		if len(ops) == 0 {
			return nil
		}
		if _, err := ds.client.Txn(ctx).Then(ops...).Commit(); err != nil {
			return NewError(ErrCodeInternal, "failed to write commit history index", err)
		}
		ops = ops[:0]
		return nil
	}

	for _, kv := range resp.Kvs {
		var entry commitEntry
		if err := json.Unmarshal(kv.Value, &entry); err != nil || entry.CommitID == "" {
			continue
		}
		ops = append(ops, clientv3.OpPut(ds.commitHistoryIndexKey(entry), string(kv.Value)))
		if len(ops) >= commitHistoryIndexBatchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}

	ops = append(ops, clientv3.OpPut(versionKey, commitHistoryIndexVersion))
	return flush()
}

// GetCommit retrieves a specific commit by ID.
func (ds *etcdDatastore) GetCommit(ctx context.Context, commitID string) (*CommitHistoryEntry, error) {
	ctx, cancel := ds.withTimeout(ctx)
	defer cancel()

	commitKey := ds.key("commits", commitID)

	resp, err := ds.client.Get(ctx, commitKey)
	if err != nil {
		return nil, NewError(ErrCodeInternal, "failed to get commit", err)
	}

	if len(resp.Kvs) == 0 {
		return nil, NewError(ErrCodeNotFound, "commit not found", nil)
	}

	var entry commitEntry
	if err := json.Unmarshal(resp.Kvs[0].Value, &entry); err != nil {
		return nil, NewError(ErrCodeInternal, "failed to unmarshal commit entry", err)
	}

	return &CommitHistoryEntry{
		CommitID:   entry.CommitID,
		User:       entry.User,
		Timestamp:  entry.Timestamp,
		Message:    entry.Message,
		ConfigText: entry.ConfigText,
		IsRollback: entry.IsRollback,
		SourceIP:   entry.SourceIP,
	}, nil
}
