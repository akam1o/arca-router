package netconf

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/akam1o/arca-router/pkg/datastore"
)

// LockRequest represents <lock> RPC
type LockRequest struct {
	XMLName struct{} `xml:"lock"`
	Target  Target   `xml:"target"`
}

// UnlockRequest represents <unlock> RPC
type UnlockRequest struct {
	XMLName struct{} `xml:"unlock"`
	Target  Target   `xml:"target"`
}

// handleLock handles <lock> RPC
func (s *Server) handleLock(ctx context.Context, sess *Session, rpc *RPC) *RPCReply {
	var req LockRequest
	if err := rpc.UnmarshalOperation(&req); err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	// Get target datastore
	target, err := req.Target.GetDatastore()
	if err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	// Validate target (candidate and running are allowed)
	if target != DatastoreCandidate && target != DatastoreRunning {
		return NewErrorReply(rpc.MessageID, ErrInvalidTarget("lock", target))
	}

	// Acquire lock with timeout (default: 1 hour absolute, 5 min idle)
	lockReq := &datastore.LockRequest{
		Target:    target,
		SessionID: sess.ID,
		User:      sess.Username,
		Timeout:   3600 * time.Second, // 1 hour absolute timeout
	}

	if err := s.datastore.AcquireLock(ctx, lockReq); err != nil {
		log.Printf("[NETCONF] Lock acquisition failed for %s by session %s: %v", target, sess.ID, err)

		// Check if lock is held by another session
		existingLock, getErr := s.datastore.GetLockInfo(ctx, target)
		if getErr == nil && existingLock != nil && existingLock.SessionID != sess.ID {
			// Convert UUID to NumericID for RFC 6241 compliant error
			ownerNumericID := s.sessionIDToNumeric(existingLock.SessionID)
			return NewErrorReply(rpc.MessageID, ErrLockDeniedForLock(target, ownerNumericID))
		}

		return NewErrorReply(rpc.MessageID, ErrDatastoreError(fmt.Sprintf("failed to acquire lock on %s: %v", target, err)))
	}

	log.Printf("[NETCONF] Lock acquired on %s by session %s (user: %s)", target, sess.ID, sess.Username)

	// Track lock in session for cleanup
	sess.AddLock(target)

	return NewOKReply(rpc.MessageID)
}

// handleUnlock handles <unlock> RPC
func (s *Server) handleUnlock(ctx context.Context, sess *Session, rpc *RPC) *RPCReply {
	var req UnlockRequest
	if err := rpc.UnmarshalOperation(&req); err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	// Get target datastore
	target, err := req.Target.GetDatastore()
	if err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	// Validate target
	if target != DatastoreCandidate && target != DatastoreRunning {
		return NewErrorReply(rpc.MessageID, ErrInvalidTarget("unlock", target))
	}

	// Check if lock exists and is owned by this session
	lockInfo, err := s.datastore.GetLockInfo(ctx, target)
	if err != nil {
		log.Printf("[NETCONF] Failed to get lock info for %s: %v", target, err)
		return NewErrorReply(rpc.MessageID, ErrDatastoreError(fmt.Sprintf("failed to check lock status: %v", err)))
	}

	if lockInfo == nil {
		// Lock doesn't exist (already released or timeout)
		return NewErrorReply(rpc.MessageID, ErrLockTimeout(target))
	}

	if lockInfo.SessionID != sess.ID {
		// Lock is held by another session
		ownerNumericID := s.sessionIDToNumeric(lockInfo.SessionID)
		return NewErrorReply(rpc.MessageID, ErrLockDeniedForUnlock(target, ownerNumericID))
	}

	// Release lock
	if err := s.datastore.ReleaseLock(ctx, target, sess.ID); err != nil {
		log.Printf("[NETCONF] Lock release failed for %s by session %s: %v", target, sess.ID, err)
		return NewErrorReply(rpc.MessageID, ErrDatastoreError(fmt.Sprintf("failed to release lock on %s: %v", target, err)))
	}

	log.Printf("[NETCONF] Lock released on %s by session %s", target, sess.ID)

	// Remove lock from session tracking
	sess.RemoveLock(target)

	return NewOKReply(rpc.MessageID)
}
