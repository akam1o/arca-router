package frr

import "fmt"

// Error codes for FRR operations
const (
	// ErrCodeGenerateFailed indicates FRR configuration generation failed
	ErrCodeGenerateFailed = "FRR_GENERATE_FAILED"

	// ErrCodeValidateFailed indicates FRR configuration validation failed
	ErrCodeValidateFailed = "FRR_VALIDATE_FAILED"

	// ErrCodeApplyFailed indicates FRR configuration apply failed
	ErrCodeApplyFailed = "FRR_APPLY_FAILED"

	// ErrCodeBackupFailed indicates FRR configuration backup failed
	ErrCodeBackupFailed = "FRR_BACKUP_FAILED"

	// ErrCodeRollbackFailed indicates FRR configuration rollback failed
	ErrCodeRollbackFailed = "FRR_ROLLBACK_FAILED"

	// ErrCodeToolNotFound indicates required FRR tool not found
	ErrCodeToolNotFound = "FRR_TOOL_NOT_FOUND"

	// ErrCodePermissionDenied indicates permission denied for FRR operation
	ErrCodePermissionDenied = "FRR_PERMISSION_DENIED"

	// ErrCodeInvalidConfig indicates invalid FRR configuration
	ErrCodeInvalidConfig = "FRR_INVALID_CONFIG"
)

// Error represents an FRR-specific error.
type Error struct {
	// Code is the error code
	Code string

	// Message is the human-readable error message
	Message string

	// Err is the underlying error
	Err error
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the underlying error.
func (e *Error) Unwrap() error {
	return e.Err
}

// NewError creates a new FRR error.
func NewError(code, message string, err error) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

// NewGenerateError creates an error for configuration generation failure.
func NewGenerateError(message string, err error) *Error {
	return NewError(ErrCodeGenerateFailed, message, err)
}

// NewValidateError creates an error for configuration validation failure.
func NewValidateError(message string, err error) *Error {
	return NewError(ErrCodeValidateFailed, message, err)
}

// NewApplyError creates an error for configuration apply failure.
func NewApplyError(message string, err error) *Error {
	return NewError(ErrCodeApplyFailed, message, err)
}

// NewBackupError creates an error for configuration backup failure.
func NewBackupError(message string, err error) *Error {
	return NewError(ErrCodeBackupFailed, message, err)
}

// NewRollbackError creates an error for configuration rollback failure.
func NewRollbackError(message string, err error) *Error {
	return NewError(ErrCodeRollbackFailed, message, err)
}

// NewToolNotFoundError creates an error for missing FRR tool.
func NewToolNotFoundError(tool string) *Error {
	return NewError(ErrCodeToolNotFound, fmt.Sprintf("FRR tool not found: %s", tool), nil)
}

// NewPermissionDeniedError creates an error for permission denied.
func NewPermissionDeniedError(operation string, err error) *Error {
	return NewError(ErrCodePermissionDenied, fmt.Sprintf("Permission denied for operation: %s", operation), err)
}

// NewInvalidConfigError creates an error for invalid configuration.
func NewInvalidConfigError(message string) *Error {
	return NewError(ErrCodeInvalidConfig, message, nil)
}
