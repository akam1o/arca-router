// Package grpc implements the internal gRPC API server for arca-routerd.
// This is the unified entry point for both arca-cli and the NETCONF subsystem.
package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	"github.com/akam1o/arca-router/internal/store"
	"github.com/google/uuid"
	"google.golang.org/grpc"
)

// Server is the internal gRPC server that exposes configuration,
// session, and operational state services over a Unix socket.
type Server struct {
	engine   *engine.Engine
	store    store.ConfigStore
	sessions *SessionManager
	log      *slog.Logger
	server   *grpc.Server
}

// NewServer creates a new gRPC server.
func NewServer(eng *engine.Engine, st store.ConfigStore, log *slog.Logger) *Server {
	return &Server{
		engine:   eng,
		store:    st,
		sessions: NewSessionManager(),
		log:      log.With("component", "grpc"),
	}
}

// Serve starts the gRPC server on the given listener.
func (s *Server) Serve(lis net.Listener) error {
	s.server = grpc.NewServer()
	// Register services would go here once proto is compiled:
	// apiv1.RegisterConfigServiceServer(s.server, s)
	// apiv1.RegisterSessionServiceServer(s.server, s)
	// apiv1.RegisterStateServiceServer(s.server, s)
	s.log.Info("gRPC server starting", slog.String("address", lis.Addr().String()))
	return s.server.Serve(lis)
}

// Stop gracefully stops the gRPC server.
func (s *Server) Stop() {
	if s.server != nil {
		s.server.GracefulStop()
	}
}

// --- ConfigService implementation ---

// GetRunning returns the current running configuration.
func (s *Server) GetRunning(ctx context.Context) (configText string, version uint64, err error) {
	snap := s.engine.RunningSnapshot()
	if snap == nil {
		return "", 0, nil
	}
	// Serialize config to set-command text format for backward compat
	// For now, return JSON; text serialization can be added later
	return fmt.Sprintf("version: %d", snap.Version), snap.Version, nil
}

// EditCandidate applies set-command text to a session's candidate config.
func (s *Server) EditCandidate(ctx context.Context, sessionID, configText string) error {
	session, err := s.sessions.Get(sessionID)
	if err != nil {
		return err
	}
	if !session.HasLock {
		return fmt.Errorf("session %s does not hold the candidate lock", sessionID)
	}

	// Append to candidate text
	session.mu.Lock()
	if session.CandidateText != "" {
		session.CandidateText += "\n"
	}
	session.CandidateText += configText
	session.mu.Unlock()

	return nil
}

// Commit promotes the candidate configuration to running.
func (s *Server) Commit(ctx context.Context, sessionID, user, message string) (string, uint64, error) {
	session, err := s.sessions.Get(sessionID)
	if err != nil {
		return "", 0, err
	}
	if !session.HasLock {
		return "", 0, fmt.Errorf("session %s does not hold the candidate lock", sessionID)
	}

	// Parse the candidate config text using the existing pkg/config parser
	session.mu.RLock()
	candidateText := session.CandidateText
	session.mu.RUnlock()

	if candidateText == "" {
		return "", 0, fmt.Errorf("no candidate configuration to commit")
	}

	// Parse candidate text into new config model
	newCfg, err := parseConfigText(candidateText)
	if err != nil {
		return "", 0, fmt.Errorf("parse candidate config: %w", err)
	}

	// Apply via engine (diff + validate + apply atomically)
	if err := s.engine.Apply(ctx, newCfg, user, message); err != nil {
		return "", 0, err
	}

	// Persist to store
	snap := s.engine.RunningSnapshot()
	commitID := uuid.New().String()
	if err := s.store.SaveCommit(ctx, commitID, snap); err != nil {
		s.log.Error("Failed to persist commit (config applied but not persisted)",
			slog.String("commit_id", commitID),
			slog.Any("error", err))
	}

	return commitID, snap.Version, nil
}

// Discard clears the candidate configuration for a session.
func (s *Server) Discard(ctx context.Context, sessionID string) error {
	session, err := s.sessions.Get(sessionID)
	if err != nil {
		return err
	}
	session.mu.Lock()
	session.CandidateText = ""
	session.mu.Unlock()
	return nil
}

// --- SessionService implementation ---

// CreateSession creates a new configuration session.
func (s *Server) CreateSession(ctx context.Context, user string) (string, error) {
	return s.sessions.Create(user)
}

// CloseSession closes a configuration session.
func (s *Server) CloseSession(ctx context.Context, sessionID string) error {
	return s.sessions.Close(sessionID)
}

// AcquireLock acquires the exclusive candidate lock for a session.
func (s *Server) AcquireLock(ctx context.Context, sessionID, user string) error {
	return s.sessions.AcquireLock(sessionID)
}

// ReleaseLock releases the candidate lock.
func (s *Server) ReleaseLock(ctx context.Context, sessionID string) error {
	return s.sessions.ReleaseLock(sessionID)
}

// ConfigTextParser is a hook for parsing set-command text into legacy config.
// Set at initialization to break circular dependency with pkg/config.
var ConfigTextParser func(text string) (*model.RouterConfig, error)

// parseConfigText parses set-command text into the new config model.
func parseConfigText(text string) (*model.RouterConfig, error) {
	if ConfigTextParser != nil {
		return ConfigTextParser(text)
	}
	return nil, fmt.Errorf("config text parser not initialized")
}

// --- Session Management ---

// Session represents an active configuration session.
type Session struct {
	mu            sync.RWMutex
	ID            string
	User          string
	HasLock       bool
	CandidateText string
	CreatedAt     time.Time
}

// SessionManager manages active sessions with exclusive locking.
type SessionManager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	lockHeld string // session ID holding the candidate lock
}

// NewSessionManager creates a new session manager.
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
	}
}

// Create creates a new session.
func (m *SessionManager) Create(user string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := uuid.New().String()
	m.sessions[id] = &Session{
		ID:        id,
		User:      user,
		CreatedAt: time.Now(),
	}
	return id, nil
}

// Get retrieves an existing session.
func (m *SessionManager) Get(id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("session %s not found", id)
	}
	return s, nil
}

// Close closes a session and releases any held lock.
func (m *SessionManager) Close(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[id]
	if !ok {
		return fmt.Errorf("session %s not found", id)
	}
	if s.HasLock {
		s.HasLock = false
		m.lockHeld = ""
	}
	delete(m.sessions, id)
	return nil
}

// AcquireLock acquires the exclusive candidate lock for a session.
func (m *SessionManager) AcquireLock(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[id]
	if !ok {
		return fmt.Errorf("session %s not found", id)
	}
	if m.lockHeld != "" && m.lockHeld != id {
		return fmt.Errorf("candidate lock held by session %s", m.lockHeld)
	}
	s.HasLock = true
	m.lockHeld = id
	return nil
}

// ReleaseLock releases the candidate lock.
func (m *SessionManager) ReleaseLock(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[id]
	if !ok {
		return fmt.Errorf("session %s not found", id)
	}
	if !s.HasLock {
		return fmt.Errorf("session %s does not hold the candidate lock", id)
	}
	s.HasLock = false
	m.lockHeld = ""
	return nil
}
