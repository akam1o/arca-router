package netconf

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/akam1o/arca-router/pkg/auth"
	"github.com/akam1o/arca-router/pkg/logger"
)

func TestUserDatabaseRejectsInvalidPasswordHashOnCreate(t *testing.T) {
	userDB := newTestUserDatabase(t)

	err := userDB.CreateUser("testuser", weakPasswordHash(t), RoleAdmin)
	if err == nil || !strings.Contains(err.Error(), "invalid password_hash") {
		t.Fatalf("CreateUser() error = %v, want invalid password_hash", err)
	}
	count, err := userDB.CountUsers()
	if err != nil {
		t.Fatalf("CountUsers() error = %v", err)
	}
	if count != 0 {
		t.Fatalf("user count = %d, want 0", count)
	}
}

func TestUserDatabaseRejectsInvalidPasswordHashOnUpdate(t *testing.T) {
	userDB := newTestUserDatabase(t)
	passwordHash, err := auth.HashPassword("old-password")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if err := userDB.CreateUser("testuser", passwordHash, RoleAdmin); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	err = userDB.UpdateUser("testuser", weakPasswordHash(t), "", true)
	if err == nil || !strings.Contains(err.Error(), "invalid password_hash") {
		t.Fatalf("UpdateUser() error = %v, want invalid password_hash", err)
	}
	user, err := userDB.GetUser("testuser")
	if err != nil {
		t.Fatalf("GetUser() error = %v", err)
	}
	if user.PasswordHash != passwordHash {
		t.Fatal("UpdateUser() changed password hash after validation failure")
	}
}

func TestUserDatabaseVerifyPasswordUsesDummyHashForMissingUser(t *testing.T) {
	userDB := newTestUserDatabase(t)
	calls := capturePasswordVerification(t, false, nil)

	_, err := userDB.VerifyPassword("missing", "password")
	if err == nil {
		t.Fatal("VerifyPassword() error = nil, want authentication failure")
	}
	if len(*calls) != 1 || (*calls)[0] != dummyPasswordHash {
		t.Fatalf("verified hashes = %v, want dummy hash only", *calls)
	}
}

func TestUserDatabaseVerifyPasswordUsesDummyHashForDisabledUser(t *testing.T) {
	userDB := newTestUserDatabase(t)
	passwordHash, err := auth.HashPassword("password")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if err := userDB.CreateUser("testuser", passwordHash, RoleAdmin); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if err := userDB.UpdateUser("testuser", "", "", false); err != nil {
		t.Fatalf("UpdateUser() error = %v", err)
	}
	calls := capturePasswordVerification(t, true, nil)

	_, err = userDB.VerifyPassword("testuser", "password")
	if err == nil {
		t.Fatal("VerifyPassword() error = nil, want authentication failure")
	}
	if len(*calls) != 1 || (*calls)[0] != dummyPasswordHash {
		t.Fatalf("verified hashes = %v, want dummy hash only", *calls)
	}
}

func newTestUserDatabase(t *testing.T) *UserDatabase {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "users.db")
	userDB, err := NewUserDatabase(dbPath, logger.New("test", logger.DefaultConfig()))
	if err != nil {
		t.Fatalf("NewUserDatabase() error = %v", err)
	}
	t.Cleanup(func() { _ = userDB.Close() })
	return userDB
}

func weakPasswordHash(t *testing.T) string {
	t.Helper()

	passwordHash, err := auth.HashPassword("password")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	weakHash := strings.Replace(passwordHash, "m=65536,t=3,p=4", "m=8,t=1,p=1", 1)
	if weakHash == passwordHash {
		t.Fatal("failed to weaken password hash parameters")
	}
	return weakHash
}

func capturePasswordVerification(t *testing.T, valid bool, err error) *[]string {
	t.Helper()

	oldVerifyPasswordHash := verifyPasswordHash
	var calls []string
	verifyPasswordHash = func(password, encodedHash string) (bool, error) {
		calls = append(calls, encodedHash)
		return valid, err
	}
	t.Cleanup(func() {
		verifyPasswordHash = oldVerifyPasswordHash
	})
	return &calls
}
