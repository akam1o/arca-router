package netconf

import (
	"context"
	"testing"
	"time"

	"github.com/akam1o/arca-router/pkg/datastore"
)

type copyConfigDatastore struct {
	datastore.Datastore
	running    *datastore.RunningConfig
	candidate  *datastore.CandidateConfig
	lockInfo   *datastore.LockInfo
	saveCalled bool
	savedText  string
	savedID    string
}

func (d *copyConfigDatastore) GetRunning(context.Context) (*datastore.RunningConfig, error) {
	return d.running, nil
}

func (d *copyConfigDatastore) GetCandidate(context.Context, string) (*datastore.CandidateConfig, error) {
	return d.candidate, nil
}

func (d *copyConfigDatastore) SaveCandidate(_ context.Context, sessionID string, configText string) error {
	d.saveCalled = true
	d.savedID = sessionID
	d.savedText = configText
	return nil
}

func (d *copyConfigDatastore) GetLockInfo(context.Context, string) (*datastore.LockInfo, error) {
	return d.lockInfo, nil
}

func TestCopyConfigValidatesRunningSourceBeforeSavingCandidate(t *testing.T) {
	ds := &copyConfigDatastore{
		running: &datastore.RunningConfig{ConfigText: "set system host-name bad_name\n"},
		lockInfo: &datastore.LockInfo{
			IsLocked:  true,
			SessionID: "session-1",
		},
	}

	reply := copyConfigRPC(t, ds, "<source><running/></source>")
	if len(reply.Errors) != 1 {
		t.Fatalf("copy-config errors = %d, want 1", len(reply.Errors))
	}
	err := reply.Errors[0]
	if err.ErrorTag != ErrorTagInvalidValue {
		t.Fatalf("copy-config error tag = %s, want %s", err.ErrorTag, ErrorTagInvalidValue)
	}
	if err.ErrorPath != "/rpc/copy-config/source" {
		t.Fatalf("copy-config error path = %q, want /rpc/copy-config/source", err.ErrorPath)
	}
	if ds.saveCalled {
		t.Fatal("copy-config saved invalid source config")
	}
}

func TestCopyConfigSavesValidatedRunningSource(t *testing.T) {
	const runningConfig = "set system host-name router1\n"
	ds := &copyConfigDatastore{
		running: &datastore.RunningConfig{ConfigText: runningConfig},
		lockInfo: &datastore.LockInfo{
			IsLocked:  true,
			SessionID: "session-1",
		},
	}

	reply := copyConfigRPC(t, ds, "<source><running/></source>")
	if len(reply.Errors) != 0 {
		t.Fatalf("copy-config errors = %#v, want none", reply.Errors)
	}
	if reply.OK == nil {
		t.Fatal("copy-config OK = nil, want ok")
	}
	if !ds.saveCalled {
		t.Fatal("copy-config did not save candidate")
	}
	if ds.savedID != "session-1" {
		t.Fatalf("saved session ID = %q, want session-1", ds.savedID)
	}
	if ds.savedText != runningConfig {
		t.Fatalf("saved candidate = %q, want %q", ds.savedText, runningConfig)
	}
}

func copyConfigRPC(t *testing.T, ds datastore.Datastore, source string) *RPCReply {
	t.Helper()

	srv := NewServer(ds, nil)
	sess := &Session{
		ID:             "session-1",
		NumericID:      1,
		Username:       "alice",
		Role:           RoleOperator,
		LastUsed:       time.Now(),
		datastoreLocks: map[string]struct{}{},
	}
	rpc, err := ParseRPC([]byte(`<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
		<copy-config>
			<target><candidate/></target>
			` + source + `
		</copy-config>
	</rpc>`))
	if err != nil {
		t.Fatalf("ParseRPC() error = %v", err)
	}
	return srv.HandleRPC(context.Background(), sess, rpc)
}
