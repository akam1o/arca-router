package netconf

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestNewRPCError(t *testing.T) {
	err := NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue, "test error")

	if err.ErrorType != ErrorTypeProtocol {
		t.Errorf("Expected error type protocol, got %s", err.ErrorType)
	}

	if err.ErrorTag != ErrorTagInvalidValue {
		t.Errorf("Expected error tag invalid-value, got %s", err.ErrorTag)
	}

	if err.ErrorMessage != "test error" {
		t.Errorf("Expected message 'test error', got %s", err.ErrorMessage)
	}

	if err.ErrorSeverity != ErrorSeverityError {
		t.Errorf("Expected severity error, got %s", err.ErrorSeverity)
	}
}

func TestRPCErrorWithPath(t *testing.T) {
	err := NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue, "test").
		WithPath("/rpc/get-config/source")

	if err.ErrorPath != "/rpc/get-config/source" {
		t.Errorf("Expected path '/rpc/get-config/source', got %s", err.ErrorPath)
	}
}

func TestRPCErrorWithBadElement(t *testing.T) {
	err := NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue, "test").
		WithBadElement("running")

	if err.ErrorInfo == nil {
		t.Fatal("Expected error-info to be set")
	}

	if err.ErrorInfo.BadElement != "running" {
		t.Errorf("Expected bad-element 'running', got %s", err.ErrorInfo.BadElement)
	}
}

func TestRPCErrorChaining(t *testing.T) {
	err := NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue, "test").
		WithPath("/rpc/lock/target").
		WithBadElement("startup").
		WithAppTag("custom-error")

	if err.ErrorPath != "/rpc/lock/target" {
		t.Errorf("Expected path, got %s", err.ErrorPath)
	}

	if err.ErrorInfo.BadElement != "startup" {
		t.Errorf("Expected bad-element, got %s", err.ErrorInfo.BadElement)
	}

	if err.ErrorAppTag != "custom-error" {
		t.Errorf("Expected app-tag, got %s", err.ErrorAppTag)
	}
}

func TestErrMalformedMessage(t *testing.T) {
	err := ErrMalformedMessage("invalid XML")

	if err.ErrorType != ErrorTypeProtocol {
		t.Errorf("Expected protocol error type")
	}

	if err.ErrorTag != ErrorTagMalformedMessage {
		t.Errorf("Expected malformed-message tag")
	}

	if err.ErrorPath != "/rpc" {
		t.Errorf("Expected /rpc path, got %s", err.ErrorPath)
	}
}

func TestErrDTDNotAllowed(t *testing.T) {
	err := ErrDTDNotAllowed()

	if err.ErrorType != ErrorTypeProtocol {
		t.Errorf("Expected protocol error type")
	}

	if err.ErrorInfo == nil || err.ErrorInfo.BadElement != "DOCTYPE" {
		t.Errorf("Expected bad-element DOCTYPE")
	}
}

func TestErrLockDenied(t *testing.T) {
	// Test ErrLockDenied (lock not acquired) - with target element
	err := ErrLockDenied("candidate", "edit-config", true)

	if err.ErrorTag != ErrorTagLockDenied {
		t.Errorf("Expected lock-denied tag")
	}

	if err.ErrorPath != "/rpc/edit-config/target" {
		t.Errorf("Expected error-path /rpc/edit-config/target, got %s", err.ErrorPath)
	}

	if err.ErrorInfo != nil && err.ErrorInfo.LockOwnerSession != "" {
		t.Errorf("ErrLockDenied should not have lock-owner-session")
	}
}

func TestErrLockDeniedWithOwner(t *testing.T) {
	// Test ErrLockDeniedWithOwner (lock held by another session) - with target element
	err := ErrLockDeniedWithOwner("candidate", "edit-config", 123, true)

	if err.ErrorTag != ErrorTagLockDenied {
		t.Errorf("Expected lock-denied tag")
	}

	if err.ErrorPath != "/rpc/edit-config/target" {
		t.Errorf("Expected error-path /rpc/edit-config/target, got %s", err.ErrorPath)
	}

	if err.ErrorInfo == nil || err.ErrorInfo.LockOwnerSession != "123" {
		t.Errorf("Expected lock-owner-session 123, got %v", err.ErrorInfo)
	}
}

func TestErrLockDeniedWithoutTargetElement(t *testing.T) {
	// Test ErrLockDenied for operations without <target> element (commit, discard-changes)
	err := ErrLockDenied("candidate", "commit", false)

	if err.ErrorTag != ErrorTagLockDenied {
		t.Errorf("Expected lock-denied tag")
	}

	if err.ErrorPath != "/rpc/commit" {
		t.Errorf("Expected error-path /rpc/commit, got %s", err.ErrorPath)
	}
}

func TestErrLockDeniedForLockRPC(t *testing.T) {
	// Test ErrLockDeniedForLock (used by lock RPC)
	err := ErrLockDeniedForLock("candidate", 456)

	if err.ErrorTag != ErrorTagLockDenied {
		t.Errorf("Expected lock-denied tag")
	}

	if err.ErrorPath != "/rpc/lock/target" {
		t.Errorf("Expected error-path /rpc/lock/target, got %s", err.ErrorPath)
	}

	if err.ErrorInfo == nil || err.ErrorInfo.LockOwnerSession != "456" {
		t.Errorf("Expected lock-owner-session 456")
	}
}

func TestErrLockDeniedForUnlockRPC(t *testing.T) {
	// Test ErrLockDeniedForUnlock (used by unlock RPC)
	err := ErrLockDeniedForUnlock("candidate", 789)

	if err.ErrorTag != ErrorTagLockDenied {
		t.Errorf("Expected lock-denied tag")
	}

	if err.ErrorPath != "/rpc/unlock/target" {
		t.Errorf("Expected error-path /rpc/unlock/target, got %s", err.ErrorPath)
	}

	if err.ErrorInfo == nil || err.ErrorInfo.LockOwnerSession != "789" {
		t.Errorf("Expected lock-owner-session 789")
	}
}

func TestRPCErrorXMLSerialization(t *testing.T) {
	err := NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue, "test error").
		WithPath("/rpc/get-config").
		WithBadElement("source")

	data, xmlErr := xml.Marshal(err)
	if xmlErr != nil {
		t.Fatalf("Failed to marshal error: %v", xmlErr)
	}

	xmlStr := string(data)

	// Check required elements
	if !strings.Contains(xmlStr, "<error-type>protocol</error-type>") {
		t.Errorf("Missing error-type in XML")
	}

	if !strings.Contains(xmlStr, "<error-tag>invalid-value</error-tag>") {
		t.Errorf("Missing error-tag in XML")
	}

	if !strings.Contains(xmlStr, "<error-message>test error</error-message>") {
		t.Errorf("Missing error-message in XML")
	}

	if !strings.Contains(xmlStr, "<error-path>/rpc/get-config</error-path>") {
		t.Errorf("Missing error-path in XML")
	}

	if !strings.Contains(xmlStr, "<bad-element>source</bad-element>") {
		t.Errorf("Missing bad-element in XML")
	}
}

func TestErrUnsupportedFilterType(t *testing.T) {
	// Test xpath rejection
	err := ErrUnsupportedFilterType("get", "xpath")
	if err.ErrorTag != ErrorTagOperationNotSupported {
		t.Errorf("Expected operation-not-supported for xpath, got %s", err.ErrorTag)
	}

	// Test other unsupported types
	err2 := ErrUnsupportedFilterType("get", "invalid")
	if err2.ErrorTag != ErrorTagInvalidValue {
		t.Errorf("Expected invalid-value for other types, got %s", err2.ErrorTag)
	}
}

func TestErrAccessDenied(t *testing.T) {
	err := ErrAccessDenied("commit", "read-only role")

	if err.ErrorTag != ErrorTagAccessDenied {
		t.Errorf("Expected access-denied tag")
	}

	if err.ErrorAppTag != "rbac-deny" {
		t.Errorf("Expected rbac-deny app-tag, got %s", err.ErrorAppTag)
	}
}
