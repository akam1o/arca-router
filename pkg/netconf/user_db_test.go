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

func TestNewUserDatabaseDefaultsNilLogger(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "users.db")

	userDB, err := NewUserDatabase(dbPath, nil)
	if err != nil {
		t.Fatalf("NewUserDatabase() error = %v", err)
	}
	t.Cleanup(func() { _ = userDB.Close() })

	if userDB.log == nil {
		t.Fatal("user database logger = nil")
	}
	if err := userDB.HealthCheck(); err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
}

func TestUserDatabaseLifecycleMethodsNilReceiver(t *testing.T) {
	var userDB *UserDatabase

	userDB.SetAuditLogger(nil)
	userDB.LogAuthSuccess("alice", "192.0.2.1")
	userDB.LogAuthSuccessWithMethod("alice", "192.0.2.1", "publickey")
	userDB.LogAuthFailure("alice", "192.0.2.1", "invalid_password")
	userDB.LogAuthFailureWithMethod("alice", "192.0.2.1", "publickey", "key_not_found")

	if err := userDB.HealthCheck(); err == nil {
		t.Fatal("HealthCheck() error = nil, want database connection error")
	}
	if err := userDB.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestUserDatabaseLifecycleMethodsZeroValue(t *testing.T) {
	userDB := &UserDatabase{}

	userDB.SetAuditLogger(nil)
	userDB.LogAuthSuccess("alice", "192.0.2.1")
	userDB.LogAuthFailure("alice", "192.0.2.1", "invalid_password")

	if err := userDB.HealthCheck(); err == nil {
		t.Fatal("HealthCheck() error = nil, want database connection error")
	}
	if err := userDB.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestUserDatabaseOperationsNilReceiver(t *testing.T) {
	var userDB *UserDatabase

	requireUserDatabaseOperationsUnavailable(t, userDB)
}

func TestUserDatabaseOperationsZeroValue(t *testing.T) {
	userDB := &UserDatabase{}

	requireUserDatabaseOperationsUnavailable(t, userDB)
}

func TestUserDatabaseListPublicKeysUsesStableTieBreak(t *testing.T) {
	userDB := newTestUserDatabase(t)
	passwordHash, err := auth.HashPassword("password")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if err := userDB.CreateUser("alice", passwordHash, RoleAdmin); err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	db, err := userDB.database()
	if err != nil {
		t.Fatalf("database() error = %v", err)
	}
	for _, fingerprint := range []string{"SHA256:z-key", "SHA256:a-key", "SHA256:m-key"} {
		_, err := db.Exec(
			`INSERT INTO user_public_keys (username, algorithm, key_data, fingerprint, comment, enabled, created_at)
			 VALUES (?, ?, ?, ?, ?, 1, ?)`,
			"alice",
			"ssh-ed25519",
			"key-data-"+fingerprint,
			fingerprint,
			"test key",
			int64(1000),
		)
		if err != nil {
			t.Fatalf("insert public key %s error = %v", fingerprint, err)
		}
	}

	keys, err := userDB.ListPublicKeys("alice")
	if err != nil {
		t.Fatalf("ListPublicKeys() error = %v", err)
	}
	want := []string{"SHA256:a-key", "SHA256:m-key", "SHA256:z-key"}
	if len(keys) != len(want) {
		t.Fatalf("ListPublicKeys() returned %d keys, want %d", len(keys), len(want))
	}
	for i := range want {
		if keys[i].Fingerprint != want[i] {
			t.Fatalf("ListPublicKeys()[%d].Fingerprint = %q, want %q (all keys: %#v)", i, keys[i].Fingerprint, want[i], keys)
		}
	}
}

func TestUserDatabaseListUsersPaginatedNormalizesNegativeInputs(t *testing.T) {
	userDB := newTestUserDatabase(t)
	passwordHash, err := auth.HashPassword("password")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	for _, username := range []string{"carol", "alice", "bob"} {
		if err := userDB.CreateUser(username, passwordHash, RoleOperator); err != nil {
			t.Fatalf("CreateUser(%s) error = %v", username, err)
		}
	}

	users, err := userDB.ListUsersPaginated(2, -10)
	if err != nil {
		t.Fatalf("ListUsersPaginated() error = %v", err)
	}
	want := []string{"alice", "bob"}
	if len(users) != len(want) {
		t.Fatalf("ListUsersPaginated() returned %d users, want %d", len(users), len(want))
	}
	for i := range want {
		if users[i].Username != want[i] {
			t.Fatalf("ListUsersPaginated()[%d].Username = %q, want %q (all users: %#v)", i, users[i].Username, want[i], users)
		}
	}

	allUsers, err := userDB.ListUsersPaginated(-1, 2)
	if err != nil {
		t.Fatalf("ListUsersPaginated() with negative limit error = %v", err)
	}
	if len(allUsers) != 3 {
		t.Fatalf("ListUsersPaginated() with negative limit returned %d users, want 3", len(allUsers))
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

func requireUserDatabaseOperationsUnavailable(t *testing.T, userDB *UserDatabase) {
	t.Helper()

	passwordHash, err := auth.HashPassword("password")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	requireUserDatabaseConnectionError(t, userDB.Initialize())
	requireUserDatabaseConnectionError(t, userDB.CreateUser("alice", passwordHash, RoleAdmin))
	requireUserDatabaseConnectionError(t, userDB.UpdateUser("alice", "", RoleAdmin, true))
	requireUserDatabaseConnectionError(t, userDB.DeleteUser("alice"))

	if _, err := userDB.GetUser("alice"); err == nil {
		t.Fatal("GetUser() error = nil, want database connection error")
	} else {
		requireUserDatabaseConnectionError(t, err)
	}

	if _, err := userDB.ListUsers(); err == nil {
		t.Fatal("ListUsers() error = nil, want database connection error")
	} else {
		requireUserDatabaseConnectionError(t, err)
	}

	if _, err := userDB.ListUsersPaginated(10, 0); err == nil {
		t.Fatal("ListUsersPaginated() error = nil, want database connection error")
	} else {
		requireUserDatabaseConnectionError(t, err)
	}

	if _, err := userDB.CountUsers(); err == nil {
		t.Fatal("CountUsers() error = nil, want database connection error")
	} else {
		requireUserDatabaseConnectionError(t, err)
	}

	calls := capturePasswordVerification(t, false, nil)
	if _, err := userDB.VerifyPassword("alice", "password"); err == nil || err.Error() != "authentication failed" {
		t.Fatalf("VerifyPassword() error = %v, want authentication failed", err)
	}
	if _, reason, err := userDB.VerifyPasswordWithReason("alice", "password"); err == nil || reason != "user_not_found" {
		t.Fatalf("VerifyPasswordWithReason() reason=%q error=%v, want user_not_found authentication failure", reason, err)
	}
	if len(*calls) != 2 {
		t.Fatalf("password verification calls = %d, want 2", len(*calls))
	}

	requireUserDatabaseConnectionError(t, userDB.AddPublicKey("alice", "ssh-ed25519", "key-data", "SHA256:test", "test key"))
	requireUserDatabaseConnectionError(t, userDB.RemovePublicKey("SHA256:test"))
	requireUserDatabaseConnectionError(t, userDB.UpdatePublicKeyStatus("SHA256:test", false))

	if _, err := userDB.GetPublicKey("SHA256:test"); err == nil {
		t.Fatal("GetPublicKey() error = nil, want database connection error")
	} else {
		requireUserDatabaseConnectionError(t, err)
	}

	if _, err := userDB.ListPublicKeys("alice"); err == nil {
		t.Fatal("ListPublicKeys() error = nil, want database connection error")
	} else {
		requireUserDatabaseConnectionError(t, err)
	}

	if _, reason, err := userDB.VerifyPublicKeyAuth("alice", "key-data"); err == nil || reason != "user_not_found" {
		t.Fatalf("VerifyPublicKeyAuth() reason=%q error=%v, want user_not_found authentication failure", reason, err)
	}
}

func requireUserDatabaseConnectionError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("error = nil, want database connection error")
	}
	if !strings.Contains(err.Error(), "database connection is nil") {
		t.Fatalf("error = %v, want database connection error", err)
	}
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
