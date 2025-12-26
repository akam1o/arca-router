package datastore

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
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

// Commit promotes a candidate configuration to running configuration.
func (ds *etcdDatastore) Commit(ctx context.Context, req *CommitRequest) (string, error) {
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
		Key:       auditULID, // Set Key field for consistency
		Timestamp: now,
		User:      req.User,
		SessionID: req.SessionID,
		SourceIP:  req.SourceIP,
		Action:    "commit",
		Result:    "success",
		Details:   fmt.Sprintf("commit_id=%s message=%q", commitID, req.Message),
	}

	auditJSON, err := json.Marshal(auditEvent)
	if err != nil {
		return "", NewError(ErrCodeInternal, "failed to marshal audit event", err)
	}

	// Get lock key for transaction
	lockKey := ds.key("lock")
	runningMetadataKey := ds.key("running", "current")
	runningConfigKey := ds.key("running", "config")
	commitKey := ds.key("commits", commitID)
	auditKey := ds.key("audit", auditULID)

	// Get current lock to verify ownership
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
			clientv3.Compare(clientv3.Value(candidateKey), "=", candidateValue), // Candidate unchanged
			clientv3.Compare(clientv3.ModRevision(candidateKey), "=", candidateModRevision), // No concurrent modification
		).
		Then(
			clientv3.OpPut(runningMetadataKey, string(metadataJSON)),
			clientv3.OpPut(runningConfigKey, candidateData.ConfigText),
			clientv3.OpPut(commitKey, string(entryJSON)),
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
		ds.client.Revoke(ctx, clientv3.LeaseID(currentLock.LeaseID))
	}

	return commitID, nil
}

// Rollback reverts to a previous commit.
func (ds *etcdDatastore) Rollback(ctx context.Context, req *RollbackRequest) (string, error) {
	ctx, cancel := ds.withTimeout(ctx)
	defer cancel()

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
		Key:       auditULID, // Set Key field for consistency
		Timestamp: now,
		User:      req.User,
		SourceIP:  req.SourceIP,
		Action:    "rollback",
		Result:    "success",
		Details:   fmt.Sprintf("rollback_commit_id=%s target_commit_id=%s message=%q", newCommitID, req.CommitID, req.Message),
	}

	auditJSON, err := json.Marshal(auditEvent)
	if err != nil {
		return "", NewError(ErrCodeInternal, "failed to marshal audit event", err)
	}

	// Keys
	runningMetadataKey := ds.key("running", "current")
	runningConfigKey := ds.key("running", "config")
	commitKey := ds.key("commits", newCommitID)
	auditKey := ds.key("audit", auditULID)

	// Atomic rollback transaction
	txnResp, err := ds.client.Txn(ctx).
		Then(
			clientv3.OpPut(runningMetadataKey, string(metadataJSON)),
			clientv3.OpPut(runningConfigKey, targetCommit.ConfigText),
			clientv3.OpPut(commitKey, string(entryJSON)),
			clientv3.OpPut(auditKey, string(auditJSON)),
		).
		Commit()

	if err != nil {
		return "", NewError(ErrCodeInternal, "failed to rollback transaction", err)
	}

	if !txnResp.Succeeded {
		return "", NewError(ErrCodeInternal, "rollback transaction failed", nil)
	}

	return newCommitID, nil
}

// ListCommitHistory retrieves commit history with optional filtering.
func (ds *etcdDatastore) ListCommitHistory(ctx context.Context, opts *HistoryOptions) ([]*CommitHistoryEntry, error) {
	ctx, cancel := ds.withTimeout(ctx)
	defer cancel()

	// Set default options if nil
	if opts == nil {
		opts = &HistoryOptions{}
	}

	// Get commits from etcd with server-side pagination
	commitsPrefix := ds.key("commits") + "/"

	// Build options for etcd Get request
	getOpts := []clientv3.OpOption{
		clientv3.WithPrefix(),
		clientv3.WithSort(clientv3.SortByKey, clientv3.SortDescend),
	}

	// Apply server-side limit if specified (more efficient than client-side filtering)
	// Note: We fetch more than needed to account for filtering, then truncate
	if opts.Limit > 0 {
		// Fetch extra entries to account for client-side filtering
		// Multiply by 2 to handle filtering overhead (conservative estimate)
		fetchLimit := int64(opts.Limit * 2)
		getOpts = append(getOpts, clientv3.WithLimit(fetchLimit))
	}

	resp, err := ds.client.Get(ctx, commitsPrefix, getOpts...)
	if err != nil {
		return nil, NewError(ErrCodeInternal, "failed to list commits", err)
	}

	var entries []*CommitHistoryEntry

	for _, kv := range resp.Kvs {
		var entry commitEntry
		if err := json.Unmarshal(kv.Value, &entry); err != nil {
			// Skip malformed entries
			continue
		}

		// Apply filters (client-side for complex predicates)
		if opts.ExcludeRollbacks && entry.IsRollback {
			continue
		}

		if opts.User != "" && entry.User != opts.User {
			continue
		}

		if !opts.StartTime.IsZero() && entry.Timestamp.Before(opts.StartTime) {
			continue
		}

		if !opts.EndTime.IsZero() && entry.Timestamp.After(opts.EndTime) {
			continue
		}

		entries = append(entries, &CommitHistoryEntry{
			CommitID:   entry.CommitID,
			User:       entry.User,
			Timestamp:  entry.Timestamp,
			Message:    entry.Message,
			ConfigText: entry.ConfigText,
			IsRollback: entry.IsRollback,
			SourceIP:   entry.SourceIP,
		})
	}

	// Sort by timestamp descending (newest first)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp.After(entries[j].Timestamp)
	})

	// Apply offset and limit
	if opts.Offset > 0 {
		if opts.Offset >= len(entries) {
			return []*CommitHistoryEntry{}, nil
		}
		entries = entries[opts.Offset:]
	}

	if opts.Limit > 0 && opts.Limit < len(entries) {
		entries = entries[:opts.Limit]
	}

	return entries, nil
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

// generateCommitKey generates a sortable key for commit history.
// Format: <timestamp-unix-nano>_<commit-id>
// This ensures commits are naturally sorted by time in etcd.
func generateCommitKey(timestamp time.Time, commitID string) string {
	return fmt.Sprintf("%020d_%s", timestamp.UnixNano(), commitID)
}

// parseCommitKey extracts commit ID from a commit key.
func parseCommitKey(key string) string {
	// Key format: /arca-router/commits/<timestamp>_<commit-id>
	parts := strings.Split(key, "/")
	if len(parts) == 0 {
		return ""
	}

	lastPart := parts[len(parts)-1]
	// Split by underscore to get commit ID
	idParts := strings.SplitN(lastPart, "_", 2)
	if len(idParts) != 2 {
		return lastPart // Fallback to whole part
	}

	return idParts[1]
}
