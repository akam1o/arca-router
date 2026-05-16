package netconf

import (
	"context"
	"strings"
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

func TestEditConfigTestOnlyValidatesWithoutSavingCandidate(t *testing.T) {
	ds := &copyConfigDatastore{
		candidate: &datastore.CandidateConfig{ConfigText: "set system host-name old-router\n"},
		lockInfo: &datastore.LockInfo{
			IsLocked:  true,
			SessionID: "session-1",
		},
	}

	reply := editConfigRPC(t, ds, "test-only", "<config><system><host-name>router1</host-name></system></config>")
	if len(reply.Errors) != 0 {
		t.Fatalf("edit-config test-only errors = %#v, want none", reply.Errors)
	}
	if reply.OK == nil {
		t.Fatal("edit-config test-only OK = nil, want ok")
	}
	if ds.saveCalled {
		t.Fatal("edit-config test-only saved candidate")
	}
}

func TestEditConfigTestOnlyReturnsSemanticError(t *testing.T) {
	ds := &copyConfigDatastore{
		candidate: &datastore.CandidateConfig{ConfigText: "set system host-name old-router\n"},
		lockInfo: &datastore.LockInfo{
			IsLocked:  true,
			SessionID: "session-1",
		},
	}

	reply := editConfigRPC(t, ds, "test-only", "<config><system><host-name>bad_name</host-name></system></config>")
	if len(reply.Errors) != 1 {
		t.Fatalf("edit-config test-only errors = %d, want 1", len(reply.Errors))
	}
	err := reply.Errors[0]
	if err.ErrorTag != ErrorTagInvalidValue {
		t.Fatalf("edit-config test-only error tag = %s, want %s", err.ErrorTag, ErrorTagInvalidValue)
	}
	if err.ErrorPath != "/rpc/edit-config/config" {
		t.Fatalf("edit-config test-only error path = %q, want /rpc/edit-config/config", err.ErrorPath)
	}
	if ds.saveCalled {
		t.Fatal("edit-config test-only saved invalid candidate")
	}
}

func TestEditConfigTestThenSetSavesCandidate(t *testing.T) {
	ds := &copyConfigDatastore{
		candidate: &datastore.CandidateConfig{ConfigText: "set system host-name old-router\n"},
		lockInfo: &datastore.LockInfo{
			IsLocked:  true,
			SessionID: "session-1",
		},
	}

	reply := editConfigRPC(t, ds, "test-then-set", "<config><system><host-name>router1</host-name></system></config>")
	if len(reply.Errors) != 0 {
		t.Fatalf("edit-config test-then-set errors = %#v, want none", reply.Errors)
	}
	if reply.OK == nil {
		t.Fatal("edit-config test-then-set OK = nil, want ok")
	}
	if !ds.saveCalled {
		t.Fatal("edit-config test-then-set did not save candidate")
	}
	if ds.savedText != "set system host-name router1\n" {
		t.Fatalf("saved candidate = %q, want test-then-set edit", ds.savedText)
	}
}

func TestEditConfigDefaultOperationReplaceSavesReplacedSubtree(t *testing.T) {
	ds := &copyConfigDatastore{
		candidate: &datastore.CandidateConfig{ConfigText: strings.Join([]string{
			"set system host-name old-router",
			"set system services web-ui enabled true",
			"set routing-options router-id 192.0.2.1",
			"",
		}, "\n")},
		lockInfo: &datastore.LockInfo{
			IsLocked:  true,
			SessionID: "session-1",
		},
	}

	reply := editConfigRPCWithDefaultOperation(t, ds, "replace", "<config><system><host-name>router1</host-name></system></config>")
	if len(reply.Errors) != 0 {
		t.Fatalf("edit-config replace errors = %#v, want none", reply.Errors)
	}
	if reply.OK == nil {
		t.Fatal("edit-config replace OK = nil, want ok")
	}
	if !ds.saveCalled {
		t.Fatal("edit-config replace did not save candidate")
	}
	for _, want := range []string{
		"set system host-name router1",
		"set routing-options router-id 192.0.2.1",
	} {
		if !strings.Contains(ds.savedText, want) {
			t.Fatalf("saved candidate missing %q:\n%s", want, ds.savedText)
		}
	}
	for _, unwanted := range []string{
		"old-router",
		"set system services web-ui enabled true",
	} {
		if strings.Contains(ds.savedText, unwanted) {
			t.Fatalf("saved candidate contains %q after replace:\n%s", unwanted, ds.savedText)
		}
	}
}

func TestEditConfigDefaultOperationNoneStillRejected(t *testing.T) {
	ds := &copyConfigDatastore{
		candidate: &datastore.CandidateConfig{ConfigText: "set system host-name old-router\n"},
		lockInfo: &datastore.LockInfo{
			IsLocked:  true,
			SessionID: "session-1",
		},
	}

	reply := editConfigRPCWithDefaultOperation(t, ds, "none", "<config><system><host-name>router1</host-name></system></config>")
	if len(reply.Errors) != 1 {
		t.Fatalf("edit-config default-operation none errors = %d, want 1", len(reply.Errors))
	}
	err := reply.Errors[0]
	if err.ErrorTag != ErrorTagOperationNotSupported {
		t.Fatalf("edit-config default-operation none error tag = %s, want %s", err.ErrorTag, ErrorTagOperationNotSupported)
	}
	if err.ErrorPath != "/rpc/edit-config/default-operation" {
		t.Fatalf("edit-config default-operation none error path = %q, want /rpc/edit-config/default-operation", err.ErrorPath)
	}
	if ds.saveCalled {
		t.Fatal("edit-config default-operation none saved candidate")
	}
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

func TestCopyConfigSavesInlineConfigSource(t *testing.T) {
	ds := &copyConfigDatastore{
		lockInfo: &datastore.LockInfo{
			IsLocked:  true,
			SessionID: "session-1",
		},
	}

	reply := copyConfigRPC(t, ds, "<source><config><system><host-name>router1</host-name></system></config></source>")
	if len(reply.Errors) != 0 {
		t.Fatalf("copy-config inline source errors = %#v, want none", reply.Errors)
	}
	if reply.OK == nil {
		t.Fatal("copy-config inline source OK = nil, want ok")
	}
	if !ds.saveCalled {
		t.Fatal("copy-config inline source did not save candidate")
	}
	if ds.savedText != "set system host-name router1\n" {
		t.Fatalf("saved candidate = %q, want host-name config", ds.savedText)
	}
}

func TestCopyConfigInlineSourcePreservesAncestorNamespaceDeclarations(t *testing.T) {
	ds := &copyConfigDatastore{
		lockInfo: &datastore.LockInfo{
			IsLocked:  true,
			SessionID: "session-1",
		},
	}

	reply := copyConfigParsedRPC(t, ds, `<rpc message-id="101"
		xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"
		xmlns:arca="`+ArcaConfigNS+`">
		<copy-config>
			<target><candidate/></target>
			<source><config><arca:system><arca:host-name>router1</arca:host-name></arca:system></config></source>
		</copy-config>
	</rpc>`)
	if len(reply.Errors) != 0 {
		t.Fatalf("copy-config namespace-prefixed inline source errors = %#v, want none", reply.Errors)
	}
	if ds.savedText != "set system host-name router1\n" {
		t.Fatalf("saved candidate = %q, want namespace-prefixed host-name config", ds.savedText)
	}
}

func TestCopyConfigValidatesInlineConfigSourceBeforeSavingCandidate(t *testing.T) {
	ds := &copyConfigDatastore{
		lockInfo: &datastore.LockInfo{
			IsLocked:  true,
			SessionID: "session-1",
		},
	}

	reply := copyConfigRPC(t, ds, "<source><config><system><host-name>bad_name</host-name></system></config></source>")
	if len(reply.Errors) != 1 {
		t.Fatalf("copy-config inline source errors = %d, want 1", len(reply.Errors))
	}
	err := reply.Errors[0]
	if err.ErrorTag != ErrorTagInvalidValue {
		t.Fatalf("copy-config inline source error tag = %s, want %s", err.ErrorTag, ErrorTagInvalidValue)
	}
	if err.ErrorPath != "/rpc/copy-config/source" {
		t.Fatalf("copy-config inline source error path = %q, want /rpc/copy-config/source", err.ErrorPath)
	}
	if ds.saveCalled {
		t.Fatal("copy-config saved invalid inline source config")
	}
}

func TestParseRPCRejectsCopyConfigMultipleSourceChoices(t *testing.T) {
	_, err := ParseRPC([]byte(`<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
		<copy-config>
			<target><candidate/></target>
			<source><running/><config><system><host-name>router1</host-name></system></config></source>
		</copy-config>
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

func TestParseRPCAcceptsCopyConfigInlineSource(t *testing.T) {
	if _, err := ParseRPC([]byte(`<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
		<copy-config>
			<target><candidate/></target>
			<source><config><system><host-name>router1</host-name></system></config></source>
		</copy-config>
	</rpc>`)); err != nil {
		t.Fatalf("ParseRPC() inline copy-config source error = %v", err)
	}
}

func copyConfigRPC(t *testing.T, ds datastore.Datastore, source string) *RPCReply {
	t.Helper()

	return copyConfigParsedRPC(t, ds, `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
		<copy-config>
			<target><candidate/></target>
			`+source+`
		</copy-config>
	</rpc>`)
}

func copyConfigParsedRPC(t *testing.T, ds datastore.Datastore, rpcXML string) *RPCReply {
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

func editConfigRPC(t *testing.T, ds datastore.Datastore, testOption string, configXML string) *RPCReply {
	t.Helper()

	return editConfigRPCWithOptions(t, ds, testOption, "", configXML)
}

func editConfigRPCWithDefaultOperation(t *testing.T, ds datastore.Datastore, defaultOperation string, configXML string) *RPCReply {
	t.Helper()

	return editConfigRPCWithOptions(t, ds, "", defaultOperation, configXML)
}

func editConfigRPCWithOptions(t *testing.T, ds datastore.Datastore, testOption string, defaultOperation string, configXML string) *RPCReply {
	t.Helper()

	testOptionXML := ""
	if testOption != "" {
		testOptionXML = "<test-option>" + testOption + "</test-option>"
	}
	defaultOperationXML := ""
	if defaultOperation != "" {
		defaultOperationXML = "<default-operation>" + defaultOperation + "</default-operation>"
	}
	return copyConfigParsedRPC(t, ds, `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
		<edit-config>
			<target><candidate/></target>
			`+defaultOperationXML+`
			`+testOptionXML+`
			`+configXML+`
		</edit-config>
	</rpc>`)
}
