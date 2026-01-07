package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestParsePublicKey(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
		expectAlgo  string
	}{
		{
			name:        "valid ed25519 key with comment",
			input:       "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMW3vXcGYNmJnPqF8pGdN6TuQvJJJqKJJJ5JJJJ5JJJ user@host",
			expectError: false,
			expectAlgo:  "ssh-ed25519",
		},
		{
			name:        "valid ed25519 key without comment",
			input:       "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMW3vXcGYNmJnPqF8pGdN6TuQvJJJqKJJJ5JJJJ5JJJ",
			expectError: false,
			expectAlgo:  "ssh-ed25519",
		},
		{
			name:        "empty line",
			input:       "",
			expectError: true,
		},
		{
			name:        "comment line",
			input:       "# This is a comment",
			expectError: true,
		},
		{
			name:        "invalid format - only algorithm",
			input:       "ssh-ed25519",
			expectError: true,
		},
		{
			name:        "invalid base64",
			input:       "ssh-ed25519 invalid-base64!!!",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := ParsePublicKey(tt.input)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("ParsePublicKey failed: %v", err)
			}

			if key.Algorithm != tt.expectAlgo {
				t.Errorf("Expected algorithm %s, got %s", tt.expectAlgo, key.Algorithm)
			}

			if key.Fingerprint == "" {
				t.Errorf("Expected fingerprint to be generated")
			}

			if !strings.HasPrefix(key.Fingerprint, "SHA256:") {
				t.Errorf("Expected fingerprint to start with SHA256:, got %s", key.Fingerprint)
			}
		})
	}
}

func TestParsePublicKeyWithRealKey(t *testing.T) {
	// Generate a real ED25519 key pair
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	// Convert to SSH public key
	sshPublicKey, err := ssh.NewPublicKey(privateKey.Public())
	if err != nil {
		t.Fatalf("Failed to create SSH public key: %v", err)
	}

	// Format as authorized_keys line
	authorizedKey := string(ssh.MarshalAuthorizedKey(sshPublicKey))
	authorizedKey = strings.TrimSpace(authorizedKey) + " test@example.com"

	// Parse the key
	parsedKey, err := ParsePublicKey(authorizedKey)
	if err != nil {
		t.Fatalf("Failed to parse real key: %v", err)
	}

	if parsedKey.Algorithm != "ssh-ed25519" {
		t.Errorf("Expected algorithm ssh-ed25519, got %s", parsedKey.Algorithm)
	}

	if parsedKey.Comment != "test@example.com" {
		t.Errorf("Expected comment 'test@example.com', got '%s'", parsedKey.Comment)
	}

	if !strings.HasPrefix(parsedKey.Fingerprint, "SHA256:") {
		t.Errorf("Expected fingerprint to start with SHA256:, got %s", parsedKey.Fingerprint)
	}
}

func TestGenerateFingerprint(t *testing.T) {
	// Use a known key for deterministic testing
	algorithm := "ssh-ed25519"
	keyData := "AAAAC3NzaC1lZDI1NTE5AAAAIOMW3vXcGYNmJnPqF8pGdN6TuQvJJJqKJJJ5JJJJ5JJJ"

	fingerprint1, err := GenerateFingerprint(algorithm, keyData)
	if err != nil {
		t.Fatalf("GenerateFingerprint failed: %v", err)
	}

	// Generate again to ensure deterministic
	fingerprint2, err := GenerateFingerprint(algorithm, keyData)
	if err != nil {
		t.Fatalf("GenerateFingerprint failed: %v", err)
	}

	if fingerprint1 != fingerprint2 {
		t.Errorf("Fingerprints should be deterministic: %s != %s", fingerprint1, fingerprint2)
	}

	if !strings.HasPrefix(fingerprint1, "SHA256:") {
		t.Errorf("Expected fingerprint to start with SHA256:, got %s", fingerprint1)
	}
}

func TestVerifyPublicKey(t *testing.T) {
	// Generate a real key pair
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	sshPublicKey, err := ssh.NewPublicKey(privateKey.Public())
	if err != nil {
		t.Fatalf("Failed to create SSH public key: %v", err)
	}

	// Store the key data (base64-encoded marshal)
	authorizedKey := string(ssh.MarshalAuthorizedKey(sshPublicKey))
	parsedKey, err := ParsePublicKey(strings.TrimSpace(authorizedKey))
	if err != nil {
		t.Fatalf("Failed to parse key: %v", err)
	}

	// Verify the same key
	valid, err := VerifyPublicKey(sshPublicKey, parsedKey.KeyData)
	if err != nil {
		t.Fatalf("VerifyPublicKey failed: %v", err)
	}
	if !valid {
		t.Errorf("Expected key to be valid")
	}

	// Generate a different key and verify it doesn't match
	_, privateKey2, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate second key: %v", err)
	}

	sshPublicKey2, err := ssh.NewPublicKey(privateKey2.Public())
	if err != nil {
		t.Fatalf("Failed to create second SSH public key: %v", err)
	}

	// Verify different key
	valid, err = VerifyPublicKey(sshPublicKey2, parsedKey.KeyData)
	if err != nil {
		t.Fatalf("VerifyPublicKey failed: %v", err)
	}
	if valid {
		t.Errorf("Expected different key to be invalid")
	}
}

func TestFormatAuthorizedKey(t *testing.T) {
	tests := []struct {
		name     string
		key      PublicKey
		expected string
	}{
		{
			name: "key with comment",
			key: PublicKey{
				Algorithm: "ssh-ed25519",
				KeyData:   "AAAAC3NzaC1lZDI1NTE5AAAAIOMW3vXcGYNmJnPqF8pGdN6TuQvJJJqKJJJ5JJJJ5JJJ",
				Comment:   "user@host",
			},
			expected: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMW3vXcGYNmJnPqF8pGdN6TuQvJJJqKJJJ5JJJJ5JJJ user@host",
		},
		{
			name: "key without comment",
			key: PublicKey{
				Algorithm: "ssh-ed25519",
				KeyData:   "AAAAC3NzaC1lZDI1NTE5AAAAIOMW3vXcGYNmJnPqF8pGdN6TuQvJJJqKJJJ5JJJJ5JJJ",
				Comment:   "",
			},
			expected: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMW3vXcGYNmJnPqF8pGdN6TuQvJJJqKJJJ5JJJJ5JJJ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.key.FormatAuthorizedKey()
			if result != tt.expected {
				t.Errorf("Expected:\n%s\nGot:\n%s", tt.expected, result)
			}
		})
	}
}

func TestValidateKeyAlgorithm(t *testing.T) {
	tests := []struct {
		algorithm   string
		expectError bool
	}{
		{"ssh-rsa", false},
		{"rsa-sha2-256", false},
		{"rsa-sha2-512", false},
		{"ssh-ed25519", false},
		{"ecdsa-sha2-nistp256", false},
		{"ecdsa-sha2-nistp384", false},
		{"ecdsa-sha2-nistp521", false},
		{"ssh-dss", true},      // Not supported
		{"unknown-algo", true}, // Not supported
		{"", true},             // Empty
	}

	for _, tt := range tests {
		t.Run(tt.algorithm, func(t *testing.T) {
			err := ValidateKeyAlgorithm(tt.algorithm)
			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for algorithm %s", tt.algorithm)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for algorithm %s: %v", tt.algorithm, err)
				}
			}
		})
	}
}

func TestPublicKeyRoundTrip(t *testing.T) {
	// Generate a real key
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	sshPublicKey, err := ssh.NewPublicKey(privateKey.Public())
	if err != nil {
		t.Fatalf("Failed to create SSH public key: %v", err)
	}

	// Marshal to authorized_keys format
	authorizedKey := string(ssh.MarshalAuthorizedKey(sshPublicKey))
	authorizedKey = strings.TrimSpace(authorizedKey) + " test@example.com"

	// Parse the key
	parsedKey, err := ParsePublicKey(authorizedKey)
	if err != nil {
		t.Fatalf("Failed to parse key: %v", err)
	}

	// Format back to authorized_keys
	formatted := parsedKey.FormatAuthorizedKey()

	// Parse again
	reparsedKey, err := ParsePublicKey(formatted)
	if err != nil {
		t.Fatalf("Failed to reparse key: %v", err)
	}

	// Verify all fields match
	if parsedKey.Algorithm != reparsedKey.Algorithm {
		t.Errorf("Algorithm mismatch: %s != %s", parsedKey.Algorithm, reparsedKey.Algorithm)
	}
	if parsedKey.KeyData != reparsedKey.KeyData {
		t.Errorf("KeyData mismatch")
	}
	if parsedKey.Comment != reparsedKey.Comment {
		t.Errorf("Comment mismatch: %s != %s", parsedKey.Comment, reparsedKey.Comment)
	}
	if parsedKey.Fingerprint != reparsedKey.Fingerprint {
		t.Errorf("Fingerprint mismatch: %s != %s", parsedKey.Fingerprint, reparsedKey.Fingerprint)
	}
}
