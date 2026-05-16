package netconf

import (
	"context"
	"encoding/xml"
	"errors"
	"testing"
	"time"

	"github.com/akam1o/arca-router/pkg/datastore"
)

type validateDatastore struct {
	datastore.Datastore
	running      *datastore.RunningConfig
	candidate    *datastore.CandidateConfig
	runningErr   error
	candidateErr error
}

func (d *validateDatastore) GetRunning(context.Context) (*datastore.RunningConfig, error) {
	if d.runningErr != nil {
		return nil, d.runningErr
	}
	return d.running, nil
}

func (d *validateDatastore) GetCandidate(context.Context, string) (*datastore.CandidateConfig, error) {
	if d.candidateErr != nil {
		return nil, d.candidateErr
	}
	return d.candidate, nil
}

func TestValidateRunningDatastore(t *testing.T) {
	reply := validateRPC(t, &validateDatastore{
		running: &datastore.RunningConfig{ConfigText: "set system host-name router1\n"},
	}, "<source><running/></source>")

	if len(reply.Errors) != 0 {
		t.Fatalf("validate running errors = %#v, want none", reply.Errors)
	}
	if reply.OK == nil {
		t.Fatal("validate running OK = nil, want ok")
	}
}

func TestValidateRunningDatastoreSemanticError(t *testing.T) {
	reply := validateRPC(t, &validateDatastore{
		running: &datastore.RunningConfig{ConfigText: "set system host-name bad_name\n"},
	}, "<source><running/></source>")

	if len(reply.Errors) != 1 {
		t.Fatalf("validate running errors = %d, want 1", len(reply.Errors))
	}
	err := reply.Errors[0]
	if err.ErrorTag != ErrorTagInvalidValue {
		t.Fatalf("validate running error tag = %s, want %s", err.ErrorTag, ErrorTagInvalidValue)
	}
	if err.ErrorPath != "/rpc/validate/source" {
		t.Fatalf("validate running error path = %q, want /rpc/validate/source", err.ErrorPath)
	}
}

func TestValidateCandidateDatastoreStillSupported(t *testing.T) {
	reply := validateRPC(t, &validateDatastore{
		candidate: &datastore.CandidateConfig{ConfigText: "set system host-name router1\n"},
	}, "<source><candidate/></source>")

	if len(reply.Errors) != 0 {
		t.Fatalf("validate candidate errors = %#v, want none", reply.Errors)
	}
	if reply.OK == nil {
		t.Fatal("validate candidate OK = nil, want ok")
	}
}

func TestValidateStartupDatastoreRejected(t *testing.T) {
	reply := validateRPC(t, &validateDatastore{}, "<source><startup/></source>")

	if len(reply.Errors) != 1 {
		t.Fatalf("validate startup errors = %d, want 1", len(reply.Errors))
	}
	err := reply.Errors[0]
	if err.ErrorTag != ErrorTagInvalidValue {
		t.Fatalf("validate startup error tag = %s, want %s", err.ErrorTag, ErrorTagInvalidValue)
	}
}

func TestValidateRunningDatastoreReadError(t *testing.T) {
	reply := validateRPC(t, &validateDatastore{
		runningErr: errors.New("backend unavailable"),
	}, "<source><running/></source>")

	if len(reply.Errors) != 1 {
		t.Fatalf("validate running read errors = %d, want 1", len(reply.Errors))
	}
	if reply.Errors[0].ErrorTag != ErrorTagOperationFailed {
		t.Fatalf("validate running read error tag = %s, want %s", reply.Errors[0].ErrorTag, ErrorTagOperationFailed)
	}
}

func validateRPC(t *testing.T, ds datastore.Datastore, content string) *RPCReply {
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
	rpc := &RPC{
		MessageID: "101",
		Operation: xml.Name{Local: "validate"},
		Content:   []byte(content),
	}
	return srv.HandleRPC(context.Background(), sess, rpc)
}
