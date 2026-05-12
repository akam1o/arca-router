package config

import (
	"strings"
	"testing"

	"github.com/akam1o/arca-router/pkg/auth"
)

func TestParserHashesSecurityUserPassword(t *testing.T) {
	const plainPassword = "plain-password-value"
	cfg, err := NewParser(strings.NewReader(`set security users user admin password ` + plainPassword)).Parse()
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	got := cfg.Security.Users["admin"].Password
	if got == "" || got == plainPassword {
		t.Fatalf("stored password = %q, want encoded hash", got)
	}
	if !IsEncodedPasswordHash(got) {
		t.Fatalf("stored password = %q, want argon2id hash", got)
	}
	valid, err := auth.VerifyPassword(plainPassword, got)
	if err != nil {
		t.Fatalf("VerifyPassword() error = %v", err)
	}
	if !valid {
		t.Fatal("hashed password did not verify")
	}
}

func TestProtectSecretsInSetCommandHashesPassword(t *testing.T) {
	const plainPassword = "plain-password-value"
	line, err := ProtectSecretsInSetCommand(`set security users user admin password "` + plainPassword + `"`)
	if err != nil {
		t.Fatalf("ProtectSecretsInSetCommand() error = %v", err)
	}
	if strings.Contains(line, plainPassword) {
		t.Fatalf("protected line contains plain password: %s", line)
	}
	if !strings.Contains(line, `"$argon2id$`) {
		t.Fatalf("protected line = %q, want quoted argon2id hash", line)
	}
}

func TestProtectSecretsInSetCommandPreservesEncodedHash(t *testing.T) {
	hash, err := NormalizePasswordForStorage("plain-password-value")
	if err != nil {
		t.Fatalf("NormalizePasswordForStorage() error = %v", err)
	}

	line, err := ProtectSecretsInSetCommand(`set security users user admin password "` + hash + `"`)
	if err != nil {
		t.Fatalf("ProtectSecretsInSetCommand() error = %v", err)
	}
	want := `set security users user admin password "` + hash + `"`
	if line != want {
		t.Fatalf("protected line = %q, want %q", line, want)
	}
}

func TestNormalizePasswordForStorageRejectsInvalidEncodedHash(t *testing.T) {
	_, err := NormalizePasswordForStorage("$argon2id$not-a-valid-hash")
	if err == nil {
		t.Fatal("NormalizePasswordForStorage() error = nil, want invalid hash error")
	}
}

func TestNormalizePasswordForStorageRejectsWeakEncodedHash(t *testing.T) {
	hash, err := NormalizePasswordForStorage("plain-password-value")
	if err != nil {
		t.Fatalf("NormalizePasswordForStorage() error = %v", err)
	}
	weakHash := strings.Replace(hash, "m=65536,t=3,p=4", "m=8,t=1,p=1", 1)

	_, err = NormalizePasswordForStorage(weakHash)
	if err == nil {
		t.Fatal("NormalizePasswordForStorage() error = nil, want invalid hash error")
	}
}

func TestToSetCommandsWithErrorRejectsInvalidPasswordHash(t *testing.T) {
	cfg := &Config{
		Security: &SecurityConfig{
			Users: map[string]*UserConfig{
				"admin": {Username: "admin", Password: "$argon2id$not-a-valid-hash"},
			},
		},
	}

	_, err := ToSetCommandsWithError(cfg)
	if err == nil {
		t.Fatal("ToSetCommandsWithError() error = nil, want invalid hash error")
	}
}
