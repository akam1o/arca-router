package netconf

import (
	"context"
	"encoding/xml"
	"path/filepath"
	"testing"
	"time"

	"github.com/akam1o/arca-router/pkg/datastore"
)

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
