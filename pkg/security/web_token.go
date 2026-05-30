package security

import (
	"errors"
	"strings"
	"unicode"
)

// MinimumWebAPITokenLength is the minimum accepted length for Web/NMS API
// automation tokens. The threshold is intentionally conservative for bearer
// secrets stored in a local 0600 file.
const MinimumWebAPITokenLength = 32

var weakWebAPITokens = map[string]struct{}{
	"operator-token": {},
	"secret-token":   {},
	"shared-token":   {},
	"nms-token":      {},
	"changeme":       {},
	"password":       {},
	"secret":         {},
	"token":          {},
}

// ValidateWebAPIToken rejects short, whitespace-bearing, and well-known sample
// Web/NMS API tokens. The supplied value is a bearer secret and is
// intentionally not included in returned errors.
func ValidateWebAPIToken(token string) error {
	trimmed := strings.TrimSpace(token)
	if len(trimmed) < MinimumWebAPITokenLength {
		return errors.New("web API token must be at least 32 characters")
	}
	if strings.ContainsFunc(trimmed, unicode.IsSpace) {
		return errors.New("web API token must not contain whitespace")
	}
	if _, weak := weakWebAPITokens[strings.ToLower(trimmed)]; weak {
		return errors.New("web API token must not use a well-known sample value")
	}
	if isSingleRepeatedRune(trimmed) {
		return errors.New("web API token must contain more than one repeated character")
	}
	return nil
}

func isSingleRepeatedRune(value string) bool {
	var first rune
	for i, r := range value {
		if i == 0 {
			first = r
			continue
		}
		if r != first {
			return false
		}
	}
	return true
}
