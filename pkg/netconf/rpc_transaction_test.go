package netconf

import (
	"context"
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

func TestValidateInlineConfigSource(t *testing.T) {
	reply := validateRPC(t, &validateDatastore{}, "<source><config><system><host-name>router1</host-name></system></config></source>")

	if len(reply.Errors) != 0 {
		t.Fatalf("validate inline source errors = %#v, want none", reply.Errors)
	}
	if reply.OK == nil {
		t.Fatal("validate inline source OK = nil, want ok")
	}
}

func TestValidateInlineConfigSourceSemanticError(t *testing.T) {
	reply := validateRPC(t, &validateDatastore{}, "<source><config><system><host-name>bad_name</host-name></system></config></source>")

	if len(reply.Errors) != 1 {
		t.Fatalf("validate inline source errors = %d, want 1", len(reply.Errors))
	}
	err := reply.Errors[0]
	if err.ErrorTag != ErrorTagInvalidValue {
		t.Fatalf("validate inline source error tag = %s, want %s", err.ErrorTag, ErrorTagInvalidValue)
	}
	if err.ErrorPath != "/rpc/validate/source" {
		t.Fatalf("validate inline source error path = %q, want /rpc/validate/source", err.ErrorPath)
	}
}

func TestValidateInlineSourcePreservesAncestorNamespaceDeclarations(t *testing.T) {
	reply := validateParsedRPC(t, &validateDatastore{}, `<rpc message-id="101"
		xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"
		xmlns:arca="`+ArcaConfigNS+`">
		<validate>
			<source><config><arca:system><arca:host-name>router1</arca:host-name></arca:system></config></source>
		</validate>
	</rpc>`)

	if len(reply.Errors) != 0 {
		t.Fatalf("validate namespace-prefixed inline source errors = %#v, want none", reply.Errors)
	}
	if reply.OK == nil {
		t.Fatal("validate namespace-prefixed inline source OK = nil, want ok")
	}
}

func TestValidateStartupDatastoreRejected(t *testing.T) {
	reply := validateRPC(t, &validateDatastore{}, "<source><startup/></source>")

	if len(reply.Errors) != 1 {
		t.Fatalf("validate startup errors = %d, want 1", len(reply.Errors))
	}
	err := reply.Errors[0]
	if err.ErrorTag != ErrorTagOperationNotSupported {
		t.Fatalf("validate startup error tag = %s, want %s", err.ErrorTag, ErrorTagOperationNotSupported)
	}
	if err.ErrorPath != "/rpc/validate/source" {
		t.Fatalf("validate startup error path = %q, want /rpc/validate/source", err.ErrorPath)
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

func TestParseRPCAcceptsValidateInlineSource(t *testing.T) {
	if _, err := ParseRPC([]byte(`<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
		<validate>
			<source><config><system><host-name>router1</host-name></system></config></source>
		</validate>
	</rpc>`)); err != nil {
		t.Fatalf("ParseRPC() inline validate source error = %v", err)
	}
}

func TestParseRPCRejectsValidateMultipleSourceChoices(t *testing.T) {
	_, err := ParseRPC([]byte(`<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
		<validate>
			<source><candidate/><config><system><host-name>router1</host-name></system></config></source>
		</validate>
	</rpc>`))
	if err == nil {
		t.Fatal("ParseRPC() error = nil, want multiple source choices error")
	}
	rpcErr, ok := err.(*RPCError)
	if !ok {
		t.Fatalf("ParseRPC() error = %T, want *RPCError", err)
	}
	if rpcErr.ErrorTag != ErrorTagMalformedMessage {
		t.Fatalf("ParseRPC() error tag = %s, want %s", rpcErr.ErrorTag, ErrorTagMalformedMessage)
	}
}

func validateRPC(t *testing.T, ds datastore.Datastore, content string) *RPCReply {
	t.Helper()

	return validateParsedRPC(t, ds, `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
		<validate>
			`+content+`
		</validate>
	</rpc>`)
}

func validateParsedRPC(t *testing.T, ds datastore.Datastore, rpcXML string) *RPCReply {
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
	rpc, err := ParseRPC([]byte(rpcXML))
	if err != nil {
		t.Fatalf("ParseRPC() error = %v", err)
	}
	return srv.HandleRPC(context.Background(), sess, rpc)
}
