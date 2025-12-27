package netconf

import (
	"context"
	"fmt"
	"log"

	"github.com/akam1o/arca-router/pkg/datastore"
)

// Server represents NETCONF server with RPC dispatch
type Server struct {
	datastore datastore.Datastore
	sessions  *SessionManager
}

// NewServer creates a new NETCONF server
func NewServer(ds datastore.Datastore, sm *SessionManager) *Server {
	return &Server{
		datastore: ds,
		sessions:  sm,
	}
}

// HandleRPC dispatches RPC to appropriate handler with RBAC enforcement
func (s *Server) HandleRPC(ctx context.Context, sess *Session, rpc *RPC) *RPCReply {
	opName := rpc.GetOperationName()

	// Update session last used timestamp
	sess.UpdateLastUsed()

	// Dispatch to operation handler (check if operation exists first)
	var handler func(context.Context, *Session, *RPC) *RPCReply

	switch opName {
	case "get-config":
		handler = s.handleGetConfig
	case "edit-config":
		handler = s.handleEditConfig
	case "copy-config":
		handler = s.handleCopyConfig
	case "delete-config":
		handler = s.handleDeleteConfig
	case "lock":
		handler = s.handleLock
	case "unlock":
		handler = s.handleUnlock
	case "commit":
		handler = s.handleCommit
	case "discard-changes":
		handler = s.handleDiscardChanges
	case "validate":
		handler = s.handleValidate
	case "get":
		handler = s.handleGet
	case "close-session":
		handler = s.handleCloseSession
	case "kill-session":
		handler = s.handleKillSession
	default:
		// Unknown operation -> operation-not-supported (not access-denied)
		return NewErrorReply(rpc.MessageID, ErrUnknownRPC(opName))
	}

	// Check RBAC after confirming operation exists
	if err := s.checkRBAC(sess.Role, opName); err != nil {
		return NewErrorReply(rpc.MessageID, err)
	}

	// Execute handler
	return handler(ctx, sess, rpc)
}

// checkRBAC enforces role-based access control per design document Section 4
func (s *Server) checkRBAC(role, operation string) *RPCError {
	// Define RBAC matrix per design document
	readOnlyOps := map[string]bool{
		"get-config": true,
		"get":        true,
	}

	operatorOps := map[string]bool{
		"get-config":      true,
		"get":             true,
		"lock":            true,
		"unlock":          true,
		"edit-config":     true,
		"validate":        true,
		"commit":          true,
		"discard-changes": true,
		"copy-config":     true,
		"delete-config":   true,
		"close-session":   true,
	}

	adminOps := map[string]bool{
		"get-config":      true,
		"get":             true,
		"lock":            true,
		"unlock":          true,
		"edit-config":     true,
		"validate":        true,
		"commit":          true,
		"discard-changes": true,
		"copy-config":     true,
		"delete-config":   true,
		"close-session":   true,
		"kill-session":    true,
	}

	switch role {
	case RoleReadOnly:
		if !readOnlyOps[operation] {
			return ErrAccessDenied(operation, "read-only role cannot perform this operation")
		}
	case RoleOperator:
		if !operatorOps[operation] {
			return ErrAccessDenied(operation, "operator role cannot perform this operation")
		}
	case RoleAdmin:
		if !adminOps[operation] {
			return ErrAccessDenied(operation, "unknown operation")
		}
	default:
		return ErrAccessDenied(operation, "unknown role")
	}

	return nil
}

// handleCloseSession handles <close-session> RPC
func (s *Server) handleCloseSession(ctx context.Context, sess *Session, rpc *RPC) *RPCReply {
	// Session cleanup will be handled by SSH server after reply is sent
	return NewOKReply(rpc.MessageID)
}

// handleKillSession handles <kill-session> RPC (admin only)
func (s *Server) handleKillSession(ctx context.Context, sess *Session, rpc *RPC) *RPCReply {
	type KillSession struct {
		XMLName   struct{} `xml:"kill-session"`
		SessionID uint32   `xml:"session-id"` // RFC 6241: session-id is an integer
	}

	var req KillSession
	if err := rpc.UnmarshalOperation(&req); err != nil {
		return NewErrorReply(rpc.MessageID, err.(*RPCError))
	}

	if req.SessionID == 0 {
		return NewErrorReply(rpc.MessageID, NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue, "session-id must be non-zero"))
	}

	// Cannot kill own session
	if req.SessionID == sess.NumericID {
		return NewErrorReply(rpc.MessageID, NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue, "cannot kill own session"))
	}

	// Kill the target session by numeric ID
	if err := s.sessions.CloseSessionByNumericID(req.SessionID); err != nil {
		log.Printf("[NETCONF] Failed to kill session %d: %v", req.SessionID, err)
		return NewErrorReply(rpc.MessageID, NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue, fmt.Sprintf("unknown session-id: %d", req.SessionID)))
	}

	return NewOKReply(rpc.MessageID)
}

// ErrOperationFailed is a helper for generic operation failures
func ErrOperationFailed(message string) *RPCError {
	return NewRPCError(ErrorTypeApplication, ErrorTagOperationFailed, message)
}

// sessionIDToNumeric converts UUID session ID to numeric ID for RFC 6241 compliance
// Returns 0 if session not found (caller should handle as unknown session)
func (s *Server) sessionIDToNumeric(sessionID string) uint32 {
	if sess, ok := s.sessions.Get(sessionID); ok {
		return sess.NumericID
	}
	return 0 // Session not found or already closed
}
