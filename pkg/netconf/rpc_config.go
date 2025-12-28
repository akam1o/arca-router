package netconf

import (
	"context"
	"encoding/xml"
	"fmt"
	"log"
)

// GetConfigRequest represents <get-config> RPC
type GetConfigRequest struct {
	XMLName xml.Name `xml:"get-config"`
	Source  Source   `xml:"source"`
	Filter  *Filter  `xml:"filter"`
}

// checkLockOwnership verifies if the session holds the lock for the target datastore.
// Write operations (edit-config, copy-config, delete-config, commit, discard-changes)
// require the session to hold the lock. Returns an RPCError if:
// - Lock is not acquired at all
// - Lock is held by another session
// Returns nil if this session holds the lock.
//
// rpcName should be the operation name (edit-config, copy-config, delete-config, commit, discard-changes).
// The error-path will be set to:
// - /rpc/{rpcName}/target for operations with explicit target element
// - /rpc/{rpcName} for operations without target element (commit, discard-changes)
func (s *Server) checkLockOwnership(ctx context.Context, sess *Session, target, rpcName string) *RPCError {
	lockInfo, err := s.datastore.GetLockInfo(ctx, target)
	if err != nil {
		log.Printf("[NETCONF] Failed to get lock info for %s: %v", target, err)
		return ErrDatastoreError(fmt.Sprintf("failed to check lock status for %s", target))
	}

	// Determine if RPC has explicit target element in XML
	hasTargetElement := (rpcName == "edit-config" || rpcName == "copy-config" || rpcName == "delete-config")

	// Check if lock is acquired
	if !lockInfo.IsLocked {
		// Lock not acquired - deny operation
		return ErrLockDenied(target, rpcName, hasTargetElement)
	}

	// Check if this session owns the lock
	if lockInfo.SessionID != sess.ID {
		// Lock held by another session - deny operation
		ownerNumericID := s.sessionIDToNumeric(lockInfo.SessionID)
		return ErrLockDeniedWithOwner(target, rpcName, ownerNumericID, hasTargetElement)
	}

	return nil
}

// handleGetConfig handles <get-config> RPC
func (s *Server) handleGetConfig(ctx context.Context, sess *Session, rpc *RPC) *RPCReply {
	var req GetConfigRequest
	if err := rpc.UnmarshalOperation(&req); err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	// Get datastore name
	datastore, err := req.Source.GetDatastore()
	if err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	// Validate filter
	if err := req.Filter.Validate("get-config"); err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	// Validate filter depth and size limits
	if err := ValidateFilterDepthAndSize("get-config", req.Filter); err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	// Get configuration text from datastore
	var textCfg string
	switch datastore {
	case DatastoreRunning:
		runningCfg, err := s.datastore.GetRunning(ctx)
		if err != nil {
			log.Printf("[NETCONF] GetConfig error for %s: %v", datastore, err)
			return NewErrorReply(rpc.MessageID, ErrDatastoreError(fmt.Sprintf("failed to retrieve %s config: %v", datastore, err)))
		}
		textCfg = runningCfg.ConfigText
	case DatastoreCandidate:
		candidateCfg, err := s.datastore.GetCandidate(ctx, sess.ID)
		if err != nil {
			log.Printf("[NETCONF] GetConfig error for %s: %v", datastore, err)
			return NewErrorReply(rpc.MessageID, ErrDatastoreError(fmt.Sprintf("failed to retrieve %s config: %v", datastore, err)))
		}
		textCfg = candidateCfg.ConfigText
	case DatastoreStartup:
		// Startup datastore not implemented (reads from /etc/arca-router/arca-router.conf at boot)
		return NewErrorReply(rpc.MessageID, ErrInvalidTarget("get-config", "startup"))
	default:
		return NewErrorReply(rpc.MessageID, ErrInvalidTarget("get-config", datastore))
	}

	// Convert text to config.Config structure
	cfg, err := TextToConfig(textCfg)
	if err != nil {
		log.Printf("[NETCONF] Text to config conversion error: %v", err)
		return NewErrorReply(rpc.MessageID, ErrDatastoreError(fmt.Sprintf("failed to parse %s config: %v", datastore, err)))
	}

	// Convert config to XML
	xmlData, err := ConfigToXML(cfg, req.Filter)
	if err != nil {
		log.Printf("[NETCONF] Config to XML conversion error: %v", err)
		return NewErrorReply(rpc.MessageID, ErrOperationFailed(fmt.Sprintf("config serialization failed: %v", err)))
	}

	return NewDataReply(rpc.MessageID, xmlData)
}

// EditConfigRequest represents <edit-config> RPC
type EditConfigRequest struct {
	XMLName          xml.Name          `xml:"edit-config"`
	Target           Target            `xml:"target"`
	DefaultOperation *DefaultOperation `xml:"default-operation"`
	TestOption       *TestOption       `xml:"test-option"`
	ErrorOption      *ErrorOption      `xml:"error-option"`
	Config           ConfigElement     `xml:"config"`
}

// ConfigElement represents <config> element in edit-config
type ConfigElement struct {
	Content []byte `xml:",innerxml"`
}

// handleEditConfig handles <edit-config> RPC
func (s *Server) handleEditConfig(ctx context.Context, sess *Session, rpc *RPC) *RPCReply {
	var req EditConfigRequest
	if err := rpc.UnmarshalOperation(&req); err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	// Get target datastore
	target, err := req.Target.GetDatastore()
	if err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	// Only candidate is writable (writable-running not supported)
	if target != DatastoreCandidate {
		if target == DatastoreRunning {
			return NewErrorReply(rpc.MessageID, ErrWritableRunningNotSupported())
		}
		return NewErrorReply(rpc.MessageID, ErrInvalidTarget("edit-config", target))
	}

	// Check if session holds candidate lock
	if lockErr := s.checkLockOwnership(ctx, sess, DatastoreCandidate, "edit-config"); lockErr != nil {
		return NewErrorReply(rpc.MessageID, lockErr)
	}

	// Validate test-option and error-option (Phase 2: not supported)
	if req.TestOption != nil && *req.TestOption != TestSet {
		return NewErrorReply(rpc.MessageID,
			NewRPCError(ErrorTypeProtocol, ErrorTagOperationNotSupported,
				fmt.Sprintf("unsupported test-option: %s", *req.TestOption)).
				WithPath("/rpc/edit-config/test-option").
				WithBadElement(string(*req.TestOption)))
	}
	if req.ErrorOption != nil && *req.ErrorOption != ErrorStop {
		return NewErrorReply(rpc.MessageID,
			NewRPCError(ErrorTypeProtocol, ErrorTagOperationNotSupported,
				fmt.Sprintf("unsupported error-option: %s", *req.ErrorOption)).
				WithPath("/rpc/edit-config/error-option").
				WithBadElement(string(*req.ErrorOption)))
	}

	// Set default operation (Phase 2: only merge supported)
	defaultOp := DefaultOpMerge
	if req.DefaultOperation != nil {
		defaultOp = *req.DefaultOperation
		// Validate: Phase 2 only supports merge
		if defaultOp != DefaultOpMerge {
			return NewErrorReply(rpc.MessageID,
				NewRPCError(ErrorTypeProtocol, ErrorTagOperationNotSupported,
					fmt.Sprintf("unsupported default-operation: %s", defaultOp)).
					WithPath("/rpc/edit-config/default-operation").
					WithBadElement(string(defaultOp)))
		}
	}

	// Parse config XML to internal config structure
	newCfg, err := XMLToConfig(req.Config.Content, defaultOp)
	if err != nil {
		log.Printf("[NETCONF] XML to config conversion error: %v", err)
		if rpcErr, ok := err.(*RPCError); ok {
			return NewErrorReply(rpc.MessageID, rpcErr)
		}
		return NewErrorReply(rpc.MessageID, ErrOperationFailed(fmt.Sprintf("config parsing failed: %v", err)))
	}

	// Get existing candidate text or create new from running
	var existingTextCfg string
	candidateCfg, err := s.datastore.GetCandidate(ctx, sess.ID)
	if err != nil {
		// Candidate doesn't exist, copy from running
		runningCfg, err := s.datastore.GetRunning(ctx)
		if err != nil {
			log.Printf("[NETCONF] Failed to get running config: %v", err)
			return NewErrorReply(rpc.MessageID, ErrDatastoreError("failed to initialize candidate"))
		}
		existingTextCfg = runningCfg.ConfigText
	} else {
		existingTextCfg = candidateCfg.ConfigText
	}

	// Convert existing text to config struct
	existingCfg, err := TextToConfig(existingTextCfg)
	if err != nil {
		log.Printf("[NETCONF] Failed to parse existing config: %v", err)
		return NewErrorReply(rpc.MessageID, ErrDatastoreError("failed to parse existing candidate"))
	}

	// Apply edit based on default-operation
	mergedCfg, err := ApplyConfigEdit(existingCfg, newCfg, defaultOp)
	if err != nil {
		log.Printf("[NETCONF] Config merge error: %v", err)
		if rpcErr, ok := err.(*RPCError); ok {
			return NewErrorReply(rpc.MessageID, rpcErr)
		}
		return NewErrorReply(rpc.MessageID, ErrOperationFailed(fmt.Sprintf("config merge failed: %v", err)))
	}

	// Convert merged config back to text
	mergedTextCfg, err := ConfigToText(mergedCfg)
	if err != nil {
		log.Printf("[NETCONF] Failed to convert merged config to text: %v", err)
		return NewErrorReply(rpc.MessageID, ErrDatastoreError("failed to serialize merged config"))
	}

	// Save merged config to candidate
	if err := s.datastore.SaveCandidate(ctx, sess.ID, mergedTextCfg); err != nil {
		log.Printf("[NETCONF] Failed to save candidate: %v", err)
		return NewErrorReply(rpc.MessageID, ErrDatastoreError(fmt.Sprintf("failed to save candidate: %v", err)))
	}

	return NewOKReply(rpc.MessageID)
}

// CopyConfigRequest represents <copy-config> RPC
type CopyConfigRequest struct {
	XMLName xml.Name `xml:"copy-config"`
	Target  Target   `xml:"target"`
	Source  Source   `xml:"source"`
}

// handleCopyConfig handles <copy-config> RPC
func (s *Server) handleCopyConfig(ctx context.Context, sess *Session, rpc *RPC) *RPCReply {
	var req CopyConfigRequest
	if err := rpc.UnmarshalOperation(&req); err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	// Get target and source datastores
	target, err := req.Target.GetDatastore()
	if err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	source, err := req.Source.GetDatastore()
	if err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	// Only candidate is writable as target
	if target != DatastoreCandidate {
		if target == DatastoreRunning {
			return NewErrorReply(rpc.MessageID, ErrWritableRunningNotSupported())
		}
		return NewErrorReply(rpc.MessageID, ErrInvalidTarget("copy-config", target))
	}

	// Check if session holds candidate lock
	if lockErr := s.checkLockOwnership(ctx, sess, DatastoreCandidate, "copy-config"); lockErr != nil {
		return NewErrorReply(rpc.MessageID, lockErr)
	}

	// Get source config text
	var srcTextCfg string
	switch source {
	case DatastoreRunning:
		runningCfg, err := s.datastore.GetRunning(ctx)
		if err != nil {
			log.Printf("[NETCONF] CopyConfig source read error: %v", err)
			return NewErrorReply(rpc.MessageID, ErrDatastoreError(fmt.Sprintf("failed to read source %s: %v", source, err)))
		}
		srcTextCfg = runningCfg.ConfigText
	case DatastoreCandidate:
		candidateCfg, err := s.datastore.GetCandidate(ctx, sess.ID)
		if err != nil {
			log.Printf("[NETCONF] CopyConfig source read error: %v", err)
			return NewErrorReply(rpc.MessageID, ErrDatastoreError(fmt.Sprintf("failed to read source %s: %v", source, err)))
		}
		srcTextCfg = candidateCfg.ConfigText
	default:
		return NewErrorReply(rpc.MessageID, ErrInvalidTarget("copy-config", source))
	}

	// Save to candidate
	if err := s.datastore.SaveCandidate(ctx, sess.ID, srcTextCfg); err != nil {
		log.Printf("[NETCONF] CopyConfig target write error: %v", err)
		return NewErrorReply(rpc.MessageID, ErrDatastoreError(fmt.Sprintf("failed to write target %s: %v", target, err)))
	}

	return NewOKReply(rpc.MessageID)
}

// DeleteConfigRequest represents <delete-config> RPC
type DeleteConfigRequest struct {
	XMLName xml.Name `xml:"delete-config"`
	Target  Target   `xml:"target"`
}

// handleDeleteConfig handles <delete-config> RPC
func (s *Server) handleDeleteConfig(ctx context.Context, sess *Session, rpc *RPC) *RPCReply {
	var req DeleteConfigRequest
	if err := rpc.UnmarshalOperation(&req); err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	// Get target datastore
	target, err := req.Target.GetDatastore()
	if err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	// Only candidate can be deleted
	if target != DatastoreCandidate {
		if target == DatastoreRunning {
			return NewErrorReply(rpc.MessageID, NewRPCError(ErrorTypeProtocol, ErrorTagOperationNotSupported, "cannot delete running datastore"))
		}
		return NewErrorReply(rpc.MessageID, ErrInvalidTarget("delete-config", target))
	}

	// Check if session holds candidate lock
	if lockErr := s.checkLockOwnership(ctx, sess, DatastoreCandidate, "delete-config"); lockErr != nil {
		return NewErrorReply(rpc.MessageID, lockErr)
	}

	// Delete candidate (idempotent)
	if err := s.datastore.DeleteCandidate(ctx, sess.ID); err != nil {
		log.Printf("[NETCONF] DeleteConfig error: %v", err)
		return NewErrorReply(rpc.MessageID, ErrDatastoreError(fmt.Sprintf("failed to delete candidate: %v", err)))
	}

	return NewOKReply(rpc.MessageID)
}
