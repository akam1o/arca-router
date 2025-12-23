package errors

import (
	"errors"
	"fmt"
)

// Error represents an arca-router error with context
type Error struct {
	// Code is the error code (e.g., "CONFIG_PARSE_ERROR")
	Code string
	// Message is the human-readable error message
	Message string
	// Cause describes why the error occurred
	Cause string
	// Action suggests what the user should do
	Action string
	// Underlying is the wrapped error
	Underlying error
}

// Error implements the error interface
func (e *Error) Error() string {
	if e.Underlying != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Underlying)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap implements errors.Unwrap
func (e *Error) Unwrap() error {
	return e.Underlying
}

// New creates a new Error
func New(code, message, cause, action string) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Cause:   cause,
		Action:  action,
	}
}

// Wrap wraps an existing error with additional context
func Wrap(err error, code, message, cause, action string) *Error {
	return &Error{
		Code:       code,
		Message:    message,
		Cause:      cause,
		Action:     action,
		Underlying: err,
	}
}

// Common error codes
const (
	// Configuration errors
	ErrCodeConfigNotFound   = "CONFIG_NOT_FOUND"
	ErrCodeConfigParseError = "CONFIG_PARSE_ERROR"
	ErrCodeConfigValidation = "CONFIG_VALIDATION_ERROR"
	ErrCodeConfigPermission = "CONFIG_PERMISSION_ERROR"

	// Hardware errors
	ErrCodeHardwareNotFound   = "HARDWARE_NOT_FOUND"
	ErrCodeHardwareParseError = "HARDWARE_PARSE_ERROR"
	ErrCodePCINotFound        = "PCI_DEVICE_NOT_FOUND"
	ErrCodePCIInvalid         = "PCI_ADDRESS_INVALID"

	// VPP errors
	ErrCodeVPPConnection = "VPP_CONNECTION_ERROR"
	ErrCodeVPPTimeout    = "VPP_TIMEOUT"
	ErrCodeVPPAPIError   = "VPP_API_ERROR"
	ErrCodeVPPNotRunning = "VPP_NOT_RUNNING"
	ErrCodeVPPOperation  = "VPP_OPERATION_ERROR"

	// System errors
	ErrCodePermissionDenied = "PERMISSION_DENIED"
	ErrCodeSystemError      = "SYSTEM_ERROR"
)

// Common error constructors

// ConfigNotFound creates a config not found error
func ConfigNotFound(path string) *Error {
	return New(
		ErrCodeConfigNotFound,
		fmt.Sprintf("Configuration file not found: %s", path),
		"The specified configuration file does not exist",
		"Check the file path and ensure the configuration file has been created",
	)
}

// ConfigParseError creates a config parse error
func ConfigParseError(path string, err error) *Error {
	return Wrap(
		err,
		ErrCodeConfigParseError,
		fmt.Sprintf("Failed to parse configuration file: %s", path),
		"The configuration file contains invalid syntax or format",
		"Review the configuration file syntax and fix any errors",
	)
}

// PCINotFound creates a PCI device not found error
func PCINotFound(pciAddr string) *Error {
	return New(
		ErrCodePCINotFound,
		fmt.Sprintf("PCI device not found: %s", pciAddr),
		"The specified PCI address does not exist on this system",
		"Verify the PCI address with 'lspci' command and update hardware.yaml",
	)
}

// VPPConnectionError creates a VPP connection error
func VPPConnectionError(err error) *Error {
	return Wrap(
		err,
		ErrCodeVPPConnection,
		"Failed to connect to VPP",
		"VPP service may not be running or the socket is not accessible",
		"Ensure VPP is running with 'systemctl status vpp' and check socket permissions",
	)
}

// Is reports whether any error in err's chain matches target
func Is(err, target error) bool {
	return errors.Is(err, target)
}

// As finds the first error in err's chain that matches target
func As(err error, target interface{}) bool {
	return errors.As(err, target)
}
