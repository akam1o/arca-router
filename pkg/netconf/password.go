package netconf

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters (recommended by OWASP)
const (
	argon2Time      = 3        // iterations
	argon2Memory    = 64 * 1024 // 64 MB
	argon2Threads   = 4        // parallelism
	argon2KeyLength = 32       // 32 bytes
	argon2SaltLength = 16       // 16 bytes
)

// HashPassword generates an argon2id hash for the given password
// Format: $argon2id$v=19$m=65536,t=3,p=4$<base64-salt>$<base64-hash>
func HashPassword(password string) (string, error) {
	// Generate random salt
	salt := make([]byte, argon2SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}

	// Hash password
	hash := argon2.IDKey(
		[]byte(password),
		salt,
		argon2Time,
		argon2Memory,
		argon2Threads,
		argon2KeyLength,
	)

	// Encode to string
	saltB64 := base64.RawStdEncoding.EncodeToString(salt)
	hashB64 := base64.RawStdEncoding.EncodeToString(hash)

	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argon2Memory, argon2Time, argon2Threads, saltB64, hashB64), nil
}

// VerifyPassword verifies a password against an argon2id hash
// Returns true if the password matches, false otherwise
func VerifyPassword(password, encodedHash string) (bool, error) {
	// Parse encoded hash
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 {
		return false, fmt.Errorf("invalid hash format")
	}

	if parts[1] != "argon2id" {
		return false, fmt.Errorf("unsupported algorithm: %s", parts[1])
	}

	if parts[2] != "v=19" {
		return false, fmt.Errorf("unsupported argon2 version: %s", parts[2])
	}

	// Parse parameters
	var memory, time uint32
	var threads uint8
	_, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads)
	if err != nil {
		return false, fmt.Errorf("invalid parameters: %w", err)
	}

	// Decode salt
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("invalid salt encoding: %w", err)
	}

	// Decode hash
	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("invalid hash encoding: %w", err)
	}

	// Hash the input password with the same parameters
	actualHash := argon2.IDKey(
		[]byte(password),
		salt,
		time,
		memory,
		threads,
		uint32(len(expectedHash)),
	)

	// Constant-time comparison
	return subtle.ConstantTimeCompare(expectedHash, actualHash) == 1, nil
}
