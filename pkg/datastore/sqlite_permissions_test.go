package datastore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLiteDatastoreRestrictsDatabaseFilePermissions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "config.db")
	if err := os.WriteFile(dbPath, []byte{}, 0644); err != nil {
		t.Fatalf("write preexisting db: %v", err)
	}

	ds := openSQLiteDatastoreForTest(t, dbPath)
	assertSQLiteFileMode(t, dbPath, secureSQLiteFilePerms)

	if _, err := ds.db.Exec(`INSERT INTO audit_log (user, action, result) VALUES ('alice', 'test', 'success')`); err != nil {
		t.Fatalf("force sqlite write: %v", err)
	}

	assertSQLiteFileModeIfExists(t, dbPath+"-wal", secureSQLiteFilePerms)
	assertSQLiteFileModeIfExists(t, dbPath+"-shm", secureSQLiteFilePerms)
}

func TestSQLiteDatastoreRejectsInsecureDatabaseDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "open")
	if err := os.Mkdir(dir, 0777); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	ds, err := NewSQLiteDatastore(&Config{
		Backend:    BackendSQLite,
		SQLitePath: filepath.Join(dir, "config.db"),
	})
	if err == nil {
		_ = ds.Close()
		t.Fatal("NewSQLiteDatastore() error = nil, want insecure directory error")
	}
}

func TestSQLiteDatastoreRejectsSymlinkDatabaseFile(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "target-config.db")
	dbPath := filepath.Join(dir, "config.db")
	if err := os.WriteFile(targetPath, []byte{}, 0644); err != nil {
		t.Fatalf("write target db: %v", err)
	}
	if err := os.Chmod(targetPath, 0644); err != nil {
		t.Fatalf("chmod target db: %v", err)
	}
	if err := os.Symlink(targetPath, dbPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	ds, err := NewSQLiteDatastore(&Config{
		Backend:    BackendSQLite,
		SQLitePath: dbPath,
	})
	if err == nil {
		_ = ds.Close()
		t.Fatal("NewSQLiteDatastore() error = nil, want symlink database rejection")
	}
	if !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("NewSQLiteDatastore() error = %v, want symbolic link rejection", err)
	}
	assertSQLiteFileMode(t, targetPath, 0644)
}

func TestSQLiteDatastoreRejectsHardLinkedDatabaseFile(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "target-config.db")
	dbPath := filepath.Join(dir, "config.db")
	if err := os.WriteFile(targetPath, []byte{}, 0600); err != nil {
		t.Fatalf("write target db: %v", err)
	}
	if err := os.Link(targetPath, dbPath); err != nil {
		t.Skipf("hard links not supported: %v", err)
	}

	ds, err := NewSQLiteDatastore(&Config{
		Backend:    BackendSQLite,
		SQLitePath: dbPath,
	})
	if err == nil {
		_ = ds.Close()
		t.Fatal("NewSQLiteDatastore() error = nil, want hard link database rejection")
	}
	if !strings.Contains(err.Error(), "multiple hard links") {
		t.Fatalf("NewSQLiteDatastore() error = %v, want hard link rejection", err)
	}
}

func TestSQLiteDatastoreRejectsSymlinkDatabaseDirectory(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, "target")
	dbDir := filepath.Join(root, "linked")
	if err := os.Mkdir(targetDir, 0750); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.Symlink(targetDir, dbDir); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	ds, err := NewSQLiteDatastore(&Config{
		Backend:    BackendSQLite,
		SQLitePath: filepath.Join(dbDir, "config.db"),
	})
	if err == nil {
		_ = ds.Close()
		t.Fatal("NewSQLiteDatastore() error = nil, want symlink directory rejection")
	}
	if !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("NewSQLiteDatastore() error = %v, want symbolic link rejection", err)
	}
}

func TestSQLiteDatastoreRejectsDSNPaths(t *testing.T) {
	tests := []struct {
		name   string
		dbPath string
	}{
		{
			name:   "query parameters",
			dbPath: filepath.Join(t.TempDir(), "config.db") + "?cache=shared",
		},
		{
			name:   "sqlite uri",
			dbPath: "file:" + filepath.Join(t.TempDir(), "config.db") + "?mode=rwc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds, err := NewSQLiteDatastore(&Config{
				Backend:    BackendSQLite,
				SQLitePath: tt.dbPath,
			})
			if err == nil {
				_ = ds.Close()
				t.Fatal("NewSQLiteDatastore() error = nil, want SQLite path validation error")
			}
			if !strings.Contains(err.Error(), "filesystem path") {
				t.Fatalf("NewSQLiteDatastore() error = %v, want filesystem path validation error", err)
			}
		})
	}
}

func TestAcquireSQLiteProcessLockExcludesSecondOwner(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "config.db")
	first, err := AcquireSQLiteProcessLock(dbPath)
	if err != nil {
		t.Fatalf("AcquireSQLiteProcessLock(first) error = %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })

	second, err := AcquireSQLiteProcessLock(dbPath)
	if err == nil {
		_ = second.Close()
		t.Fatal("AcquireSQLiteProcessLock(second) error = nil, want conflict")
	}
	var dsErr *Error
	if !errors.As(err, &dsErr) || dsErr.Code != ErrCodeConflict {
		t.Fatalf("AcquireSQLiteProcessLock(second) error = %v, want conflict", err)
	}

	if err := first.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	third, err := AcquireSQLiteProcessLock(dbPath)
	if err != nil {
		t.Fatalf("AcquireSQLiteProcessLock(third) error = %v", err)
	}
	if err := third.Close(); err != nil {
		t.Fatalf("third Close() error = %v", err)
	}
	assertSQLiteFileMode(t, dbPath+".process.lock", secureSQLiteFilePerms)
}

func TestAcquireSQLiteProcessLockRejectsSymlinkLockFile(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "config.db")
	targetPath := filepath.Join(dir, "target.lock")
	lockPath := dbPath + ".process.lock"
	if err := os.WriteFile(targetPath, []byte{}, 0644); err != nil {
		t.Fatalf("write target lock: %v", err)
	}
	if err := os.Chmod(targetPath, 0644); err != nil {
		t.Fatalf("chmod target lock: %v", err)
	}
	if err := os.Symlink(targetPath, lockPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	lock, err := AcquireSQLiteProcessLock(dbPath)
	if err == nil {
		_ = lock.Close()
		t.Fatal("AcquireSQLiteProcessLock() error = nil, want symlink lock rejection")
	}
	if !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("AcquireSQLiteProcessLock() error = %v, want symbolic link rejection", err)
	}
	assertSQLiteFileMode(t, targetPath, 0644)
}

func TestAcquireSQLiteProcessLockRejectsHardLinkedLockFile(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "config.db")
	targetPath := filepath.Join(dir, "target.lock")
	lockPath := dbPath + ".process.lock"
	if err := os.WriteFile(targetPath, []byte{}, 0600); err != nil {
		t.Fatalf("write target lock: %v", err)
	}
	if err := os.Link(targetPath, lockPath); err != nil {
		t.Skipf("hard links not supported: %v", err)
	}

	lock, err := AcquireSQLiteProcessLock(dbPath)
	if err == nil {
		_ = lock.Close()
		t.Fatal("AcquireSQLiteProcessLock() error = nil, want hard link lock rejection")
	}
	if !strings.Contains(err.Error(), "multiple hard links") {
		t.Fatalf("AcquireSQLiteProcessLock() error = %v, want hard link rejection", err)
	}
}

func TestAcquireSQLiteProcessLockRejectsDSNPath(t *testing.T) {
	dbPath := "file:" + filepath.Join(t.TempDir(), "config.db") + "?mode=rwc"

	lock, err := AcquireSQLiteProcessLock(dbPath)
	if err == nil {
		_ = lock.Close()
		t.Fatal("AcquireSQLiteProcessLock() error = nil, want SQLite path validation error")
	}
	if !strings.Contains(err.Error(), "filesystem path") {
		t.Fatalf("AcquireSQLiteProcessLock() error = %v, want filesystem path validation error", err)
	}
}

func TestRestrictSQLiteFilePermissionsRejectsSymlinkSidecarFile(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "config.db")
	targetPath := filepath.Join(dir, "target-wal")
	if err := os.WriteFile(dbPath, []byte{}, secureSQLiteFilePerms); err != nil {
		t.Fatalf("write db: %v", err)
	}
	if err := os.WriteFile(targetPath, []byte{}, 0644); err != nil {
		t.Fatalf("write target sidecar: %v", err)
	}
	if err := os.Chmod(targetPath, 0644); err != nil {
		t.Fatalf("chmod target sidecar: %v", err)
	}
	if err := os.Symlink(targetPath, dbPath+"-wal"); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	err := restrictSQLiteFilePermissions(dbPath)
	if err == nil {
		t.Fatal("restrictSQLiteFilePermissions() error = nil, want symlink sidecar rejection")
	}
	if !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("restrictSQLiteFilePermissions() error = %v, want symbolic link rejection", err)
	}
	assertSQLiteFileMode(t, targetPath, 0644)
}

func assertSQLiteFileModeIfExists(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("stat %s: %v", path, err)
	}
	assertSQLiteFileMode(t, path, want)
}

func assertSQLiteFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}
