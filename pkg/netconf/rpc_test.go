package netconf

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestParseRPC(t *testing.T) {
	tests := []struct {
		name    string
		xml     string
		wantErr bool
		errType string
	}{
		{
			name: "valid get-config",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
				<get-config>
					<source><running/></source>
				</get-config>
			</rpc>`,
			wantErr: false,
		},
		{
			name: "missing message-id",
			xml: `<rpc xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
				<get-config><source><running/></source></get-config>
			</rpc>`,
			wantErr: true,
			errType: "missing-element",
		},
		{
			name: "invalid namespace",
			xml: `<rpc message-id="101" xmlns="http://example.com/invalid">
				<get-config><source><running/></source></get-config>
			</rpc>`,
			wantErr: true,
			errType: "malformed-message",
		},
		{
			name:    "DTD not allowed",
			xml:     `<!DOCTYPE rpc SYSTEM "evil.dtd"><rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><get-config/></rpc>`,
			wantErr: true,
			errType: "malformed-message",
		},
		{
			name:    "ENTITY not allowed",
			xml:     `<!ENTITY xxe SYSTEM "file:///etc/passwd"><rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><get-config/></rpc>`,
			wantErr: true,
			errType: "malformed-message",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpc, err := ParseRPC([]byte(tt.xml))

			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error, got nil")
					return
				}
				rpcErr, ok := err.(*RPCError)
				if !ok {
					t.Errorf("Expected RPCError, got %T", err)
					return
				}
				if tt.errType != "" && string(rpcErr.ErrorTag) != tt.errType {
					t.Errorf("Expected error tag %s, got %s", tt.errType, rpcErr.ErrorTag)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
					return
				}
				if rpc == nil {
					t.Errorf("Expected RPC, got nil")
				}
			}
		})
	}
}

func TestRPCGetOperationName(t *testing.T) {
	xml := `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
		<get-config>
			<source><running/></source>
		</get-config>
	</rpc>`

	rpc, err := ParseRPC([]byte(xml))
	if err != nil {
		t.Fatalf("Failed to parse RPC: %v", err)
	}

	opName := rpc.GetOperationName()
	if opName != "get-config" {
		t.Errorf("Expected operation name 'get-config', got %s", opName)
	}
}

func TestSourceGetDatastore(t *testing.T) {
	tests := []struct {
		name     string
		source   Source
		expected string
		wantErr  bool
	}{
		{
			name:     "running",
			source:   Source{Running: &struct{}{}},
			expected: DatastoreRunning,
			wantErr:  false,
		},
		{
			name:     "candidate",
			source:   Source{Candidate: &struct{}{}},
			expected: DatastoreCandidate,
			wantErr:  false,
		},
		{
			name:     "none",
			source:   Source{},
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds, err := tt.source.GetDatastore()

			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
				if ds != tt.expected {
					t.Errorf("Expected datastore %s, got %s", tt.expected, ds)
				}
			}
		})
	}
}

func TestFilterValidate(t *testing.T) {
	tests := []struct {
		name    string
		filter  *Filter
		rpcName string
		wantErr bool
	}{
		{
			name:    "nil filter",
			filter:  nil,
			rpcName: "get-config",
			wantErr: false,
		},
		{
			name:    "subtree filter",
			filter:  &Filter{Type: "subtree", Content: []byte("<interfaces/>")},
			rpcName: "get-config",
			wantErr: false,
		},
		{
			name:    "xpath filter rejected",
			filter:  &Filter{Type: "xpath", Select: "/interfaces"},
			rpcName: "get-config",
			wantErr: true,
		},
		{
			name:    "predicate rejected",
			filter:  &Filter{Type: "subtree", Content: []byte("<interface[name='xe-0/0/0']/>")},
			rpcName: "get-config",
			wantErr: true,
		},
		{
			name:    "default to subtree",
			filter:  &Filter{Content: []byte("<interfaces/>")},
			rpcName: "get-config",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.filter.Validate(tt.rpcName)

			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
			}
		})
	}
}

func TestParseSizeLimit(t *testing.T) {
	// Create a large XML (> 10MB)
	largeXML := `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><get-config>`
	largeXML += strings.Repeat("<data>x</data>", 2*1024*1024) // ~20MB
	largeXML += `</get-config></rpc>`

	_, err := ParseRPC([]byte(largeXML))
	if err == nil {
		t.Errorf("Expected error for oversized RPC, got nil")
	}

	rpcErr, ok := err.(*RPCError)
	if !ok {
		t.Errorf("Expected RPCError, got %T", err)
		return
	}

	if rpcErr.ErrorTag != ErrorTagMalformedMessage {
		t.Errorf("Expected malformed-message error for size limit")
	}
}

func TestParseAndValidateRPC(t *testing.T) {
	xml := `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
		<lock><target><candidate/></target></lock>
	</rpc>`

	rpc, err := ParseAndValidateRPC([]byte(xml))
	if err != nil {
		t.Fatalf("ParseAndValidateRPC failed: %v", err)
	}

	if rpc.MessageID != "101" {
		t.Errorf("Expected message-id 101, got %s", rpc.MessageID)
	}

	if rpc.GetOperationName() != "lock" {
		t.Errorf("Expected operation lock, got %s", rpc.GetOperationName())
	}
}

func TestReadRPCFromFraming(t *testing.T) {
	// Test base:1.1 chunked framing
	rpcXML := `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><get-config><source><running/></source></get-config></rpc>`
	chunked := []byte(fmt.Sprintf("\n#%d\n%s\n##\n", len(rpcXML), rpcXML))

	reader := bytes.NewReader(chunked)
	rpc, err := ReadRPCFromFraming(reader, "1.1")
	if err != nil {
		t.Fatalf("ReadRPCFromFraming failed: %v", err)
	}

	if rpc.MessageID != "101" {
		t.Errorf("Expected message-id 101, got %s", rpc.MessageID)
	}
}
