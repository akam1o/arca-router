package main

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

// vtyshExecutor is the interface for executing vtysh commands
// This allows for dependency injection in tests
type vtyshExecutor func(ctx context.Context, command string) (string, error)

// VtyshError represents different types of vtysh execution errors
type VtyshError struct {
	Type    VtyshErrorType
	Message string
	Output  string
}

type VtyshErrorType int

const (
	VtyshErrorNotFound VtyshErrorType = iota
	VtyshErrorTimeout
	VtyshErrorExitCode
	VtyshErrorExec
)

func (e *VtyshError) Error() string {
	return e.Message
}

// runVtyshCommandReal is the real implementation of vtysh execution
func runVtyshCommandReal(ctx context.Context, command string) (string, error) {
	// Locate vtysh binary
	vtyshPath, err := exec.LookPath("vtysh")
	if err != nil {
		return "", &VtyshError{
			Type:    VtyshErrorNotFound,
			Message: fmt.Sprintf("vtysh not found in PATH: %v", err),
		}
	}

	// Create context with timeout
	const vtyshTimeout = 10 * time.Second
	cmdCtx, cancel := context.WithTimeout(ctx, vtyshTimeout)
	defer cancel()

	// Build vtysh command: vtysh -c '<command>'
	cmd := exec.CommandContext(cmdCtx, vtyshPath, "-c", command)

	// Capture stdout and stderr separately
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		// Check if it's a timeout
		if cmdCtx.Err() == context.DeadlineExceeded {
			return "", &VtyshError{
				Type:    VtyshErrorTimeout,
				Message: fmt.Sprintf("vtysh command timed out after %v", vtyshTimeout),
				Output:  stderr.String(), // Include any stderr output before timeout
			}
		}

		// Check exit code
		if exitErr, ok := err.(*exec.ExitError); ok {
			return stdout.String(), &VtyshError{
				Type:    VtyshErrorExitCode,
				Message: fmt.Sprintf("vtysh exited with code %d", exitErr.ExitCode()),
				Output:  stderr.String(),
			}
		}

		return "", &VtyshError{
			Type:    VtyshErrorExec,
			Message: fmt.Sprintf("failed to execute vtysh: %v", err),
			Output:  stderr.String(), // Include any stderr output from exec failure
		}
	}

	return stdout.String(), nil
}

// Default executor using real exec.Command
var defaultVtyshExecutor vtyshExecutor = runVtyshCommandReal

// RunVtyshCommand executes a vtysh command and returns the output
// This uses the default executor which can be overridden in tests
func RunVtyshCommand(ctx context.Context, command string, f *flags) (string, error) {
	debugLog(f, "Looking up vtysh path")
	output, err := runVtyshCommandWithExecutor(ctx, command, f, defaultVtyshExecutor)
	if err == nil {
		debugLog(f, "vtysh command successful (%d bytes output)", len(output))
	}
	return output, err
}

// runVtyshCommandWithExecutor allows dependency injection for testing
func runVtyshCommandWithExecutor(ctx context.Context, command string, f *flags, executor vtyshExecutor) (string, error) {
	debugLog(f, "Executing vtysh command: %s", command)
	return executor(ctx, command)
}
