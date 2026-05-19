package security

import (
	"strings"
	"testing"
)

func TestValidateWebAPITokenAcceptsLongToken(t *testing.T) {
	if err := ValidateWebAPIToken("0123456789abcdef0123456789ABCDEF"); err != nil {
		t.Fatalf("ValidateWebAPIToken() error = %v", err)
	}
}

func TestValidateWebAPITokenRejectsShortToken(t *testing.T) {
	const token = "secret-token"
	err := ValidateWebAPIToken(token)
	if err == nil {
		t.Fatal("ValidateWebAPIToken() error = nil, want short token error")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("ValidateWebAPIToken() error leaked token: %v", err)
	}
}

func TestValidateWebAPITokenRejectsWhitespace(t *testing.T) {
	const token = "0123456789abcdef 0123456789ABCDEF"
	err := ValidateWebAPIToken(token)
	if err == nil {
		t.Fatal("ValidateWebAPIToken() error = nil, want whitespace error")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("ValidateWebAPIToken() error leaked token: %v", err)
	}
}

func TestValidateWebAPITokenRejectsSingleRepeatedRune(t *testing.T) {
	token := strings.Repeat("a", MinimumWebAPITokenLength)
	err := ValidateWebAPIToken(token)
	if err == nil {
		t.Fatal("ValidateWebAPIToken() error = nil, want repeated character error")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("ValidateWebAPIToken() error leaked token: %v", err)
	}
}
