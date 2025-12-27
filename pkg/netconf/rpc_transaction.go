package netconf

import (
	"context"
	"fmt"
	"log"

	"github.com/akam1o/arca-router/pkg/datastore"
)

// CommitRequest represents <commit> RPC
type CommitRequest struct {
	XMLName struct{} `xml:"commit"`
}

// handleCommit handles <commit> RPC - promotes candidate to running
func (s *Server) handleCommit(ctx context.Context, sess *Session, rpc *RPC) *RPCReply {
	var req CommitRequest
	if err := rpc.UnmarshalOperation(&req); err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	// Check if candidate lock is held by this session
	if lockErr := s.checkLockOwnership(ctx, sess, DatastoreCandidate, "commit"); lockErr != nil {
		return NewErrorReply(rpc.MessageID, lockErr)
	}

	// Check if candidate exists
	candidate, err := s.datastore.GetCandidate(ctx, sess.ID)
	if err != nil || candidate == nil {
		log.Printf("[NETCONF] No candidate config to commit for session %s: %v", sess.ID, err)
		return NewErrorReply(rpc.MessageID, ErrOperationFailed("no candidate configuration to commit"))
	}

	// Perform commit
	commitReq := &datastore.CommitRequest{
		SessionID: sess.ID,
		User:      sess.Username,
		SourceIP:  sess.RemoteAddr(),
		Message:   fmt.Sprintf("NETCONF commit by %s", sess.Username),
	}

	commitID, err := s.datastore.Commit(ctx, commitReq)
	if err != nil {
		log.Printf("[NETCONF] Commit failed for session %s: %v", sess.ID, err)
		// Check if it's a backend validation error
		return NewErrorReply(rpc.MessageID, ErrBackendValidationFailed(fmt.Sprintf("commit failed: %v", err)))
	}

	log.Printf("[NETCONF] Commit successful: %s (session: %s, user: %s)", commitID, sess.ID, sess.Username)

	return NewOKReply(rpc.MessageID)
}

// DiscardChangesRequest represents <discard-changes> RPC
type DiscardChangesRequest struct {
	XMLName struct{} `xml:"discard-changes"`
}

// handleDiscardChanges handles <discard-changes> RPC - discards candidate
func (s *Server) handleDiscardChanges(ctx context.Context, sess *Session, rpc *RPC) *RPCReply {
	var req DiscardChangesRequest
	if err := rpc.UnmarshalOperation(&req); err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	// Check if candidate lock is held by this session
	if lockErr := s.checkLockOwnership(ctx, sess, DatastoreCandidate, "discard-changes"); lockErr != nil {
		return NewErrorReply(rpc.MessageID, lockErr)
	}

	// Delete candidate (idempotent)
	if err := s.datastore.DeleteCandidate(ctx, sess.ID); err != nil {
		log.Printf("[NETCONF] Discard changes failed for session %s: %v", sess.ID, err)
		return NewErrorReply(rpc.MessageID, ErrDatastoreError(fmt.Sprintf("failed to discard candidate: %v", err)))
	}

	log.Printf("[NETCONF] Candidate discarded for session %s", sess.ID)

	return NewOKReply(rpc.MessageID)
}

// ValidateRequest represents <validate> RPC
type ValidateRequest struct {
	XMLName struct{} `xml:"validate"`
	Source  Source   `xml:"source"`
}

// handleValidate handles <validate> RPC - validates candidate config
func (s *Server) handleValidate(ctx context.Context, sess *Session, rpc *RPC) *RPCReply {
	var req ValidateRequest
	if err := rpc.UnmarshalOperation(&req); err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	// Get source datastore
	source, err := req.Source.GetDatastore()
	if err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	// Only candidate validation is supported
	if source != DatastoreCandidate {
		return NewErrorReply(rpc.MessageID, ErrInvalidTarget("validate", source))
	}

	// Get candidate config
	candidate, err := s.datastore.GetCandidate(ctx, sess.ID)
	if err != nil || candidate == nil {
		log.Printf("[NETCONF] No candidate config to validate for session %s: %v", sess.ID, err)
		return NewErrorReply(rpc.MessageID, ErrOperationFailed("no candidate configuration to validate"))
	}

	// Parse candidate text to config structure
	cfg, err := TextToConfig(candidate.ConfigText)
	if err != nil {
		log.Printf("[NETCONF] Failed to parse candidate config: %v", err)
		return NewErrorReply(rpc.MessageID, ErrValidationFailed(fmt.Sprintf("config parsing failed: %v", err)))
	}

	// Perform basic validation
	if err := ValidateConfig(cfg); err != nil {
		log.Printf("[NETCONF] Validation failed for session %s: %v", sess.ID, err)
		if rpcErr, ok := err.(*RPCError); ok {
			return NewErrorReply(rpc.MessageID, rpcErr)
		}
		return NewErrorReply(rpc.MessageID, ErrValidationFailed(fmt.Sprintf("validation error: %v", err)))
	}

	log.Printf("[NETCONF] Validation successful for session %s", sess.ID)

	return NewOKReply(rpc.MessageID)
}
