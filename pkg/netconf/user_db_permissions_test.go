package netconf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akam1o/arca-router/pkg/logger"
)

func TestUserDatabaseRestrictsDatabaseFilePermissions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "users.db")
	if err := os.WriteFile(dbPath, []byte{}, 0644); err != nil {
		t.Fatalf("write preexisting db: %v", err)
	}

	userDB, err := NewUserDatabase(dbPath, logger.New("test", logger.DefaultConfig()))
	if err != nil {
		t.Fatalf("NewUserDatabase() error = %v", err)
	}
	t.Cleanup(func() { _ = userDB.Close() })

	assertUserDBFileMode(t, dbPath, userDBFilePerms)
	assertUserDBFileModeIfExists(t, dbPath+"-wal", userDBFilePerms)
	assertUserDBFileModeIfExists(t, dbPath+"-shm", userDBFilePerms)
}

func TestUserDatabaseRejectsInsecureDatabaseDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "open")
	if err := os.Mkdir(dir, 0777); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	userDB, err := NewUserDatabase(filepath.Join(dir, "users.db"), logger.New("test", logger.DefaultConfig()))
	if err == nil {
		_ = userDB.Close()
		t.Fatal("NewUserDatabase() error = nil, want insecure directory error")
	}
}

func TestUserDatabaseRejectsSymlinkDatabaseFile(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "target-users.db")
	dbPath := filepath.Join(dir, "users.db")
	if err := os.WriteFile(targetPath, []byte{}, 0644); err != nil {
		t.Fatalf("write target db: %v", err)
	}
	if err := os.Chmod(targetPath, 0644); err != nil {
		t.Fatalf("chmod target db: %v", err)
	}
	if err := os.Symlink(targetPath, dbPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	userDB, err := NewUserDatabase(dbPath, logger.New("test", logger.DefaultConfig()))
	if err == nil {
		_ = userDB.Close()
		t.Fatal("NewUserDatabase() error = nil, want symlink database rejection")
	}
	if !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("NewUserDatabase() error = %v, want symbolic link rejection", err)
	}
	assertUserDBFileMode(t, targetPath, 0644)
}

func TestUserDatabaseRejectsHardLinkedDatabaseFile(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "target-users.db")
	dbPath := filepath.Join(dir, "users.db")
	if err := os.WriteFile(targetPath, []byte{}, 0600); err != nil {
		t.Fatalf("write target db: %v", err)
	}
	if err := os.Link(targetPath, dbPath); err != nil {
		t.Skipf("hard links not supported: %v", err)
	}

	userDB, err := NewUserDatabase(dbPath, logger.New("test", logger.DefaultConfig()))
	if err == nil {
		_ = userDB.Close()
		t.Fatal("NewUserDatabase() error = nil, want hard link database rejection")
	}
	if !strings.Contains(err.Error(), "multiple hard links") {
		t.Fatalf("NewUserDatabase() error = %v, want hard link rejection", err)
	}
}

func TestUserDatabaseRejectsSymlinkDatabaseDirectory(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, "target")
	dbDir := filepath.Join(root, "linked")
	if err := os.Mkdir(targetDir, 0750); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.Symlink(targetDir, dbDir); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	userDB, err := NewUserDatabase(filepath.Join(dbDir, "users.db"), logger.New("test", logger.DefaultConfig()))
	if err == nil {
		_ = userDB.Close()
		t.Fatal("NewUserDatabase() error = nil, want symlink directory rejection")
	}
	if !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("NewUserDatabase() error = %v, want symbolic link rejection", err)
	}
}

func TestUserDatabaseRejectsSQLiteDSNPaths(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{
			name: "query parameters",
			path: filepath.Join(t.TempDir(), "users.db") + "?cache=shared",
		},
		{
			name: "sqlite uri",
			path: "file:" + filepath.Join(t.TempDir(), "users.db") + "?mode=rwc",
		},
		{
			name: "memory database",
			path: ":memory:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userDB, err := NewUserDatabase(tt.path, logger.New("test", logger.DefaultConfig()))
			if err == nil {
				_ = userDB.Close()
				t.Fatal("NewUserDatabase() error = nil, want user database path validation error")
			}
			if !strings.Contains(err.Error(), "filesystem path") {
				t.Fatalf("NewUserDatabase() error = %v, want filesystem path validation error", err)
			}
		})
	}
}

func assertUserDBFileModeIfExists(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("stat %s: %v", path, err)
	}
	assertUserDBFileMode(t, path, want)
}

func assertUserDBFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}
