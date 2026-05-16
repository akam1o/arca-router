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
