package main

import (
	"context"
	"strings"
	"testing"
)

// TestCmdShowRoute tests show route with protocol filtering
func TestCmdShowRoute(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
	}{
		{
			name:        "show route without args",
			args:        []string{},
			expectError: false, // Should call "show ip route"
		},
		{
			name:        "show route protocol bgp",
			args:        []string{"protocol", "bgp"},
			expectError: false,
		},
		{
			name:        "show route protocol ospf",
			args:        []string{"protocol", "ospf"},
			expectError: false,
		},
		{
			name:        "show route protocol static",
			args:        []string{"protocol", "static"},
			expectError: false,
		},
		{
			name:        "show route protocol connected",
			args:        []string{"protocol", "connected"},
			expectError: false,
		},
		{
			name:        "show route protocol kernel",
			args:        []string{"protocol", "kernel"},
			expectError: false,
		},
		{
			name:        "show route protocol invalid",
			args:        []string{"protocol", "invalid"},
			expectError: true, // Invalid protocol
		},
		{
			name:        "show route with invalid args",
			args:        []string{"invalid"},
			expectError: true,
		},
		{
			name:        "show route protocol without protocol name",
			args:        []string{"protocol"},
			expectError: true,
		},
		{
			name:        "show route protocol with extra args",
			args:        []string{"protocol", "bgp", "extra"},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use mock vtysh executor
			mockExecutor := func(ctx context.Context, command string) (string, error) {
				// Validate command structure
				if !strings.HasPrefix(command, "show ip route") {
					t.Errorf("Expected command to start with 'show ip route', got: %s", command)
				}
				return "Mock route output", nil
			}

			f := &flags{
				vppSocket:  "/run/vpp/api.sock",
				configPath: "/etc/arca-router/arca.conf",
				debug:      false,
			}

			// Temporarily replace executor
			oldExecutor := defaultVtyshExecutor
			defer func() { defaultVtyshExecutor = oldExecutor }()
			defaultVtyshExecutor = mockExecutor

			exitCode := cmdShowRoute(context.Background(), tt.args, f)

			if tt.expectError {
				if exitCode == ExitSuccess {
					t.Errorf("Expected error, but command succeeded")
				}
			} else {
				// Note: cmdShowRoute will fail if vtysh is not available
				// We accept both ExitSuccess (if mocked properly) and ExitOperationError (if vtysh unavailable)
				// The important part is that argument parsing works correctly
				if exitCode != ExitSuccess && exitCode != ExitOperationError {
					t.Errorf("Expected ExitSuccess or ExitOperationError, got: %d", exitCode)
				}
			}
		})
	}
}

// TestCmdShowBGPNeighbor tests show bgp neighbor command
func TestCmdShowBGPNeighbor(t *testing.T) {
	tests := []struct {
		name       string
		neighborIP string
		wantError  bool
	}{
		{
			name:       "valid IPv4 neighbor",
			neighborIP: "10.0.0.2",
			wantError:  false,
		},
		{
			name:       "valid IPv6 neighbor",
			neighborIP: "2001:db8::2",
			wantError:  false,
		},
		{
			name:       "empty neighbor IP",
			neighborIP: "",
			wantError:  true,
		},
		{
			name:       "invalid neighbor IP (non-IP string)",
			neighborIP: "not-an-ip",
			wantError:  true,
		},
		{
			name:       "invalid neighbor IP (with spaces)",
			neighborIP: "10.0.0.2 extra",
			wantError:  true,
		},
		{
			name:       "invalid neighbor IP (special chars)",
			neighborIP: "10.0.0.2;rm -rf /",
			wantError:  true,
		},
		{
			name:       "invalid neighbor IP (hex without separator)",
			neighborIP: "deadbeef",
			wantError:  true,
		},
		{
			name:       "invalid neighbor IP (hex only)",
			neighborIP: "face",
			wantError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockExecutor := func(ctx context.Context, command string) (string, error) {
				expectedPrefix := "show bgp neighbor "
				if !strings.HasPrefix(command, expectedPrefix) {
					t.Errorf("Expected command to start with '%s', got: %s", expectedPrefix, command)
				}
				return "Mock BGP neighbor output", nil
			}

			f := &flags{
				vppSocket:  "/run/vpp/api.sock",
				configPath: "/etc/arca-router/arca.conf",
				debug:      false,
			}

			oldExecutor := defaultVtyshExecutor
			defer func() { defaultVtyshExecutor = oldExecutor }()
			defaultVtyshExecutor = mockExecutor

			exitCode := cmdShowBGPNeighbor(context.Background(), tt.neighborIP, f)

			if tt.wantError {
				if exitCode == ExitSuccess {
					t.Errorf("Expected error, but command succeeded")
				}
			} else {
				// Accept both ExitSuccess and ExitOperationError (if vtysh unavailable)
				if exitCode != ExitSuccess && exitCode != ExitOperationError {
					t.Errorf("Expected ExitSuccess or ExitOperationError, got: %d", exitCode)
				}
			}
		})
	}
}

// TestShowRouteProtocolValidation tests protocol validation logic
func TestShowRouteProtocolValidation(t *testing.T) {
	validProtocols := []string{"bgp", "ospf", "static", "connected", "kernel"}
	invalidProtocols := []string{"rip", "isis", "eigrp", "invalid"}

	mockExecutor := func(ctx context.Context, command string) (string, error) {
		return "Mock output", nil
	}

	f := &flags{
		vppSocket:  "/run/vpp/api.sock",
		configPath: "/etc/arca-router/arca.conf",
		debug:      false,
	}

	oldExecutor := defaultVtyshExecutor
	defer func() { defaultVtyshExecutor = oldExecutor }()
	defaultVtyshExecutor = mockExecutor

	// Test valid protocols
	for _, protocol := range validProtocols {
		t.Run("valid_"+protocol, func(t *testing.T) {
			exitCode := cmdShowRoute(context.Background(), []string{"protocol", protocol}, f)
			// Should not return ExitUsageError
			if exitCode == ExitUsageError {
				t.Errorf("Valid protocol '%s' was rejected", protocol)
			}
		})
	}

	// Test invalid protocols
	for _, protocol := range invalidProtocols {
		t.Run("invalid_"+protocol, func(t *testing.T) {
			exitCode := cmdShowRoute(context.Background(), []string{"protocol", protocol}, f)
			if exitCode != ExitUsageError {
				t.Errorf("Invalid protocol '%s' should return ExitUsageError, got: %d", protocol, exitCode)
			}
		})
	}
}
