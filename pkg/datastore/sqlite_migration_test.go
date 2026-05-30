package datastore

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSQLiteMigrationConvertsLegacyLockTimestamps(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "config.db")
	db := openMigrationTestDB(t, dbPath)
	mustExec(t, db, `
		CREATE TABLE schema_version (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO schema_version (version) VALUES (1);
		CREATE TABLE config_locks (
			lock_id INTEGER PRIMARY KEY CHECK (lock_id = 1),
			session_id TEXT NOT NULL,
			user TEXT NOT NULL,
			acquired_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME NOT NULL,
			last_activity DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO config_locks (lock_id, session_id, user, acquired_at, expires_at, last_activity)
		VALUES (1, 'legacy-session', 'alice', '2026-05-11 00:00:00', '2099-01-02 03:04:05', '2026-05-11 00:01:00');
	`)
	closeDB(t, db)

	ds := openSQLiteDatastoreForTest(t, dbPath)

	var version int
	if err := ds.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&version); err != nil {
		t.Fatalf("schema version query failed: %v", err)
	}
	if version != 2 {
		t.Fatalf("schema version = %d, want 2", version)
	}

	var storageType string
	if err := ds.db.QueryRow(`SELECT typeof(expires_at) FROM config_locks WHERE target = ?`, LockTargetCandidate).Scan(&storageType); err != nil {
		t.Fatalf("lock timestamp type query failed: %v", err)
	}
	if storageType != "integer" {
		t.Fatalf("expires_at storage type = %q, want integer", storageType)
	}

	info, err := ds.GetLockInfo(context.Background(), LockTargetCandidate)
	if err != nil {
		t.Fatalf("GetLockInfo() error = %v", err)
	}
	if !info.IsLocked || info.SessionID != "legacy-session" || info.User != "alice" {
		t.Fatalf("GetLockInfo() = %#v, want migrated legacy lock", info)
	}
}

func TestSQLiteMigrationRepairsUnrecordedTargetLockMigration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "config.db")
	db := openMigrationTestDB(t, dbPath)
	future := time.Now().Add(time.Hour).UTC().Format("2006-01-02 15:04:05")
	mustExec(t, db, `
		CREATE TABLE schema_version (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO schema_version (version) VALUES (1);
		CREATE TABLE config_locks (
			target TEXT NOT NULL PRIMARY KEY CHECK(target IN ('candidate', 'running')),
			session_id TEXT NOT NULL,
			user TEXT NOT NULL,
			acquired_at INTEGER NOT NULL,
			expires_at INTEGER NOT NULL,
			last_activity INTEGER NOT NULL
		);
	`)
	if _, err := db.Exec(`
		INSERT INTO config_locks (target, session_id, user, acquired_at, expires_at, last_activity)
		VALUES (?, ?, ?, ?, ?, ?)
	`, LockTargetCandidate, "unrecorded-session", "bob", "2026-05-11 00:00:00", future, "2026-05-11 00:01:00"); err != nil {
		t.Fatalf("insert unrecorded target lock: %v", err)
	}
	closeDB(t, db)

	ds := openSQLiteDatastoreForTest(t, dbPath)

	var version int
	if err := ds.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&version); err != nil {
		t.Fatalf("schema version query failed: %v", err)
	}
	if version != 2 {
		t.Fatalf("schema version = %d, want repaired version 2", version)
	}

	info, err := ds.GetLockInfo(context.Background(), LockTargetCandidate)
	if err != nil {
		t.Fatalf("GetLockInfo() error = %v", err)
	}
	if !info.IsLocked || info.SessionID != "unrecorded-session" || info.User != "bob" {
		t.Fatalf("GetLockInfo() = %#v, want existing target lock", info)
	}
}

func TestSQLiteMigrationRejectsNewerSchemaVersion(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "config.db")
	db := openMigrationTestDB(t, dbPath)
	mustExec(t, db, `
		CREATE TABLE schema_version (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO schema_version (version) VALUES (999);
	`)
	closeDB(t, db)

	ds, err := NewSQLiteDatastore(&Config{
		Backend:    BackendSQLite,
		SQLitePath: dbPath,
	})
	if err == nil {
		_ = ds.Close()
		t.Fatal("NewSQLiteDatastore() error = nil, want newer schema version rejection")
	}
	if !strings.Contains(err.Error(), "newer than supported version") {
		t.Fatalf("NewSQLiteDatastore() error = %v, want newer schema version rejection", err)
	}
}

func TestSQLiteMigrationCreateBackupBindsBackupPath(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "config'; DROP TABLE sample; --.db")
	db := openMigrationTestDB(t, dbPath)
	defer closeDB(t, db)
	mustExec(t, db, `
		CREATE TABLE sample (
			id INTEGER PRIMARY KEY,
			value TEXT NOT NULL
		);
		INSERT INTO sample (value) VALUES ('kept');
	`)

	backupPath, err := NewSQLiteMigrationManager(db, dbPath).CreateBackup()
	if err != nil {
		t.Fatalf("CreateBackup() error = %v", err)
	}
	if backupPath == "" {
		t.Fatal("CreateBackup() backupPath is empty, want backup file")
	}
	assertSQLiteFileMode(t, backupPath, secureSQLiteFilePerms)

	var sourceValue string
	if err := db.QueryRow(`SELECT value FROM sample WHERE id = 1`).Scan(&sourceValue); err != nil {
		t.Fatalf("source query failed after CreateBackup(): %v", err)
	}
	if sourceValue != "kept" {
		t.Fatalf("source value = %q, want kept", sourceValue)
	}

	backupDB := openMigrationTestDB(t, backupPath)
	defer closeDB(t, backupDB)
	var backupValue string
	if err := backupDB.QueryRow(`SELECT value FROM sample WHERE id = 1`).Scan(&backupValue); err != nil {
		t.Fatalf("backup query failed: %v", err)
	}
	if backupValue != "kept" {
		t.Fatalf("backup value = %q, want kept", backupValue)
	}
}

func TestSQLiteMigrationCreateBackupAvoidsExistingBackupPath(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "config.db")
	db := openMigrationTestDB(t, dbPath)
	defer closeDB(t, db)
	mustExec(t, db, `
		CREATE TABLE sample (
			id INTEGER PRIMARY KEY,
			value TEXT NOT NULL
		);
		INSERT INTO sample (value) VALUES ('kept');
	`)

	now := time.Date(2026, 5, 23, 10, 11, 12, 0, time.UTC)
	timestamp := now.Format("20060102_150405")
	firstBackupPath := sqliteBackupPath(dbPath, timestamp, 0)
	if err := os.WriteFile(firstBackupPath, []byte("existing backup"), secureSQLiteFilePerms); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", firstBackupPath, err)
	}

	backupPath, err := NewSQLiteMigrationManager(db, dbPath).(*sqliteMigrationManager).createBackup(now)
	if err != nil {
		t.Fatalf("createBackup() error = %v", err)
	}
	if backupPath != sqliteBackupPath(dbPath, timestamp, 1) {
		t.Fatalf("backupPath = %q, want suffixed collision-free path", backupPath)
	}
	assertSQLiteFileMode(t, backupPath, secureSQLiteFilePerms)

	existing, err := os.ReadFile(firstBackupPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", firstBackupPath, err)
	}
	if string(existing) != "existing backup" {
		t.Fatalf("existing backup content = %q, want unchanged", existing)
	}

	backupDB := openMigrationTestDB(t, backupPath)
	defer closeDB(t, backupDB)
	var backupValue string
	if err := backupDB.QueryRow(`SELECT value FROM sample WHERE id = 1`).Scan(&backupValue); err != nil {
		t.Fatalf("backup query failed: %v", err)
	}
	if backupValue != "kept" {
		t.Fatalf("backup value = %q, want kept", backupValue)
	}
}

func TestSQLiteMigrationCreateBackupAvoidsBrokenSymlinkBackupPath(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "config.db")
	db := openMigrationTestDB(t, dbPath)
	defer closeDB(t, db)
	mustExec(t, db, `
		CREATE TABLE sample (
			id INTEGER PRIMARY KEY,
			value TEXT NOT NULL
		);
		INSERT INTO sample (value) VALUES ('kept');
	`)

	now := time.Date(2026, 5, 23, 10, 11, 12, 0, time.UTC)
	timestamp := now.Format("20060102_150405")
	firstBackupPath := sqliteBackupPath(dbPath, timestamp, 0)
	missingTargetPath := filepath.Join(dir, "missing-backup-target")
	if err := os.Symlink(missingTargetPath, firstBackupPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	backupPath, err := NewSQLiteMigrationManager(db, dbPath).(*sqliteMigrationManager).createBackup(now)
	if err != nil {
		t.Fatalf("createBackup() error = %v", err)
	}
	if backupPath != sqliteBackupPath(dbPath, timestamp, 1) {
		t.Fatalf("backupPath = %q, want suffixed collision-free path", backupPath)
	}
	if _, err := os.Lstat(firstBackupPath); err != nil {
		t.Fatalf("Lstat(%s) error = %v, want symlink preserved", firstBackupPath, err)
	}
	if _, err := os.Stat(missingTargetPath); !os.IsNotExist(err) {
		t.Fatalf("Stat(%s) error = %v, want missing symlink target", missingTargetPath, err)
	}
	assertSQLiteFileMode(t, backupPath, secureSQLiteFilePerms)

	backupDB := openMigrationTestDB(t, backupPath)
	defer closeDB(t, backupDB)
	var backupValue string
	if err := backupDB.QueryRow(`SELECT value FROM sample WHERE id = 1`).Scan(&backupValue); err != nil {
		t.Fatalf("backup query failed: %v", err)
	}
	if backupValue != "kept" {
		t.Fatalf("backup value = %q, want kept", backupValue)
	}
}

func TestSQLiteMigrationCreateBackupRejectsSymlinkSource(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "target-config.db")
	dbPath := filepath.Join(dir, "config.db")
	targetDB := openMigrationTestDB(t, targetPath)
	mustExec(t, targetDB, `
		CREATE TABLE sample (
			id INTEGER PRIMARY KEY,
			value TEXT NOT NULL
		);
		INSERT INTO sample (value) VALUES ('kept');
	`)
	closeDB(t, targetDB)
	if err := os.Symlink(targetPath, dbPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	db := openMigrationTestDB(t, dbPath)
	defer closeDB(t, db)
	backupPath, err := NewSQLiteMigrationManager(db, dbPath).CreateBackup()
	if err == nil {
		t.Fatalf("CreateBackup() error = nil, backupPath = %q, want symlink source rejection", backupPath)
	}
	if !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("CreateBackup() error = %v, want symbolic link rejection", err)
	}
}

func TestSQLiteMigrationCreateBackupRejectsHardLinkedSource(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "target-config.db")
	dbPath := filepath.Join(dir, "config.db")
	targetDB := openMigrationTestDB(t, targetPath)
	mustExec(t, targetDB, `
		CREATE TABLE sample (
			id INTEGER PRIMARY KEY,
			value TEXT NOT NULL
		);
		INSERT INTO sample (value) VALUES ('kept');
	`)
	closeDB(t, targetDB)
	if err := os.Link(targetPath, dbPath); err != nil {
		t.Skipf("hard links not supported: %v", err)
	}

	db := openMigrationTestDB(t, dbPath)
	defer closeDB(t, db)
	backupPath, err := NewSQLiteMigrationManager(db, dbPath).CreateBackup()
	if err == nil {
		t.Fatalf("CreateBackup() error = nil, backupPath = %q, want hard link source rejection", backupPath)
	}
	if !strings.Contains(err.Error(), "multiple hard links") {
		t.Fatalf("CreateBackup() error = %v, want hard link rejection", err)
	}
}

func openMigrationTestDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("open sqlite test db: %v", err)
	}
	return db
}

func openSQLiteDatastoreForTest(t *testing.T, dbPath string) *sqliteDatastore {
	t.Helper()
	ds, err := NewSQLiteDatastore(&Config{
		Backend:    BackendSQLite,
		SQLitePath: dbPath,
	})
	if err != nil {
		t.Fatalf("NewSQLiteDatastore() error = %v", err)
	}
	t.Cleanup(func() { _ = ds.Close() })

	sqliteDS, ok := ds.(*sqliteDatastore)
	if !ok {
		t.Fatalf("NewSQLiteDatastore() returned %T, want *sqliteDatastore", ds)
	}
	return sqliteDS
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec query failed: %v", err)
	}
}

func closeDB(t *testing.T, db *sql.DB) {
	t.Helper()
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
}
