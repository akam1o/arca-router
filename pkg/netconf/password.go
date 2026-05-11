package netconf

import "github.com/akam1o/arca-router/pkg/auth"

// HashPassword generates an argon2id hash for the given password.
func HashPassword(password string) (string, error) {
	return auth.HashPassword(password)
}

// VerifyPassword verifies a password against an argon2id hash.
// Returns true if the password matches, false otherwise.
func VerifyPassword(password, encodedHash string) (bool, error) {
	return auth.VerifyPassword(password, encodedHash)
}
