package netconf

import (
	"encoding/xml"
	"fmt"
)

// ErrorType represents NETCONF error-type values per RFC 6241
type ErrorType string

const (
	ErrorTypeProtocol    ErrorType = "protocol"
	ErrorTypeApplication ErrorType = "application"
	ErrorTypeTransport   ErrorType = "transport"
	ErrorTypeRPC         ErrorType = "rpc"
)

// ErrorTag represents NETCONF error-tag values per RFC 6241
type ErrorTag string

const (
	ErrorTagInvalidValue        ErrorTag = "invalid-value"
	ErrorTagMalformedMessage    ErrorTag = "malformed-message"
	ErrorTagOperationNotSupported ErrorTag = "operation-not-supported"
	ErrorTagAccessDenied        ErrorTag = "access-denied"
	ErrorTagLockDenied          ErrorTag = "lock-denied"
	ErrorTagInUse               ErrorTag = "in-use"
	ErrorTagOperationFailed     ErrorTag = "operation-failed"
	ErrorTagMissingElement      ErrorTag = "missing-element"
	ErrorTagUnknownElement      ErrorTag = "unknown-element"
)

// ErrorSeverity represents NETCONF error-severity values per RFC 6241
type ErrorSeverity string

const (
	ErrorSeverityError   ErrorSeverity = "error"
	ErrorSeverityWarning ErrorSeverity = "warning"
)

// RPCError represents a NETCONF <rpc-error> structure per RFC 6241
type RPCError struct {
	XMLName       xml.Name      `xml:"urn:ietf:params:xml:ns:netconf:base:1.0 rpc-error"`
	ErrorType     ErrorType     `xml:"error-type"`
	ErrorTag      ErrorTag      `xml:"error-tag"`
	ErrorSeverity ErrorSeverity `xml:"error-severity"`
	ErrorAppTag   string        `xml:"error-app-tag,omitempty"` // RFC 6241: direct child of rpc-error
	ErrorPath     string        `xml:"error-path,omitempty"`
	ErrorMessage  string        `xml:"error-message,omitempty"`
	ErrorInfo     *ErrorInfo    `xml:"error-info,omitempty"`
}

// ErrorInfo contains structured error details per RFC 6241
type ErrorInfo struct {
	BadElement       string `xml:"bad-element,omitempty"`
	BadAttribute     string `xml:"bad-attribute,omitempty"`
	BadNamespace     string `xml:"bad-namespace,omitempty"`
	LockOwnerSession string `xml:"lock-owner-session,omitempty"`
}

// NewRPCError creates a new RPCError with required fields
func NewRPCError(errType ErrorType, errTag ErrorTag, message string) *RPCError {
	return &RPCError{
		ErrorType:     errType,
		ErrorTag:      errTag,
		ErrorSeverity: ErrorSeverityError,
		ErrorMessage:  message,
	}
}

// WithPath adds error-path to the error
func (e *RPCError) WithPath(path string) *RPCError {
	e.ErrorPath = path
	return e
}

// WithBadElement adds bad-element to error-info
func (e *RPCError) WithBadElement(element string) *RPCError {
	if e.ErrorInfo == nil {
		e.ErrorInfo = &ErrorInfo{}
	}
	e.ErrorInfo.BadElement = element
	return e
}

// WithBadAttribute adds bad-attribute to error-info
func (e *RPCError) WithBadAttribute(attribute string) *RPCError {
	if e.ErrorInfo == nil {
		e.ErrorInfo = &ErrorInfo{}
	}
	e.ErrorInfo.BadAttribute = attribute
	return e
}

// WithBadNamespace adds bad-namespace to error-info
func (e *RPCError) WithBadNamespace(namespace string) *RPCError {
	if e.ErrorInfo == nil {
		e.ErrorInfo = &ErrorInfo{}
	}
	e.ErrorInfo.BadNamespace = namespace
	return e
}

// WithLockOwner adds lock-owner-session to error-info
func (e *RPCError) WithLockOwner(sessionID string) *RPCError {
	if e.ErrorInfo == nil {
		e.ErrorInfo = &ErrorInfo{}
	}
	e.ErrorInfo.LockOwnerSession = sessionID
	return e
}

// WithAppTag adds error-app-tag as direct child of rpc-error (RFC 6241)
func (e *RPCError) WithAppTag(tag string) *RPCError {
	e.ErrorAppTag = tag
	return e
}

// Error implements the error interface for RPCError
func (e *RPCError) Error() string {
	if e == nil {
		return "unknown NETCONF RPC error"
	}
	return fmt.Sprintf("NETCONF error [%s/%s]: %s", e.ErrorType, e.ErrorTag, e.ErrorMessage)
}

// Common error constructors following the design document error mapping table

// ErrMalformedMessage returns XML parse error
func ErrMalformedMessage(message string) *RPCError {
	return NewRPCError(ErrorTypeProtocol, ErrorTagMalformedMessage, message).
		WithPath("/rpc")
}

// ErrMalformedMessageWithElement returns XML parse error with bad element
func ErrMalformedMessageWithElement(message, element string) *RPCError {
	return ErrMalformedMessage(message).WithBadElement(element)
}

// ErrDTDNotAllowed returns error for DTD/DOCTYPE in XML
func ErrDTDNotAllowed() *RPCError {
	return NewRPCError(ErrorTypeProtocol, ErrorTagMalformedMessage, "DTD declarations are not allowed").
		WithPath("/rpc").
		WithBadElement("DOCTYPE")
}

// ErrUnknownRPC returns error for unsupported RPC operation
func ErrUnknownRPC(rpcName string) *RPCError {
	return NewRPCError(ErrorTypeProtocol, ErrorTagOperationNotSupported, fmt.Sprintf("unknown RPC operation: %s", rpcName)).
		WithPath("/rpc/*").
		WithBadElement(rpcName)
}

// ErrInvalidTarget returns error for unsupported datastore target
func ErrInvalidTarget(rpcName, target string) *RPCError {
	return NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue, fmt.Sprintf("unsupported datastore target: %s", target)).
		WithPath(fmt.Sprintf("/rpc/%s/target", rpcName)).
		WithBadElement(target)
}

// ErrUnsupportedFilterType returns error for unsupported filter type
func ErrUnsupportedFilterType(rpcName, filterType string) *RPCError {
	if filterType == "xpath" {
		return NewRPCError(ErrorTypeProtocol, ErrorTagOperationNotSupported, "XPath filters are not supported").
			WithPath(fmt.Sprintf("/rpc/%s/filter", rpcName)).
			WithBadAttribute("type")
	}
	return NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue, fmt.Sprintf("unsupported filter type: %s", filterType)).
		WithPath(fmt.Sprintf("/rpc/%s/filter", rpcName)).
		WithBadAttribute("type")
}

// ErrInvalidFilter returns error for invalid filter content
func ErrInvalidFilter(rpcName, message string) *RPCError {
	return NewRPCError(ErrorTypeProtocol, ErrorTagInvalidValue, message).
		WithPath(fmt.Sprintf("/rpc/%s/filter", rpcName)).
		WithBadElement("filter")
}

// ErrInvalidNamespace returns error for XML namespace mismatch
func ErrInvalidNamespace(namespace string) *RPCError {
	return NewRPCError(ErrorTypeProtocol, ErrorTagMalformedMessage, fmt.Sprintf("invalid namespace: %s", namespace)).
		WithPath("/rpc").
		WithBadNamespace(namespace)
}

// ErrMissingElement returns error for missing required element
func ErrMissingElement(rpcName, element string) *RPCError {
	return NewRPCError(ErrorTypeProtocol, ErrorTagMissingElement, fmt.Sprintf("missing required element: %s", element)).
		WithPath(fmt.Sprintf("/rpc/%s", rpcName)).
		WithBadElement(element)
}

// ErrUnknownElement returns error for unknown/unsupported element
func ErrUnknownElement(path, element string) *RPCError {
	return NewRPCError(ErrorTypeProtocol, ErrorTagUnknownElement, fmt.Sprintf("unknown element: %s", element)).
		WithPath(path).
		WithBadElement(element)
}

// ErrAccessDenied returns error for RBAC denial
func ErrAccessDenied(rpcName, reason string) *RPCError {
	return NewRPCError(ErrorTypeProtocol, ErrorTagAccessDenied, fmt.Sprintf("access denied: %s", reason)).
		WithPath(fmt.Sprintf("/rpc/%s", rpcName)).
		WithAppTag("rbac-deny")
}

// ErrLockDenied returns error when lock is not acquired for write operation
// rpcName should be the operation name (edit-config, copy-config, delete-config, commit, discard-changes)
// hasTargetElement indicates if the RPC has an explicit <target> element in its XML structure
func ErrLockDenied(target, rpcName string, hasTargetElement bool) *RPCError {
	errorPath := fmt.Sprintf("/rpc/%s", rpcName)
	if hasTargetElement {
		errorPath = fmt.Sprintf("/rpc/%s/target", rpcName)
	}
	return NewRPCError(ErrorTypeProtocol, ErrorTagLockDenied, fmt.Sprintf("target datastore %s must be locked before %s operation", target, rpcName)).
		WithPath(errorPath)
}

// ErrLockDeniedWithOwner returns error when lock is held by another session
// rpcName should be the operation name (edit-config, copy-config, delete-config, commit, discard-changes)
// hasTargetElement indicates if the RPC has an explicit <target> element in its XML structure
// If ownerNumericID is 0, omits lock-owner-session (unknown/closed session)
func ErrLockDeniedWithOwner(target, rpcName string, ownerNumericID uint32, hasTargetElement bool) *RPCError {
	errorPath := fmt.Sprintf("/rpc/%s", rpcName)
	if hasTargetElement {
		errorPath = fmt.Sprintf("/rpc/%s/target", rpcName)
	}
	err := NewRPCError(ErrorTypeProtocol, ErrorTagLockDenied, fmt.Sprintf("target datastore %s is locked by another session", target)).
		WithPath(errorPath)

	// Only include lock-owner-session if valid (non-zero)
	if ownerNumericID != 0 {
		err = err.WithLockOwner(fmt.Sprintf("%d", ownerNumericID))
	}
	return err
}

// ErrLockDeniedForLock returns error for lock conflict (used by lock RPC itself)
func ErrLockDeniedForLock(target string, ownerNumericID uint32) *RPCError {
	err := NewRPCError(ErrorTypeProtocol, ErrorTagLockDenied, fmt.Sprintf("target datastore %s is locked by another session", target)).
		WithPath("/rpc/lock/target")

	// Only include lock-owner-session if valid (non-zero)
	if ownerNumericID != 0 {
		err = err.WithLockOwner(fmt.Sprintf("%d", ownerNumericID))
	}
	return err
}

// ErrLockDeniedForUnlock returns error for lock conflict (used by unlock RPC)
func ErrLockDeniedForUnlock(target string, ownerNumericID uint32) *RPCError {
	err := NewRPCError(ErrorTypeProtocol, ErrorTagLockDenied, fmt.Sprintf("target datastore %s is locked by another session", target)).
		WithPath("/rpc/unlock/target")

	// Only include lock-owner-session if valid (non-zero)
	if ownerNumericID != 0 {
		err = err.WithLockOwner(fmt.Sprintf("%d", ownerNumericID))
	}
	return err
}

// ErrLockTimeout returns error for lock timeout
func ErrLockTimeout(target string) *RPCError {
	return NewRPCError(ErrorTypeProtocol, ErrorTagOperationFailed, "lock has been released due to timeout").
		WithPath("/rpc/unlock/target").
		WithBadElement("target")
}

// ErrValidationFailed returns error for validation failure
func ErrValidationFailed(message string) *RPCError {
	return NewRPCError(ErrorTypeApplication, ErrorTagInvalidValue, message).
		WithPath("/rpc/validate/source").
		WithAppTag("validation-failed")
}

// ErrUnsupportedConfigElement returns error for unknown config element
func ErrUnsupportedConfigElement(element string) *RPCError {
	return NewRPCError(ErrorTypeApplication, ErrorTagInvalidValue, fmt.Sprintf("unsupported configuration element: %s", element)).
		WithPath(fmt.Sprintf("/rpc/edit-config/config/%s", element)).
		WithBadElement(element)
}

// ErrDatastoreError returns error for internal datastore error
func ErrDatastoreError(message string) *RPCError {
	return NewRPCError(ErrorTypeApplication, ErrorTagOperationFailed, message).
		WithAppTag("datastore-error")
}

// ErrTimeout returns error for operation timeout
func ErrTimeout(message string) *RPCError {
	return NewRPCError(ErrorTypeApplication, ErrorTagOperationFailed, message).
		WithAppTag("timeout")
}

// ErrTransportClosed returns error for transport/session cleanup
func ErrTransportClosed() *RPCError {
	return NewRPCError(ErrorTypeTransport, ErrorTagOperationFailed, "transport connection closed")
}

// ErrWritableRunningNotSupported returns error for writable-running capability not advertised
func ErrWritableRunningNotSupported() *RPCError {
	return NewRPCError(ErrorTypeProtocol, ErrorTagOperationNotSupported, "writable-running capability not supported").
		WithPath("/rpc/edit-config/target").
		WithBadElement("running")
}

// ErrBackendValidationFailed returns error for backend (VPP/FRR) validation failure
func ErrBackendValidationFailed(message string) *RPCError {
	return NewRPCError(ErrorTypeApplication, ErrorTagInvalidValue, message).
		WithAppTag("backend-validation-failed")
}
