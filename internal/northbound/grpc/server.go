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
	return s.runningText()
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

	session.mu.Lock()
	defer session.mu.Unlock()
	if !session.HasLock {
		return fmt.Errorf("session %s does not hold the candidate lock", sessionID)
	}
	if err := s.ensureCandidateBaseCurrentLocked(session); err != nil {
		return err
	}

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

	session.mu.Lock()
	defer session.mu.Unlock()
	if !session.HasLock {
		return "", 0, fmt.Errorf("session %s does not hold the candidate lock", sessionID)
	}

	// Parse the candidate config text using the existing pkg/config parser
	candidateText := session.CandidateText

	if !session.CandidateBaseSet {
		return "", 0, fmt.Errorf("no candidate configuration to commit")
	}
	if err := s.ensureCandidateBaseCurrentLocked(session); err != nil {
		return "", 0, err
	}

	// Parse candidate text into new config model
	newCfg, err := parseConfigText(candidateText)
	if err != nil {
		return "", 0, fmt.Errorf("parse candidate config: %w", err)
	}
	if err := s.engine.Validate(ctx, newCfg); err != nil {
		return "", 0, err
	}
	if !s.hasCandidateChanges(newCfg) {
		return "", 0, fmt.Errorf("no configuration changes to commit")
	}

	var prepared store.PreparedCommit
	if s.store != nil {
		version := uint64(1)
		if current := s.engine.RunningSnapshot(); current != nil {
			version = current.Version + 1
		}
		prepared, err = s.store.PrepareCommit(ctx, model.NewSnapshot(newCfg, version, user, message))
		if err != nil {
			return "", 0, fmt.Errorf("prepare commit persistence: %w", err)
		}
		if err := s.ensureCandidateBaseCurrentLocked(session); err != nil {
			abortErr := prepared.Abort(context.Background())
			if abortErr != nil {
				return "", 0, fmt.Errorf("%w (abort failed: %v)", err, abortErr)
			}
			return "", 0, err
		}
		if !s.hasCandidateChanges(newCfg) {
			abortErr := prepared.Abort(context.Background())
			if abortErr != nil {
				return "", 0, fmt.Errorf("no configuration changes to commit (abort failed: %v)", abortErr)
			}
			return "", 0, fmt.Errorf("no configuration changes to commit")
		}
	}

	beforeSnap := s.engine.RunningSnapshot()

	// Apply via engine (diff + validate + apply atomically)
	if err := s.engine.Apply(ctx, newCfg, user, message); err != nil {
		if prepared != nil {
			_ = prepared.Abort(context.Background())
		}
		return "", 0, err
	}

	snap := s.engine.RunningSnapshot()
	commitID := ""
	if prepared != nil {
		commitID, err = prepared.Commit(ctx)
		if err != nil {
			abortErr := prepared.Abort(context.Background())
			if rollbackErr := s.rollbackToSnapshot(context.Background(), beforeSnap, user); rollbackErr != nil {
				return "", 0, fmt.Errorf("persist commit after apply: %w (rollback failed: %v)", err, rollbackErr)
			}
			if abortErr != nil {
				return "", 0, fmt.Errorf("persist commit after apply: %w (abort failed: %v)", err, abortErr)
			}
			return "", 0, fmt.Errorf("persist commit after apply: %w", err)
		}
	}
	if err := s.resetSessionCandidateLocked(session); err != nil {
		return "", 0, err
	}
	return commitID, snap.Version, nil
}

// ValidateCandidate parses and validates the session candidate without applying it.
func (s *Server) ValidateCandidate(ctx context.Context, sessionID string) error {
	session, err := s.sessions.Get(sessionID)
	if err != nil {
		return err
	}
	session.mu.RLock()
	candidateText := session.CandidateText
	hasCandidate := session.CandidateBaseSet
	staleErr := s.ensureCandidateBaseCurrentLocked(session)
	session.mu.RUnlock()
	if !hasCandidate {
		return fmt.Errorf("no candidate configuration to validate")
	}
	if staleErr != nil {
		return staleErr
	}
	cfg, err := parseConfigText(candidateText)
	if err != nil {
		return fmt.Errorf("parse candidate config: %w", err)
	}
	return s.engine.Validate(ctx, cfg)
}

// Discard clears the candidate configuration for a session.
func (s *Server) Discard(ctx context.Context, sessionID string) error {
	session, err := s.sessions.Get(sessionID)
	if err != nil {
		return err
	}
	return s.resetSessionCandidate(session)
}

// Rollback reverts running configuration to a previous commit.
func (s *Server) Rollback(ctx context.Context, sessionID, commitID, user, message string) (string, uint64, error) {
	if s.store == nil {
		return "", 0, fmt.Errorf("commit history is unavailable")
	}
	if commitID == "" {
		return "", 0, fmt.Errorf("commit ID is required")
	}
	session, err := s.sessions.Get(sessionID)
	if err != nil {
		return "", 0, err
	}

	session.mu.Lock()
	defer session.mu.Unlock()
	if !session.HasLock {
		return "", 0, fmt.Errorf("session %s does not hold the candidate lock", sessionID)
	}
	if err := s.ensureCandidateBaseCurrentLocked(session); err != nil {
		return "", 0, err
	}
	if user == "" {
		user = session.User
	}
	if message == "" {
		message = fmt.Sprintf("rollback to commit %s", commitID)
	}

	record, err := s.store.GetCommit(ctx, commitID)
	if err != nil {
		return "", 0, fmt.Errorf("load rollback commit: %w", err)
	}
	if record == nil || record.Config == nil {
		return "", 0, fmt.Errorf("commit %s has no configuration", commitID)
	}
	newCfg := record.Config
	if err := s.engine.Validate(ctx, newCfg); err != nil {
		return "", 0, err
	}
	if !s.hasCandidateChanges(newCfg) {
		return "", 0, fmt.Errorf("rollback target matches running configuration")
	}

	version := uint64(1)
	if current := s.engine.RunningSnapshot(); current != nil {
		version = current.Version + 1
	}
	rollbackSnap := model.NewSnapshot(newCfg, version, user, message)
	var prepared store.PreparedCommit
	if rollbackStore, ok := s.store.(store.RollbackPreparer); ok {
		prepared, err = rollbackStore.PrepareRollback(ctx, rollbackSnap, commitID)
	} else {
		prepared, err = s.store.PrepareCommit(ctx, rollbackSnap)
	}
	if err != nil {
		return "", 0, fmt.Errorf("prepare rollback persistence: %w", err)
	}
	if err := s.ensureCandidateBaseCurrentLocked(session); err != nil {
		abortErr := prepared.Abort(context.Background())
		if abortErr != nil {
			return "", 0, fmt.Errorf("%w (abort failed: %v)", err, abortErr)
		}
		return "", 0, err
	}
	if !s.hasCandidateChanges(newCfg) {
		abortErr := prepared.Abort(context.Background())
		if abortErr != nil {
			return "", 0, fmt.Errorf("rollback target matches running configuration (abort failed: %v)", abortErr)
		}
		return "", 0, fmt.Errorf("rollback target matches running configuration")
	}

	beforeSnap := s.engine.RunningSnapshot()
	if err := s.engine.Apply(ctx, newCfg, user, message); err != nil {
		_ = prepared.Abort(context.Background())
		return "", 0, err
	}

	newCommitID, err := prepared.Commit(ctx)
	if err != nil {
		_ = prepared.Abort(context.Background())
		if rollbackErr := s.rollbackToSnapshot(context.Background(), beforeSnap, user); rollbackErr != nil {
			return "", 0, fmt.Errorf("persist rollback after apply: %w (engine rollback failed: %v)", err, rollbackErr)
		}
		return "", 0, fmt.Errorf("persist rollback after apply: %w", err)
	}

	snap := s.engine.RunningSnapshot()
	if err := s.resetSessionCandidateLocked(session); err != nil {
		return "", 0, err
	}
	if snap == nil {
		return newCommitID, 0, nil
	}
	return newCommitID, snap.Version, nil
}

// Diff returns a simple line-oriented diff between running and candidate config.
func (s *Server) Diff(ctx context.Context, sessionID string) (string, bool, error) {
	session, err := s.sessions.Get(sessionID)
	if err != nil {
		return "", false, err
	}
	running, _, err := s.runningText()
	if err != nil {
		return "", false, err
	}
	session.mu.RLock()
	candidate := session.CandidateText
	session.mu.RUnlock()
	diff := lineDiff(running, candidate)
	return diff, diff != "", nil
}

// ListHistory returns persisted commit history.
func (s *Server) ListHistory(ctx context.Context, limit, offset int) ([]CommitInfo, error) {
	if limit < 0 {
		return nil, fmt.Errorf("invalid history limit: %d", limit)
	}
	if offset < 0 {
		return nil, fmt.Errorf("invalid history offset: %d", offset)
	}
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
		_ = s.sessions.ReleaseLock(sessionID)
		return err
	}
	session.mu.Lock()
	if session.CandidateText == "" {
		if err := s.resetSessionCandidateLocked(session); err != nil {
			session.mu.Unlock()
			_ = s.sessions.ReleaseLock(sessionID)
			return err
		}
	} else if err := s.ensureCandidateBaseCurrentLocked(session); err != nil {
		session.mu.Unlock()
		_ = s.sessions.ReleaseLock(sessionID)
		return err
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

func (s *Server) runningText() (string, uint64, error) {
	snap := s.engine.RunningSnapshot()
	if snap == nil || snap.Config == nil {
		return "", 0, nil
	}
	text, err := pkgconfig.ToSetCommandsWithError(snap.Config.ToLegacyConfig())
	if err != nil {
		return "", 0, fmt.Errorf("serialize running config: %w", err)
	}
	return text, snap.Version, nil
}

func (s *Server) resetSessionCandidate(session *Session) error {
	session.mu.Lock()
	err := s.resetSessionCandidateLocked(session)
	session.mu.Unlock()
	return err
}

func (s *Server) resetSessionCandidateLocked(session *Session) error {
	snap := s.engine.RunningSnapshot()
	text := ""
	if snap != nil && snap.Config != nil {
		var err error
		text, err = pkgconfig.ToSetCommandsWithError(snap.Config.ToLegacyConfig())
		if err != nil {
			return fmt.Errorf("serialize running config: %w", err)
		}
	}
	session.CandidateBaseSet = true
	if snap == nil {
		session.CandidateBaseVersion = 0
		session.CandidateBaseHash = [32]byte{}
	} else {
		session.CandidateBaseVersion = snap.Version
		session.CandidateBaseHash = snap.Hash
	}
	session.CandidateText = text
	return nil
}

func (s *Server) ensureCandidateBaseCurrentLocked(session *Session) error {
	if !session.CandidateBaseSet {
		return nil
	}
	snap := s.engine.RunningSnapshot()
	var version uint64
	var hash [32]byte
	if snap != nil {
		version = snap.Version
		hash = snap.Hash
	}
	if session.CandidateBaseVersion != version || session.CandidateBaseHash != hash {
		return fmt.Errorf("candidate configuration is stale: running configuration changed from version %d to %d; discard or reload the candidate before editing", session.CandidateBaseVersion, version)
	}
	return nil
}

func (s *Server) hasCandidateChanges(candidate *model.RouterConfig) bool {
	snap := s.engine.RunningSnapshot()
	var running *model.RouterConfig
	if snap != nil {
		running = snap.Config
	}
	return engine.ComputeDiff(running, candidate).HasChanges()
}

func (s *Server) rollbackToSnapshot(ctx context.Context, snap *model.ConfigSnapshot, user string) error {
	cfg := model.NewRouterConfig()
	if snap != nil && snap.Config != nil {
		cfg = snap.Config
	}
	return s.engine.Apply(ctx, cfg, user, "rollback failed commit persistence")
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
			if prefixes := replacementPrefixes(parts[1:]); len(prefixes) > 0 {
				lines = removeMatchingPrefixes(lines, prefixes)
			}
			if containsLine(lines, line) {
				continue
			}
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

func removeMatchingPrefixes(lines []string, prefixes []string) []string {
	filtered := lines[:0]
	for _, line := range lines {
		matched := false
		for _, prefix := range prefixes {
			if cli.MatchesPrefix(line, prefix) {
				matched = true
				break
			}
		}
		if !matched {
			filtered = append(filtered, line)
		}
	}
	return filtered
}

func containsLine(lines []string, target string) bool {
	for _, line := range lines {
		if line == target {
			return true
		}
	}
	return false
}

func replacementPrefixes(path []string) []string {
	prefix := func(n int) []string {
		return []string{"set " + cli.NormalizeConfigPath(path[:n])}
	}
	if len(path) >= 3 && path[0] == "system" && path[1] == "host-name" {
		return prefix(2)
	}
	if len(path) >= 4 && path[0] == "interfaces" && path[2] == "description" {
		return prefix(3)
	}
	if len(path) >= 3 && path[0] == "routing-options" {
		switch path[1] {
		case "router-id", "autonomous-system":
			return prefix(2)
		case "static":
			if len(path) >= 5 && path[2] == "route" {
				return prefix(4)
			}
		}
	}
	if len(path) >= 4 && path[0] == "protocols" {
		switch path[1] {
		case "ospf":
			if path[2] == "router-id" {
				return prefix(3)
			}
			if len(path) >= 7 && path[2] == "area" && path[4] == "interface" {
				switch path[6] {
				case "passive", "metric", "priority":
					return prefix(7)
				}
			}
		case "bgp":
			if len(path) >= 5 && path[2] == "group" {
				switch path[4] {
				case "type", "import", "export":
					return prefix(5)
				case "neighbor":
					if len(path) >= 8 {
						switch path[6] {
						case "peer-as", "description", "local-address":
							return prefix(7)
						}
					}
				}
			}
		}
	}
	if len(path) >= 7 && path[0] == "policy-options" && path[1] == "policy-statement" && path[3] == "term" {
		if path[5] == "from" {
			if len(path) >= 8 {
				switch path[6] {
				case "protocol", "neighbor", "as-path":
					return prefix(7)
				}
			}
		}
		if path[5] == "then" {
			switch path[6] {
			case "local-preference", "community":
				return prefix(7)
			case "accept", "reject":
				base := "set " + cli.NormalizeConfigPath(path[:6])
				return []string{base + " accept", base + " reject"}
			}
		}
	}
	if len(path) >= 4 && path[0] == "security" {
		if path[1] == "netconf" && len(path) >= 5 && path[2] == "ssh" && path[3] == "port" {
			return prefix(4)
		}
		if path[1] == "rate-limit" && len(path) >= 4 {
			switch path[2] {
			case "per-ip", "per-user":
				return prefix(3)
			}
		}
		if len(path) >= 6 && path[1] == "users" && path[2] == "user" {
			switch path[4] {
			case "password", "role", "ssh-key":
				return prefix(5)
			}
		}
	}
	return nil
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
	mu                   sync.RWMutex
	ID                   string
	User                 string
	HasLock              bool
	CandidateText        string
	CandidateBaseVersion uint64
	CandidateBaseHash    [32]byte
	CandidateBaseSet     bool
	CreatedAt            time.Time
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
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.HasLock {
		return fmt.Errorf("session %s does not hold the candidate lock", id)
	}
	s.HasLock = false
	m.lockHeld = ""
	return nil
}
