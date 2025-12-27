package netconf

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"

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
	netconfServer *Server
	sshConfig     *ssh.ServerConfig
	rateLimiter   *RateLimiter
	done          chan struct{}
	wg            sync.WaitGroup
	mu            sync.Mutex
	log           *logger.Logger

	// Metrics (thread-safe via atomic operations)
	totalConnections      uint64 // Total TCP connections accepted (use atomic)
	successfulHandshakes  uint64 // Successful SSH handshakes (use atomic)
	failedHandshakes      uint64 // Failed SSH handshakes (use atomic)
	activeConnections     int32  // Currently active SSH connections (use atomic)
	isListening           int32  // Whether server is actively accepting (use atomic: 0=no, 1=yes)
}

// NewSSHServer creates a new SSH server instance
func NewSSHServer(config *SSHConfig) (*SSHServer, error) {
	if config == nil {
		config = DefaultSSHConfig()
	}

	log := logger.New("netconf-ssh", logger.DefaultConfig())

	// Load or generate host key
	hostKey, err := loadOrGenerateHostKey(config.HostKeyPath, log)
	if err != nil {
		return nil, fmt.Errorf("failed to load host key: %w", err)
	}

	// Validate host key permissions for security
	// This ensures the key file has 0600 permissions (owner read/write only)
	if err := auth.ValidateKeyFilePermissions(config.HostKeyPath, 0, 0); err != nil {
		log.Warn("Host key has insecure permissions - startup allowed but should be fixed",
			"path", config.HostKeyPath,
			"error", err)
		// Note: We warn but don't fail startup to avoid breaking existing deployments
		// In production, consider making this a hard failure
	}

	// Create user database
	userDB, err := NewUserDatabase(config.UserDBPath, log)
	if err != nil {
		return nil, fmt.Errorf("failed to create user database: %w", err)
	}

	// Initialize datastore
	datastoreConfig := &datastore.Config{
		Backend:    datastore.BackendSQLite,
		SQLitePath: config.DatastorePath,
	}
	ds, err := datastore.NewSQLiteDatastore(datastoreConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create datastore: %w", err)
	}

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
		netconfServer: netconfServer,
		rateLimiter:   rateLimiter,
		sshConfig:     nil, // Will be set below
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

// Start starts the SSH server
func (s *SSHServer) Start(ctx context.Context) error {
	s.mu.Lock()
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
	s.mu.Unlock()

	s.log.Info("SSH server started", "addr", s.config.ListenAddr)

	// Mark as listening
	atomic.StoreInt32(&s.isListening, 1)

	// Start session cleanup goroutine
	s.wg.Add(1)
	go s.sessionMgr.StartCleanup(ctx, &s.wg)

	// Accept connections
	s.wg.Add(1)
	go s.acceptConnections(ctx)

	return nil
}

// Stop stops the SSH server gracefully
func (s *SSHServer) Stop() error {
	// Mark as not listening
	atomic.StoreInt32(&s.isListening, 0)

	s.mu.Lock()
	if s.listener == nil {
		s.mu.Unlock()
		return nil
	}

	// Close listener
	if err := s.listener.Close(); err != nil {
		s.log.Error("Failed to close listener", "error", err)
	}
	s.listener = nil
	s.mu.Unlock()

	// Signal shutdown
	close(s.done)

	// Close all sessions (this will trigger cleanup goroutine to stop)
	s.sessionMgr.CloseAll()

	// Stop rate limiter
	s.rateLimiter.Stop()

	// Wait for goroutines to finish
	s.wg.Wait()

	// Close datastore
	if err := s.datastore.Close(); err != nil {
		s.log.Error("Failed to close datastore", "error", err)
	}

	// Close user database
	if err := s.userDB.Close(); err != nil {
		s.log.Error("Failed to close user database", "error", err)
	}

	s.log.Info("SSH server stopped")
	return nil
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
		listener.(*net.TCPListener).SetDeadline(time.Now().Add(1 * time.Second))

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

		// Handle connection in goroutine
		s.wg.Add(1)
		go s.handleConnection(ctx, conn)
	}
}

// handleConnection handles a single SSH connection
func (s *SSHServer) handleConnection(ctx context.Context, conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

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
	defer sshConn.Close()

	atomic.AddUint64(&s.successfulHandshakes, 1)
	s.log.Info("SSH connection established", "remote", conn.RemoteAddr(), "user", sshConn.User())

	// Handle SSH connection
	go ssh.DiscardRequests(reqs)

	// Handle channels
	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			s.log.Error("Failed to accept channel", "error", err)
			continue
		}

		// Handle session (NETCONF subsystem will be handled in Phase 2)
		s.wg.Add(1)
		go s.handleSession(ctx, sshConn, channel, requests)
	}
}

// handleSession handles a single SSH session
func (s *SSHServer) handleSession(ctx context.Context, sshConn *ssh.ServerConn, channel ssh.Channel, requests <-chan *ssh.Request) {
	defer s.wg.Done()
	defer channel.Close()

	// Wait for subsystem request
	for req := range requests {
		switch req.Type {
		case "subsystem":
			if len(req.Payload) < 4 {
				req.Reply(false, nil)
				continue
			}
			// Parse subsystem name (SSH string format: uint32 BE length + data)
			subsystemLen := binary.BigEndian.Uint32(req.Payload[0:4])
			if len(req.Payload) < int(4+subsystemLen) {
				req.Reply(false, nil)
				continue
			}
			subsystem := string(req.Payload[4 : 4+subsystemLen])

			if subsystem == "netconf" {
				req.Reply(true, nil)
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
				session := s.sessionMgr.Create(sshConn.User(), role, sshConn, channel)

				// Start NETCONF protocol handling
				s.wg.Add(1)
				go s.handleNETCONF(ctx, session, channel)
			} else {
				req.Reply(false, nil)
				s.log.Warn("Unsupported subsystem", "subsystem", subsystem)
			}
		default:
			req.Reply(false, nil)
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

	// Write key file with restricted permissions
	if err := os.WriteFile(path, pem.EncodeToMemory(pemBytes), 0600); err != nil {
		return nil, fmt.Errorf("failed to write host key: %w", err)
	}

	log.Info("Generated and saved new host key", "path", path)
	return signer, nil
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
	return ServerMetrics{
		TotalConnections:     atomic.LoadUint64(&s.totalConnections),
		SuccessfulHandshakes: atomic.LoadUint64(&s.successfulHandshakes),
		FailedHandshakes:     atomic.LoadUint64(&s.failedHandshakes),
		ActiveConnections:    atomic.LoadInt32(&s.activeConnections),
		ActiveSessions:       s.sessionMgr.Count(),
		ListenAddr:           s.config.ListenAddr,
		IsListening:          atomic.LoadInt32(&s.isListening) == 1,
	}
}

// HealthCheck verifies the server is healthy and operational
// This method checks:
// 1. Server is actively accepting connections (not stopped or failed)
// 2. User database is accessible and healthy
// 3. Session count is within configured limits
func (s *SSHServer) HealthCheck() error {
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
	if err := s.userDB.HealthCheck(); err != nil {
		return fmt.Errorf("user database unhealthy: %w", err)
	}

	// Check session manager is operational
	metrics := s.GetMetrics()
	if metrics.ActiveSessions > s.config.MaxSessions {
		return fmt.Errorf("session count (%d) exceeds max sessions (%d)",
			metrics.ActiveSessions, s.config.MaxSessions)
	}

	return nil
}

// handleNETCONF handles NETCONF protocol over SSH channel
func (s *SSHServer) handleNETCONF(ctx context.Context, sess *Session, channel ssh.Channel) {
	defer s.wg.Done()
	defer func() {
		// Clean up session and release any locks held by this session
		if err := s.sessionMgr.CloseSession(sess.ID); err != nil {
			s.log.Error("Failed to close session", "error", err)
		}
		s.log.Info("NETCONF session closed", "session", sess.ID, "user", sess.Username)
	}()

	// Phase 1: Send server hello
	serverHello := ServerHello(sess.NumericID)
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
			errorReply := NewErrorReply("", rpcErr)
			errorXML, _ := MarshalReply(errorReply)
			writer.WriteMessage(errorXML)
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
				writer.WriteMessage(replyXML)
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
			errorReply := NewErrorReply(rpc.MessageID, ErrOperationFailed("reply serialization failed"))
			errorXML, _ := MarshalReply(errorReply)
			writer.WriteMessage(errorXML)
			continue
		}

		if err := writer.WriteMessage(replyXML); err != nil {
			s.log.Error("Failed to send reply", "error", err)
			return
		}

		s.log.Debug("RPC reply sent", "session", sess.ID, "message_id", rpc.MessageID)
	}
}
