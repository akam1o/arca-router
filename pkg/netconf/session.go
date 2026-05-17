package netconf

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"

	"github.com/akam1o/arca-router/pkg/logger"
)

// sessionIDCounter is an atomic counter for generating numeric session IDs
// RFC 6241 specifies session-id as an integer (uint32)
var sessionIDCounter uint32

// NETCONFSession represents a NETCONF session
type NETCONFSession struct {
	ID              string // UUID v4 (internal identifier)
	NumericID       uint32 // RFC 6241 session-id (integer for NETCONF protocol)
	Username        string
	Role            string // admin, operator, read-only
	CreatedAt       time.Time
	LastUsed        time.Time
	IdleTimeout     time.Duration // Idle timeout (e.g., 30m)
	AbsoluteTimeout time.Duration // Absolute max lifetime (e.g., 24h)
	BaseVersion     string        // "1.0" or "1.1" (NETCONF protocol version)
	conn            ssh.Conn
	channel         ssh.Channel
	ctx             context.Context
	cancel          context.CancelFunc
	datastoreLocks  map[string]struct{} // Set of locked datastores ("candidate", "running")
	mu              sync.RWMutex        // Protects datastoreLocks and LastUsed
}

// SessionManager manages NETCONF sessions
type SessionManager struct {
	sessions       map[string]*NETCONFSession // UUID -> Session
	numericIDIndex map[uint32]*NETCONFSession // NumericID -> Session (for RFC 6241 operations)
	config         *SSHConfig
	datastore      DatastoreLockReleaser // For lock cleanup on session close
	mu             sync.RWMutex
	cleanupMu      sync.Mutex
	cleanup        *time.Ticker
	cleanupDone    chan struct{}
	cleanupStopped sync.Once
	log            *logger.Logger
}

// DatastoreLockReleaser interface for session cleanup (minimal subset)
// This avoids circular dependency with full datastore.Datastore interface
type DatastoreLockReleaser interface {
	ReleaseLock(ctx context.Context, target string, sessionID string) error
}

// NewSessionManager creates a new session manager
func NewSessionManager(config *SSHConfig, ds DatastoreLockReleaser, log *logger.Logger) *SessionManager {
	config = sessionManagerConfigWithDefaults(config)
	if log == nil {
		log = logger.New("netconf-session", logger.DefaultConfig())
	}

	return &SessionManager{
		sessions:       make(map[string]*NETCONFSession),
		numericIDIndex: make(map[uint32]*NETCONFSession),
		config:         config,
		datastore:      ds,
		cleanupDone:    make(chan struct{}),
		log:            log,
	}
}

func sessionManagerConfigWithDefaults(config *SSHConfig) *SSHConfig {
	defaults := DefaultSSHConfig()
	if config == nil {
		return defaults
	}
	merged := *config
	if merged.IdleTimeout <= 0 {
		merged.IdleTimeout = defaults.IdleTimeout
	}
	if merged.AbsoluteTimeout <= 0 {
		merged.AbsoluteTimeout = defaults.AbsoluteTimeout
	}
	if merged.MaxSessions <= 0 {
		merged.MaxSessions = defaults.MaxSessions
	}
	return &merged
}

// Create creates a new NETCONF session
func (sm *SessionManager) Create(username, role string, conn ssh.Conn, channel ssh.Channel) *NETCONFSession {
	if sm == nil {
		return nil
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.ensureRuntimeStateLocked()

	ctx, cancel := context.WithCancel(context.Background())

	session := &NETCONFSession{
		ID:              uuid.New().String(),
		NumericID:       atomic.AddUint32(&sessionIDCounter, 1),
		Username:        username,
		Role:            role,
		CreatedAt:       time.Now(),
		LastUsed:        time.Now(),
		IdleTimeout:     sm.config.IdleTimeout,
		AbsoluteTimeout: sm.config.AbsoluteTimeout,
		BaseVersion:     "1.1", // Default, will be negotiated
		conn:            conn,
		channel:         channel,
		ctx:             ctx,
		cancel:          cancel,
		datastoreLocks:  make(map[string]struct{}),
	}

	sm.sessions[session.ID] = session
	sm.numericIDIndex[session.NumericID] = session
	sm.log.Info("Session created", "id", session.ID, "numeric_id", session.NumericID, "user", username, "role", role)

	return session
}

func (sm *SessionManager) ensureRuntimeStateLocked() {
	if sm.sessions == nil {
		sm.sessions = make(map[string]*NETCONFSession)
	}
	if sm.numericIDIndex == nil {
		sm.numericIDIndex = make(map[uint32]*NETCONFSession)
	}
	sm.config = sessionManagerConfigWithDefaults(sm.config)
	if sm.log == nil {
		sm.log = logger.New("netconf-session", logger.DefaultConfig())
	}
}

// Get retrieves a session by UUID
func (sm *SessionManager) Get(id string) (*NETCONFSession, bool) {
	if sm == nil {
		return nil, false
	}

	sm.mu.RLock()
	defer sm.mu.RUnlock()
	session, ok := sm.sessions[id]
	return session, ok
}

// GetByNumericID retrieves a session by numeric ID (RFC 6241 session-id)
func (sm *SessionManager) GetByNumericID(numericID uint32) (*NETCONFSession, bool) {
	if sm == nil {
		return nil, false
	}

	sm.mu.RLock()
	defer sm.mu.RUnlock()
	session, ok := sm.numericIDIndex[numericID]
	return session, ok
}

// Count returns the number of active sessions
func (sm *SessionManager) Count() int {
	if sm == nil {
		return 0
	}

	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.sessions)
}

// CloseAll closes all active sessions and stops cleanup
func (sm *SessionManager) CloseAll() {
	if sm == nil {
		return
	}

	// Stop cleanup goroutine if running (only once)
	sm.cleanupStopped.Do(func() {
		if sm.cleanupDone != nil {
			close(sm.cleanupDone)
		}
		sm.cleanupMu.Lock()
		if sm.cleanup != nil {
			sm.cleanup.Stop()
			sm.cleanup = nil
		}
		sm.cleanupMu.Unlock()
	})

	sm.mu.Lock()
	sessions := make([]*NETCONFSession, 0, len(sm.sessions))
	for _, session := range sm.sessions {
		sessions = append(sessions, session)
	}
	sm.sessions = make(map[string]*NETCONFSession)
	sm.numericIDIndex = make(map[uint32]*NETCONFSession)
	sm.mu.Unlock()

	for _, session := range sessions {
		sm.closeSession(session, "server shutdown")
	}
}

// StartCleanup starts the session cleanup goroutine
func (sm *SessionManager) StartCleanup(ctx context.Context, wg *sync.WaitGroup) {
	if wg != nil {
		defer wg.Done()
	}
	if sm == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	ticker := time.NewTicker(1 * time.Minute)
	sm.cleanupMu.Lock()
	sm.cleanup = ticker
	sm.cleanupMu.Unlock()
	defer ticker.Stop()

	sm.mu.Lock()
	if sm.cleanupDone == nil {
		sm.cleanupDone = make(chan struct{})
	}
	cleanupDone := sm.cleanupDone
	sm.mu.Unlock()

	for {
		select {
		case <-ctx.Done():
			return
		case <-cleanupDone:
			return
		case <-ticker.C:
			sm.cleanupExpiredSessions(ctx)
		}
	}
}

// cleanupExpiredSessions removes expired sessions
func (sm *SessionManager) cleanupExpiredSessions(ctx context.Context) {
	if sm == nil {
		return
	}

	now := time.Now()
	var toClose []*NETCONFSession

	sm.mu.Lock()
	for id, session := range sm.sessions {
		// Read LastUsed with lock held
		session.mu.RLock()
		lastUsed := session.LastUsed
		session.mu.RUnlock()

		// Check absolute timeout
		if now.Sub(session.CreatedAt) > session.AbsoluteTimeout {
			toClose = append(toClose, session)
			delete(sm.sessions, id)
			delete(sm.numericIDIndex, session.NumericID)
			if sm.log != nil {
				sm.log.Info("Session expired (absolute timeout)", "id", id, "user", session.Username)
			}
			continue
		}

		// Check idle timeout
		if now.Sub(lastUsed) > session.IdleTimeout {
			toClose = append(toClose, session)
			delete(sm.sessions, id)
			delete(sm.numericIDIndex, session.NumericID)
			if sm.log != nil {
				sm.log.Info("Session expired (idle timeout)", "id", id, "user", session.Username)
			}
		}
	}
	sm.mu.Unlock()

	// Close sessions outside lock
	for _, session := range toClose {
		sm.closeSession(session, "timeout")
	}
}

// closeSession closes a session and releases resources
func (sm *SessionManager) closeSession(session *NETCONFSession, reason string) {
	if sm == nil || session == nil {
		return
	}
	if session.cancel != nil {
		session.cancel()
	}

	// Release all held datastore locks
	releasedLocks := 0
	if sm.datastore != nil {
		locks := session.GetLocks()
		for _, target := range locks {
			// Use background context since session context is cancelled
			ctx := context.Background()

			// Release lock using datastore interface
			if err := sm.datastore.ReleaseLock(ctx, target, session.ID); err != nil {
				if sm.log != nil {
					sm.log.Warn("Failed to release lock on session close",
						"session_id", session.ID,
						"target", target,
						"error", err)
				}
			} else {
				session.RemoveLock(target)
				releasedLocks++
				if sm.log != nil {
					sm.log.Info("Lock released on session close",
						"session_id", session.ID,
						"target", target)
				}
			}
		}
	}

	// Close SSH connection to force chans/reqs to terminate
	// This ensures handleConnection exits cleanly during shutdown
	if session.conn != nil {
		_ = session.conn.Close()
	}

	if session.channel != nil {
		_ = session.channel.Close()
	}

	if sm.log != nil {
		sm.log.Info("Session closed", "id", session.ID, "user", session.Username, "reason", reason, "locks_released", releasedLocks)
	}
}

// UpdateLastUsed updates the last used timestamp for a session
func (sm *SessionManager) UpdateLastUsed(id string) {
	if sm == nil {
		return
	}

	sm.mu.RLock()
	session, ok := sm.sessions[id]
	sm.mu.RUnlock()

	if ok {
		session.mu.Lock()
		session.LastUsed = time.Now()
		session.mu.Unlock()
	}
}

// CloseSession closes a specific session by UUID
func (sm *SessionManager) CloseSession(id string) error {
	if sm == nil {
		return nil
	}

	sm.mu.Lock()
	session, ok := sm.sessions[id]
	if !ok {
		sm.mu.Unlock()
		return nil // Already closed
	}
	delete(sm.sessions, id)
	delete(sm.numericIDIndex, session.NumericID)
	sm.mu.Unlock()

	sm.closeSession(session, "kill-session RPC")
	return nil
}

// CloseSessionByNumericID closes a specific session by numeric ID (RFC 6241)
// Returns error if session not found (for RFC 6241 invalid-value error)
func (sm *SessionManager) CloseSessionByNumericID(numericID uint32) error {
	if sm == nil {
		return fmt.Errorf("session not found: %d", numericID)
	}

	sm.mu.Lock()
	session, ok := sm.numericIDIndex[numericID]
	if !ok {
		sm.mu.Unlock()
		return fmt.Errorf("session not found: %d", numericID)
	}
	delete(sm.sessions, session.ID)
	delete(sm.numericIDIndex, numericID)
	sm.mu.Unlock()

	sm.closeSession(session, "kill-session RPC")
	return nil
}

// Session represents an alias for NETCONFSession (for RPC handlers)
type Session = NETCONFSession

// Session helper methods for lock tracking

// AddLock adds a datastore lock to session tracking
func (s *NETCONFSession) AddLock(target string) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.datastoreLocks == nil {
		s.datastoreLocks = make(map[string]struct{})
	}
	s.datastoreLocks[target] = struct{}{}
}

// RemoveLock removes a datastore lock from session tracking
func (s *NETCONFSession) RemoveLock(target string) {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.datastoreLocks, target)
}

// GetLocks returns the list of locked datastores
func (s *NETCONFSession) GetLocks() []string {
	if s == nil {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	locks := make([]string, 0, len(s.datastoreLocks))
	for target := range s.datastoreLocks {
		locks = append(locks, target)
	}
	return locks
}

// UpdateLastUsed updates the last used timestamp (called on each RPC)
func (s *NETCONFSession) UpdateLastUsed() {
	if s == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastUsed = time.Now()
}

// RemoteAddr returns the remote address (for logging)
func (s *NETCONFSession) RemoteAddr() string {
	if s == nil {
		return "unknown"
	}
	if s.conn != nil {
		return s.conn.RemoteAddr().String()
	}
	return "unknown"
}
