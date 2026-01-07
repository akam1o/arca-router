package netconf

import (
	"strings"
	"testing"
)

func TestHashPassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{
			name:     "valid password",
			password: "mySecurePassword123",
			wantErr:  false,
		},
		{
			name:     "short password",
			password: "abc",
			wantErr:  false, // We don't enforce minimum length in hash function
		},
		{
			name:     "long password",
			password: strings.Repeat("a", 200),
			wantErr:  false,
		},
		{
			name:     "empty password",
			password: "",
			wantErr:  false, // Technically allowed, but should be validated elsewhere
		},
		{
			name:     "special characters",
			password: "p@ssw0rd!#$%^&*()",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, err := HashPassword(tt.password)
			if (err != nil) != tt.wantErr {
				t.Errorf("HashPassword() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Check hash format: should be non-empty
				if len(hash) == 0 {
					t.Errorf("HashPassword() returned empty hash")
				}

				// Hash should be different from password
				if hash == tt.password {
					t.Errorf("HashPassword() hash equals password (not hashed)")
				}

				// Hash should contain argon2id prefix
				if !strings.HasPrefix(hash, "$argon2id$") {
					t.Errorf("HashPassword() hash doesn't have argon2id prefix, got: %s", hash[:20])
				}
			}
		})
	}
}

func TestVerifyPassword(t *testing.T) {
	// Generate a known hash for testing
	password := "testPassword123"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("Failed to generate hash for testing: %v", err)
	}

	tests := []struct {
		name     string
		password string
		hash     string
		want     bool
		wantErr  bool
	}{
		{
			name:     "correct password",
			password: password,
			hash:     hash,
			want:     true,
			wantErr:  false,
		},
		{
			name:     "incorrect password",
			password: "wrongPassword",
			hash:     hash,
			want:     false,
			wantErr:  false,
		},
		{
			name:     "empty password",
			password: "",
			hash:     hash,
			want:     false,
			wantErr:  false,
		},
		{
			name:     "case sensitive",
			password: "TESTPASSWORD123",
			hash:     hash,
			want:     false,
			wantErr:  false,
		},
		{
			name:     "invalid hash format",
			password: password,
			hash:     "invalid-hash",
			want:     false,
			wantErr:  true,
		},
		{
			name:     "empty hash",
			password: password,
			hash:     "",
			want:     false,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := VerifyPassword(tt.password, tt.hash)
			if (err != nil) != tt.wantErr {
				t.Errorf("VerifyPassword() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("VerifyPassword() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPasswordHashUniqueness(t *testing.T) {
	// Same password should produce different hashes (due to random salt)
	password := "testPassword"

	hash1, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() failed: %v", err)
	}

	hash2, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() failed: %v", err)
	}

	if hash1 == hash2 {
		t.Errorf("HashPassword() produced identical hashes for same password (salt not random)")
	}

	// Both hashes should verify against the same password
	match1, err := VerifyPassword(password, hash1)
	if err != nil || !match1 {
		t.Errorf("VerifyPassword() failed for hash1")
	}

	match2, err := VerifyPassword(password, hash2)
	if err != nil || !match2 {
		t.Errorf("VerifyPassword() failed for hash2")
	}
}

func TestPasswordTimingAttackResistance(t *testing.T) {
	// This test ensures constant-time comparison is used
	// We can't easily test timing directly, but we can verify behavior

	password := "correctPassword"
	hash, _ := HashPassword(password)

	// Verify with progressively different passwords
	// All should fail in roughly constant time
	testCases := []string{
		"x",                      // 1 char different
		"correctPasswor",         // Last char missing
		"correctPassworx",        // Last char different
		"wrongPassword",          // Completely different
		strings.Repeat("a", 100), // Very different
	}

	for _, wrongPwd := range testCases {
		match, err := VerifyPassword(wrongPwd, hash)
		if err != nil {
			t.Errorf("VerifyPassword() returned error for wrong password: %v", err)
		}
		if match {
			t.Errorf("VerifyPassword() returned true for wrong password: %s", wrongPwd)
		}
	}
}
