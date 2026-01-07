package integration

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/akam1o/arca-router/pkg/auth"
	"github.com/akam1o/arca-router/pkg/logger"
	"github.com/akam1o/arca-router/pkg/netconf"
)

// TestPasswordAuthentication tests password-based authentication
func TestPasswordAuthentication(t *testing.T) {
	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_users.db")

	log := logger.New("test", logger.DefaultConfig())
	userDB, err := netconf.NewUserDatabase(dbPath, log)
	if err != nil {
		t.Fatalf("Failed to create user database: %v", err)
	}
	t.Cleanup(func() {
		if err := userDB.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	})

	// Create test user with password
	password := "test-password-123"
	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	err = userDB.CreateUser("testuser", passwordHash, netconf.RoleAdmin)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Test successful authentication
	user, reason, err := userDB.VerifyPasswordWithReason("testuser", password)
	if err != nil {
		t.Errorf("Authentication should succeed: %v (reason: %s)", err, reason)
	}
	if user == nil {
		t.Errorf("Expected user object, got nil")
	}
	if user != nil && user.Username != "testuser" {
		t.Errorf("Expected username 'testuser', got '%s'", user.Username)
	}
	if user != nil && user.Role != netconf.RoleAdmin {
		t.Errorf("Expected role 'admin', got '%s'", user.Role)
	}

	// Test wrong password
	_, reason, err = userDB.VerifyPasswordWithReason("testuser", "wrong-password")
	if err == nil {
		t.Errorf("Authentication should fail with wrong password")
	}
	if reason != "invalid_password" {
		t.Errorf("Expected reason 'invalid_password', got '%s'", reason)
	}

	// Test non-existent user
	_, reason, err = userDB.VerifyPasswordWithReason("nonexistent", password)
	if err == nil {
		t.Errorf("Authentication should fail for non-existent user")
	}
	if reason != "user_not_found" {
		t.Errorf("Expected reason 'user_not_found', got '%s'", reason)
	}

	// Test disabled user
	err = userDB.UpdateUser("testuser", "", "", false)
	if err != nil {
		t.Fatalf("Failed to disable user: %v", err)
	}

	_, reason, err = userDB.VerifyPasswordWithReason("testuser", password)
	if err == nil {
		t.Errorf("Authentication should fail for disabled user")
	}
	if reason != "user_disabled" {
		t.Errorf("Expected reason 'user_disabled', got '%s'", reason)
	}
}

// TestPublicKeyAuthentication tests public key-based authentication
func TestPublicKeyAuthentication(t *testing.T) {
	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_users.db")

	log := logger.New("test", logger.DefaultConfig())
	userDB, err := netconf.NewUserDatabase(dbPath, log)
	if err != nil {
		t.Fatalf("Failed to create user database: %v", err)
	}
	t.Cleanup(func() {
		if err := userDB.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	})

	// Create test user (password required but won't be used for pubkey auth)
	passwordHash, err := auth.HashPassword("dummy-password")
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	err = userDB.CreateUser("testuser", passwordHash, netconf.RoleOperator)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Generate ED25519 key pair
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key pair: %v", err)
	}

	sshPublicKey, err := ssh.NewPublicKey(privateKey.Public())
	if err != nil {
		t.Fatalf("Failed to create SSH public key: %v", err)
	}

	// Parse the public key to get algorithm, key data, and fingerprint
	authorizedKey := string(ssh.MarshalAuthorizedKey(sshPublicKey))
	parsedKey, err := auth.ParsePublicKey(authorizedKey + " testuser@example.com")
	if err != nil {
		t.Fatalf("Failed to parse public key: %v", err)
	}

	// Add public key to user database
	err = userDB.AddPublicKey("testuser", parsedKey.Algorithm, parsedKey.KeyData, parsedKey.Fingerprint, parsedKey.Comment)
	if err != nil {
		t.Fatalf("Failed to add public key: %v", err)
	}

	// Test successful authentication
	user, reason, err := userDB.VerifyPublicKeyAuth("testuser", parsedKey.KeyData)
	if err != nil {
		t.Errorf("Authentication should succeed: %v (reason: %s)", err, reason)
	}
	if user == nil {
		t.Errorf("Expected user object, got nil")
	}
	if user != nil && user.Username != "testuser" {
		t.Errorf("Expected username 'testuser', got '%s'", user.Username)
	}
	if user != nil && user.Role != netconf.RoleOperator {
		t.Errorf("Expected role 'operator', got '%s'", user.Role)
	}

	// Test wrong public key
	_, privateKey2, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate second key pair: %v", err)
	}

	sshPublicKey2, err := ssh.NewPublicKey(privateKey2.Public())
	if err != nil {
		t.Fatalf("Failed to create second SSH public key: %v", err)
	}

	wrongKeyData := base64.StdEncoding.EncodeToString(sshPublicKey2.Marshal())
	_, reason, err = userDB.VerifyPublicKeyAuth("testuser", wrongKeyData)
	if err == nil {
		t.Errorf("Authentication should fail with wrong public key")
	}
	if reason != "key_not_found" {
		t.Errorf("Expected reason 'key_not_found', got '%s'", reason)
	}

	// Test non-existent user
	_, reason, err = userDB.VerifyPublicKeyAuth("nonexistent", parsedKey.KeyData)
	if err == nil {
		t.Errorf("Authentication should fail for non-existent user")
	}
	if reason != "user_not_found" {
		t.Errorf("Expected reason 'user_not_found', got '%s'", reason)
	}

	// Test disabled user
	err = userDB.UpdateUser("testuser", "", "", false)
	if err != nil {
		t.Fatalf("Failed to disable user: %v", err)
	}

	_, reason, err = userDB.VerifyPublicKeyAuth("testuser", parsedKey.KeyData)
	if err == nil {
		t.Errorf("Authentication should fail for disabled user")
	}
	if reason != "user_disabled" {
		t.Errorf("Expected reason 'user_disabled', got '%s'", reason)
	}

	// Re-enable user for next test
	err = userDB.UpdateUser("testuser", "", "", true)
	if err != nil {
		t.Fatalf("Failed to re-enable user: %v", err)
	}

	// Test disabled key
	err = userDB.UpdatePublicKeyStatus(parsedKey.Fingerprint, false)
	if err != nil {
		t.Fatalf("Failed to disable key: %v", err)
	}

	_, reason, err = userDB.VerifyPublicKeyAuth("testuser", parsedKey.KeyData)
	if err == nil {
		t.Errorf("Authentication should fail with disabled key")
	}
	if reason != "key_not_found" {
		t.Errorf("Expected reason 'key_not_found', got '%s'", reason)
	}
}

// TestPublicKeyManagement tests public key CRUD operations
func TestPublicKeyManagement(t *testing.T) {
	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_users.db")

	log := logger.New("test", logger.DefaultConfig())
	userDB, err := netconf.NewUserDatabase(dbPath, log)
	if err != nil {
		t.Fatalf("Failed to create user database: %v", err)
	}
	t.Cleanup(func() {
		if err := userDB.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	})

	// Create test user
	passwordHash, err := auth.HashPassword("test-password")
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	err = userDB.CreateUser("testuser", passwordHash, netconf.RoleAdmin)
	if err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Generate two key pairs
	_, privateKey1, _ := ed25519.GenerateKey(rand.Reader)
	sshPublicKey1, _ := ssh.NewPublicKey(privateKey1.Public())
	authorizedKey1 := string(ssh.MarshalAuthorizedKey(sshPublicKey1))
	parsedKey1, _ := auth.ParsePublicKey(authorizedKey1 + " key1@example.com")

	_, privateKey2, _ := ed25519.GenerateKey(rand.Reader)
	sshPublicKey2, _ := ssh.NewPublicKey(privateKey2.Public())
	authorizedKey2 := string(ssh.MarshalAuthorizedKey(sshPublicKey2))
	parsedKey2, _ := auth.ParsePublicKey(authorizedKey2 + " key2@example.com")

	// Add two keys
	err = userDB.AddPublicKey("testuser", parsedKey1.Algorithm, parsedKey1.KeyData, parsedKey1.Fingerprint, parsedKey1.Comment)
	if err != nil {
		t.Fatalf("Failed to add first key: %v", err)
	}

	err = userDB.AddPublicKey("testuser", parsedKey2.Algorithm, parsedKey2.KeyData, parsedKey2.Fingerprint, parsedKey2.Comment)
	if err != nil {
		t.Fatalf("Failed to add second key: %v", err)
	}

	// List keys
	keys, err := userDB.ListPublicKeys("testuser")
	if err != nil {
		t.Fatalf("Failed to list keys: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("Expected 2 keys, got %d", len(keys))
	}

	// Verify key details
	for _, key := range keys {
		if key.Username != "testuser" {
			t.Errorf("Expected username 'testuser', got '%s'", key.Username)
		}
		if key.Algorithm != "ssh-ed25519" {
			t.Errorf("Expected algorithm 'ssh-ed25519', got '%s'", key.Algorithm)
		}
		if !key.Enabled {
			t.Errorf("Expected key to be enabled")
		}
	}

	// Get specific key
	key, err := userDB.GetPublicKey(parsedKey1.Fingerprint)
	if err != nil {
		t.Fatalf("Failed to get key: %v", err)
	}
	if key.Comment != "key1@example.com" {
		t.Errorf("Expected comment 'key1@example.com', got '%s'", key.Comment)
	}

	// Remove one key
	err = userDB.RemovePublicKey(parsedKey1.Fingerprint)
	if err != nil {
		t.Fatalf("Failed to remove key: %v", err)
	}

	// List keys again
	keys, err = userDB.ListPublicKeys("testuser")
	if err != nil {
		t.Fatalf("Failed to list keys after removal: %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("Expected 1 key after removal, got %d", len(keys))
	}
	if keys[0].Comment != "key2@example.com" {
		t.Errorf("Expected remaining key to be key2, got %s", keys[0].Comment)
	}

	// Try to remove non-existent key
	err = userDB.RemovePublicKey("SHA256:nonexistent")
	if err == nil {
		t.Errorf("Expected error when removing non-existent key")
	}
}

// TestAuthAuditLogging tests that authentication events are properly logged
func TestAuthAuditLogging(t *testing.T) {
	// Create temporary database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_users.db")

	log := logger.New("test", logger.DefaultConfig())
	userDB, err := netconf.NewUserDatabase(dbPath, log)
	if err != nil {
		t.Fatalf("Failed to create user database: %v", err)
	}
	t.Cleanup(func() {
		if err := userDB.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	})

	// Create test user
	password := "test-password"
	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}
	if err := userDB.CreateUser("testuser", passwordHash, netconf.RoleAdmin); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Test password auth logging (success)
	if _, _, err := userDB.VerifyPasswordWithReason("testuser", password); err != nil {
		t.Fatalf("VerifyPasswordWithReason should succeed: %v", err)
	}
	// Verify log was called (in real implementation, check log output)

	// Test password auth logging (failure)
	if _, _, err := userDB.VerifyPasswordWithReason("testuser", "wrong-password"); err == nil {
		t.Fatalf("VerifyPasswordWithReason should fail for wrong password")
	}
	// Verify log was called with failure reason

	// Test explicit logging methods
	userDB.LogAuthSuccess("testuser", "192.168.1.100")
	userDB.LogAuthFailure("testuser", "192.168.1.100", "invalid_password")

	// No assertions here - just verify methods don't panic
	// In production, these would be tested by checking log output or audit trail
}

// TestCascadeDeletion tests that public keys are deleted when user is deleted
func TestCascadeDeletion(t *testing.T) {
	// Skip this test if CASCADE is not properly supported
	// SQLite foreign key constraints need to be explicitly enabled

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_users.db")

	log := logger.New("test", logger.DefaultConfig())
	userDB, err := netconf.NewUserDatabase(dbPath, log)
	if err != nil {
		t.Fatalf("Failed to create user database: %v", err)
	}
	t.Cleanup(func() {
		if err := userDB.Close(); err != nil {
			t.Fatalf("Close failed: %v", err)
		}
	})

	// Create user
	passwordHash, err := auth.HashPassword("test-password")
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}
	if err := userDB.CreateUser("testuser", passwordHash, netconf.RoleAdmin); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Add public key
	_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	sshPublicKey, _ := ssh.NewPublicKey(privateKey.Public())
	authorizedKey := string(ssh.MarshalAuthorizedKey(sshPublicKey))
	parsedKey, _ := auth.ParsePublicKey(authorizedKey)
	if err := userDB.AddPublicKey("testuser", parsedKey.Algorithm, parsedKey.KeyData, parsedKey.Fingerprint, ""); err != nil {
		t.Fatalf("Failed to add public key: %v", err)
	}

	// Verify key exists
	keys, err := userDB.ListPublicKeys("testuser")
	if err != nil {
		t.Fatalf("Failed to list public keys: %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("Expected 1 key before deletion, got %d", len(keys))
	}

	// Delete user
	err = userDB.DeleteUser("testuser")
	if err != nil {
		t.Fatalf("Failed to delete user: %v", err)
	}

	// Verify key was also deleted (cascade)
	_, err = userDB.GetPublicKey(parsedKey.Fingerprint)
	if err == nil {
		t.Errorf("Expected error when getting key for deleted user")
	}
}

// Cleanup temporary files after tests
func TestMain(m *testing.M) {
	// Run tests
	code := m.Run()

	// Exit
	os.Exit(code)
}
