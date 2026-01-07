package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRunVtyshCommandWithExecutor_Success(t *testing.T) {
	mockExecutor := func(ctx context.Context, command string) (string, error) {
		return "BGP router identifier 10.0.0.1\nLocal AS 65000", nil
	}

	f := &flags{debug: false}
	output, err := runVtyshCommandWithExecutor(context.Background(), "show bgp summary", f, mockExecutor)

	if err != nil {
		t.Errorf("runVtyshCommandWithExecutor() error = %v, want nil", err)
	}

	if output != "BGP router identifier 10.0.0.1\nLocal AS 65000" {
		t.Errorf("runVtyshCommandWithExecutor() output = %q, want BGP output", output)
	}
}

func TestRunVtyshCommandWithExecutor_NotFound(t *testing.T) {
	mockExecutor := func(ctx context.Context, command string) (string, error) {
		return "", &VtyshError{
			Type:    VtyshErrorNotFound,
			Message: "vtysh not found in PATH",
		}
	}

	f := &flags{debug: false}
	_, err := runVtyshCommandWithExecutor(context.Background(), "show bgp summary", f, mockExecutor)

	if err == nil {
		t.Error("runVtyshCommandWithExecutor() expected error, got nil")
	}

	vtyshErr, ok := err.(*VtyshError)
	if !ok {
		t.Errorf("runVtyshCommandWithExecutor() error type = %T, want *VtyshError", err)
		return
	}

	if vtyshErr.Type != VtyshErrorNotFound {
		t.Errorf("VtyshError.Type = %v, want VtyshErrorNotFound", vtyshErr.Type)
	}
}

func TestRunVtyshCommandWithExecutor_Timeout(t *testing.T) {
	mockExecutor := func(ctx context.Context, command string) (string, error) {
		return "", &VtyshError{
			Type:    VtyshErrorTimeout,
			Message: "vtysh command timed out after 10s",
			Output:  "partial output",
		}
	}

	f := &flags{debug: false}
	_, err := runVtyshCommandWithExecutor(context.Background(), "show bgp summary", f, mockExecutor)

	if err == nil {
		t.Error("runVtyshCommandWithExecutor() expected timeout error, got nil")
	}

	vtyshErr, ok := err.(*VtyshError)
	if !ok {
		t.Errorf("runVtyshCommandWithExecutor() error type = %T, want *VtyshError", err)
		return
	}

	if vtyshErr.Type != VtyshErrorTimeout {
		t.Errorf("VtyshError.Type = %v, want VtyshErrorTimeout", vtyshErr.Type)
	}
}

func TestRunVtyshCommandWithExecutor_ExitCode(t *testing.T) {
	mockExecutor := func(ctx context.Context, command string) (string, error) {
		return "partial output", &VtyshError{
			Type:    VtyshErrorExitCode,
			Message: "vtysh exited with code 1",
			Output:  "% Unknown command: invalid",
		}
	}

	f := &flags{debug: false}
	output, err := runVtyshCommandWithExecutor(context.Background(), "invalid command", f, mockExecutor)

	if err == nil {
		t.Error("runVtyshCommandWithExecutor() expected exit code error, got nil")
	}

	// Even on exit code error, partial output should be returned
	if output != "partial output" {
		t.Errorf("runVtyshCommandWithExecutor() output = %q, want partial output", output)
	}

	vtyshErr, ok := err.(*VtyshError)
	if !ok {
		t.Errorf("runVtyshCommandWithExecutor() error type = %T, want *VtyshError", err)
		return
	}

	if vtyshErr.Type != VtyshErrorExitCode {
		t.Errorf("VtyshError.Type = %v, want VtyshErrorExitCode", vtyshErr.Type)
	}
}

func TestRunVtyshCommandWithExecutor_ExecError(t *testing.T) {
	mockExecutor := func(ctx context.Context, command string) (string, error) {
		return "", &VtyshError{
			Type:    VtyshErrorExec,
			Message: "failed to execute vtysh: permission denied",
		}
	}

	f := &flags{debug: false}
	_, err := runVtyshCommandWithExecutor(context.Background(), "show bgp summary", f, mockExecutor)

	if err == nil {
		t.Error("runVtyshCommandWithExecutor() expected exec error, got nil")
	}

	vtyshErr, ok := err.(*VtyshError)
	if !ok {
		t.Errorf("runVtyshCommandWithExecutor() error type = %T, want *VtyshError", err)
		return
	}

	if vtyshErr.Type != VtyshErrorExec {
		t.Errorf("VtyshError.Type = %v, want VtyshErrorExec", vtyshErr.Type)
	}
}

func TestVtyshError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *VtyshError
		want string
	}{
		{
			name: "not found",
			err: &VtyshError{
				Type:    VtyshErrorNotFound,
				Message: "vtysh not found in PATH",
			},
			want: "vtysh not found in PATH",
		},
		{
			name: "timeout",
			err: &VtyshError{
				Type:    VtyshErrorTimeout,
				Message: "vtysh command timed out after 10s",
			},
			want: "vtysh command timed out after 10s",
		},
		{
			name: "exit code",
			err: &VtyshError{
				Type:    VtyshErrorExitCode,
				Message: "vtysh exited with code 1",
			},
			want: "vtysh exited with code 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got != tt.want {
				t.Errorf("VtyshError.Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRunVtyshCommandReal_Integration(t *testing.T) {
	// Skip if vtysh is not available (integration test)
	t.Skip("Skipping integration test - requires vtysh installed")

	ctx := context.Background()
	output, err := runVtyshCommandReal(ctx, "show version")

	if err != nil {
		vtyshErr, ok := err.(*VtyshError)
		if ok && vtyshErr.Type == VtyshErrorNotFound {
			t.Skip("vtysh not found in PATH")
		}
		t.Errorf("runVtyshCommandReal() error = %v", err)
		return
	}

	if output == "" {
		t.Error("runVtyshCommandReal() returned empty output")
	}
}

func TestRunVtyshCommandWithExecutor_ContextCancellation(t *testing.T) {
	mockExecutor := func(ctx context.Context, command string) (string, error) {
		// Simulate context cancellation
		select {
		case <-ctx.Done():
			return "", &VtyshError{
				Type:    VtyshErrorTimeout,
				Message: "context cancelled",
			}
		case <-time.After(100 * time.Millisecond):
			return "output", nil
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	f := &flags{debug: false}
	_, err := runVtyshCommandWithExecutor(ctx, "show bgp summary", f, mockExecutor)

	if err == nil {
		t.Error("runVtyshCommandWithExecutor() expected context cancellation error, got nil")
	}
}

func TestRunVtyshCommandWithExecutor_MultipleCommands(t *testing.T) {
	commands := []string{
		"show bgp summary",
		"show ospf neighbor",
		"show ip route",
	}

	expectedOutputs := map[string]string{
		"show bgp summary":   "BGP summary output",
		"show ospf neighbor": "OSPF neighbor output",
		"show ip route":      "IP route output",
	}

	mockExecutor := func(ctx context.Context, command string) (string, error) {
		if output, ok := expectedOutputs[command]; ok {
			return output, nil
		}
		return "", errors.New("unknown command")
	}

	f := &flags{debug: false}
	for _, cmd := range commands {
		output, err := runVtyshCommandWithExecutor(context.Background(), cmd, f, mockExecutor)
		if err != nil {
			t.Errorf("runVtyshCommandWithExecutor(%q) error = %v", cmd, err)
		}
		if output != expectedOutputs[cmd] {
			t.Errorf("runVtyshCommandWithExecutor(%q) output = %q, want %q", cmd, output, expectedOutputs[cmd])
		}
	}
}

func TestVtyshError_Types(t *testing.T) {
	types := []VtyshErrorType{
		VtyshErrorNotFound,
		VtyshErrorTimeout,
		VtyshErrorExitCode,
		VtyshErrorExec,
	}

	// Verify all types are distinct
	seen := make(map[VtyshErrorType]bool)
	for _, typ := range types {
		if seen[typ] {
			t.Errorf("Duplicate VtyshErrorType value: %v", typ)
		}
		seen[typ] = true
	}

	if len(seen) != 4 {
		t.Errorf("Expected 4 distinct VtyshErrorType values, got %d", len(seen))
	}
}
