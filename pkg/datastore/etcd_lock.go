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

// lockKeyForTarget generates the lock key path for a specific target.
func (ds *etcdDatastore) lockKeyForTarget(target string) string {
	return ds.key("lock/" + target)
}

// legacyLockKey returns the legacy lock key path (for migration detection).
func (ds *etcdDatastore) legacyLockKey() string {
	return ds.key("lock")
}

// checkLegacyLock checks if a legacy lock key exists and returns an error if found.
// This implements fail-closed behavior for mixed-version deployments.
func (ds *etcdDatastore) checkLegacyLock(ctx context.Context) error {
	legacyKey := ds.legacyLockKey()
	resp, err := ds.client.Get(ctx, legacyKey)
	if err != nil {
		return NewError(ErrCodeInternal, "failed to check legacy lock", err)
	}

	if len(resp.Kvs) > 0 {
		return NewError(ErrCodeConflict,
			"legacy lock key detected at "+legacyKey+"; migrate to target-based locks before use",
			nil)
	}
	return nil
}

// AcquireLock attempts to acquire the exclusive configuration lock for a specific target.
func (ds *etcdDatastore) AcquireLock(ctx context.Context, req *LockRequest) error {
	// Validate target
	if err := ValidateLockTarget(req.Target); err != nil {
		return err
	}

	ctx, cancel := ds.withTimeout(ctx)
	defer cancel()

	// Check for legacy lock (fail-closed)
	if err := ds.checkLegacyLock(ctx); err != nil {
		return err
	}

	lockKey := ds.lockKeyForTarget(req.Target)
	now := time.Now()

	// Set default timeout if not specified
	timeout := req.Timeout
	if timeout == 0 {
		timeout = 30 * time.Minute
	}
	expiresAt := now.Add(timeout)

	// Create etcd lease for TTL-based lock expiration
	ttlSeconds := int64(timeout.Seconds())
	if ttlSeconds < 1 {
		ttlSeconds = 1
	}

	leaseResp, err := ds.client.Grant(ctx, ttlSeconds)
	if err != nil {
		return NewError(ErrCodeInternal, fmt.Sprintf("failed to create lease for %s lock", req.Target), err)
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
		ds.client.Revoke(ctx, leaseID)
		return NewError(ErrCodeInternal, fmt.Sprintf("failed to marshal %s lock data", req.Target), err)
	}

	// Check current lock state
	getLockResp, err := ds.client.Get(ctx, lockKey)
	if err != nil {
		ds.client.Revoke(ctx, leaseID)
		return NewError(ErrCodeInternal, fmt.Sprintf("failed to check existing %s lock", req.Target), err)
	}

	// If lock exists, check if it's expired or held by this session
	if len(getLockResp.Kvs) > 0 {
		existingValue := string(getLockResp.Kvs[0].Value)
		existingModRevision := getLockResp.Kvs[0].ModRevision

		var existingLock lockData
		if err := json.Unmarshal(getLockResp.Kvs[0].Value, &existingLock); err != nil {
			// Malformed lock data - delete it with CAS
			deleteTxn, delErr := ds.client.Txn(ctx).
				If(clientv3.Compare(clientv3.ModRevision(lockKey), "=", existingModRevision)).
				Then(clientv3.OpDelete(lockKey)).
				Commit()

			if delErr != nil || !deleteTxn.Succeeded {
				ds.client.Revoke(ctx, leaseID)
				return NewError(ErrCodeConflict, fmt.Sprintf("failed to delete malformed %s lock", req.Target), delErr)
			}
		} else {
			// Check if lock is held by this session (allow re-acquire)
			if existingLock.SessionID == req.SessionID {
				ds.client.Revoke(ctx, leaseID)
				return ds.ExtendLock(ctx, req.Target, req.SessionID, timeout)
			}

			// Check if lease is still active (server-side TTL)
			if existingLock.LeaseID > 0 {
				leaseTTLResp, leaseErr := ds.client.TimeToLive(ctx, clientv3.LeaseID(existingLock.LeaseID))
				if leaseErr != nil {
					ds.client.Revoke(ctx, leaseID)
					return NewError(ErrCodeConflict, "cannot verify lock status", leaseErr)
				}
				if leaseTTLResp.TTL > 0 {
					ds.client.Revoke(ctx, leaseID)
					return NewError(ErrCodeConflict,
						fmt.Sprintf("%s lock already held by session %s (user: %s)", req.Target, existingLock.SessionID, existingLock.User),
						nil)
				}
			}

			// Lock is expired - delete with CAS
			if existingLock.LeaseID > 0 {
				ds.client.Revoke(ctx, clientv3.LeaseID(existingLock.LeaseID))
			}

			deleteTxn, delErr := ds.client.Txn(ctx).
				If(
					clientv3.Compare(clientv3.Value(lockKey), "=", existingValue),
					clientv3.Compare(clientv3.ModRevision(lockKey), "=", existingModRevision),
				).
				Then(clientv3.OpDelete(lockKey)).
				Commit()

			if delErr != nil || !deleteTxn.Succeeded {
				ds.client.Revoke(ctx, leaseID)
				return NewError(ErrCodeConflict, fmt.Sprintf("%s lock was modified during expiration check", req.Target), delErr)
			}
		}
	}

	// Attempt to acquire lock
	txnResp, err := ds.client.Txn(ctx).
		If(clientv3.Compare(clientv3.ModRevision(lockKey), "=", 0)).
		Then(clientv3.OpPut(lockKey, string(lockJSON), clientv3.WithLease(leaseID))).
		Else(clientv3.OpGet(lockKey)).
		Commit()

	if err != nil {
		ds.client.Revoke(ctx, leaseID)
		return NewError(ErrCodeInternal, fmt.Sprintf("failed to acquire %s lock transaction", req.Target), err)
	}

	if !txnResp.Succeeded {
		ds.client.Revoke(ctx, leaseID)
		if len(txnResp.Responses) > 0 && len(txnResp.Responses[0].GetResponseRange().Kvs) > 0 {
			var currentLock lockData
			if err := json.Unmarshal(txnResp.Responses[0].GetResponseRange().Kvs[0].Value, &currentLock); err == nil {
				return NewError(ErrCodeConflict,
					fmt.Sprintf("%s lock acquired by another session %s (user: %s) during attempt", req.Target, currentLock.SessionID, currentLock.User),
					nil)
			}
		}
		return NewError(ErrCodeConflict, fmt.Sprintf("%s lock acquired by another session during attempt", req.Target), nil)
	}

	// Log audit event with target
	auditEvent := &AuditEvent{
		Timestamp: now,
		User:      req.User,
		SessionID: req.SessionID,
		Action:    "lock_acquire",
		Result:    "success",
		Details:   fmt.Sprintf("target=%s, timeout=%v", req.Target, timeout),
	}

	if err := ds.LogAuditEvent(ctx, auditEvent); err != nil {
		_ = err // Non-critical
	}

	return nil
}

// ReleaseLock releases the configuration lock held by a session for a specific target.
func (ds *etcdDatastore) ReleaseLock(ctx context.Context, target string, sessionID string) error {
	// Validate target
	if err := ValidateLockTarget(target); err != nil {
		return err
	}

	ctx, cancel := ds.withTimeout(ctx)
	defer cancel()

	// Check for legacy lock (fail-closed)
	if err := ds.checkLegacyLock(ctx); err != nil {
		return err
	}

	lockKey := ds.lockKeyForTarget(target)

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
			fmt.Sprintf("%s lock is held by another session %s", target, currentLock.SessionID),
			nil)
	}

	// Release lock with CAS
	txnResp, err := ds.client.Txn(ctx).
		If(clientv3.Compare(clientv3.Value(lockKey), "=", string(getLockResp.Kvs[0].Value))).
		Then(clientv3.OpDelete(lockKey)).
		Commit()

	if err != nil {
		return NewError(ErrCodeInternal, fmt.Sprintf("failed to release %s lock", target), err)
	}

	if !txnResp.Succeeded {
		return NewError(ErrCodeConflict, fmt.Sprintf("%s lock was modified during release attempt", target), nil)
	}

	// Revoke lease
	if currentLock.LeaseID > 0 {
		ds.client.Revoke(ctx, clientv3.LeaseID(currentLock.LeaseID))
	}

	// Log audit event with target
	auditEvent := &AuditEvent{
		Timestamp: time.Now(),
		User:      currentLock.User,
		SessionID: sessionID,
		Action:    "lock_release",
		Result:    "success",
		Details:   fmt.Sprintf("target=%s", target),
	}

	if err := ds.LogAuditEvent(ctx, auditEvent); err != nil {
		_ = err // Non-critical
	}

	return nil
}

// ExtendLock extends the expiration time of an existing lock for a specific target.
func (ds *etcdDatastore) ExtendLock(ctx context.Context, target string, sessionID string, duration time.Duration) error {
	// Validate target
	if err := ValidateLockTarget(target); err != nil {
		return err
	}

	ctx, cancel := ds.withTimeout(ctx)
	defer cancel()

	// Check for legacy lock (fail-closed)
	if err := ds.checkLegacyLock(ctx); err != nil {
		return err
	}

	lockKey := ds.lockKeyForTarget(target)

	// Set default duration
	if duration == 0 {
		duration = 30 * time.Minute
	}

	// Get current lock
	getLockResp, err := ds.client.Get(ctx, lockKey)
	if err != nil {
		return NewError(ErrCodeInternal, fmt.Sprintf("failed to get current %s lock", target), err)
	}

	if len(getLockResp.Kvs) == 0 {
		return NewError(ErrCodeNotFound, fmt.Sprintf("no %s lock to extend", target), nil)
	}

	currentValue := string(getLockResp.Kvs[0].Value)
	currentModRevision := getLockResp.Kvs[0].ModRevision

	// Parse lock data
	var currentLock lockData
	if err := json.Unmarshal(getLockResp.Kvs[0].Value, &currentLock); err != nil {
		return NewError(ErrCodeInternal, fmt.Sprintf("failed to parse %s lock data", target), err)
	}

	// Verify session owns the lock
	if currentLock.SessionID != sessionID {
		return NewError(ErrCodeConflict,
			fmt.Sprintf("%s lock is held by another session %s", target, currentLock.SessionID),
			nil)
	}

	// Check if lease is still active (server-side TTL to avoid time skew)
	if currentLock.LeaseID > 0 {
		leaseTTLResp, leaseErr := ds.client.TimeToLive(ctx, clientv3.LeaseID(currentLock.LeaseID))
		if leaseErr != nil || leaseTTLResp.TTL <= 0 {
			return NewError(ErrCodeConflict,
				fmt.Sprintf("%s lock has expired, cannot extend (re-acquire lock instead)", target), nil)
		}
	}

	now := time.Now()
	oldLeaseID := currentLock.LeaseID

	// Create new lease
	ttlSeconds := int64(duration.Seconds())
	if ttlSeconds < 1 {
		ttlSeconds = 1
	}

	newLeaseResp, err := ds.client.Grant(ctx, ttlSeconds)
	if err != nil {
		return NewError(ErrCodeInternal, fmt.Sprintf("failed to create new lease for %s lock extension", target), err)
	}

	newLeaseID := newLeaseResp.ID

	// Update lock data
	currentLock.ExpiresAt = now.Add(duration)
	currentLock.LeaseID = int64(newLeaseID)

	newLockJSON, err := json.Marshal(currentLock)
	if err != nil {
		ds.client.Revoke(ctx, newLeaseID)
		return NewError(ErrCodeInternal, fmt.Sprintf("failed to marshal updated %s lock data", target), err)
	}

	// Atomic update with CAS
	txnResp, err := ds.client.Txn(ctx).
		If(
			clientv3.Compare(clientv3.Value(lockKey), "=", currentValue),
			clientv3.Compare(clientv3.ModRevision(lockKey), "=", currentModRevision),
		).
		Then(clientv3.OpPut(lockKey, string(newLockJSON), clientv3.WithLease(newLeaseID))).
		Commit()

	if err != nil {
		ds.client.Revoke(ctx, newLeaseID)
		return NewError(ErrCodeInternal, fmt.Sprintf("failed to extend %s lock transaction", target), err)
	}

	if !txnResp.Succeeded {
		ds.client.Revoke(ctx, newLeaseID)
		return NewError(ErrCodeConflict, fmt.Sprintf("%s lock was modified during extension attempt", target), nil)
	}

	// Revoke old lease
	if oldLeaseID > 0 && int64(newLeaseID) != oldLeaseID {
		ds.client.Revoke(ctx, clientv3.LeaseID(oldLeaseID))
	}

	// Log audit event for lock extension
	auditEvent := &AuditEvent{
		Timestamp: time.Now(),
		User:      currentLock.User,
		SessionID: sessionID,
		Action:    "lock_extend",
		Result:    "success",
		Details:   fmt.Sprintf("target=%s, duration=%v", target, duration),
	}

	if err := ds.LogAuditEvent(ctx, auditEvent); err != nil {
		_ = err // Non-critical
	}

	return nil
}

// StealLock forcibly takes the lock from another session for a specific target (admin only).
func (ds *etcdDatastore) StealLock(ctx context.Context, req *StealLockRequest) error {
	// Validate target
	if err := ValidateLockTarget(req.Target); err != nil {
		return err
	}

	ctx, cancel := ds.withTimeout(ctx)
	defer cancel()

	// Check for legacy lock (fail-closed)
	if err := ds.checkLegacyLock(ctx); err != nil {
		return err
	}

	lockKey := ds.lockKeyForTarget(req.Target)

	// Get current lock with revision for CAS
	getLockResp, err := ds.client.Get(ctx, lockKey)
	if err != nil {
		return NewError(ErrCodeInternal, fmt.Sprintf("failed to get current %s lock", req.Target), err)
	}

	var oldUser string
	var oldSessionID string
	var oldLeaseID int64
	var existingValue string
	var existingModRevision int64

	if len(getLockResp.Kvs) > 0 {
		existingValue = string(getLockResp.Kvs[0].Value)
		existingModRevision = getLockResp.Kvs[0].ModRevision

		var currentLock lockData
		if err := json.Unmarshal(getLockResp.Kvs[0].Value, &currentLock); err == nil {
			oldUser = currentLock.User
			oldSessionID = currentLock.SessionID
			oldLeaseID = currentLock.LeaseID

			if req.TargetSessionID != "" && currentLock.SessionID != req.TargetSessionID {
				return NewError(ErrCodeConflict,
					fmt.Sprintf("%s lock is held by %s, not %s", req.Target, currentLock.SessionID, req.TargetSessionID),
					nil)
			}
		}
	}

	// Delete old lock with CAS
	if len(getLockResp.Kvs) > 0 {
		deleteTxn, delErr := ds.client.Txn(ctx).
			If(
				clientv3.Compare(clientv3.Value(lockKey), "=", existingValue),
				clientv3.Compare(clientv3.ModRevision(lockKey), "=", existingModRevision),
			).
			Then(clientv3.OpDelete(lockKey)).
			Commit()

		if delErr != nil || !deleteTxn.Succeeded {
			failAuditEvent := &AuditEvent{
				Timestamp: time.Now(),
				User:      req.User,
				SessionID: req.NewSessionID,
				Action:    "lock_steal",
				Result:    "failure",
				Details:   fmt.Sprintf("target=%s, failed to delete lock from user=%s session=%s", req.Target, oldUser, oldSessionID),
			}
			ds.LogAuditEvent(ctx, failAuditEvent)
			if delErr != nil {
				return NewError(ErrCodeInternal, fmt.Sprintf("failed to delete old %s lock", req.Target), delErr)
			}
			return NewError(ErrCodeConflict, fmt.Sprintf("%s lock was modified during steal attempt", req.Target), nil)
		}
	}

	// Revoke old lease
	if oldLeaseID > 0 {
		ds.client.Revoke(ctx, clientv3.LeaseID(oldLeaseID))
	}

	// Acquire new lock
	acquireErr := ds.AcquireLock(ctx, &LockRequest{
		Target:    req.Target,
		SessionID: req.NewSessionID,
		User:      req.User,
		Timeout:   30 * time.Minute,
	})

	if acquireErr != nil {
		failAuditEvent := &AuditEvent{
			Timestamp: time.Now(),
			User:      req.User,
			SessionID: req.NewSessionID,
			Action:    "lock_steal",
			Result:    "failure",
			Details:   fmt.Sprintf("target=%s, deleted old lock from user=%s session=%s but failed to acquire: %v", req.Target, oldUser, oldSessionID, acquireErr),
		}
		ds.LogAuditEvent(ctx, failAuditEvent)
		return acquireErr
	}

	// Log success audit
	successAuditEvent := &AuditEvent{
		Timestamp: time.Now(),
		User:      req.User,
		SessionID: req.NewSessionID,
		Action:    "lock_steal",
		Result:    "success",
		Details:   fmt.Sprintf("target=%s, stolen from user=%s session=%s reason=%s", req.Target, oldUser, oldSessionID, req.Reason),
	}

	if err := ds.LogAuditEvent(ctx, successAuditEvent); err != nil {
		_ = err // Non-critical
	}

	return nil
}

// GetLockInfo retrieves the current lock state for a specific target.
func (ds *etcdDatastore) GetLockInfo(ctx context.Context, target string) (*LockInfo, error) {
	// Validate target
	if err := ValidateLockTarget(target); err != nil {
		return nil, err
	}

	ctx, cancel := ds.withTimeout(ctx)
	defer cancel()

	// Check for legacy lock (fail-closed)
	if err := ds.checkLegacyLock(ctx); err != nil {
		return nil, err
	}

	lockKey := ds.lockKeyForTarget(target)

	// Get current lock
	getLockResp, err := ds.client.Get(ctx, lockKey)
	if err != nil {
		return nil, NewError(ErrCodeInternal, fmt.Sprintf("failed to get %s lock info", target), err)
	}

	if len(getLockResp.Kvs) == 0 {
		return &LockInfo{
			IsLocked: false,
		}, nil
	}

	// Parse lock data
	var currentLock lockData
	if err := json.Unmarshal(getLockResp.Kvs[0].Value, &currentLock); err != nil {
		return nil, NewError(ErrCodeInternal, fmt.Sprintf("failed to parse %s lock data", target), err)
	}

	// Check if lease is still active (server-side TTL)
	if currentLock.LeaseID > 0 {
		leaseTTLResp, leaseErr := ds.client.TimeToLive(ctx, clientv3.LeaseID(currentLock.LeaseID))
		if leaseErr != nil || leaseTTLResp.TTL <= 0 {
			return &LockInfo{
				IsLocked: false,
			}, nil
		}
	}

	return &LockInfo{
		IsLocked:   true,
		SessionID:  currentLock.SessionID,
		User:       currentLock.User,
		AcquiredAt: currentLock.AcquiredAt,
		ExpiresAt:  currentLock.ExpiresAt,
	}, nil
}
