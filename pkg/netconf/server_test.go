package netconf

import (
	"context"
	"testing"
	"time"
)

func TestKillSessionWithoutSessionManagerReturnsOperationFailed(t *testing.T) {
	srv := NewServer(nil, nil)
	sess := &Session{
		ID:             "session-1",
		NumericID:      1,
		Username:       "alice",
		Role:           RoleAdmin,
		LastUsed:       time.Now(),
		datastoreLocks: map[string]struct{}{},
	}
	rpc, err := ParseRPC([]byte(`<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
		<kill-session><session-id>2</session-id></kill-session>
	</rpc>`))
	if err != nil {
		t.Fatalf("ParseRPC() error = %v", err)
	}

	reply := srv.HandleRPC(context.Background(), sess, rpc)
	if len(reply.Errors) != 1 {
		t.Fatalf("kill-session errors = %d, want 1", len(reply.Errors))
	}
	if reply.Errors[0].ErrorTag != ErrorTagOperationFailed {
		t.Fatalf("kill-session error tag = %s, want %s", reply.Errors[0].ErrorTag, ErrorTagOperationFailed)
	}
}

func TestSessionIDToNumericWithoutSessionManagerReturnsZero(t *testing.T) {
	srv := NewServer(nil, nil)

	if got := srv.sessionIDToNumeric("missing-session"); got != 0 {
		t.Fatalf("sessionIDToNumeric() = %d, want 0", got)
	}
}

func TestDatastoreBackedRPCWithoutDatastoreReturnsOperationFailed(t *testing.T) {
	tests := []struct {
		name string
		xml  string
	}{
		{
			name: "get-config",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
				<get-config><source><running/></source></get-config>
			</rpc>`,
		},
		{
			name: "edit-config",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
				<edit-config>
					<target><candidate/></target>
					<config><system><host-name>router1</host-name></system></config>
				</edit-config>
			</rpc>`,
		},
		{
			name: "lock",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
				<lock><target><candidate/></target></lock>
			</rpc>`,
		},
		{
			name: "validate-running",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
				<validate><source><running/></source></validate>
			</rpc>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewServer(nil, nil)
			sess := &Session{
				ID:             "session-1",
				NumericID:      1,
				Username:       "alice",
				Role:           RoleOperator,
				LastUsed:       time.Now(),
				datastoreLocks: map[string]struct{}{},
			}
			rpc, err := ParseRPC([]byte(tt.xml))
			if err != nil {
				t.Fatalf("ParseRPC() error = %v", err)
			}

			reply := srv.HandleRPC(context.Background(), sess, rpc)
			if len(reply.Errors) != 1 {
				t.Fatalf("%s errors = %d, want 1", tt.name, len(reply.Errors))
			}
			if reply.Errors[0].ErrorTag != ErrorTagOperationFailed {
				t.Fatalf("%s error tag = %s, want %s", tt.name, reply.Errors[0].ErrorTag, ErrorTagOperationFailed)
			}
		})
	}
}

func TestHandleRPCWithoutSessionReturnsOperationFailed(t *testing.T) {
	srv := NewServer(nil, nil)
	rpc, err := ParseRPC([]byte(`<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
		<get/>
	</rpc>`))
	if err != nil {
		t.Fatalf("ParseRPC() error = %v", err)
	}

	reply := srv.HandleRPC(context.Background(), nil, rpc)
	if len(reply.Errors) != 1 {
		t.Fatalf("nil session errors = %d, want 1", len(reply.Errors))
	}
	if reply.Errors[0].ErrorTag != ErrorTagOperationFailed {
		t.Fatalf("nil session error tag = %s, want %s", reply.Errors[0].ErrorTag, ErrorTagOperationFailed)
	}
}

func TestHandleRPCWithoutServerReturnsOperationFailed(t *testing.T) {
	var srv *Server
	sess := &Session{
		ID:             "session-1",
		NumericID:      1,
		Username:       "alice",
		Role:           RoleOperator,
		LastUsed:       time.Now(),
		datastoreLocks: map[string]struct{}{},
	}
	rpc, err := ParseRPC([]byte(`<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
		<get/>
	</rpc>`))
	if err != nil {
		t.Fatalf("ParseRPC() error = %v", err)
	}

	reply := srv.HandleRPC(context.Background(), sess, rpc)
	if len(reply.Errors) != 1 {
		t.Fatalf("nil server errors = %d, want 1", len(reply.Errors))
	}
	if reply.Errors[0].ErrorTag != ErrorTagOperationFailed {
		t.Fatalf("nil server error tag = %s, want %s", reply.Errors[0].ErrorTag, ErrorTagOperationFailed)
	}
}

func TestHandleRPCWithoutRPCReturnsOperationFailed(t *testing.T) {
	srv := NewServer(nil, nil)
	sess := &Session{
		ID:             "session-1",
		NumericID:      1,
		Username:       "alice",
		Role:           RoleOperator,
		LastUsed:       time.Now(),
		datastoreLocks: map[string]struct{}{},
	}

	reply := srv.HandleRPC(context.Background(), sess, nil)
	if len(reply.Errors) != 1 {
		t.Fatalf("nil rpc errors = %d, want 1", len(reply.Errors))
	}
	if reply.Errors[0].ErrorTag != ErrorTagOperationFailed {
		t.Fatalf("nil rpc error tag = %s, want %s", reply.Errors[0].ErrorTag, ErrorTagOperationFailed)
	}
}

func TestHandleRPCWithZeroValueRPCReturnsOperationFailed(t *testing.T) {
	srv := NewServer(nil, nil)
	sess := &Session{
		ID:             "session-1",
		NumericID:      1,
		Username:       "alice",
		Role:           RoleOperator,
		LastUsed:       time.Now(),
		datastoreLocks: map[string]struct{}{},
	}

	reply := srv.HandleRPC(context.Background(), sess, &RPC{MessageID: "101"})
	if len(reply.Errors) != 1 {
		t.Fatalf("zero-value rpc errors = %d, want 1", len(reply.Errors))
	}
	if reply.Errors[0].ErrorTag != ErrorTagOperationFailed {
		t.Fatalf("zero-value rpc error tag = %s, want %s", reply.Errors[0].ErrorTag, ErrorTagOperationFailed)
	}
	if reply.MessageID != "101" {
		t.Fatalf("zero-value rpc reply message-id = %q, want 101", reply.MessageID)
	}
}

func TestValidateInlineSourceWithoutDatastoreSucceeds(t *testing.T) {
	srv := NewServer(nil, nil)
	sess := &Session{
		ID:             "session-1",
		NumericID:      1,
		Username:       "alice",
		Role:           RoleOperator,
		LastUsed:       time.Now(),
		datastoreLocks: map[string]struct{}{},
	}
	rpc, err := ParseRPC([]byte(`<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
		<validate><source><config><system><host-name>router1</host-name></system></config></source></validate>
	</rpc>`))
	if err != nil {
		t.Fatalf("ParseRPC() error = %v", err)
	}

	reply := srv.HandleRPC(context.Background(), sess, rpc)
	if len(reply.Errors) != 0 {
		t.Fatalf("validate inline errors = %#v, want none", reply.Errors)
	}
	if reply.OK == nil {
		t.Fatal("validate inline OK = nil, want ok")
	}
}

func TestServerHookSettersNilReceiver(t *testing.T) {
	var srv *Server

	srv.SetCommitHook(nil)
	srv.SetOperationalStateProvider(nil)
}
