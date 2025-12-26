package netconf

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"

	"github.com/akam1o/arca-router/pkg/logger"
)

// NETCONFSession represents a NETCONF session
type NETCONFSession struct {
	ID              string               // UUID v4
	Username        string
	Role            string               // admin, operator, read-only
	CreatedAt       time.Time
	LastUsed        time.Time
	IdleTimeout     time.Duration        // Idle timeout (e.g., 30m)
	AbsoluteTimeout time.Duration        // Absolute max lifetime (e.g., 24h)
	BaseVersion     string               // "1.0" or "1.1" (NETCONF protocol version)
	conn            ssh.Conn
	channel         ssh.Channel
	ctx             context.Context
	cancel          context.CancelFunc
	datastoreLocks  map[string]struct{}  // Set of locked datastores ("candidate", "running")
	mu              sync.RWMutex         // Protects datastoreLocks and LastUsed
}

// SessionManager manages NETCONF sessions
type SessionManager struct {
	sessions       map[string]*NETCONFSession
	config         *SSHConfig
	mu             sync.RWMutex
	cleanupMu      sync.Mutex
	cleanup        *time.Ticker
	cleanupDone    chan struct{}
	cleanupStopped sync.Once
	log            *logger.Logger
}

// NewSessionManager creates a new session manager
func NewSessionManager(config *SSHConfig, log *logger.Logger) *SessionManager {
	return &SessionManager{
		sessions:    make(map[string]*NETCONFSession),
		config:      config,
		cleanupDone: make(chan struct{}),
		log:         log,
	}
}

// Create creates a new NETCONF session
func (sm *SessionManager) Create(username, role string, conn ssh.Conn, channel ssh.Channel) *NETCONFSession {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())

	session := &NETCONFSession{
		ID:              uuid.New().String(),
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
	sm.log.Info("Session created", "id", session.ID, "user", username, "role", role)

	return session
}

// Get retrieves a session by ID
func (sm *SessionManager) Get(id string) (*NETCONFSession, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	session, ok := sm.sessions[id]
	return session, ok
}

// Count returns the number of active sessions
func (sm *SessionManager) Count() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.sessions)
}

// CloseAll closes all active sessions and stops cleanup
func (sm *SessionManager) CloseAll() {
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
	sm.mu.Unlock()

	for _, session := range sessions {
		sm.closeSession(session, "server shutdown")
	}
}

// StartCleanup starts the session cleanup goroutine
func (sm *SessionManager) StartCleanup(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	sm.cleanupMu.Lock()
	sm.cleanup = ticker
	sm.cleanupMu.Unlock()
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-sm.cleanupDone:
			return
		case <-ticker.C:
			sm.cleanupExpiredSessions(ctx)
		}
	}
}

// cleanupExpiredSessions removes expired sessions
func (sm *SessionManager) cleanupExpiredSessions(ctx context.Context) {
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
			sm.log.Info("Session expired (absolute timeout)", "id", id, "user", session.Username)
			continue
		}

		// Check idle timeout
		if now.Sub(lastUsed) > session.IdleTimeout {
			toClose = append(toClose, session)
			delete(sm.sessions, id)
			sm.log.Info("Session expired (idle timeout)", "id", id, "user", session.Username)
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
	session.cancel()

	// TODO: Release datastore locks (will be implemented in Phase 2)

	// Close SSH connection to force chans/reqs to terminate
	// This ensures handleConnection exits cleanly during shutdown
	if session.conn != nil {
		_ = session.conn.Close()
	}

	if session.channel != nil {
		_ = session.channel.Close()
	}

	sm.log.Info("Session closed", "id", session.ID, "user", session.Username, "reason", reason)
}

// UpdateLastUsed updates the last used timestamp for a session
func (sm *SessionManager) UpdateLastUsed(id string) {
	sm.mu.RLock()
	session, ok := sm.sessions[id]
	sm.mu.RUnlock()

	if ok {
		session.mu.Lock()
		session.LastUsed = time.Now()
		session.mu.Unlock()
	}
}
