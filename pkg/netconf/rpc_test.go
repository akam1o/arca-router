package netconf

import (
	"bytes"
	"encoding/xml"
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
			errType: "missing-attribute",
		},
		{
			name: "invalid namespace",
			xml: `<rpc message-id="101" xmlns="http://example.com/invalid">
					<get-config><source><running/></source></get-config>
				</rpc>`,
			wantErr: true,
			errType: "unknown-namespace",
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
		{
			name: "trailing rpc not allowed",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
					<get-config><source><running/></source></get-config>
				</rpc><rpc message-id="102" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><close-session/></rpc>`,
			wantErr: true,
			errType: "malformed-message",
		},
		{
			name: "rpc root text before operation rejected",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
					junk
					<get-config><source><running/></source></get-config>
				</rpc>`,
			wantErr: true,
			errType: "malformed-message",
		},
		{
			name: "rpc root text after operation rejected",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
					<get-config><source><running/></source></get-config>
					junk
				</rpc>`,
			wantErr: true,
			errType: "malformed-message",
		},
		{
			name: "multiple operations not allowed",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
					<get-config><source><running/></source></get-config>
					<kill-session><session-id>1</session-id></kill-session>
				</rpc>`,
			wantErr: true,
			errType: "malformed-message",
		},
		{
			name: "operation attribute rejected",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
					<get-config foo="bar"><source><running/></source></get-config>
				</rpc>`,
			wantErr: true,
			errType: "unknown-attribute",
		},
		{
			name: "rpc root additional attribute accepted",
			xml: `<rpc message-id="101" foo="bar" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
						<get-config><source><running/></source></get-config>
					</rpc>`,
			wantErr: false,
		},
		{
			name: "operation empty namespace rejected",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
					<get-config xmlns=""><source><running/></source></get-config>
				</rpc>`,
			wantErr: true,
			errType: "unknown-namespace",
		},
		{
			name: "operation text rejected",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
					<close-session>junk</close-session>
				</rpc>`,
			wantErr: true,
			errType: "malformed-message",
		},
		{
			name: "unknown operation child rejected",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
					<get-config><source><running/></source><unexpected/></get-config>
				</rpc>`,
			wantErr: true,
			errType: "unknown-element",
		},
		{
			name: "filter attribute rejected",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
					<get-config><source><running/></source><filter foo="bar"><interfaces/></filter></get-config>
				</rpc>`,
			wantErr: true,
			errType: "unknown-attribute",
		},
		{
			name: "filter empty namespace rejected",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
					<get-config><source><running/></source><filter xmlns=""><interfaces/></filter></get-config>
				</rpc>`,
			wantErr: true,
			errType: "unknown-namespace",
		},
		{
			name: "missing required operation child rejected",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
					<get-config/>
				</rpc>`,
			wantErr: true,
			errType: "missing-element",
		},
		{
			name: "duplicate operation child rejected",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
					<get-config><source><running/></source><source><candidate/></source></get-config>
				</rpc>`,
			wantErr: true,
			errType: "malformed-message",
		},
		{
			name: "multiple datastore choices rejected",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
					<get-config><source><running/><candidate/></source></get-config>
				</rpc>`,
			wantErr: true,
			errType: "malformed-message",
		},
		{
			name: "datastore choice text rejected",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
					<get-config><source>running<running/></source></get-config>
				</rpc>`,
			wantErr: true,
			errType: "malformed-message",
		},
		{
			name: "valid kill-session text leaf",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
					<kill-session><session-id>1</session-id></kill-session>
				</rpc>`,
			wantErr: false,
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

func TestParseRPCContentIsOperationInnerXML(t *testing.T) {
	xmlData := `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
		<get-config>
			<source><running/></source>
		</get-config>
	</rpc>`

	rpc, err := ParseRPC([]byte(xmlData))
	if err != nil {
		t.Fatalf("ParseRPC() error = %v", err)
	}

	var req GetConfigRequest
	if err := rpc.UnmarshalOperation(&req); err != nil {
		t.Fatalf("UnmarshalOperation() error = %v", err)
	}
	if req.Source.Running == nil {
		t.Fatalf("UnmarshalOperation() source = %#v, want running", req.Source)
	}
}

func TestParseRPCPreservesReplyAttributes(t *testing.T) {
	xmlData := `<rpc message-id="101"
		xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"
		xmlns:ex="http://example.net/content/1.0"
		ex:user-id="fred"
		trace-id="abc">
		<get/>
	</rpc>`

	rpc, err := ParseRPC([]byte(xmlData))
	if err != nil {
		t.Fatalf("ParseRPC() error = %v", err)
	}

	reply := NewOKReply(rpc.MessageID).WithAttributes(rpc.ReplyAttrs)
	data, err := MarshalReply(reply)
	if err != nil {
		t.Fatalf("MarshalReply() error = %v", err)
	}

	xmlStr := string(data)
	for _, want := range []string{
		`xmlns:ex="http://example.net/content/1.0"`,
		`ex:user-id="fred"`,
		`trace-id="abc"`,
	} {
		if !strings.Contains(xmlStr, want) {
			t.Fatalf("MarshalReply() missing %s in %s", want, xmlStr)
		}
	}
}

func TestExtractRPCReplyContextFromMalformedRPC(t *testing.T) {
	xmlData := `<rpc message-id="101"
		xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"
		xmlns:ex="http://example.net/content/1.0"
		ex:user-id="fred">
		<get/>
		trailing text
	</rpc>`

	if _, err := ParseRPC([]byte(xmlData)); err == nil {
		t.Fatal("ParseRPC() error = nil, want malformed RPC error")
	}

	messageID, attrs := extractRPCReplyContext([]byte(xmlData))
	if messageID != "101" {
		t.Fatalf("messageID = %q, want 101", messageID)
	}

	reply := NewErrorReply(messageID, ErrMalformedMessage("bad rpc")).WithAttributes(attrs)
	data, err := MarshalReply(reply)
	if err != nil {
		t.Fatalf("MarshalReply() error = %v", err)
	}

	xmlStr := string(data)
	for _, want := range []string{
		`message-id="101"`,
		`xmlns:ex="http://example.net/content/1.0"`,
		`ex:user-id="fred"`,
	} {
		if !strings.Contains(xmlStr, want) {
			t.Fatalf("MarshalReply() missing %s in %s", want, xmlStr)
		}
	}
}

func TestUnmarshalOperationPreservesAncestorNamespaceDeclarations(t *testing.T) {
	tests := []struct {
		name string
		xml  string
	}{
		{
			name: "rpc namespace declaration",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" xmlns:arca="urn:arca:router:config:1.0">
				<edit-config>
					<target><candidate/></target>
					<config>
						<arca:system><arca:host-name>router1</arca:host-name></arca:system>
					</config>
				</edit-config>
			</rpc>`,
		},
		{
			name: "operation namespace declaration",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
				<edit-config xmlns:arca="urn:arca:router:config:1.0">
					<target><candidate/></target>
					<config>
						<arca:system><arca:host-name>router1</arca:host-name></arca:system>
					</config>
				</edit-config>
			</rpc>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpc, err := ParseRPC([]byte(tt.xml))
			if err != nil {
				t.Fatalf("ParseRPC() error = %v", err)
			}

			var req EditConfigRequest
			if err := rpc.UnmarshalOperation(&req); err != nil {
				t.Fatalf("UnmarshalOperation() error = %v", err)
			}

			configXML, err := req.Config.XML()
			if err != nil {
				t.Fatalf("Config.XML() error = %v", err)
			}
			cfg, err := XMLToConfig(configXML, DefaultOpMerge)
			if err != nil {
				t.Fatalf("XMLToConfig() error = %v\nXML:\n%s", err, configXML)
			}
			if cfg.System == nil || cfg.System.HostName != "router1" {
				t.Fatalf("XMLToConfig() system = %#v, want host-name router1", cfg.System)
			}
		})
	}
}

func TestConfigElementXMLRejectsUndeclaredAttributeNamespace(t *testing.T) {
	config := ConfigElement{
		XMLName: xml.Name{Local: "config"},
		Attrs: []xml.Attr{
			{Name: xml.Name{Space: "urn:example:attrs", Local: "mode"}, Value: "replace"},
		},
		Content: []byte(`<system><host-name>router1</host-name></system>`),
	}

	_, err := config.XML()
	if err == nil {
		t.Fatal("Config.XML() error = nil, want undeclared namespace error")
	}
	rpcErr, ok := err.(*RPCError)
	if !ok {
		t.Fatalf("Config.XML() error = %T, want *RPCError", err)
	}
	if rpcErr.ErrorTag != ErrorTagUnknownNamespace {
		t.Fatalf("Config.XML() error tag = %s, want %s", rpcErr.ErrorTag, ErrorTagUnknownNamespace)
	}
	if rpcErr.ErrorInfo == nil || rpcErr.ErrorInfo.BadNamespace != "urn:example:attrs" {
		t.Fatalf("Config.XML() error info = %#v, want bad namespace", rpcErr.ErrorInfo)
	}
}

func TestInheritedNamespaceReceiversNilSafe(t *testing.T) {
	attrs := []xml.Attr{
		{Name: xml.Name{Space: "xmlns", Local: "arca"}, Value: ArcaConfigNS},
	}

	tests := []struct {
		name string
		set  func()
	}{
		{
			name: "get",
			set: func() {
				var req *GetRequest
				req.SetInheritedNamespaceAttrs(attrs)
			},
		},
		{
			name: "get-config",
			set: func() {
				var req *GetConfigRequest
				req.SetInheritedNamespaceAttrs(attrs)
			},
		},
		{
			name: "edit-config",
			set: func() {
				var req *EditConfigRequest
				req.SetInheritedNamespaceAttrs(attrs)
			},
		},
		{
			name: "copy-config",
			set: func() {
				var req *CopyConfigRequest
				req.SetInheritedNamespaceAttrs(attrs)
			},
		},
		{
			name: "validate",
			set: func() {
				var req *ValidateRequest
				req.SetInheritedNamespaceAttrs(attrs)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.set()
		})
	}
}

func TestUnmarshalOperationPreservesFilterNamespaceDeclarations(t *testing.T) {
	tests := []struct {
		name string
		xml  string
	}{
		{
			name: "rpc namespace declaration",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" xmlns:if="urn:ietf:params:xml:ns:yang:ietf-interfaces">
				<get-config>
					<source><running/></source>
					<filter><if:interfaces/></filter>
				</get-config>
			</rpc>`,
		},
		{
			name: "filter namespace declaration",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
				<get-config>
					<source><running/></source>
					<filter xmlns:if="urn:ietf:params:xml:ns:yang:ietf-interfaces"><if:interfaces/></filter>
				</get-config>
			</rpc>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpc, err := ParseRPC([]byte(tt.xml))
			if err != nil {
				t.Fatalf("ParseRPC() error = %v", err)
			}

			var req GetConfigRequest
			if err := rpc.UnmarshalOperation(&req); err != nil {
				t.Fatalf("UnmarshalOperation() error = %v", err)
			}
			if req.Filter == nil {
				t.Fatal("UnmarshalOperation() filter = nil")
			}
			if !filterMatches(req.Filter, "interfaces") {
				t.Fatalf("filterMatches() = false, want true for namespace-prefixed interfaces filter")
			}
		})
	}
}

func TestUnmarshalOperationPreservesXPathFilterNamespaceDeclarations(t *testing.T) {
	tests := []struct {
		name string
		xml  string
	}{
		{
			name: "rpc namespace declaration",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" xmlns:if="urn:ietf:params:xml:ns:yang:ietf-interfaces">
				<get-config>
					<source><running/></source>
					<filter type="xpath" select="/if:interfaces/if:interface[if:name='ge-0/0/0']"/>
				</get-config>
			</rpc>`,
		},
		{
			name: "filter namespace declaration",
			xml: `<rpc message-id="101" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
				<get-config>
					<source><running/></source>
					<filter type="xpath" select="/if:interfaces/if:interface[if:name='ge-0/0/0']" xmlns:if="urn:ietf:params:xml:ns:yang:ietf-interfaces"/>
				</get-config>
			</rpc>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpc, err := ParseRPC([]byte(tt.xml))
			if err != nil {
				t.Fatalf("ParseRPC() error = %v", err)
			}

			var req GetConfigRequest
			if err := rpc.UnmarshalOperation(&req); err != nil {
				t.Fatalf("UnmarshalOperation() error = %v", err)
			}
			if err := req.Filter.Validate("get-config"); err != nil {
				t.Fatalf("Filter.Validate() error = %v", err)
			}
			if !filterMatches(req.Filter, "interfaces") {
				t.Fatalf("filterMatches() = false, want true for namespace-prefixed xpath filter")
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

func TestRPCAccessorsNilReceiver(t *testing.T) {
	var rpc *RPC

	if got := rpc.GetOperationName(); got != "" {
		t.Fatalf("GetOperationName() = %q, want empty", got)
	}
	if got := rpc.GetOperationNamespace(); got != "" {
		t.Fatalf("GetOperationNamespace() = %q, want empty", got)
	}
	var req GetConfigRequest
	err := rpc.UnmarshalOperation(&req)
	if err == nil {
		t.Fatal("UnmarshalOperation() error = nil, want rpc unavailable")
	}
	if rpcErr, ok := err.(*RPCError); !ok || rpcErr.ErrorTag != ErrorTagOperationFailed {
		t.Fatalf("UnmarshalOperation() error = %#v, want operation-failed RPCError", err)
	}
	err = rpc.ValidateOperationNamespace()
	if err == nil {
		t.Fatal("ValidateOperationNamespace() error = nil, want rpc unavailable")
	}
	if rpcErr, ok := err.(*RPCError); !ok || rpcErr.ErrorTag != ErrorTagOperationFailed {
		t.Fatalf("ValidateOperationNamespace() error = %#v, want operation-failed RPCError", err)
	}
}

func TestRPCZeroValueOperationAccessors(t *testing.T) {
	rpc := &RPC{}

	var req GetConfigRequest
	err := rpc.UnmarshalOperation(&req)
	if err == nil {
		t.Fatal("UnmarshalOperation() error = nil, want rpc operation unavailable")
	}
	if rpcErr, ok := err.(*RPCError); !ok || rpcErr.ErrorTag != ErrorTagOperationFailed {
		t.Fatalf("UnmarshalOperation() error = %#v, want operation-failed RPCError", err)
	}

	err = rpc.ValidateOperationNamespace()
	if err == nil {
		t.Fatal("ValidateOperationNamespace() error = nil, want rpc operation unavailable")
	}
	if rpcErr, ok := err.(*RPCError); !ok || rpcErr.ErrorTag != ErrorTagOperationFailed {
		t.Fatalf("ValidateOperationNamespace() error = %#v, want operation-failed RPCError", err)
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
		{
			name:     "multiple datastores",
			source:   Source{Running: &struct{}{}, Candidate: &struct{}{}},
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

func TestSourceGetDatastoreNilReceiver(t *testing.T) {
	var source *Source

	ds, err := source.GetDatastore()
	if err == nil {
		t.Fatal("GetDatastore() error = nil, want missing datastore")
	}
	if ds != "" {
		t.Fatalf("GetDatastore() datastore = %q, want empty", ds)
	}
	if rpcErr, ok := err.(*RPCError); !ok || rpcErr.ErrorTag != ErrorTagMissingElement {
		t.Fatalf("GetDatastore() error = %#v, want missing-element RPCError", err)
	}
}

func TestTargetGetDatastore(t *testing.T) {
	tests := []struct {
		name     string
		target   Target
		expected string
		wantErr  bool
	}{
		{
			name:     "candidate",
			target:   Target{Candidate: &struct{}{}},
			expected: DatastoreCandidate,
			wantErr:  false,
		},
		{
			name:     "none",
			target:   Target{},
			expected: "",
			wantErr:  true,
		},
		{
			name:     "multiple datastores",
			target:   Target{Running: &struct{}{}, Candidate: &struct{}{}},
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds, err := tt.target.GetDatastore()

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

func TestTargetGetDatastoreNilReceiver(t *testing.T) {
	var target *Target

	ds, err := target.GetDatastore()
	if err == nil {
		t.Fatal("GetDatastore() error = nil, want missing datastore")
	}
	if ds != "" {
		t.Fatalf("GetDatastore() datastore = %q, want empty", ds)
	}
	if rpcErr, ok := err.(*RPCError); !ok || rpcErr.ErrorTag != ErrorTagMissingElement {
		t.Fatalf("GetDatastore() error = %#v, want missing-element RPCError", err)
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
			name: "subtree filter namespace prefix on filter",
			filter: &Filter{
				Type:    "subtree",
				Content: []byte(`<if:interfaces/>`),
				Attrs: []xml.Attr{
					{Name: xml.Name{Space: "xmlns", Local: "if"}, Value: IETFInterfacesNS},
				},
			},
			rpcName: "get-config",
			wantErr: false,
		},
		{
			name: "subtree filter nested model path",
			filter: &Filter{
				Type:    "subtree",
				Content: []byte(`<interfaces><interface><name/></interface></interfaces>`),
			},
			rpcName: "get-config",
			wantErr: false,
		},
		{
			name: "subtree filter nested namespace prefix",
			filter: &Filter{
				Type:    "subtree",
				Content: []byte(`<if:interfaces><if:interface><if:name/></if:interface></if:interfaces>`),
				Attrs: []xml.Attr{
					{Name: xml.Name{Space: "xmlns", Local: "if"}, Value: IETFInterfacesNS},
				},
			},
			rpcName: "get-config",
			wantErr: false,
		},
		{
			name: "subtree filter namespace prefix mismatch",
			filter: &Filter{
				Type:    "subtree",
				Content: []byte(`<rt:interfaces/>`),
				Attrs: []xml.Attr{
					{Name: xml.Name{Space: "xmlns", Local: "rt"}, Value: IETFRoutingNS},
				},
			},
			rpcName: "get-config",
			wantErr: true,
		},
		{
			name: "subtree filter rejects unknown top-level element",
			filter: &Filter{
				Type:    "subtree",
				Content: []byte(`<unknown/>`),
			},
			rpcName: "get-config",
			wantErr: true,
		},
		{
			name: "subtree filter rejects unknown nested element",
			filter: &Filter{
				Type:    "subtree",
				Content: []byte(`<interfaces><interface><unknown/></interface></interfaces>`),
			},
			rpcName: "get-config",
			wantErr: true,
		},
		{
			name: "subtree filter rejects nested namespace prefix mismatch",
			filter: &Filter{
				Type:    "subtree",
				Content: []byte(`<if:interfaces><rt:interface/></if:interfaces>`),
				Attrs: []xml.Attr{
					{Name: xml.Name{Space: "xmlns", Local: "if"}, Value: IETFInterfacesNS},
					{Name: xml.Name{Space: "xmlns", Local: "rt"}, Value: IETFRoutingNS},
				},
			},
			rpcName: "get-config",
			wantErr: true,
		},
		{
			name:    "xpath filter",
			filter:  &Filter{Type: "xpath", Select: "/interfaces"},
			rpcName: "get-config",
			wantErr: false,
		},
		{
			name:    "xpath filter trims type",
			filter:  &Filter{Type: "\n xpath \t", Select: "/interfaces"},
			rpcName: "get-config",
			wantErr: false,
		},
		{
			name:    "xpath filter nested model path",
			filter:  &Filter{Type: "xpath", Select: "/protocols/bgp/group/neighbor/peer-as"},
			rpcName: "get-config",
			wantErr: false,
		},
		{
			name: "xpath filter namespace prefix on filter",
			filter: &Filter{
				Type:   "xpath",
				Select: "/if:interfaces/if:interface[if:name='ge-0/0/0']",
				Attrs: []xml.Attr{
					{Name: xml.Name{Space: "xmlns", Local: "if"}, Value: IETFInterfacesNS},
				},
			},
			rpcName: "get-config",
			wantErr: false,
		},
		{
			name: "xpath filter namespace prefix inherited from rpc",
			filter: &Filter{
				Type:   "xpath",
				Select: "/arca:protocols/arca:bgp/arca:group/arca:neighbor",
				InheritedAttrs: []xml.Attr{
					{Name: xml.Name{Space: "xmlns", Local: "arca"}, Value: ArcaConfigNS},
				},
			},
			rpcName: "get-config",
			wantErr: false,
		},
		{
			name: "xpath filter routing namespace prefix",
			filter: &Filter{
				Type:   "xpath",
				Select: "/rt:routing/rt:static-routes/rt:route[rt:prefix='10.0.0.0/24']",
				Attrs: []xml.Attr{
					{Name: xml.Name{Space: "xmlns", Local: "rt"}, Value: IETFRoutingNS},
				},
			},
			rpcName: "get-config",
			wantErr: false,
		},
		{
			name:    "xpath filter accepts multiple predicates",
			filter:  &Filter{Type: "xpath", Select: "/state/routes/route[prefix='192.0.2.0/24'][protocol='static']"},
			rpcName: "get",
			wantErr: false,
		},
		{
			name:    "xpath filter requires select",
			filter:  &Filter{Type: "xpath"},
			rpcName: "get-config",
			wantErr: true,
		},
		{
			name:    "xpath filter rejects relative select",
			filter:  &Filter{Type: "xpath", Select: "interfaces"},
			rpcName: "get-config",
			wantErr: true,
		},
		{
			name:    "xpath filter rejects unknown model path",
			filter:  &Filter{Type: "xpath", Select: "/interfaces/interface/unknown"},
			rpcName: "get-config",
			wantErr: true,
		},
		{
			name:    "xpath filter rejects unknown predicate key",
			filter:  &Filter{Type: "xpath", Select: "/interfaces/interface[foo='bar']"},
			rpcName: "get-config",
			wantErr: true,
		},
		{
			name:    "xpath filter rejects undeclared namespace prefix",
			filter:  &Filter{Type: "xpath", Select: "/if:interfaces"},
			rpcName: "get-config",
			wantErr: true,
		},
		{
			name: "xpath filter rejects namespace prefix mismatch",
			filter: &Filter{
				Type:   "xpath",
				Select: "/rt:interfaces",
				Attrs: []xml.Attr{
					{Name: xml.Name{Space: "xmlns", Local: "rt"}, Value: IETFRoutingNS},
				},
			},
			rpcName: "get-config",
			wantErr: true,
		},
		{
			name:    "xpath filter rejects subtree content",
			filter:  &Filter{Type: "xpath", Select: "/interfaces", Content: []byte("<interfaces/>")},
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
		{
			name:    "subtree filter trims type",
			filter:  &Filter{Type: "\n subtree \t", Content: []byte("<interfaces/>")},
			rpcName: "get-config",
			wantErr: false,
		},
		{
			name:    "subtree filter text rejected",
			filter:  &Filter{Type: "subtree", Content: []byte("junk")},
			rpcName: "get-config",
			wantErr: true,
		},
		{
			name:    "default subtree xpath text rejected",
			filter:  &Filter{Content: []byte("/interfaces")},
			rpcName: "get-config",
			wantErr: true,
		},
		{
			name:    "comment only filter rejected",
			filter:  &Filter{Content: []byte("<!-- no element -->")},
			rpcName: "get-config",
			wantErr: true,
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

func TestFilterValidateDoesNotMutateFilter(t *testing.T) {
	tests := []struct {
		name   string
		filter *Filter
	}{
		{
			name:   "default subtree keeps empty type",
			filter: &Filter{Content: []byte("<interfaces/>")},
		},
		{
			name:   "xpath keeps raw type and select",
			filter: &Filter{Type: "\n xpath \t", Select: "\n /interfaces \t"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalType := tt.filter.Type
			originalSelect := tt.filter.Select

			if err := tt.filter.Validate("get-config"); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if tt.filter.Type != originalType {
				t.Fatalf("Validate() Type = %q, want %q", tt.filter.Type, originalType)
			}
			if tt.filter.Select != originalSelect {
				t.Fatalf("Validate() Select = %q, want %q", tt.filter.Select, originalSelect)
			}
		})
	}
}

func TestValidateFilterDepthAndSizeTrimsFilterType(t *testing.T) {
	filter := &Filter{Type: "\n xpath \t", Select: "interfaces"}

	err := ValidateFilterDepthAndSize("get-config", filter)
	if err == nil {
		t.Fatal("ValidateFilterDepthAndSize() error = nil, want invalid xpath filter")
	}
	rpcErr, ok := err.(*RPCError)
	if !ok {
		t.Fatalf("ValidateFilterDepthAndSize() error = %T, want *RPCError", err)
	}
	if rpcErr.ErrorTag != ErrorTagInvalidValue {
		t.Fatalf("ValidateFilterDepthAndSize() error tag = %s, want %s", rpcErr.ErrorTag, ErrorTagInvalidValue)
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

	if rpcErr.ErrorType != ErrorTypeRPC || rpcErr.ErrorTag != ErrorTagMalformedMessage {
		t.Errorf("Expected rpc/malformed-message error for size limit, got %s/%s", rpcErr.ErrorType, rpcErr.ErrorTag)
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
	// Format: \n#<length>\n<data>\n##\n
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
