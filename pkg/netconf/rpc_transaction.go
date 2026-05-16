package netconf

import (
	"context"
	"encoding/xml"
	"fmt"
	"log"

	"github.com/akam1o/arca-router/pkg/config"
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

	cfg, err := TextToConfig(candidate.ConfigText)
	if err != nil {
		log.Printf("[NETCONF] Failed to parse candidate config before commit: %v", err)
		return NewErrorReply(rpc.MessageID, ErrValidationFailed(fmt.Sprintf("config parsing failed: %v", err)))
	}
	if rpcErr := validateConfigSemantics("commit", cfg); rpcErr != nil {
		log.Printf("[NETCONF] Commit validation failed for session %s: %v", sess.ID, rpcErr)
		return NewErrorReply(rpc.MessageID, rpcErr)
	}

	// Perform commit
	commitReq := &datastore.CommitRequest{
		SessionID: sess.ID,
		User:      sess.Username,
		SourceIP:  sess.RemoteAddr(),
		Message:   fmt.Sprintf("NETCONF commit by %s", sess.Username),
	}

	persist := func(ctx context.Context) (string, error) {
		return s.datastore.Commit(ctx, commitReq)
	}

	var commitID string
	if s.commitHook != nil {
		commitID, err = s.commitHook(ctx, &CommitHookRequest{
			SessionID:  sess.ID,
			User:       sess.Username,
			SourceIP:   sess.RemoteAddr(),
			Message:    commitReq.Message,
			ConfigText: candidate.ConfigText,
		}, persist)
	} else {
		commitID, err = persist(ctx)
	}
	if err != nil {
		log.Printf("[NETCONF] Commit failed for session %s: %v", sess.ID, err)
		// Check if it's a backend validation error
		return NewErrorReply(rpc.MessageID, ErrBackendValidationFailed(fmt.Sprintf("commit failed: %v", err)))
	}
	sess.RemoveLock(DatastoreCandidate)

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

func (r *ValidateRequest) SetInheritedNamespaceAttrs(attrs []xml.Attr) {
	if r.Source.Config != nil {
		r.Source.Config.InheritedAttrs = cloneXMLAttrs(attrs)
	}
}

// handleValidate handles <validate> RPC - validates datastore config
func (s *Server) handleValidate(ctx context.Context, sess *Session, rpc *RPC) *RPCReply {
	var req ValidateRequest
	if err := rpc.UnmarshalOperation(&req); err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	cfg, rpcErr := s.validateSourceConfig(ctx, sess, &req.Source)
	if rpcErr != nil {
		return NewErrorReply(rpc.MessageID, rpcErr)
	}

	if rpcErr := validateConfigSemantics("validate", cfg); rpcErr != nil {
		log.Printf("[NETCONF] Validation failed for session %s: %v", sess.ID, rpcErr)
		return NewErrorReply(rpc.MessageID, rpcErr)
	}

	log.Printf("[NETCONF] Validation successful for source config (session %s)", sess.ID)

	return NewOKReply(rpc.MessageID)
}

func (s *Server) validateSourceConfig(ctx context.Context, sess *Session, sourceReq *Source) (*config.Config, *RPCError) {
	if sourceReq.Config != nil {
		configXML, err := sourceReq.Config.XML()
		if err != nil {
			return nil, err.(*RPCError)
		}
		cfg, err := XMLToConfig(configXML, DefaultOpMerge)
		if err != nil {
			log.Printf("[NETCONF] Failed to parse inline validate source: %v", err)
			if rpcErr, ok := err.(*RPCError); ok {
				return nil, rpcErr.WithPath("/rpc/validate/source")
			}
			return nil, ErrValidationFailed(fmt.Sprintf("config parsing failed: %v", err))
		}
		return cfg, nil
	}

	source, err := sourceReq.GetDatastore()
	if err != nil {
		return nil, err.(*RPCError)
	}

	configText, rpcErr := s.validateSourceConfigText(ctx, sess, source)
	if rpcErr != nil {
		return nil, rpcErr
	}

	cfg, err := TextToConfig(configText)
	if err != nil {
		log.Printf("[NETCONF] Failed to parse %s config: %v", source, err)
		return nil, ErrValidationFailed(fmt.Sprintf("config parsing failed: %v", err))
	}
	return cfg, nil
}

func (s *Server) validateSourceConfigText(ctx context.Context, sess *Session, source string) (string, *RPCError) {
	switch source {
	case DatastoreRunning:
		running, err := s.datastore.GetRunning(ctx)
		if err != nil || running == nil {
			log.Printf("[NETCONF] No running config to validate for session %s: %v", sess.ID, err)
			return "", ErrOperationFailed("no running configuration to validate")
		}
		return running.ConfigText, nil
	case DatastoreCandidate:
		candidate, err := s.datastore.GetCandidate(ctx, sess.ID)
		if err != nil || candidate == nil {
			log.Printf("[NETCONF] No candidate config to validate for session %s: %v", sess.ID, err)
			return "", ErrOperationFailed("no candidate configuration to validate")
		}
		return candidate.ConfigText, nil
	case DatastoreStartup:
		return "", ErrStartupNotSupported("validate", "source")
	default:
		return "", ErrInvalidTarget("validate", source)
	}
}
