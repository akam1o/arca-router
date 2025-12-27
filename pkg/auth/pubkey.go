package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

// PublicKey represents an SSH public key for user authentication
type PublicKey struct {
	// Username is the owner of this key
	Username string

	// Algorithm is the key algorithm (e.g., "ssh-rsa", "ssh-ed25519", "ecdsa-sha2-nistp256")
	Algorithm string

	// KeyData is the base64-encoded public key data
	KeyData string

	// Fingerprint is the SHA256 fingerprint of the key (format: "SHA256:...")
	Fingerprint string

	// Comment is an optional comment for the key (from authorized_keys format)
	Comment string

	// Enabled indicates if this key is enabled for authentication
	Enabled bool

	// CreatedAt is the Unix timestamp when the key was added
	CreatedAt int64
}

// ParsePublicKey parses an SSH public key from authorized_keys format
// Format: <algorithm> <base64-key> [comment]
// Example: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFoo... user@host"
func ParsePublicKey(line string) (*PublicKey, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return nil, fmt.Errorf("empty or comment line")
	}

	parts := strings.Fields(line)
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid public key format: expected at least '<algorithm> <key-data>'")
	}

	algorithm := parts[0]
	keyData := parts[1]
	comment := ""
	if len(parts) > 2 {
		comment = strings.Join(parts[2:], " ")
	}

	// Validate key by parsing it with golang.org/x/crypto/ssh
	_, err := ssh.ParsePublicKey([]byte(fmt.Sprintf("%s %s", algorithm, keyData)))
	if err != nil {
		// Try to parse as openssh authorized_keys format
		sshKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			return nil, fmt.Errorf("invalid SSH public key: %w", err)
		}
		// Extract algorithm and key data from parsed key
		algorithm = sshKey.Type()
		keyData = base64.StdEncoding.EncodeToString(sshKey.Marshal())
	}

	// Generate fingerprint
	fingerprint, err := GenerateFingerprint(algorithm, keyData)
	if err != nil {
		return nil, fmt.Errorf("failed to generate fingerprint: %w", err)
	}

	return &PublicKey{
		Algorithm:   algorithm,
		KeyData:     keyData,
		Fingerprint: fingerprint,
		Comment:     comment,
		Enabled:     true,
	}, nil
}

// GenerateFingerprint generates a SHA256 fingerprint for an SSH public key
// Format: "SHA256:<base64-hash>" (no padding)
func GenerateFingerprint(algorithm, keyData string) (string, error) {
	// Parse the key to ensure it's valid
	_, err := base64.StdEncoding.DecodeString(keyData)
	if err != nil {
		return "", fmt.Errorf("invalid key data encoding: %w", err)
	}

	// Reconstruct the full key format for hashing
	fullKey := fmt.Sprintf("%s %s", algorithm, keyData)
	sshKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(fullKey))
	if err != nil {
		return "", fmt.Errorf("failed to parse key: %w", err)
	}

	// Generate SHA256 hash of the key marshal (SSH wire format)
	hash := sha256.Sum256(sshKey.Marshal())
	fingerprint := base64.RawStdEncoding.EncodeToString(hash[:])

	return fmt.Sprintf("SHA256:%s", fingerprint), nil
}

// VerifyPublicKey verifies that a given SSH public key matches the stored key data
// This is used during SSH authentication
func VerifyPublicKey(providedKey ssh.PublicKey, storedKeyData string) (bool, error) {
	// Decode the stored key
	storedBytes, err := base64.StdEncoding.DecodeString(storedKeyData)
	if err != nil {
		return false, fmt.Errorf("invalid stored key encoding: %w", err)
	}

	// Marshal the provided key to SSH wire format
	providedBytes := providedKey.Marshal()

	// Compare the key data (SSH wire format)
	// We compare the marshaled form because that's the canonical representation
	if len(storedBytes) != len(providedBytes) {
		return false, nil
	}

	// Constant-time comparison would be ideal, but keys are public data
	// and timing attacks are not a concern for public key authentication
	for i := range storedBytes {
		if storedBytes[i] != providedBytes[i] {
			return false, nil
		}
	}

	return true, nil
}

// FormatAuthorizedKey formats a PublicKey as an authorized_keys line
// Format: <algorithm> <base64-key> [comment]
func (pk *PublicKey) FormatAuthorizedKey() string {
	if pk.Comment != "" {
		return fmt.Sprintf("%s %s %s", pk.Algorithm, pk.KeyData, pk.Comment)
	}
	return fmt.Sprintf("%s %s", pk.Algorithm, pk.KeyData)
}

// ValidateKeyAlgorithm validates that the key algorithm is supported
// Supported algorithms: ssh-rsa, ssh-ed25519, ecdsa-sha2-nistp256, ecdsa-sha2-nistp384, ecdsa-sha2-nistp521
func ValidateKeyAlgorithm(algorithm string) error {
	supportedAlgorithms := []string{
		"ssh-rsa",
		"rsa-sha2-256",
		"rsa-sha2-512",
		"ssh-ed25519",
		"ecdsa-sha2-nistp256",
		"ecdsa-sha2-nistp384",
		"ecdsa-sha2-nistp521",
	}

	for _, supported := range supportedAlgorithms {
		if algorithm == supported {
			return nil
		}
	}

	return fmt.Errorf("unsupported key algorithm: %s (supported: %v)", algorithm, supportedAlgorithms)
}
