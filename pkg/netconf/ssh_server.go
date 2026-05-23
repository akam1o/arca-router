package netconf

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/akam1o/arca-router/pkg/audit"
	"github.com/akam1o/arca-router/pkg/auth"
	"github.com/akam1o/arca-router/pkg/datastore"
	"github.com/akam1o/arca-router/pkg/logger"
)

// SSHServer manages SSH connections for NETCONF
// Note: This server is not designed to be restarted after Stop() is called.
// Create a new instance if restart is needed.
type SSHServer struct {
	config        *SSHConfig
	listener      net.Listener
	sessionMgr    *SessionManager
	userDB        *UserDatabase
	datastore     datastore.Datastore
	processLock   *datastore.ProcessLock
	netconfServer *Server
	sshConfig     *ssh.ServerConfig
	rateLimiter   *RateLimiter
	activeConns   map[net.Conn]struct{}
	done          chan struct{}
	wg            sync.WaitGroup
	mu            sync.Mutex
	stopOnce      sync.Once
	stopped       bool
	log           *logger.Logger

	// Metrics (thread-safe via atomic operations)
	totalConnections     uint64 // Total TCP connections accepted (use atomic)
	successfulHandshakes uint64 // Successful SSH handshakes (use atomic)
	failedHandshakes     uint64 // Failed SSH handshakes (use atomic)
	activeConnections    int32  // Currently active SSH connections (use atomic)
	isListening          int32  // Whether server is actively accepting (use atomic: 0=no, 1=yes)
}

// NewSSHServer creates a new SSH server instance
func NewSSHServer(config *SSHConfig) (*SSHServer, error) {
	config = sshConfigWithDefaults(config)

	log := logger.New("netconf-ssh", logger.DefaultConfig())

	if err := ensureHostKeyFilePermissions(config.HostKeyPath); err != nil {
		return nil, fmt.Errorf("secure host key permissions: %w", err)
	}

	// Load or generate host key
	hostKey, err := loadOrGenerateHostKey(config.HostKeyPath, log)
	if err != nil {
		return nil, fmt.Errorf("failed to load host key: %w", err)
	}
	if err := ensureHostKeyFilePermissions(config.HostKeyPath); err != nil {
		return nil, fmt.Errorf("secure host key permissions: %w", err)
	}

	// Create user database
	userDB, err := NewUserDatabase(config.UserDBPath, log)
	if err != nil {
		return nil, fmt.Errorf("failed to create user database: %w", err)
	}

	datastoreConfig := netconfDatastoreConfig(config)

	var processLock *datastore.ProcessLock
	if datastoreConfig.Backend == datastore.BackendSQLite && !config.SkipDatastoreStartupCleanup {
		processLock, err = datastore.AcquireSQLiteProcessLock(datastoreConfig.SQLitePath)
		if err != nil {
			_ = userDB.Close()
			return nil, fmt.Errorf("acquire datastore process lock: %w", err)
		}
	}

	// Initialize datastore
	ds, err := datastore.NewDatastore(datastoreConfig)
	if err != nil {
		if processLock != nil {
			_ = processLock.Close()
		}
		_ = userDB.Close()
		return nil, fmt.Errorf("failed to create datastore: %w", err)
	}
	if !config.SkipDatastoreStartupCleanup {
		if err := cleanupDatastoreEphemeralState(context.Background(), ds); err != nil {
			if processLock != nil {
				_ = processLock.Close()
			}
			_ = ds.Close()
			_ = userDB.Close()
			return nil, fmt.Errorf("failed to cleanup datastore ephemeral state: %w", err)
		}
	}

	// Create audit logger with datastore for persistent audit trail
	// Use nil for slog - audit.NewLogger will use slog.Default() internally
	auditLogger := audit.NewLogger(ds, nil)

	// Set audit logger in user database for authentication audit
	userDB.SetAuditLogger(auditLogger)

	// Create session manager with datastore for lock cleanup
	sessionMgr := NewSessionManager(config, ds, log)

	// Create NETCONF server
	netconfServer := NewServer(ds, sessionMgr)

	// Create rate limiter for brute force protection
	rateLimiter := NewRateLimiter(config)

	// Create server instance first to reference in password callback
	srv := &SSHServer{
		config:        config,
		sessionMgr:    sessionMgr,
		userDB:        userDB,
		datastore:     ds,
		processLock:   processLock,
		netconfServer: netconfServer,
		rateLimiter:   rateLimiter,
		sshConfig:     nil, // Will be set below
		activeConns:   make(map[net.Conn]struct{}),
		done:          make(chan struct{}),
		log:           log,
	}

	// Create SSH server config with password and public key authentication
	sshConfig := &ssh.ServerConfig{
		Config: ssh.Config{
			Ciphers:      config.SSHCiphers,
			KeyExchanges: config.SSHKeyExchanges,
			MACs:         config.SSHMACs,
		},
		// Phase 4: Password authentication via user database
		PasswordCallback: srv.passwordCallback,
		// Public key authentication
		PublicKeyCallback: srv.publicKeyCallback,
	}
	sshConfig.AddHostKey(hostKey)
	srv.sshConfig = sshConfig

	return srv, nil
}

func ensureHostKeyFilePermissions(path string) error {
	if err := auth.ValidateKeyFilePermissions(path, 0, 0); err == nil {
		return nil
	} else if errors.Is(err, os.ErrNotExist) {
		return nil
	} else {
		var permErr *auth.KeyPermissionError
		if !errors.As(err, &permErr) {
			return err
		}
	}

	if err := os.Chmod(path, auth.ExpectedKeyFilePerms); err != nil {
		return fmt.Errorf("restrict %s to %04o: %w", path, auth.ExpectedKeyFilePerms, err)
	}
	if err := auth.ValidateKeyFilePermissions(path, 0, 0); err != nil {
		return err
	}
	return nil
}

func netconfDatastoreConfig(config *SSHConfig) *datastore.Config {
	if config.DatastoreConfig != nil {
		cfg := *config.DatastoreConfig
		if cfg.SQLitePath == "" {
			cfg.SQLitePath = config.DatastorePath
		}
		return &cfg
	}
	return &datastore.Config{
		Backend:    datastore.BackendSQLite,
		SQLitePath: config.DatastorePath,
	}
}

type ephemeralStateCleaner interface {
	CleanupEphemeralState(ctx context.Context) error
}

func cleanupDatastoreEphemeralState(ctx context.Context, ds datastore.Datastore) error {
	cleaner, ok := ds.(ephemeralStateCleaner)
	if !ok {
		return nil
	}
	return cleaner.CleanupEphemeralState(ctx)
}

// SetCommitHook installs a commit coordinator for NETCONF commits.
func (s *SSHServer) SetCommitHook(h CommitHook) {
	if s != nil && s.netconfServer != nil {
		s.netconfServer.SetCommitHook(h)
	}
}

// SetOperationalStateProvider installs a live-state source for <get> replies.
func (s *SSHServer) SetOperationalStateProvider(provider OperationalStateProvider) {
	if s != nil && s.netconfServer != nil {
		s.netconfServer.SetOperationalStateProvider(provider)
	}
}

// Start starts the SSH server
func (s *SSHServer) Start(ctx context.Context) error {
	if s == nil || s.config == nil || s.sessionMgr == nil || s.activeConns == nil || s.done == nil || s.log == nil {
		return fmt.Errorf("server not initialized")
	}

	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return fmt.Errorf("server stopped")
	}
	if s.listener != nil {
		s.mu.Unlock()
		return fmt.Errorf("server already started")
	}

	listener, err := net.Listen("tcp", s.config.ListenAddr)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("failed to listen on %s: %w", s.config.ListenAddr, err)
	}
	s.listener = listener
	atomic.StoreInt32(&s.isListening, 1)

	// Start goroutines while holding the lifecycle lock so Stop cannot wait
	// before all startup workers have been registered.
	s.wg.Add(1)
	go s.sessionMgr.StartCleanup(ctx, &s.wg)
	s.wg.Add(1)
	go s.acceptConnections(ctx)
	s.mu.Unlock()

	s.log.Info("SSH server started", "addr", s.config.ListenAddr)

	return nil
}

// Stop stops the SSH server gracefully
func (s *SSHServer) Stop() error {
	if s == nil {
		return nil
	}

	s.stopOnce.Do(func() {
		// Mark as not listening
		atomic.StoreInt32(&s.isListening, 0)

		s.mu.Lock()
		s.stopped = true
		listener := s.listener
		s.listener = nil
		activeConns := make([]net.Conn, 0, len(s.activeConns))
		for conn := range s.activeConns {
			activeConns = append(activeConns, conn)
		}
		s.mu.Unlock()

		// Signal shutdown even if Start failed before creating a listener.
		if s.done != nil {
			close(s.done)
		}

		if listener != nil {
			if err := listener.Close(); err != nil {
				if s.log != nil {
					s.log.Error("Failed to close listener", "error", err)
				}
			}
		}
		for _, conn := range activeConns {
			if err := conn.Close(); err != nil {
				_ = err
			}
		}

		// Close all sessions (this will trigger cleanup goroutine to stop)
		if s.sessionMgr != nil {
			s.sessionMgr.CloseAll()
		}

		// Stop rate limiter
		s.rateLimiter.Stop()

		// Wait for goroutines to finish
		s.wg.Wait()

		// Close datastore
		if s.datastore != nil {
			if err := s.datastore.Close(); err != nil && s.log != nil {
				s.log.Error("Failed to close datastore", "error", err)
			}
		}
		if s.processLock != nil {
			if err := s.processLock.Close(); err != nil && s.log != nil {
				s.log.Error("Failed to release datastore process lock", "error", err)
			}
		}

		// Close user database
		if s.userDB != nil {
			if err := s.userDB.Close(); err != nil && s.log != nil {
				s.log.Error("Failed to close user database", "error", err)
			}
		}

		if s.log != nil {
			s.log.Info("SSH server stopped")
		}
	})
	return nil
}

func (s *SSHServer) startConnectionHandler(ctx context.Context, conn net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return false
	}
	s.activeConns[conn] = struct{}{}
	s.wg.Add(1)
	go s.handleConnection(ctx, conn)
	return true
}

func (s *SSHServer) unregisterConnection(conn net.Conn) {
	s.mu.Lock()
	delete(s.activeConns, conn)
	s.mu.Unlock()
}

func (s *SSHServer) startWorker(fn func()) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return false
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		fn()
	}()
	return true
}

func (s *SSHServer) startNETCONFHandler(ctx context.Context, username, role string, sshConn *ssh.ServerConn, channel ssh.Channel) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return false
	}
	session := s.sessionMgr.Create(username, role, sshConn, channel)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.handleNETCONF(ctx, session, channel)
	}()
	return true
}

// acceptConnections accepts incoming SSH connections
func (s *SSHServer) acceptConnections(ctx context.Context) {
	defer s.wg.Done()

	// Capture listener locally to avoid nil reference during shutdown
	s.mu.Lock()
	listener := s.listener
	s.mu.Unlock()

	if listener == nil {
		return
	}

	for {
		select {
		case <-s.done:
			return
		case <-ctx.Done():
			return
		default:
		}

		// Set accept deadline to allow checking done channel
		if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(1 * time.Second)); err != nil {
			s.log.Warn("Failed to set accept deadline", "error", err)
		}

		conn, err := listener.Accept()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			select {
			case <-s.done:
				return
			default:
				s.log.Error("Failed to accept connection", "error", err)
				continue
			}
		}

		if !s.startConnectionHandler(ctx, conn) {
			if err := conn.Close(); err != nil {
				_ = err
			}
			return
		}
	}
}

// handleConnection handles a single SSH connection
func (s *SSHServer) handleConnection(ctx context.Context, conn net.Conn) {
	defer s.wg.Done()
	defer func() {
		s.unregisterConnection(conn)
		if err := conn.Close(); err != nil {
			_ = err
		}
	}()

	// Update metrics
	atomic.AddUint64(&s.totalConnections, 1)
	atomic.AddInt32(&s.activeConnections, 1)
	defer atomic.AddInt32(&s.activeConnections, -1)

	// Check max sessions
	if s.sessionMgr.Count() >= s.config.MaxSessions {
		s.log.Warn("Max sessions reached, rejecting connection", "remote", conn.RemoteAddr())
		return
	}

	// Perform SSH handshake
	sshConn, chans, reqs, err := ssh.NewServerConn(conn, s.sshConfig)
	if err != nil {
		atomic.AddUint64(&s.failedHandshakes, 1)
		s.log.Error("SSH handshake failed", "remote", conn.RemoteAddr(), "error", err)
		return
	}
	defer func() {
		if err := sshConn.Close(); err != nil {
			_ = err
		}
	}()

	atomic.AddUint64(&s.successfulHandshakes, 1)
	s.log.Info("SSH connection established", "remote", conn.RemoteAddr(), "user", sshConn.User())

	// Handle SSH connection
	go ssh.DiscardRequests(reqs)

	// Handle channels
	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			if err := newChannel.Reject(ssh.UnknownChannelType, "unknown channel type"); err != nil {
				s.log.Warn("Failed to reject unknown channel", "error", err)
			}
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			s.log.Error("Failed to accept channel", "error", err)
			continue
		}

		if !s.startWorker(func() {
			s.handleSession(ctx, sshConn, channel, requests)
		}) {
			if err := channel.Close(); err != nil {
				_ = err
			}
			return
		}
	}
}

// handleSession handles a single SSH session
func (s *SSHServer) handleSession(ctx context.Context, sshConn *ssh.ServerConn, channel ssh.Channel, requests <-chan *ssh.Request) {
	defer func() {
		if err := channel.Close(); err != nil {
			_ = err
		}
	}()

	// Wait for subsystem request
	for req := range requests {
		switch req.Type {
		case "subsystem":
			if len(req.Payload) < 4 {
				if err := req.Reply(false, nil); err != nil {
					s.log.Warn("Failed to reply to request", "error", err)
				}
				continue
			}
			// Parse subsystem name (SSH string format: uint32 BE length + data)
			subsystemLen := binary.BigEndian.Uint32(req.Payload[0:4])
			if len(req.Payload) < int(4+subsystemLen) {
				if err := req.Reply(false, nil); err != nil {
					s.log.Warn("Failed to reply to request", "error", err)
				}
				continue
			}
			subsystem := string(req.Payload[4 : 4+subsystemLen])

			if subsystem == "netconf" {
				if err := req.Reply(true, nil); err != nil {
					s.log.Warn("Failed to reply to request", "error", err)
				}
				s.log.Info("NETCONF subsystem requested", "user", sshConn.User())

				// Create NETCONF session
				// Extract role from authenticated user's permissions
				// Default fallback is read-only for security (least privilege)
				role := RoleReadOnly
				if sshConn.Permissions != nil && sshConn.Permissions.Extensions != nil {
					if authRole, ok := sshConn.Permissions.Extensions["role"]; ok {
						role = authRole
					}
				}
				// Start NETCONF protocol handling
				if !s.startNETCONFHandler(ctx, sshConn.User(), role, sshConn, channel) {
					return
				}
			} else {
				if err := req.Reply(false, nil); err != nil {
					s.log.Warn("Failed to reply to request", "error", err)
				}
				s.log.Warn("Unsupported subsystem", "subsystem", subsystem)
			}
		default:
			if err := req.Reply(false, nil); err != nil {
				s.log.Warn("Failed to reply to request", "error", err)
			}
		}
	}
}

// passwordCallback handles SSH password authentication
func (s *SSHServer) passwordCallback(meta ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
	username := meta.User()
	sourceIP := extractIP(meta.RemoteAddr())

	// Check rate limiting - IP lockout
	if allowed, unlockAt := s.rateLimiter.CheckIP(sourceIP); !allowed {
		s.log.Warn("Authentication blocked - IP locked out", "ip", sourceIP, "unlock_at", unlockAt)
		return nil, FormatLockoutError(unlockAt)
	}

	// Check rate limiting - User lockout
	if allowed, unlockAt := s.rateLimiter.CheckUser(username); !allowed {
		s.log.Warn("Authentication blocked - user locked out", "username", username, "unlock_at", unlockAt)
		return nil, FormatLockoutError(unlockAt)
	}

	// Verify password using user database
	user, reason, err := s.userDB.VerifyPasswordWithReason(username, string(password))
	if err != nil {
		// Record failure in rate limiter
		ipLocked, userLocked := s.rateLimiter.RecordFailure(sourceIP, username)
		if ipLocked {
			s.log.Warn("IP locked out due to repeated failures", "ip", sourceIP, "failures", s.config.IPFailureLimit)
		}
		if userLocked {
			s.log.Warn("User locked out due to repeated failures", "username", username, "failures", s.config.UserFailureLimit)
		}

		// Log authentication failure with detailed reason
		s.userDB.LogAuthFailure(username, sourceIP, reason)
		return nil, fmt.Errorf("authentication failed")
	}

	// Record success (clears failure history)
	s.rateLimiter.RecordSuccess(sourceIP, username)

	// Log authentication success
	s.userDB.LogAuthSuccess(username, sourceIP)

	// Return permissions with user context for session creation
	perms := &ssh.Permissions{
		Extensions: map[string]string{
			"username": username,
			"role":     user.Role,
		},
	}
	return perms, nil
}

// publicKeyCallback handles SSH public key authentication
func (s *SSHServer) publicKeyCallback(meta ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	username := meta.User()
	sourceIP := extractIP(meta.RemoteAddr())

	// Check rate limiting - IP lockout
	if allowed, unlockAt := s.rateLimiter.CheckIP(sourceIP); !allowed {
		s.log.Warn("Authentication blocked - IP locked out", "ip", sourceIP, "unlock_at", unlockAt)
		return nil, FormatLockoutError(unlockAt)
	}

	// Check rate limiting - User lockout
	if allowed, unlockAt := s.rateLimiter.CheckUser(username); !allowed {
		s.log.Warn("Authentication blocked - user locked out", "username", username, "unlock_at", unlockAt)
		return nil, FormatLockoutError(unlockAt)
	}

	// Get base64-encoded key data from the provided key
	keyData := base64.StdEncoding.EncodeToString(key.Marshal())

	// Verify public key using user database
	user, reason, err := s.userDB.VerifyPublicKeyAuth(username, keyData)
	if err != nil {
		// Record failure in rate limiter
		ipLocked, userLocked := s.rateLimiter.RecordFailure(sourceIP, username)
		if ipLocked {
			s.log.Warn("IP locked out due to repeated failures", "ip", sourceIP, "failures", s.config.IPFailureLimit)
		}
		if userLocked {
			s.log.Warn("User locked out due to repeated failures", "username", username, "failures", s.config.UserFailureLimit)
		}

		// Log authentication failure with public-key method
		s.userDB.LogAuthFailureWithMethod(username, sourceIP, "publickey", reason)
		return nil, fmt.Errorf("authentication failed")
	}

	// Record success (clears failure history)
	s.rateLimiter.RecordSuccess(sourceIP, username)

	// Log authentication success with public-key method
	s.userDB.LogAuthSuccessWithMethod(username, sourceIP, "publickey")

	// Return permissions with user context for session creation
	perms := &ssh.Permissions{
		Extensions: map[string]string{
			"username": username,
			"role":     user.Role,
		},
	}
	return perms, nil
}

// extractIP extracts the IP address from a net.Addr (format: "host:port")
func extractIP(addr net.Addr) string {
	if tcpAddr, ok := addr.(*net.TCPAddr); ok {
		return tcpAddr.IP.String()
	}
	// Fallback: parse string representation
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}

// loadOrGenerateHostKey loads or generates an ED25519 host key
func loadOrGenerateHostKey(path string, log *logger.Logger) (ssh.Signer, error) {
	// Try to load existing key
	data, err := os.ReadFile(path)
	if err == nil {
		// Parse existing key
		signer, err := ssh.ParsePrivateKey(data)
		if err != nil {
			return nil, fmt.Errorf("failed to parse host key: %w", err)
		}
		log.Info("Loaded existing host key", "path", path)
		return signer, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("failed to read host key: %w", err)
	}

	// Generate new key
	log.Info("Generating new ED25519 host key", "path", path)

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate ED25519 key: %w", err)
	}

	// Convert to SSH format
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create signer: %w", err)
	}

	// Marshal private key to OpenSSH format
	pemBytes, err := ssh.MarshalPrivateKey(privateKey, "")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal private key: %w", err)
	}

	// Create directory if needed
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Write key file with restricted permissions.
	if err := writeHostKeyFile(path, pem.EncodeToMemory(pemBytes)); err != nil {
		return nil, fmt.Errorf("failed to write host key: %w", err)
	}

	log.Info("Generated and saved new host key", "path", path)
	return signer, nil
}

func writeHostKeyFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, auth.ExpectedKeyFilePerms)
	if err != nil {
		return err
	}
	removeOnError := true
	defer func() {
		if removeOnError {
			_ = os.Remove(path)
		}
	}()

	n, err := file.Write(data)
	if err != nil {
		_ = file.Close()
		return err
	}
	if n != len(data) {
		_ = file.Close()
		return io.ErrShortWrite
	}
	if err := file.Close(); err != nil {
		return err
	}
	removeOnError = false
	return nil
}

// ServerMetrics contains server health and performance metrics
type ServerMetrics struct {
	TotalConnections     uint64 // Total TCP connections accepted since server start
	SuccessfulHandshakes uint64 // Successful SSH protocol handshakes (not authentication - NoClientAuth mode)
	FailedHandshakes     uint64 // Failed SSH handshakes (protocol errors, not authentication)
	ActiveConnections    int32  // Currently active SSH connections
	ActiveSessions       int    // Currently active NETCONF sessions
	ListenAddr           string // Server listen address
	IsListening          bool   // Whether server is currently accepting connections (Start/Stop state)
}

// GetMetrics returns current server metrics
// All metrics are thread-safe and can be called concurrently
func (s *SSHServer) GetMetrics() ServerMetrics {
	if s == nil {
		return ServerMetrics{}
	}

	metrics := ServerMetrics{
		TotalConnections:     atomic.LoadUint64(&s.totalConnections),
		SuccessfulHandshakes: atomic.LoadUint64(&s.successfulHandshakes),
		FailedHandshakes:     atomic.LoadUint64(&s.failedHandshakes),
		ActiveConnections:    atomic.LoadInt32(&s.activeConnections),
		IsListening:          atomic.LoadInt32(&s.isListening) == 1,
	}
	if s.sessionMgr != nil {
		metrics.ActiveSessions = s.sessionMgr.Count()
	}
	if s.config != nil {
		metrics.ListenAddr = s.config.ListenAddr
	}
	return metrics
}

// HealthCheck verifies the server is healthy and operational
// This method checks:
// 1. Server is actively accepting connections (not stopped or failed)
// 2. User database is accessible and healthy
// 3. Session count is within configured limits
func (s *SSHServer) HealthCheck() error {
	if s == nil {
		return fmt.Errorf("server unavailable")
	}

	// Check if server is actively accepting connections
	// Uses atomic flag set by Start/Stop to avoid race conditions
	if atomic.LoadInt32(&s.isListening) != 1 {
		return fmt.Errorf("server is not accepting connections")
	}

	// Verify listener is still valid
	s.mu.Lock()
	if s.listener == nil {
		s.mu.Unlock()
		return fmt.Errorf("server listener is nil (stopped or failed)")
	}
	s.mu.Unlock()

	// Check user database health
	if s.userDB == nil {
		return fmt.Errorf("user database unavailable")
	}
	if err := s.userDB.HealthCheck(); err != nil {
		return fmt.Errorf("user database unhealthy: %w", err)
	}

	// Check session manager is operational
	if s.config == nil {
		return fmt.Errorf("server config unavailable")
	}
	metrics := s.GetMetrics()
	if metrics.ActiveSessions > s.config.MaxSessions {
		return fmt.Errorf("session count (%d) exceeds max sessions (%d)",
			metrics.ActiveSessions, s.config.MaxSessions)
	}

	return nil
}

// handleNETCONF handles NETCONF protocol over SSH channel
func (s *SSHServer) handleNETCONF(ctx context.Context, sess *Session, channel ssh.Channel) {
	defer func() {
		// Clean up session and release any locks held by this session
		if err := s.sessionMgr.CloseSession(sess.ID); err != nil {
			s.log.Error("Failed to close session", "error", err)
		}
		s.log.Info("NETCONF session closed", "session", sess.ID, "user", sess.Username)
	}()

	// Phase 1: Send server hello
	serverHello := ServerHelloWithOptions(sess.NumericID, HelloOptions{
		AdvertiseStandardXPath: s.config.AdvertiseStandardXPath,
		DisableStandardXPath:   !s.config.AdvertiseStandardXPath,
	})
	serverHelloXML, err := MarshalHello(serverHello)
	if err != nil {
		s.log.Error("Failed to generate server hello", "error", err)
		return
	}

	// Hello exchange MUST use base:1.0 EOM framing (RFC 6241 Section 4.1)
	// Base version is negotiated after Hello exchange completes
	// Create reader/writer ONCE to preserve buffered data for pipelined RPCs
	reader := NewFramingReader(channel, "1.0")
	writer := NewFramingWriter(channel, "1.0")

	// Send server hello
	if err := writer.WriteMessage(serverHelloXML); err != nil {
		s.log.Error("Failed to send server hello", "error", err)
		return
	}
	s.log.Debug("Server hello sent", "session", sess.ID)

	// Phase 2: Receive and validate client hello (still using base:1.0)
	clientHelloXML, err := reader.ReadMessage()
	if err != nil {
		s.log.Error("Failed to read client hello", "error", err)
		return
	}

	clientHello, err := UnmarshalHello(clientHelloXML)
	if err != nil {
		s.log.Error("Failed to parse client hello", "error", err)
		return
	}

	// Validate client hello
	if err := ValidateClientHello(clientHello); err != nil {
		s.log.Error("Invalid client hello", "error", err)
		return
	}

	// Negotiate base version
	negotiatedVersion := NegotiateBaseVersion(clientHello)
	s.log.Info("Client hello received", "session", sess.ID, "base_version", negotiatedVersion)

	// Update session with negotiated base version
	sess.mu.Lock()
	sess.BaseVersion = negotiatedVersion
	sess.mu.Unlock()

	// Switch to negotiated framing for RPC messages (after Hello exchange)
	// IMPORTANT: Use SetBaseVersion() to preserve buffered data for pipelined RPCs
	reader.SetBaseVersion(negotiatedVersion)
	writer.SetBaseVersion(negotiatedVersion)

	// Phase 3: RPC loop
	s.log.Debug("Starting RPC loop", "session", sess.ID, "base_version", negotiatedVersion)

	for {
		// Check context cancellation
		select {
		case <-ctx.Done():
			s.log.Info("Context cancelled, closing NETCONF session", "session", sess.ID)
			return
		default:
		}

		// Read RPC message
		rpcXML, err := reader.ReadMessage()
		if err != nil {
			// EOF or connection closed
			s.log.Debug("RPC read failed, closing session", "session", sess.ID, "error", err)
			return
		}

		// Parse RPC
		rpc, err := ParseRPC(rpcXML)
		if err != nil {
			s.log.Error("Failed to parse RPC", "error", err)
			// Send error reply
			rpcErr, ok := err.(*RPCError)
			if !ok {
				rpcErr = ErrOperationFailed(fmt.Sprintf("RPC parsing failed: %v", err))
			}
			messageID, replyAttrs := extractRPCReplyContext(rpcXML)
			errorXML, err := marshalErrorReply(messageID, rpcErr, replyAttrs)
			if err != nil {
				s.log.Error("Failed to serialize error reply", "error", err)
				return
			}
			if err := writer.WriteMessage(errorXML); err != nil {
				s.log.Error("Failed to send error reply", "error", err)
				return
			}
			continue
		}

		s.log.Debug("RPC received", "session", sess.ID, "operation", rpc.GetOperationName(), "message_id", rpc.MessageID)

		// Handle close-session specially (need to send reply before closing)
		if rpc.GetOperationName() == "close-session" {
			reply := s.netconfServer.HandleRPC(ctx, sess, rpc)
			replyXML, err := MarshalReply(reply)
			if err != nil {
				s.log.Error("Failed to serialize reply", "error", err)
			} else {
				if err := writer.WriteMessage(replyXML); err != nil {
					s.log.Error("Failed to send reply", "error", err)
					return
				}
			}
			s.log.Info("Close-session requested, terminating", "session", sess.ID)
			return
		}

		// Dispatch RPC to server
		reply := s.netconfServer.HandleRPC(ctx, sess, rpc)

		// Serialize and send reply
		replyXML, err := MarshalReply(reply)
		if err != nil {
			s.log.Error("Failed to serialize reply", "error", err)
			// Send generic error
			errorXML, err := marshalErrorReply(rpc.MessageID, ErrOperationFailed("reply serialization failed"), rpc.ReplyAttrs)
			if err != nil {
				s.log.Error("Failed to serialize error reply", "error", err)
				return
			}
			if err := writer.WriteMessage(errorXML); err != nil {
				s.log.Error("Failed to send error reply", "error", err)
				return
			}
			continue
		}

		if err := writer.WriteMessage(replyXML); err != nil {
			s.log.Error("Failed to send reply", "error", err)
			return
		}

		s.log.Debug("RPC reply sent", "session", sess.ID, "message_id", rpc.MessageID)
	}
}

func marshalErrorReply(messageID string, rpcErr *RPCError, attrs []xml.Attr) ([]byte, error) {
	errorReply := NewErrorReply(messageID, rpcErr).WithAttributes(attrs)
	errorXML, err := MarshalReply(errorReply)
	if err == nil {
		return errorXML, nil
	}

	fallbackXML, fallbackErr := MarshalReply(NewErrorReply(messageID, rpcErr))
	if fallbackErr != nil {
		return nil, fmt.Errorf("marshal error reply: %w; marshal fallback error reply: %v", err, fallbackErr)
	}
	return fallbackXML, nil
}
