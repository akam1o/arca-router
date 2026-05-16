package netconf

import (
	"context"
	"encoding/xml"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/akam1o/arca-router/pkg/datastore"
)

type lockFailureDatastore struct {
	datastore.Datastore
}

func (d *lockFailureDatastore) AcquireLock(context.Context, *datastore.LockRequest) error {
	return errors.New("backend unavailable")
}

func (d *lockFailureDatastore) GetLockInfo(context.Context, string) (*datastore.LockInfo, error) {
	return &datastore.LockInfo{IsLocked: false}, nil
}

func TestLockFailureWithInactiveLockInfoReturnsOperationFailed(t *testing.T) {
	srv := NewServer(&lockFailureDatastore{}, nil)
	sess := &Session{
		ID:             "session-1",
		NumericID:      1,
		Username:       "alice",
		Role:           RoleOperator,
		LastUsed:       time.Now(),
		datastoreLocks: map[string]struct{}{},
	}
	rpc := &RPC{
		MessageID: "101",
		Operation: xml.Name{Local: "lock"},
		Content: []byte(`
			<target><candidate/></target>
		`),
	}

	reply := srv.HandleRPC(context.Background(), sess, rpc)
	if len(reply.Errors) != 1 {
		t.Fatalf("lock reply errors = %d, want 1", len(reply.Errors))
	}
	if reply.Errors[0].ErrorTag != ErrorTagOperationFailed {
		t.Fatalf("lock error tag = %s, want %s", reply.Errors[0].ErrorTag, ErrorTagOperationFailed)
	}
}

func TestUnlockWithoutActiveLockReturnsTimeout(t *testing.T) {
	ds, err := datastore.NewSQLiteDatastore(&datastore.Config{
		Backend:    datastore.BackendSQLite,
		SQLitePath: filepath.Join(t.TempDir(), "config.db"),
	})
	if err != nil {
		t.Fatalf("NewSQLiteDatastore() error = %v", err)
	}
	t.Cleanup(func() { _ = ds.Close() })

	srv := NewServer(ds, nil)
	sess := &Session{
		ID:             "session-1",
		NumericID:      1,
		Username:       "alice",
		Role:           RoleOperator,
		LastUsed:       time.Now(),
		datastoreLocks: map[string]struct{}{},
	}
	rpc := &RPC{
		MessageID: "101",
		Operation: xml.Name{Local: "unlock"},
		Content: []byte(`
			<target><candidate/></target>
		`),
	}

	reply := srv.HandleRPC(context.Background(), sess, rpc)
	if len(reply.Errors) != 1 {
		t.Fatalf("unlock reply errors = %d, want 1", len(reply.Errors))
	}
	if reply.Errors[0].ErrorTag != ErrorTagOperationFailed {
		t.Fatalf("unlock error tag = %s, want %s", reply.Errors[0].ErrorTag, ErrorTagOperationFailed)
	}
}

func TestLockStartupTargetRejectedAsUnsupported(t *testing.T) {
	srv := NewServer(&lockFailureDatastore{}, nil)
	sess := &Session{
		ID:             "session-1",
		NumericID:      1,
		Username:       "alice",
		Role:           RoleOperator,
		LastUsed:       time.Now(),
		datastoreLocks: map[string]struct{}{},
	}
	rpc := &RPC{
		MessageID: "101",
		Operation: xml.Name{Local: "lock"},
		Content: []byte(`
			<target><startup/></target>
		`),
	}

	reply := srv.HandleRPC(context.Background(), sess, rpc)
	assertStartupUnsupported(t, reply, "/rpc/lock/target")
}

func TestUnlockStartupTargetRejectedAsUnsupported(t *testing.T) {
	srv := NewServer(&lockFailureDatastore{}, nil)
	sess := &Session{
		ID:             "session-1",
		NumericID:      1,
		Username:       "alice",
		Role:           RoleOperator,
		LastUsed:       time.Now(),
		datastoreLocks: map[string]struct{}{},
	}
	rpc := &RPC{
		MessageID: "101",
		Operation: xml.Name{Local: "unlock"},
		Content: []byte(`
			<target><startup/></target>
		`),
	}

	reply := srv.HandleRPC(context.Background(), sess, rpc)
	assertStartupUnsupported(t, reply, "/rpc/unlock/target")
}
