package netconf

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/akam1o/arca-router/pkg/datastore"
)

type validateDatastore struct {
	datastore.Datastore
	running      *datastore.RunningConfig
	candidate    *datastore.CandidateConfig
	lockInfo     *datastore.LockInfo
	runningErr   error
	candidateErr error
	commitErr    error
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

func (d *validateDatastore) GetLockInfo(context.Context, string) (*datastore.LockInfo, error) {
	return d.lockInfo, nil
}

func (d *validateDatastore) Commit(context.Context, *datastore.CommitRequest) (string, error) {
	if d.commitErr != nil {
		return "", d.commitErr
	}
	return "commit-1", nil
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

func TestValidateCandidateFallsBackToRunningWhenMissing(t *testing.T) {
	reply := validateRPC(t, &validateDatastore{
		running: &datastore.RunningConfig{ConfigText: "set system host-name router1\n"},
	}, "<source><candidate/></source>")

	if len(reply.Errors) != 0 {
		t.Fatalf("validate missing candidate errors = %#v, want none", reply.Errors)
	}
	if reply.OK == nil {
		t.Fatal("validate missing candidate OK = nil, want ok")
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
		runningErr: errors.New("sqlite /var/lib/arca-router/config.db unavailable"),
	}, "<source><running/></source>")

	if len(reply.Errors) != 1 {
		t.Fatalf("validate running read errors = %d, want 1", len(reply.Errors))
	}
	if reply.Errors[0].ErrorTag != ErrorTagOperationFailed {
		t.Fatalf("validate running read error tag = %s, want %s", reply.Errors[0].ErrorTag, ErrorTagOperationFailed)
	}
	if got := reply.Errors[0].ErrorMessage; got != "failed to retrieve running config for validate" {
		t.Fatalf("validate running read message = %q, want redacted datastore failure", got)
	}
	if strings.Contains(reply.Errors[0].ErrorMessage, "/var/lib/arca-router/config.db") {
		t.Fatalf("validate running read message leaked backend path: %q", reply.Errors[0].ErrorMessage)
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

func TestCommitConfirmedOptionsRejectedAsUnsupported(t *testing.T) {
	tests := []struct {
		name    string
		content string
		element string
	}{
		{
			name:    "confirmed",
			content: "<confirmed/>",
			element: "confirmed",
		},
		{
			name:    "confirm-timeout",
			content: "<confirm-timeout>60</confirm-timeout>",
			element: "confirm-timeout",
		},
		{
			name:    "persist",
			content: "<persist>commit-token</persist>",
			element: "persist",
		},
		{
			name:    "persist-id",
			content: "<persist-id>commit-token</persist-id>",
			element: "persist-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reply := commitRPC(t, &validateDatastore{}, tt.content)

			assertConfirmedCommitUnsupported(t, reply, tt.element)
		})
	}
}

func TestCommitCandidateReadErrorReturnsDatastoreError(t *testing.T) {
	reply := commitRPC(t, &validateDatastore{
		candidateErr: errors.New("sqlite /var/lib/arca-router/config.db unavailable"),
		lockInfo: &datastore.LockInfo{
			IsLocked:  true,
			SessionID: "session-1",
		},
	}, "")

	if len(reply.Errors) != 1 {
		t.Fatalf("commit candidate read errors = %d, want 1", len(reply.Errors))
	}
	err := reply.Errors[0]
	if err.ErrorTag != ErrorTagOperationFailed {
		t.Fatalf("commit candidate read error tag = %s, want %s", err.ErrorTag, ErrorTagOperationFailed)
	}
	if err.ErrorAppTag != "datastore-error" {
		t.Fatalf("commit candidate read app-tag = %q, want datastore-error", err.ErrorAppTag)
	}
	if err.ErrorMessage != "failed to read candidate config" {
		t.Fatalf("commit candidate read message = %q, want redacted read failure", err.ErrorMessage)
	}
	if strings.Contains(err.ErrorMessage, "/var/lib/arca-router/config.db") {
		t.Fatalf("commit candidate read message leaked backend path: %q", err.ErrorMessage)
	}
}

func TestCommitDatastoreFailureReturnsDatastoreError(t *testing.T) {
	reply := commitRPC(t, &validateDatastore{
		candidate: &datastore.CandidateConfig{ConfigText: "set system host-name router1\n"},
		lockInfo: &datastore.LockInfo{
			IsLocked:  true,
			SessionID: "session-1",
		},
		commitErr: datastore.NewError(datastore.ErrCodeInternal, "failed to insert commit history", errors.New("disk full")),
	}, "")

	if len(reply.Errors) != 1 {
		t.Fatalf("commit datastore errors = %d, want 1", len(reply.Errors))
	}
	err := reply.Errors[0]
	if err.ErrorTag != ErrorTagOperationFailed {
		t.Fatalf("commit datastore error tag = %s, want %s", err.ErrorTag, ErrorTagOperationFailed)
	}
	if err.ErrorAppTag != "datastore-error" {
		t.Fatalf("commit datastore app-tag = %q, want datastore-error", err.ErrorAppTag)
	}
	if err.ErrorMessage != "commit failed" {
		t.Fatalf("commit datastore message = %q, want redacted datastore failure", err.ErrorMessage)
	}
	if strings.Contains(err.ErrorMessage, "disk full") {
		t.Fatalf("commit datastore message leaked backend detail: %q", err.ErrorMessage)
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

func commitRPC(t *testing.T, ds datastore.Datastore, content string) *RPCReply {
	t.Helper()

	return validateParsedRPC(t, ds, `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
		<commit>
			`+content+`
		</commit>
	</rpc>`)
}

func assertConfirmedCommitUnsupported(t *testing.T, reply *RPCReply, element string) {
	t.Helper()

	if len(reply.Errors) != 1 {
		t.Fatalf("commit errors = %d, want 1", len(reply.Errors))
	}
	err := reply.Errors[0]
	if err.ErrorTag != ErrorTagOperationNotSupported {
		t.Fatalf("commit error tag = %s, want %s", err.ErrorTag, ErrorTagOperationNotSupported)
	}
	wantPath := "/rpc/commit/" + element
	if err.ErrorPath != wantPath {
		t.Fatalf("commit error path = %q, want %s", err.ErrorPath, wantPath)
	}
	if err.ErrorInfo == nil || err.ErrorInfo.BadElement != element {
		t.Fatalf("commit error info = %#v, want bad-element %s", err.ErrorInfo, element)
	}
}
