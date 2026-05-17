package netconf

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/akam1o/arca-router/pkg/logger"
)

func TestSessionManagerCountsScaledSessions(t *testing.T) {
	sm := newTestSessionManager(nil)

	const sessionCount = 256
	sessions := make([]*NETCONFSession, 0, sessionCount)
	seenIDs := make(map[string]struct{}, sessionCount)
	seenNumericIDs := make(map[uint32]struct{}, sessionCount)
	for i := 0; i < sessionCount; i++ {
		session := sm.Create(fmt.Sprintf("user-%03d", i), RoleOperator, nil, nil)
		sessions = append(sessions, session)
		if session.ID == "" {
			t.Fatal("Create() returned empty session ID")
		}
		if session.NumericID == 0 {
			t.Fatal("Create() returned zero numeric session ID")
		}
		if _, ok := seenIDs[session.ID]; ok {
			t.Fatalf("duplicate session ID %q", session.ID)
		}
		seenIDs[session.ID] = struct{}{}
		if _, ok := seenNumericIDs[session.NumericID]; ok {
			t.Fatalf("duplicate numeric session ID %d", session.NumericID)
		}
		seenNumericIDs[session.NumericID] = struct{}{}
	}

	if got := sm.Count(); got != sessionCount {
		t.Fatalf("Count() = %d, want %d", got, sessionCount)
	}
	for _, session := range sessions {
		if got, ok := sm.Get(session.ID); !ok || got != session {
			t.Fatalf("Get(%q) = %#v, %t; want original session", session.ID, got, ok)
		}
		if got, ok := sm.GetByNumericID(session.NumericID); !ok || got != session {
			t.Fatalf("GetByNumericID(%d) = %#v, %t; want original session", session.NumericID, got, ok)
		}
	}

	if err := sm.CloseSessionByNumericID(sessions[0].NumericID); err != nil {
		t.Fatalf("CloseSessionByNumericID() error = %v", err)
	}
	if got := sm.Count(); got != sessionCount-1 {
		t.Fatalf("Count() after numeric close = %d, want %d", got, sessionCount-1)
	}
	if _, ok := sm.Get(sessions[0].ID); ok {
		t.Fatalf("Get(%q) found session after numeric close", sessions[0].ID)
	}
	if _, ok := sm.GetByNumericID(sessions[0].NumericID); ok {
		t.Fatalf("GetByNumericID(%d) found session after numeric close", sessions[0].NumericID)
	}

	for _, session := range sessions[1:] {
		if err := sm.CloseSession(session.ID); err != nil {
			t.Fatalf("CloseSession(%q) error = %v", session.ID, err)
		}
	}
	if got := sm.Count(); got != 0 {
		t.Fatalf("Count() after close = %d, want 0", got)
	}
}

func TestSessionManagerCloseAllClearsScaledSessionsAndLocks(t *testing.T) {
	store := &recordingLockReleaser{}
	sm := newTestSessionManager(store)

	const sessionCount = 128
	sessions := make([]*NETCONFSession, 0, sessionCount)
	for i := 0; i < sessionCount; i++ {
		session := sm.Create(fmt.Sprintf("user-%03d", i), RoleAdmin, nil, nil)
		session.AddLock("candidate")
		sessions = append(sessions, session)
	}
	if got := sm.Count(); got != sessionCount {
		t.Fatalf("Count() before CloseAll() = %d, want %d", got, sessionCount)
	}

	sm.CloseAll()

	if got := sm.Count(); got != 0 {
		t.Fatalf("Count() after CloseAll() = %d, want 0", got)
	}
	if got := store.releaseCount(); got != sessionCount {
		t.Fatalf("released locks = %d, want %d", got, sessionCount)
	}
	for _, session := range sessions {
		select {
		case <-session.ctx.Done():
		default:
			t.Fatalf("session %q context is still active after CloseAll()", session.ID)
		}
		if _, ok := sm.Get(session.ID); ok {
			t.Fatalf("Get(%q) found session after CloseAll()", session.ID)
		}
		if _, ok := sm.GetByNumericID(session.NumericID); ok {
			t.Fatalf("GetByNumericID(%d) found session after CloseAll()", session.NumericID)
		}
		if locks := session.GetLocks(); len(locks) != 0 {
			t.Fatalf("session %q locks after CloseAll() = %#v, want none", session.ID, locks)
		}
	}
}

func TestNewSessionManagerDefaultsNilDependencies(t *testing.T) {
	sm := NewSessionManager(nil, nil, nil)
	if sm == nil {
		t.Fatal("NewSessionManager() = nil")
	}

	session := sm.Create("alice", RoleOperator, nil, nil)
	if session == nil {
		t.Fatal("Create() = nil")
	}
	if session.IdleTimeout != 30*time.Minute {
		t.Fatalf("IdleTimeout = %s, want 30m", session.IdleTimeout)
	}
	if session.AbsoluteTimeout != 24*time.Hour {
		t.Fatalf("AbsoluteTimeout = %s, want 24h", session.AbsoluteTimeout)
	}
	if sm.log == nil {
		t.Fatal("session manager logger = nil")
	}
}

func TestSessionAddLockInitializesNilTrackingMap(t *testing.T) {
	session := &Session{}

	session.AddLock("candidate")

	locks := session.GetLocks()
	if len(locks) != 1 || locks[0] != "candidate" {
		t.Fatalf("GetLocks() = %#v, want candidate", locks)
	}
}

func newTestSessionManager(store DatastoreLockReleaser) *SessionManager {
	cfg := DefaultSSHConfig()
	cfg.IdleTimeout = time.Hour
	cfg.AbsoluteTimeout = 24 * time.Hour
	log := logger.New("test", &logger.Config{Level: slog.LevelError, AddSource: false})
	return NewSessionManager(cfg, store, log)
}

type recordingLockReleaser struct {
	mu       sync.Mutex
	releases []string
}

func (r *recordingLockReleaser) ReleaseLock(ctx context.Context, target string, sessionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.releases = append(r.releases, target+"\x00"+sessionID)
	return nil
}

func (r *recordingLockReleaser) releaseCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.releases)
}
