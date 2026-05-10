// Package grpc implements the internal gRPC API server for arca-routerd.
// This is the unified entry point for both arca-cli and the NETCONF subsystem.
package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/akam1o/arca-router/internal/engine"
	"github.com/akam1o/arca-router/internal/model"
	"github.com/akam1o/arca-router/internal/store"
	"github.com/akam1o/arca-router/pkg/cli"
	pkgconfig "github.com/akam1o/arca-router/pkg/config"
	"github.com/google/uuid"
	googlegrpc "google.golang.org/grpc"
)

// Server is the internal gRPC server that exposes configuration,
// session, and operational state services over a Unix socket.
type Server struct {
	engine   *engine.Engine
	store    store.ConfigStore
	sessions *SessionManager
	log      *slog.Logger
	server   *googlegrpc.Server
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
	s.server = googlegrpc.NewServer(googlegrpc.ForceServerCodec(jsonCodec{}))
	registerConfigServiceServer(s.server, s)
	registerSessionServiceServer(s.server, s)
	registerStateServiceServer(s.server, s)
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
	text, version := s.runningText()
	return text, version, nil
}

// GetCandidate returns the session candidate configuration.
func (s *Server) GetCandidate(ctx context.Context, sessionID string) (string, error) {
	session, err := s.sessions.Get(sessionID)
	if err != nil {
		return "", err
	}
	session.mu.RLock()
	defer session.mu.RUnlock()
	return session.CandidateText, nil
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

	session.mu.Lock()
	defer session.mu.Unlock()
	updated, err := applyCandidateCommand(session.CandidateText, configText)
	if err != nil {
		return err
	}
	session.CandidateText = updated
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

	snap := s.engine.RunningSnapshot()
	commitID := ""
	if s.store != nil {
		commitID, err = s.store.SaveCommit(ctx, snap)
		if err != nil {
			return "", 0, fmt.Errorf("persist commit after apply: %w", err)
		}
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
	session.CandidateText, _ = s.runningText()
	session.mu.Unlock()
	return nil
}

// Diff returns a simple line-oriented diff between running and candidate config.
func (s *Server) Diff(ctx context.Context, sessionID string) (string, bool, error) {
	session, err := s.sessions.Get(sessionID)
	if err != nil {
		return "", false, err
	}
	running, _ := s.runningText()
	session.mu.RLock()
	candidate := session.CandidateText
	session.mu.RUnlock()
	diff := lineDiff(running, candidate)
	return diff, diff != "", nil
}

// ListHistory returns persisted commit history.
func (s *Server) ListHistory(ctx context.Context, limit, offset int) ([]CommitInfo, error) {
	if s.store == nil {
		return nil, nil
	}
	records, err := s.store.ListCommits(ctx, &store.ListOptions{Limit: limit, Offset: offset})
	if err != nil {
		return nil, err
	}
	entries := make([]CommitInfo, 0, len(records))
	for _, r := range records {
		entries = append(entries, CommitInfo{
			CommitID:   r.CommitID,
			User:       r.Author,
			Timestamp:  r.Timestamp,
			Message:    r.Message,
			IsRollback: r.IsRollback,
		})
	}
	return entries, nil
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
	if err := s.sessions.AcquireLock(sessionID); err != nil {
		return err
	}
	session, err := s.sessions.Get(sessionID)
	if err != nil {
		return err
	}
	session.mu.Lock()
	if session.CandidateText == "" {
		session.CandidateText, _ = s.runningText()
	}
	session.mu.Unlock()
	return nil
}

// ReleaseLock releases the candidate lock.
func (s *Server) ReleaseLock(ctx context.Context, sessionID string) error {
	return s.sessions.ReleaseLock(sessionID)
}

// GetInterfaces returns configured interfaces with basic status placeholders.
func (s *Server) GetInterfaces(ctx context.Context, nameFilter string) ([]InterfaceInfo, error) {
	cfg := s.engine.Running()
	names := make([]string, 0, len(cfg.Interfaces))
	for name := range cfg.Interfaces {
		if nameFilter == "" || name == nameFilter {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	result := make([]InterfaceInfo, 0, len(names))
	for _, name := range names {
		result = append(result, InterfaceInfo{
			Name:        name,
			AdminStatus: "configured",
			OperStatus:  "unknown",
		})
	}
	return result, nil
}

// GetRoutes returns configured static routes.
func (s *Server) GetRoutes(ctx context.Context, prefixFilter, protoFilter string) ([]RouteInfo, error) {
	if protoFilter != "" && protoFilter != "static" {
		return nil, nil
	}
	cfg := s.engine.Running()
	if cfg.Routing == nil {
		return nil, nil
	}
	routes := make([]RouteInfo, 0, len(cfg.Routing.StaticRoutes))
	for _, route := range cfg.Routing.StaticRoutes {
		if route == nil {
			continue
		}
		if prefixFilter != "" && route.Prefix != prefixFilter {
			continue
		}
		routes = append(routes, RouteInfo{
			Prefix:   route.Prefix,
			NextHop:  route.NextHop,
			Protocol: "static",
			Metric:   uint32(route.Distance),
			Active:   true,
		})
	}
	sort.Slice(routes, func(i, j int) bool {
		return routes[i].Prefix < routes[j].Prefix
	})
	return routes, nil
}

// GetBGPNeighbors returns configured BGP neighbors.
func (s *Server) GetBGPNeighbors(ctx context.Context) ([]BGPNeighborInfo, error) {
	cfg := s.engine.Running()
	if cfg.Protocols == nil || cfg.Protocols.BGP == nil {
		return nil, nil
	}
	var neighbors []BGPNeighborInfo
	for _, groupName := range sortedMapKeys(cfg.Protocols.BGP.Groups) {
		group := cfg.Protocols.BGP.Groups[groupName]
		if group == nil {
			continue
		}
		for _, peer := range sortedMapKeys(group.Neighbors) {
			neighbor := group.Neighbors[peer]
			if neighbor == nil {
				continue
			}
			neighbors = append(neighbors, BGPNeighborInfo{
				PeerAddress: peer,
				PeerAS:      neighbor.PeerAS,
				State:       "configured",
			})
		}
	}
	return neighbors, nil
}

// GetSystemInfo returns basic system information.
func (s *Server) GetSystemInfo(ctx context.Context) (*SystemInfo, error) {
	cfg := s.engine.Running()
	info := &SystemInfo{Version: "unknown"}
	if cfg.System != nil {
		info.Hostname = cfg.System.HostName
	}
	return info, nil
}

func (s *Server) runningText() (string, uint64) {
	snap := s.engine.RunningSnapshot()
	if snap == nil || snap.Config == nil {
		return "", 0
	}
	return pkgconfig.ToSetCommands(snap.Config.ToLegacyConfig()), snap.Version
}

func applyCandidateCommand(candidate, commandText string) (string, error) {
	lines := normalizeConfigLines(candidate)
	commands := strings.Split(commandText, "\n")
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		parts, err := cli.TokenizeCommand(command)
		if err != nil {
			return "", err
		}
		if len(parts) == 0 {
			continue
		}
		switch parts[0] {
		case "set":
			if len(parts) < 2 {
				return "", fmt.Errorf("'set' requires arguments")
			}
			line := "set " + cli.NormalizeConfigPath(parts[1:])
			lines = append(lines, line)
		case "delete":
			prefix, err := cli.ParseDeleteCommand(parts[1:], nil)
			if err != nil {
				return "", err
			}
			filtered := lines[:0]
			for _, line := range lines {
				if !cli.MatchesPrefix(line, prefix) {
					filtered = append(filtered, line)
				}
			}
			lines = filtered
		default:
			return "", fmt.Errorf("unsupported candidate command: %s", parts[0])
		}
	}
	return strings.Join(lines, "\n"), nil
}

func normalizeConfigLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func lineDiff(oldText, newText string) string {
	oldSet := make(map[string]struct{})
	for _, line := range normalizeConfigLines(oldText) {
		oldSet[line] = struct{}{}
	}
	newSet := make(map[string]struct{})
	for _, line := range normalizeConfigLines(newText) {
		newSet[line] = struct{}{}
	}
	var out []string
	for line := range oldSet {
		if _, ok := newSet[line]; !ok {
			out = append(out, "- "+line)
		}
	}
	for line := range newSet {
		if _, ok := oldSet[line]; !ok {
			out = append(out, "+ "+line)
		}
	}
	sort.Strings(out)
	return strings.Join(out, "\n")
}

func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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
