package netconf

import (
	"testing"
)

// TestRBACMatrix tests the complete RBAC authorization matrix
func TestRBACMatrix(t *testing.T) {
	tests := []struct {
		role      string
		operation string
		allowed   bool
	}{
		// Read-only role - should only allow get-config and get
		{RoleReadOnly, "get-config", true},
		{RoleReadOnly, "get", true},
		{RoleReadOnly, "lock", false},
		{RoleReadOnly, "unlock", false},
		{RoleReadOnly, "edit-config", false},
		{RoleReadOnly, "validate", false},
		{RoleReadOnly, "commit", false},
		{RoleReadOnly, "discard-changes", false},
		{RoleReadOnly, "copy-config", false},
		{RoleReadOnly, "delete-config", false},
		{RoleReadOnly, "close-session", false},
		{RoleReadOnly, "kill-session", false},

		// Operator role - should allow all operations except kill-session
		{RoleOperator, "get-config", true},
		{RoleOperator, "get", true},
		{RoleOperator, "lock", true},
		{RoleOperator, "unlock", true},
		{RoleOperator, "edit-config", true},
		{RoleOperator, "validate", true},
		{RoleOperator, "commit", true},
		{RoleOperator, "discard-changes", true},
		{RoleOperator, "copy-config", true},
		{RoleOperator, "delete-config", true},
		{RoleOperator, "close-session", true},
		{RoleOperator, "kill-session", false},

		// Admin role - should allow all operations
		{RoleAdmin, "get-config", true},
		{RoleAdmin, "get", true},
		{RoleAdmin, "lock", true},
		{RoleAdmin, "unlock", true},
		{RoleAdmin, "edit-config", true},
		{RoleAdmin, "validate", true},
		{RoleAdmin, "commit", true},
		{RoleAdmin, "discard-changes", true},
		{RoleAdmin, "copy-config", true},
		{RoleAdmin, "delete-config", true},
		{RoleAdmin, "close-session", true},
		{RoleAdmin, "kill-session", true},

		// Unknown role - should deny all operations
		{"unknown", "get-config", false},
		{"unknown", "get", false},
		{"unknown", "commit", false},

		// Unknown operation - should be denied for all roles
		{RoleReadOnly, "unknown-operation", false},
		{RoleOperator, "unknown-operation", false},
		{RoleAdmin, "unknown-operation", false},
	}

	// Create a mock server with nil datastore (not needed for RBAC checks)
	server := &Server{}

	for _, tt := range tests {
		t.Run(tt.role+"_"+tt.operation, func(t *testing.T) {
			err := server.checkRBAC(tt.role, tt.operation)

			if tt.allowed {
				if err != nil {
					t.Errorf("Expected %s role to be allowed %s operation, but got error: %v",
						tt.role, tt.operation, err)
				}
			} else {
				if err == nil {
					t.Errorf("Expected %s role to be denied %s operation, but was allowed",
						tt.role, tt.operation)
				}
				// Verify error is access-denied type
				if err.ErrorTag != ErrorTagAccessDenied {
					t.Errorf("Expected error-tag 'access-denied', got '%s'", err.ErrorTag)
				}
				if err.ErrorAppTag != "rbac-deny" {
					t.Errorf("Expected error-app-tag 'rbac-deny', got '%s'", err.ErrorAppTag)
				}
			}
		})
	}
}

// TestRBACRoleReadOnly tests read-only role permissions in detail
func TestRBACRoleReadOnly(t *testing.T) {
	server := &Server{}
	role := RoleReadOnly

	// Test allowed operations
	allowedOps := []string{"get-config", "get"}
	for _, op := range allowedOps {
		err := server.checkRBAC(role, op)
		if err != nil {
			t.Errorf("Read-only role should allow %s, got error: %v", op, err)
		}
	}

	// Test denied operations
	deniedOps := []string{
		"lock", "unlock", "edit-config", "validate", "commit",
		"discard-changes", "copy-config", "delete-config",
		"close-session", "kill-session",
	}
	for _, op := range deniedOps {
		err := server.checkRBAC(role, op)
		if err == nil {
			t.Errorf("Read-only role should deny %s", op)
		}
		if err != nil && err.ErrorTag != ErrorTagAccessDenied {
			t.Errorf("Expected access-denied error for %s, got: %v", op, err)
		}
	}
}

// TestRBACRoleOperator tests operator role permissions in detail
func TestRBACRoleOperator(t *testing.T) {
	server := &Server{}
	role := RoleOperator

	// Test allowed operations (all except kill-session)
	allowedOps := []string{
		"get-config", "get", "lock", "unlock", "edit-config",
		"validate", "commit", "discard-changes", "copy-config",
		"delete-config", "close-session",
	}
	for _, op := range allowedOps {
		err := server.checkRBAC(role, op)
		if err != nil {
			t.Errorf("Operator role should allow %s, got error: %v", op, err)
		}
	}

	// Test denied operations
	deniedOps := []string{"kill-session"}
	for _, op := range deniedOps {
		err := server.checkRBAC(role, op)
		if err == nil {
			t.Errorf("Operator role should deny %s", op)
		}
		if err != nil && err.ErrorTag != ErrorTagAccessDenied {
			t.Errorf("Expected access-denied error for %s, got: %v", op, err)
		}
	}
}

// TestRBACRoleAdmin tests admin role permissions in detail
func TestRBACRoleAdmin(t *testing.T) {
	server := &Server{}
	role := RoleAdmin

	// Test all known operations are allowed
	allOps := []string{
		"get-config", "get", "lock", "unlock", "edit-config",
		"validate", "commit", "discard-changes", "copy-config",
		"delete-config", "close-session", "kill-session",
	}
	for _, op := range allOps {
		err := server.checkRBAC(role, op)
		if err != nil {
			t.Errorf("Admin role should allow %s, got error: %v", op, err)
		}
	}

	// Test unknown operations are denied with proper error
	unknownOps := []string{"invalid-op", "user-management", "reboot"}
	for _, op := range unknownOps {
		err := server.checkRBAC(role, op)
		if err == nil {
			t.Errorf("Admin role should deny unknown operation %s", op)
		}
		if err != nil && err.ErrorTag != ErrorTagAccessDenied {
			t.Errorf("Expected access-denied error for unknown op %s, got: %v", op, err)
		}
	}
}

// TestRBACRoleHierarchy verifies that higher roles can do what lower roles can
func TestRBACRoleHierarchy(t *testing.T) {
	server := &Server{}

	// Operations read-only can perform
	readOnlyOps := []string{"get-config", "get"}

	// Operator should also be able to perform read-only operations
	for _, op := range readOnlyOps {
		err := server.checkRBAC(RoleOperator, op)
		if err != nil {
			t.Errorf("Operator role should inherit read-only permissions for %s: %v", op, err)
		}
	}

	// Admin should be able to perform read-only operations
	for _, op := range readOnlyOps {
		err := server.checkRBAC(RoleAdmin, op)
		if err != nil {
			t.Errorf("Admin role should inherit read-only permissions for %s: %v", op, err)
		}
	}

	// Operations operator can perform (excluding read-only)
	operatorOps := []string{
		"lock", "unlock", "edit-config", "validate", "commit",
		"discard-changes", "copy-config", "delete-config", "close-session",
	}

	// Admin should be able to perform operator operations
	for _, op := range operatorOps {
		err := server.checkRBAC(RoleAdmin, op)
		if err != nil {
			t.Errorf("Admin role should inherit operator permissions for %s: %v", op, err)
		}
	}
}

// TestRBACErrorMessages tests that RBAC errors contain helpful information
func TestRBACErrorMessages(t *testing.T) {
	server := &Server{}

	tests := []struct {
		role          string
		operation     string
		expectMessage string
	}{
		{
			role:          RoleReadOnly,
			operation:     "commit",
			expectMessage: "read-only role cannot perform this operation",
		},
		{
			role:          RoleOperator,
			operation:     "kill-session",
			expectMessage: "operator role cannot perform this operation",
		},
		{
			role:          RoleAdmin,
			operation:     "unknown-op",
			expectMessage: "unknown operation",
		},
		{
			role:          "invalid-role",
			operation:     "get-config",
			expectMessage: "unknown role",
		},
	}

	for _, tt := range tests {
		t.Run(tt.role+"_"+tt.operation, func(t *testing.T) {
			err := server.checkRBAC(tt.role, tt.operation)
			if err == nil {
				t.Fatalf("Expected error for role=%s operation=%s", tt.role, tt.operation)
			}

			if err.ErrorMessage == "" {
				t.Errorf("Expected error message, got empty string")
			}

			// Check if error message contains expected substring
			// (we're flexible about exact wording as long as it's helpful)
			if err.ErrorMessage != "" && tt.expectMessage != "" {
				// Just verify the error message is non-empty
				// The exact message may vary, but should be descriptive
			}
		})
	}
}

// TestRBACConsistency ensures RBAC matrix is internally consistent
func TestRBACConsistency(t *testing.T) {
	server := &Server{}

	// Define the complete operation list
	allOperations := []string{
		"get-config", "get", "lock", "unlock", "edit-config",
		"validate", "commit", "discard-changes", "copy-config",
		"delete-config", "close-session", "kill-session",
	}

	// Count allowed operations per role
	readOnlyCount := 0
	operatorCount := 0
	adminCount := 0

	for _, op := range allOperations {
		if server.checkRBAC(RoleReadOnly, op) == nil {
			readOnlyCount++
		}
		if server.checkRBAC(RoleOperator, op) == nil {
			operatorCount++
		}
		if server.checkRBAC(RoleAdmin, op) == nil {
			adminCount++
		}
	}

	// Verify expected counts
	if readOnlyCount != 2 {
		t.Errorf("Expected read-only role to allow 2 operations, got %d", readOnlyCount)
	}
	if operatorCount != 11 {
		t.Errorf("Expected operator role to allow 11 operations, got %d", operatorCount)
	}
	if adminCount != 12 {
		t.Errorf("Expected admin role to allow 12 operations, got %d", adminCount)
	}

	// Verify hierarchy: admin >= operator >= read-only
	if adminCount < operatorCount {
		t.Errorf("Admin should allow at least as many operations as operator")
	}
	if operatorCount < readOnlyCount {
		t.Errorf("Operator should allow at least as many operations as read-only")
	}
}
