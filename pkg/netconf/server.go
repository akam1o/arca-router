package netconf

import (
	"context"
	"fmt"
	"log"

	"github.com/akam1o/arca-router/pkg/datastore"
)

// Server represents NETCONF server with RPC dispatch
type Server struct {
	datastore           datastore.Datastore
	sessions            *SessionManager
	commitHook          CommitHook
	operationalProvider OperationalStateProvider
}

// CommitHookRequest contains the data needed to apply a NETCONF candidate
// through an external commit coordinator before it is persisted.
type CommitHookRequest struct {
	SessionID  string
	User       string
	SourceIP   string
	Message    string
	ConfigText string
}

// CommitHook can wrap the datastore commit path. The persist callback performs
// the legacy datastore commit after the hook has applied any external state.
type CommitHook func(ctx context.Context, req *CommitHookRequest, persist func(context.Context) (string, error)) (string, error)

// NewServer creates a new NETCONF server
func NewServer(ds datastore.Datastore, sm *SessionManager) *Server {
	return &Server{
		datastore: ds,
		sessions:  sm,
	}
}

// SetCommitHook installs a commit coordinator for NETCONF commits.
func (s *Server) SetCommitHook(h CommitHook) {
	if s == nil {
		return
	}
	s.commitHook = h
}

// SetOperationalStateProvider installs a live-state source for <get> replies.
func (s *Server) SetOperationalStateProvider(provider OperationalStateProvider) {
	if s == nil {
		return
	}
	s.operationalProvider = provider
}

// HandleRPC dispatches RPC to appropriate handler with RBAC enforcement
func (s *Server) HandleRPC(ctx context.Context, sess *Session, rpc *RPC) *RPCReply {
	if rpc == nil {
		return NewErrorReply("", ErrOperationFailed("rpc unavailable"))
	}
	if s == nil {
		return NewErrorReply(rpc.MessageID, ErrOperationFailed("server unavailable")).WithAttributes(rpc.ReplyAttrs)
	}
	if sess == nil {
		return NewErrorReply(rpc.MessageID, ErrOperationFailed("session unavailable")).WithAttributes(rpc.ReplyAttrs)
	}

	opName := rpc.GetOperationName()
	if opName == "" {
		return NewErrorReply(rpc.MessageID, ErrOperationFailed("rpc operation unavailable")).WithAttributes(rpc.ReplyAttrs)
	}

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
		return NewErrorReply(rpc.MessageID, ErrUnknownRPC(opName)).WithAttributes(rpc.ReplyAttrs)
	}

	// Check RBAC after confirming operation exists
	if err := s.checkRBAC(sess.Role, opName); err != nil {
		// Log RBAC denial for audit trail
		log.Printf("[RBAC] Access denied: user=%s role=%s operation=%s session=%s",
			sess.Username, sess.Role, opName, sess.ID)
		return NewErrorReply(rpc.MessageID, err).WithAttributes(rpc.ReplyAttrs)
	}

	// Execute handler
	return handler(ctx, sess, rpc).WithAttributes(rpc.ReplyAttrs)
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
		return NewErrorReply(rpc.MessageID, rpcErrorFromError(err))
	}

	if req.SessionID == 0 {
		return NewErrorReply(rpc.MessageID, NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue, "session-id must be non-zero"))
	}

	// Cannot kill own session
	if req.SessionID == sess.NumericID {
		return NewErrorReply(rpc.MessageID, NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue, "cannot kill own session"))
	}

	if s.sessions == nil {
		return NewErrorReply(rpc.MessageID, ErrOperationFailed("session manager unavailable"))
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
	if s == nil || s.sessions == nil {
		return 0
	}
	if sess, ok := s.sessions.Get(sessionID); ok {
		return sess.NumericID
	}
	return 0 // Session not found or already closed
}
