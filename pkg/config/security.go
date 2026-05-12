package config

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/akam1o/arca-router/pkg/auth"
)

const encodedPasswordHashPrefix = "$argon2id$"

// NormalizePasswordForStorage converts a plain-text password to the stored hash
// representation. Already-encoded argon2id hashes are preserved for round trips.
func NormalizePasswordForStorage(password string) (string, error) {
	if password == "" {
		return password, nil
	}
	if strings.HasPrefix(password, encodedPasswordHashPrefix) {
		if err := ValidatePasswordHash(password); err != nil {
			return "", err
		}
		return password, nil
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return hash, nil
}

// IsEncodedPasswordHash reports whether password is a valid stored argon2id
// hash.
func IsEncodedPasswordHash(password string) bool {
	return ValidatePasswordHash(password) == nil
}

// ValidatePasswordHash verifies that passwordHash is a supported encoded
// argon2id hash.
func ValidatePasswordHash(passwordHash string) error {
	if !strings.HasPrefix(passwordHash, encodedPasswordHashPrefix) {
		return fmt.Errorf("password hash must use argon2id")
	}
	if err := auth.ValidatePasswordHash(passwordHash); err != nil {
		return fmt.Errorf("invalid password hash: %w", err)
	}
	return nil
}

// ProtectSecretsInSetCommands hashes plain-text secrets in set-command text
// without otherwise rewriting unrelated configuration lines.
func ProtectSecretsInSetCommands(text string) (string, error) {
	if text == "" {
		return text, nil
	}

	lines := strings.Split(text, "\n")
	changed := false
	for i, line := range lines {
		protected, err := ProtectSecretsInSetCommand(line)
		if err != nil {
			return "", err
		}
		if protected != line {
			lines[i] = protected
			changed = true
		}
	}
	if !changed {
		return text, nil
	}
	return strings.Join(lines, "\n"), nil
}

// ProtectSecretsInSetCommand hashes a plain-text password in a single
// "set security users user <name> password <value>" command.
func ProtectSecretsInSetCommand(line string) (string, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return line, nil
	}

	username, password, ok, err := parseSecurityPasswordSetCommand(trimmed)
	if err != nil || !ok {
		return line, err
	}

	storedPassword, err := NormalizePasswordForStorage(password)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("set security users user %s password %s", username, EscapeValue(storedPassword)), nil
}

func parseSecurityPasswordSetCommand(line string) (username, password string, ok bool, err error) {
	const prefix = "set security users user "
	if !strings.HasPrefix(line, prefix) {
		return "", "", false, nil
	}

	rest := strings.TrimLeftFunc(strings.TrimPrefix(line, prefix), unicode.IsSpace)
	username, rest, ok = cutConfigField(rest)
	if !ok {
		return "", "", true, fmt.Errorf("expected username in security password command")
	}

	param, rest, ok := cutConfigField(strings.TrimLeftFunc(rest, unicode.IsSpace))
	if !ok {
		return "", "", true, fmt.Errorf("expected user parameter in security password command")
	}
	if param != "password" {
		return "", "", false, nil
	}

	valueText := strings.TrimSpace(rest)
	if valueText == "" {
		return "", "", true, fmt.Errorf("expected password value in security password command")
	}
	password, err = parseConfigScalar(valueText)
	if err != nil {
		return "", "", true, err
	}
	return username, password, true, nil
}

func cutConfigField(text string) (field, rest string, ok bool) {
	text = strings.TrimLeftFunc(text, unicode.IsSpace)
	if text == "" {
		return "", "", false
	}
	for i, ch := range text {
		if unicode.IsSpace(ch) {
			return text[:i], text[i:], true
		}
	}
	return text, "", true
}

func parseConfigScalar(text string) (string, error) {
	if strings.HasPrefix(text, `"`) {
		lexer := NewLexer(strings.NewReader(text))
		token := lexer.NextToken()
		if token.Type == TokenError {
			return "", fmt.Errorf("invalid quoted password value: %s", token.Value)
		}
		if token.Type != TokenString {
			return "", fmt.Errorf("expected quoted password value")
		}
		next := lexer.NextToken()
		if next.Type != TokenEOF {
			return "", fmt.Errorf("unexpected trailing content after password value")
		}
		return token.Value, nil
	}
	if strings.ContainsFunc(text, unicode.IsSpace) {
		return "", fmt.Errorf("password value with whitespace must be quoted")
	}
	return text, nil
}
