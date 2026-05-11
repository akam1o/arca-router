package netconf

import (
	"os"
	"path/filepath"
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
