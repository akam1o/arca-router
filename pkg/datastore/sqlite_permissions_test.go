package datastore

import (
	"os"
	"path/filepath"
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
