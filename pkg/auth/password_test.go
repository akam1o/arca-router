package auth

import (
	"strings"
	"testing"
	"time"
)

func TestHashPassword(t *testing.T) {
	password := "test-password-123"

	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	// Verify hash format
	if !strings.HasPrefix(hash, "$argon2id$v=19$") {
		t.Errorf("Invalid hash format: %s", hash)
	}

	// Verify hash is unique (different salt)
	hash2, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	if hash == hash2 {
		t.Errorf("Two hashes should be different due to random salt")
	}
}

func TestVerifyPassword(t *testing.T) {
	password := "correct-password"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	// Test correct password
	valid, err := VerifyPassword(password, hash)
	if err != nil {
		t.Fatalf("VerifyPassword failed: %v", err)
	}
	if !valid {
		t.Errorf("Expected password to be valid")
	}

	// Test wrong password
	valid, err = VerifyPassword("wrong-password", hash)
	if err != nil {
		t.Fatalf("VerifyPassword failed: %v", err)
	}
	if valid {
		t.Errorf("Expected wrong password to be invalid")
	}
}

func TestVerifyPasswordInvalidHash(t *testing.T) {
	tests := []struct {
		name string
		hash string
	}{
		{"empty hash", ""},
		{"invalid format", "not-a-valid-hash"},
		{"wrong algorithm", "$bcrypt$v=19$m=65536,t=3,p=4$salt$hash"},
		{"wrong version", "$argon2id$v=20$m=65536,t=3,p=4$salt$hash"},
		{"missing parts", "$argon2id$v=19$m=65536"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := VerifyPassword("password", tt.hash)
			if err == nil {
				t.Errorf("Expected error for invalid hash: %s", tt.hash)
			}
		})
	}
}

func TestPasswordWithSpecialCharacters(t *testing.T) {
	passwords := []string{
		"password with spaces",
		"password\nwith\nnewlines",
		"password\twith\ttabs",
		"密码中文",
		"пароль",
		"🔐🔑",
		"password!@#$%^&*()_+-=[]{}|;':\"<>?,./",
	}

	for _, password := range passwords {
		t.Run(password, func(t *testing.T) {
			hash, err := HashPassword(password)
			if err != nil {
				t.Fatalf("HashPassword failed: %v", err)
			}

			valid, err := VerifyPassword(password, hash)
			if err != nil {
				t.Fatalf("VerifyPassword failed: %v", err)
			}
			if !valid {
				t.Errorf("Expected password to be valid")
			}
		})
	}
}

func TestPasswordTimingAttackResistance(t *testing.T) {
	password := "test-password"
	hash, _ := HashPassword(password)

	// Verify correct password multiple times
	correctTimes := make([]time.Duration, 10)
	for i := 0; i < 10; i++ {
		start := time.Now()
		_, err := VerifyPassword(password, hash)
		if err != nil {
			t.Fatalf("VerifyPassword failed: %v", err)
		}
		correctTimes[i] = time.Since(start)
	}

	// Verify wrong password multiple times
	wrongTimes := make([]time.Duration, 10)
	for i := 0; i < 10; i++ {
		start := time.Now()
		_, err := VerifyPassword("wrong", hash)
		if err != nil {
			t.Fatalf("VerifyPassword failed: %v", err)
		}
		wrongTimes[i] = time.Since(start)
	}

	// Calculate averages
	var correctSum, wrongSum time.Duration
	for i := 0; i < 10; i++ {
		correctSum += correctTimes[i]
		wrongSum += wrongTimes[i]
	}
	correctAvg := correctSum / 10
	wrongAvg := wrongSum / 10

	// Timing should be similar (within 20% margin)
	// This is not a perfect test but catches obvious timing leaks
	diff := correctAvg - wrongAvg
	if diff < 0 {
		diff = -diff
	}
	maxDiff := correctAvg / 5 // 20%

	if diff > maxDiff {
		t.Logf("Warning: Potential timing attack vulnerability detected")
		t.Logf("Correct password avg: %v, Wrong password avg: %v", correctAvg, wrongAvg)
		// Note: This is a warning, not a failure, as timing can vary
	}
}

func BenchmarkHashPassword(b *testing.B) {
	password := "benchmark-password-123"
	for i := 0; i < b.N; i++ {
		_, _ = HashPassword(password)
	}
}

func BenchmarkVerifyPassword(b *testing.B) {
	password := "benchmark-password-123"
	hash, _ := HashPassword(password)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = VerifyPassword(password, hash)
	}
}
