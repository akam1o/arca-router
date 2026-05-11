package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters (recommended by OWASP)
const (
	argon2Time       = 3         // iterations
	argon2Memory     = 64 * 1024 // 64 MB
	argon2Threads    = 4         // parallelism
	argon2KeyLength  = 32        // 32 bytes
	argon2SaltLength = 16        // 16 bytes
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

	memory, time, threads, err := parseArgon2Params(parts[3])
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

func parseArgon2Params(encoded string) (uint32, uint32, uint8, error) {
	fields := strings.Split(encoded, ",")
	if len(fields) != 3 {
		return 0, 0, 0, fmt.Errorf("expected m, t, and p parameters")
	}

	memory, err := parseArgon2Param(fields[0], "m", 32)
	if err != nil {
		return 0, 0, 0, err
	}
	timeCost, err := parseArgon2Param(fields[1], "t", 32)
	if err != nil {
		return 0, 0, 0, err
	}
	threads, err := parseArgon2Param(fields[2], "p", 8)
	if err != nil {
		return 0, 0, 0, err
	}

	if memory == 0 || memory > argon2Memory {
		return 0, 0, 0, fmt.Errorf("memory cost out of range")
	}
	if timeCost == 0 || timeCost > argon2Time {
		return 0, 0, 0, fmt.Errorf("time cost out of range")
	}
	if threads == 0 || threads > argon2Threads {
		return 0, 0, 0, fmt.Errorf("parallelism out of range")
	}

	return uint32(memory), uint32(timeCost), uint8(threads), nil
}

func parseArgon2Param(field, name string, bitSize int) (uint64, error) {
	key, value, ok := strings.Cut(field, "=")
	if !ok || key != name || value == "" {
		return 0, fmt.Errorf("missing %s parameter", name)
	}
	parsed, err := strconv.ParseUint(value, 10, bitSize)
	if err != nil {
		return 0, fmt.Errorf("invalid %s parameter: %w", name, err)
	}
	return parsed, nil
}
