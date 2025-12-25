package datastore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// lockData represents the data stored in the lock key.
type lockData struct {
	SessionID  string    `json:"session_id"`
	User       string    `json:"user"`
	AcquiredAt time.Time `json:"acquired_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	LeaseID    int64     `json:"lease_id"` // etcd lease ID for TTL management
}

// AcquireLock attempts to acquire the exclusive configuration lock.
func (ds *etcdDatastore) AcquireLock(ctx context.Context, req *LockRequest) error {
	ctx, cancel := ds.withTimeout(ctx)
	defer cancel()

	lockKey := ds.key("lock")
	now := time.Now()

	// Set default timeout if not specified
	timeout := req.Timeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	expiresAt := now.Add(timeout)

	// Create etcd lease for TTL-based lock expiration
	// Convert timeout to seconds (minimum 1 second)
	ttlSeconds := int64(timeout.Seconds())
	if ttlSeconds < 1 {
		ttlSeconds = 1
	}

	leaseResp, err := ds.client.Grant(ctx, ttlSeconds)
	if err != nil {
		return NewError(ErrCodeInternal, "failed to create lease for lock", err)
	}

	leaseID := leaseResp.ID

	// Prepare lock data
	lock := lockData{
		SessionID:  req.SessionID,
		User:       req.User,
		AcquiredAt: now,
		ExpiresAt:  expiresAt,
		LeaseID:    int64(leaseID),
	}

	lockJSON, err := json.Marshal(lock)
	if err != nil {
		// Revoke lease if we can't marshal
		ds.client.Revoke(ctx, leaseID)
		return NewError(ErrCodeInternal, "failed to marshal lock data", err)
	}

	// Atomic lock acquisition using transaction:
	// - Condition: Lock key does not exist OR lock has expired
	// - Success: Put lock with lease
	// - Failure: Return conflict error

	// First, check current lock state
	getLockResp, err := ds.client.Get(ctx, lockKey)
	if err != nil {
		ds.client.Revoke(ctx, leaseID)
		return NewError(ErrCodeInternal, "failed to check existing lock", err)
	}

	// If lock exists, check if it's expired or held by this session
	if len(getLockResp.Kvs) > 0 {
		existingValue := string(getLockResp.Kvs[0].Value)
		existingModRevision := getLockResp.Kvs[0].ModRevision

		var existingLock lockData
		if err := json.Unmarshal(getLockResp.Kvs[0].Value, &existingLock); err != nil {
			// Malformed lock data - delete it with CAS and retry acquisition
			deleteTxn, delErr := ds.client.Txn(ctx).
				If(clientv3.Compare(clientv3.ModRevision(lockKey), "=", existingModRevision)).
				Then(clientv3.OpDelete(lockKey)).
				Commit()

			if delErr != nil || !deleteTxn.Succeeded {
				ds.client.Revoke(ctx, leaseID)
				return NewError(ErrCodeConflict, "failed to delete malformed lock", delErr)
			}

			// Lock deleted, fall through to acquire
		} else {
			// Check if lock is held by this session (allow re-acquire)
			if existingLock.SessionID == req.SessionID {
				// Same session - allow (this is effectively an extend)
				ds.client.Revoke(ctx, leaseID) // Revoke new lease
				return ds.ExtendLock(ctx, req.SessionID, timeout)
			}

			// Check if lock is expired
			if now.Before(existingLock.ExpiresAt) {
				// Lock is still valid and held by another session
				ds.client.Revoke(ctx, leaseID)
				return NewError(ErrCodeConflict,
					fmt.Sprintf("lock already held by session %s (user: %s)", existingLock.SessionID, existingLock.User),
					nil)
			}

			// Lock is expired - delete it with CAS to prevent race
			// Revoke old lease first
			if existingLock.LeaseID > 0 {
				ds.client.Revoke(ctx, clientv3.LeaseID(existingLock.LeaseID))
			}

			// Delete expired lock with CAS to ensure we're deleting the same lock we checked
			deleteTxn, delErr := ds.client.Txn(ctx).
				If(
					clientv3.Compare(clientv3.Value(lockKey), "=", existingValue),
					clientv3.Compare(clientv3.ModRevision(lockKey), "=", existingModRevision),
				).
				Then(clientv3.OpDelete(lockKey)).
				Commit()

			if delErr != nil {
				ds.client.Revoke(ctx, leaseID)
				return NewError(ErrCodeInternal, "failed to delete expired lock", delErr)
			}

			if !deleteTxn.Succeeded {
				// Lock was modified by another process - retry acquisition
				ds.client.Revoke(ctx, leaseID)
				return NewError(ErrCodeConflict, "lock was modified during expiration check, retry", nil)
			}

			// Lock successfully deleted, fall through to acquire
		}
	}

	// Attempt to acquire lock using transaction
	// Compare: Lock key does not exist (ModRevision == 0) after deletion
	txnResp, err := ds.client.Txn(ctx).
		If(clientv3.Compare(clientv3.ModRevision(lockKey), "=", 0)).
		Then(clientv3.OpPut(lockKey, string(lockJSON), clientv3.WithLease(leaseID))).
		Else(clientv3.OpGet(lockKey)).
		Commit()

	if err != nil {
		ds.client.Revoke(ctx, leaseID)
		return NewError(ErrCodeInternal, "failed to acquire lock transaction", err)
	}

	if !txnResp.Succeeded {
		// Transaction failed - lock was created by another process
		ds.client.Revoke(ctx, leaseID)

		// Get the current lock holder info
		if len(txnResp.Responses) > 0 && len(txnResp.Responses[0].GetResponseRange().Kvs) > 0 {
			var currentLock lockData
			if err := json.Unmarshal(txnResp.Responses[0].GetResponseRange().Kvs[0].Value, &currentLock); err == nil {
				return NewError(ErrCodeConflict,
					fmt.Sprintf("lock acquired by another session %s (user: %s) during attempt", currentLock.SessionID, currentLock.User),
					nil)
			}
		}

		return NewError(ErrCodeConflict, "lock acquired by another session during attempt", nil)
	}

	// Log audit event
	auditEvent := &AuditEvent{
		Timestamp: now,
		User:      req.User,
		SessionID: req.SessionID,
		Action:    "lock_acquire",
		Result:    "success",
		Details:   fmt.Sprintf("timeout: %v", timeout),
	}

	if err := ds.LogAuditEvent(ctx, auditEvent); err != nil {
		// Log audit failure but don't fail the lock acquisition
		// In production, this would be sent to a monitoring system
		_ = err
	}

	return nil
}

// ReleaseLock releases the configuration lock held by a session.
func (ds *etcdDatastore) ReleaseLock(ctx context.Context, sessionID string) error {
	ctx, cancel := ds.withTimeout(ctx)
	defer cancel()

	lockKey := ds.key("lock")

	// Get current lock
	getLockResp, err := ds.client.Get(ctx, lockKey)
	if err != nil {
		return NewError(ErrCodeInternal, "failed to get current lock", err)
	}

	if len(getLockResp.Kvs) == 0 {
		// Lock doesn't exist - idempotent success
		return nil
	}

	// Parse lock data
	var currentLock lockData
	if err := json.Unmarshal(getLockResp.Kvs[0].Value, &currentLock); err != nil {
		// Malformed lock data - delete it anyway
		ds.client.Delete(ctx, lockKey)
		return nil
	}

	// Verify session owns the lock
	if currentLock.SessionID != sessionID {
		return NewError(ErrCodeConflict,
			fmt.Sprintf("lock is held by another session %s", currentLock.SessionID),
			nil)
	}

	// Release lock using transaction to ensure atomicity
	txnResp, err := ds.client.Txn(ctx).
		If(clientv3.Compare(clientv3.Value(lockKey), "=", string(getLockResp.Kvs[0].Value))).
		Then(clientv3.OpDelete(lockKey)).
		Commit()

	if err != nil {
		return NewError(ErrCodeInternal, "failed to release lock", err)
	}

	if !txnResp.Succeeded {
		// Lock was modified by another process
		return NewError(ErrCodeConflict, "lock was modified during release attempt", nil)
	}

	// Revoke lease
	if currentLock.LeaseID > 0 {
		ds.client.Revoke(ctx, clientv3.LeaseID(currentLock.LeaseID))
	}

	// Log audit event
	auditEvent := &AuditEvent{
		Timestamp: time.Now(),
		User:      currentLock.User,
		SessionID: sessionID,
		Action:    "lock_release",
		Result:    "success",
	}

	if err := ds.LogAuditEvent(ctx, auditEvent); err != nil {
		_ = err // Non-critical
	}

	return nil
}

// ExtendLock extends the expiration time of an existing lock.
func (ds *etcdDatastore) ExtendLock(ctx context.Context, sessionID string, duration time.Duration) error {
	ctx, cancel := ds.withTimeout(ctx)
	defer cancel()

	lockKey := ds.key("lock")

	// Set default duration if not specified (match SQLite behavior)
	if duration == 0 {
		duration = 30 * time.Minute
	}

	// Get current lock
	getLockResp, err := ds.client.Get(ctx, lockKey)
	if err != nil {
		return NewError(ErrCodeInternal, "failed to get current lock", err)
	}

	if len(getLockResp.Kvs) == 0 {
		return NewError(ErrCodeNotFound, "lock not found", nil)
	}

	currentValue := string(getLockResp.Kvs[0].Value)
	currentModRevision := getLockResp.Kvs[0].ModRevision

	// Parse lock data
	var currentLock lockData
	if err := json.Unmarshal(getLockResp.Kvs[0].Value, &currentLock); err != nil {
		return NewError(ErrCodeInternal, "failed to parse lock data", err)
	}

	// Verify session owns the lock
	if currentLock.SessionID != sessionID {
		return NewError(ErrCodeConflict,
			fmt.Sprintf("lock is held by another session %s", currentLock.SessionID),
			nil)
	}

	// Check if lock is expired
	now := time.Now()
	if now.After(currentLock.ExpiresAt) {
		return NewError(ErrCodeConflict, "lock has expired", nil)
	}

	// Save old lease ID before updating
	oldLeaseID := currentLock.LeaseID

	// Create new lease with the requested duration
	ttlSeconds := int64(duration.Seconds())
	if ttlSeconds < 1 {
		ttlSeconds = 1
	}

	newLeaseResp, err := ds.client.Grant(ctx, ttlSeconds)
	if err != nil {
		return NewError(ErrCodeInternal, "failed to create new lease for extension", err)
	}

	newLeaseID := newLeaseResp.ID

	// Update lock data with new lease and expiration
	currentLock.ExpiresAt = now.Add(duration)
	currentLock.LeaseID = int64(newLeaseID)

	newLockJSON, err := json.Marshal(currentLock)
	if err != nil {
		ds.client.Revoke(ctx, newLeaseID) // Clean up new lease on error
		return NewError(ErrCodeInternal, "failed to marshal updated lock data", err)
	}

	// Atomic update using transaction:
	// - Condition: Lock value and ModRevision match (no other process modified it)
	// - Success: Update lock with new lease
	// - Failure: Return conflict error
	txnResp, err := ds.client.Txn(ctx).
		If(
			clientv3.Compare(clientv3.Value(lockKey), "=", currentValue),
			clientv3.Compare(clientv3.ModRevision(lockKey), "=", currentModRevision),
		).
		Then(clientv3.OpPut(lockKey, string(newLockJSON), clientv3.WithLease(newLeaseID))).
		Commit()

	if err != nil {
		ds.client.Revoke(ctx, newLeaseID) // Clean up new lease on error
		return NewError(ErrCodeInternal, "failed to extend lock transaction", err)
	}

	if !txnResp.Succeeded {
		// Transaction failed - lock was modified by another process
		ds.client.Revoke(ctx, newLeaseID) // Clean up new lease
		return NewError(ErrCodeConflict, "lock was modified during extension attempt", nil)
	}

	// Revoke old lease (use saved oldLeaseID before it was overwritten)
	if oldLeaseID > 0 && int64(newLeaseID) != oldLeaseID {
		ds.client.Revoke(ctx, clientv3.LeaseID(oldLeaseID))
	}

	return nil
}

// StealLock forcibly takes the lock from another session (admin only).
func (ds *etcdDatastore) StealLock(ctx context.Context, req *StealLockRequest) error {
	ctx, cancel := ds.withTimeout(ctx)
	defer cancel()

	lockKey := ds.key("lock")

	// Get current lock
	getLockResp, err := ds.client.Get(ctx, lockKey)
	if err != nil {
		return NewError(ErrCodeInternal, "failed to get current lock", err)
	}

	var oldUser string
	var oldSessionID string
	var oldLeaseID int64

	if len(getLockResp.Kvs) > 0 {
		var currentLock lockData
		if err := json.Unmarshal(getLockResp.Kvs[0].Value, &currentLock); err == nil {
			oldUser = currentLock.User
			oldSessionID = currentLock.SessionID
			oldLeaseID = currentLock.LeaseID

			// Verify target session matches
			if req.TargetSessionID != "" && currentLock.SessionID != req.TargetSessionID {
				return NewError(ErrCodeConflict,
					fmt.Sprintf("lock is held by %s, not %s", currentLock.SessionID, req.TargetSessionID),
					nil)
			}
		}
	}

	// Log audit event for stealing
	stealAuditEvent := &AuditEvent{
		Timestamp: time.Now(),
		User:      req.User,
		SessionID: req.NewSessionID,
		Action:    "lock_steal",
		Result:    "success",
		Details:   fmt.Sprintf("stolen from user=%s session=%s reason=%s", oldUser, oldSessionID, req.Reason),
	}

	if err := ds.LogAuditEvent(ctx, stealAuditEvent); err != nil {
		_ = err // Non-critical
	}

	// Revoke old lease
	if oldLeaseID > 0 {
		ds.client.Revoke(ctx, clientv3.LeaseID(oldLeaseID))
	}

	// Delete old lock
	_, err = ds.client.Delete(ctx, lockKey)
	if err != nil {
		return NewError(ErrCodeInternal, "failed to delete old lock", err)
	}

	// Acquire new lock for the new session
	return ds.AcquireLock(ctx, &LockRequest{
		SessionID: req.NewSessionID,
		User:      req.User,
		Timeout:   30 * time.Minute,
	})
}

// GetLockInfo retrieves the current lock state.
func (ds *etcdDatastore) GetLockInfo(ctx context.Context) (*LockInfo, error) {
	ctx, cancel := ds.withTimeout(ctx)
	defer cancel()

	lockKey := ds.key("lock")

	// Get current lock
	getLockResp, err := ds.client.Get(ctx, lockKey)
	if err != nil {
		return nil, NewError(ErrCodeInternal, "failed to get lock info", err)
	}

	if len(getLockResp.Kvs) == 0 {
		// No lock exists
		return &LockInfo{
			IsLocked: false,
		}, nil
	}

	// Parse lock data
	var currentLock lockData
	if err := json.Unmarshal(getLockResp.Kvs[0].Value, &currentLock); err != nil {
		return nil, NewError(ErrCodeInternal, "failed to parse lock data", err)
	}

	// Check if lock is expired
	now := time.Now()
	if now.After(currentLock.ExpiresAt) {
		// Lock is expired but not yet cleaned up
		return &LockInfo{
			IsLocked: false,
		}, nil
	}

	return &LockInfo{
		IsLocked:   true,
		SessionID:  currentLock.SessionID,
		User:       currentLock.User,
		AcquiredAt: currentLock.AcquiredAt,
		ExpiresAt:  currentLock.ExpiresAt,
	}, nil
}
