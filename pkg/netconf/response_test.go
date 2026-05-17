package netconf

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

func TestNewOKReply(t *testing.T) {
	reply := NewOKReply("101")

	if reply.MessageID != "101" {
		t.Errorf("Expected message-id 101, got %s", reply.MessageID)
	}

	if reply.OK == nil {
		t.Errorf("Expected <ok/> element")
	}

	if reply.Data != nil {
		t.Errorf("Expected no <data> element")
	}

	if len(reply.Errors) > 0 {
		t.Errorf("Expected no <rpc-error> elements")
	}
}

func TestNewDataReply(t *testing.T) {
	data := []byte("<interfaces><interface>xe-0/0/0</interface></interfaces>")
	reply := NewDataReply("102", data)

	if reply.MessageID != "102" {
		t.Errorf("Expected message-id 102, got %s", reply.MessageID)
	}

	if reply.Data == nil {
		t.Errorf("Expected <data> element")
		return
	}

	if string(reply.Data.Content) != string(data) {
		t.Errorf("Expected data content to match")
	}

	if reply.OK != nil {
		t.Errorf("Expected no <ok/> element")
	}

	if len(reply.Errors) > 0 {
		t.Errorf("Expected no <rpc-error> elements")
	}
}

func TestNewDataReplyCopiesContent(t *testing.T) {
	data := []byte("<interfaces/>")
	reply := NewDataReply("102", data)

	data[1] = 'x'

	if string(reply.Data.Content) != "<interfaces/>" {
		t.Fatalf("reply data content = %q, want original data", string(reply.Data.Content))
	}
}

func TestNewErrorReply(t *testing.T) {
	err := NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue, "test error")
	reply := NewErrorReply("103", err)

	if reply.MessageID != "103" {
		t.Errorf("Expected message-id 103, got %s", reply.MessageID)
	}

	if len(reply.Errors) != 1 {
		t.Errorf("Expected 1 error, got %d", len(reply.Errors))
		return
	}

	if reply.Errors[0].ErrorMessage != "test error" {
		t.Errorf("Expected error message 'test error'")
	}

	if reply.OK != nil {
		t.Errorf("Expected no <ok/> element")
	}

	if reply.Data != nil {
		t.Errorf("Expected no <data> element")
	}
}

func TestNewErrorReplyDefaultsNilError(t *testing.T) {
	reply := NewErrorReply("103", nil)

	if len(reply.Errors) != 1 {
		t.Fatalf("errors = %d, want 1", len(reply.Errors))
	}
	if reply.Errors[0] == nil {
		t.Fatal("error = nil, want default RPC error")
	}
	if reply.Errors[0].ErrorTag != ErrorTagOperationFailed {
		t.Fatalf("error tag = %s, want %s", reply.Errors[0].ErrorTag, ErrorTagOperationFailed)
	}
	if _, err := MarshalReply(reply); err != nil {
		t.Fatalf("MarshalReply() error = %v", err)
	}
}

func TestNewErrorReplyCopiesError(t *testing.T) {
	err := ErrLockDeniedForLock("candidate", 123)
	reply := NewErrorReply("103", err)

	err.ErrorMessage = "mutated"
	err.ErrorInfo.LockOwnerSession = "456"

	if reply.Errors[0].ErrorMessage == "mutated" {
		t.Fatal("reply error message changed after source mutation")
	}
	if reply.Errors[0].ErrorInfo.LockOwnerSession != "123" {
		t.Fatalf("reply lock owner = %q, want copied lock owner", reply.Errors[0].ErrorInfo.LockOwnerSession)
	}
}

func TestNewMultiErrorReply(t *testing.T) {
	errors := []*RPCError{
		NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue, "error 1"),
		NewRPCError(ErrorTypeApplication, ErrorTagOperationFailed, "error 2"),
	}

	reply := NewMultiErrorReply("104", errors)

	if reply.MessageID != "104" {
		t.Errorf("Expected message-id 104, got %s", reply.MessageID)
	}

	if len(reply.Errors) != 2 {
		t.Errorf("Expected 2 errors, got %d", len(reply.Errors))
		return
	}

	if reply.Errors[0].ErrorMessage != "error 1" {
		t.Errorf("Expected first error message 'error 1'")
	}

	if reply.Errors[1].ErrorMessage != "error 2" {
		t.Errorf("Expected second error message 'error 2'")
	}
}

func TestNewMultiErrorReplyCopiesErrors(t *testing.T) {
	err := ErrLockDeniedForUnlock("candidate", 123)
	reply := NewMultiErrorReply("104", []*RPCError{err})

	err.ErrorMessage = "mutated"
	err.ErrorInfo.LockOwnerSession = "456"

	if reply.Errors[0].ErrorMessage == "mutated" {
		t.Fatal("reply error message changed after source mutation")
	}
	if reply.Errors[0].ErrorInfo.LockOwnerSession != "123" {
		t.Fatalf("reply lock owner = %q, want copied lock owner", reply.Errors[0].ErrorInfo.LockOwnerSession)
	}
}

func TestNewMultiErrorReplyDefaultsNilErrors(t *testing.T) {
	reply := NewMultiErrorReply("104", []*RPCError{nil})

	if len(reply.Errors) != 1 {
		t.Fatalf("errors = %d, want 1", len(reply.Errors))
	}
	if reply.Errors[0] == nil {
		t.Fatal("error = nil, want default RPC error")
	}
	if reply.Errors[0].ErrorTag != ErrorTagOperationFailed {
		t.Fatalf("error tag = %s, want %s", reply.Errors[0].ErrorTag, ErrorTagOperationFailed)
	}
	if _, err := MarshalReply(reply); err != nil {
		t.Fatalf("MarshalReply() error = %v", err)
	}
}

func TestNewMultiErrorReplyDefaultsEmptyErrors(t *testing.T) {
	for _, errors := range [][]*RPCError{nil, []*RPCError{}} {
		reply := NewMultiErrorReply("104", errors)

		if len(reply.Errors) != 1 {
			t.Fatalf("errors = %d, want 1", len(reply.Errors))
		}
		if reply.Errors[0] == nil {
			t.Fatal("error = nil, want default RPC error")
		}
		if reply.Errors[0].ErrorTag != ErrorTagOperationFailed {
			t.Fatalf("error tag = %s, want %s", reply.Errors[0].ErrorTag, ErrorTagOperationFailed)
		}
		data, err := MarshalReply(reply)
		if err != nil {
			t.Fatalf("MarshalReply() error = %v", err)
		}
		if !strings.Contains(string(data), "<rpc-error") {
			t.Fatalf("MarshalReply() = %s, want rpc-error", string(data))
		}
	}
}

func TestMarshalOKReply(t *testing.T) {
	reply := NewOKReply("101")
	data, err := MarshalReply(reply)
	if err != nil {
		t.Fatalf("Failed to marshal reply: %v", err)
	}

	xmlStr := string(data)

	// Check required elements
	if !strings.Contains(xmlStr, `message-id="101"`) {
		t.Errorf("Missing message-id attribute")
	}

	if !strings.Contains(xmlStr, "<ok") {
		t.Errorf("Missing <ok/> element")
	}

	if !strings.Contains(xmlStr, `xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"`) {
		t.Errorf("Missing NETCONF namespace")
	}
}

func TestMarshalReplyNormalizesNilErrors(t *testing.T) {
	reply := &RPCReply{
		MessageID: "105",
		Errors:    []*RPCError{nil},
	}

	data, err := MarshalReply(reply)
	if err != nil {
		t.Fatalf("MarshalReply() error = %v", err)
	}

	xmlStr := string(data)
	if !strings.Contains(xmlStr, "<rpc-error") {
		t.Fatalf("MarshalReply() = %s, want rpc-error", xmlStr)
	}
	if !strings.Contains(xmlStr, "<error-tag>operation-failed</error-tag>") {
		t.Fatalf("MarshalReply() = %s, want default operation-failed error", xmlStr)
	}
}

func TestMarshalReplyNormalizesIncompleteErrors(t *testing.T) {
	err := &RPCError{
		ErrorMessage: "hand-built error",
		ErrorInfo: &ErrorInfo{
			BadElement: "bad",
		},
	}
	reply := NewErrorReply("105", err)

	data, marshalErr := MarshalReply(reply)
	if marshalErr != nil {
		t.Fatalf("MarshalReply() error = %v", marshalErr)
	}

	xmlStr := string(data)
	for _, want := range []string{
		"<error-type>rpc</error-type>",
		"<error-tag>operation-failed</error-tag>",
		"<error-severity>error</error-severity>",
		"<error-message>hand-built error</error-message>",
		"<bad-element>bad</bad-element>",
	} {
		if !strings.Contains(xmlStr, want) {
			t.Fatalf("MarshalReply() = %s, want %s", xmlStr, want)
		}
	}
	if err.ErrorType != "" || err.ErrorTag != "" || err.ErrorSeverity != "" {
		t.Fatalf("MarshalReply() mutated error = %#v", err)
	}
}

func TestMarshalReplyOmitsEmptyErrorInfo(t *testing.T) {
	err := &RPCError{
		ErrorType:     ErrorTypeRPC,
		ErrorTag:      ErrorTagOperationFailed,
		ErrorSeverity: ErrorSeverityError,
		ErrorMessage:  "hand-built error",
		ErrorInfo:     &ErrorInfo{},
	}
	reply := NewErrorReply("105", err)

	data, marshalErr := MarshalReply(reply)
	if marshalErr != nil {
		t.Fatalf("MarshalReply() error = %v", marshalErr)
	}

	xmlStr := string(data)
	if strings.Contains(xmlStr, "<error-info") {
		t.Fatalf("MarshalReply() = %s, want empty error-info omitted", xmlStr)
	}
	if err.ErrorInfo == nil {
		t.Fatal("MarshalReply() mutated caller-owned error info")
	}
}

func TestMarshalReplyRejectsInvalidErrorEnums(t *testing.T) {
	tests := []struct {
		name string
		err  *RPCError
		want string
	}{
		{
			name: "type",
			err: &RPCError{
				ErrorType:     ErrorType("bad-type"),
				ErrorTag:      ErrorTagOperationFailed,
				ErrorSeverity: ErrorSeverityError,
			},
			want: "invalid RPC error type",
		},
		{
			name: "tag",
			err: &RPCError{
				ErrorType:     ErrorTypeRPC,
				ErrorTag:      ErrorTag("bad-tag"),
				ErrorSeverity: ErrorSeverityError,
			},
			want: "invalid RPC error tag",
		},
		{
			name: "severity",
			err: &RPCError{
				ErrorType:     ErrorTypeRPC,
				ErrorTag:      ErrorTagOperationFailed,
				ErrorSeverity: ErrorSeverity("bad-severity"),
			},
			want: "invalid RPC error severity",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := MarshalReply(NewErrorReply("106", tt.err))
			if err == nil {
				t.Fatalf("MarshalReply() error = nil, want %q; data=%s", tt.want, string(data))
			}
			if data != nil {
				t.Fatalf("MarshalReply() data length = %d, want nil", len(data))
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("MarshalReply() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestMarshalReplyRejectsInvalidPayloads(t *testing.T) {
	tests := []struct {
		name  string
		reply *RPCReply
		want  string
	}{
		{
			name:  "empty payload",
			reply: &RPCReply{MessageID: "106"},
			want:  "no payload",
		},
		{
			name: "multiple payloads",
			reply: &RPCReply{
				MessageID: "107",
				OK:        &struct{}{},
				Errors:    []*RPCError{ErrOperationFailed("failed")},
			},
			want: "multiple payloads",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := MarshalReply(tt.reply)
			if err == nil {
				t.Fatalf("MarshalReply() error = nil, want %q; data=%s", tt.want, string(data))
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("MarshalReply() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestMarshalReplyRejectsInvalidDataContent(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
		want    string
	}{
		{
			name:    "malformed xml",
			content: []byte("<interfaces>"),
			want:    "data reply content is malformed",
		},
		{
			name:    "unsafe directive",
			content: []byte(`<!DOCTYPE data SYSTEM "evil.dtd"><interfaces/>`),
			want:    "unsafe XML directives",
		},
		{
			name:    "root text",
			content: []byte("text"),
			want:    "text outside elements",
		},
		{
			name:    "oversized",
			content: bytes.Repeat([]byte("x"), MaxXMLSize+1),
			want:    "data reply content exceeds maximum",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := MarshalReply(NewDataReply("108", tt.content))
			if err == nil {
				t.Fatal("MarshalReply() error = nil, want data content error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("MarshalReply() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestMarshalReplyRejectsOversizedReplyXML(t *testing.T) {
	reply := NewErrorReply("109", ErrOperationFailed(strings.Repeat("x", MaxXMLSize)))

	data, err := MarshalReply(reply)
	if err == nil {
		t.Fatal("MarshalReply() error = nil, want size limit error")
	}
	if data != nil {
		t.Fatalf("MarshalReply() data length = %d, want nil", len(data))
	}
	if !strings.Contains(err.Error(), "RPC reply exceeds maximum") {
		t.Fatalf("MarshalReply() error = %v, want reply size limit error", err)
	}
}

func TestMarshalReplyPreservesAttributes(t *testing.T) {
	reply := NewOKReply("101").WithAttributes([]xml.Attr{
		{Name: xml.Name{Space: "xmlns", Local: "ex"}, Value: "http://example.net/content/1.0"},
		{Name: xml.Name{Space: "http://example.net/content/1.0", Local: "user-id"}, Value: "fred"},
		{Name: xml.Name{Local: "trace-id"}, Value: "abc"},
	})

	data, err := MarshalReply(reply)
	if err != nil {
		t.Fatalf("Failed to marshal reply: %v", err)
	}

	xmlStr := string(data)
	for _, want := range []string{
		`xmlns:ex="http://example.net/content/1.0"`,
		`ex:user-id="fred"`,
		`trace-id="abc"`,
	} {
		if !strings.Contains(xmlStr, want) {
			t.Errorf("Missing preserved attribute %s in %s", want, xmlStr)
		}
	}
}

func TestMarshalReplyRejectsEmptyAttributeName(t *testing.T) {
	reply := NewOKReply("101").WithAttributes([]xml.Attr{
		{Name: xml.Name{Local: ""}, Value: "bad"},
	})

	_, err := MarshalReply(reply)
	if err == nil {
		t.Fatal("MarshalReply() error = nil, want empty attribute name error")
	}
	if !strings.Contains(err.Error(), "reply attribute name must not be empty") {
		t.Fatalf("MarshalReply() error = %v, want empty attribute name", err)
	}
}

func TestMarshalReplyRejectsEmptyNamespacePrefixDeclaration(t *testing.T) {
	reply := NewOKReply("101").WithAttributes([]xml.Attr{
		{Name: xml.Name{Space: "xmlns"}, Value: "urn:bad"},
	})

	_, err := MarshalReply(reply)
	if err == nil {
		t.Fatal("MarshalReply() error = nil, want empty namespace prefix error")
	}
	if !strings.Contains(err.Error(), "reply attribute name must not be empty") {
		t.Fatalf("MarshalReply() error = %v, want empty attribute name", err)
	}
}

func TestMarshalReplyRejectsReservedNamespaceDeclarations(t *testing.T) {
	tests := []struct {
		name string
		attr xml.Attr
		want string
	}{
		{
			name: "xml prefix rebound",
			attr: xml.Attr{Name: xml.Name{Space: "xmlns", Local: "xml"}, Value: "urn:bad"},
			want: "namespace prefix xml must be bound",
		},
		{
			name: "xmlns prefix declared",
			attr: xml.Attr{Name: xml.Name{Space: "xmlns", Local: "xmlns"}, Value: "urn:bad"},
			want: "namespace prefix xmlns must not be declared",
		},
		{
			name: "xml namespace default",
			attr: xml.Attr{Name: xml.Name{Local: "xmlns"}, Value: xmlNamespace},
			want: "xml namespace must use xml prefix",
		},
		{
			name: "xmlns namespace value",
			attr: xml.Attr{Name: xml.Name{Space: "xmlns", Local: "bad"}, Value: xmlnsNamespace},
			want: "xmlns namespace must not be declared",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := MarshalReply(NewOKReply("101").WithAttributes([]xml.Attr{tt.attr}))
			if err == nil {
				t.Fatalf("MarshalReply() error = nil, want %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("MarshalReply() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestMarshalReplyOmitsEmptyMessageID(t *testing.T) {
	reply := NewErrorReply("", ErrMissingAttribute("rpc", "message-id"))

	data, err := MarshalReply(reply)
	if err != nil {
		t.Fatalf("Failed to marshal reply: %v", err)
	}

	xmlStr := string(data)
	if strings.Contains(xmlStr, `message-id=""`) || strings.Contains(xmlStr, `message-id=`) {
		t.Fatalf("Expected message-id to be omitted, got %s", xmlStr)
	}
}

func TestMarshalDataReply(t *testing.T) {
	data := []byte("<interfaces><interface>xe-0/0/0</interface></interfaces>")
	reply := NewDataReply("102", data)

	xmlData, err := MarshalReply(reply)
	if err != nil {
		t.Fatalf("Failed to marshal reply: %v", err)
	}

	xmlStr := string(xmlData)

	if !strings.Contains(xmlStr, `message-id="102"`) {
		t.Errorf("Missing message-id attribute")
	}

	if !strings.Contains(xmlStr, "<data") {
		t.Errorf("Missing <data> element")
	}

	if !strings.Contains(xmlStr, "xe-0/0/0") {
		t.Errorf("Missing data content")
	}
	assertSingleDataElement(t, xmlData)
}

func TestMarshalOperationalDataReply(t *testing.T) {
	reply := NewDataReply("105", []byte(buildAllOperationalData()))

	xmlData, err := MarshalReply(reply)
	if err != nil {
		t.Fatalf("Failed to marshal reply: %v", err)
	}

	assertSingleDataElement(t, xmlData)
}

func TestMarshalErrorReply(t *testing.T) {
	err := NewRPCError(ErrorTypeProtocol, ErrorTagLockDenied, "lock denied").
		WithPath("/rpc/lock/target").
		WithLockOwner("session-456")

	reply := NewErrorReply("103", err)

	data, marshalErr := MarshalReply(reply)
	if marshalErr != nil {
		t.Fatalf("Failed to marshal reply: %v", marshalErr)
	}

	xmlStr := string(data)
	t.Logf("Marshaled XML:\n%s", xmlStr)

	// Check error structure (namespace may be present)
	if !strings.Contains(xmlStr, "<rpc-error") {
		t.Errorf("Missing <rpc-error> element")
	}

	if !strings.Contains(xmlStr, "<error-type>protocol</error-type>") {
		t.Errorf("Missing error-type")
	}

	if !strings.Contains(xmlStr, "<error-tag>lock-denied</error-tag>") {
		t.Errorf("Missing error-tag")
	}

	if !strings.Contains(xmlStr, "<error-path>/rpc/lock/target</error-path>") {
		t.Errorf("Missing error-path")
	}

	if !strings.Contains(xmlStr, "<lock-owner-session>session-456</lock-owner-session>") {
		t.Errorf("Missing lock-owner-session in error-info")
	}
}

func TestRPCReplyRoundtrip(t *testing.T) {
	// Test that we can marshal and unmarshal a reply
	original := NewOKReply("101")
	data, err := xml.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var roundtrip RPCReply
	if err := xml.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if roundtrip.MessageID != original.MessageID {
		t.Errorf("Message ID mismatch after roundtrip")
	}

	if roundtrip.OK == nil {
		t.Errorf("Lost <ok/> element after roundtrip")
	}
}

func TestDataReplyNamespace(t *testing.T) {
	reply := NewDataReply("102", []byte("<test/>"))
	data, err := MarshalReply(reply)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	xmlStr := string(data)

	// Both rpc-reply and data should have NETCONF namespace
	if !strings.Contains(xmlStr, `xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"`) {
		t.Errorf("Missing NETCONF namespace on rpc-reply")
	}
}

func assertSingleDataElement(t *testing.T, xmlData []byte) {
	t.Helper()

	decoder := xml.NewDecoder(bytes.NewReader(xmlData))
	depth := 0
	directDataElements := 0
	allDataElements := 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("reply is not valid XML: %v\n%s", err, xmlData)
		}

		switch t := token.(type) {
		case xml.StartElement:
			depth++
			if t.Name.Local == "data" {
				allDataElements++
				if depth == 2 {
					directDataElements++
				}
			}
		case xml.EndElement:
			depth--
		}
	}

	if directDataElements != 1 || allDataElements != 1 {
		t.Fatalf("reply has direct/all data elements = %d/%d, want 1/1:\n%s", directDataElements, allDataElements, xmlData)
	}
}
