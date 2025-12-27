package netconf

import (
	"encoding/xml"
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

	// Check error structure
	if !strings.Contains(xmlStr, "<rpc-error>") {
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
